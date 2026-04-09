package history

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	maxHistoryItems          = 100
	maxPastedContentLength   = 1024
	defaultFlushInterval     = 5 * time.Second
	historyFileName          = "history.jsonl"
)

// PastedContentType represents the type of pasted content.
type PastedContentType string

const (
	PastedContentTypeText  PastedContentType = "text"
	PastedContentTypeImage PastedContentType = "image"
)

// PastedContent represents content that was pasted by the user.
type PastedContent struct {
	ID          int               `json:"id"`
	Type        PastedContentType `json:"type"`
	Content     string            `json:"content,omitempty"`
	ContentHash string            `json:"content_hash,omitempty"`
	MediaType   string            `json:"media_type,omitempty"`
	Filename    string            `json:"filename,omitempty"`
}

// HistoryEntry represents a command history entry.
type HistoryEntry struct {
	Display        string                   `json:"display"`
	PastedContents map[int]*PastedContent   `json:"pasted_contents,omitempty"`
}

// LogEntry represents an entry in the history log file.
type LogEntry struct {
	SessionID      string                   `json:"session_id"`
	Project        string                   `json:"project"`
	Display        string                   `json:"display"`
	Timestamp      int64                    `json:"timestamp"`
	PastedContents map[int]*PastedContent   `json:"pasted_contents,omitempty"`
}

// Manager manages command history with async flushing.
type Manager struct {
	mu                sync.RWMutex
	historyDir        string
	sessionID         string
	projectRoot       string
	pendingEntries    []*LogEntry
	lastAddedEntry    *LogEntry
	skippedTimestamps map[int64]bool
	flushTimer        *time.Timer
	flushMu           sync.Mutex
	stopFlush         chan struct{}
	wg                sync.WaitGroup
}

// NewManager creates a new history manager.
func NewManager(historyDir, sessionID, projectRoot string) *Manager {
	m := &Manager{
		historyDir:        historyDir,
		sessionID:         sessionID,
		projectRoot:       projectRoot,
		pendingEntries:    make([]*LogEntry, 0),
		skippedTimestamps: make(map[int64]bool),
		stopFlush:         make(chan struct{}),
	}
	m.startFlushTimer()
	return m
}

// AddToHistory adds a new entry to history.
func (m *Manager) AddToHistory(entry *HistoryEntry) {
	m.mu.Lock()
	defer m.mu.Unlock()

	logEntry := &LogEntry{
		SessionID:      m.sessionID,
		Project:        m.projectRoot,
		Display:        entry.Display,
		Timestamp:      time.Now().UnixMilli(),
		PastedContents: entry.PastedContents,
	}

	m.pendingEntries = append(m.pendingEntries, logEntry)
	m.lastAddedEntry = logEntry

	// Reset flush timer
	if m.flushTimer != nil {
		m.flushTimer.Reset(defaultFlushInterval)
	}
}

// RemoveLastFromHistory removes the most recently added entry.
// Used for undo operations (e.g., when user interrupts before response).
func (m *Manager) RemoveLastFromHistory() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.lastAddedEntry == nil {
		return
	}

	entry := m.lastAddedEntry
	m.lastAddedEntry = nil

	// Try to remove from pending buffer (fast path)
	for i := len(m.pendingEntries) - 1; i >= 0; i-- {
		if m.pendingEntries[i] == entry {
			m.pendingEntries = append(m.pendingEntries[:i], m.pendingEntries[i+1:]...)
			return
		}
	}

	// Already flushed, mark for skipping during reads
	m.skippedTimestamps[entry.Timestamp] = true
}

