package compact

import (
	"claude-codex/internal/public/types"
)

// CompactionResult represents the result of a compaction operation.
type CompactionResult struct {
	Messages       []types.Message
	CompactedCount int
	TokensSaved    int
	Error          error
}

// CompactionStrategy defines the type of compaction to perform.
type CompactionStrategy string

const (
	// StrategySnip truncates tool results to a maximum size
	StrategySnip CompactionStrategy = "snip"

	// StrategyMicroCompact removes old tool results using cache editing
	StrategyMicroCompact CompactionStrategy = "microcompact"

	// StrategyAutoCompact summarizes conversation when approaching context limit
	StrategyAutoCompact CompactionStrategy = "autocompact"

	// StrategyTimeBased clears old tool results based on time threshold
	StrategyTimeBased CompactionStrategy = "timebased"
)

// CompactionOptions configures compaction behavior.
type CompactionOptions struct {
	// Strategy to use for compaction
	Strategy CompactionStrategy

	// MaxToolResultSize for snip compaction (in bytes)
	MaxToolResultSize int

	// TokenThreshold for auto compaction
	TokenThreshold int

	// TimeThresholdMinutes for time-based compaction
	TimeThresholdMinutes int

	// Model being used (affects context window calculations)
	Model string

	// QuerySource identifies the query context
	QuerySource string
}

// SnipConfig configures snip compaction behavior.
type SnipConfig struct {
	// MaxSize is the maximum size for tool results in bytes
	MaxSize int

	// TruncateMessage is appended when content is truncated
	TruncateMessage string

	// PreservePrefix preserves this many bytes at the start
	PreservePrefix int

	// PreserveSuffix preserves this many bytes at the end
	PreserveSuffix int
}

// DefaultSnipConfig returns the default snip configuration.
func DefaultSnipConfig() *SnipConfig {
	return &SnipConfig{
		MaxSize:         50000, // 50KB default
		TruncateMessage: "\n\n[Output truncated due to length. Use offset/limit parameters to read specific sections.]\n",
		PreservePrefix:  10000, // Keep first 10KB
		PreserveSuffix:  5000,  // Keep last 5KB
	}
}

// CompactableTools lists tools whose results can be compacted.
var CompactableTools = map[string]bool{
	"Read":      true,
	"Bash":      true,
	"Grep":      true,
	"Glob":      true,
	"WebSearch": true,
	"WebFetch":  true,
	"Edit":      true,
	"Write":     true,
}

// IsCompactable checks if a tool's results can be compacted.
func IsCompactable(toolName string) bool {
	return CompactableTools[toolName]
}

// TimeBasedMCConfig configures time-based microcompaction.
type TimeBasedMCConfig struct {
	// Enabled indicates if time-based MC is active
	Enabled bool

	// ThresholdMinutes is the time gap that triggers clearing
	ThresholdMinutes int

	// ClearMessage is the replacement text for cleared content
	ClearMessage string
}

// DefaultTimeBasedMCConfig returns the default time-based MC configuration.
func DefaultTimeBasedMCConfig() *TimeBasedMCConfig {
	return &TimeBasedMCConfig{
		Enabled:          true,
		ThresholdMinutes: 5, // 5 minutes default
		ClearMessage:     "[Old tool result content cleared]",
	}
}

// MicrocompactResult represents the result of microcompaction.
type MicrocompactResult struct {
	Messages       []types.Message
	DeletedToolIDs []string
	TokensSaved    int
}

// AutoCompactTrackingState tracks auto-compaction state across turns.
type AutoCompactTrackingState struct {
	Compacted            bool
	TurnCounter          int
	TurnID               string
	ConsecutiveFailures  int
}

// ContextWindowConfig defines context window limits.
type ContextWindowConfig struct {
	// TotalSize is the total context window size
	TotalSize int

	// MaxOutputTokens is reserved for model output
	MaxOutputTokens int

	// EffectiveSize is TotalSize - MaxOutputTokens
	EffectiveSize int

	// AutoCompactThreshold triggers auto-compaction
	AutoCompactThreshold int

	// WarningThreshold shows warning to user
	WarningThreshold int

	// ErrorThreshold shows error to user
	ErrorThreshold int

	// BlockingLimit prevents new queries
	BlockingLimit int
}

// Buffer sizes for context window calculations
const (
	AutoCompactBufferTokens      = 13000
	WarningThresholdBufferTokens = 20000
	ErrorThresholdBufferTokens   = 20000
	ManualCompactBufferTokens    = 3000
	MaxOutputTokensForSummary    = 20000
)

// GetContextWindowConfig calculates context window configuration for a model.
func GetContextWindowConfig(model string, contextWindowSize int) *ContextWindowConfig {
	maxOutput := min(contextWindowSize/4, MaxOutputTokensForSummary)
	effectiveSize := contextWindowSize - maxOutput

	return &ContextWindowConfig{
		TotalSize:            contextWindowSize,
		MaxOutputTokens:      maxOutput,
		EffectiveSize:        effectiveSize,
		AutoCompactThreshold: effectiveSize - AutoCompactBufferTokens,
		WarningThreshold:     effectiveSize - WarningThresholdBufferTokens,
		ErrorThreshold:       effectiveSize - ErrorThresholdBufferTokens,
		BlockingLimit:        effectiveSize - ManualCompactBufferTokens,
	}
}
