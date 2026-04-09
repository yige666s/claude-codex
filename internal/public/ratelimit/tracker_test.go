package ratelimit

import (
	"net/http"
	"testing"
	"time"
)

func TestNewTracker(t *testing.T) {
	tracker := NewTracker()
	limits := tracker.GetCurrentLimits()

	if limits.Status != QuotaAllowed {
		t.Errorf("expected status %s, got %s", QuotaAllowed, limits.Status)
	}
	if limits.IsUsingOverage {
		t.Error("expected IsUsingOverage to be false")
	}
}

func TestProcessResponseHeaders_FiveHourLimit(t *testing.T) {
	tracker := NewTracker()
	headers := http.Header{}

	// Simulate 95% utilization in 5-hour window (should trigger warning)
	resetTime := time.Now().Add(2 * time.Hour)
	headers.Set("anthropic-ratelimit-requests-limit", "1000")
	headers.Set("anthropic-ratelimit-requests-remaining", "50")
	headers.Set("anthropic-ratelimit-requests-reset", resetTime.Format(time.RFC3339))

	tracker.ProcessResponseHeaders(headers)
	limits := tracker.GetCurrentLimits()

	if limits.Status != QuotaAllowedWarning {
		t.Errorf("expected status %s, got %s", QuotaAllowedWarning, limits.Status)
	}

	if limits.RateLimitType == nil || *limits.RateLimitType != RateLimitFiveHour {
		t.Error("expected RateLimitFiveHour")
	}

	raw := tracker.GetRawUtilization()
	if raw.FiveHour == nil {
		t.Fatal("expected FiveHour utilization to be set")
	}
	if raw.FiveHour.Utilization < 0.94 || raw.FiveHour.Utilization > 0.96 {
		t.Errorf("expected utilization ~0.95, got %f", raw.FiveHour.Utilization)
	}
}

func TestProcessResponseHeaders_SevenDayLimit(t *testing.T) {
	tracker := NewTracker()
	headers := http.Header{}

	// Simulate 80% utilization in 7-day window early in the period
	resetTime := time.Now().Add(5 * 24 * time.Hour)
	headers.Set("anthropic-ratelimit-tokens-limit", "10000")
	headers.Set("anthropic-ratelimit-tokens-remaining", "2000")
	headers.Set("anthropic-ratelimit-tokens-reset", resetTime.Format(time.RFC3339))

	tracker.ProcessResponseHeaders(headers)
	limits := tracker.GetCurrentLimits()

	if limits.Status != QuotaAllowedWarning {
		t.Errorf("expected status %s, got %s", QuotaAllowedWarning, limits.Status)
	}

	if limits.RateLimitType == nil || *limits.RateLimitType != RateLimitSevenDay {
		t.Error("expected RateLimitSevenDay")
	}
}

func TestProcessResponseHeaders_NoWarning(t *testing.T) {
	tracker := NewTracker()
	headers := http.Header{}

	// Simulate low utilization
	resetTime := time.Now().Add(4 * time.Hour)
	headers.Set("anthropic-ratelimit-requests-limit", "1000")
	headers.Set("anthropic-ratelimit-requests-remaining", "900")
	headers.Set("anthropic-ratelimit-requests-reset", resetTime.Format(time.RFC3339))

	tracker.ProcessResponseHeaders(headers)
	limits := tracker.GetCurrentLimits()

	if limits.Status != QuotaAllowed {
		t.Errorf("expected status %s, got %s", QuotaAllowed, limits.Status)
	}
}

func TestProcessError_429(t *testing.T) {
	tracker := NewTracker()
	headers := http.Header{}

	resetTime := time.Now().Add(1 * time.Hour)
	headers.Set("anthropic-ratelimit-requests-limit", "1000")
	headers.Set("anthropic-ratelimit-requests-remaining", "0")
	headers.Set("anthropic-ratelimit-requests-reset", resetTime.Format(time.RFC3339))

	tracker.ProcessError(429, headers)
	limits := tracker.GetCurrentLimits()

	if limits.Status != QuotaRejected {
		t.Errorf("expected status %s, got %s", QuotaRejected, limits.Status)
	}
}

