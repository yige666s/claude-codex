package agentruntime

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"claude-codex/internal/harness/engine"
)

func TestNormalizeMessageStructuredOutputDefaults(t *testing.T) {
	output, err := NormalizeMessageStructuredOutput(json.RawMessage(`{"title":"Plan","kind":"unknown","summary":"Ready"}`), MessageStructuredOutput{
		UserID:    "user-1",
		SessionID: "sess-1",
		RunID:     "run-1",
	})
	if err != nil {
		t.Fatalf("NormalizeMessageStructuredOutput() error = %v", err)
	}
	if output.ID == "" {
		t.Fatal("expected generated structured output id")
	}
	if output.Kind != "card" {
		t.Fatalf("kind = %q, want card", output.Kind)
	}
	if output.SchemaVersion != StructuredOutputSchemaVersion {
		t.Fatalf("version = %q, want %q", output.SchemaVersion, StructuredOutputSchemaVersion)
	}
	var payload map[string]any
	if err := json.Unmarshal(output.Payload, &payload); err != nil {
		t.Fatalf("payload json: %v", err)
	}
	if payload["id"] != output.ID || payload["version"] != StructuredOutputSchemaVersion || payload["kind"] != "card" {
		t.Fatalf("payload was not normalized: %#v", payload)
	}
}

func TestMemoryRuntimeOutputStoreReserveChatTurnIsIdempotent(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryRuntimeOutputStore()
	first, err := store.ReserveChatTurn(ctx, ChatTurnReservation{
		UserID:         "user-1",
		SessionID:      "sess-1",
		IdempotencyKey: "idem-1",
		RunID:          "run-1",
	})
	if err != nil {
		t.Fatalf("first ReserveChatTurn() error = %v", err)
	}
	if !first.Reserved || first.RunID != "run-1" {
		t.Fatalf("first reservation = %#v", first)
	}
	second, err := store.ReserveChatTurn(ctx, ChatTurnReservation{
		UserID:         "user-1",
		SessionID:      "sess-1",
		IdempotencyKey: "idem-1",
		RunID:          "run-2",
	})
	if err != nil {
		t.Fatalf("second ReserveChatTurn() error = %v", err)
	}
	if second.Reserved {
		t.Fatalf("second reservation should be replay, got %#v", second)
	}
	if second.RunID != first.RunID {
		t.Fatalf("duplicate run id = %q, want %q", second.RunID, first.RunID)
	}
}

func TestMemoryRuntimeOutputStoreRejectsConcurrentSessionTurn(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryRuntimeOutputStore()
	first, err := store.ReserveChatTurn(ctx, ChatTurnReservation{
		UserID:         "user-1",
		SessionID:      "sess-1",
		IdempotencyKey: "idem-1",
		RunID:          "run-1",
	})
	if err != nil || !first.Reserved {
		t.Fatalf("first ReserveChatTurn() = %#v, %v", first, err)
	}
	if _, err := store.ReserveChatTurn(ctx, ChatTurnReservation{
		UserID:         "user-1",
		SessionID:      "sess-1",
		IdempotencyKey: "idem-2",
		RunID:          "run-2",
	}); !errors.Is(err, ErrSessionTurnRunning) {
		t.Fatalf("concurrent ReserveChatTurn() error = %v, want %v", err, ErrSessionTurnRunning)
	}
	if err := store.UpdateChatTurnReservationStatus(ctx, "user-1", "sess-1", "run-1", "succeeded"); err != nil {
		t.Fatalf("finish first reservation: %v", err)
	}
	second, err := store.ReserveChatTurn(ctx, ChatTurnReservation{
		UserID:         "user-1",
		SessionID:      "sess-1",
		IdempotencyKey: "idem-2",
		RunID:          "run-2",
	})
	if err != nil || !second.Reserved {
		t.Fatalf("second ReserveChatTurn() after release = %#v, %v", second, err)
	}
}

