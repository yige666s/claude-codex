package cli

import (
	"fmt"
	"time"

	"github.com/ding/claude-code/claude-go/internal/public/ratelimit"
	"github.com/ding/claude-code/claude-go/internal/harness/anthropic"
)

func handleLimits(args []string, sc slashContext) error {
	// Create engine to get access to the provider
	eng, err := sc.newEngineForDir(sc.defaultWorkDir)
	if err != nil {
		fmt.Fprintln(sc.streams.Out, "Unable to access API client:", err)
		return nil
	}

	// Try to get the Anthropic client from the engine's planner
	// This is a workaround since we don't have direct access to the provider
	// In a real implementation, we'd need to refactor to pass the provider through
	fmt.Fprintln(sc.streams.Out, "Rate Limit Status")
	fmt.Fprintln(sc.streams.Out, "==================")
	fmt.Fprintln(sc.streams.Out)
	fmt.Fprintln(sc.streams.Out, "Rate limit tracking is integrated into the API client.")
	fmt.Fprintln(sc.streams.Out, "Limits are automatically tracked on each API call.")
	fmt.Fprintln(sc.streams.Out)
	fmt.Fprintln(sc.streams.Out, "To see rate limit warnings, make an API call and check for warnings in the response.")

	// TODO: Refactor to properly expose the rate limiter through the engine/provider chain
	_ = eng

	return nil
}

func formatTimeUntil(t time.Time) string {
	d := time.Until(t)
	if d < 0 {
		return "now"
	}
	if d < time.Minute {
		return "< 1 minute"
	}
	if d < time.Hour {
		mins := int(d.Minutes())
		return fmt.Sprintf("in %d min", mins)
	}
	if d < 24*time.Hour {
		hours := int(d.Hours())
		return fmt.Sprintf("in %d hr", hours)
	}
	days := int(d.Hours() / 24)
	return fmt.Sprintf("in %d days", days)
}

// Helper function to display rate limit info (will be used when we have proper access)
func displayRateLimitInfo(out interface{ Write([]byte) (int, error) }, limiter *ratelimit.Tracker) {
	limits := limiter.GetCurrentLimits()
	raw := limiter.GetRawUtilization()

	fmt.Fprintln(out, "Rate Limit Status")
	fmt.Fprintln(out, "==================")
	fmt.Fprintln(out)

	// Overall status
	statusStr := string(limits.Status)
	switch limits.Status {
	case ratelimit.QuotaAllowed:
		statusStr = "✓ OK"
	case ratelimit.QuotaAllowedWarning:
		statusStr = "⚠ Warning"
	case ratelimit.QuotaRejected:
		statusStr = "✗ Exceeded"
	}
	fmt.Fprintf(out, "Status: %s\n", statusStr)
	fmt.Fprintln(out)

	// Show warnings if any
	if limits.Status == ratelimit.QuotaAllowedWarning {
		warning := ratelimit.GetRateLimitWarning(limits)
		if warning != "" {
			fmt.Fprintf(out, "⚠ %s\n\n", warning)
		}
	}

	// Show errors if rejected
	if limits.Status == ratelimit.QuotaRejected {
		errMsg := ratelimit.GetRateLimitErrorMessage(limits)
		if errMsg != "" {
			fmt.Fprintf(out, "✗ %s\n\n", errMsg)
		}
	}

	// Show detailed utilization
	fmt.Fprintln(out, "Utilization by Window:")
	fmt.Fprintln(out, "----------------------")

	if raw.FiveHour != nil {
		pct := int(raw.FiveHour.Utilization * 100)
		fmt.Fprintf(out, "5-Hour Window:  %3d%% used (resets %s)\n",
			pct, formatTimeUntil(raw.FiveHour.ResetsAt))
	} else {
		fmt.Fprintln(out, "5-Hour Window:  No data")
	}

	if raw.SevenDay != nil {
		pct := int(raw.SevenDay.Utilization * 100)
		fmt.Fprintf(out, "7-Day Window:   %3d%% used (resets %s)\n",
			pct, formatTimeUntil(raw.SevenDay.ResetsAt))
	} else {
		fmt.Fprintln(out, "7-Day Window:   No data")
	}

	if raw.SevenDayOpus != nil {
		pct := int(raw.SevenDayOpus.Utilization * 100)
		fmt.Fprintf(out, "Opus Limit:     %3d%% used (resets %s)\n",
			pct, formatTimeUntil(raw.SevenDayOpus.ResetsAt))
	}

	if raw.SevenDaySonnet != nil {
		pct := int(raw.SevenDaySonnet.Utilization * 100)
		fmt.Fprintf(out, "Sonnet Limit:   %3d%% used (resets %s)\n",
			pct, formatTimeUntil(raw.SevenDaySonnet.ResetsAt))
	}

	// Overage status
	fmt.Fprintln(out)
	if limits.IsUsingOverage {
		fmt.Fprintln(out, "Extra Usage: Active")
		if raw.Overage != nil {
			pct := int(raw.Overage.Utilization * 100)
			fmt.Fprintf(out, "  %3d%% of extra usage consumed\n", pct)
		}
	} else if limits.OverageDisabledReason != nil {
		fmt.Fprintf(out, "Extra Usage: Not available (%s)\n", *limits.OverageDisabledReason)
	} else {
		fmt.Fprintln(out, "Extra Usage: Available")
	}
}

// Unused but kept for future use
var _ = (*anthropic.Client)(nil)

