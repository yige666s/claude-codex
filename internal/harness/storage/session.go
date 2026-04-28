package storage

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// SessionStorage manages session persistence and transcript storage
type SessionStorage struct {
	mu             sync.RWMutex
	homeDir        string
	sessionID      string
	projectDir     string
	writer         *TranscriptWriter
	metadata       *SessionMetadata
	transcriptPath string
}

// NewSessionStorage creates a new session storage manager
func NewSessionStorage(homeDir, sessionID, projectDir string) (*SessionStorage, error) {
	if sessionID == "" {
		sessionID = generateSessionID()
	}

	transcriptPath, err := ensureTranscriptPath(homeDir, projectDir, sessionID)
	if err != nil {
		return nil, err
	}

	ss := &SessionStorage{
		homeDir:        homeDir,
		sessionID:      sessionID,
		projectDir:     projectDir,
		transcriptPath: transcriptPath,
		metadata:       &SessionMetadata{},
	}

	// Initialize writer
	ss.writer = NewTranscriptWriter(transcriptPath, sessionID)

	return ss, nil
}

func ensureTranscriptPath(homeDir, projectDir, sessionID string) (string, error) {
	targetPath := transcriptPath(homeDir, projectDir, sessionID)
	if _, err := os.Stat(targetPath); err == nil {
		return targetPath, nil
	} else if !os.IsNotExist(err) {
		return "", err
	}

	legacyPath := legacyTranscriptPath(projectDir, sessionID)
	if _, err := os.Stat(legacyPath); err == nil {
		if err := moveLegacyTranscript(legacyPath, targetPath); err != nil {
			return "", err
		}
	}

	return targetPath, nil
}

func transcriptDir(homeDir, projectDir string) string {
	return filepath.Join(projectRoot(homeDir, projectDir), "sessions")
}

func transcriptPath(homeDir, projectDir, sessionID string) string {
	return filepath.Join(transcriptDir(homeDir, projectDir), sessionID+".jsonl")
}

func legacyTranscriptPath(projectDir, sessionID string) string {
	return filepath.Join(projectDir, sessionID+".jsonl")
}

func projectRoot(homeDir, projectDir string) string {
	return filepath.Join(homeDir, "projects", projectStorageKey(projectDir))
}

func projectStorageKey(projectDir string) string {
	clean := filepath.Clean(projectDir)
	abs := clean
	if resolved, err := filepath.Abs(clean); err == nil {
		abs = resolved
	}

	sum := sha256.Sum256([]byte(abs))
	return fmt.Sprintf("%s-%s", sanitizeProjectPart(filepath.Base(clean)), hex.EncodeToString(sum[:6]))
}

func sanitizeProjectPart(name string) string {
	name = strings.TrimSpace(strings.ToLower(name))
	if name == "" || name == "." || name == string(filepath.Separator) {
		return "project"
	}

	var b strings.Builder
	lastDash := false
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		case r == '-' || r == '_':
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		default:
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}

	sanitized := strings.Trim(b.String(), "-")
	if sanitized == "" {
		return "project"
	}
	return sanitized
}

func moveLegacyTranscript(sourcePath, targetPath string) error {
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o700); err != nil {
		return fmt.Errorf("create transcript dir: %w", err)
	}
	if err := os.Rename(sourcePath, targetPath); err == nil {
		return nil
	} else if !isCrossDeviceRename(err) {
		return fmt.Errorf("move legacy transcript: %w", err)
	}

	source, err := os.Open(sourcePath)
	if err != nil {
		return fmt.Errorf("open legacy transcript: %w", err)
	}
	defer source.Close()

	target, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("create migrated transcript: %w", err)
	}

	if _, err := source.WriteTo(target); err != nil {
		_ = target.Close()
		return fmt.Errorf("copy legacy transcript: %w", err)
	}
	if err := target.Close(); err != nil {
		return fmt.Errorf("close migrated transcript: %w", err)
	}

	if err := os.Remove(sourcePath); err != nil {
		return fmt.Errorf("remove legacy transcript: %w", err)
	}
	return nil
}

