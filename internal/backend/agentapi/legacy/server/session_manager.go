package server

import (
	"fmt"
	"sync"
	"time"
)

// UserHourlyRateLimiter tracks new-session creations per user within a rolling 1-hour window
type UserHourlyRateLimiter struct {
	attempts   map[string][]int64
	maxPerHour int
	mu         sync.RWMutex
}

// NewUserHourlyRateLimiter creates a new rate limiter
func NewUserHourlyRateLimiter(maxPerHour int) *UserHourlyRateLimiter {
	limiter := &UserHourlyRateLimiter{
		attempts:   make(map[string][]int64),
		maxPerHour: maxPerHour,
	}

	// Start cleanup goroutine
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			limiter.cleanup()
		}
	}()

	return limiter
}

// Allow checks if a user can create a new session (non-destructive peek)
func (l *UserHourlyRateLimiter) Allow(userID string) bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return len(l.recent(userID)) < l.maxPerHour
}

// Record commits a session creation attempt
func (l *UserHourlyRateLimiter) Record(userID string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	recent := l.recent(userID)
	recent = append(recent, time.Now().UnixMilli())
	l.attempts[userID] = recent
}

// RetryAfterSeconds returns seconds until the oldest attempt falls off
func (l *UserHourlyRateLimiter) RetryAfterSeconds(userID string) int {
	l.mu.RLock()
	defer l.mu.RUnlock()

	recent := l.recent(userID)
	if len(recent) == 0 {
		return 0
	}

	oldest := recent[0]
	for _, t := range recent {
		if t < oldest {
			oldest = t
		}
	}

	expiresAt := oldest + 3600000 // 1 hour in milliseconds
	now := time.Now().UnixMilli()
	if expiresAt <= now {
		return 0
	}

	return int((expiresAt - now + 999) / 1000) // Round up
}

// recent returns attempts within the last hour for a user
func (l *UserHourlyRateLimiter) recent(userID string) []int64 {
	cutoff := time.Now().UnixMilli() - 3600000 // 1 hour ago
	attempts := l.attempts[userID]

	filtered := make([]int64, 0, len(attempts))
	for _, t := range attempts {
		if t > cutoff {
			filtered = append(filtered, t)
		}
	}

	return filtered
}

// cleanup removes stale entries
func (l *UserHourlyRateLimiter) cleanup() {
	l.mu.Lock()
	defer l.mu.Unlock()

	cutoff := time.Now().UnixMilli() - 3600000
	for userID, attempts := range l.attempts {
		filtered := make([]int64, 0, len(attempts))
		for _, t := range attempts {
			if t > cutoff {
				filtered = append(filtered, t)
			}
		}

		if len(filtered) == 0 {
			delete(l.attempts, userID)
		} else {
			l.attempts[userID] = filtered
		}
	}
}

// PTYSpawner is a function that spawns a PTY process
type PTYSpawner func(cols, rows int, userID string) (interface{}, error)

// SessionManager manages PTY sessions with lifecycle and rate limiting
type SessionManager struct {
	store              *SessionStore
	maxSessions        int
	maxSessionsPerUser int
	spawnPty           PTYSpawner
	rateLimiter        *UserHourlyRateLimiter
	wiredPtys          map[string]bool
	mu                 sync.RWMutex
}

// NewSessionManager creates a new session manager
func NewSessionManager(
	maxSessions int,
	spawnPty PTYSpawner,
	gracePeriodMs int,
	scrollbackBytes int,
	maxSessionsPerUser int,
	maxSessionsPerHour int,
) *SessionManager {
	if maxSessionsPerUser == 0 {
		maxSessionsPerUser = maxSessions
	}
	if maxSessionsPerHour == 0 {
		maxSessionsPerHour = 100
	}

	return &SessionManager{
		store:              NewSessionStore(gracePeriodMs, scrollbackBytes),
		maxSessions:        maxSessions,
		maxSessionsPerUser: maxSessionsPerUser,
		spawnPty:           spawnPty,
		rateLimiter:        NewUserHourlyRateLimiter(maxSessionsPerHour),
		wiredPtys:          make(map[string]bool),
	}
}

