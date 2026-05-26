package agentruntime

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"claude-codex/internal/backend/httpjson"
)

func TestSanitizeAPIErrorMessageHidesCredentialInternals(t *testing.T) {
	raw := "live vertex access token is required: read GOOGLE_APPLICATION_CREDENTIALS: open /run/agentapi/secrets/vertex-service-account.json: no such file or directory"
	got := sanitizeAPIErrorMessage(raw)
	if got == raw || got == "" {
		t.Fatalf("sanitizeAPIErrorMessage() = %q", got)
	}
	if containsAny(got, []string{"GOOGLE_APPLICATION_CREDENTIALS", "/run/agentapi", "vertex-service-account"}) {
		t.Fatalf("sanitized message leaked internals: %q", got)
	}
}

func TestSanitizeAPIErrorMessageHidesSandboxInternals(t *testing.T) {
	got := sanitizeAPIErrorMessage("docker: Error response from daemon: OCI runtime create failed: operation not permitted")
	if got != "The tool sandbox could not start. Ask an administrator to check the sandbox configuration." {
		t.Fatalf("sanitizeAPIErrorMessage() = %q", got)
	}
}

func TestSanitizeAPIErrorMessageKeepsValidationText(t *testing.T) {
	const want = "email is required"
	if got := sanitizeAPIErrorMessage(want); got != want {
		t.Fatalf("sanitizeAPIErrorMessage() = %q, want %q", got, want)
	}
}

func TestWriteJSONErrorUsesHTTPJSONErrorStatusAndCode(t *testing.T) {
	rec := httptest.NewRecorder()
	rec.Header().Set("X-Request-ID", "req-1")

	writeJSONError(rec, &httpjson.Error{
		Status:  http.StatusRequestEntityTooLarge,
		Code:    "payload_too_large",
		Message: "request body must not exceed 8 bytes",
	})

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d", rec.Code)
	}
	var body APIError
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Code != "payload_too_large" || body.RequestID != "req-1" {
		t.Fatalf("body = %+v", body)
	}
}

func containsAny(value string, needles []string) bool {
	for _, needle := range needles {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}
