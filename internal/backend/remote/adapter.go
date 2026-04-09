package remote

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

// ConvertSDKMessage converts an SDK message to internal message format
func ConvertSDKMessage(msg *SessionsMessage, opts *ConvertOptions) *ConvertedMessage {
	if opts == nil {
		opts = &ConvertOptions{}
	}

	switch msg.Type {
	case string(SDKMessageTypeAssistant):
		return &ConvertedMessage{
			Type:    "message",
			Message: convertAssistantMessage(msg),
		}

	case string(SDKMessageTypePartial):
		return &ConvertedMessage{
			Type:        "stream_event",
			StreamEvent: msg.Event,
		}

	case string(SDKMessageTypeUser):
		// Check if this is a tool result message
		if opts.ConvertToolResults && msg.ToolUseResult != nil {
			return &ConvertedMessage{
				Type:    "message",
				Message: convertUserToolResultMessage(msg),
			}
		}
		// Check if this is a text message
		if opts.ConvertUserTextMessages && msg.UserMessage != nil {
			return &ConvertedMessage{
				Type:    "message",
				Message: convertUserTextMessage(msg),
			}
		}
		return &ConvertedMessage{Type: "ignored"}

	case string(SDKMessageTypeResult):
		return &ConvertedMessage{
			Type:    "message",
			Message: convertResultMessage(msg),
		}

	case string(SDKMessageTypeSystem):
		if msg.Subtype == "init" {
			return &ConvertedMessage{
				Type:    "message",
				Message: convertInitMessage(msg),
			}
		}
		return &ConvertedMessage{Type: "ignored"}

	case string(SDKMessageTypeStatus):
		if msg.Status != "" {
			return &ConvertedMessage{
				Type:    "message",
				Message: convertStatusMessage(msg),
			}
		}
		return &ConvertedMessage{Type: "ignored"}

	case string(SDKMessageTypeToolProgress):
		return &ConvertedMessage{
			Type:    "message",
			Message: convertToolProgressMessage(msg),
		}

	case string(SDKMessageTypeCompactBoundary):
		return &ConvertedMessage{
			Type:    "message",
			Message: convertCompactBoundaryMessage(msg),
		}

	case string(SDKMessageTypeAuthStatus),
		string(SDKMessageTypeToolUseSummary),
		string(SDKMessageTypeRateLimitEvent):
		// These are SDK-only events, not displayed
		return &ConvertedMessage{Type: "ignored"}

	default:
		// Unknown message type - gracefully ignore
		return &ConvertedMessage{Type: "ignored"}
	}
}

// convertAssistantMessage converts SDK assistant message to internal format
func convertAssistantMessage(msg *SessionsMessage) interface{} {
	return map[string]interface{}{
		"type":      "assistant",
		"message":   msg.Message,
		"uuid":      msg.UUID,
		"timestamp": time.Now().Format(time.RFC3339),
		"error":     msg.Error,
	}
}

// convertUserToolResultMessage converts tool result to internal format
func convertUserToolResultMessage(msg *SessionsMessage) interface{} {
	return map[string]interface{}{
		"type":           "user",
		"uuid":           msg.UUID,
		"tool_use_result": msg.ToolUseResult,
		"timestamp":      msg.Timestamp,
	}
}

// convertUserTextMessage converts user text message to internal format
func convertUserTextMessage(msg *SessionsMessage) interface{} {
	return map[string]interface{}{
		"type":      "user",
		"uuid":      msg.UUID,
		"message":   msg.UserMessage,
		"timestamp": msg.Timestamp,
	}
}

// convertResultMessage converts result message to internal format
func convertResultMessage(msg *SessionsMessage) interface{} {
	isError := msg.Subtype != "success"
	content := "Session completed successfully"
	level := "info"

	if isError {
		if len(msg.Errors) > 0 {
			content = msg.Errors[0]
		} else {
			content = "Unknown error"
		}
		level = "warning"
	}

	return map[string]interface{}{
		"type":      "system",
		"subtype":   "informational",
		"content":   content,
		"level":     level,
		"uuid":      msg.UUID,
		"timestamp": time.Now().Format(time.RFC3339),
	}
}

