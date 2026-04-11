package tokens

import (
	api "claude-codex/internal/harness/anthropic"
)

// Token counting constants
const (
	// Minimal values for token counting with thinking enabled
	// API constraint: max_tokens must be greater than thinking.budget_tokens
	TokenCountThinkingBudget = 1024
	TokenCountMaxTokens      = 2048

	// Default bytes per token for rough estimation
	DefaultBytesPerToken = 4
)

// TokenCounter provides token counting functionality
type TokenCounter interface {
	// CountTokensWithAPI counts tokens using the API
	CountTokensWithAPI(content string) (int, error)

	// CountMessagesTokensWithAPI counts tokens for messages and tools
	CountMessagesTokensWithAPI(messages []api.InputMessage, tools []api.Tool) (int, error)

	// RoughTokenCountEstimation provides a rough estimate without API call
	RoughTokenCountEstimation(content string) int

	// RoughTokenCountEstimationForMessages estimates tokens for messages
	RoughTokenCountEstimationForMessages(messages []api.InputMessage) int
}

// FileTypeTokenEstimator provides file-type-specific token estimation
type FileTypeTokenEstimator interface {
	// BytesPerTokenForFileType returns bytes per token for a file extension
	BytesPerTokenForFileType(fileExtension string) int

	// RoughTokenCountEstimationForFileType estimates tokens for file content
	RoughTokenCountEstimationForFileType(content string, fileExtension string) int
}
