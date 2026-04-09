package recovery

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestNewRetryHandler(t *testing.T) {
	options := RetryOptions{
		Model:          "claude-sonnet-4-6",
		FallbackModel:  "claude-haiku-4-5",
		ThinkingConfig: ThinkingConfig{Type: "adaptive"},
	}

	handler := NewRetryHandler(options)

	if handler.options.MaxRetries != DefaultMaxRetries {
		t.Errorf("Expected MaxRetries %d, got %d", DefaultMaxRetries, handler.options.MaxRetries)
	}
	if handler.state.Attempt != 0 {
		t.Errorf("Expected Attempt 0, got %d", handler.state.Attempt)
	}
}

func TestShouldRetry_MaxRetriesExceeded(t *testing.T) {
	options := RetryOptions{
		MaxRetries: 3,
		Model:      "claude-sonnet-4-6",
	}
	handler := NewRetryHandler(options)
	handler.state.Attempt = 3

	err := &RetryableError{StatusCode: 500, Message: "Internal error"}
	if handler.ShouldRetry(err) {
		t.Error("Should not retry after max retries exceeded")
	}
}

func TestShouldRetry_RetryableStatuses(t *testing.T) {
	tests := []struct {
		statusCode int
		shouldRetry bool
	}{
		{429, true},  // Rate limit
		{401, true},  // Unauthorized
		{403, true},  // Forbidden
		{500, true},  // Internal error
		{502, true},  // Bad gateway
		{503, true},  // Service unavailable
		{529, true},  // Overloaded
		{400, false}, // Bad request
		{404, false}, // Not found
		{200, false}, // Success
	}

	for _, tt := range tests {
		options := RetryOptions{
			Model:       "claude-sonnet-4-6",
			QuerySource: "repl_main_thread", // Foreground source for 529
		}
		handler := NewRetryHandler(options)

		err := &RetryableError{StatusCode: tt.statusCode, Message: "Test error"}
		result := handler.ShouldRetry(err)

		if result != tt.shouldRetry {
			t.Errorf("Status %d: expected shouldRetry=%v, got %v", tt.statusCode, tt.shouldRetry, result)
		}
	}
}

func TestShouldRetry_529Limit(t *testing.T) {
	options := RetryOptions{
		Model:       "claude-sonnet-4-6",
		QuerySource: "repl_main_thread",
	}
	handler := NewRetryHandler(options)

	err := &RetryableError{StatusCode: 529, Message: "Overloaded"}

	// First 3 attempts should retry
	for i := 0; i < Max529Retries; i++ {
		if !handler.ShouldRetry(err) {
			t.Errorf("Attempt %d: should retry 529", i)
		}
		handler.RecordAttempt(err)
	}

	// 4th attempt should not retry
	if handler.ShouldRetry(err) {
		t.Error("Should not retry after max 529 retries")
	}
}

func TestShouldRetry529_QuerySource(t *testing.T) {
	tests := []struct {
		querySource string
		shouldRetry bool
	}{
		{"repl_main_thread", true},
		{"sdk", true},
		{"agent:custom", true},
		{"compact", true},
		{"background_task", false},
		{"", true}, // Conservative default
	}

	for _, tt := range tests {
		options := RetryOptions{
			Model:       "claude-sonnet-4-6",
			QuerySource: tt.querySource,
		}
		handler := NewRetryHandler(options)

		result := handler.shouldRetry529()
		if result != tt.shouldRetry {
			t.Errorf("QuerySource %q: expected shouldRetry529=%v, got %v",
				tt.querySource, tt.shouldRetry, result)
		}
	}
}

func TestCalculateDelay_ExponentialBackoff(t *testing.T) {
	options := RetryOptions{
		Model: "claude-sonnet-4-6",
	}
	handler := NewRetryHandler(options)

	err := &RetryableError{StatusCode: 500, Message: "Internal error"}

	// Test exponential backoff
	expectedDelays := []time.Duration{
		500 * time.Millisecond,  // 2^0 * 500ms
		1000 * time.Millisecond, // 2^1 * 500ms
		2000 * time.Millisecond, // 2^2 * 500ms
		4000 * time.Millisecond, // 2^3 * 500ms
	}

	for i, expected := range expectedDelays {
		handler.state.Attempt = i
		delay := handler.CalculateDelay(err)

		if delay != expected {
			t.Errorf("Attempt %d: expected delay %v, got %v", i, expected, delay)
		}
	}
}

