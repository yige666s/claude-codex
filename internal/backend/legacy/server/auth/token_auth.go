package auth

import (
	"net/http"
	"os"
	"strings"

	"claude-codex/internal/backend/httpjson"
)

// TokenAuthAdapter implements simple token-based authentication
type TokenAuthAdapter struct {
	authToken  string
	adminUsers map[string]bool
}

// NewTokenAuthAdapter creates a new token auth adapter
func NewTokenAuthAdapter() *TokenAuthAdapter {
	authToken := os.Getenv("AUTH_TOKEN")

	adminUsers := make(map[string]bool)
	if adminList := os.Getenv("ADMIN_USERS"); adminList != "" {
		for _, email := range strings.Split(adminList, ",") {
			adminUsers[strings.TrimSpace(email)] = true
		}
	}

	return &TokenAuthAdapter{
		authToken:  authToken,
		adminUsers: adminUsers,
	}
}

// SetupRoutes registers authentication routes (no-op for token auth)
func (a *TokenAuthAdapter) SetupRoutes(mux *http.ServeMux) {
	// Token auth doesn't need any routes
}

// RequireAuth is middleware that ensures the request has a valid token
func (a *TokenAuthAdapter) RequireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// If no auth token is configured, allow all requests
		if a.authToken == "" {
			user := &AuthUser{
				ID:      "default",
				Email:   "default@localhost",
				Name:    "Default User",
				IsAdmin: true,
			}
			r = r.WithContext(setUserContext(r.Context(), user))
			next(w, r)
			return
		}

		// Check Authorization header
		authHeader := r.Header.Get("Authorization")
		if strings.HasPrefix(authHeader, "Bearer ") {
			token := strings.TrimPrefix(authHeader, "Bearer ")
			if token == a.authToken {
				user := &AuthUser{
					ID:      "token-user",
					Email:   "token@localhost",
					Name:    "Token User",
					IsAdmin: a.adminUsers["token@localhost"],
				}
				r = r.WithContext(setUserContext(r.Context(), user))
				next(w, r)
				return
			}
		}

		// Check query parameter
		if token := r.URL.Query().Get("token"); token == a.authToken {
			user := &AuthUser{
				ID:      "token-user",
				Email:   "token@localhost",
				Name:    "Token User",
				IsAdmin: a.adminUsers["token@localhost"],
			}
			r = r.WithContext(setUserContext(r.Context(), user))
			next(w, r)
			return
		}

		httpjson.Write(w, http.StatusUnauthorized, map[string]string{
			"error": "Unauthorized",
		})
	}
}

// GetUser extracts the authenticated user from the request
func (a *TokenAuthAdapter) GetUser(r *http.Request) (*AuthUser, error) {
	return GetUserFromContext(r.Context())
}