// GetHistory returns recent history entries for the current project.
func (m *Manager) GetHistory(limit int) ([]*HistoryEntry, error) {
	if limit <= 0 {
		limit = maxHistoryItems
	}

	entries := make([]*HistoryEntry, 0, limit)
	seen := make(map[string]bool)

	// Add pending entries first (newest)
	m.mu.RLock()
	for i := len(m.pendingEntries) - 1; i >= 0 && len(entries) < limit; i-- {
		entry := m.pendingEntries[i]
		if entry.Project == m.projectRoot && !seen[entry.Display] {
			entries = append(entries, &HistoryEntry{
				Display:        entry.Display,
				PastedContents: entry.PastedContents,
			})
			seen[entry.Display] = true
		}
	}
	m.mu.RUnlock()

	// Read from disk
	historyPath := filepath.Join(m.historyDir, historyFileName)
	file, err := os.Open(historyPath)
	if err != nil {
		if os.IsNotExist(err) {
			return entries, nil
		}
		return nil, fmt.Errorf("failed to open history file: %w", err)
	}
	defer file.Close()

	// Read lines in reverse order (newest first)
	lines := make([]string, 0)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	for i := len(lines) - 1; i >= 0 && len(entries) < limit; i-- {
		var logEntry LogEntry
		if err := json.Unmarshal([]byte(lines[i]), &logEntry); err != nil {
			continue
		}

		// Skip if from different project or already seen
		if logEntry.Project != m.projectRoot || seen[logEntry.Display] {
			continue
		}

		// Skip if marked for removal
		if logEntry.SessionID == m.sessionID && m.skippedTimestamps[logEntry.Timestamp] {
			continue
		}

		entries = append(entries, &HistoryEntry{
			Display:        logEntry.Display,
			PastedContents: logEntry.PastedContents,
		})
		seen[logEntry.Display] = true
	}

	return entries, nil
}

// Flush writes pending entries to disk.
func (m *Manager) Flush() error {
	m.flushMu.Lock()
	defer m.flushMu.Unlock()

	m.mu.Lock()
	if len(m.pendingEntries) == 0 {
		m.mu.Unlock()
		return nil
	}

	entries := make([]*LogEntry, len(m.pendingEntries))
	copy(entries, m.pendingEntries)
	m.pendingEntries = m.pendingEntries[:0]
	m.mu.Unlock()

	// Ensure directory exists
	if err := os.MkdirAll(m.historyDir, 0755); err != nil {
		return fmt.Errorf("failed to create history directory: %w", err)
	}

	historyPath := filepath.Join(m.historyDir, historyFileName)
	file, err := os.OpenFile(historyPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open history file: %w", err)
	}
	defer file.Close()

	writer := bufio.NewWriter(file)
	for _, entry := range entries {
		data, err := json.Marshal(entry)
		if err != nil {
			continue
		}
		writer.Write(data)
		writer.WriteByte('\n')
	}

	return writer.Flush()
}

// startFlushTimer starts the periodic flush timer.
func (m *Manager) startFlushTimer() {
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		ticker := time.NewTicker(defaultFlushInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				m.Flush()
			case <-m.stopFlush:
				return
			}
		}
	}()
}

// Close flushes pending entries and stops the flush timer.
func (m *Manager) Close() error {
	close(m.stopFlush)
	m.wg.Wait()
	return m.Flush()
}

// ClearPendingEntries clears all pending entries without flushing.
func (m *Manager) ClearPendingEntries() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pendingEntries = m.pendingEntries[:0]
	m.lastAddedEntry = nil
	m.skippedTimestamps = make(map[int64]bool)
}

// FormatPastedTextRef formats a pasted text reference.
func FormatPastedTextRef(id, numLines int) string {
	if numLines == 0 {
		return fmt.Sprintf("[Pasted text #%d]", id)
	}
	return fmt.Sprintf("[Pasted text #%d +%d lines]", id, numLines)
}

// FormatImageRef formats an image reference.
func FormatImageRef(id int) string {
	return fmt.Sprintf("[Image #%d]", id)
}

// GetPastedTextRefNumLines counts the number of newlines in text.
func GetPastedTextRefNumLines(text string) int {
	return strings.Count(text, "\n")
}

// ExpandPastedTextRefs replaces [Pasted text #N] placeholders with actual content.
func ExpandPastedTextRefs(input string, pastedContents map[int]*PastedContent) string {
	// Simple implementation - can be optimized with regex if needed
	result := input
	for id, content := range pastedContents {
		if content.Type != PastedContentTypeText {
			continue
		}
		placeholder := FormatPastedTextRef(id, GetPastedTextRefNumLines(content.Content))
		result = strings.ReplaceAll(result, placeholder, content.Content)
	}
	return result
}
