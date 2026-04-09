package messages

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// MessageRole represents the role of a message
type MessageRole string

const (
	RoleUser      MessageRole = "user"
	RoleAssistant MessageRole = "assistant"
)

// ContentBlockType represents the type of content block
type ContentBlockType string

const (
	ContentTypeText      ContentBlockType = "text"
	ContentTypeToolUse   ContentBlockType = "tool_use"
	ContentTypeToolResult ContentBlockType = "tool_result"
	ContentTypeThinking  ContentBlockType = "thinking"
)

// ContentBlock represents a content block in a message
type ContentBlock interface {
	Type() ContentBlockType
}

// TextBlock represents a text content block
type TextBlock struct {
	BlockType ContentBlockType `json:"type"`
	Text      string           `json:"text"`
}

func (t *TextBlock) Type() ContentBlockType {
	return ContentTypeText
}

// ToolUseBlock represents a tool use content block
type ToolUseBlock struct {
	BlockType ContentBlockType `json:"type"`
	ID        string           `json:"id"`
	Name      string           `json:"name"`
	Input     json.RawMessage  `json:"input"`
}

func (t *ToolUseBlock) Type() ContentBlockType {
	return ContentTypeToolUse
}

// ToolResultBlock represents a tool result content block
type ToolResultBlock struct {
	BlockType  ContentBlockType `json:"type"`
	ToolUseID  string           `json:"tool_use_id"`
	Content    interface{}      `json:"content"` // Can be string or []ContentBlock
	IsError    bool             `json:"is_error,omitempty"`
}

func (t *ToolResultBlock) Type() ContentBlockType {
	return ContentTypeToolResult
}

// ThinkingBlock represents a thinking content block
type ThinkingBlock struct {
	BlockType ContentBlockType `json:"type"`
	Thinking  string           `json:"thinking"`
}

func (t *ThinkingBlock) Type() ContentBlockType {
	return ContentTypeThinking
}

// Usage represents token usage information
type Usage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens,omitempty"`
}

// MessageContent represents the content of a message (can be string or []ContentBlock)
type MessageContent interface{}

// BaseMessage represents the base message structure
type BaseMessage struct {
	Role    MessageRole    `json:"role"`
	Content MessageContent `json:"content"`
}

// AssistantMessageData represents the full assistant message data
type AssistantMessageData struct {
	ID          string         `json:"id"`
	Role        MessageRole    `json:"role"`
	Content     []ContentBlock `json:"content"`
	Model       string         `json:"model"`
	StopReason  string         `json:"stop_reason,omitempty"`
	StopSequence string        `json:"stop_sequence,omitempty"`
	Usage       *Usage         `json:"usage,omitempty"`
}

// AssistantMessage represents an assistant message
type AssistantMessage struct {
	Type              string                `json:"type"`
	UUID              string                `json:"uuid"`
	Timestamp         string                `json:"timestamp"`
	Message           *AssistantMessageData `json:"message"`
	RequestID         *string               `json:"requestId,omitempty"`
	IsMeta            bool                  `json:"isMeta,omitempty"`
	IsVirtual         bool                  `json:"isVirtual,omitempty"`
	IsAPIErrorMessage bool                  `json:"isApiErrorMessage,omitempty"`
	Error             interface{}           `json:"error,omitempty"`
}

// UserMessage represents a user message
type UserMessage struct {
	Type                     string         `json:"type"`
	UUID                     string         `json:"uuid"`
	Timestamp                string         `json:"timestamp"`
	Message                  *BaseMessage   `json:"message"`
	IsMeta                   bool           `json:"isMeta,omitempty"`
	IsVirtual                bool           `json:"isVirtual,omitempty"`
	IsVisibleInTranscriptOnly bool          `json:"isVisibleInTranscriptOnly,omitempty"`
	ToolUseResult            interface{}    `json:"toolUseResult,omitempty"`
	SourceToolAssistantUUID  *string        `json:"sourceToolAssistantUUID,omitempty"`
}

// Message represents any message type
type Message interface {
	GetType() string
	GetUUID() string
	GetTimestamp() string
}

func (m *AssistantMessage) GetType() string     { return m.Type }
func (m *AssistantMessage) GetUUID() string     { return m.UUID }
func (m *AssistantMessage) GetTimestamp() string { return m.Timestamp }

func (m *UserMessage) GetType() string     { return m.Type }
func (m *UserMessage) GetUUID() string     { return m.UUID }
func (m *UserMessage) GetTimestamp() string { return m.Timestamp }

// NormalizedMessage represents a normalized message with single content block
type NormalizedMessage interface {
	Message
	GetContentBlock() ContentBlock
}

// NormalizedAssistantMessage represents a normalized assistant message
type NormalizedAssistantMessage struct {
	*AssistantMessage
	ContentBlock ContentBlock
}

func (m *NormalizedAssistantMessage) GetContentBlock() ContentBlock {
	return m.ContentBlock
}

// NormalizedUserMessage represents a normalized user message
type NormalizedUserMessage struct {
	*UserMessage
	ContentBlock ContentBlock
}

func (m *NormalizedUserMessage) GetContentBlock() ContentBlock {
	return m.ContentBlock
}

// Constants for synthetic messages
const (
	NoContentMessage              = "<no-content>"
	InterruptMessage              = "<interrupt>"
	InterruptMessageForToolUse    = "<interrupt-for-tool-use>"
	CancelMessage                 = "<cancel>"
	RejectMessage                 = "<reject>"
	NoResponseRequested           = "<no-response-requested>"
	SyntheticModel                = "<synthetic>"
)

