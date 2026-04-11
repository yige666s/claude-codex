package compact

import (
	api "claude-codex/internal/harness/anthropic"
)

// GroupMessagesByAPIRound groups messages at API-round boundaries
// One group per API round-trip, with boundaries at new assistant responses
func GroupMessagesByAPIRound(messages []api.InputMessage) [][]api.InputMessage {
	groups := [][]api.InputMessage{}
	current := []api.InputMessage{}
	var lastAssistantID string

	for _, msg := range messages {
		// Check if this is a new assistant message (different ID)
		if msg.Role == "assistant" {
			// Extract message ID from metadata if available
			currentID := extractMessageID(msg)

			if currentID != "" && currentID != lastAssistantID && len(current) > 0 {
				// New assistant response - start new group
				groups = append(groups, current)
				current = []api.InputMessage{msg}
			} else {
				current = append(current, msg)
			}

			if currentID != "" {
				lastAssistantID = currentID
			}
		} else {
			current = append(current, msg)
		}
	}

	if len(current) > 0 {
		groups = append(groups, current)
	}

	return groups
}

// extractMessageID extracts the message ID from an assistant message
// In the real implementation, this would extract from message metadata
func extractMessageID(msg api.InputMessage) string {
	// Placeholder - in real implementation would extract from message structure
	// For now, we'll use a simple heuristic based on content
	if msg.Role == "assistant" && len(msg.Content) > 0 {
		// In practice, the API response includes an ID field
		// This is a simplified version
		return ""
	}
	return ""
}

// HasTextBlocks checks if a message contains text blocks
func HasTextBlocks(msg api.InputMessage) bool {
	for _, block := range msg.Content {
		if block.Type == "text" && block.Text != "" {
			return true
		}
	}
	return false
}

// GetToolResultIDs extracts tool_use_ids from tool_result blocks
func GetToolResultIDs(msg api.InputMessage) []string {
	if msg.Role != "user" {
		return nil
	}

	ids := []string{}
	for _, block := range msg.Content {
		if block.Type == "tool_result" && block.ToolUseID != "" {
			ids = append(ids, block.ToolUseID)
		}
	}
	return ids
}

// HasToolUseWithIDs checks if a message contains tool_use blocks with given IDs
func HasToolUseWithIDs(msg api.InputMessage, toolUseIDs map[string]bool) bool {
	if msg.Role != "assistant" {
		return false
	}

	for _, block := range msg.Content {
		if block.Type == "tool_use" && block.ID != "" {
			if toolUseIDs[block.ID] {
				return true
			}
		}
	}
	return false
}
