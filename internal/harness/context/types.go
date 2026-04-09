package context

import "time"

// SystemContext contains system-level context information
type SystemContext struct {
	GitStatus    string
	CacheBreaker string
}

// UserContext contains user-level context information
type UserContext struct {
	ClaudeMd    string
	CurrentDate string
}

// GitStatusInfo contains detailed git repository information
type GitStatusInfo struct {
	CurrentBranch string
	MainBranch    string
	GitUser       string
	Status        string
	RecentCommits string
	Timestamp     time.Time
}

// ContextWindowConfig contains context window configuration
type ContextWindowConfig struct {
	ModelName          string
	ContextWindow      int
	MaxOutputTokens    int
	MaxOutputLimit     int
	Supports1M         bool
	UsedPercentage     int
	RemainingPercentage int
}

// TokenUsage represents token usage statistics
type TokenUsage struct {
	InputTokens              int
	CacheCreationInputTokens int
	CacheReadInputTokens     int
	OutputTokens             int
}
