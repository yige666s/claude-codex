package compact

import (
	"context"
	"os"
	"strconv"

	api "github.com/ding/claude-code/claude-go/internal/harness/anthropic"
)

// GetEffectiveContextWindowSize returns context window minus reserved output tokens
func GetEffectiveContextWindowSize(model string, contextWindow int) int {
	reservedTokensForSummary := min(getMaxOutputTokensForModel(model), MaxOutputTokensForSummary)

	effectiveWindow := contextWindow

	// Allow override for testing
	if autoCompactWindow := os.Getenv("CLAUDE_CODE_AUTO_COMPACT_WINDOW"); autoCompactWindow != "" {
		if parsed, err := strconv.Atoi(autoCompactWindow); err == nil && parsed > 0 {
			effectiveWindow = min(effectiveWindow, parsed)
		}
	}

	return effectiveWindow - reservedTokensForSummary
}

// GetAutoCompactThreshold returns the token threshold for triggering auto-compaction
func GetAutoCompactThreshold(model string, contextWindow int) int {
	effectiveContextWindow := GetEffectiveContextWindowSize(model, contextWindow)
	autocompactThreshold := effectiveContextWindow - AutoCompactBufferTokens

	// Override for easier testing
	if envPercent := os.Getenv("CLAUDE_AUTOCOMPACT_PCT_OVERRIDE"); envPercent != "" {
		if parsed, err := strconv.ParseFloat(envPercent, 64); err == nil && parsed > 0 && parsed <= 100 {
			percentageThreshold := int(float64(effectiveContextWindow) * (parsed / 100))
			return min(percentageThreshold, autocompactThreshold)
		}
	}

	return autocompactThreshold
}

// CalculateTokenWarningState calculates token usage warning thresholds
func CalculateTokenWarningState(tokenUsage int, model string, contextWindow int, autoCompactEnabled bool) TokenWarningState {
	autoCompactThreshold := GetAutoCompactThreshold(model, contextWindow)

	threshold := autoCompactThreshold
	if !autoCompactEnabled {
		threshold = GetEffectiveContextWindowSize(model, contextWindow)
	}

	percentLeft := max(0, int(float64(threshold-tokenUsage)/float64(threshold)*100))

	warningThreshold := threshold - WarningThresholdBufferTokens
	errorThreshold := threshold - ErrorThresholdBufferTokens

	isAboveWarningThreshold := tokenUsage >= warningThreshold
	isAboveErrorThreshold := tokenUsage >= errorThreshold
	isAboveAutoCompactThreshold := autoCompactEnabled && tokenUsage >= autoCompactThreshold

	actualContextWindow := GetEffectiveContextWindowSize(model, contextWindow)
	defaultBlockingLimit := actualContextWindow - ManualCompactBufferTokens

	// Allow override for testing
	blockingLimit := defaultBlockingLimit
	if blockingLimitOverride := os.Getenv("CLAUDE_CODE_BLOCKING_LIMIT_OVERRIDE"); blockingLimitOverride != "" {
		if parsed, err := strconv.Atoi(blockingLimitOverride); err == nil && parsed > 0 {
			blockingLimit = parsed
		}
	}

	isAtBlockingLimit := tokenUsage >= blockingLimit

	return TokenWarningState{
		PercentLeft:                 percentLeft,
		IsAboveWarningThreshold:     isAboveWarningThreshold,
		IsAboveErrorThreshold:       isAboveErrorThreshold,
		IsAboveAutoCompactThreshold: isAboveAutoCompactThreshold,
		IsAtBlockingLimit:           isAtBlockingLimit,
	}
}

// IsAutoCompactEnabled checks if auto-compaction is enabled
func IsAutoCompactEnabled() bool {
	if os.Getenv("DISABLE_COMPACT") == "true" || os.Getenv("DISABLE_COMPACT") == "1" {
		return false
	}
	if os.Getenv("DISABLE_AUTO_COMPACT") == "true" || os.Getenv("DISABLE_AUTO_COMPACT") == "1" {
		return false
	}
	return true
}

// ShouldTriggerAutoCompact checks if auto-compaction should be triggered
func ShouldTriggerAutoCompact(
	tokenUsage int,
	model string,
	contextWindow int,
	tracking *AutoCompactTrackingState,
	turnID string,
) bool {
	if !IsAutoCompactEnabled() {
		return false
	}

	// Circuit breaker: stop trying after consecutive failures
	if tracking != nil && tracking.ConsecutiveFailures >= MaxConsecutiveAutoCompactFailures {
		return false
	}

	// Only trigger once per turn
	if tracking != nil && tracking.Compacted && tracking.TurnID == turnID {
		return false
	}

	threshold := GetAutoCompactThreshold(model, contextWindow)
	return tokenUsage >= threshold
}

// Helper function to get max output tokens for a model
func getMaxOutputTokensForModel(model string) int {
	// Simplified version - in real implementation this would check model capabilities
	switch model {
	case "claude-opus-4-6", "claude-sonnet-4-6":
		return 8192
	case "claude-3-5-sonnet-20241022", "claude-3-5-sonnet-20240620":
		return 8192
	case "claude-3-opus-20240229":
		return 4096
	default:
		return 4096
	}
}

// AutoCompactResult represents the result of an auto-compaction attempt
type AutoCompactResult struct {
	WasCompacted         bool
	CompactionResult     *CompactionResult
	ConsecutiveFailures  int
	PostCompactTokenCount int
}

// TryAutoCompact attempts to perform auto-compaction
func TryAutoCompact(
	ctx context.Context,
	messages []api.InputMessage,
	model string,
	contextWindow int,
	tracking *AutoCompactTrackingState,
	turnID string,
) (*AutoCompactResult, error) {
	if !ShouldTriggerAutoCompact(0, model, contextWindow, tracking, turnID) {
		return &AutoCompactResult{WasCompacted: false}, nil
	}

	// TODO: Implement actual compaction logic
	// This is a placeholder that would call the full compaction flow

	return &AutoCompactResult{
		WasCompacted:        false,
		ConsecutiveFailures: 0,
	}, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
