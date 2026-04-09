package ratelimit

import (
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// Early warning configurations in priority order
var earlyWarningConfigs = []EarlyWarningConfig{
	{
		RateLimitType: RateLimitFiveHour,
		ClaimAbbrev:   "5h",
		WindowSeconds: 5 * 60 * 60,
		Thresholds:    []EarlyWarningThreshold{{Utilization: 0.9, TimePct: 0.72}},
	},
	{
		RateLimitType: RateLimitSevenDay,
		ClaimAbbrev:   "7d",
		WindowSeconds: 7 * 24 * 60 * 60,
		Thresholds: []EarlyWarningThreshold{
			{Utilization: 0.75, TimePct: 0.6},
			{Utilization: 0.5, TimePct: 0.35},
			{Utilization: 0.25, TimePct: 0.15},
		},
	},
}

// Maps claim abbreviations to rate limit types
var earlyWarningClaimMap = map[string]RateLimitType{
	"5h":      RateLimitFiveHour,
	"7d":      RateLimitSevenDay,
	"overage": RateLimitOverage,
}

// Display names for rate limit types
var rateLimitDisplayNames = map[RateLimitType]string{
	RateLimitFiveHour:       "session limit",
	RateLimitSevenDay:       "weekly limit",
	RateLimitSevenDayOpus:   "Opus limit",
	RateLimitSevenDaySonnet: "Sonnet limit",
	RateLimitOverage:        "extra usage limit",
}

// Tracker manages rate limit state and notifications
type Tracker struct {
	mu              sync.RWMutex
	currentLimits   ClaudeAILimits
	rawUtilization  RawUtilization
	statusListeners []StatusListener
}

// NewTracker creates a new rate limit tracker
func NewTracker() *Tracker {
	return &Tracker{
		currentLimits: ClaudeAILimits{
			Status:                            QuotaAllowed,
			UnifiedRateLimitFallbackAvailable: false,
			IsUsingOverage:                    false,
		},
		rawUtilization:  RawUtilization{},
		statusListeners: []StatusListener{},
	}
}

// GetCurrentLimits returns the current rate limit state
func (t *Tracker) GetCurrentLimits() ClaudeAILimits {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.currentLimits
}

// GetRawUtilization returns raw utilization data for all windows
func (t *Tracker) GetRawUtilization() RawUtilization {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.rawUtilization
}

// AddStatusListener registers a callback for status changes
func (t *Tracker) AddStatusListener(listener StatusListener) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.statusListeners = append(t.statusListeners, listener)
}

// ProcessResponseHeaders extracts rate limit info from API response headers
func (t *Tracker) ProcessResponseHeaders(headers http.Header) {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Extract raw utilization from headers
	t.rawUtilization = extractRawUtilization(headers)

	// Compute new limits from headers
	newLimits := t.computeNewLimitsFromHeaders(headers)

	// Check if limits changed
	if !limitsEqual(t.currentLimits, newLimits) {
		t.currentLimits = newLimits
		t.emitStatusChange(newLimits)
	}
}

