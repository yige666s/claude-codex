package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// DefaultExtractionWaitTimeout is how long WaitForExtraction blocks at most.
// Mirrors EXTRACTION_WAIT_TIMEOUT_MS = 15 000 ms in sessionMemoryUtils.ts.
const DefaultExtractionWaitTimeout = 15 * time.Second

// DefaultExtractionStaleThreshold marks an extraction as stale after this
// duration.  Mirrors EXTRACTION_STALE_THRESHOLD_MS = 60 000 ms.
const DefaultExtractionStaleThreshold = 60 * time.Second

// SessionMemory manages the per-session running notes file.
// It mirrors the TypeScript SessionMemory service in
// src/services/SessionMemory/sessionMemory.ts.
//
// A session memory file lives at:
//   ~/.claude/session-memory/<sessionID>.md
//
// It is updated by a background subagent whenever token/tool-call thresholds
// are exceeded — keeping a structured summary without blocking the main turn.
type SessionMemory struct {
	sessionID string
	dir       string // base directory for session-memory files

	mu                       sync.RWMutex
	initialized              bool
	tokensAtLastExtraction   int
	toolCallsSinceExtraction int
	lastSummarizedMessageID  string
	extractionStartedAt      time.Time
	extractionInProgress     bool

	config SessionMemoryConfig // defined in types.go
}

// NewSessionMemory creates a SessionMemory handler for the given session ID.
// dir is the parent directory for session-memory files (typically
// ~/.claude/session-memory/).
func NewSessionMemory(sessionID, dir string, cfg SessionMemoryConfig) *SessionMemory {
	return &SessionMemory{
		sessionID: sessionID,
		dir:       dir,
		config:    cfg,
	}
}

// Path returns the absolute path to this session's memory file.
func (s *SessionMemory) Path() string {
	return filepath.Join(s.dir, s.sessionID+".md")
}

// ShouldExtract returns true when the token/tool-call thresholds have been met
// and no extraction is currently in progress.
func (s *SessionMemory) ShouldExtract(currentTokenCount, toolCallCount int) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.extractionInProgress {
		return false
	}

	// First extraction: wait for initialization threshold.
	if !s.initialized {
		return currentTokenCount >= s.config.InitializationThreshold
	}

	tokenGrowth := currentTokenCount - s.tokensAtLastExtraction
	return tokenGrowth >= s.config.MinimumTokensBetweenUpdate &&
		toolCallCount >= s.config.ToolCallsBetweenUpdates
}

// MarkExtractionStarted records that a background extraction has begun.
func (s *SessionMemory) MarkExtractionStarted() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.extractionInProgress = true
	s.extractionStartedAt = time.Now()
}

// MarkExtractionCompleted updates state after a successful extraction.
func (s *SessionMemory) MarkExtractionCompleted(currentTokenCount int, lastMessageID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.extractionInProgress = false
	s.extractionStartedAt = time.Time{}
	s.initialized = true
	s.tokensAtLastExtraction = currentTokenCount
	s.lastSummarizedMessageID = lastMessageID
	s.toolCallsSinceExtraction = 0
}

// ShouldExtractFull mirrors the full TypeScript shouldExtractMemory logic,
// including the "natural break" check: if the last assistant turn had no tool
// calls, extraction fires on token threshold alone (no tool-call threshold
// required).
//
//	hasToolCallsInLastTurn – caller must pass whether the last assistant turn
//	                         contained any tool_use blocks.
func (s *SessionMemory) ShouldExtractFull(currentTokenCount, toolCallCount int, hasToolCallsInLastTurn bool) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.extractionInProgress {
		return false
	}

	// First extraction: wait for initialization threshold.
	if !s.initialized {
		return currentTokenCount >= s.config.InitializationThreshold
	}

	tokenGrowth := currentTokenCount - s.tokensAtLastExtraction
	hasMetTokenThreshold := tokenGrowth >= s.config.MinimumTokensBetweenUpdate
	hasMetToolCallThreshold := toolCallCount >= s.config.ToolCallsBetweenUpdates

	// Token threshold is always required.
	if !hasMetTokenThreshold {
		return false
	}
	// Extract when both thresholds met, OR at a natural conversation break
	// (last turn had no tool calls).
	return hasMetToolCallThreshold || !hasToolCallsInLastTurn
}

