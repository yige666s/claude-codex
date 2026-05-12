package engine

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"claude-codex/internal/harness/permissions"
	"claude-codex/internal/harness/state"
	toolkit "claude-codex/internal/harness/tools"
	bashtool "claude-codex/internal/harness/tools/bash"
)

type followupPlanner struct {
	call ToolCall
}

func (p followupPlanner) Next(_ context.Context, session *state.Session, _ []toolkit.Descriptor) (Plan, error) {
	last := session.LastMessage()
	if last != nil && last.Role == "tool" {
		return Plan{
			AssistantText: "handled: " + last.ToolOutput,
			StopReason:    "end_turn",
		}, nil
	}
	return Plan{
		ToolCalls:  []ToolCall{p.call},
		StopReason: "tool_use",
	}, nil
}

type failingTool struct {
	name string
	err  error
}

func (t failingTool) Name() string { return t.name }

func (t failingTool) Description() string { return "test failing tool" }

func (t failingTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object"}`)
}

func (t failingTool) Permission() permissions.Level { return permissions.LevelExecute }

func (t failingTool) Execute(context.Context, json.RawMessage) (toolkit.Result, error) {
	return toolkit.Result{}, t.err
}

func (t failingTool) IsConcurrencySafe() bool { return true }

func TestRunContinuesAfterBashApprovalStyleError(t *testing.T) {
	registry := toolkit.NewRegistry(bashtool.NewTool(t.TempDir()))
	engine := NewWithDir(NewSimplePlanner(), registry, permissions.NewChecker(permissions.ModeBypass, nil, nil), 3, t.TempDir())

	session := state.NewSession(t.TempDir())
	result, err := engine.Run(context.Background(), session, "run `echo hi \\| cat`")
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if !strings.Contains(result.Output, "bash command requires approval") {
		t.Fatalf("expected tool error to be surfaced in final output, got %q", result.Output)
	}

	var sawToolResult bool
	for _, message := range session.Messages {
		if message.Role == "tool" && message.ToolName == "Bash" {
			sawToolResult = true
			if !strings.Contains(message.ToolOutput, "bash command requires approval") {
				t.Fatalf("expected bash tool output to be preserved, got %q", message.ToolOutput)
			}
		}
	}
	if !sawToolResult {
		t.Fatalf("expected bash tool result to be recorded, got %#v", session.Messages)
	}
}

func TestRunContinuesAfterToolExecutionFailure(t *testing.T) {
	input, err := json.Marshal(map[string]any{"value": "test"})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	registry := toolkit.NewRegistry(failingTool{
		name: "failing_tool",
		err:  errors.New("boom"),
	})
	engine := NewWithDir(followupPlanner{
		call: ToolCall{
			ID:    "tool-1",
			Name:  "failing_tool",
			Input: input,
		},
	}, registry, permissions.NewChecker(permissions.ModeBypass, nil, nil), 3, t.TempDir())

	session := state.NewSession(t.TempDir())
	result, err := engine.Run(context.Background(), session, "trigger tool")
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if !strings.Contains(result.Output, "handled: failing_tool: boom") {
		t.Fatalf("expected engine to continue after tool failure, got %q", result.Output)
	}

	last := session.LastMessage()
	if last == nil || last.Role != "assistant" {
		t.Fatalf("expected final assistant message, got %#v", last)
	}
}

func TestRunWithUnlimitedMaxTurnsAllowsFollowUpTurn(t *testing.T) {
	input, err := json.Marshal(map[string]any{"value": "test"})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	registry := toolkit.NewRegistry(failingTool{
		name: "failing_tool",
		err:  errors.New("boom"),
	})
	engine := NewWithDir(followupPlanner{
		call: ToolCall{
			ID:    "tool-1",
			Name:  "failing_tool",
			Input: input,
		},
	}, registry, permissions.NewChecker(permissions.ModeBypass, nil, nil), 0, t.TempDir())

	session := state.NewSession(t.TempDir())
	result, err := engine.Run(context.Background(), session, "trigger tool")
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if !strings.Contains(result.Output, "handled: failing_tool: boom") {
		t.Fatalf("expected unlimited max turns to allow the follow-up assistant turn, got %q", result.Output)
	}
}
