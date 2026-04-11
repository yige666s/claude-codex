package tokens

import (
	"encoding/json"
	"strings"

	api "claude-codex/internal/harness/anthropic"
)

// RoughTokenCountEstimation provides a rough estimate of token count
// without making an API call. Uses bytes per token heuristic.
func RoughTokenCountEstimation(content string, bytesPerToken int) int {
	if bytesPerToken <= 0 {
		bytesPerToken = DefaultBytesPerToken
	}
	byteCount := len([]byte(content))
	return byteCount / bytesPerToken
}

// BytesPerTokenForFileType returns the bytes per token ratio for a file type
func BytesPerTokenForFileType(fileExtension string) int {
	ext := strings.ToLower(strings.TrimPrefix(fileExtension, "."))

	switch ext {
	// Dense formats (more tokens per byte)
	case "json", "xml", "yaml", "yml":
		return 3
	// Code with lots of symbols
	case "js", "ts", "jsx", "tsx", "py", "rb", "go", "rs", "java", "c", "cpp", "cs":
		return 3
	// Markup and structured text
	case "html", "css", "scss", "sass", "less":
		return 3
	// Plain text and documentation
	case "txt", "md", "markdown", "rst":
		return 4
	// Default
	default:
		return DefaultBytesPerToken
	}
}

// RoughTokenCountEstimationForFileType estimates tokens for file content
func RoughTokenCountEstimationForFileType(content string, fileExtension string) int {
	bytesPerToken := BytesPerTokenForFileType(fileExtension)
	return RoughTokenCountEstimation(content, bytesPerToken)
}

// RoughTokenCountEstimationForMessages estimates tokens for a message array
func RoughTokenCountEstimationForMessages(messages []api.InputMessage) int {
	total := 0
	for _, msg := range messages {
		total += roughTokenCountEstimationForMessage(msg)
	}
	return total
}

// roughTokenCountEstimationForMessage estimates tokens for a single message
func roughTokenCountEstimationForMessage(message api.InputMessage) int {
	total := 0

	for _, block := range message.Content {
		total += roughTokenCountForContentBlock(block)
	}

	return total
}

// roughTokenCountForContentBlock estimates tokens for a content block
func roughTokenCountForContentBlock(block api.ContentBlock) int {
	// Serialize the block to JSON and estimate based on that
	// This is a rough approximation that works reasonably well
	data, err := json.Marshal(block)
	if err != nil {
		return 0
	}
	return RoughTokenCountEstimation(string(data), DefaultBytesPerToken)
}

// HasThinkingBlocks checks if messages contain thinking blocks
func HasThinkingBlocks(messages []api.InputMessage) bool {
	for _, message := range messages {
		if message.Role != "assistant" {
			continue
		}

		for _, block := range message.Content {
			// Check for thinking or redacted_thinking type
			if block.Type == "thinking" || block.Type == "redacted_thinking" {
				return true
			}
		}
	}
	return false
}
