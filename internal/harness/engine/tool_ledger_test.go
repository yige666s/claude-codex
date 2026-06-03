package engine

import (
	"context"
	"encoding/json"
	"testing"

	"claude-codex/internal/harness/permissions"
	"claude-codex/internal/harness/state"
	toolkit "claude-codex/internal/harness/tools"
)

type countingTool struct {
	count *int
}

func (t countingTool) Name() string { return "counting_tool" }

func (t countingTool) Description() string { return "test counting tool" }

func (t countingTool) InputSchema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }

func (t countingTool) Permission() permissions.Level { return permissions.LevelRead }

func (t countingTool) Execute(context.Context, json.RawMessage) (toolkit.Result, error) {
	*t.count++
	return toolkit.Result{Output: "fresh"}, nil
}

func (t countingTool) IsConcurrencySafe() bool { return false }

type memoryEngineLedger struct {
	entries map[string]ToolLedgerEntry
}

func newMemoryEngineLedger() *memoryEngineLedger {
	return &memoryEngineLedger{entries: map[string]ToolLedgerEntry{}}
}

func (l *memoryEngineLedger) BeginToolCall(_ context.Context, entry ToolLedgerEntry) (ToolLedgerEntry, bool, error) {
	if existing, ok := l.entries[entry.IdempotencyKey]; ok {
		if existing.Status == ToolLedgerStatusSucceeded {
			return existing, true, nil
		}
	}
	entry.ID = "ledger-1"
	entry.Status = ToolLedgerStatusRunning
	entry.Attempt = 1
	l.entries[entry.IdempotencyKey] = entry
	return entry, false, nil
}

func (l *memoryEngineLedger) CompleteToolCall(_ context.Context, idempotencyKey, output string, _ map[string]any) error {
	entry := l.entries[idempotencyKey]
	entry.Status = ToolLedgerStatusSucceeded
	entry.Output = output
	l.entries[idempotencyKey] = entry
	return nil
}

func (l *memoryEngineLedger) FailToolCall(_ context.Context, idempotencyKey, errText string, _ map[string]any) error {
	entry := l.entries[idempotencyKey]
	entry.Status = ToolLedgerStatusFailed
	entry.Error = errText
	l.entries[idempotencyKey] = entry
	return nil
}

func TestEngineToolLedgerReusesSucceededToolResult(t *testing.T) {
	calls := 0
	input := json.RawMessage(`{"value":"same"}`)
	registry := toolkit.NewRegistry(countingTool{count: &calls})
	engine := New(followupPlanner{call: ToolCall{ID: "tool-1", Name: "counting_tool", Input: input}}, registry, permissions.NewChecker(permissions.ModeBypass, nil, nil), 3)
	ledger := newMemoryEngineLedger()
	engine.SetToolLedger(ledger)
	engine.SetDefaultToolExecutionScope(ToolExecutionScope{UserID: "alice", SessionID: "session-1"})

	ctx := WithToolExecutionScope(context.Background(), ToolExecutionScope{
		WorkflowRunID:     "run-1",
		WorkflowStepID:    "step-1",
		WorkflowStepIndex: 3,
	})
	session := state.NewSession("")
	first := engine.executeToolCall(ctx, session, "interaction-1", ToolCall{ID: "tool-1", Name: "counting_tool", Input: input}, toolkit.NoOpProgressReporter{})
	second := engine.executeToolCall(ctx, session, "interaction-2", ToolCall{ID: "tool-2", Name: "counting_tool", Input: input}, toolkit.NoOpProgressReporter{})
	if calls != 1 {
		t.Fatalf("expected tool to execute once, executed %d times", calls)
	}
	if first.ToolOutput != "fresh" || second.ToolOutput != "fresh" {
		t.Fatalf("unexpected tool outputs: first=%q second=%q", first.ToolOutput, second.ToolOutput)
	}
}
