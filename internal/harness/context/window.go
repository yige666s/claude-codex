package context

import (
	"fmt"
	"os"
	"strings"
)

const (
	// Default context window size (200k tokens)
	ModelContextWindowDefault = 200_000

	// Maximum output tokens
	MaxOutputTokensDefault    = 32_000
	MaxOutputTokensUpperLimit = 64_000

	// Capped default for slot-reservation optimization
	CappedDefaultMaxTokens = 8_000
	EscalatedMaxTokens     = 64_000

	// Compact operation max output tokens
	CompactMaxOutputTokens = 20_000
)

// ModelMaxOutputTokens contains default and upper limit for max output tokens
type ModelMaxOutputTokens struct {
	Default    int
	UpperLimit int
}

// Is1MContextDisabled checks if 1M context is disabled via environment variable
func Is1MContextDisabled() bool {
	return os.Getenv("CLAUDE_CODE_DISABLE_1M_CONTEXT") == "1" ||
		os.Getenv("CLAUDE_CODE_DISABLE_1M_CONTEXT") == "true"
}

// Has1MContext checks if model name explicitly includes [1m] suffix
func Has1MContext(model string) bool {
	if Is1MContextDisabled() {
		return false
	}
	return strings.Contains(strings.ToLower(model), "[1m]")
}

// ModelSupports1M checks if model supports 1M context window
func ModelSupports1M(model string) bool {
	if Is1MContextDisabled() {
		return false
	}
	canonical := getCanonicalName(model)
	return strings.Contains(canonical, "claude-sonnet-4") ||
		strings.Contains(canonical, "opus-4-6")
}

// GetContextWindowForModel returns the context window size for a model
func GetContextWindowForModel(model string) int {
	// Allow override via environment variable
	if override := os.Getenv("CLAUDE_CODE_MAX_CONTEXT_TOKENS"); override != "" {
		var size int
		if _, err := fmt.Sscanf(override, "%d", &size); err == nil && size > 0 {
			return size
		}
	}

	// [1m] suffix - explicit client-side opt-in
	if Has1MContext(model) {
		return 1_000_000
	}

	// Check if model supports 1M
	if ModelSupports1M(model) {
		return 1_000_000
	}

	return ModelContextWindowDefault
}

// GetModelMaxOutputTokens returns the default and upper limit for max output tokens
func GetModelMaxOutputTokens(model string) ModelMaxOutputTokens {
	canonical := getCanonicalName(model)

	var defaultTokens, upperLimit int

	switch {
	case strings.Contains(canonical, "opus-4-6"):
		defaultTokens = 64_000
		upperLimit = 128_000
	case strings.Contains(canonical, "sonnet-4-6"):
		defaultTokens = 32_000
		upperLimit = 128_000
	case strings.Contains(canonical, "opus-4-5") ||
		strings.Contains(canonical, "sonnet-4") ||
		strings.Contains(canonical, "haiku-4"):
		defaultTokens = 32_000
		upperLimit = 64_000
	case strings.Contains(canonical, "opus-4-1") || strings.Contains(canonical, "opus-4"):
		defaultTokens = 32_000
		upperLimit = 32_000
	case strings.Contains(canonical, "claude-3-opus"):
		defaultTokens = 4_096
		upperLimit = 4_096
	case strings.Contains(canonical, "claude-3-sonnet"):
		defaultTokens = 8_192
		upperLimit = 8_192
	case strings.Contains(canonical, "claude-3-haiku"):
		defaultTokens = 4_096
		upperLimit = 4_096
	case strings.Contains(canonical, "3-5-sonnet") || strings.Contains(canonical, "3-5-haiku"):
		defaultTokens = 8_192
		upperLimit = 8_192
	case strings.Contains(canonical, "3-7-sonnet"):
		defaultTokens = 32_000
		upperLimit = 64_000
	default:
		defaultTokens = MaxOutputTokensDefault
		upperLimit = MaxOutputTokensUpperLimit
	}

	return ModelMaxOutputTokens{
		Default:    defaultTokens,
		UpperLimit: upperLimit,
	}
}

// CalculateContextPercentages calculates context window usage percentages
func CalculateContextPercentages(usage *TokenUsage, contextWindowSize int) (used, remaining int) {
	if usage == nil {
		return 0, 100
	}

	totalInputTokens := usage.InputTokens +
		usage.CacheCreationInputTokens +
		usage.CacheReadInputTokens

	usedPercentage := (totalInputTokens * 100) / contextWindowSize

	// Clamp to 0-100 range
	if usedPercentage < 0 {
		usedPercentage = 0
	}
	if usedPercentage > 100 {
		usedPercentage = 100
	}

	return usedPercentage, 100 - usedPercentage
}

// getCanonicalName normalizes model name for comparison
func getCanonicalName(model string) string {
	return strings.ToLower(strings.TrimSpace(model))
}
