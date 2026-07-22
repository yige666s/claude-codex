package compact

import (
	"context"
	"fmt"

	"claude-codex/internal/public/types"
)

// AutoCompactor manages automatic compaction based on context window usage.
type AutoCompactor struct {
	config        *AutoCompactConfig
	trackingState *AutoCompactTrackingState
}

// AutoCompactConfig configures auto-compaction behavior.
type AutoCompactConfig struct {
	// Enabled controls whether auto-compaction is active
	Enabled bool

	// Model being used (affects context window size)
	Model string

	// ContextWindowSize is the total context window
	ContextWindowSize int

	// CurrentTokenUsage is the current token count
	CurrentTokenUsage int

	// MaxConsecutiveFailures stops retrying after this many failures
	MaxConsecutiveFailures int
}

// NewAutoCompactor creates a new auto-compactor.
func NewAutoCompactor(config *AutoCompactConfig) *AutoCompactor {
	if config == nil {
		config = &AutoCompactConfig{
			Enabled:                true,
			Model:                  "claude-sonnet-4-6",
			ContextWindowSize:      200000,
			MaxConsecutiveFailures: 3,
		}
	}

	return &AutoCompactor{
		config: config,
		trackingState: &AutoCompactTrackingState{
			Compacted:           false,
			TurnCounter:         0,
			TurnID:              "",
			ConsecutiveFailures: 0,
		},
	}
}

// ShouldTriggerAutoCompact checks if auto-compaction should be triggered.
func (ac *AutoCompactor) ShouldTriggerAutoCompact() bool {
	if !ac.config.Enabled {
		return false
	}

	// Check if we've hit the failure limit
	if ac.trackingState.ConsecutiveFailures >= ac.config.MaxConsecutiveFailures {
		return false
	}

	// Calculate thresholds
	windowConfig := GetContextWindowConfig(ac.config.Model, ac.config.ContextWindowSize)

	// Trigger if we're above the auto-compact threshold
	return ac.config.CurrentTokenUsage >= windowConfig.AutoCompactThreshold
}

// CalculateTokenWarningState calculates the current warning state.
func (ac *AutoCompactor) CalculateTokenWarningState() *TokenWarningState {
	windowConfig := GetContextWindowConfig(ac.config.Model, ac.config.ContextWindowSize)

	threshold := windowConfig.AutoCompactThreshold
	if !ac.config.Enabled {
		threshold = windowConfig.EffectiveSize
	}

	percentLeft := 0
	if threshold > 0 {
		percentLeft = max(0, (threshold-ac.config.CurrentTokenUsage)*100/threshold)
	}

	return &TokenWarningState{
		PercentLeft:                 percentLeft,
		IsAboveWarningThreshold:     ac.config.CurrentTokenUsage >= windowConfig.WarningThreshold,
		IsAboveErrorThreshold:       ac.config.CurrentTokenUsage >= windowConfig.ErrorThreshold,
		IsAboveAutoCompactThreshold: ac.config.Enabled && ac.config.CurrentTokenUsage >= windowConfig.AutoCompactThreshold,
		IsAtBlockingLimit:           ac.config.CurrentTokenUsage >= windowConfig.BlockingLimit,
	}
}

// TokenWarningState represents the current token usage state.
type TokenWarningState struct {
	PercentLeft                 int
	IsAboveWarningThreshold     bool
	IsAboveErrorThreshold       bool
	IsAboveAutoCompactThreshold bool
	IsAtBlockingLimit           bool
}

// RecordCompactionSuccess records a successful compaction.
func (ac *AutoCompactor) RecordCompactionSuccess() {
	ac.trackingState.Compacted = true
	ac.trackingState.ConsecutiveFailures = 0
}

// RecordCompactionFailure records a failed compaction.
func (ac *AutoCompactor) RecordCompactionFailure() {
	ac.trackingState.ConsecutiveFailures++
}

// IncrementTurn increments the turn counter.
func (ac *AutoCompactor) IncrementTurn(turnID string) {
	ac.trackingState.TurnCounter++
	ac.trackingState.TurnID = turnID
	ac.trackingState.Compacted = false
}

// GetTrackingState returns the current tracking state.
func (ac *AutoCompactor) GetTrackingState() *AutoCompactTrackingState {
	return ac.trackingState
}

