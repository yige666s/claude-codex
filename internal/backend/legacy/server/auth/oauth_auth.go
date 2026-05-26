package auth

import (
	"fmt"
	"net/http"
	"os"
	"strings"

	"claude-codex/internal/backend/httpjson"
)

// OAuthAdapter implements OAuth-based authentication
type OAuthAdapter struct {
	sessionStore *SessionStore
	clientID     string
	clientSecret string
	redirectURI  string
	adminUsers   map[string]bool
}

// NewOAuthAdapter creates a new OAuth adapter
func NewOAuthAdapter(sessionStore *SessionStore) *OAuthAdapter {
	adminUsers := make(map[string]bool)
	if adminList := os.Getenv("ADMIN_USERS"); adminList != "" {
		for _, email := range strings.Split(adminList, ",") {
			adminUsers[strings.TrimSpace(email)] = true
		}
	}

	return &OAuthAdapter{
		sessionStore: sessionStore,
		clientID:     os.Getenv("OAUTH_CLIENT_ID"),
		clientSecret: os.Getenv("OAUTH_CLIENT_SECRET"),
		redirectURI:  os.Getenv("OAUTH_REDIRECT_URI"),
		adminUsers:   adminUsers,
	}
}

// SetupRoutes registers OAuth routes
func (a *OAuthAdapter) SetupRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/auth/login", a.handleLogin)
	mux.HandleFunc("/auth/callback", a.handleCallback)
	mux.HandleFunc("/auth/logout", a.handleLogout)
}

// handleLogin redirects to OAuth provider
func (a *OAuthAdapter) handleLogin(w http.ResponseWriter, r *http.Request) {
	if a.clientID == "" {
		httpjson.Write(w, http.StatusInternalServerError, map[string]string{"error": "OAuth not configured"})
		return
	}

	// Build OAuth authorization URL
	authURL := fmt.Sprintf(
		"https://oauth.provider.com/authorize?client_id=%s&redirect_uri=%s&response_type=code&scope=email profile",
		a.clientID,
		a.redirectURI,
	)

	http.Redirect(w, r, authURL, http.StatusTemporaryRedirect)
}

// handleCallback processes OAuth callback
func (a *OAuthAdapter) handleCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	if code == "" {
		httpjson.Write(w, http.StatusBadRequest, map[string]string{"error": "Missing authorization code"})
		return
	}

	// Exchange code for token (simplified - real implementation would call OAuth provider)
	// For now, create a mock user
	user := &AuthUser{
		ID:      "oauth-user-" + code[:8],
		Email:   "user@example.com",
		Name:    "OAuth User",
		IsAdmin: a.adminUsers["user@example.com"],
	}

	// Create session
	token, err := a.sessionStore.Create(user)
	if err != nil {
		httpjson.Write(w, http.StatusInternalServerError, map[string]string{"error": "Failed to create session"})
		return
	}

	// Set session cookie
	a.sessionStore.SetCookie(w, token, 0)

	// Redirect to home
	http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
}

// handleLogout logs out the user
func (a *OAuthAdapter) handleLogout(w http.ResponseWriter, r *http.Request) {
	token, err := a.sessionStore.GetCookie(r)
	if err == nil {
		a.sessionStore.Delete(token)
	}

	a.sessionStore.ClearCookie(w)

	httpjson.Write(w, http.StatusOK, map[string]string{
		"status": "logged out",
	})
}

// RequireAuth is middleware that ensures the request is authenticated
func (a *OAuthAdapter) RequireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token, err := a.sessionStore.GetCookie(r)
		if err != nil {
			httpjson.Write(w, http.StatusUnauthorized, map[string]string{
				"error": "Unauthorized",
			})
			return
		}

		user, ok := a.sessionStore.Get(token)
		if !ok {
			httpjson.Write(w, http.StatusUnauthorized, map[string]string{
				"error": "Invalid session",
			})
			return
		}

		r = r.WithContext(setUserContext(r.Context(), user))
		next(w, r)
	}
}

// GetUser extracts the authenticated user from the request
func (a *OAuthAdapter) GetUser(r *http.Request) (*AuthUser, error) {
	return GetUserFromContext(r.Context())
}
