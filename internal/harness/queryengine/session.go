// Package engine provides session state management for the QueryEngine.
package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

// SessionState manages the mutable state for a conversation session.
// It tracks messages, usage, permissions, and other session-specific data.
type SessionState struct {
	// Messages in the conversation
	messages []Message

	// Accumulated usage across all turns
	totalUsage *Usage

	// Permission denials that occurred
	permissionDenials []PermissionDenial

	// File state cache for tracking read files
	readFileState interface{}

	// Discovered skill names (turn-scoped)
	discoveredSkillNames map[string]bool

	// Loaded nested memory paths (turn-scoped)
	loadedNestedMemoryPaths map[string]bool

	// Whether orphaned permission has been handled
	hasHandledOrphanedPermission bool

	// Session metadata
	sessionID  string
	startedAt  time.Time
	lastActive time.Time

	mu sync.RWMutex
}

// NewSessionState creates a new session state.
func NewSessionState(initialMessages []Message, readFileCache interface{}) *SessionState {
	if initialMessages == nil {
		initialMessages = []Message{}
	}

	return &SessionState{
		messages:                make([]Message, len(initialMessages)),
		totalUsage:              EmptyUsage(),
		permissionDenials:       []PermissionDenial{},
		readFileState:           readFileCache,
		discoveredSkillNames:    make(map[string]bool),
		loadedNestedMemoryPaths: make(map[string]bool),
		sessionID:               uuid.New().String(),
		startedAt:               time.Now(),
		lastActive:              time.Now(),
	}
}

// AddMessage appends a message to the conversation history.
func (s *SessionState) AddMessage(msg Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages = append(s.messages, msg)
	s.lastActive = time.Now()
}

// GetMessages returns a copy of all messages.
func (s *SessionState) GetMessages() []Message {
	s.mu.RLock()
	defer s.mu.RUnlock()
	messages := make([]Message, len(s.messages))
	copy(messages, s.messages)
	return messages
}

// GetMessageCount returns the number of messages.
func (s *SessionState) GetMessageCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.messages)
}

// GetLastMessage returns the last message, or nil if no messages exist.
func (s *SessionState) GetLastMessage() *Message {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.messages) == 0 {
		return nil
	}
	msg := s.messages[len(s.messages)-1]
	return &msg
}

// UpdateUsage accumulates usage statistics.
func (s *SessionState) UpdateUsage(usage *Usage) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.totalUsage = AccumulateUsage(s.totalUsage, usage)
	s.lastActive = time.Now()
}

// GetTotalUsage returns the accumulated usage.
func (s *SessionState) GetTotalUsage() *Usage {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.totalUsage
}

// AddPermissionDenial records a permission denial.
func (s *SessionState) AddPermissionDenial(denial PermissionDenial) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.permissionDenials = append(s.permissionDenials, denial)
	s.lastActive = time.Now()
}

// GetPermissionDenials returns all permission denials.
func (s *SessionState) GetPermissionDenials() []PermissionDenial {
	s.mu.RLock()
	defer s.mu.RUnlock()
	denials := make([]PermissionDenial, len(s.permissionDenials))
	copy(denials, s.permissionDenials)
	return denials
}

// SetReadFileState updates the file state cache.
func (s *SessionState) SetReadFileState(state interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.readFileState = state
	s.lastActive = time.Now()
}

// GetReadFileState returns the file state cache.
func (s *SessionState) GetReadFileState() interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.readFileState
}

// MarkSkillDiscovered marks a skill as discovered in this turn.
func (s *SessionState) MarkSkillDiscovered(skillName string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.discoveredSkillNames[skillName] = true
}

// IsSkillDiscovered checks if a skill was discovered in this turn.
func (s *SessionState) IsSkillDiscovered(skillName string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.discoveredSkillNames[skillName]
}

// ClearDiscoveredSkills clears the discovered skills for a new turn.
func (s *SessionState) ClearDiscoveredSkills() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.discoveredSkillNames = make(map[string]bool)
}

// MarkNestedMemoryPathLoaded marks a nested memory path as loaded.
func (s *SessionState) MarkNestedMemoryPathLoaded(path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadedNestedMemoryPaths[path] = true
}

// IsNestedMemoryPathLoaded checks if a nested memory path was loaded.
func (s *SessionState) IsNestedMemoryPathLoaded(path string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.loadedNestedMemoryPaths[path]
}

// SetOrphanedPermissionHandled marks the orphaned permission as handled.
func (s *SessionState) SetOrphanedPermissionHandled(handled bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hasHandledOrphanedPermission = handled
}

// HasHandledOrphanedPermission returns whether orphaned permission was handled.
func (s *SessionState) HasHandledOrphanedPermission() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.hasHandledOrphanedPermission
}

// GetSessionID returns the session ID.
func (s *SessionState) GetSessionID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.sessionID
}

// GetStartedAt returns when the session started.
func (s *SessionState) GetStartedAt() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.startedAt
}

// GetLastActive returns when the session was last active.
func (s *SessionState) GetLastActive() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastActive
}