// ActiveCount returns the number of active sessions
func (m *SessionManager) ActiveCount() int {
	return m.store.Size()
}

// IsFull returns whether the server is at capacity
func (m *SessionManager) IsFull() bool {
	return m.store.Size() >= m.maxSessions
}

// GetSession retrieves a session by token
func (m *SessionManager) GetSession(token string) *SessionStoreEntry {
	return m.store.Get(token)
}

// ListSessions returns all session tokens
func (m *SessionManager) ListSessions() []string {
	return m.store.List()
}

// GetAllSessions returns all sessions in admin dashboard format
func (m *SessionManager) GetAllSessions() []map[string]interface{} {
	entries := m.store.GetAll()
	result := make([]map[string]interface{}, len(entries))

	for i, entry := range entries {
		result[i] = map[string]interface{}{
			"id":        entry.Token,
			"userId":    entry.UserID,
			"createdAt": entry.CreatedAt.UnixMilli(),
		}
	}

	return result
}

// GetUserSessions returns sessions for a specific user
func (m *SessionManager) GetUserSessions(userID string) []string {
	return m.store.ListByUser(userID)
}

// IsUserAtConcurrentLimit checks if user has reached concurrent session limit
func (m *SessionManager) IsUserAtConcurrentLimit(userID string) bool {
	return m.store.CountByUser(userID) >= m.maxSessionsPerUser
}

// IsUserRateLimited checks if user is rate limited
func (m *SessionManager) IsUserRateLimited(userID string) bool {
	return !m.rateLimiter.Allow(userID)
}

// RetryAfterSeconds returns retry-after seconds for a rate-limited user
func (m *SessionManager) RetryAfterSeconds(userID string) int {
	return m.rateLimiter.RetryAfterSeconds(userID)
}

// Create spawns a new PTY session
func (m *SessionManager) Create(token string, cols, rows int, userID string) error {
	if m.IsFull() {
		return fmt.Errorf("server at capacity")
	}

	if m.IsUserAtConcurrentLimit(userID) {
		return fmt.Errorf("user session limit reached (max %d)", m.maxSessionsPerUser)
	}

	if m.IsUserRateLimited(userID) {
		return fmt.Errorf("rate limited")
	}

	pty, err := m.spawnPty(cols, rows, userID)
	if err != nil {
		return fmt.Errorf("PTY spawn failed: %w", err)
	}

	m.store.Add(token, userID, pty)
	m.rateLimiter.Record(userID)

	return nil
}

// Reconnect reconnects to an existing session
func (m *SessionManager) Reconnect(token string) (*SessionStoreEntry, error) {
	entry := m.store.Get(token)
	if entry == nil {
		return nil, fmt.Errorf("session not found")
	}

	// Cancel grace period if active
	if entry.InGracePeriod {
		m.store.CancelGrace(token)
	}

	m.store.UpdateActivity(token)
	return entry, nil
}

// StartGracePeriod begins the grace period for a session
func (m *SessionManager) StartGracePeriod(token string, onExpire func()) {
	m.store.StartGrace(token, onExpire)
}

// DestroySession force-kills a session immediately
func (m *SessionManager) DestroySession(token string) {
	m.store.Delete(token)
}

// DestroyAll removes all sessions
func (m *SessionManager) DestroyAll() {
	m.store.DestroyAll()
}

// MarkPtyWired marks a PTY as having event listeners wired
func (m *SessionManager) MarkPtyWired(token string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.wiredPtys[token] = true
}

// IsPtyWired checks if a PTY has been wired
func (m *SessionManager) IsPtyWired(token string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.wiredPtys[token]
}