func TestMemoryRuntimeOutputStoreAtomicallyHandsChatTurnToJob(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryRuntimeOutputStore()
	first, err := store.ReserveChatTurn(ctx, ChatTurnReservation{
		UserID:         "user-1",
		SessionID:      "sess-1",
		IdempotencyKey: "chat-1",
		RunID:          "chat-run-1",
	})
	if err != nil || !first.Reserved {
		t.Fatalf("reserve chat turn = %#v, %v", first, err)
	}
	jobReservation, err := store.HandoffChatTurn(ctx, first.RunID, ChatTurnReservation{
		UserID:         "user-1",
		SessionID:      "sess-1",
		IdempotencyKey: "job:job-1",
		RunID:          "job-1",
	})
	if err != nil || !jobReservation.Reserved {
		t.Fatalf("handoff chat turn = %#v, %v", jobReservation, err)
	}
	repeated, err := store.HandoffChatTurn(ctx, first.RunID, ChatTurnReservation{
		UserID:         "user-1",
		SessionID:      "sess-1",
		IdempotencyKey: "job:job-1",
		RunID:          "job-1",
	})
	if err != nil || repeated.Reserved || repeated.RunID != jobReservation.RunID {
		t.Fatalf("repeated handoff should adopt existing job reservation = %#v, %v", repeated, err)
	}
	if _, err := store.ReserveChatTurn(ctx, ChatTurnReservation{
		UserID:         "user-1",
		SessionID:      "sess-1",
		IdempotencyKey: "chat-2",
		RunID:          "chat-run-2",
	}); !errors.Is(err, ErrSessionTurnRunning) {
		t.Fatalf("concurrent turn after handoff error = %v, want %v", err, ErrSessionTurnRunning)
	}
	adopted, err := store.ReserveChatTurn(ctx, ChatTurnReservation{
		UserID:         "user-1",
		SessionID:      "sess-1",
		IdempotencyKey: "job:job-1",
		RunID:          "job-1",
	})
	if err != nil || adopted.Reserved || adopted.RunID != "job-1" || adopted.Status != "reserved" {
		t.Fatalf("adopt handed-off job turn = %#v, %v", adopted, err)
	}
}

func TestResumableChatSinkPersistsStructuredOutputAndSnapshot(t *testing.T) {
	ctx := context.Background()
	streams := NewMemoryChatStreamStore()
	outputs := NewMemoryRuntimeOutputStore()
	if _, err := streams.CreateRunWithID(ctx, "user-1", "sess-1", "run-1"); err != nil {
		t.Fatalf("CreateRunWithID() error = %v", err)
	}
	sink := &resumableChatSink{
		runID:             "run-1",
		userID:            "user-1",
		sessionID:         "sess-1",
		store:             streams,
		structuredOutputs: outputs,
		snapshots:         outputs,
		reservations:      outputs,
	}
	if err := sink.Send(ctx, Event{
		Type:      StructuredOutputEventType,
		SessionID: "sess-1",
		RunID:     "run-1",
		Data:      json.RawMessage(`{"id":"so-1","kind":"card","title":"Result"}`),
	}); err != nil {
		t.Fatalf("send structured_output: %v", err)
	}
	if err := sink.Send(ctx, Event{Type: "message", ID: "msg-a1", SessionID: "sess-1", RunID: "run-1", Role: "assistant", Content: "done"}); err != nil {
		t.Fatalf("send assistant message: %v", err)
	}
	if err := sink.Send(ctx, Event{Type: "done", SessionID: "sess-1", RunID: "run-1"}); err != nil {
		t.Fatalf("send done: %v", err)
	}
	structured, err := outputs.ListStructuredOutputsByRun(ctx, "user-1", "run-1")
	if err != nil {
		t.Fatalf("ListStructuredOutputsByRun() error = %v", err)
	}
	if len(structured) != 1 || structured[0].ID != "so-1" {
		t.Fatalf("structured outputs = %#v", structured)
	}
	snapshot, err := outputs.GetChatRunSnapshot(ctx, "user-1", "run-1")
	if err != nil {
		t.Fatalf("GetChatRunSnapshot() error = %v", err)
	}
	if snapshot.Status != "succeeded" || snapshot.StructuredOutputCount != 1 || snapshot.FinalContent != "done" {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	ledger := NewMemoryToolCallLedgerStore()
	_, _, err = ledger.BeginToolCall(ctx, engine.ToolLedgerEntry{
		UserID:         "user-1",
		SessionID:      "sess-1",
		WorkflowRunID:  "run-1",
		ToolName:       "search",
		IdempotencyKey: "tool-1",
	})
	if err != nil {
		t.Fatalf("BeginToolCall() error = %v", err)
	}
	if err := ledger.FailToolCall(ctx, "tool-1", "boom", nil); err != nil {
		t.Fatalf("FailToolCall() error = %v", err)
	}
	summary, err := SummarizeRunUsage(ctx, outputs, outputs, ledger, "user-1", "run-1")
	if err != nil {
		t.Fatalf("SummarizeRunUsage() error = %v", err)
	}
	if summary.Status != "succeeded" || summary.StructuredOutputCount != 1 || summary.ToolCallCount != 1 || summary.ToolErrorCount != 1 {
		t.Fatalf("summary = %#v", summary)
	}
}
