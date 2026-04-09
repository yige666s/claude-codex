package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// UsageClient handles API usage and rate limit queries
type UsageClient struct {
	BaseURL    string
	HTTPClient *http.Client
}

// NewUsageClient creates a new usage client
func NewUsageClient(baseURL string) *UsageClient {
	if baseURL == "" {
		baseURL = "https://api.claude.ai"
	}

	return &UsageClient{
		BaseURL: baseURL,
		HTTPClient: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

// FetchUtilization fetches current API utilization and rate limits
func (c *UsageClient) FetchUtilization(ctx context.Context, authToken string) (*Utilization, error) {
	if authToken == "" {
		return nil, fmt.Errorf("auth token required")
	}

	url := fmt.Sprintf("%s/api/oauth/usage", c.BaseURL)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", authToken))
	req.Header.Set("User-Agent", getUserAgent())

	// Execute request
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch utilization: %w", err)
	}
	defer resp.Body.Close()

	// Check status code
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}

	// Parse response
	var utilization Utilization
	if err := json.NewDecoder(resp.Body).Decode(&utilization); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &utilization, nil
}

// IsRateLimitApproaching checks if any rate limit is approaching threshold
func (u *Utilization) IsRateLimitApproaching(threshold float64) bool {
	if threshold <= 0 || threshold > 100 {
		threshold = 80.0 // Default to 80%
	}

	checkLimit := func(limit *RateLimit) bool {
		if limit == nil || limit.Utilization == nil {
			return false
		}
		return *limit.Utilization >= threshold
	}

	return checkLimit(u.FiveHour) ||
		checkLimit(u.SevenDay) ||
		checkLimit(u.SevenDayOAuthApps) ||
		checkLimit(u.SevenDayOpus) ||
		checkLimit(u.SevenDaySonnet)
}

// GetHighestUtilization returns the highest utilization percentage
func (u *Utilization) GetHighestUtilization() float64 {
	highest := 0.0

	checkLimit := func(limit *RateLimit) {
		if limit != nil && limit.Utilization != nil && *limit.Utilization > highest {
			highest = *limit.Utilization
		}
	}

	checkLimit(u.FiveHour)
	checkLimit(u.SevenDay)
	checkLimit(u.SevenDayOAuthApps)
	checkLimit(u.SevenDayOpus)
	checkLimit(u.SevenDaySonnet)

	return highest
}

// GetNextResetTime returns the earliest rate limit reset time
func (u *Utilization) GetNextResetTime() *time.Time {
	var earliest *time.Time

	checkLimit := func(limit *RateLimit) {
		if limit != nil && limit.ResetsAt != nil {
			if earliest == nil || limit.ResetsAt.Before(*earliest) {
				earliest = limit.ResetsAt
			}
		}
	}

	checkLimit(u.FiveHour)
	checkLimit(u.SevenDay)
	checkLimit(u.SevenDayOAuthApps)
	checkLimit(u.SevenDayOpus)
	checkLimit(u.SevenDaySonnet)

	return earliest
}

// FormatUtilization formats utilization info for display
func (u *Utilization) FormatUtilization() string {
	if u == nil {
		return "No utilization data available"
	}

	result := "API Utilization:\n"

	formatLimit := func(name string, limit *RateLimit) string {
		if limit == nil || limit.Utilization == nil {
			return ""
		}

		line := fmt.Sprintf("  %s: %.1f%%", name, *limit.Utilization)
		if limit.ResetsAt != nil {
			line += fmt.Sprintf(" (resets at %s)", limit.ResetsAt.Format(time.RFC3339))
		}
		return line + "\n"
	}

	result += formatLimit("5-hour", u.FiveHour)
	result += formatLimit("7-day", u.SevenDay)
	result += formatLimit("7-day OAuth apps", u.SevenDayOAuthApps)
	result += formatLimit("7-day Opus", u.SevenDayOpus)
	result += formatLimit("7-day Sonnet", u.SevenDaySonnet)

	if u.ExtraUsage != nil && u.ExtraUsage.IsEnabled {
		result += fmt.Sprintf("\nExtra Usage:\n")
		if u.ExtraUsage.MonthlyLimit != nil {
			result += fmt.Sprintf("  Monthly limit: %d\n", *u.ExtraUsage.MonthlyLimit)
		}
		if u.ExtraUsage.UsedCredits != nil {
			result += fmt.Sprintf("  Used credits: %d\n", *u.ExtraUsage.UsedCredits)
		}
		if u.ExtraUsage.Utilization != nil {
			result += fmt.Sprintf("  Utilization: %.1f%%\n", *u.ExtraUsage.Utilization)
		}
	}

	return result
}

// getUserAgent returns the user agent string
func getUserAgent() string {
	version := os.Getenv("CLAUDE_CODE_VERSION")
	if version == "" {
		version = "dev"
	}
	return fmt.Sprintf("claude-code-go/%s", version)
}

// ShouldWarnAboutRateLimit checks if user should be warned about rate limits
func ShouldWarnAboutRateLimit(utilization *Utilization) bool {
	if utilization == nil {
		return false
	}

	// Warn if any limit is above 80%
	return utilization.IsRateLimitApproaching(80.0)
}

// GetRateLimitWarningMessage generates a warning message for rate limits
func GetRateLimitWarningMessage(utilization *Utilization) string {
	if utilization == nil || !ShouldWarnAboutRateLimit(utilization) {
		return ""
	}

	highest := utilization.GetHighestUtilization()
	resetTime := utilization.GetNextResetTime()

	msg := fmt.Sprintf("⚠️  API usage is at %.1f%%", highest)

	if resetTime != nil {
		duration := time.Until(*resetTime)
		if duration > 0 {
			msg += fmt.Sprintf(" (resets in %s)", formatDuration(duration))
		}
	}

	return msg
}

// formatDuration formats a duration in human-readable form
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	hours := int(d.Hours())
	minutes := int(d.Minutes()) % 60
	if minutes > 0 {
		return fmt.Sprintf("%dh %dm", hours, minutes)
	}
	return fmt.Sprintf("%dh", hours)
}