func TestCalculateDelay_RetryAfterHeader(t *testing.T) {
	options := RetryOptions{
		Model: "claude-sonnet-4-6",
	}
	handler := NewRetryHandler(options)

	retryAfter := 5 * time.Second
	err := &RetryableError{
		StatusCode: 429,
		Message:    "Rate limited",
		RetryAfter: &retryAfter,
	}

	delay := handler.CalculateDelay(err)
	if delay != retryAfter {
		t.Errorf("Expected delay %v, got %v", retryAfter, delay)
	}
}

func TestRecordAttempt(t *testing.T) {
	options := RetryOptions{
		Model: "claude-sonnet-4-6",
	}
	handler := NewRetryHandler(options)

	err := &RetryableError{StatusCode: 500, Message: "Internal error"}
	handler.RecordAttempt(err)

	if handler.state.Attempt != 1 {
		t.Errorf("Expected Attempt 1, got %d", handler.state.Attempt)
	}
	if handler.state.LastError != err {
		t.Error("LastError not recorded")
	}
}

func TestRecordAttempt_Consecutive529(t *testing.T) {
	options := RetryOptions{
		Model: "claude-sonnet-4-6",
	}
	handler := NewRetryHandler(options)

	err529 := &RetryableError{StatusCode: 529, Message: "Overloaded"}
	err500 := &RetryableError{StatusCode: 500, Message: "Internal error"}

	// Record two 529 errors
	handler.RecordAttempt(err529)
	handler.RecordAttempt(err529)

	if handler.state.Consecutive529Errors != 2 {
		t.Errorf("Expected Consecutive529Errors 2, got %d", handler.state.Consecutive529Errors)
	}

	// Record a different error - should reset counter
	handler.RecordAttempt(err500)

	if handler.state.Consecutive529Errors != 0 {
		t.Errorf("Expected Consecutive529Errors reset to 0, got %d", handler.state.Consecutive529Errors)
	}
}

func TestWait(t *testing.T) {
	options := RetryOptions{
		Model: "claude-sonnet-4-6",
	}
	handler := NewRetryHandler(options)

	ctx := context.Background()
	delay := 50 * time.Millisecond

	start := time.Now()
	err := handler.Wait(ctx, delay)
	elapsed := time.Since(start)

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	if elapsed < delay {
		t.Errorf("Wait returned too early: %v < %v", elapsed, delay)
	}

	if handler.state.TotalDelay != delay {
		t.Errorf("Expected TotalDelay %v, got %v", delay, handler.state.TotalDelay)
	}
}

func TestWait_ContextCanceled(t *testing.T) {
	options := RetryOptions{
		Model: "claude-sonnet-4-6",
	}
	handler := NewRetryHandler(options)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	delay := 1 * time.Second
	err := handler.Wait(ctx, delay)

	if !errors.Is(err, context.Canceled) {
		t.Errorf("Expected context.Canceled, got %v", err)
	}
}

func TestShouldFallback(t *testing.T) {
	options := RetryOptions{
		Model:         "claude-sonnet-4-6",
		FallbackModel: "claude-haiku-4-5",
	}
	handler := NewRetryHandler(options)

	// Should not fallback initially
	if handler.ShouldFallback() {
		t.Error("Should not fallback initially")
	}

	// Should fallback after max 529 errors
	handler.state.Consecutive529Errors = Max529Retries
	if !handler.ShouldFallback() {
		t.Error("Should fallback after max 529 errors")
	}
}

func TestShouldFallback_NoFallbackModel(t *testing.T) {
	options := RetryOptions{
		Model: "claude-sonnet-4-6",
		// No fallback model
	}
	handler := NewRetryHandler(options)

	handler.state.Consecutive529Errors = Max529Retries
	if handler.ShouldFallback() {
		t.Error("Should not fallback without fallback model")
	}
}

func TestIsTransientError(t *testing.T) {
	tests := []struct {
		statusCode  int
		isTransient bool
	}{
		{429, true},
		{529, true},
		{500, false},
		{401, false},
	}

	for _, tt := range tests {
		err := &RetryableError{StatusCode: tt.statusCode, Message: "Test"}
		result := IsTransientError(err)

		if result != tt.isTransient {
			t.Errorf("Status %d: expected isTransient=%v, got %v",
				tt.statusCode, tt.isTransient, result)
		}
	}
}

func TestIsConnectionError(t *testing.T) {
	tests := []struct {
		errMsg       string
		isConnection bool
	}{
		{"connection reset by peer", true},
		{"broken pipe", true},
		{"EOF", true},
		{"internal server error", false},
		{"rate limited", false},
	}

	for _, tt := range tests {
		err := errors.New(tt.errMsg)
		result := IsConnectionError(err)

		if result != tt.isConnection {
			t.Errorf("Error %q: expected isConnection=%v, got %v",
				tt.errMsg, tt.isConnection, result)
		}
	}
}
