package api

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	backendretry "claude-codex/internal/backend/retry"
)

// CalculateBackoff calculates exponential backoff delay
func CalculateBackoff(attempt int, baseDelayMS int) time.Duration {
	return apiRetryPolicy(baseDelayMS).Delay(attempt+1, nil)
}

// GetDefaultMaxRetries returns the default max retry count
func GetDefaultMaxRetries() int {
	if envVal := os.Getenv("CLAUDE_CODE_MAX_RETRIES"); envVal != "" {
		if parsed, err := strconv.Atoi(envVal); err == nil && parsed > 0 {
			return parsed
		}
	}
	return DefaultMaxRetries
}

// IsPersistentRetryEnabled checks if persistent retry mode is enabled
func IsPersistentRetryEnabled() bool {
	return os.Getenv("CLAUDE_CODE_UNATTENDED_RETRY") == "true" ||
		os.Getenv("CLAUDE_CODE_UNATTENDED_RETRY") == "1"
}

// RetryState tracks retry attempt state
type RetryState struct {
	Attempt              int
	Consecutive529Errors int
	LastError            error
	LastStatusCode       int
	TotalDelay           time.Duration
	ShouldFallback       bool
	FallbackReason       string
}

// ShouldRetry determines if another retry attempt should be made
func (s *RetryState) ShouldRetry(opts RetryOptions) bool {
	maxRetries := opts.MaxRetries
	if maxRetries == 0 {
		maxRetries = GetDefaultMaxRetries()
	}

	// Check max retries
	if s.Attempt >= maxRetries {
		return false
	}

	// Check 529 retry limit
	if s.Consecutive529Errors >= Max529Retries {
		// Check if this query source should retry on 529
		if !ShouldRetry529(opts.QuerySource) {
			return false
		}

		// In persistent retry mode, continue retrying
		if !IsPersistentRetryEnabled() {
			return false
		}
	}

	// If no error, don't retry
	if s.LastError == nil {
		return false
	}

	// Check if error is retryable
	// For testing purposes, treat any error as potentially retryable
	// In production, this would check specific error types
	isClaudeAISubscriber := false
	isEnterpriseSubscriber := false

	return IsRetryableError(
		s.LastError,
		s.LastStatusCode,
		isClaudeAISubscriber,
		isEnterpriseSubscriber,
	)
}

// UpdateForError updates retry state after an error
func (s *RetryState) UpdateForError(err error, statusCode int) {
	s.LastError = err

	// Extract status code from error message if not provided
	if statusCode == 0 {
		if Is529Error(err) {
			statusCode = 529
		} else if Is429Error(err) {
			statusCode = 429
		} else if IsConnectionError(err) {
			statusCode = 0 // Connection errors don't have status codes
		} else {
			// Try to extract from error message
			errMsg := err.Error()
			if strings.Contains(errMsg, "500") {
				statusCode = 500
			} else if strings.Contains(errMsg, "401") {
				statusCode = 401
			} else if strings.Contains(errMsg, "403") {
				statusCode = 403
			}
		}
	}

	s.LastStatusCode = statusCode
	s.Attempt++

	// Track consecutive 529 errors
	if Is529Error(err) {
		s.Consecutive529Errors++
	} else {
		s.Consecutive529Errors = 0
	}
}

// GetNextDelay calculates the delay before next retry
func (s *RetryState) GetNextDelay(opts RetryOptions) time.Duration {
	// Check for retry-after header
	if retryAfter, ok := ExtractRetryAfterSeconds(s.LastError); ok {
		return time.Duration(retryAfter) * time.Second
	}

	// Use exponential backoff
	delay := CalculateBackoff(s.Attempt, BaseDelayMS)

	// In persistent retry mode, use higher backoff
	if IsPersistentRetryEnabled() && IsTransientCapacityError(s.LastError) {
		delay = CalculateBackoff(s.Attempt, BaseDelayMS*2)
	}

	return delay
}

