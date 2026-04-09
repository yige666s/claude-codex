package budget

import "time"

// Tool result size limits
const (
	// DefaultMaxResultSizeChars is the default maximum size in characters for tool results
	// before they get persisted to disk. Individual tools may declare a lower limit.
	DefaultMaxResultSizeChars = 50_000

	// MaxToolResultTokens is the maximum size for tool results in tokens.
	// This is approximately 400KB of text (assuming ~4 bytes per token).
	MaxToolResultTokens = 100_000

	// BytesPerToken is the conservative estimate for calculating token count from byte size.
	BytesPerToken = 4

	// MaxToolResultBytes is the maximum size for tool results in bytes.
	MaxToolResultBytes = MaxToolResultTokens * BytesPerToken

	// MaxToolResultsPerMessageChars is the default maximum aggregate size in characters
	// for tool_result blocks within a SINGLE user message (one turn's batch of parallel
	// tool results). When a message's blocks together exceed this, the largest blocks
	// are persisted to disk and replaced with previews until under budget.
	MaxToolResultsPerMessageChars = 200_000

	// ToolSummaryMaxLength is the maximum character length for tool summary strings.
	ToolSummaryMaxLength = 50
)

// Token budget tracking
const (
	// CompletionThreshold is the percentage of budget at which to stop (90%)
	CompletionThreshold = 0.9

	// DiminishingThreshold is the minimum token delta to consider progress
	DiminishingThreshold = 500
)

// BudgetTracker tracks token budget usage across continuations.
type BudgetTracker struct {
	ContinuationCount    int
	LastDeltaTokens      int
	LastGlobalTurnTokens int
	StartedAt            time.Time
}

// NewBudgetTracker creates a new budget tracker.
func NewBudgetTracker() *BudgetTracker {
	return &BudgetTracker{
		ContinuationCount:    0,
		LastDeltaTokens:      0,
		LastGlobalTurnTokens: 0,
		StartedAt:            time.Now(),
	}
}

// ContinueDecision represents a decision to continue with a nudge message.
type ContinueDecision struct {
	Action            string
	NudgeMessage      string
	ContinuationCount int
	Pct               int
	TurnTokens        int
	Budget            int
}

// CompletionEvent represents the completion event data.
type CompletionEvent struct {
	ContinuationCount  int
	Pct                int
	TurnTokens         int
	Budget             int
	DiminishingReturns bool
	DurationMs         int64
}

// StopDecision represents a decision to stop.
type StopDecision struct {
	Action          string
	CompletionEvent *CompletionEvent
}

// TokenBudgetDecision is either a continue or stop decision.
type TokenBudgetDecision interface {
	GetAction() string
}

func (c *ContinueDecision) GetAction() string { return c.Action }
func (s *StopDecision) GetAction() string     { return s.Action }
