package retry

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"
)

type retryAfterHeaderError string

func (e retryAfterHeaderError) Error() string            { return "retry after" }
func (e retryAfterHeaderError) RetryAfterHeader() string { return string(e) }

func TestPolicyDelayUsesExponentialBackoff(t *testing.T) {
	policy := Policy{BaseDelay: 10 * time.Millisecond, MaxDelay: 50 * time.Millisecond, Jitter: 0}
	if got := policy.Delay(1, nil); got != 10*time.Millisecond {
		t.Fatalf("attempt 1 delay = %s", got)
	}
	if got := policy.Delay(3, nil); got != 40*time.Millisecond {
		t.Fatalf("attempt 3 delay = %s", got)
	}
	if got := policy.Delay(6, nil); got != 50*time.Millisecond {
		t.Fatalf("capped delay = %s", got)
	}
}

func TestPolicyDelayUsesRetryAfterHeader(t *testing.T) {
	now := time.Date(2026, 5, 25, 10, 0, 0, 0, time.UTC)
	policy := Policy{BaseDelay: time.Millisecond, Jitter: 0, Now: func() time.Time { return now }}
	if got := policy.Delay(1, retryAfterHeaderError("3")); got != 3*time.Second {
		t.Fatalf("seconds retry-after delay = %s", got)
	}
	when := now.Add(5 * time.Second).UTC().Format(http.TimeFormat)
	if got := policy.Delay(1, retryAfterHeaderError(when)); got != 5*time.Second {
		t.Fatalf("date retry-after delay = %s", got)
	}
}

func TestPolicySleepHonorsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := (Policy{BaseDelay: time.Hour}).Sleep(ctx, 1, errors.New("boom"))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("sleep error = %v, want context canceled", err)
	}
}

func TestPolicyRestrictsMethodsAndStatuses(t *testing.T) {
	policy := Policy{
		MaxAttempts: 3,
		Methods:     map[string]bool{http.MethodGet: true},
		Statuses:    map[int]bool{http.StatusServiceUnavailable: true},
	}
	if got := policy.AttemptsForMethod(http.MethodPost); got != 1 {
		t.Fatalf("POST attempts = %d", got)
	}
	if !policy.ShouldRetry(http.MethodGet, http.StatusServiceUnavailable, errors.New("boom")) {
		t.Fatal("expected GET 503 to retry")
	}
	if policy.ShouldRetry(http.MethodGet, http.StatusBadRequest, errors.New("boom")) {
		t.Fatal("did not expect GET 400 to retry")
	}
}