func isCrossDeviceRename(err error) bool {
	linkErr, ok := err.(*os.LinkError)
	return ok && strings.Contains(strings.ToLower(linkErr.Err.Error()), "cross-device")
}

// GetSessionID returns the current session ID
func (ss *SessionStorage) GetSessionID() string {
	ss.mu.RLock()
	defer ss.mu.RUnlock()
	return ss.sessionID
}

// GetTranscriptPath returns the path to the transcript file
func (ss *SessionStorage) GetTranscriptPath() string {
	ss.mu.RLock()
	defer ss.mu.RUnlock()
	return ss.transcriptPath
}

// RecordMessage records a conversation message
func (ss *SessionStorage) RecordMessage(msg *TranscriptMessage) error {
	ss.mu.Lock()
	msg.SessionID = ss.sessionID
	msg.Timestamp = time.Now().UTC().Format(time.RFC3339)
	ss.mu.Unlock()

	return ss.writer.AppendEntry(msg)
}

// RecordMessageSync records a message and waits for it to be written
func (ss *SessionStorage) RecordMessageSync(msg *TranscriptMessage) error {
	ss.mu.Lock()
	msg.SessionID = ss.sessionID
	msg.Timestamp = time.Now().UTC().Format(time.RFC3339)
	ss.mu.Unlock()

	return ss.writer.AppendEntrySync(msg)
}

// SetCustomTitle sets a custom title for the session
func (ss *SessionStorage) SetCustomTitle(title string) error {
	ss.mu.Lock()
	ss.metadata.CustomTitle = title
	ss.mu.Unlock()

	entry := &MetadataEntry{
		BaseEntry: BaseEntry{
			Type:      EntryTypeCustomTitle,
			SessionID: ss.sessionID,
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		},
		CustomTitle: title,
	}

	return ss.writer.AppendEntry(entry)
}

// SetTag sets a tag for the session
func (ss *SessionStorage) SetTag(tag string) error {
	ss.mu.Lock()
	ss.metadata.Tag = tag
	ss.mu.Unlock()

	entry := &MetadataEntry{
		BaseEntry: BaseEntry{
			Type:      EntryTypeTag,
			SessionID: ss.sessionID,
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		},
		Tag: tag,
	}

	return ss.writer.AppendEntry(entry)
}

// SetAgentName sets the agent name for the session
func (ss *SessionStorage) SetAgentName(name string) error {
	ss.mu.Lock()
	ss.metadata.AgentName = name
	ss.mu.Unlock()

	entry := &MetadataEntry{
		BaseEntry: BaseEntry{
			Type:      EntryTypeAgentName,
			SessionID: ss.sessionID,
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		},
		AgentName: name,
	}

	return ss.writer.AppendEntry(entry)
}

// SetAgentColor sets the agent color for the session
func (ss *SessionStorage) SetAgentColor(color string) error {
	ss.mu.Lock()
	ss.metadata.AgentColor = color
	ss.mu.Unlock()

	entry := &MetadataEntry{
		BaseEntry: BaseEntry{
			Type:      EntryTypeAgentColor,
			SessionID: ss.sessionID,
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		},
		AgentColor: color,
	}

	return ss.writer.AppendEntry(entry)
}

// SetLastPrompt sets the last user prompt
func (ss *SessionStorage) SetLastPrompt(prompt string) error {
	ss.mu.Lock()
	ss.metadata.LastPrompt = prompt
	ss.mu.Unlock()

	entry := &MetadataEntry{
		BaseEntry: BaseEntry{
			Type:      EntryTypeLastPrompt,
			SessionID: ss.sessionID,
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		},
		LastPrompt: prompt,
	}

	return ss.writer.AppendEntry(entry)
}

// LinkPR links a GitHub PR to the session
func (ss *SessionStorage) LinkPR(prNumber int, prUrl, prRepository string) error {
	ss.mu.Lock()
	ss.metadata.PRNumber = prNumber
	ss.metadata.PRUrl = prUrl
	ss.metadata.PRRepository = prRepository
	ss.mu.Unlock()

	entry := &PRLinkEntry{
		BaseEntry: BaseEntry{
			Type:      EntryTypePRLink,
			SessionID: ss.sessionID,
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		},
		PRNumber:     prNumber,
		PRUrl:        prUrl,
		PRRepository: prRepository,
	}

	return ss.writer.AppendEntry(entry)
}

