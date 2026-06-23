package agentruntime

import (
	"encoding/json"
	"net/http"
	"strings"
)

func (s *Server) handleBrowserPushConfig(w http.ResponseWriter, _ *http.Request, _ User) {
	if s.runtime == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "runtime is not configured"})
		return
	}
	writeJSON(w, http.StatusOK, s.runtime.BrowserPushPublicConfig())
}

func (s *Server) handleUpsertBrowserPushSubscription(w http.ResponseWriter, r *http.Request, user User) {
	if s.runtime == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "runtime is not configured"})
		return
	}
	var input BrowserPushSubscriptionInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if strings.TrimSpace(input.UserAgent) == "" {
		input.UserAgent = strings.TrimSpace(r.UserAgent())
	}
	sub, err := s.runtime.UpsertBrowserPushSubscription(r.Context(), user.ID, input)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"subscription": browserPushSubscriptionResponse(sub)})
}

func (s *Server) handleDeleteBrowserPushSubscription(w http.ResponseWriter, r *http.Request, user User, subscriptionID string) {
	if s.runtime == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "runtime is not configured"})
		return
	}
	if err := s.runtime.DeleteBrowserPushSubscription(r.Context(), user.ID, subscriptionID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) handleTestBrowserPush(w http.ResponseWriter, r *http.Request, user User) {
	if s.runtime == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "runtime is not configured"})
		return
	}
	if err := s.runtime.SendTestBrowserPush(r.Context(), user.ID); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sent": true})
}