// Snapshot returns a serializable snapshot of the session state.
func (s *SessionState) Snapshot() *SessionSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	messages := make([]Message, len(s.messages))
	copy(messages, s.messages)

	denials := make([]PermissionDenial, len(s.permissionDenials))
	copy(denials, s.permissionDenials)

	return &SessionSnapshot{
		SessionID:         s.sessionID,
		Messages:          messages,
		TotalUsage:        s.totalUsage,
		PermissionDenials: denials,
		StartedAt:         s.startedAt,
		LastActive:        s.lastActive,
	}
}

// SessionSnapshot is a serializable snapshot of session state.
type SessionSnapshot struct {
	SessionID         string             `json:"session_id"`
	Messages          []Message          `json:"messages"`
	TotalUsage        *Usage             `json:"total_usage"`
	PermissionDenials []PermissionDenial `json:"permission_denials"`
	StartedAt         time.Time          `json:"started_at"`
	LastActive        time.Time          `json:"last_active"`
}

// ToJSON serializes the snapshot to JSON.
func (s *SessionSnapshot) ToJSON() ([]byte, error) {
	return json.MarshalIndent(s, "", "  ")
}

// FromJSON deserializes a snapshot from JSON.
func FromJSON(data []byte) (*SessionSnapshot, error) {
	var snapshot SessionSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return nil, fmt.Errorf("failed to unmarshal session snapshot: %w", err)
	}
	return &snapshot, nil
}

// RestoreFromSnapshot creates a new SessionState from a snapshot.
func RestoreFromSnapshot(snapshot *SessionSnapshot, readFileCache interface{}) *SessionState {
	state := &SessionState{
		messages:                make([]Message, len(snapshot.Messages)),
		totalUsage:              snapshot.TotalUsage,
		permissionDenials:       make([]PermissionDenial, len(snapshot.PermissionDenials)),
		readFileState:           readFileCache,
		discoveredSkillNames:    make(map[string]bool),
		loadedNestedMemoryPaths: make(map[string]bool),
		sessionID:               snapshot.SessionID,
		startedAt:               snapshot.StartedAt,
		lastActive:              snapshot.LastActive,
	}

	copy(state.messages, snapshot.Messages)
	copy(state.permissionDenials, snapshot.PermissionDenials)

	return state
}

// MessageFilter is a function that filters messages.
type MessageFilter func(Message) bool

// FilterMessages returns messages that match the filter.
func (s *SessionState) FilterMessages(filter MessageFilter) []Message {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var filtered []Message
	for _, msg := range s.messages {
		if filter(msg) {
			filtered = append(filtered, msg)
		}
	}
	return filtered
}

// GetUserMessages returns only user messages.
func (s *SessionState) GetUserMessages() []Message {
	return s.FilterMessages(func(m Message) bool {
		return m.Type == "user"
	})
}

// GetAssistantMessages returns only assistant messages.
func (s *SessionState) GetAssistantMessages() []Message {
	return s.FilterMessages(func(m Message) bool {
		return m.Type == "assistant"
	})
}

// GetTurnCount returns the number of user turns (user messages).
func (s *SessionState) GetTurnCount() int {
	return len(s.GetUserMessages())
}

// Clear resets the session state while preserving the session ID.
func (s *SessionState) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.messages = []Message{}
	s.totalUsage = EmptyUsage()
	s.permissionDenials = []PermissionDenial{}
	s.discoveredSkillNames = make(map[string]bool)
	s.loadedNestedMemoryPaths = make(map[string]bool)
	s.hasHandledOrphanedPermission = false
	s.lastActive = time.Now()
}

// SessionManager manages multiple session states.
type SessionManager struct {
	sessions map[string]*SessionState
	mu       sync.RWMutex
}

// NewSessionManager creates a new session manager.
func NewSessionManager() *SessionManager {
	return &SessionManager{
		sessions: make(map[string]*SessionState),
	}
}

// CreateSession creates a new session.
func (sm *SessionManager) CreateSession(initialMessages []Message, readFileCache interface{}) *SessionState {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	state := NewSessionState(initialMessages, readFileCache)
	sm.sessions[state.sessionID] = state
	return state
}

// GetSession retrieves a session by ID.
func (sm *SessionManager) GetSession(sessionID string) (*SessionState, error) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	state, exists := sm.sessions[sessionID]
	if !exists {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}
	return state, nil
}

// DeleteSession removes a session.
func (sm *SessionManager) DeleteSession(sessionID string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if _, exists := sm.sessions[sessionID]; !exists {
		return fmt.Errorf("session not found: %s", sessionID)
	}
	delete(sm.sessions, sessionID)
	return nil
}

// ListSessions returns all session IDs.
func (sm *SessionManager) ListSessions() []string {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	ids := make([]string, 0, len(sm.sessions))
	for id := range sm.sessions {
		ids = append(ids, id)
	}
	return ids
}

// CleanupInactiveSessions removes sessions inactive for longer than the duration.
func (sm *SessionManager) CleanupInactiveSessions(ctx context.Context, inactiveDuration time.Duration) int {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	now := time.Now()
	removed := 0

	for id, state := range sm.sessions {
		if now.Sub(state.GetLastActive()) > inactiveDuration {
			delete(sm.sessions, id)
			removed++
		}
	}

	return removed
}