// SetWorktreeState sets the worktree state for the session
func (ss *SessionStorage) SetWorktreeState(state *WorktreeSession) error {
	ss.mu.Lock()
	ss.metadata.WorktreeState = state
	ss.mu.Unlock()

	entry := &WorktreeStateEntry{
		BaseEntry: BaseEntry{
			Type:      EntryTypeWorktreeState,
			SessionID: ss.sessionID,
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		},
		WorktreeSession: state,
	}

	return ss.writer.AppendEntry(entry)
}

// RecordFileHistorySnapshot records a file history snapshot
func (ss *SessionStorage) RecordFileHistorySnapshot(messageID string, snapshot map[string]interface{}, isUpdate bool) error {
	entry := &FileHistorySnapshotEntry{
		BaseEntry: BaseEntry{
			Type:      EntryTypeFileHistorySnapshot,
			SessionID: ss.sessionID,
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		},
		MessageID:        messageID,
		Snapshot:         snapshot,
		IsSnapshotUpdate: isUpdate,
	}

	return ss.writer.AppendEntry(entry)
}

// RecordContentReplacement records a content replacement decision
func (ss *SessionStorage) RecordContentReplacement(messageID, decision, replacement string) error {
	entry := &ContentReplacementEntry{
		BaseEntry: BaseEntry{
			Type:      EntryTypeContentReplacement,
			SessionID: ss.sessionID,
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		},
		MessageID:   messageID,
		Decision:    decision,
		Replacement: replacement,
	}

	return ss.writer.AppendEntry(entry)
}

// GetMetadata returns the current session metadata
func (ss *SessionStorage) GetMetadata() *SessionMetadata {
	ss.mu.RLock()
	defer ss.mu.RUnlock()

	// Return a copy to prevent external modification
	meta := *ss.metadata
	return &meta
}

// LoadTranscript loads the transcript from disk
func (ss *SessionStorage) LoadTranscript() ([]Entry, error) {
	reader := NewTranscriptReader(ss.transcriptPath)
	entries, err := reader.ReadAll()
	if err != nil {
		return nil, err
	}

	// Update metadata from loaded entries
	ss.updateMetadataFromEntries(entries)

	return entries, nil
}

// updateMetadataFromEntries updates cached metadata from loaded entries
func (ss *SessionStorage) updateMetadataFromEntries(entries []Entry) {
	ss.mu.Lock()
	defer ss.mu.Unlock()

	// Process entries in order, later entries override earlier ones
	for _, entry := range entries {
		switch e := entry.(type) {
		case *MetadataEntry:
			if e.CustomTitle != "" {
				ss.metadata.CustomTitle = e.CustomTitle
			}
			if e.AITitle != "" {
				ss.metadata.AITitle = e.AITitle
			}
			if e.LastPrompt != "" {
				ss.metadata.LastPrompt = e.LastPrompt
			}
			if e.Tag != "" {
				ss.metadata.Tag = e.Tag
			}
			if e.AgentName != "" {
				ss.metadata.AgentName = e.AgentName
			}
			if e.AgentColor != "" {
				ss.metadata.AgentColor = e.AgentColor
			}
			if e.AgentSetting != "" {
				ss.metadata.AgentSetting = e.AgentSetting
			}
			if e.Mode != "" {
				ss.metadata.Mode = e.Mode
			}
			if e.TaskSummary != "" {
				ss.metadata.TaskSummary = e.TaskSummary
			}

		case *PRLinkEntry:
			ss.metadata.PRNumber = e.PRNumber
			ss.metadata.PRUrl = e.PRUrl
			ss.metadata.PRRepository = e.PRRepository

		case *WorktreeStateEntry:
			ss.metadata.WorktreeState = e.WorktreeSession
		}
	}
}

// Flush flushes all pending writes
func (ss *SessionStorage) Flush() error {
	return ss.writer.Flush()
}

