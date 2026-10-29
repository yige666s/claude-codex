package server

import (
	"sync"
)

// UserStore tracks active users and enforces per-user session limits
type UserStore struct {
	users map[string]int // userID -> active session count
	mu    sync.RWMutex
}

// NewUserStore creates a new user store
func NewUserStore() *UserStore {
	return &UserStore{
		users: make(map[string]int),
	}
}

// Acquire increments the session count for a user
// Returns false if the user already has sessions
func (s *UserStore) Acquire(userID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.users[userID]++
	return true
}

// Release decrements the session count for a user
func (s *UserStore) Release(userID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if count, ok := s.users[userID]; ok {
		if count <= 1 {
			delete(s.users, userID)
		} else {
			s.users[userID]--
		}
	}
}

// Count returns the number of active sessions for a user
func (s *UserStore) Count(userID string) int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.users[userID]
}

// TotalUsers returns the total number of users with active sessions
func (s *UserStore) TotalUsers() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return len(s.users)
}

// ListUsers returns all user IDs with active sessions
func (s *UserStore) ListUsers() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	users := make([]string, 0, len(s.users))
	for userID := range s.users {
		users = append(users, userID)
	}
	return users
}
