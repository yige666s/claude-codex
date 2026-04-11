package claudeailimits

import (
	"net/http"
	"testing"
)

func TestServiceProcessesHeaders(t *testing.T) {
	svc := NewService()
	headers := http.Header{}
	headers.Set("anthropic-ratelimit-requests-limit", "100")
	headers.Set("anthropic-ratelimit-requests-remaining", "5")
	headers.Set("anthropic-ratelimit-requests-reset", "2030-01-01T00:00:00Z")
	svc.ProcessResponseHeaders(headers)
	limits := svc.CurrentLimits()
	if limits.Status == "" {
		t.Fatalf("expected limits to be updated")
	}
}
