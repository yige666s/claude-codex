package server

import (
	"sync"
	"time"
)

// ScrollbackBuffer maintains a circular buffer of terminal output
type ScrollbackBuffer struct {
	data     []byte
	maxBytes int
	mu       sync.RWMutex
}

// NewScrollbackBuffer creates a new scrollback buffer
func NewScrollbackBuffer(maxBytes int) *ScrollbackBuffer {
	return &ScrollbackBuffer{
		data:     make([]byte, 0, maxBytes),
		maxBytes: maxBytes,
	}
}

// Write appends data to the buffer, truncating from the front if needed
func (b *ScrollbackBuffer) Write(p []byte) (n int, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Append new data
	b.data = append(b.data, p...)

	// Truncate from front if over limit
	if len(b.data) > b.maxBytes {
		excess := len(b.data) - b.maxBytes
		b.data = b.data[excess:]
	}

	return len(p), nil
}

// Read returns all buffered data
func (b *ScrollbackBuffer) Read() []byte {
	b.mu.RLock()
	defer b.mu.RUnlock()

	result := make([]byte, len(b.data))
	copy(result, b.data)
	return result
}

// Clear empties the buffer
func (b *ScrollbackBuffer) Clear() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.data = b.data[:0]
}

// Size returns the current buffer size
func (b *ScrollbackBuffer) Size() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.data)
}

// SessionStoreEntry represents a stored session
type SessionStoreEntry struct {
	Token          string
	UserID         string
	PTY            interface{} // Will be *os.Process or similar
	Scrollback     *ScrollbackBuffer
	CreatedAt      time.Time
	LastActiveAt   time.Time
	GraceTimer     *time.Timer
	InGracePeriod  bool
}

// SessionStore manages session storage with grace period support
type SessionStore struct {
	sessions       map[string]*SessionStoreEntry
	gracePeriodMs  int
	scrollbackSize int
	mu             sync.RWMutex
}

// NewSessionStore creates a new session store
func NewSessionStore(gracePeriodMs, scrollbackSize int) *SessionStore {
	if gracePeriodMs == 0 {
		gracePeriodMs = 5 * 60 * 1000 // 5 minutes default
	}
	if scrollbackSize == 0 {
		scrollbackSize = 100 * 1024 // 100KB default
	}

	return &SessionStore{
		sessions:       make(map[string]*SessionStoreEntry),
		gracePeriodMs:  gracePeriodMs,
		scrollbackSize: scrollbackSize,
	}
}

// Add creates a new session entry
func (s *SessionStore) Add(token, userID string, pty interface{}) *SessionStoreEntry {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry := &SessionStoreEntry{
		Token:        token,
		UserID:       userID,
		PTY:          pty,
		Scrollback:   NewScrollbackBuffer(s.scrollbackSize),
		CreatedAt:    time.Now(),
		LastActiveAt: time.Now(),
	}

	s.sessions[token] = entry
	return entry
}

// Get retrieves a session by token
func (s *SessionStore) Get(token string) *SessionStoreEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.sessions[token]
}

// Delete removes a session
func (s *SessionStore) Delete(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if entry, ok := s.sessions[token]; ok {
		if entry.GraceTimer != nil {
			entry.GraceTimer.Stop()
		}
		delete(s.sessions, token)
	}
}

// List returns all session tokens
func (s *SessionStore) List() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tokens := make([]string, 0, len(s.sessions))
	for token := range s.sessions {
		tokens = append(tokens, token)
	}
	return tokens
}

// GetAll returns all sessions
func (s *SessionStore) GetAll() []*SessionStoreEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entries := make([]*SessionStoreEntry, 0, len(s.sessions))
	for _, entry := range s.sessions {
		entries = append(entries, entry)
	}
	return entries
}

// ListByUser returns sessions for a specific user
func (s *SessionStore) ListByUser(userID string) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tokens := make([]string, 0)
	for token, entry := range s.sessions {
		if entry.UserID == userID {
			tokens = append(tokens, token)
		}
	}
	return tokens
}

// CountByUser returns the number of sessions for a user
func (s *SessionStore) CountByUser(userID string) int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	count := 0
	for _, entry := range s.sessions {
		if entry.UserID == userID {
			count++
		}
	}
	return count
}

// Size returns the total number of sessions
func (s *SessionStore) Size() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.sessions)
}

// StartGrace begins the grace period for a session
func (s *SessionStore) StartGrace(token string, onExpire func()) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry, ok := s.sessions[token]
	if !ok {
		return
	}

	// Cancel existing timer if any
	if entry.GraceTimer != nil {
		entry.GraceTimer.Stop()
	}

	entry.InGracePeriod = true
	entry.GraceTimer = time.AfterFunc(time.Duration(s.gracePeriodMs)*time.Millisecond, func() {
		s.mu.Lock()
		defer s.mu.Unlock()

		if e, exists := s.sessions[token]; exists && e.InGracePeriod {
			delete(s.sessions, token)
			if onExpire != nil {
				onExpire()
			}
		}
	})
}

// CancelGrace cancels the grace period for a session
func (s *SessionStore) CancelGrace(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry, ok := s.sessions[token]
	if !ok {
		return
	}

	if entry.GraceTimer != nil {
		entry.GraceTimer.Stop()
		entry.GraceTimer = nil
	}
	entry.InGracePeriod = false
	entry.LastActiveAt = time.Now()
}

// UpdateActivity updates the last active time for a session
func (s *SessionStore) UpdateActivity(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if entry, ok := s.sessions[token]; ok {
		entry.LastActiveAt = time.Now()
	}
}

// DestroyAll removes all sessions
func (s *SessionStore) DestroyAll() {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, entry := range s.sessions {
		if entry.GraceTimer != nil {
			entry.GraceTimer.Stop()
		}
	}
	s.sessions = make(map[string]*SessionStoreEntry)
}
