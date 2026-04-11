package builtin

import (
	"context"
	"fmt"
	"time"

	"claude-codex/internal/harness/hooks"
)

// PreToolUseHook validates and potentially modifies tool input before execution.
type PreToolUseHook struct {
	name    string
	timeout time.Duration
}

// NewPreToolUseHook creates a new PreToolUse hook.
func NewPreToolUseHook() *PreToolUseHook {
	return &PreToolUseHook{
		name:    "builtin:pretooluse",
		timeout: 5 * time.Second,
	}
}

// Name returns the hook name.
func (h *PreToolUseHook) Name() string {
	return h.name
}

// Event returns the hook event type.
func (h *PreToolUseHook) Event() hooks.HookEvent {
	return hooks.EventPreToolUse
}

// IsAsync returns false (PreToolUse hooks are synchronous).
func (h *PreToolUseHook) IsAsync() bool {
	return false
}

// Timeout returns the hook timeout.
func (h *PreToolUseHook) Timeout() time.Duration {
	return h.timeout
}

// Execute runs the hook logic.
func (h *PreToolUseHook) Execute(ctx context.Context, input *hooks.HookInput) (*hooks.HookResult, error) {
	if input.Tool == nil {
		return &hooks.HookResult{Continue: true}, nil
	}

	// Validate tool input
	if err := h.validateToolInput(input.Tool); err != nil {
		return &hooks.HookResult{
			Continue:      false,
			StopReason:    fmt.Sprintf("Tool validation failed: %v", err),
			BlockingError: err.Error(),
		}, nil
	}

	// Check for dangerous operations
	if h.isDangerousOperation(input.Tool) {
		return &hooks.HookResult{
			Continue: true,
			PermissionDecision: &hooks.PermissionDecision{
				Behavior: "ask",
				Reason:   "This operation may be destructive or hard to reverse",
			},
		}, nil
	}

	// Allow by default
	return &hooks.HookResult{
		Continue: true,
		PermissionDecision: &hooks.PermissionDecision{
			Behavior: "allow",
			Reason:   "Tool validation passed",
		},
	}, nil
}

// validateToolInput validates tool input parameters.
func (h *PreToolUseHook) validateToolInput(tool *hooks.ToolInfo) error {
	if tool.Name == "" {
		return fmt.Errorf("tool name is required")
	}

	// Add more validation as needed
	return nil
}

// isDangerousOperation checks if the tool operation is potentially dangerous.
func (h *PreToolUseHook) isDangerousOperation(tool *hooks.ToolInfo) bool {
	// Check for destructive Bash commands
	if tool.Name == "Bash" {
		if cmd, ok := tool.Input["command"].(string); ok {
			return containsDangerousCommand(cmd)
		}
	}

	// Check for file deletion
	if tool.Name == "mcp__filesystem__delete_file" {
		return true
	}

	return false
}

// containsDangerousCommand checks if a bash command contains dangerous operations.
func containsDangerousCommand(cmd string) bool {
	dangerousPatterns := []string{
		"rm -rf",
		"rm -fr",
		"git reset --hard",
		"git push --force",
		"git push -f",
		"DROP TABLE",
		"DROP DATABASE",
		"> /dev/",
	}

	for _, pattern := range dangerousPatterns {
		if contains(cmd, pattern) {
			return true
		}
	}

	return false
}

// contains checks if a string contains a substring (case-insensitive).
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr ||
		len(s) > len(substr) && containsAt(s, substr, 0))
}

func containsAt(s, substr string, start int) bool {
	for i := start; i <= len(s)-len(substr); i++ {
		match := true
		for j := 0; j < len(substr); j++ {
			if s[i+j] != substr[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
