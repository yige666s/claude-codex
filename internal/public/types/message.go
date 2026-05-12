// Package types provides common types used across the Claude Code core packages.
package types

import (
	"time"
)

// MessageType represents the type of a message.
type MessageType string

const (
	MessageTypeUser        MessageType = "user"
	MessageTypeAssistant   MessageType = "assistant"
	MessageTypeSystem      MessageType = "system"
	MessageTypeProgress    MessageType = "progress"
	MessageTypeAttachment  MessageType = "attachment"
	MessageTypeStreamEvent MessageType = "stream_event"
	MessageTypeTombstone   MessageType = "tombstone"
	MessageTypeTool        MessageType = "tool"
)

// Message represents a conversation message in the Claude Code system.
type Message struct {
	Type                      MessageType      `json:"type"`
	UUID                      string           `json:"uuid"`
	Timestamp                 time.Time        `json:"timestamp"`
	Message                   interface{}      `json:"message,omitempty"`
	Content                   []ContentBlock   `json:"content,omitempty"`
	Subtype                   string           `json:"subtype,omitempty"`
	IsMeta                    bool             `json:"is_meta,omitempty"`
	Data                      interface{}      `json:"data,omitempty"`
	Event                     interface{}      `json:"event,omitempty"`
	ToolUseID                 string           `json:"tool_use_id,omitempty"`
	Attachment                interface{}      `json:"attachment,omitempty"`
	StopReason                string           `json:"stop_reason,omitempty"`
	CompactMetadata           *CompactMetadata `json:"compact_metadata,omitempty"`
	IsCompactSummary          bool             `json:"is_compact_summary,omitempty"`
	IsVisibleInTranscriptOnly bool             `json:"is_visible_in_transcript_only,omitempty"`
	ToolUseResult             bool             `json:"tool_use_result,omitempty"`
	IsApiErrorMessage         bool             `json:"is_api_error_message,omitempty"`
}

// CompactMetadata contains metadata for compact boundary messages.
type CompactMetadata struct {
	PreservedSegment *PreservedSegment `json:"preserved_segment,omitempty"`
}

// PreservedSegment identifies the preserved portion of history.
type PreservedSegment struct {
	TailUUID string `json:"tail_uuid"`
}

// ContentBlock represents a piece of message content.
type ContentBlock struct {
	Type      string                 `json:"type"` // "text", "tool_use", "tool_result", "thinking"
	Text      string                 `json:"text,omitempty"`
	Source    map[string]interface{} `json:"source,omitempty"`
	ID        string                 `json:"id,omitempty"`
	Name      string                 `json:"name,omitempty"`
	Input     map[string]interface{} `json:"input,omitempty"`
	ToolUseID string                 `json:"tool_use_id,omitempty"`
	Content   string                 `json:"content,omitempty"`
	IsError   bool                   `json:"is_error,omitempty"`
}

// ToolUseBlock represents a tool use block in assistant messages.
type ToolUseBlock struct {
	Type  string                 `json:"type"`
	ID    string                 `json:"id"`
	Name  string                 `json:"name"`
	Input map[string]interface{} `json:"input"`
}

// UserMessage represents a message from the user.
type UserMessage struct {
	Content []ContentBlock `json:"content"`
}

// AssistantMessage represents a message from the assistant.
type AssistantMessage struct {
	Type     string  `json:"type"`
	Message  Message `json:"message"`
	APIError string  `json:"api_error,omitempty"`
}

// SystemMessage represents a system message.
type SystemMessage struct {
	Content string `json:"content"`
}

// ProgressMessage represents a progress update message.
type ProgressMessage struct {
	ToolName string  `json:"tool_name"`
	Status   string  `json:"status"`
	Progress float64 `json:"progress,omitempty"`
	Message  string  `json:"message,omitempty"`
}

// ToolUseSummaryMessage represents a summary of tool usage.
type ToolUseSummaryMessage struct {
	ToolName   string                 `json:"tool_name"`
	ToolUseID  string                 `json:"tool_use_id"`
	Input      map[string]interface{} `json:"input"`
	Output     string                 `json:"output"`
	DurationMS int64                  `json:"duration_ms"`
	Success    bool                   `json:"success"`
	Error      string                 `json:"error,omitempty"`
}

// SystemPrompt represents the SystemPrompt configuration.
type SystemPrompt struct {
	Content string                 `json:"content"`
	Parts   []SystemPromptPart     `json:"parts,omitempty"`
	Cache   map[string]interface{} `json:"cache,omitempty"`
}

// SystemPromptPart represents a part of the SystemPrompt.
type SystemPromptPart struct {
	Type    string `json:"type"`
	Content string `json:"content"`
	Cache   bool   `json:"cache,omitempty"`
}

// ToolDefinition represents a tool definition for the API.
type ToolDefinition struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"input_schema"`
}

// NewMessage creates a new message with the given type and content.
func NewMessage(msgType MessageType, content interface{}) *Message {
	return &Message{
		Type:      msgType,
		UUID:      UUID(),
		Timestamp: time.Now().UTC(),
		Data:      content,
	}
}