// WithRetry executes a function with retry logic
func WithRetry(
	ctx context.Context,
	opts RetryOptions,
	fn func(ctx context.Context) error,
) error {
	state := &RetryState{
		Consecutive529Errors: opts.InitialConsecutive529Errors,
	}

	for {
		// Execute the function
		err := fn(ctx)

		// Success case
		if err == nil {
			return nil
		}

		// Extract status code if available
		statusCode := 0
		if Is529Error(err) {
			statusCode = 529
		} else if Is429Error(err) {
			statusCode = 429
		}

		// Update state
		state.UpdateForError(err, statusCode)

		// Check if we should retry
		if !state.ShouldRetry(opts) {
			return &CannotRetryError{
				OriginalError: err,
				RetryContext: RetryContext{
					Model:          opts.Model,
					ThinkingConfig: opts.ThinkingConfig,
					FastMode:       opts.FastMode,
				},
			}
		}

		// Calculate delay
		delay := state.GetNextDelay(opts)
		state.TotalDelay += delay

		if err := backendretry.Sleep(ctx, delay); err != nil {
			return err
		}

		// In persistent retry mode, send heartbeat
		if IsPersistentRetryEnabled() && delay > time.Duration(HeartbeatIntervalMS)*time.Millisecond {
			// TODO: Send heartbeat message to keep session alive
		}
	}
}

// RetryWithFallback executes with retry and optional fallback model
func RetryWithFallback(
	ctx context.Context,
	opts RetryOptions,
	fn func(ctx context.Context, model string) error,
) error {
	// First try with primary model
	err := WithRetry(ctx, opts, func(ctx context.Context) error {
		return fn(ctx, opts.Model)
	})

	// If no fallback model, return error
	if opts.FallbackModel == "" {
		return err
	}

	// Check if we should fallback
	if err != nil {
		var cannotRetry *CannotRetryError
		if ok := err.(*CannotRetryError); ok != nil {
			cannotRetry = ok
		}

		// Only fallback on certain errors
		if cannotRetry != nil && IsTransientCapacityError(cannotRetry.OriginalError) {
			// Try with fallback model
			fallbackOpts := opts
			fallbackOpts.Model = opts.FallbackModel
			fallbackOpts.FallbackModel = "" // No nested fallback

			fallbackErr := WithRetry(ctx, fallbackOpts, func(ctx context.Context) error {
				return fn(ctx, opts.FallbackModel)
			})

			if fallbackErr == nil {
				return &FallbackTriggeredError{
					OriginalModel: opts.Model,
					FallbackModel: opts.FallbackModel,
					Reason:        "capacity_error",
				}
			}

			return fallbackErr
		}
	}

	return err
}

// SleepWithContext sleeps for duration or until context is cancelled
func SleepWithContext(ctx context.Context, duration time.Duration) error {
	return backendretry.Sleep(ctx, duration)
}

// FormatRetryError formats a retry error for display
func FormatRetryError(err error) string {
	if err == nil {
		return ""
	}

	var cannotRetry *CannotRetryError
	if ok := err.(*CannotRetryError); ok != nil {
		cannotRetry = ok
	}

	if cannotRetry != nil {
		return fmt.Sprintf("Max retries exceeded: %s", FormatAPIError(cannotRetry.OriginalError, 0))
	}

	var fallback *FallbackTriggeredError
	if ok := err.(*FallbackTriggeredError); ok != nil {
		fallback = ok
	}

	if fallback != nil {
		return fmt.Sprintf("Fallback to %s due to %s", fallback.FallbackModel, fallback.Reason)
	}

	return err.Error()
}

func apiRetryPolicy(baseDelayMS int) backendretry.Policy {
	if baseDelayMS <= 0 {
		baseDelayMS = BaseDelayMS
	}
	return backendretry.Policy{
		BaseDelay: time.Duration(baseDelayMS) * time.Millisecond,
		MaxDelay:  time.Duration(PersistentMaxBackoffMS) * time.Millisecond,
		Jitter:    0,
	}
}
