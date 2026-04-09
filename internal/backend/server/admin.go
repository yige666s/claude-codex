package server

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/ding/claude-code/claude-go/internal/backend/server/auth"
)

// AdminHandler provides admin endpoints for managing sessions
type AdminHandler struct {
	sessionManager *SessionManager
	userStore      *UserStore
}

// NewAdminHandler creates a new admin handler
func NewAdminHandler(sessionManager *SessionManager, userStore *UserStore) *AdminHandler {
	return &AdminHandler{
		sessionManager: sessionManager,
		userStore:      userStore,
	}
}

// SetupRoutes registers admin routes
func (h *AdminHandler) SetupRoutes(mux *http.ServeMux, authAdapter auth.AuthAdapter) {
	mux.HandleFunc("/admin/sessions", authAdapter.RequireAuth(h.handleListSessions))
	mux.HandleFunc("/admin/users", authAdapter.RequireAuth(h.handleListUsers))
	mux.HandleFunc("/admin/stats", authAdapter.RequireAuth(h.handleStats))
}

// handleListSessions returns all active sessions (admin only)
func (h *AdminHandler) handleListSessions(w http.ResponseWriter, r *http.Request) {
	user, err := getUserFromContext(r.Context())
	if err != nil || !user.IsAdmin {
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Admin access required",
		})
		return
	}

	sessions := h.sessionManager.GetAllSessions()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(sessions)
}

// handleListUsers returns all active users (admin only)
func (h *AdminHandler) handleListUsers(w http.ResponseWriter, r *http.Request) {
	user, err := getUserFromContext(r.Context())
	if err != nil || !user.IsAdmin {
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Admin access required",
		})
		return
	}

	users := h.userStore.ListUsers()
	userStats := make([]map[string]interface{}, len(users))

	for i, userID := range users {
		userStats[i] = map[string]interface{}{
			"userId":        userID,
			"sessionCount":  h.userStore.Count(userID),
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"users":      userStats,
		"totalUsers": h.userStore.TotalUsers(),
	})
}

// handleStats returns server statistics (admin only)
func (h *AdminHandler) handleStats(w http.ResponseWriter, r *http.Request) {
	user, err := getUserFromContext(r.Context())
	if err != nil || !user.IsAdmin {
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Admin access required",
		})
		return
	}

	stats := map[string]interface{}{
		"activeSessions": h.sessionManager.ActiveCount(),
		"activeUsers":    h.userStore.TotalUsers(),
		"maxSessions":    h.sessionManager.maxSessions,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

// GetUserFromContext is a helper to extract user from context
func getUserFromContext(ctx context.Context) (*auth.AuthUser, error) {
	return auth.GetUserFromContext(ctx)
}
