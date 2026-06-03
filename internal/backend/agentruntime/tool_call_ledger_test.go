package agentruntime

import (
	"context"
	"testing"

	"claude-codex/internal/harness/engine"
)

func TestMemoryToolCallLedgerReusesSucceededResult(t *testing.T) {
	store := NewMemoryToolCallLedgerStore()
	entry := engine.ToolLedgerEntry{
		UserID:         "alice",
		SessionID:      "session-1",
		WorkflowRunID:  "run-1",
		WorkflowStepID: "step-1",
		ToolCallID:     "tool-1",
		ToolName:       "Read",
		ArgsHash:       "hash",
		IdempotencyKey: "step-1:Read:hash",
	}
	started, reused, err := store.BeginToolCall(context.Background(), entry)
	if err != nil {
		t.Fatalf("BeginToolCall() error = %v", err)
	}
	if reused || started.Attempt != 1 {
		t.Fatalf("unexpected first begin: reused=%v entry=%#v", reused, started)
	}
	if err := store.CompleteToolCall(context.Background(), entry.IdempotencyKey, "cached output", nil); err != nil {
		t.Fatalf("CompleteToolCall() error = %v", err)
	}
	cached, reused, err := store.BeginToolCall(context.Background(), entry)
	if err != nil {
		t.Fatalf("second BeginToolCall() error = %v", err)
	}
	if !reused || cached.Output != "cached output" || cached.Status != engine.ToolLedgerStatusSucceeded {
		t.Fatalf("expected cached result, reused=%v entry=%#v", reused, cached)
	}
	list, err := store.ListToolCalls(context.Background(), ToolCallLedgerFilter{UserID: "alice", WorkflowRunID: "run-1"})
	if err != nil {
		t.Fatalf("ListToolCalls() error = %v", err)
	}
	if len(list) != 1 || list[0].IdempotencyKey != entry.IdempotencyKey {
		t.Fatalf("unexpected ledger list: %#v", list)
	}
}

func TestMemoryToolCallLedgerRetriesFailedResult(t *testing.T) {
	store := NewMemoryToolCallLedgerStore()
	entry := engine.ToolLedgerEntry{
		UserID:         "alice",
		WorkflowRunID:  "run-1",
		WorkflowStepID: "step-1",
		ToolName:       "Bash",
		ArgsHash:       "hash",
		IdempotencyKey: "step-1:Bash:hash",
	}
	if _, _, err := store.BeginToolCall(context.Background(), entry); err != nil {
		t.Fatalf("BeginToolCall() error = %v", err)
	}
	if err := store.FailToolCall(context.Background(), entry.IdempotencyKey, "timeout", nil); err != nil {
		t.Fatalf("FailToolCall() error = %v", err)
	}
	retry, reused, err := store.BeginToolCall(context.Background(), entry)
	if err != nil {
		t.Fatalf("retry BeginToolCall() error = %v", err)
	}
	if reused || retry.Attempt != 2 || retry.Status != engine.ToolLedgerStatusRunning {
		t.Fatalf("expected retry attempt, reused=%v entry=%#v", reused, retry)
	}
}

func TestSQLToolCallLedgerPostgresLifecycle(t *testing.T) {
	db := openPostgresMigrationTestDB(t)
	ctx := context.Background()
	if err := RunPostgresGooseMigrations(ctx, db, SQLDialectPostgres); err != nil {
		t.Fatalf("RunPostgresGooseMigrations() error = %v", err)
	}
	store := NewSQLToolCallLedgerStoreWithDialect(db, SQLDialectPostgres)
	if err := store.Init(ctx); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	entry := engine.ToolLedgerEntry{
		UserID:            "alice",
		SessionID:         "session-1",
		JobID:             "job-1",
		WorkflowRunID:     "run-1",
		WorkflowStepID:    "step-1",
		WorkflowStepIndex: 1,
		ToolCallID:        "tool-1",
		ToolName:          "Artifact",
		ArgsHash:          "hash",
		IdempotencyKey:    "step-1:Artifact:hash",
	}
	if _, reused, err := store.BeginToolCall(ctx, entry); err != nil || reused {
		t.Fatalf("BeginToolCall() reused=%v error=%v", reused, err)
	}
	if err := store.CompleteToolCall(ctx, entry.IdempotencyKey, `{"artifact_id":"a1"}`, map[string]any{"output_chars": 20}); err != nil {
		t.Fatalf("CompleteToolCall() error = %v", err)
	}
	cached, reused, err := store.BeginToolCall(ctx, entry)
	if err != nil {
		t.Fatalf("second BeginToolCall() error = %v", err)
	}
	if !reused || cached.Output == "" || cached.Status != engine.ToolLedgerStatusSucceeded {
		t.Fatalf("expected SQL cached result, reused=%v entry=%#v", reused, cached)
	}
	list, err := store.ListToolCalls(ctx, ToolCallLedgerFilter{UserID: "alice", WorkflowRunID: "run-1", Status: engine.ToolLedgerStatusSucceeded})
	if err != nil {
		t.Fatalf("ListToolCalls() error = %v", err)
	}
	if len(list) != 1 || list[0].WorkflowStepIndex != 1 || list[0].Metadata["output_chars"] == nil {
		t.Fatalf("unexpected SQL ledger list: %#v", list)
	}
}
