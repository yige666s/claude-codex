package queryhelpers

import publictypes "claude-codex/internal/public/types"

// IsResultSuccessful mirrors the TS helper's terminal-message success heuristic.
func IsResultSuccessful(message *publictypes.Message, stopReason string) bool {
	if message == nil {
		return false
	}

	if message.Type == publictypes.MessageTypeAssistant {
		for i := len(message.Content) - 1; i >= 0; i-- {
			switch message.Content[i].Type {
			case "text", "thinking", "redacted_thinking":
				return true
			}
		}
	}

	if message.Type == publictypes.MessageTypeUser {
		if len(message.Content) > 0 {
			allToolResults := true
			for _, block := range message.Content {
				if block.Type != "tool_result" {
					allToolResults = false
					break
				}
			}
			if allToolResults {
				return true
			}
		}
	}

	return stopReason == "end_turn"
}
