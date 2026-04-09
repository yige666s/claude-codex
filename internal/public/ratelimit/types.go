package ratelimit

import "time"

// QuotaStatus represents the current quota state
type QuotaStatus string

const (
	QuotaAllowed        QuotaStatus = "allowed"
	QuotaAllowedWarning QuotaStatus = "allowed_warning"
	QuotaRejected       QuotaStatus = "rejected"
)

// RateLimitType represents different rate limit windows
type RateLimitType string

const (
	RateLimitFiveHour      RateLimitType = "five_hour"
	RateLimitSevenDay      RateLimitType = "seven_day"
	RateLimitSevenDayOpus  RateLimitType = "seven_day_opus"
	RateLimitSevenDaySonnet RateLimitType = "seven_day_sonnet"
	RateLimitOverage       RateLimitType = "overage"
)

// OverageDisabledReason explains why overage is not available
type OverageDisabledReason string

const (
	OverageNotProvisioned         OverageDisabledReason = "overage_not_provisioned"
	OverageOrgLevelDisabled       OverageDisabledReason = "org_level_disabled"
	OverageOrgLevelDisabledUntil  OverageDisabledReason = "org_level_disabled_until"
	OverageOutOfCredits           OverageDisabledReason = "out_of_credits"
	OverageSeatTierLevelDisabled  OverageDisabledReason = "seat_tier_level_disabled"
	OverageMemberLevelDisabled    OverageDisabledReason = "member_level_disabled"
	OverageSeatTierZeroCreditLimit OverageDisabledReason = "seat_tier_zero_credit_limit"
	OverageGroupZeroCreditLimit   OverageDisabledReason = "group_zero_credit_limit"
	OverageMemberZeroCreditLimit  OverageDisabledReason = "member_zero_credit_limit"
	OverageOrgServiceLevelDisabled OverageDisabledReason = "org_service_level_disabled"
	OverageOrgServiceZeroCreditLimit OverageDisabledReason = "org_service_zero_credit_limit"
	OverageNoLimitsConfigured     OverageDisabledReason = "no_limits_configured"
	OverageUnknown                OverageDisabledReason = "unknown"
)

// ClaudeAILimits represents the current rate limit state
type ClaudeAILimits struct {
	Status                            QuotaStatus
	UnifiedRateLimitFallbackAvailable bool
	ResetsAt                          *time.Time
	RateLimitType                     *RateLimitType
	Utilization                       *float64
	OverageStatus                     *QuotaStatus
	OverageResetsAt                   *time.Time
	OverageDisabledReason             *OverageDisabledReason
	IsUsingOverage                    bool
	SurpassedThreshold                *float64
}

// RawWindowUtilization tracks per-window usage from headers
type RawWindowUtilization struct {
	Utilization float64
	ResetsAt    time.Time
}

// RawUtilization tracks all rate limit windows
type RawUtilization struct {
	FiveHour      *RawWindowUtilization
	SevenDay      *RawWindowUtilization
	SevenDayOpus  *RawWindowUtilization
	SevenDaySonnet *RawWindowUtilization
	Overage       *RawWindowUtilization
}

// EarlyWarningThreshold defines when to warn users
type EarlyWarningThreshold struct {
	Utilization float64 // 0-1 scale: trigger warning when usage >= this
	TimePct     float64 // 0-1 scale: trigger warning when time elapsed <= this
}

// EarlyWarningConfig defines early warning rules for a rate limit type
type EarlyWarningConfig struct {
	RateLimitType RateLimitType
	ClaimAbbrev   string
	WindowSeconds int
	Thresholds    []EarlyWarningThreshold
}

// StatusListener is called when rate limit status changes
type StatusListener func(limits ClaudeAILimits)
