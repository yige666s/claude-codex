package compact

import (
	"encoding/json"
	"time"

	api "github.com/ding/claude-code/claude-go/internal/harness/anthropic"
)

// EstimateMessageTokens estimates token count for messages
func EstimateMessageTokens(messages []api.InputMessage) int {
	// Serialize messages to JSON and estimate tokens
	data, err := json.Marshal(messages)
	if err != nil {
		return 0
	}

	// Use 4 bytes per token as default estimation
	return len(data) / 4
}

// CalculateToolResultTokens calculates tokens for a tool result block
func CalculateToolResultTokens(content string) int {
	return roughTokenCountEstimation(content)
}

// roughTokenCountEstimation provides a rough token count estimate
func roughTokenCountEstimation(content string) int {
	// Default: 4 bytes per token
	return len(content) / 4
}

// TimeBasedMicrocompact performs time-based microcompaction
func TimeBasedMicrocompact(
	messages []api.InputMessage,
	gapThresholdMinutes int,
	keepRecent int,
) ([]api.InputMessage, int) {
	if len(messages) == 0 {
		return messages, 0
	}

	// Find the last assistant message timestamp
	var lastAssistantTime *time.Time
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "assistant" {
			// In real implementation, we'd extract timestamp from message metadata
			now := time.Now()
			lastAssistantTime = &now
			break
		}
	}

	if lastAssistantTime == nil {
		return messages, 0
	}

	gapMinutes := time.Since(*lastAssistantTime).Minutes()
	if gapMinutes < float64(gapThresholdMinutes) {
		return messages, 0
	}

	// Collect compactable tool results
	toolResultIndices := []int{}
	for i, msg := range messages {
		if msg.Role == "user" {
			for _, block := range msg.Content {
				if block.Type == "tool_result" {
					toolResultIndices = append(toolResultIndices, i)
					break
				}
			}
		}
	}

	if len(toolResultIndices) == 0 {
		return messages, 0
	}

	// Keep only the most recent N tool results
	clearSet := make(map[int]bool)
	keepCount := min(keepRecent, len(toolResultIndices))
	for i := 0; i < len(toolResultIndices)-keepCount; i++ {
		clearSet[toolResultIndices[i]] = true
	}

	// Clear old tool results
	tokensSaved := 0
	result := make([]api.InputMessage, len(messages))
	for i, msg := range messages {
		if clearSet[i] {
			// Clear tool result content
			newContent := make([]api.ContentBlock, len(msg.Content))
			for j, block := range msg.Content {
				if block.Type == "tool_result" {
					tokensSaved += CalculateToolResultTokens(block.Content)
					newContent[j] = api.ContentBlock{
						Type:      "tool_result",
						ToolUseID: block.ToolUseID,
						Content:   TimeBasedMCClearedMessage,
					}
				} else {
					newContent[j] = block
				}
			}
			result[i] = api.InputMessage{
				Role:    msg.Role,
				Content: newContent,
			}
		} else {
			result[i] = msg
		}
	}

	return result, tokensSaved
}

// IsCompactableTool checks if a tool is compactable
func IsCompactableTool(toolName string) bool {
	return CompactableTools[toolName]
}

// StripImagesFromMessages removes image blocks from messages
func StripImagesFromMessages(messages []api.InputMessage) []api.InputMessage {
	result := make([]api.InputMessage, len(messages))
	for i, msg := range messages {
		if msg.Role != "user" {
			result[i] = msg
			continue
		}

		newContent := make([]api.ContentBlock, 0, len(msg.Content))
		for _, block := range msg.Content {
			if block.Type == "image" {
				// Replace image with text marker
				newContent = append(newContent, api.ContentBlock{
					Type: "text",
					Text: "[Image was shared]",
				})
			} else if block.Type == "tool_result" {
				// Keep tool_result as-is since Content is already a string
				newContent = append(newContent, block)
			} else {
				newContent = append(newContent, block)
			}
		}

		result[i] = api.InputMessage{
			Role:    msg.Role,
			Content: newContent,
		}
	}
	return result
}