func TestProcessError_NonRateLimit(t *testing.T) {
	tracker := NewTracker()
	headers := http.Header{}

	tracker.ProcessError(500, headers)
	limits := tracker.GetCurrentLimits()

	// Should not change status
	if limits.Status != QuotaAllowed {
		t.Errorf("expected status %s, got %s", QuotaAllowed, limits.Status)
	}
}

func TestStatusListener(t *testing.T) {
	tracker := NewTracker()
	called := false
	var receivedLimits ClaudeAILimits

	tracker.AddStatusListener(func(limits ClaudeAILimits) {
		called = true
		receivedLimits = limits
	})

	headers := http.Header{}
	resetTime := time.Now().Add(2 * time.Hour)
	headers.Set("anthropic-ratelimit-requests-limit", "1000")
	headers.Set("anthropic-ratelimit-requests-remaining", "50")
	headers.Set("anthropic-ratelimit-requests-reset", resetTime.Format(time.RFC3339))

	tracker.ProcessResponseHeaders(headers)

	if !called {
		t.Error("expected status listener to be called")
	}
	if receivedLimits.Status != QuotaAllowedWarning {
		t.Errorf("expected status %s, got %s", QuotaAllowedWarning, receivedLimits.Status)
	}
}

func TestComputeTimeProgress(t *testing.T) {
	// Test at 50% through a 1-hour window
	resetsAt := time.Now().Add(30 * time.Minute)
	windowSeconds := 3600

	progress := computeTimeProgress(resetsAt, windowSeconds)

	if progress < 0.45 || progress > 0.55 {
		t.Errorf("expected progress ~0.5, got %f", progress)
	}
}

func TestGetRateLimitDisplayName(t *testing.T) {
	tests := []struct {
		rateLimitType RateLimitType
		expected      string
	}{
		{RateLimitFiveHour, "session limit"},
		{RateLimitSevenDay, "weekly limit"},
		{RateLimitSevenDayOpus, "Opus limit"},
		{RateLimitOverage, "extra usage limit"},
	}

	for _, tt := range tests {
		result := GetRateLimitDisplayName(tt.rateLimitType)
		if result != tt.expected {
			t.Errorf("expected %s, got %s", tt.expected, result)
		}
	}
}

func TestGetRateLimitErrorMessage(t *testing.T) {
	resetTime := time.Now().Add(2 * time.Hour)
	rateLimitType := RateLimitFiveHour

	limits := ClaudeAILimits{
		Status:        QuotaRejected,
		RateLimitType: &rateLimitType,
		ResetsAt:      &resetTime,
	}

	msg := GetRateLimitErrorMessage(limits)

	if msg == "" {
		t.Error("expected non-empty error message")
	}
	if !contains(msg, "session limit") {
		t.Errorf("expected message to contain 'session limit', got: %s", msg)
	}
}

func TestGetRateLimitWarning(t *testing.T) {
	resetTime := time.Now().Add(3 * time.Hour)
	rateLimitType := RateLimitSevenDay
	utilization := 0.75

	limits := ClaudeAILimits{
		Status:        QuotaAllowedWarning,
		RateLimitType: &rateLimitType,
		Utilization:   &utilization,
		ResetsAt:      &resetTime,
	}

	msg := GetRateLimitWarning(limits)

	if msg == "" {
		t.Error("expected non-empty warning message")
	}
	if !contains(msg, "75%") {
		t.Errorf("expected message to contain '75%%', got: %s", msg)
	}
	if !contains(msg, "weekly limit") {
		t.Errorf("expected message to contain 'weekly limit', got: %s", msg)
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		duration time.Duration
		expected string
	}{
		{30 * time.Second, "less than a minute"},
		{1 * time.Minute, "1 minute"},
		{45 * time.Minute, "45 minutes"},
		{1 * time.Hour, "1 hour"},
		{3 * time.Hour, "3 hours"},
		{24 * time.Hour, "1 day"},
		{72 * time.Hour, "3 days"},
	}

	for _, tt := range tests {
		result := formatDuration(tt.duration)
		if result != tt.expected {
			t.Errorf("formatDuration(%v) = %s, expected %s", tt.duration, result, tt.expected)
		}
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
