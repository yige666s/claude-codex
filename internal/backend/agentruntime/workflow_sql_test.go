package agentruntime

import (
	"context"
	"errors"
	"testing"
)

func TestSQLWorkflowStorePostgresLifecycle(t *testing.T) {
	db := openPostgresMigrationTestDB(t)
	ctx := context.Background()
	if err := RunPostgresGooseMigrations(ctx, db, SQLDialectPostgres); err != nil {
		t.Fatalf("RunPostgresGooseMigrations() error = %v", err)
	}
	store := NewSQLWorkflowStoreWithDialect(db, SQLDialectPostgres)
	if err := store.Init(ctx); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	engine := NewWorkflowEngine(store, NoopWorkflowEventSink{})
	engine.RegisterStepHandler("first", func(context.Context, *WorkflowRun, map[string]any) (map[string]any, error) {
		return map[string]any{"message_count": 2}, nil
	})
	engine.RegisterStepHandler("second", func(_ context.Context, _ *WorkflowRun, input map[string]any) (map[string]any, error) {
		if input["message_count"] != float64(2) {
			t.Fatalf("expected persisted state in second step, got %#v", input)
		}
		return map[string]any{"final_status": "answered"}, nil
	})
	run, err := engine.Execute(ctx, WorkflowRequest{
		Definition: WorkflowDefinition{
			Name:    "agentic_task",
			Version: "v1",
			Steps:   []WorkflowStepDefinition{{Name: "first"}, {Name: "second"}},
		},
		UserID:         "alice",
		SessionID:      "session-1",
		JobID:          "job-1",
		State:          map[string]any{"request_id": "req-1"},
		IdempotencyKey: "req-1",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	loaded, err := store.GetWorkflowRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetWorkflowRun() error = %v", err)
	}
	if loaded.Status != WorkflowStatusSucceeded || loaded.State["final_status"] != "answered" {
		t.Fatalf("unexpected loaded run: %#v", loaded)
	}
	if loaded.RequestID != "req-1" || loaded.IdempotencyKey != "req-1" {
		t.Fatalf("expected request id/idempotency key to persist, got %#v", loaded)
	}
	runs, err := store.ListWorkflowRuns(ctx, WorkflowRunFilter{
		UserID: "alice",
		JobID:  "job-1",
		Name:   "agentic_task",
		Status: WorkflowStatusSucceeded,
		Limit:  10,
	})
	if err != nil {
		t.Fatalf("ListWorkflowRuns() error = %v", err)
	}
	if len(runs) != 1 || runs[0].ID != run.ID {
		t.Fatalf("unexpected filtered workflow runs: %#v", runs)
	}
	steps, err := store.ListWorkflowStepRuns(ctx, run.ID)
	if err != nil {
		t.Fatalf("ListWorkflowStepRuns() error = %v", err)
	}
	if len(steps) != 2 || steps[0].StepName != "first" || steps[1].StepName != "second" {
		t.Fatalf("unexpected workflow steps: %#v", steps)
	}
	if steps[0].StepIndex != 0 || steps[1].StepIndex != 1 || steps[0].Attempt != 1 || steps[1].Attempt != 1 {
		t.Fatalf("unexpected workflow step resume metadata: %#v", steps)
	}
}

func TestSQLWorkflowStorePostgresRecordsFailedRun(t *testing.T) {
	db := openPostgresMigrationTestDB(t)
	ctx := context.Background()
	if err := RunPostgresGooseMigrations(ctx, db, SQLDialectPostgres); err != nil {
		t.Fatalf("RunPostgresGooseMigrations() error = %v", err)
	}
	store := NewSQLWorkflowStoreWithDialect(db, SQLDialectPostgres)
	if err := store.Init(ctx); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	expected := errors.New("workflow failed")
	engine := NewWorkflowEngine(store, NoopWorkflowEventSink{})
	engine.RegisterStepHandler("fail", func(context.Context, *WorkflowRun, map[string]any) (map[string]any, error) {
		return nil, expected
	})
	run, err := engine.Execute(ctx, WorkflowRequest{
		Definition: WorkflowDefinition{Name: "demo", Steps: []WorkflowStepDefinition{{Name: "fail"}}},
		UserID:     "alice",
	})
	if !errors.Is(err, expected) {
		t.Fatalf("Execute() error = %v, want %v", err, expected)
	}
	loaded, err := store.GetWorkflowRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetWorkflowRun() error = %v", err)
	}
	if loaded.Status != WorkflowStatusFailed || loaded.Error != expected.Error() {
		t.Fatalf("unexpected failed run: %#v", loaded)
	}
}

func TestSQLWorkflowStorePostgresExecuteOrResume(t *testing.T) {
	db := openPostgresMigrationTestDB(t)
	ctx := context.Background()
	if err := RunPostgresGooseMigrations(ctx, db, SQLDialectPostgres); err != nil {
		t.Fatalf("RunPostgresGooseMigrations() error = %v", err)
	}
	store := NewSQLWorkflowStoreWithDialect(db, SQLDialectPostgres)
	if err := store.Init(ctx); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	engine := NewWorkflowEngine(store, NoopWorkflowEventSink{})
	calls := 0
	engine.RegisterStepHandler("ok", func(context.Context, *WorkflowRun, map[string]any) (map[string]any, error) {
		calls++
		return map[string]any{"answer": "ok"}, nil
	})
	req := WorkflowRequest{
		Definition:     WorkflowDefinition{Name: "demo", Version: "v1", Steps: []WorkflowStepDefinition{{Name: "ok"}}},
		UserID:         "alice",
		SessionID:      "session-1",
		IdempotencyKey: "same-request",
		Recoverable:    true,
	}
	first, err := engine.ExecuteOrResume(ctx, req)
	if err != nil {
		t.Fatalf("first ExecuteOrResume() error = %v", err)
	}
	second, err := engine.ExecuteOrResume(ctx, req)
	if err != nil {
		t.Fatalf("second ExecuteOrResume() error = %v", err)
	}
	if first.ID != second.ID || calls != 1 {
		t.Fatalf("expected idempotent SQL workflow reuse, first=%s second=%s calls=%d", first.ID, second.ID, calls)
	}
	found, err := store.FindWorkflowRunByIdempotencyKey(ctx, "alice", "demo", "same-request")
	if err != nil {
		t.Fatalf("FindWorkflowRunByIdempotencyKey() error = %v", err)
	}
	if found == nil || found.ID != first.ID || !found.Recoverable {
		t.Fatalf("unexpected found run: %#v", found)
	}
	step, err := store.GetWorkflowStepByIndex(ctx, first.ID, 0)
	if err != nil {
		t.Fatalf("GetWorkflowStepByIndex() error = %v", err)
	}
	if step == nil || step.StepName != "ok" || step.Status != WorkflowStepStatusSucceeded {
		t.Fatalf("unexpected found step: %#v", step)
	}
}
