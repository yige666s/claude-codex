package messages

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// DeriveUUID creates a deterministic UUID from a parent UUID and index
// This produces a stable UUID-shaped string so the same input always produces
// the same key across calls
func DeriveUUID(parentUUID string, index int) string {
	// Use first 24 chars of parent UUID + 12 hex chars from index
	hex := fmt.Sprintf("%012x", index)
	if len(parentUUID) >= 24 {
		return parentUUID[:24] + hex
	}
	// If parent UUID is shorter, pad it
	padded := parentUUID
	for len(padded) < 24 {
		padded += "0"
	}
	return padded[:24] + hex
}

// NormalizeMessages splits messages so each content block gets its own message
func NormalizeMessages(messages []Message) []NormalizedMessage {
	var result []NormalizedMessage
	isNewChain := false

	for _, msg := range messages {
		switch m := msg.(type) {
		case *AssistantMessage:
			// Check if we need to start generating new UUIDs
			isNewChain = isNewChain || len(m.Message.Content) > 1

			for index, block := range m.Message.Content {
				msgUUID := m.UUID
				if isNewChain {
					msgUUID = DeriveUUID(m.UUID, index)
				}

				// Create a copy of the assistant message with single content block
				normalizedMsg := &NormalizedAssistantMessage{
					AssistantMessage: &AssistantMessage{
						Type:              m.Type,
						UUID:              msgUUID,
						Timestamp:         m.Timestamp,
						Message: &AssistantMessageData{
							ID:           m.Message.ID,
							Role:         m.Message.Role,
							Content:      []ContentBlock{block},
							Model:        m.Message.Model,
							StopReason:   m.Message.StopReason,
							StopSequence: m.Message.StopSequence,
							Usage:        m.Message.Usage,
						},
						RequestID:         m.RequestID,
						IsMeta:            m.IsMeta,
						IsVirtual:         m.IsVirtual,
						IsAPIErrorMessage: m.IsAPIErrorMessage,
						Error:             m.Error,
					},
					ContentBlock: block,
				}
				result = append(result, normalizedMsg)
			}

		case *UserMessage:
			// Handle user messages with content blocks
			if blocks, ok := m.Message.Content.([]ContentBlock); ok {
				isNewChain = isNewChain || len(blocks) > 1

				for index, block := range blocks {
					msgUUID := m.UUID
					if isNewChain {
						msgUUID = DeriveUUID(m.UUID, index)
					}

					normalizedMsg := &NormalizedUserMessage{
						UserMessage: &UserMessage{
							Type:                     m.Type,
							UUID:                     msgUUID,
							Timestamp:                m.Timestamp,
							Message: &BaseMessage{
								Role:    m.Message.Role,
								Content: []ContentBlock{block},
							},
							IsMeta:                   m.IsMeta,
							IsVirtual:                m.IsVirtual,
							IsVisibleInTranscriptOnly: m.IsVisibleInTranscriptOnly,
							ToolUseResult:            m.ToolUseResult,
							SourceToolAssistantUUID:  m.SourceToolAssistantUUID,
						},
						ContentBlock: block,
					}
					result = append(result, normalizedMsg)
				}
			} else {
				// Single content (string), wrap it as a text block
				contentStr := ""
				if str, ok := m.Message.Content.(string); ok {
					contentStr = str
				} else {
					contentStr = fmt.Sprintf("%v", m.Message.Content)
				}

				textBlock := &TextBlock{
					BlockType: ContentTypeText,
					Text:      contentStr,
				}

				// Create a copy of the user message with the text block
				normalizedMsg := &NormalizedUserMessage{
					UserMessage: &UserMessage{
						Type:                     m.Type,
						UUID:                     m.UUID,
						Timestamp:                m.Timestamp,
						Message: &BaseMessage{
							Role:    m.Message.Role,
							Content: []ContentBlock{textBlock},
						},
						IsMeta:                   m.IsMeta,
						IsVirtual:                m.IsVirtual,
						IsVisibleInTranscriptOnly: m.IsVisibleInTranscriptOnly,
						ToolUseResult:            m.ToolUseResult,
						SourceToolAssistantUUID:  m.SourceToolAssistantUUID,
					},
					ContentBlock: textBlock,
				}
				result = append(result, normalizedMsg)
			}
		}
	}

	return result
}

// IsNotEmptyMessage checks if a message has meaningful content
func IsNotEmptyMessage(msg Message) bool {
	text := ExtractTextContent(msg)
	if text == "" {
		return false
	}

	// Check for synthetic empty messages
	if text == NoContentMessage || text == InterruptMessageForToolUse {
		return false
	}

	return len(text) > 0
}

// GetToolUseID extracts the tool use ID from a tool use message
func GetToolUseID(msg Message) string {
	if assistantMsg, ok := msg.(*AssistantMessage); ok {
		for _, block := range assistantMsg.Message.Content {
			if toolUseBlock, ok := block.(*ToolUseBlock); ok {
				return toolUseBlock.ID
			}
		}
	}
	return ""
}

// GetToolResultID extracts the tool use ID from a tool result message
func GetToolResultID(msg Message) string {
	if userMsg, ok := msg.(*UserMessage); ok {
		if blocks, ok := userMsg.Message.Content.([]ContentBlock); ok {
			for _, block := range blocks {
				if toolResultBlock, ok := block.(*ToolResultBlock); ok {
					return toolResultBlock.ToolUseID
				}
			}
		}
	}
	return ""
}

// HashContent creates a hash of message content for deduplication
func HashContent(content string) string {
	hash := sha256.Sum256([]byte(content))
	return hex.EncodeToString(hash[:])
}

// IsSyntheticMessage checks if a message is synthetic
func IsSyntheticMessage(msg Message) bool {
	text := ExtractTextContent(msg)
	switch text {
	case InterruptMessage, InterruptMessageForToolUse, CancelMessage, RejectMessage, NoResponseRequested:
		return true
	}
	return false
}

// GetLastAssistantMessage returns the last assistant message from a list
func GetLastAssistantMessage(messages []Message) *AssistantMessage {
	for i := len(messages) - 1; i >= 0; i-- {
		if assistantMsg, ok := messages[i].(*AssistantMessage); ok {
			return assistantMsg
		}
	}
	return nil
}

// HasToolCallsInLastAssistantTurn checks if the last assistant turn has tool calls
func HasToolCallsInLastAssistantTurn(messages []Message) bool {
	for i := len(messages) - 1; i >= 0; i-- {
		if assistantMsg, ok := messages[i].(*AssistantMessage); ok {
			for _, block := range assistantMsg.Message.Content {
				if block.Type() == ContentTypeToolUse {
					return true
				}
			}
			return false
		}
	}
	return false
}
