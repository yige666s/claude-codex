package api

import (
	"time"

	anthropic "claude-codex/internal/harness/anthropic"
)

// RetryContext holds context for retry operations
type RetryContext struct {
	MaxTokensOverride *int
	Model             string
	ThinkingConfig    ThinkingConfig
	FastMode          bool
}

// ThinkingConfig controls extended thinking behavior
type ThinkingConfig struct {
	Enabled       bool
	BudgetTokens  int
	Type          string // "enabled" or "disabled"
	ThinkingType  string // "extended" or "normal"
}

// RetryOptions configures retry behavior
type RetryOptions struct {
	MaxRetries                  int
	Model                       string
	FallbackModel               string
	ThinkingConfig              ThinkingConfig
	FastMode                    bool
	QuerySource                 string
	InitialConsecutive529Errors int
}

// ErrorClassification categorizes API errors
type ErrorClassification string

const (
	ErrorClassUnknown              ErrorClassification = "unknown"
	ErrorClassRateLimit            ErrorClassification = "rate_limit"
	ErrorClassAuthenticationFailed ErrorClassification = "authentication_failed"
	ErrorClassServerError          ErrorClassification = "server_error"
	ErrorClassConnectionError      ErrorClassification = "connection_error"
	ErrorClassSSLCertError         ErrorClassification = "ssl_cert_error"
	ErrorClassInvalidRequest       ErrorClassification = "invalid_request"
)

// RateLimit represents rate limit information
type RateLimit struct {
	Utilization *float64   `json:"utilization"` // percentage 0-100
	ResetsAt    *time.Time `json:"resets_at"`   // ISO 8601 timestamp
}

// ExtraUsage represents extra usage credits
type ExtraUsage struct {
	IsEnabled    bool     `json:"is_enabled"`
	MonthlyLimit *int     `json:"monthly_limit"`
	UsedCredits  *int     `json:"used_credits"`
	Utilization  *float64 `json:"utilization"`
}

// Utilization represents API usage and rate limits
type Utilization struct {
	FiveHour          *RateLimit  `json:"five_hour,omitempty"`
	SevenDay          *RateLimit  `json:"seven_day,omitempty"`
	SevenDayOAuthApps *RateLimit  `json:"seven_day_oauth_apps,omitempty"`
	SevenDayOpus      *RateLimit  `json:"seven_day_opus,omitempty"`
	SevenDaySonnet    *RateLimit  `json:"seven_day_sonnet,omitempty"`
	ExtraUsage        *ExtraUsage `json:"extra_usage,omitempty"`
}

// Constants for retry behavior
const (
	DefaultMaxRetries           = 10
	FloorOutputTokens           = 3000
	Max529Retries               = 3
	BaseDelayMS                 = 500
	PersistentMaxBackoffMS      = 5 * 60 * 1000  // 5 minutes
	PersistentResetCapMS        = 6 * 60 * 60 * 1000 // 6 hours
	HeartbeatIntervalMS         = 30000
	DefaultFastModeFallbackMS   = 30 * 60 * 1000 // 30 minutes
	ShortRetryThresholdMS       = 20 * 1000       // 20 seconds
	MinCooldownMS               = 10 * 60 * 1000  // 10 minutes
)

// Error messages
const (
	APIErrorMessagePrefix              = "API Error"
	PromptTooLongErrorMessage          = "Prompt is too long"
	CreditBalanceTooLowErrorMessage    = "Credit balance is too low"
	InvalidAPIKeyErrorMessage          = "Not logged in · Please run /login"
	InvalidAPIKeyErrorMessageExternal  = "Invalid API key · Fix external API key"
	Repeated529ErrorMessage            = "API is experiencing high demand"
)

// Foreground query sources that should retry on 529 errors
var Foreground529RetrySources = map[string]bool{
	"repl_main_thread":                          true,
	"repl_main_thread:outputStyle:custom":       true,
	"repl_main_thread:outputStyle:Explanatory":  true,
	"repl_main_thread:outputStyle:Learning":     true,
	"sdk":                                       true,
	"agent:custom":                              true,
	"agent:default":                             true,
	"agent:builtin":                             true,
	"compact":                                   true,
	"hook_agent":                                true,
	"hook_prompt":                               true,
	"verification_agent":                        true,
	"side_question":                             true,
	"auto_mode":                                 true,
}

// CannotRetryError wraps an error that cannot be retried
type CannotRetryError struct {
	OriginalError error
	RetryContext  RetryContext
	Message       string
}

func (e *CannotRetryError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	if e.OriginalError != nil {
		return e.OriginalError.Error()
	}
	return "cannot retry"
}

func (e *CannotRetryError) Unwrap() error {
	return e.OriginalError
}

// FallbackTriggeredError indicates a fallback model was triggered
type FallbackTriggeredError struct {
	OriginalModel string
	FallbackModel string
	Reason        string
}

func (e *FallbackTriggeredError) Error() string {
	return "fallback triggered: " + e.Reason
}

// APIProvider represents different API providers
type APIProvider string

const (
	ProviderFirstParty APIProvider = "firstParty"
	ProviderAWS        APIProvider = "aws"
	ProviderVertex     APIProvider = "vertex"
	ProviderFoundry    APIProvider = "foundry"
)

// ClientConfig holds configuration for creating API clients
type ClientConfig struct {
	APIKey        string
	MaxRetries    int
	Model         string
	FetchOverride interface{} // Custom fetch function
	Source        string
	Provider      APIProvider
}

// MessageRequest wraps the Anthropic API message request
type MessageRequest = anthropic.MessageRequest

// MessageResponse wraps the Anthropic API message response
type MessageResponse = anthropic.MessageResponse
