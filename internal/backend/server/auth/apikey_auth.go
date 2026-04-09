package auth

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
)

// APIKeyAdapter implements API key-based authentication
// Users provide their own Anthropic API keys
type APIKeyAdapter struct {
	sessionStore *SessionStore
	adminUsers   map[string]bool
}

// NewAPIKeyAdapter creates a new API key adapter
func NewAPIKeyAdapter(sessionStore *SessionStore) *APIKeyAdapter {
	adminUsers := make(map[string]bool)
	if adminList := os.Getenv("ADMIN_USERS"); adminList != "" {
		for _, email := range strings.Split(adminList, ",") {
			adminUsers[strings.TrimSpace(email)] = true
		}
	}

	return &APIKeyAdapter{
		sessionStore: sessionStore,
		adminUsers:   adminUsers,
	}
}

// SetupRoutes registers API key auth routes
func (a *APIKeyAdapter) SetupRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/auth/login", a.handleLogin)
	mux.HandleFunc("/auth/logout", a.handleLogout)
}

// handleLogin authenticates with API key
func (a *APIKeyAdapter) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Email  string `json:"email"`
		APIKey string `json:"apiKey"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	if req.Email == "" || req.APIKey == "" {
		http.Error(w, "Email and API key required", http.StatusBadRequest)
		return
	}

	// Validate API key format (basic check)
	if !strings.HasPrefix(req.APIKey, "sk-ant-") {
		http.Error(w, "Invalid API key format", http.StatusBadRequest)
		return
	}

	// Create user with their API key
	user := &AuthUser{
		ID:      req.Email,
		Email:   req.Email,
		Name:    req.Email,
		IsAdmin: a.adminUsers[req.Email],
		APIKey:  req.APIKey,
	}

	// Create session
	token, err := a.sessionStore.Create(user)
	if err != nil {
		http.Error(w, "Failed to create session", http.StatusInternalServerError)
		return
	}

	// Set session cookie
	a.sessionStore.SetCookie(w, token, 0)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status": "logged in",
		"email":  req.Email,
	})
}

// handleLogout logs out the user
func (a *APIKeyAdapter) handleLogout(w http.ResponseWriter, r *http.Request) {
	token, err := a.sessionStore.GetCookie(r)
	if err == nil {
		a.sessionStore.Delete(token)
	}

	a.sessionStore.ClearCookie(w)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status": "logged out",
	})
}

// RequireAuth is middleware that ensures the request is authenticated
func (a *APIKeyAdapter) RequireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token, err := a.sessionStore.GetCookie(r)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]string{
				"error": "Unauthorized",
			})
			return
		}

		user, ok := a.sessionStore.Get(token)
		if !ok {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]string{
				"error": "Invalid session",
			})
			return
		}

		r = r.WithContext(setUserContext(r.Context(), user))
		next(w, r)
	}
}

// GetUser extracts the authenticated user from the request
func (a *APIKeyAdapter) GetUser(r *http.Request) (*AuthUser, error) {
	return GetUserFromContext(r.Context())
}
