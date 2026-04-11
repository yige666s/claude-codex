package compact

import (
	"fmt"
	"strings"

	"claude-codex/internal/public/types"
)

// SnipMessages truncates large tool results to prevent context overflow.
// This is the simplest form of compaction - it just cuts off long outputs.
func SnipMessages(messages []types.Message, config *SnipConfig) []types.Message {
	if config == nil {
		config = DefaultSnipConfig()
	}

	result := make([]types.Message, 0, len(messages))

	for _, msg := range messages {
		if msg.Type != types.MessageTypeUser {
			result = append(result, msg)
			continue
		}

		// Check if this message has tool results that need snipping
		hasLargeToolResult := false
		for _, block := range msg.Content {
			if block.Type == "tool_result" {
				size := len(block.Content)
				if size > config.MaxSize {
					hasLargeToolResult = true
					break
				}
			}
		}

		if !hasLargeToolResult {
			result = append(result, msg)
			continue
		}

		// Create a new message with snipped tool results
		newContent := make([]types.ContentBlock, 0, len(msg.Content))
		for _, block := range msg.Content {
			if block.Type == "tool_result" {
				snipped := snipToolResult(block, config)
				newContent = append(newContent, snipped)
			} else {
				newContent = append(newContent, block)
			}
		}

		newMsg := msg
		newMsg.Content = newContent
		result = append(result, newMsg)
	}

	return result
}

// snipToolResult truncates a single tool result block.
func snipToolResult(block types.ContentBlock, config *SnipConfig) types.ContentBlock {
	content := block.Content
	size := len(content)

	if size <= config.MaxSize {
		return block
	}

	// Calculate how much to preserve
	preserveTotal := config.PreservePrefix + config.PreserveSuffix
	if preserveTotal >= config.MaxSize {
		// If preserve sizes are too large, just take prefix
		prefix := content[:config.MaxSize]
		newContent := prefix + config.TruncateMessage

		newBlock := block
		newBlock.Content = newContent
		return newBlock
	}

	// Extract prefix and suffix
	prefix := content[:config.PreservePrefix]
	suffix := content[size-config.PreserveSuffix:]

	// Calculate how many bytes were removed
	removedBytes := size - preserveTotal
	truncateMsg := fmt.Sprintf(
		"\n\n[... %d bytes truncated ...]\n\n",
		removedBytes,
	)

	newContent := prefix + truncateMsg + suffix + config.TruncateMessage

	newBlock := block
	newBlock.Content = newContent
	return newBlock
}

// SnipToolResultContent is a helper that snips a single content string.
func SnipToolResultContent(content string, maxSize int) string {
	if len(content) <= maxSize {
		return content
	}

	config := DefaultSnipConfig()
	config.MaxSize = maxSize

	// Use default preserve ratios
	preservePrefix := maxSize * 2 / 3
	preserveSuffix := maxSize / 3

	if preservePrefix+preserveSuffix >= maxSize {
		preservePrefix = maxSize - 1000
		preserveSuffix = 1000
	}

	prefix := content[:preservePrefix]
	suffix := content[len(content)-preserveSuffix:]
	removedBytes := len(content) - preservePrefix - preserveSuffix

	return fmt.Sprintf(
		"%s\n\n[... %d bytes truncated ...]\n\n%s%s",
		prefix,
		removedBytes,
		suffix,
		config.TruncateMessage,
	)
}

// EstimateTokensSaved estimates how many tokens were saved by snipping.
func EstimateTokensSaved(originalSize, snippedSize int) int {
	// Rough estimation: 1 token ≈ 4 characters
	bytesRemoved := originalSize - snippedSize
	return bytesRemoved / 4
}

// ShouldSnipToolResult checks if a tool result should be snipped.
func ShouldSnipToolResult(toolName string, contentSize int, maxSize int) bool {
	if !IsCompactable(toolName) {
		return false
	}
	return contentSize > maxSize
}

// GetToolResultSize returns the size of a tool result in bytes.
func GetToolResultSize(block types.ContentBlock) int {
	if block.Type != "tool_result" {
		return 0
	}
	return len(block.Content)
}

// CountLargeToolResults counts how many tool results exceed the size limit.
func CountLargeToolResults(messages []types.Message, maxSize int) int {
	count := 0
	for _, msg := range messages {
		if msg.Type != types.MessageTypeUser {
			continue
		}
		for _, block := range msg.Content {
			if block.Type == "tool_result" && len(block.Content) > maxSize {
				count++
			}
		}
	}
	return count
}

// SnipStats contains statistics about snipping operations.
type SnipStats struct {
	TotalMessages       int
	MessagesWithSnips   int
	ToolResultsSnipped  int
	BytesRemoved        int
	EstimatedTokensSaved int
}

// SnipMessagesWithStats performs snipping and returns statistics.
func SnipMessagesWithStats(messages []types.Message, config *SnipConfig) ([]types.Message, *SnipStats) {
	if config == nil {
		config = DefaultSnipConfig()
	}

	stats := &SnipStats{
		TotalMessages: len(messages),
	}

	result := make([]types.Message, 0, len(messages))

	for _, msg := range messages {
		if msg.Type != types.MessageTypeUser {
			result = append(result, msg)
			continue
		}

		hasLargeToolResult := false
		originalSize := 0
		snippedSize := 0

		newContent := make([]types.ContentBlock, 0, len(msg.Content))
		for _, block := range msg.Content {
			if block.Type == "tool_result" {
				size := len(block.Content)
				originalSize += size

				if size > config.MaxSize {
					hasLargeToolResult = true
					stats.ToolResultsSnipped++
					snipped := snipToolResult(block, config)
					snippedSize += len(snipped.Content)
					newContent = append(newContent, snipped)
				} else {
					snippedSize += size
					newContent = append(newContent, block)
				}
			} else {
				newContent = append(newContent, block)
			}
		}

		if hasLargeToolResult {
			stats.MessagesWithSnips++
			stats.BytesRemoved += (originalSize - snippedSize)

			newMsg := msg
			newMsg.Content = newContent
			result = append(result, newMsg)
		} else {
			result = append(result, msg)
		}
	}

	stats.EstimatedTokensSaved = stats.BytesRemoved / 4
	return result, stats
}

// FormatSnipStats formats snip statistics for display.
func FormatSnipStats(stats *SnipStats) string {
	if stats.ToolResultsSnipped == 0 {
		return "No tool results were snipped"
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Snipped %d tool results in %d messages\n",
		stats.ToolResultsSnipped, stats.MessagesWithSnips)
	fmt.Fprintf(&sb, "Removed %d bytes (~%d tokens)\n",
		stats.BytesRemoved, stats.EstimatedTokensSaved)

	return sb.String()
}
