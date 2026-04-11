package builtin

import (
	"context"
	"fmt"
	"time"

	"claude-codex/internal/harness/hooks"
)

// PostToolUseHook processes tool output after execution.
type PostToolUseHook struct {
	name    string
	timeout time.Duration
}

// NewPostToolUseHook creates a new PostToolUse hook.
func NewPostToolUseHook() *PostToolUseHook {
	return &PostToolUseHook{
		name:    "builtin:posttooluse",
		timeout: 5 * time.Second,
	}
}

// Name returns the hook name.
func (h *PostToolUseHook) Name() string {
	return h.name
}

// Event returns the hook event type.
func (h *PostToolUseHook) Event() hooks.HookEvent {
	return hooks.EventPostToolUse
}

// IsAsync returns true (PostToolUse hooks can be asynchronous).
func (h *PostToolUseHook) IsAsync() bool {
	return true
}

// Timeout returns the hook timeout.
func (h *PostToolUseHook) Timeout() time.Duration {
	return h.timeout
}

// Execute runs the hook logic.
func (h *PostToolUseHook) Execute(ctx context.Context, input *hooks.HookInput) (*hooks.HookResult, error) {
	if input.Tool == nil {
		return &hooks.HookResult{Continue: true}, nil
	}

	// Log tool execution
	h.logToolExecution(input.Tool)

	// Check for errors
	if input.Tool.Error != nil {
		return h.handleToolError(input.Tool)
	}

	// Process successful execution
	return &hooks.HookResult{
		Continue:          true,
		AdditionalContext: h.buildContext(input.Tool),
	}, nil
}

// logToolExecution logs tool execution details.
func (h *PostToolUseHook) logToolExecution(tool *hooks.ToolInfo) {
	// In a real implementation, this would log to a file or service
	// For now, we just track that the tool was executed
	_ = tool
}

// handleToolError handles tool execution errors.
func (h *PostToolUseHook) handleToolError(tool *hooks.ToolInfo) (*hooks.HookResult, error) {
	return &hooks.HookResult{
		Continue:      true,
		SystemMessage: fmt.Sprintf("Tool %s failed: %v", tool.Name, tool.Error),
	}, nil
}

// buildContext builds additional context from tool execution.
func (h *PostToolUseHook) buildContext(tool *hooks.ToolInfo) string {
	if tool.Output == "" {
		return ""
	}

	// Truncate large outputs
	maxLen := 500
	if len(tool.Output) > maxLen {
		return fmt.Sprintf("Tool %s executed successfully (output truncated)", tool.Name)
	}

	return fmt.Sprintf("Tool %s executed successfully", tool.Name)
}
