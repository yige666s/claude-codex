package builtin

import (
	"context"
	"fmt"
	"time"

	"github.com/ding/claude-code/claude-go/internal/harness/hooks"
)

// SessionStartHook handles session initialization.
type SessionStartHook struct {
	name    string
	timeout time.Duration
}

// NewSessionStartHook creates a new SessionStart hook.
func NewSessionStartHook() *SessionStartHook {
	return &SessionStartHook{
		name:    "builtin:sessionstart",
		timeout: 10 * time.Second,
	}
}

// Name returns the hook name.
func (h *SessionStartHook) Name() string {
	return h.name
}

// Event returns the hook event type.
func (h *SessionStartHook) Event() hooks.HookEvent {
	return hooks.EventSessionStart
}

// IsAsync returns false (SessionStart hooks are synchronous).
func (h *SessionStartHook) IsAsync() bool {
	return false
}

// Timeout returns the hook timeout.
func (h *SessionStartHook) Timeout() time.Duration {
	return h.timeout
}

// Execute runs the hook logic.
func (h *SessionStartHook) Execute(ctx context.Context, input *hooks.HookInput) (*hooks.HookResult, error) {
	// Build session context
	context := h.buildSessionContext(input)

	// Check for initial user message
	initialMessage := h.getInitialMessage(input)

	return &hooks.HookResult{
		Continue:           true,
		AdditionalContext:  context,
		InitialUserMessage: initialMessage,
	}, nil
}

// buildSessionContext builds context for the session.
func (h *SessionStartHook) buildSessionContext(input *hooks.HookInput) string {
	if input.WorkingDir == "" {
		return ""
	}

	return fmt.Sprintf("Session started in %s", input.WorkingDir)
}

// getInitialMessage returns the initial user message if any.
func (h *SessionStartHook) getInitialMessage(input *hooks.HookInput) string {
	if input.Metadata == nil {
		return ""
	}

	if msg, ok := input.Metadata["initialMessage"].(string); ok {
		return msg
	}

	return ""
}
