package types

import (
	"encoding/json"
	"time"
)

// ToolDescriptor describes a tool's capabilities and schema.
type ToolDescriptor struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

// ToolCall represents a request to execute a tool.
type ToolCall struct {
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

// ToolResult represents the result of a tool execution.
type ToolResult struct {
	Output   string                 `json:"output"`
	IsError  bool                   `json:"is_error,omitempty"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// ToolCallProgress represents progress information for a tool call.
type ToolCallProgress struct {
	ToolCallID string    `json:"tool_call_id"`
	ToolName   string    `json:"tool_name"`
	Status     string    `json:"status"` // "started", "running", "completed", "failed"
	Progress   float64   `json:"progress,omitempty"`
	Message    string    `json:"message,omitempty"`
	StartedAt  time.Time `json:"started_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

// ToolExecutionContext provides context for tool execution.
type ToolExecutionContext struct {
	SessionID      string                 `json:"session_id"`
	WorkingDir     string                 `json:"working_dir"`
	ToolCallID     string                 `json:"tool_call_id"`
	ParentToolUseID *string               `json:"parent_tool_use_id,omitempty"`
	Metadata       map[string]interface{} `json:"metadata,omitempty"`
}

// ToolRegistry manages available tools.
type ToolRegistry interface {
	// Register registers a tool.
	Register(tool Tool) error

	// Get retrieves a tool by name.
	Get(name string) (Tool, error)

	// List returns all registered tools.
	List() []Tool

	// Descriptors returns tool descriptors for all registered tools.
	Descriptors() []ToolDescriptor

	// Has checks if a tool is registered.
	Has(name string) bool

	// Unregister removes a tool from the registry.
	Unregister(name string) error
}

// Tool defines the interface that all tools must implement.
type Tool interface {
	// Name returns the tool's name.
	Name() string

	// Description returns the tool's description.
	Description() string

	// InputSchema returns the JSON schema for the tool's input.
	InputSchema() json.RawMessage

	// Execute executes the tool with the given input.
	Execute(ctx ToolExecutionContext, input json.RawMessage) (*ToolResult, error)

	// Permission returns the permission level required for this tool.
	Permission() PermissionLevel

	// IsConcurrencySafe returns whether this tool can be safely called concurrently.
	IsConcurrencySafe() bool
}

// ProgressAwareTool is a tool that can report progress during execution.
type ProgressAwareTool interface {
	Tool

	// ExecuteWithProgress executes the tool with progress reporting.
	ExecuteWithProgress(ctx ToolExecutionContext, input json.RawMessage, reporter ProgressReporter) (*ToolResult, error)
}

// ProgressReporter allows tools to report progress.
type ProgressReporter interface {
	// Report reports a progress event.
	Report(event ProgressEvent)
}

// ProgressEvent represents a progress event from a tool.
type ProgressEvent struct {
	ToolName string  `json:"tool_name"`
	Status   string  `json:"status"` // "started", "running", "completed", "failed"
	Progress float64 `json:"progress,omitempty"`
	Message  string  `json:"message,omitempty"`
}

// ToolCollection represents a collection of tools.
type ToolCollection struct {
	Tools []Tool `json:"tools"`
}

// NewToolResult creates a new successful tool result.
func NewToolResult(output string) *ToolResult {
	return &ToolResult{
		Output:   output,
		IsError:  false,
		Metadata: make(map[string]interface{}),
	}
}

// NewToolError creates a new error tool result.
func NewToolError(message string) *ToolResult {
	return &ToolResult{
		Output:   message,
		IsError:  true,
		Metadata: make(map[string]interface{}),
	}
}

// WithMetadata adds metadata to the tool result.
func (tr *ToolResult) WithMetadata(key string, value interface{}) *ToolResult {
	if tr.Metadata == nil {
		tr.Metadata = make(map[string]interface{})
	}
	tr.Metadata[key] = value
	return tr
}

// NewToolCallProgress creates a new tool call progress tracker.
func NewToolCallProgress(toolCallID, toolName string) *ToolCallProgress {
	now := time.Now().UTC()
	return &ToolCallProgress{
		ToolCallID: toolCallID,
		ToolName:   toolName,
		Status:     "started",
		Progress:   0.0,
		StartedAt:  now,
		UpdatedAt:  now,
	}
}

// UpdateProgress updates the progress information.
func (tcp *ToolCallProgress) UpdateProgress(progress float64, message string) {
	tcp.Progress = progress
	tcp.Message = message
	tcp.Status = "running"
	tcp.UpdatedAt = time.Now().UTC()
}

// Complete marks the tool call as completed.
func (tcp *ToolCallProgress) Complete() {
	now := time.Now().UTC()
	tcp.Status = "completed"
	tcp.Progress = 1.0
	tcp.UpdatedAt = now
	tcp.CompletedAt = &now
}

// Fail marks the tool call as failed.
func (tcp *ToolCallProgress) Fail(message string) {
	now := time.Now().UTC()
	tcp.Status = "failed"
	tcp.Message = message
	tcp.UpdatedAt = now
	tcp.CompletedAt = &now
}

// NoOpProgressReporter is a progress reporter that does nothing.
type NoOpProgressReporter struct{}

// Report does nothing.
func (NoOpProgressReporter) Report(event ProgressEvent) {}

// ChannelProgressReporter sends progress events to a channel.
type ChannelProgressReporter struct {
	ch chan<- ProgressEvent
}

// NewChannelProgressReporter creates a new channel progress reporter.
func NewChannelProgressReporter(ch chan<- ProgressEvent) *ChannelProgressReporter {
	return &ChannelProgressReporter{ch: ch}
}

// Report sends a progress event to the channel.
func (cpr *ChannelProgressReporter) Report(event ProgressEvent) {
	select {
	case cpr.ch <- event:
	default:
		// Don't block if channel is full
	}
}