// Close closes the session storage
func (ss *SessionStorage) Close() error {
	return ss.writer.Close()
}

// ReAppendMetadata re-appends session metadata to keep it at the end of the file
// This ensures metadata is in the tail window for fast reads
func (ss *SessionStorage) ReAppendMetadata() error {
	ss.mu.RLock()
	meta := ss.metadata
	sessionID := ss.sessionID
	ss.mu.RUnlock()

	// Re-append metadata entries in order
	if meta.LastPrompt != "" {
		entry := &MetadataEntry{
			BaseEntry: BaseEntry{
				Type:      EntryTypeLastPrompt,
				SessionID: sessionID,
				Timestamp: time.Now().UTC().Format(time.RFC3339),
			},
			LastPrompt: meta.LastPrompt,
		}
		if err := ss.writer.AppendEntry(entry); err != nil {
			return err
		}
	}

	if meta.CustomTitle != "" {
		entry := &MetadataEntry{
			BaseEntry: BaseEntry{
				Type:      EntryTypeCustomTitle,
				SessionID: sessionID,
				Timestamp: time.Now().UTC().Format(time.RFC3339),
			},
			CustomTitle: meta.CustomTitle,
		}
		if err := ss.writer.AppendEntry(entry); err != nil {
			return err
		}
	}

	if meta.Tag != "" {
		entry := &MetadataEntry{
			BaseEntry: BaseEntry{
				Type:      EntryTypeTag,
				SessionID: sessionID,
				Timestamp: time.Now().UTC().Format(time.RFC3339),
			},
			Tag: meta.Tag,
		}
		if err := ss.writer.AppendEntry(entry); err != nil {
			return err
		}
	}

	if meta.AgentName != "" {
		entry := &MetadataEntry{
			BaseEntry: BaseEntry{
				Type:      EntryTypeAgentName,
				SessionID: sessionID,
				Timestamp: time.Now().UTC().Format(time.RFC3339),
			},
			AgentName: meta.AgentName,
		}
		if err := ss.writer.AppendEntry(entry); err != nil {
			return err
		}
	}

	if meta.AgentColor != "" {
		entry := &MetadataEntry{
			BaseEntry: BaseEntry{
				Type:      EntryTypeAgentColor,
				SessionID: sessionID,
				Timestamp: time.Now().UTC().Format(time.RFC3339),
			},
			AgentColor: meta.AgentColor,
		}
		if err := ss.writer.AppendEntry(entry); err != nil {
			return err
		}
	}

	return nil
}

// generateSessionID generates a new random session ID
func generateSessionID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// Fallback to timestamp-based ID
		return fmt.Sprintf("session-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// SessionExists checks if a session file exists
func SessionExists(homeDir, projectDir, sessionID string) bool {
	if _, err := os.Stat(transcriptPath(homeDir, projectDir, sessionID)); err == nil {
		return true
	}
	_, err := os.Stat(legacyTranscriptPath(projectDir, sessionID))
	return err == nil
}

// ListSessions lists all session files in a project directory
func ListSessions(homeDir, projectDir string) ([]string, error) {
	seen := make(map[string]struct{})
	sessions := make([]string, 0)
	for _, dir := range []string{transcriptDir(homeDir, projectDir), projectDir} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}

		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			name := entry.Name()
			if filepath.Ext(name) != ".jsonl" {
				continue
			}
			sessionID := name[:len(name)-6]
			if _, ok := seen[sessionID]; ok {
				continue
			}
			seen[sessionID] = struct{}{}
			sessions = append(sessions, sessionID)
		}
	}

	return sessions, nil
}

// DeleteSession deletes a session file
func DeleteSession(homeDir, projectDir, sessionID string) error {
	paths := []string{
		transcriptPath(homeDir, projectDir, sessionID),
		legacyTranscriptPath(projectDir, sessionID),
	}

	var removed bool
	for _, path := range paths {
		err := os.Remove(path)
		if err == nil {
			removed = true
			continue
		}
		if !os.IsNotExist(err) {
			return err
		}
	}
	if removed {
		return nil
	}
	return os.ErrNotExist
}
