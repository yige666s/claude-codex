package tools

import (
	"context"
	"testing"
)

type stubToolExecutor struct {
	name string
}

func (s stubToolExecutor) Execute(_ context.Context, _ map[string]any) (*ToolResult, error) {
	return nil, nil
}

func (s stubToolExecutor) IsConcurrencySafe(map[string]any) bool { return true }
func (s stubToolExecutor) Name() string                          { return s.name }
func (s stubToolExecutor) Description() string                   { return s.name }
func (s stubToolExecutor) InputSchema() map[string]any           { return map[string]any{"type": "object"} }

func TestToolUseContextFindToolByName(t *testing.T) {
	toolA := stubToolExecutor{name: "tool-a"}
	toolB := stubToolExecutor{name: "tool-b"}

	ctx := NewToolUseContext("/tmp", "session", []ToolExecutor{toolA, toolB})

	if got := ctx.FindToolByName("tool-a"); got == nil || got.Name() != "tool-a" {
		t.Fatalf("expected to find tool-a, got %#v", got)
	}
	if got := ctx.FindToolByName("missing"); got != nil {
		t.Fatalf("expected missing lookup to return nil, got %#v", got)
	}
}

func TestToolUseContextSetToolsRefreshesLookup(t *testing.T) {
	ctx := NewToolUseContext("/tmp", "session", []ToolExecutor{stubToolExecutor{name: "tool-a"}})

	ctx.SetTools([]ToolExecutor{stubToolExecutor{name: "tool-b"}})

	if got := ctx.FindToolByName("tool-a"); got != nil {
		t.Fatalf("expected tool-a to be removed from lookup, got %#v", got)
	}
	if got := ctx.FindToolByName("tool-b"); got == nil || got.Name() != "tool-b" {
		t.Fatalf("expected to find tool-b after SetTools, got %#v", got)
	}
}

func TestToolUseContextInProgressToolUseIDs(t *testing.T) {
	ctx := NewToolUseContext("/tmp", "session", nil)

	ctx.AddInProgressToolUse("one")
	ctx.AddInProgressToolUse("two")

	if !ctx.IsInProgress("one") || !ctx.IsInProgress("two") {
		t.Fatalf("expected in-progress ids to be tracked")
	}

	ids := ctx.GetInProgressToolUseIDs()
	if len(ids) != 2 {
		t.Fatalf("expected 2 in-progress ids, got %d", len(ids))
	}

	ctx.RemoveInProgressToolUse("one")
	if ctx.IsInProgress("one") {
		t.Fatalf("expected one to be removed")
	}
}
