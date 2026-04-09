package storage

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	// FlushIntervalMS is the default interval for batching writes
	FlushIntervalMS = 100 * time.Millisecond
	// MaxChunkBytes is the maximum size of a single write batch
	MaxChunkBytes = 100 * 1024 * 1024 // 100MB
	// LiteReadBufSize is the buffer size for reading tail of transcript
	LiteReadBufSize = 64 * 1024 // 64KB
)

// TranscriptWriter handles writing entries to JSONL transcript files
type TranscriptWriter struct {
	mu              sync.Mutex
	filePath        string
	writeQueue      []queuedEntry
	flushTimer      *time.Timer
	pendingWrites   int
	flushResolvers  []chan struct{}
	metadata        *SessionMetadata
	sessionID       string
	closed          bool
}

type queuedEntry struct {
	entry   Entry
	resolve chan struct{}
}

// NewTranscriptWriter creates a new transcript writer
func NewTranscriptWriter(filePath, sessionID string) *TranscriptWriter {
	return &TranscriptWriter{
		filePath:       filePath,
		sessionID:      sessionID,
		writeQueue:     make([]queuedEntry, 0, 32),
		flushResolvers: make([]chan struct{}, 0),
		metadata:       &SessionMetadata{},
	}
}

// AppendEntry adds an entry to the write queue
func (tw *TranscriptWriter) AppendEntry(entry Entry) error {
	tw.mu.Lock()
	defer tw.mu.Unlock()

	if tw.closed {
		return fmt.Errorf("transcript writer is closed")
	}

	resolve := make(chan struct{})
	tw.writeQueue = append(tw.writeQueue, queuedEntry{
		entry:   entry,
		resolve: resolve,
	})

	tw.scheduleDrain()
	return nil
}

// AppendEntrySync adds an entry and waits for it to be written
func (tw *TranscriptWriter) AppendEntrySync(entry Entry) error {
	tw.mu.Lock()
	if tw.closed {
		tw.mu.Unlock()
		return fmt.Errorf("transcript writer is closed")
	}

	resolve := make(chan struct{})
	tw.writeQueue = append(tw.writeQueue, queuedEntry{
		entry:   entry,
		resolve: resolve,
	})

	tw.scheduleDrain()
	tw.mu.Unlock()

	// Wait for this specific entry to be written
	<-resolve
	return nil
}

// scheduleDrain schedules a flush if not already scheduled
func (tw *TranscriptWriter) scheduleDrain() {
	if tw.flushTimer != nil {
		return
	}

	tw.flushTimer = time.AfterFunc(FlushIntervalMS, func() {
		tw.mu.Lock()
		tw.flushTimer = nil
		tw.mu.Unlock()
		tw.drainWriteQueue()
	})
}

// drainWriteQueue writes all queued entries to disk
func (tw *TranscriptWriter) drainWriteQueue() error {
	tw.mu.Lock()
	if len(tw.writeQueue) == 0 {
		tw.mu.Unlock()
		return nil
	}

	batch := tw.writeQueue
	tw.writeQueue = make([]queuedEntry, 0, 32)
	tw.pendingWrites++
	tw.mu.Unlock()

	defer func() {
		tw.mu.Lock()
		tw.pendingWrites--
		if tw.pendingWrites == 0 {
			// Resolve all waiting flush promises
			for _, ch := range tw.flushResolvers {
				close(ch)
			}
			tw.flushResolvers = tw.flushResolvers[:0]
		}
		tw.mu.Unlock()
	}()

	var content []byte
	resolvers := make([]chan struct{}, 0, len(batch))

	for _, item := range batch {
		line, err := json.Marshal(item.entry)
		if err != nil {
			close(item.resolve)
			continue
		}
		line = append(line, '\n')

		// If adding this line would exceed max chunk size, flush first
		if len(content)+len(line) >= MaxChunkBytes {
			if err := tw.appendToFile(content); err != nil {
				// Resolve with error (close channel to unblock)
				for _, ch := range resolvers {
					close(ch)
				}
				return err
			}
			// Resolve successfully written entries
			for _, ch := range resolvers {
				close(ch)
			}
			content = content[:0]
			resolvers = resolvers[:0]
		}

		content = append(content, line...)
		resolvers = append(resolvers, item.resolve)
	}

	// Write remaining content
	if len(content) > 0 {
		if err := tw.appendToFile(content); err != nil {
			for _, ch := range resolvers {
				close(ch)
			}
			return err
		}
	}

	// Resolve all successfully written entries
	for _, ch := range resolvers {
		close(ch)
	}

	return nil
}

