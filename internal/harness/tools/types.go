package tools

import (
	"context"

	"github.com/ding/claude-code/claude-go/internal/public/types"
)

// ToolExecutor defines the interface for executing tools.
type ToolExecutor interface {
	// Execute runs the tool with the given input
	Execute(ctx context.Context, input map[string]any) (*ToolResult, error)

	// IsConcurrencySafe returns true if this tool can be run concurrently
	IsConcurrencySafe(input map[string]any) bool

	// Name returns the tool name
	Name() string

	// Description returns the tool description
	Description() string

	// InputSchema returns the JSON schema for tool input
	InputSchema() map[string]any
}

// ToolResult represents the result of a tool execution.
type ToolResult struct {
	// Content is the tool result content
	Content string

	// IsError indicates if the execution failed
	IsError bool

	// ErrorMessage contains the error message if IsError is true
	ErrorMessage string

	// ContextModifier optionally modifies the tool use context
	ContextModifier func(ctx *ToolUseContext) *ToolUseContext
}

// ToolUseContext contains context for tool execution.
type ToolUseContext struct {
	// WorkingDir is the current working directory
	WorkingDir string

	// SessionID identifies the current session
	SessionID string

	// InProgressToolUseIDs tracks tools currently executing
	InProgressToolUseIDs map[string]bool

	// Tools available for execution
	Tools []ToolExecutor

	// PermissionMode controls tool execution permissions
	PermissionMode string

	// MaxConcurrency limits concurrent tool execution
	MaxConcurrency int
}

// NewToolUseContext creates a new tool use context.
func NewToolUseContext(workingDir, sessionID string, tools []ToolExecutor) *ToolUseContext {
	return &ToolUseContext{
		WorkingDir:           workingDir,
		SessionID:            sessionID,
		InProgressToolUseIDs: make(map[string]bool),
		Tools:                tools,
		PermissionMode:       "normal",
		MaxConcurrency:       10,
	}
}

// AddInProgressToolUse marks a tool as in progress.
func (ctx *ToolUseContext) AddInProgressToolUse(toolUseID string) {
	ctx.InProgressToolUseIDs[toolUseID] = true
}

// RemoveInProgressToolUse marks a tool as complete.
func (ctx *ToolUseContext) RemoveInProgressToolUse(toolUseID string) {
	delete(ctx.InProgressToolUseIDs, toolUseID)
}

// IsInProgress checks if a tool is currently executing.
func (ctx *ToolUseContext) IsInProgress(toolUseID string) bool {
	return ctx.InProgressToolUseIDs[toolUseID]
}

// FindToolByName finds a tool by name.
func (ctx *ToolUseContext) FindToolByName(name string) ToolExecutor {
	for _, tool := range ctx.Tools {
		if tool.Name() == name {
			return tool
		}
	}
	return nil
}

// ToolUseBlock represents a tool use request from the assistant.
type ToolUseBlock struct {
	ID    string
	Name  string
	Input map[string]any
}

// MessageUpdate represents an update during tool execution.
type MessageUpdate struct {
	// Message is the tool result message
	Message *types.Message

	// NewContext is the updated context after execution
	NewContext *ToolUseContext
}

// CanUseToolFunc checks if a tool can be used.
type CanUseToolFunc func(
	ctx context.Context,
	toolName string,
	input map[string]any,
	toolUseID string,
) (bool, string, error)

// PermissionResult represents the result of a permission check.
type PermissionResult struct {
	Allowed bool
	Reason  string
}