// convertInitMessage converts init message to internal format
func convertInitMessage(msg *SessionsMessage) interface{} {
	return map[string]interface{}{
		"type":      "system",
		"subtype":   "informational",
		"content":   fmt.Sprintf("Remote session initialized (model: %s)", msg.Model),
		"level":     "info",
		"uuid":      msg.UUID,
		"timestamp": time.Now().Format(time.RFC3339),
	}
}

// convertStatusMessage converts status message to internal format
func convertStatusMessage(msg *SessionsMessage) interface{} {
	content := fmt.Sprintf("Status: %s", msg.Status)
	if msg.Status == "compacting" {
		content = "Compacting conversation…"
	}

	return map[string]interface{}{
		"type":      "system",
		"subtype":   "informational",
		"content":   content,
		"level":     "info",
		"uuid":      msg.UUID,
		"timestamp": time.Now().Format(time.RFC3339),
	}
}

// convertToolProgressMessage converts tool progress to internal format
func convertToolProgressMessage(msg *SessionsMessage) interface{} {
	return map[string]interface{}{
		"type":        "system",
		"subtype":     "informational",
		"content":     fmt.Sprintf("Tool %s running for %.1fs…", msg.ToolName, msg.ElapsedTimeSeconds),
		"level":       "info",
		"uuid":        msg.UUID,
		"timestamp":   time.Now().Format(time.RFC3339),
		"tool_use_id": msg.ToolUseID,
	}
}

// convertCompactBoundaryMessage converts compact boundary to internal format
func convertCompactBoundaryMessage(msg *SessionsMessage) interface{} {
	return map[string]interface{}{
		"type":             "system",
		"subtype":          "compact_boundary",
		"content":          "Conversation compacted",
		"level":            "info",
		"uuid":             msg.UUID,
		"timestamp":        time.Now().Format(time.RFC3339),
		"compact_metadata": msg.CompactMetadata,
	}
}

// IsSessionEndMessage checks if message indicates session end
func IsSessionEndMessage(msg *SessionsMessage) bool {
	return msg.Type == string(SDKMessageTypeResult)
}

// IsSuccessResult checks if result message indicates success
func IsSuccessResult(msg *SessionsMessage) bool {
	return msg.Type == string(SDKMessageTypeResult) && msg.Subtype == "success"
}

// GetResultText extracts result text from success message
func GetResultText(msg *SessionsMessage) string {
	if IsSuccessResult(msg) {
		return msg.Result
	}
	return ""
}

// CreateSyntheticAssistantMessage creates a synthetic assistant message for permission requests
func CreateSyntheticAssistantMessage(request *PermissionRequestInner, requestID string) interface{} {
	return map[string]interface{}{
		"type": "assistant",
		"uuid": uuid.New().String(),
		"message": map[string]interface{}{
			"id":   fmt.Sprintf("remote-%s", requestID),
			"type": "message",
			"role": "assistant",
			"content": []interface{}{
				map[string]interface{}{
					"type":  "tool_use",
					"id":    request.ToolUseID,
					"name":  request.ToolName,
					"input": request.Input,
				},
			},
			"model":        "",
			"stop_reason":  nil,
			"stop_sequence": nil,
			"usage": map[string]interface{}{
				"input_tokens":                 0,
				"output_tokens":                0,
				"cache_creation_input_tokens":  0,
				"cache_read_input_tokens":      0,
			},
		},
		"timestamp": time.Now().Format(time.RFC3339),
	}
}

// CreateToolStub creates a minimal tool stub for unknown tools
func CreateToolStub(toolName string) map[string]interface{} {
	return map[string]interface{}{
		"name":              toolName,
		"input_schema":      map[string]interface{}{},
		"is_enabled":        true,
		"user_facing_name":  toolName,
		"needs_permissions": true,
		"is_mcp":            false,
		"is_read_only":      false,
	}
}
