package budget

import (
	"fmt"
	"time"
)

// CheckTokenBudget checks if the token budget allows continuation.
func CheckTokenBudget(
	tracker *BudgetTracker,
	agentID string,
	budget int,
	globalTurnTokens int,
) TokenBudgetDecision {
	// If this is an agent or no budget set, stop without event
	if agentID != "" || budget <= 0 {
		return &StopDecision{
			Action:          "stop",
			CompletionEvent: nil,
		}
	}

	turnTokens := globalTurnTokens
	pct := int(float64(turnTokens) / float64(budget) * 100)
	deltaSinceLastCheck := globalTurnTokens - tracker.LastGlobalTurnTokens

	// Check for diminishing returns
	isDiminishing := tracker.ContinuationCount >= 3 &&
		deltaSinceLastCheck < DiminishingThreshold &&
		tracker.LastDeltaTokens < DiminishingThreshold

	// Continue if not diminishing and under threshold
	if !isDiminishing && float64(turnTokens) < float64(budget)*CompletionThreshold {
		tracker.ContinuationCount++
		tracker.LastDeltaTokens = deltaSinceLastCheck
		tracker.LastGlobalTurnTokens = globalTurnTokens

		return &ContinueDecision{
			Action:            "continue",
			NudgeMessage:      GetBudgetContinuationMessage(pct, turnTokens, budget),
			ContinuationCount: tracker.ContinuationCount,
			Pct:               pct,
			TurnTokens:        turnTokens,
			Budget:            budget,
		}
	}

	// Stop with completion event if we had continuations
	if isDiminishing || tracker.ContinuationCount > 0 {
		durationMs := time.Since(tracker.StartedAt).Milliseconds()
		return &StopDecision{
			Action: "stop",
			CompletionEvent: &CompletionEvent{
				ContinuationCount:  tracker.ContinuationCount,
				Pct:                pct,
				TurnTokens:         turnTokens,
				Budget:             budget,
				DiminishingReturns: isDiminishing,
				DurationMs:         durationMs,
			},
		}
	}

	// Stop without event
	return &StopDecision{
		Action:          "stop",
		CompletionEvent: nil,
	}
}

// GetBudgetContinuationMessage formats the continuation nudge message.
func GetBudgetContinuationMessage(pct, turnTokens, budget int) string {
	return fmt.Sprintf(
		"Stopped at %d%% of token target (%s / %s). Keep working — do not summarize.",
		pct,
		formatNumber(turnTokens),
		formatNumber(budget),
	)
}

// formatNumber formats a number with thousand separators.
func formatNumber(n int) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	if n < 1000000 {
		return fmt.Sprintf("%d,%03d", n/1000, n%1000)
	}
	millions := n / 1000000
	thousands := (n % 1000000) / 1000
	ones := n % 1000
	return fmt.Sprintf("%d,%03d,%03d", millions, thousands, ones)
}