// CreateUserMessageOptions represents options for creating a user message
type CreateUserMessageOptions struct {
	Content                  MessageContent
	IsMeta                   bool
	IsVisibleInTranscriptOnly bool
	IsVirtual                bool
	ToolUseResult            interface{}
	UUID                     *string
	Timestamp                *string
	SourceToolAssistantUUID  *string
}

// CreateUserMessage creates a new user message
func CreateUserMessage(opts CreateUserMessageOptions) *UserMessage {
	msgUUID := uuid.New().String()
	if opts.UUID != nil {
		msgUUID = *opts.UUID
	}

	timestamp := time.Now().Format(time.RFC3339)
	if opts.Timestamp != nil {
		timestamp = *opts.Timestamp
	}

	content := opts.Content
	if content == nil || content == "" {
		content = NoContentMessage
	}

	return &UserMessage{
		Type:      "user",
		UUID:      msgUUID,
		Timestamp: timestamp,
		Message: &BaseMessage{
			Role:    RoleUser,
			Content: content,
		},
		IsMeta:                   opts.IsMeta,
		IsVirtual:                opts.IsVirtual,
		IsVisibleInTranscriptOnly: opts.IsVisibleInTranscriptOnly,
		ToolUseResult:            opts.ToolUseResult,
		SourceToolAssistantUUID:  opts.SourceToolAssistantUUID,
	}
}

// CreateAssistantMessageOptions represents options for creating an assistant message
type CreateAssistantMessageOptions struct {
	Content   []ContentBlock
	Usage     *Usage
	IsVirtual bool
	Model     string
}

// CreateAssistantMessage creates a new assistant message
func CreateAssistantMessage(opts CreateAssistantMessageOptions) *AssistantMessage {
	msgUUID := uuid.New().String()
	timestamp := time.Now().Format(time.RFC3339)

	content := opts.Content
	if content == nil {
		content = []ContentBlock{
			&TextBlock{
				BlockType: ContentTypeText,
				Text:      NoContentMessage,
			},
		}
	}

	model := opts.Model
	if model == "" {
		model = SyntheticModel
	}

	usage := opts.Usage
	if usage == nil {
		usage = &Usage{
			InputTokens:  0,
			OutputTokens: 0,
		}
	}

	return &AssistantMessage{
		Type:      "assistant",
		UUID:      msgUUID,
		Timestamp: timestamp,
		Message: &AssistantMessageData{
			ID:         uuid.New().String(),
			Role:       RoleAssistant,
			Content:    content,
			Model:      model,
			StopReason: "stop_sequence",
			Usage:      usage,
		},
		IsVirtual: opts.IsVirtual,
	}
}

// CreateToolResultMessageOptions represents options for creating a tool result message
type CreateToolResultMessageOptions struct {
	ToolUseID               string
	Content                 interface{}
	IsError                 bool
	SourceToolAssistantUUID *string
}

// CreateToolResultMessage creates a new tool result message
func CreateToolResultMessage(opts CreateToolResultMessageOptions) *UserMessage {
	toolResultBlock := &ToolResultBlock{
		BlockType: ContentTypeToolResult,
		ToolUseID: opts.ToolUseID,
		Content:   opts.Content,
		IsError:   opts.IsError,
	}

	return CreateUserMessage(CreateUserMessageOptions{
		Content:                 []ContentBlock{toolResultBlock},
		SourceToolAssistantUUID: opts.SourceToolAssistantUUID,
	})
}

// CreateInterruptMessage creates a synthetic interrupt message
func CreateInterruptMessage(toolUse bool) *UserMessage {
	content := InterruptMessage
	if toolUse {
		content = InterruptMessageForToolUse
	}

	return CreateUserMessage(CreateUserMessageOptions{
		Content: []ContentBlock{
			&TextBlock{
				BlockType: ContentTypeText,
				Text:      content,
			},
		},
	})
}

// IsToolUseMessage checks if a message contains tool use
func IsToolUseMessage(msg Message) bool {
	if assistantMsg, ok := msg.(*AssistantMessage); ok {
		for _, block := range assistantMsg.Message.Content {
			if block.Type() == ContentTypeToolUse {
				return true
			}
		}
	}
	return false
}

// IsToolResultMessage checks if a message is a tool result
func IsToolResultMessage(msg Message) bool {
	if userMsg, ok := msg.(*UserMessage); ok {
		if blocks, ok := userMsg.Message.Content.([]ContentBlock); ok {
			for _, block := range blocks {
				if block.Type() == ContentTypeToolResult {
					return true
				}
			}
		}
	}
	return false
}

// ExtractTextContent extracts text content from a message
func ExtractTextContent(msg Message) string {
	switch m := msg.(type) {
	case *NormalizedAssistantMessage:
		if textBlock, ok := m.ContentBlock.(*TextBlock); ok {
			return textBlock.Text
		}
	case *NormalizedUserMessage:
		if textBlock, ok := m.ContentBlock.(*TextBlock); ok {
			return textBlock.Text
		}
	case *AssistantMessage:
		for _, block := range m.Message.Content {
			if textBlock, ok := block.(*TextBlock); ok {
				return textBlock.Text
			}
		}
	case *UserMessage:
		if str, ok := m.Message.Content.(string); ok {
			return str
		}
		if blocks, ok := m.Message.Content.([]ContentBlock); ok {
			for _, block := range blocks {
				if textBlock, ok := block.(*TextBlock); ok {
					return textBlock.Text
				}
			}
		}
	}
	return ""
}
