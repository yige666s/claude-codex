package builtin

import (
	"context"
	"fmt"
	"time"

	"claude-codex/internal/harness/hooks"
)

// PermissionRequestHook handles permission requests for tool execution.
type PermissionRequestHook struct {
	name    string
	timeout time.Duration
}

// NewPermissionRequestHook creates a new PermissionRequest hook.
func NewPermissionRequestHook() *PermissionRequestHook {
	return &PermissionRequestHook{
		name:    "builtin:permissionrequest",
		timeout: 5 * time.Second,
	}
}

// Name returns the hook name.
func (h *PermissionRequestHook) Name() string {
	return h.name
}

// Event returns the hook event type.
func (h *PermissionRequestHook) Event() hooks.HookEvent {
	return hooks.EventPermissionRequest
}

// IsAsync returns false (PermissionRequest hooks are synchronous).
func (h *PermissionRequestHook) IsAsync() bool {
	return false
}

// Timeout returns the hook timeout.
func (h *PermissionRequestHook) Timeout() time.Duration {
	return h.timeout
}

// Execute runs the hook logic.
func (h *PermissionRequestHook) Execute(ctx context.Context, input *hooks.HookInput) (*hooks.HookResult, error) {
	if input.Permission == nil {
		return &hooks.HookResult{Continue: true}, nil
	}

	// Check if this is an auto-allowed tool
	if h.isAutoAllowed(input.Permission) {
		return &hooks.HookResult{
			Continue: true,
			PermissionDecision: &hooks.PermissionDecision{
				Behavior: "allow",
				Reason:   "Tool is auto-allowed",
			},
		}, nil
	}

	// Check if this is a dangerous operation
	if h.isDangerous(input.Permission) {
		return &hooks.HookResult{
			Continue: true,
			PermissionDecision: &hooks.PermissionDecision{
				Behavior: "ask",
				Reason:   "Operation requires user confirmation",
				Message:  h.buildWarningMessage(input.Permission),
			},
		}, nil
	}

	// Default: ask user
	return &hooks.HookResult{
		Continue: true,
		PermissionDecision: &hooks.PermissionDecision{
			Behavior: "ask",
			Reason:   "Default permission policy",
		},
	}, nil
}

// isAutoAllowed checks if a tool is auto-allowed.
func (h *PermissionRequestHook) isAutoAllowed(perm *hooks.PermissionInfo) bool {
	autoAllowedTools := []string{
		"Read",
		"Glob",
		"Grep",
		"LSP",
	}

	for _, tool := range autoAllowedTools {
		if perm.ToolName == tool {
			return true
		}
	}

	return false
}

// isDangerous checks if an operation is dangerous.
func (h *PermissionRequestHook) isDangerous(perm *hooks.PermissionInfo) bool {
	dangerousTools := []string{
		"Bash",
		"Write",
		"Edit",
		"mcp__filesystem__delete_file",
		"mcp__filesystem__write_file",
	}

	for _, tool := range dangerousTools {
		if perm.ToolName == tool {
			return true
		}
	}

	return false
}

// buildWarningMessage builds a warning message for dangerous operations.
func (h *PermissionRequestHook) buildWarningMessage(perm *hooks.PermissionInfo) string {
	switch perm.ToolName {
	case "Bash":
		if cmd, ok := perm.Input["command"].(string); ok {
			return fmt.Sprintf("Execute command: %s", cmd)
		}
		return "Execute shell command"
	case "Write":
		if path, ok := perm.Input["file_path"].(string); ok {
			return fmt.Sprintf("Write file: %s", path)
		}
		return "Write file"
	case "Edit":
		if path, ok := perm.Input["file_path"].(string); ok {
			return fmt.Sprintf("Edit file: %s", path)
		}
		return "Edit file"
	default:
		return fmt.Sprintf("Execute %s", perm.ToolName)
	}
}
