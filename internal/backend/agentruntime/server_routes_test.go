package agentruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"claude-codex/internal/harness/state"
)

func TestServerRouteBehaviorSnapshot(t *testing.T) {
	runtime := testRuntime(t)
	server := NewServer(runtime, HeaderAuthenticator{}, NoopRateLimiter{}, nil)
	session, err := runtime.CreateSession(context.Background(), "alice", "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	tests := []struct {
		name         string
		method       string
		path         string
		body         string
		userID       string
		wantStatus   int
		wantContains string
	}{
		{name: "healthz", method: http.MethodGet, path: "/healthz", wantStatus: http.StatusOK, wantContains: `"status":"ok"`},
		{name: "readyz", method: http.MethodGet, path: "/readyz", wantStatus: http.StatusOK, wantContains: `"status":"ok"`},
		{name: "metrics", method: http.MethodGet, path: "/metrics", wantStatus: http.StatusOK, wantContains: "agentapi_requests_total"},
		{name: "app", method: http.MethodGet, path: "/app", wantStatus: http.StatusOK, wantContains: "<!doctype html>"},
		{name: "options before auth", method: http.MethodOptions, path: "/v1/sessions", wantStatus: http.StatusNoContent},
		{name: "public login route", method: http.MethodPost, path: "/v1/auth/login", body: `{"email":"alice@example.com","password":"password123"}`, wantStatus: http.StatusServiceUnavailable, wantContains: "user system is not configured"},
		{name: "public verify email route", method: http.MethodGet, path: "/v1/auth/verify-email?token=verify-token", wantStatus: http.StatusServiceUnavailable, wantContains: "user system is not configured"},
		{name: "authenticated sessions list", method: http.MethodGet, path: "/v1/sessions", userID: "alice", wantStatus: http.StatusOK, wantContains: `[`},
		{name: "authenticated session create", method: http.MethodPost, path: "/v1/sessions", body: `{"working_dir":""}`, userID: "alice", wantStatus: http.StatusCreated, wantContains: `"id":`},
		{name: "authenticated session get", method: http.MethodGet, path: "/v1/sessions/" + session.ID, userID: "alice", wantStatus: http.StatusOK, wantContains: session.ID},
		{name: "websocket route without upgrade", method: http.MethodGet, path: "/v1/sessions/" + session.ID + "/ws", userID: "alice", wantStatus: http.StatusBadRequest},
		{name: "live websocket route without upgrade", method: http.MethodGet, path: "/v1/sessions/" + session.ID + "/live/ws", userID: "alice", wantStatus: http.StatusBadRequest},
		{name: "authenticated jobs list", method: http.MethodGet, path: "/v1/jobs", userID: "alice", wantStatus: http.StatusOK, wantContains: `"jobs":`},
		{name: "missing job get", method: http.MethodGet, path: "/v1/jobs/missing-job", userID: "alice", wantStatus: http.StatusNotFound, wantContains: "job not found"},
		{name: "missing job stream", method: http.MethodGet, path: "/v1/jobs/missing-job/events?stream=1", userID: "alice", wantStatus: http.StatusNotFound, wantContains: "job not found"},
		{name: "authenticated attachments list", method: http.MethodGet, path: "/v1/attachments", userID: "alice", wantStatus: http.StatusOK, wantContains: `"attachments":`},
		{name: "authenticated artifacts list", method: http.MethodGet, path: "/v1/artifacts", userID: "alice", wantStatus: http.StatusOK, wantContains: `"artifacts":`},
		{name: "empty search route", method: http.MethodGet, path: "/v1/search/messages?q=", userID: "alice", wantStatus: http.StatusOK, wantContains: `"items":`},
		{name: "skills route", method: http.MethodGet, path: "/v1/skills", userID: "alice", wantStatus: http.StatusOK, wantContains: `"skills":`},
		{name: "llm status without provider", method: http.MethodGet, path: "/v1/llm/status", userID: "alice", wantStatus: http.StatusServiceUnavailable, wantContains: "llm governance is not configured"},
		{name: "authenticated unknown route", method: http.MethodGet, path: "/v1/not-found", userID: "alice", wantStatus: http.StatusNotFound, wantContains: "not found"},
		{name: "unauthenticated api route", method: http.MethodGet, path: "/v1/sessions", wantStatus: http.StatusUnauthorized, wantContains: "X-User-ID header is required"},
		{name: "unauthenticated unknown route", method: http.MethodGet, path: "/v1/not-found", wantStatus: http.StatusUnauthorized, wantContains: "X-User-ID header is required"},
		{name: "admin users requires admin token", method: http.MethodGet, path: "/v1/admin/users", userID: "alice", wantStatus: http.StatusForbidden, wantContains: "admin API is not configured"},
		{name: "admin ops health requires admin token", method: http.MethodGet, path: "/v1/admin/ops/health", userID: "alice", wantStatus: http.StatusForbidden, wantContains: "admin API is not configured"},
		{name: "admin skills requires admin token", method: http.MethodGet, path: "/v1/admin/skills", userID: "alice", wantStatus: http.StatusForbidden, wantContains: "admin API is not configured"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
			if tt.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			if tt.userID != "" {
				req.Header.Set("X-User-ID", tt.userID)
			}
			rec := httptest.NewRecorder()
			server.ServeHTTP(rec, req)
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d, body=%s", rec.Code, tt.wantStatus, rec.Body.String())
			}
			if tt.wantContains != "" && !bytes.Contains(rec.Body.Bytes(), []byte(tt.wantContains)) {
				t.Fatalf("body missing %q: %s", tt.wantContains, rec.Body.String())
			}
			if rec.Header().Get("X-Request-ID") == "" {
				t.Fatal("missing X-Request-ID header")
			}
		})
	}
}