// UpdateTokenUsage updates the current token usage.
func (ac *AutoCompactor) UpdateTokenUsage(tokens int) {
	ac.config.CurrentTokenUsage = tokens
}

// CompactMessages performs auto-compaction on messages.
// This is a simplified version - full implementation would call the API to summarize.
func (ac *AutoCompactor) CompactMessages(
	ctx context.Context,
	messages []types.Message,
) (*CompactionResult, error) {
	if !ac.ShouldTriggerAutoCompact() {
		return &CompactionResult{
			Messages:       messages,
			CompactedCount: 0,
			TokensSaved:    0,
		}, nil
	}

	working := messages
	totalSaved := 0
	compactedCount := 0
	threshold := ac.GetAutoCompactThreshold()

	// First try microcompact. A partial reduction is not a completed compact if
	// the resulting context is still above the auto-compact threshold.
	microResult := MicrocompactMessages(working, nil)
	if microResult.TokensSaved > 0 {
		working = microResult.Messages
		totalSaved += microResult.TokensSaved
		compactedCount += len(microResult.DeletedToolIDs)
		if ac.config.CurrentTokenUsage-totalSaved < threshold {
			ac.RecordCompactionSuccess()
			return &CompactionResult{Messages: working, CompactedCount: compactedCount, TokensSaved: totalSaved}, nil
		}
	}

	// If microcompact didn't help enough, try snip
	snipConfig := DefaultSnipConfig()
	snipResult, stats := SnipMessagesWithStats(working, snipConfig)
	if stats.EstimatedTokensSaved > 0 {
		working = snipResult
		totalSaved += stats.EstimatedTokensSaved
		compactedCount += stats.ToolResultsSnipped
		if ac.config.CurrentTokenUsage-totalSaved < threshold {
			ac.RecordCompactionSuccess()
			return &CompactionResult{Messages: working, CompactedCount: compactedCount, TokensSaved: totalSaved}, nil
		}
	}

	// If neither helped, we need full compaction (summarization)
	// For now, just return an error indicating we need full compaction
	ac.RecordCompactionFailure()
	return nil, fmt.Errorf("context window full, need full compaction (summarization)")
}

// GetEffectiveContextWindowSize returns the effective context window size.
func (ac *AutoCompactor) GetEffectiveContextWindowSize() int {
	windowConfig := GetContextWindowConfig(ac.config.Model, ac.config.ContextWindowSize)
	return windowConfig.EffectiveSize
}

// GetAutoCompactThreshold returns the auto-compact threshold.
func (ac *AutoCompactor) GetAutoCompactThreshold() int {
	windowConfig := GetContextWindowConfig(ac.config.Model, ac.config.ContextWindowSize)
	return windowConfig.AutoCompactThreshold
}

// IsEnabled returns whether auto-compaction is enabled.
func (ac *AutoCompactor) IsEnabled() bool {
	return ac.config.Enabled
}

// SetEnabled enables or disables auto-compaction.
func (ac *AutoCompactor) SetEnabled(enabled bool) {
	ac.config.Enabled = enabled
}

// Reset resets the tracking state.
func (ac *AutoCompactor) Reset() {
	ac.trackingState = &AutoCompactTrackingState{
		Compacted:           false,
		TurnCounter:         0,
		TurnID:              "",
		ConsecutiveFailures: 0,
	}
}

// FormatWarningMessage formats a warning message for the user.
func (ac *AutoCompactor) FormatWarningMessage(state *TokenWarningState) string {
	if state.IsAtBlockingLimit {
		return fmt.Sprintf("⛔ Context window full (%d%% remaining). Cannot continue without compaction.", state.PercentLeft)
	}

	if state.IsAboveErrorThreshold {
		return fmt.Sprintf("🔴 Context window nearly full (%d%% remaining). Compaction recommended.", state.PercentLeft)
	}

	if state.IsAboveWarningThreshold {
		return fmt.Sprintf("⚠️  Context window filling up (%d%% remaining).", state.PercentLeft)
	}

	if state.IsAboveAutoCompactThreshold {
		return fmt.Sprintf("ℹ️  Auto-compaction will trigger soon (%d%% remaining).", state.PercentLeft)
	}

	return ""
}
