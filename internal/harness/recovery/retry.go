package recovery

import (
	"context"
	"errors"
	"math"
	"strconv"
	"time"
)

// RetryHandler handles retry logic for API calls.
type RetryHandler struct {
	options RetryOptions
	state   *RetryState
}

// NewRetryHandler creates a new retry handler.
func NewRetryHandler(options RetryOptions) *RetryHandler {
	if options.MaxRetries == 0 {
		options.MaxRetries = DefaultMaxRetries
	}

	return &RetryHandler{
		options: options,
		state:   NewRetryState(options.InitialConsecutive529Errors),
	}
}

// ShouldRetry determines if an error should be retried.
func (h *RetryHandler) ShouldRetry(err error) bool {
	if err == nil {
		return false
	}

	// Check if we've exceeded max retries
	if h.state.Attempt >= h.options.MaxRetries {
		return false
	}

	// Check for retryable error
	var retryableErr *RetryableError
	if !errors.As(err, &retryableErr) {
		return false
	}

	// Check specific status codes
	return h.isRetryableStatus(retryableErr.StatusCode)
}

// isRetryableStatus checks if a status code is retryable.
func (h *RetryHandler) isRetryableStatus(statusCode int) bool {
	switch statusCode {
	case 429: // Rate limit
		return true
	case 401: // Unauthorized (can retry after clearing cache)
		return true
	case 403: // Forbidden (token revoked)
		return true
	case 529: // Overloaded
		// Check if we should retry 529 based on query source
		if !h.shouldRetry529() {
			return false
		}
		// Check consecutive 529 limit
		return h.state.Consecutive529Errors < Max529Retries
	default:
		// Retry on 5xx errors
		return statusCode >= 500 && statusCode < 600
	}
}

// shouldRetry529 checks if 529 errors should be retried for this query source.
func (h *RetryHandler) shouldRetry529() bool {
	// Foreground query sources where user is blocking on result
	foregroundSources := map[string]bool{
		"repl_main_thread": true,
		"sdk":              true,
		"agent:custom":     true,
		"agent:default":    true,
		"agent:builtin":    true,
		"compact":          true,
		"hook_agent":       true,
		"hook_prompt":      true,
		"verification_agent": true,
		"side_question":    true,
		"auto_mode":        true,
	}

	// If no query source specified, retry (conservative)
	if h.options.QuerySource == "" {
		return true
	}

	return foregroundSources[h.options.QuerySource]
}

// CalculateDelay calculates the delay before next retry.
func (h *RetryHandler) CalculateDelay(err error) time.Duration {
	var retryableErr *RetryableError
	if errors.As(err, &retryableErr) {
		// Use Retry-After header if available
		if retryableErr.RetryAfter != nil {
			return *retryableErr.RetryAfter
		}

		// Check for rate limit reset header
		if resetDelay := h.getRateLimitResetDelay(retryableErr); resetDelay > 0 {
			return resetDelay
		}
	}

	// Exponential backoff
	baseDelay := time.Duration(BaseDelayMs) * time.Millisecond
	delay := baseDelay * time.Duration(math.Pow(2, float64(h.state.Attempt)))

	// Cap at max backoff
	maxBackoff := PersistentMaxBackoff
	if delay > maxBackoff {
		delay = maxBackoff
	}

	return delay
}

// getRateLimitResetDelay extracts delay from rate limit reset header.
func (h *RetryHandler) getRateLimitResetDelay(err *RetryableError) time.Duration {
	if err.Headers == nil {
		return 0
	}

	resetHeader := err.Headers["anthropic-ratelimit-unified-reset"]
	if resetHeader == "" {
		return 0
	}

	resetUnixSec, parseErr := strconv.ParseInt(resetHeader, 10, 64)
	if parseErr != nil {
		return 0
	}

	delayMs := time.Unix(resetUnixSec, 0).Sub(time.Now())
	if delayMs <= 0 {
		return 0
	}

	// Cap at persistent reset cap
	if delayMs > PersistentResetCap {
		delayMs = PersistentResetCap
	}

	return delayMs
}

// RecordAttempt records a retry attempt.
func (h *RetryHandler) RecordAttempt(err error) {
	h.state.Attempt++
	h.state.LastError = err

	// Track consecutive 529 errors
	var retryableErr *RetryableError
	if errors.As(err, &retryableErr) && retryableErr.StatusCode == 529 {
		h.state.Consecutive529Errors++
	} else {
		h.state.Consecutive529Errors = 0
	}
}

// Wait waits for the calculated delay before retry.
func (h *RetryHandler) Wait(ctx context.Context, delay time.Duration) error {
	h.state.TotalDelay += delay

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(delay):
		return nil
	}
}

// GetState returns the current retry state.
func (h *RetryHandler) GetState() *RetryState {
	return h.state
}

// ShouldFallback checks if we should fallback to a different model.
func (h *RetryHandler) ShouldFallback() bool {
	// Fallback after max 529 retries
	if h.state.Consecutive529Errors >= Max529Retries {
		return h.options.FallbackModel != ""
	}

	// Fallback for fast mode if triggered
	if h.state.FastModeFallbackTriggered {
		return h.options.FallbackModel != ""
	}

	return false
}

// CreateFallbackError creates a fallback error.
func (h *RetryHandler) CreateFallbackError(reason string) error {
	return &FallbackTriggeredError{
		OriginalModel: h.options.Model,
		FallbackModel: h.options.FallbackModel,
		Reason:        reason,
	}
}

// CreateCannotRetryError creates a cannot retry error.
func (h *RetryHandler) CreateCannotRetryError(err error) error {
	return &CannotRetryError{
		OriginalError: err,
		RetryContext: RetryContext{
			Model:          h.options.Model,
			ThinkingConfig: h.options.ThinkingConfig,
			FastMode:       h.options.FastMode,
		},
	}
}

// IsTransientError checks if an error is transient (429 or 529).
func IsTransientError(err error) bool {
	var retryableErr *RetryableError
	if !errors.As(err, &retryableErr) {
		return false
	}
	return retryableErr.StatusCode == 429 || retryableErr.StatusCode == 529
}

// IsConnectionError checks if an error is a connection error.
func IsConnectionError(err error) bool {
	// Check for common connection error patterns
	errStr := err.Error()
	return contains(errStr, "connection reset") ||
		contains(errStr, "broken pipe") ||
		contains(errStr, "EOF")
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) &&
		(s[:len(substr)] == substr || s[len(s)-len(substr):] == substr ||
		len(s) > len(substr)*2 && containsMiddle(s, substr)))
}

func containsMiddle(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