func TestPublicSessionViewKeepsSafeAttachmentMetadata(t *testing.T) {
	now := time.Now().UTC()
	session := &state.Session{
		ID:         "session-1",
		WorkingDir: "/tmp/private",
		Messages: []state.Message{{
			Role:    state.MessageRoleUser,
			Content: "Please analyze the attached file(s).\n\nAttached files: photo.png",
			Attachments: []state.MessageAttachment{{
				ID:               "asset-1",
				MessageID:        "message-1",
				SessionID:        "session-1",
				UserID:           "alice",
				FileType:         "image",
				MimeType:         "image/png",
				FileName:         "photo.png",
				FileSize:         123,
				StorageKey:       "users/alice/private/photo.png",
				ExtractedTextKey: "private/extracted.txt",
				ThumbnailKey:     "thumbs/photo.png",
			}},
			CreatedAt: now,
		}},
	}

	public := publicSessionView(session)
	if public == nil || len(public.Messages) != 1 {
		t.Fatalf("expected one public message, got %#v", public)
	}
	attachments := public.Messages[0].Attachments
	if len(attachments) != 1 {
		t.Fatalf("expected attachment metadata to survive public view, got %#v", public.Messages[0])
	}
	got := attachments[0]
	if got.ID != "asset-1" || got.FileType != "image" || got.MimeType != "image/png" || got.FileName != "photo.png" || got.FileSize != 123 {
		t.Fatalf("unexpected public attachment metadata: %#v", got)
	}
	if got.StorageKey != "" || got.ExtractedTextKey != "" || got.UserID != "" || got.SessionID != "" || got.MessageID != "" {
		t.Fatalf("expected private attachment fields to be stripped: %#v", got)
	}
}

func TestServerRequestLogIncludesStructuredFields(t *testing.T) {
	var logs bytes.Buffer
	server := NewServer(testRuntime(t), HeaderAuthenticator{}, NoopRateLimiter{}, log.New(&logs, "", 0))

	req := httptest.NewRequest(http.MethodGet, "/v1/sessions", nil)
	req.Header.Set("X-Request-ID", "req-structured")
	req.Header.Set("X-User-ID", "alice")
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}

	var entry map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(logs.Bytes()), &entry); err != nil {
		t.Fatalf("decode log entry: %v\n%s", err, logs.String())
	}
	if entry["request_id"] != "req-structured" || entry["user_id"] != "alice" || entry["route"] != "/v1/sessions" {
		t.Fatalf("missing structured request fields: %#v", entry)
	}
	if entry["status"] != float64(http.StatusOK) {
		t.Fatalf("status field = %#v", entry["status"])
	}
	if _, ok := entry["duration_ms"].(float64); !ok {
		t.Fatalf("duration_ms missing: %#v", entry)
	}
}
