package compact

// Compaction constants
const (
	// Reserve tokens for output during compaction
	// Based on p99.99 of compact summary output being 17,387 tokens
	MaxOutputTokensForSummary = 20_000

	// Buffer tokens for different compaction triggers
	AutoCompactBufferTokens       = 13_000
	WarningThresholdBufferTokens  = 20_000
	ErrorThresholdBufferTokens    = 20_000
	ManualCompactBufferTokens     = 3_000

	// Stop trying autocompact after this many consecutive failures
	MaxConsecutiveAutoCompactFailures = 3

	// Post-compact restoration limits
	PostCompactMaxFilesToRestore      = 5
	PostCompactTokenBudget            = 50_000
	PostCompactMaxTokensPerFile       = 5_000
	PostCompactMaxTokensPerSkill      = 5_000
	PostCompactSkillsTokenBudget      = 25_000

	// Microcompact constants
	ImageMaxTokenSize              = 2000
	TimeBasedMCClearedMessage      = "[Old tool result content cleared]"

	// Max retries for compact streaming
	MaxCompactStreamingRetries = 2
)

// CompactionResult represents the result of a compaction operation
type CompactionResult struct {
	Success              bool
	CompactedMessages    int
	TokensFreed          int
	NewBoundaryMessageID string
	Error                error
}

// AutoCompactTrackingState tracks auto-compaction state
type AutoCompactTrackingState struct {
	Compacted            bool
	TurnCounter          int
	TurnID               string
	ConsecutiveFailures  int
}

// RecompactionInfo contains information about recompaction
type RecompactionInfo struct {
	IsRecompaction       bool
	PreviousBoundaryID   string
}

// TokenWarningState represents token usage warning state
type TokenWarningState struct {
	PercentLeft                  int
	IsAboveWarningThreshold      bool
	IsAboveErrorThreshold        bool
	IsAboveAutoCompactThreshold  bool
	IsAtBlockingLimit            bool
}

// CompactableTools are tools that can be compacted
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
