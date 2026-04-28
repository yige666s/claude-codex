package plannerapi

import (
	"context"
	"encoding/json"

	"claude-codex/internal/harness/state"
	toolkit "claude-codex/internal/harness/tools"
)

// ToolCall represents a single tool invocation planned by a model/runtime.
type ToolCall struct {
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

// Plan is the planner/runtime output for a single turn.
type Plan struct {
	AssistantText string     `json:"assistant_text,omitempty"`
	ToolCalls     []ToolCall `json:"tool_calls,omitempty"`
	StopReason    string     `json:"stop_reason,omitempty"`
}

// Planner advances a conversation by producing the next assistant turn plan.
type Planner interface {
	Next(ctx context.Context, session *state.Session, tools []toolkit.Descriptor) (Plan, error)
}
