package hooks

import (
	"fmt"
	"os"

	"claude-codex/internal/backend/services/cost"
)

// CostSummaryHook handles cost summary display on exit.
type CostSummaryHook struct {
	tracker         *cost.Tracker
	hasBillingAccess bool
}

// NewCostSummaryHook creates a new cost summary hook.
func NewCostSummaryHook(tracker *cost.Tracker, hasBillingAccess bool) *CostSummaryHook {
	return &CostSummaryHook{
		tracker:         tracker,
		hasBillingAccess: hasBillingAccess,
	}
}

// OnExit displays cost summary when the application exits.
func (h *CostSummaryHook) OnExit() {
	if !h.hasBillingAccess {
		return
	}

	summary := h.FormatTotalCost()
	if summary != "" {
		fmt.Fprintf(os.Stdout, "\n%s\n", summary)
	}
}

// FormatTotalCost formats the total cost for display.
func (h *CostSummaryHook) FormatTotalCost() string {
	totalCost := h.tracker.GetTotalCostUSD()
	if totalCost == 0 {
		return ""
	}

	inputTokens := h.tracker.GetTotalInputTokens()
	outputTokens := h.tracker.GetTotalOutputTokens()
	cacheRead := h.tracker.GetTotalCacheReadInputTokens()
	cacheCreation := h.tracker.GetTotalCacheCreationInputTokens()

	summary := fmt.Sprintf("Total cost: $%.4f", totalCost)

	if inputTokens > 0 || outputTokens > 0 {
		summary += fmt.Sprintf(" (Input: %s, Output: %s",
			formatTokenCount(inputTokens),
			formatTokenCount(outputTokens))

		if cacheRead > 0 {
			summary += fmt.Sprintf(", Cache read: %s", formatTokenCount(cacheRead))
		}
		if cacheCreation > 0 {
			summary += fmt.Sprintf(", Cache write: %s", formatTokenCount(cacheCreation))
		}
		summary += ")"
	}

	if h.tracker.HasUnknownModelCost() {
		summary += " (includes unknown model costs)"
	}

	return summary
}

// formatTokenCount formats token count with K/M suffixes.
func formatTokenCount(count int) string {
	if count >= 1000000 {
		return fmt.Sprintf("%.1fM", float64(count)/1000000)
	}
	if count >= 1000 {
		return fmt.Sprintf("%.1fK", float64(count)/1000)
	}
	return fmt.Sprintf("%d", count)
}
