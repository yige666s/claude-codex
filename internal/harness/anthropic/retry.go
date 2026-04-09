package anthropic

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"net/http"
	"time"
)

// HTTPError represents an HTTP error response
type HTTPError struct {
	StatusCode int
	Status     string
	Body       string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("HTTP %d: %s", e.StatusCode, e.Status)
}

// RetryConfig configures exponential backoff retry behavior
type RetryConfig struct {
	MaxRetries     int
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
	BackoffFactor  float64
	Jitter         bool
}

// DefaultRetryConfig returns sensible defaults for API retries
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxRetries:     3,
		InitialBackoff: 500 * time.Millisecond,
		MaxBackoff:     30 * time.Second,
		BackoffFactor:  2.0,
		Jitter:         true,
	}
}

// isRetryableError determines if an error should trigger a retry
func isRetryableError(err error) bool {
	if err == nil {
		return false
	}

	// Check for context cancellation - never retry
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}

	// Check for HTTP errors
	var httpErr *HTTPError
	if errors.As(err, &httpErr) {
		// Retry on rate limits, server errors, and timeouts
		return httpErr.StatusCode == http.StatusTooManyRequests ||
			httpErr.StatusCode == http.StatusRequestTimeout ||
			httpErr.StatusCode == http.StatusServiceUnavailable ||
			httpErr.StatusCode == http.StatusGatewayTimeout ||
			(httpErr.StatusCode >= 500 && httpErr.StatusCode < 600)
	}

	return false
}

// calculateBackoff computes the backoff duration for a given attempt
func calculateBackoff(config RetryConfig, attempt int) time.Duration {
	backoff := float64(config.InitialBackoff) * math.Pow(config.BackoffFactor, float64(attempt))

	if backoff > float64(config.MaxBackoff) {
		backoff = float64(config.MaxBackoff)
	}

	if config.Jitter {
		// Add random jitter: ±25% of backoff
		jitter := backoff * 0.25 * (rand.Float64()*2 - 1)
		backoff += jitter
	}

	return time.Duration(backoff)
}

// withRetry wraps a function with exponential backoff retry logic
func withRetry(ctx context.Context, config RetryConfig, fn func() error) error {
	var lastErr error

	for attempt := 0; attempt <= config.MaxRetries; attempt++ {
		if attempt > 0 {
			backoff := calculateBackoff(config, attempt-1)

			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
		}

		lastErr = fn()
		if lastErr == nil {
			return nil
		}

		if !isRetryableError(lastErr) {
			return lastErr
		}

		// If this was the last attempt, return the error
		if attempt == config.MaxRetries {
			return lastErr
		}
	}

	return lastErr
}