// IncrementToolCalls increments the tool-call counter used by ShouldExtract.
func (s *SessionMemory) IncrementToolCalls(delta int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.toolCallsSinceExtraction += delta
}

// ResetLastMessageUUID clears the last-summarized message ID (used at session
// start or after compaction).
func (s *SessionMemory) ResetLastMessageUUID() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastSummarizedMessageID = ""
}

// GetLastSummarizedMessageID returns the message ID up to which the session
// memory is current.
func (s *SessionMemory) GetLastSummarizedMessageID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastSummarizedMessageID
}

// IsExtractionInProgress returns true while a background extraction is running.
func (s *SessionMemory) IsExtractionInProgress() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.extractionInProgress
}

// WaitForExtraction blocks until the in-progress extraction finishes or until
// timeout / staleness thresholds are exceeded.
func (s *SessionMemory) WaitForExtraction(timeout, staleThreshold time.Duration) {
	deadline := time.Now().Add(timeout)
	for {
		s.mu.RLock()
		inProgress := s.extractionInProgress
		startedAt := s.extractionStartedAt
		s.mu.RUnlock()

		if !inProgress {
			return
		}

		// Don't wait for stale extractions.
		if !startedAt.IsZero() && time.Since(startedAt) > staleThreshold {
			return
		}

		if time.Now().After(deadline) {
			return
		}

		time.Sleep(100 * time.Millisecond)
	}
}

// LoadContent reads and returns the raw contents of the session memory file.
// Returns ("", nil) when the file does not yet exist.
func (s *SessionMemory) LoadContent() (string, error) {
	data, err := os.ReadFile(s.Path())
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("read session memory: %w", err)
	}
	return string(data), nil
}

// IsEmpty returns true when the session memory file is missing or contains
// only the default template with no real content.
func (s *SessionMemory) IsEmpty() bool {
	content, err := s.LoadContent()
	if err != nil || content == "" {
		return true
	}
	// A file is considered empty when every non-header line is either blank
	// or an italic placeholder (the section description lines).
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Italic placeholder: starts and ends with _
		if strings.HasPrefix(line, "_") && strings.HasSuffix(line, "_") {
			continue
		}
		return false
	}
	return true
}

// EnsureDir creates the session-memory directory if it does not exist.
func (s *SessionMemory) EnsureDir() error {
	return os.MkdirAll(s.dir, 0o755)
}

// Initialize writes the default template to the session memory file when it
// does not already exist.
func (s *SessionMemory) Initialize() error {
	if err := s.EnsureDir(); err != nil {
		return err
	}
	path := s.Path()
	if _, err := os.Stat(path); err == nil {
		return nil // already exists
	}
	return os.WriteFile(path, []byte(DefaultSessionMemoryTemplate), 0o644)
}

// TruncateForCompact returns a shortened version of the session memory content
// suitable for inclusion in the compact summary prompt.
// Mirrors truncateSessionMemoryForCompact in prompts.ts.
func (s *SessionMemory) TruncateForCompact(maxTokens int) (string, error) {
	content, err := s.LoadContent()
	if err != nil {
		return "", err
	}
	if content == "" {
		return "", nil
	}
	// Rough 4-chars-per-token estimate.
	maxChars := maxTokens * 4
	if len(content) <= maxChars {
		return content, nil
	}
	// Truncate at the last newline before the limit.
	cut := strings.LastIndex(content[:maxChars], "\n")
	if cut <= 0 {
		cut = maxChars
	}
	return content[:cut] + "\n\n[... truncated for compact ...]", nil
}