// appendToFile appends data to the transcript file
func (tw *TranscriptWriter) appendToFile(data []byte) error {
	// Ensure directory exists
	dir := filepath.Dir(tw.filePath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Open file in append mode
	f, err := os.OpenFile(tw.filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer f.Close()

	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("failed to write data: %w", err)
	}

	return nil
}

// Flush waits for all pending writes to complete
func (tw *TranscriptWriter) Flush() error {
	tw.mu.Lock()

	// Cancel pending timer and drain immediately
	if tw.flushTimer != nil {
		tw.flushTimer.Stop()
		tw.flushTimer = nil
	}

	// If no pending writes, return immediately
	if tw.pendingWrites == 0 && len(tw.writeQueue) == 0 {
		tw.mu.Unlock()
		return nil
	}

	// Create a channel to wait for flush completion
	waitCh := make(chan struct{})
	tw.flushResolvers = append(tw.flushResolvers, waitCh)
	tw.mu.Unlock()

	// Drain the queue
	if err := tw.drainWriteQueue(); err != nil {
		return err
	}

	// Wait for all pending writes to complete
	<-waitCh
	return nil
}

// Close flushes and closes the writer
func (tw *TranscriptWriter) Close() error {
	tw.mu.Lock()
	if tw.closed {
		tw.mu.Unlock()
		return nil
	}
	tw.closed = true
	tw.mu.Unlock()

	return tw.Flush()
}

// UpdateMetadata updates cached session metadata
func (tw *TranscriptWriter) UpdateMetadata(meta *SessionMetadata) {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	tw.metadata = meta
}

// GetMetadata returns the current cached metadata
func (tw *TranscriptWriter) GetMetadata() *SessionMetadata {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	return tw.metadata
}

// TranscriptReader handles reading entries from JSONL transcript files
type TranscriptReader struct {
	filePath string
}

// NewTranscriptReader creates a new transcript reader
func NewTranscriptReader(filePath string) *TranscriptReader {
	return &TranscriptReader{
		filePath: filePath,
	}
}

// ReadAll reads all entries from the transcript file
func (tr *TranscriptReader) ReadAll() ([]Entry, error) {
	f, err := os.Open(tr.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return []Entry{}, nil
		}
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer f.Close()

	var entries []Entry
	scanner := bufio.NewScanner(f)
	// Increase buffer size for large lines
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 10*1024*1024) // 10MB max line size

	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		entry, err := tr.parseEntry(line)
		if err != nil {
			// Log error but continue reading
			fmt.Fprintf(os.Stderr, "Warning: failed to parse line %d: %v\n", lineNum, err)
			continue
		}

		entries = append(entries, entry)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading file: %w", err)
	}

	return entries, nil
}

// ReadTail reads the last N bytes from the file
func (tr *TranscriptReader) ReadTail(size int) ([]byte, error) {
	f, err := os.Open(tr.filePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		return nil, err
	}

	fileSize := stat.Size()
	if fileSize == 0 {
		return []byte{}, nil
	}

	readSize := int64(size)
	if readSize > fileSize {
		readSize = fileSize
	}

	offset := fileSize - readSize
	buf := make([]byte, readSize)

	_, err = f.ReadAt(buf, offset)
	if err != nil && err != io.EOF {
		return nil, err
	}

	return buf, nil
}

// parseEntry parses a JSON line into an Entry
func (tr *TranscriptReader) parseEntry(line []byte) (Entry, error) {
	// First, parse to determine the type
	var base BaseEntry
	if err := json.Unmarshal(line, &base); err != nil {
		return nil, err
	}

	// Parse into the appropriate concrete type based on entry type
	switch base.Type {
	case EntryTypeUser, EntryTypeAssistant, EntryTypeAttachment, EntryTypeSystem, EntryTypeTool:
		var msg TranscriptMessage
		if err := json.Unmarshal(line, &msg); err != nil {
			return nil, err
		}
		return &msg, nil

	case EntryTypeCustomTitle, EntryTypeAITitle, EntryTypeLastPrompt, EntryTypeTag,
		EntryTypeAgentName, EntryTypeAgentColor, EntryTypeAgentSetting, EntryTypeMode, EntryTypeTaskSummary:
		var meta MetadataEntry
		if err := json.Unmarshal(line, &meta); err != nil {
			return nil, err
		}
		return &meta, nil

	case EntryTypePRLink:
		var pr PRLinkEntry
		if err := json.Unmarshal(line, &pr); err != nil {
			return nil, err
		}
		return &pr, nil

	case EntryTypeWorktreeState:
		var wt WorktreeStateEntry
		if err := json.Unmarshal(line, &wt); err != nil {
			return nil, err
		}
		return &wt, nil

	case EntryTypeFileHistorySnapshot:
		var fh FileHistorySnapshotEntry
		if err := json.Unmarshal(line, &fh); err != nil {
			return nil, err
		}
		return &fh, nil

	case EntryTypeContentReplacement:
		var cr ContentReplacementEntry
		if err := json.Unmarshal(line, &cr); err != nil {
			return nil, err
		}
		return &cr, nil

	default:
		// Unknown type, return as base entry
		return &base, nil
	}
}
