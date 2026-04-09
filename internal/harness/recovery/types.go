package recovery

import (
	"context"
	"fmt"
	"time"
)

// Retry configuration constants
const (
	DefaultMaxRetries     = 10
	FloorOutputTokens     = 3000
	Max529Retries         = 3
	BaseDelayMs           = 500
	PersistentMaxBackoff  = 5 * time.Minute
	PersistentResetCap    = 6 * time.Hour
	HeartbeatInterval     = 30 * time.Second
	ShortRetryThreshold   = 20 * time.Second
	MinCooldown           = 10 * time.Minute
	DefaultFastModeFallbackHold = 30 * time.Minute
)

// RetryContext contains context for retry operations.
type RetryContext struct {
	MaxTokensOverride *int
	Model             string
	ThinkingConfig    ThinkingConfig
	FastMode          bool
}

// ThinkingConfig controls thinking behavior.
type ThinkingConfig struct {
	Type string // "adaptive", "enabled", "disabled"
}

// RetryOptions configures retry behavior.
type RetryOptions struct {
	MaxRetries                  int
	Model                       string
	FallbackModel               string
	ThinkingConfig              ThinkingConfig
	FastMode                    bool
	Signal                      context.Context
	QuerySource                 string
	InitialConsecutive529Errors int
}

// RetryableError represents an error that can be retried.
type RetryableError struct {
	StatusCode int
	Message    string
	RetryAfter *time.Duration
	Headers    map[string]string
}

func (e *RetryableError) Error() string {
	return fmt.Sprintf("retryable error (status %d): %s", e.StatusCode, e.Message)
}

// CannotRetryError represents an error that cannot be retried.
type CannotRetryError struct {
	OriginalError error
	RetryContext  RetryContext
}

func (e *CannotRetryError) Error() string {
	return fmt.Sprintf("cannot retry: %v", e.OriginalError)
}

func (e *CannotRetryError) Unwrap() error {
	return e.OriginalError
}

// FallbackTriggeredError indicates a fallback was triggered.
type FallbackTriggeredError struct {
	OriginalModel string
	FallbackModel string
	Reason        string
}

func (e *FallbackTriggeredError) Error() string {
	return fmt.Sprintf("fallback from %s to %s: %s", e.OriginalModel, e.FallbackModel, e.Reason)
}

// RetryState tracks retry state across attempts.
type RetryState struct {
	Attempt                  int
	Consecutive529Errors     int
	LastError                error
	TotalDelay               time.Duration
	FastModeFallbackTriggered bool
}

// NewRetryState creates a new retry state.
func NewRetryState(initialConsecutive529Errors int) *RetryState {
	return &RetryState{
		Attempt:              0,
		Consecutive529Errors: initialConsecutive529Errors,
		TotalDelay:           0,
	}
}