// ProcessError extracts rate limit info from 429 error
func (t *Tracker) ProcessError(statusCode int, headers http.Header) {
	if statusCode != 429 {
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	newLimits := t.currentLimits
	if headers != nil {
		t.rawUtilization = extractRawUtilization(headers)
		newLimits = t.computeNewLimitsFromHeaders(headers)
	}

	// For errors, always set status to rejected
	newLimits.Status = QuotaRejected

	if !limitsEqual(t.currentLimits, newLimits) {
		t.currentLimits = newLimits
		t.emitStatusChange(newLimits)
	}
}

// extractRawUtilization parses utilization from response headers
func extractRawUtilization(headers http.Header) RawUtilization {
	raw := RawUtilization{}

	// Parse each rate limit window
	if util := parseWindowUtilization(headers, "anthropic-ratelimit-requests-limit",
		"anthropic-ratelimit-requests-remaining", "anthropic-ratelimit-requests-reset"); util != nil {
		raw.FiveHour = util
	}

	if util := parseWindowUtilization(headers, "anthropic-ratelimit-tokens-limit",
		"anthropic-ratelimit-tokens-remaining", "anthropic-ratelimit-tokens-reset"); util != nil {
		raw.SevenDay = util
	}

	// Model-specific limits
	if util := parseWindowUtilization(headers, "anthropic-ratelimit-opus-limit",
		"anthropic-ratelimit-opus-remaining", "anthropic-ratelimit-opus-reset"); util != nil {
		raw.SevenDayOpus = util
	}

	if util := parseWindowUtilization(headers, "anthropic-ratelimit-sonnet-limit",
		"anthropic-ratelimit-sonnet-remaining", "anthropic-ratelimit-sonnet-reset"); util != nil {
		raw.SevenDaySonnet = util
	}

	// Overage limits
	if util := parseWindowUtilization(headers, "anthropic-ratelimit-overage-limit",
		"anthropic-ratelimit-overage-remaining", "anthropic-ratelimit-overage-reset"); util != nil {
		raw.Overage = util
	}

	return raw
}

// parseWindowUtilization extracts utilization for a single window
func parseWindowUtilization(headers http.Header, limitKey, remainingKey, resetKey string) *RawWindowUtilization {
	limitStr := headers.Get(limitKey)
	remainingStr := headers.Get(remainingKey)
	resetStr := headers.Get(resetKey)

	if limitStr == "" || remainingStr == "" || resetStr == "" {
		return nil
	}

	limit, err1 := strconv.ParseFloat(limitStr, 64)
	remaining, err2 := strconv.ParseFloat(remainingStr, 64)
	resetTime, err3 := time.Parse(time.RFC3339, resetStr)

	if err1 != nil || err2 != nil || err3 != nil || limit == 0 {
		return nil
	}

	utilization := (limit - remaining) / limit

	return &RawWindowUtilization{
		Utilization: utilization,
		ResetsAt:    resetTime,
	}
}

// computeNewLimitsFromHeaders calculates new limit state from headers
func (t *Tracker) computeNewLimitsFromHeaders(headers http.Header) ClaudeAILimits {
	limits := ClaudeAILimits{
		Status:                            QuotaAllowed,
		UnifiedRateLimitFallbackAvailable: false,
		IsUsingOverage:                    false,
	}

	// Check for server-provided threshold warning
	surpassedHeader := headers.Get("anthropic-ratelimit-surpassed-threshold")
	if surpassedHeader != "" {
		if threshold, err := strconv.ParseFloat(surpassedHeader, 64); err == nil {
			limits.SurpassedThreshold = &threshold
			limits.Status = QuotaAllowedWarning

			// Extract claim abbreviation to determine limit type
			claimAbbrev := extractClaimAbbrev(headers)
			if rateLimitType, ok := earlyWarningClaimMap[claimAbbrev]; ok {
				limits.RateLimitType = &rateLimitType
			}
		}
	}

	// Fallback: check early warning thresholds
	if limits.Status == QuotaAllowed {
		for _, config := range earlyWarningConfigs {
			var util *RawWindowUtilization
			switch config.RateLimitType {
			case RateLimitFiveHour:
				util = t.rawUtilization.FiveHour
			case RateLimitSevenDay:
				util = t.rawUtilization.SevenDay
			}

			if util == nil {
				continue
			}

			timeProgress := computeTimeProgress(util.ResetsAt, config.WindowSeconds)

			for _, threshold := range config.Thresholds {
				if util.Utilization >= threshold.Utilization && timeProgress <= threshold.TimePct {
					limits.Status = QuotaAllowedWarning
					limits.RateLimitType = &config.RateLimitType
					limits.Utilization = &util.Utilization
					limits.ResetsAt = &util.ResetsAt
					surpassed := threshold.Utilization
					limits.SurpassedThreshold = &surpassed
					break
				}
			}

			if limits.Status == QuotaAllowedWarning {
				break
			}
		}
	}

	// Check overage status
	overageDisabledReason := headers.Get("anthropic-ratelimit-overage-disabled-reason")
	if overageDisabledReason != "" {
		reason := OverageDisabledReason(overageDisabledReason)
		limits.OverageDisabledReason = &reason
	}

	// Check if using overage
	if t.rawUtilization.Overage != nil && t.rawUtilization.Overage.Utilization > 0 {
		limits.IsUsingOverage = true
	}

	return limits
}

// extractClaimAbbrev extracts claim abbreviation from headers
func extractClaimAbbrev(headers http.Header) string {
	// Look for claim in various headers
	if claim := headers.Get("anthropic-ratelimit-claim"); claim != "" {
		return claim
	}
	// Fallback: infer from which headers are present
	if headers.Get("anthropic-ratelimit-requests-limit") != "" {
		return "5h"
	}
	if headers.Get("anthropic-ratelimit-tokens-limit") != "" {
		return "7d"
	}
	return ""
}

// computeTimeProgress calculates what fraction of a time window has elapsed
func computeTimeProgress(resetsAt time.Time, windowSeconds int) float64 {
	now := time.Now()
	windowStart := resetsAt.Add(-time.Duration(windowSeconds) * time.Second)
	elapsed := now.Sub(windowStart).Seconds()
	windowDuration := float64(windowSeconds)
	return math.Max(0, math.Min(1, elapsed/windowDuration))
}

// emitStatusChange notifies all listeners of status change
func (t *Tracker) emitStatusChange(limits ClaudeAILimits) {
	for _, listener := range t.statusListeners {
		listener(limits)
	}
}

// limitsEqual checks if two limit states are equal
func limitsEqual(a, b ClaudeAILimits) bool {
	if a.Status != b.Status || a.IsUsingOverage != b.IsUsingOverage {
		return false
	}
	if !ptrEqual(a.RateLimitType, b.RateLimitType) {
		return false
	}
	if !floatPtrEqual(a.Utilization, b.Utilization) {
		return false
	}
	if !timePtrEqual(a.ResetsAt, b.ResetsAt) {
		return false
	}
	return true
}

func ptrEqual[T comparable](a, b *T) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

func floatPtrEqual(a, b *float64) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return math.Abs(*a-*b) < 0.0001
}

