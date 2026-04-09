package query

import (
	"time"
)

// BudgetTracker tracks token budget across continuations.
func createBudgetTracker() *BudgetTracker {
	return &BudgetTracker{
		ContinuationCount:    0,
		LastDeltaTokens:      0,
		LastGlobalTurnTokens: 0,
		StartedAt:            time.Now().UnixMilli(),
	}
}

const (
	completionThreshold   = 0.9
	diminishingThreshold  = 500
)

// checkTokenBudget determines whether to continue or stop based on token budget.
func checkTokenBudget(
	tracker *BudgetTracker,
	agentID string,
	budget *int,
	globalTurnTokens int,
) TokenBudgetDecision {
	tracker.mu.Lock()
	defer tracker.mu.Unlock()

	// No budget enforcement for subagents or when budget is not set
	if agentID != "" || budget == nil || *budget <= 0 {
		return TokenBudgetDecision{
			Action:          "stop",
			CompletionEvent: nil,
		}
	}

	turnTokens := globalTurnTokens
	pct := int(float64(turnTokens) / float64(*budget) * 100)
	deltaSinceLastCheck := globalTurnTokens - tracker.LastGlobalTurnTokens

	// Check for diminishing returns
	isDiminishing := tracker.ContinuationCount >= 3 &&
		deltaSinceLastCheck < diminishingThreshold &&
		tracker.LastDeltaTokens < diminishingThreshold

	// Continue if not diminishing and under threshold
	if !isDiminishing && turnTokens < int(float64(*budget)*completionThreshold) {
		tracker.ContinuationCount++
		tracker.LastDeltaTokens = deltaSinceLastCheck
		tracker.LastGlobalTurnTokens = globalTurnTokens

		return TokenBudgetDecision{
			Action:            "continue",
			NudgeMessage:      getBudgetContinuationMessage(pct, turnTokens, *budget),
			ContinuationCount: tracker.ContinuationCount,
			Pct:               pct,
			TurnTokens:        turnTokens,
			Budget:            *budget,
		}
	}

	// Stop with completion event if we had continuations
	if isDiminishing || tracker.ContinuationCount > 0 {
		return TokenBudgetDecision{
			Action: "stop",
			CompletionEvent: &CompletionEvent{
				ContinuationCount:  tracker.ContinuationCount,
				Pct:                pct,
				TurnTokens:         turnTokens,
				Budget:             *budget,
				DiminishingReturns: isDiminishing,
				DurationMs:         time.Now().UnixMilli() - tracker.StartedAt,
			},
		}
	}

	return TokenBudgetDecision{
		Action:          "stop",
		CompletionEvent: nil,
	}
}

// getBudgetContinuationMessage generates a nudge message for budget continuation.
func getBudgetContinuationMessage(pct, turnTokens, budget int) string {
	// TODO: Implement proper message generation
	return "Continue with your task."
}
