package auth

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"sync"
	"time"
)

// AuthUser represents an authenticated user
type AuthUser struct {
	ID       string
	Email    string
	Name     string
	IsAdmin  bool
	APIKey   string
}

// AuthAdapter defines the interface for authentication providers
type AuthAdapter interface {
	// SetupRoutes registers authentication routes on the HTTP server
	SetupRoutes(mux *http.ServeMux)

	// RequireAuth is middleware that ensures the request is authenticated
	RequireAuth(next http.HandlerFunc) http.HandlerFunc

	// GetUser extracts the authenticated user from the request
	GetUser(r *http.Request) (*AuthUser, error)
}

// SessionStore manages user sessions with secure tokens
type SessionStore struct {
	sessions map[string]*AuthUser
	secret   string
	mu       sync.RWMutex
}

// NewSessionStore creates a new session store
func NewSessionStore(secret string) *SessionStore {
	if secret == "" {
		secret = generateRandomString(32)
	}

	return &SessionStore{
		sessions: make(map[string]*AuthUser),
		secret:   secret,
	}
}

// Create creates a new session for a user
func (s *SessionStore) Create(user *AuthUser) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	token := generateRandomString(32)
	s.sessions[token] = user

	return token, nil
}

// Get retrieves a user by session token
func (s *SessionStore) Get(token string) (*AuthUser, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	user, ok := s.sessions[token]
	return user, ok
}

// Delete removes a session
func (s *SessionStore) Delete(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.sessions, token)
}

// SetCookie sets a session cookie on the response
func (s *SessionStore) SetCookie(w http.ResponseWriter, token string, maxAge int) {
	if maxAge == 0 {
		maxAge = 86400 * 7 // 7 days default
	}

	cookie := &http.Cookie{
		Name:     "session",
		Value:    token,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	}

	http.SetCookie(w, cookie)
}

// GetCookie retrieves the session token from the request
func (s *SessionStore) GetCookie(r *http.Request) (string, error) {
	cookie, err := r.Cookie("session")
	if err != nil {
		return "", err
	}
	return cookie.Value, nil
}

// ClearCookie removes the session cookie
func (s *SessionStore) ClearCookie(w http.ResponseWriter) {
	cookie := &http.Cookie{
		Name:     "session",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	}

	http.SetCookie(w, cookie)
}

// generateRandomString generates a cryptographically secure random string
func generateRandomString(length int) string {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		// Fallback to timestamp-based token if crypto/rand fails
		return base64.URLEncoding.EncodeToString([]byte(time.Now().String()))
	}
	return base64.URLEncoding.EncodeToString(bytes)[:length]
}