func timePtrEqual(a, b *time.Time) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return a.Equal(*b)
}

// GetRateLimitDisplayName returns human-readable name for rate limit type
func GetRateLimitDisplayName(rateLimitType RateLimitType) string {
	if name, ok := rateLimitDisplayNames[rateLimitType]; ok {
		return name
	}
	return string(rateLimitType)
}

// GetRateLimitErrorMessage generates user-friendly error message
func GetRateLimitErrorMessage(limits ClaudeAILimits) string {
	if limits.RateLimitType == nil {
		return "Rate limit exceeded. Please try again later."
	}

	limitName := GetRateLimitDisplayName(*limits.RateLimitType)
	msg := "You've reached your " + limitName

	if limits.ResetsAt != nil {
		duration := time.Until(*limits.ResetsAt)
		if duration > 0 {
			msg += ". Resets in " + formatDuration(duration)
		}
	}

	return msg + "."
}

// GetRateLimitWarning generates warning message for approaching limits
func GetRateLimitWarning(limits ClaudeAILimits) string {
	if limits.Status != QuotaAllowedWarning || limits.RateLimitType == nil {
		return ""
	}

	limitName := GetRateLimitDisplayName(*limits.RateLimitType)
	utilizationPct := 0
	if limits.Utilization != nil {
		utilizationPct = int(*limits.Utilization * 100)
	}

	msg := "Warning: You've used " + strconv.Itoa(utilizationPct) + "% of your " + limitName

	if limits.ResetsAt != nil {
		duration := time.Until(*limits.ResetsAt)
		if duration > 0 {
			msg += ". Resets in " + formatDuration(duration)
		}
	}

	return msg + "."
}

// formatDuration formats a duration in human-readable form
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return "less than a minute"
	}
	if d < time.Hour {
		minutes := int(d.Minutes())
		if minutes == 1 {
			return "1 minute"
		}
		return strconv.Itoa(minutes) + " minutes"
	}
	if d < 24*time.Hour {
		hours := int(d.Hours())
		if hours == 1 {
			return "1 hour"
		}
		return strconv.Itoa(hours) + " hours"
	}
	days := int(d.Hours() / 24)
	if days == 1 {
		return "1 day"
	}
	return strconv.Itoa(days) + " days"
}
