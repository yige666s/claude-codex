package agentruntime

import (
	"context"
	"errors"
	"testing"
)

func TestWorkflowEngineExecutesStepsAndPersistsCheckpoints(t *testing.T) {
	store := NewMemoryWorkflowStore()
	engine := NewWorkflowEngine(store, NoopWorkflowEventSink{})
	engine.RegisterStepHandler("first", func(ctx context.Context, run *WorkflowRun, input map[string]any) (map[string]any, error) {
		return map[string]any{"first_done": true}, nil
	})
	engine.RegisterStepHandler("second", func(ctx context.Context, run *WorkflowRun, input map[string]any) (map[string]any, error) {
		if input["first_done"] != true {
			t.Fatalf("expected first step state in second input, got %#v", input)
		}
		return map[string]any{"answer": "ok"}, nil
	})

	run, err := engine.Execute(context.Background(), WorkflowRequest{
		Definition: WorkflowDefinition{
			Name:    "demo",
			Version: "v1",
			Steps: []WorkflowStepDefinition{
				{Name: "first"},
				{Name: "second"},
			},
		},
		UserID:    "alice",
		SessionID: "session-1",
		State:     map[string]any{"query": "hello"},
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if run.Status != WorkflowStatusSucceeded || run.State["answer"] != "ok" {
		t.Fatalf("unexpected run: %#v", run)
	}
	steps, err := store.ListWorkflowStepRuns(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("ListWorkflowStepRuns() error = %v", err)
	}
	if len(steps) != 2 {
		t.Fatalf("expected 2 steps, got %#v", steps)
	}
	for _, step := range steps {
		if step.Status != WorkflowStepStatusSucceeded || step.FinishedAt == nil {
			t.Fatalf("unexpected step checkpoint: %#v", step)
		}
	}
}

func TestWorkflowEngineMarksFailedRun(t *testing.T) {
	expected := errors.New("boom")
	store := NewMemoryWorkflowStore()
	engine := NewWorkflowEngine(store, NoopWorkflowEventSink{})
	engine.RegisterStepHandler("fail", func(ctx context.Context, run *WorkflowRun, input map[string]any) (map[string]any, error) {
		return nil, expected
	})

	run, err := engine.Execute(context.Background(), WorkflowRequest{
		Definition: WorkflowDefinition{Name: "demo", Steps: []WorkflowStepDefinition{{Name: "fail"}}},
	})
	if !errors.Is(err, expected) {
		t.Fatalf("expected failure, got %v", err)
	}
	if run == nil || run.Status != WorkflowStatusFailed || run.Error != expected.Error() {
		t.Fatalf("unexpected failed run: %#v", run)
	}
	steps, err := store.ListWorkflowStepRuns(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("ListWorkflowStepRuns() error = %v", err)
	}
	if len(steps) != 1 || steps[0].Status != WorkflowStepStatusFailed {
		t.Fatalf("unexpected failed step: %#v", steps)
	}
}

func TestWorkflowEngineExecuteOrResumeReusesSucceededRun(t *testing.T) {
	store := NewMemoryWorkflowStore()
	engine := NewWorkflowEngine(store, NoopWorkflowEventSink{})
	calls := 0
	engine.RegisterStepHandler("ok", func(ctx context.Context, run *WorkflowRun, input map[string]any) (map[string]any, error) {
		calls++
		return map[string]any{"answer": "ok"}, nil
	})
	req := WorkflowRequest{
		Definition:     WorkflowDefinition{Name: "demo", Version: "v1", Steps: []WorkflowStepDefinition{{Name: "ok"}}},
		UserID:         "alice",
		SessionID:      "session-1",
		IdempotencyKey: "req-1",
		Recoverable:    true,
		State:          map[string]any{"query": "hello"},
	}
	first, err := engine.ExecuteOrResume(context.Background(), req)
	if err != nil {
		t.Fatalf("first ExecuteOrResume() error = %v", err)
	}
	second, err := engine.ExecuteOrResume(context.Background(), req)
	if err != nil {
		t.Fatalf("second ExecuteOrResume() error = %v", err)
	}
	if first.ID != second.ID {
		t.Fatalf("expected same workflow run, got %s and %s", first.ID, second.ID)
	}
	if calls != 1 {
		t.Fatalf("expected handler to run once, ran %d times", calls)
	}
	runs, err := store.ListWorkflowRuns(context.Background(), WorkflowRunFilter{Name: "demo"})
	if err != nil {
		t.Fatalf("ListWorkflowRuns() error = %v", err)
	}
	if len(runs) != 1 || !runs[0].Recoverable || runs[0].IdempotencyKey != "req-1" {
		t.Fatalf("unexpected idempotent workflow runs: %#v", runs)
	}
}

func TestWorkflowEngineResumeSkipsSucceededSteps(t *testing.T) {
	expected := errors.New("temporary failure")
	store := NewMemoryWorkflowStore()
	engine := NewWorkflowEngine(store, NoopWorkflowEventSink{})
	firstCalls := 0
	secondCalls := 0
	engine.RegisterStepHandler("first", func(ctx context.Context, run *WorkflowRun, input map[string]any) (map[string]any, error) {
		firstCalls++
		return map[string]any{"first_done": true}, nil
	})
	engine.RegisterStepHandler("second", func(ctx context.Context, run *WorkflowRun, input map[string]any) (map[string]any, error) {
		secondCalls++
		if secondCalls == 1 {
			return nil, expected
		}
		if input["first_done"] != true {
			t.Fatalf("expected resumed state to include first step output, got %#v", input)
		}
		return map[string]any{"answer": "ok"}, nil
	})
	definition := WorkflowDefinition{
		Name:    "demo",
		Version: "v1",
		Steps:   []WorkflowStepDefinition{{Name: "first"}, {Name: "second"}},
	}
	run, err := engine.ExecuteOrResume(context.Background(), WorkflowRequest{
		Definition:     definition,
		UserID:         "alice",
		IdempotencyKey: "req-retry",
		Recoverable:    true,
	})
	if !errors.Is(err, expected) {
		t.Fatalf("expected first run failure, got %v", err)
	}
	if run == nil || run.Status != WorkflowStatusFailed {
		t.Fatalf("unexpected failed run: %#v", run)
	}
	resumed, err := engine.Resume(context.Background(), run.ID, definition)
	if err != nil {
		t.Fatalf("Resume() error = %v", err)
	}
	if resumed.Status != WorkflowStatusSucceeded || resumed.State["answer"] != "ok" {
		t.Fatalf("unexpected resumed run: %#v", resumed)
	}
	if firstCalls != 1 || secondCalls != 2 {
		t.Fatalf("unexpected handler calls: first=%d second=%d", firstCalls, secondCalls)
	}
	steps, err := store.ListWorkflowStepRuns(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("ListWorkflowStepRuns() error = %v", err)
	}
	if len(steps) != 2 {
		t.Fatalf("expected two persisted steps, got %#v", steps)
	}
	if steps[0].StepIndex != 0 || steps[0].Attempt != 1 || steps[0].Status != WorkflowStepStatusSucceeded {
		t.Fatalf("unexpected first step after resume: %#v", steps[0])
	}
	if steps[1].StepIndex != 1 || steps[1].Attempt != 2 || steps[1].Status != WorkflowStepStatusSucceeded {
		t.Fatalf("unexpected second step after resume: %#v", steps[1])
	}
}

func TestMemoryWorkflowStoreListsRunsWithFilters(t *testing.T) {
	store := NewMemoryWorkflowStore()
	engine := NewWorkflowEngine(store, NoopWorkflowEventSink{})
	engine.RegisterStepHandler("ok", func(context.Context, *WorkflowRun, map[string]any) (map[string]any, error) {
		return map[string]any{"result_count": 1}, nil
	})
	aliceRun, err := engine.Execute(context.Background(), WorkflowRequest{
		Definition: WorkflowDefinition{Name: "rag_search", Version: "v1", Steps: []WorkflowStepDefinition{{Name: "ok"}}},
		UserID:     "alice",
		SessionID:  "session-1",
	})
	if err != nil {
		t.Fatalf("execute alice workflow: %v", err)
	}
	if _, err := engine.Execute(context.Background(), WorkflowRequest{
		Definition: WorkflowDefinition{Name: "skill_execution", Version: "v1", Steps: []WorkflowStepDefinition{{Name: "ok"}}},
		UserID:     "bob",
		SessionID:  "session-2",
	}); err != nil {
		t.Fatalf("execute bob workflow: %v", err)
	}

	runs, err := store.ListWorkflowRuns(context.Background(), WorkflowRunFilter{
		UserID: "alice",
		Name:   "rag_search",
		Status: WorkflowStatusSucceeded,
		Limit:  10,
	})
	if err != nil {
		t.Fatalf("list workflow runs: %v", err)
	}
	if len(runs) != 1 || runs[0].ID != aliceRun.ID {
		t.Fatalf("unexpected filtered workflow runs: %#v", runs)
	}
}

func TestRuntimeSetWorkflowStoreRebindsMemoryWorkflow(t *testing.T) {
	runtime := NewRuntime(
		RuntimeConfig{},
		NewFileSessionStore(t.TempDir()),
		NewFileMemoryService(t.TempDir()),
		nil,
		func(Scope) Runner { return echoRunner{} },
	)
	store := NewMemoryWorkflowStore()
	runtime.SetWorkflowStore(store)
	session, err := runtime.CreateSession(context.Background(), "alice", "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	session.AddUserMessage("remember project alpha")
	session.AddAssistantMessage("noted")
	if err := runtime.memory.AfterTurn(context.Background(), "alice", session); err != nil {
		t.Fatalf("AfterTurn() error = %v", err)
	}
	runs, err := store.ListWorkflowRuns(context.Background(), WorkflowRunFilter{Name: memoryUpdateWorkflowName})
	if err != nil {
		t.Fatalf("ListWorkflowRuns() error = %v", err)
	}
	if len(runs) != 1 || runs[0].Status != WorkflowStatusSucceeded {
		t.Fatalf("expected memory workflow in rebound store, got %#v", runs)
	}
}

func TestRuntimeSetWorkflowStoreRebindsRAGWorkflow(t *testing.T) {
	runtime := NewRuntime(
		RuntimeConfig{MessageSearch: MessageSearchConfig{Backend: messageSearchBackendHybrid}},
		NewFileSessionStore(t.TempDir()),
		nil,
		nil,
		func(Scope) Runner { return echoRunner{} },
	)
	store := NewMemoryWorkflowStore()
	runtime.SetWorkflowStore(store)
	if _, err := runtime.SearchMessages(context.Background(), "alice", "postgres timeout", 5, 0); err != nil {
		t.Fatalf("SearchMessages() error = %v", err)
	}
	runs, err := store.ListWorkflowRuns(context.Background(), WorkflowRunFilter{Name: ragSearchWorkflowName})
	if err != nil {
		t.Fatalf("ListWorkflowRuns() error = %v", err)
	}
	if len(runs) != 1 || runs[0].Status != WorkflowStatusSucceeded {
		t.Fatalf("expected rag workflow in rebound store, got %#v", runs)
	}
	steps, err := store.ListWorkflowStepRuns(context.Background(), runs[0].ID)
	if err != nil {
		t.Fatalf("ListWorkflowStepRuns() error = %v", err)
	}
	if len(steps) != len(ragSearchWorkflowDefinition().Steps) {
		t.Fatalf("unexpected rag workflow steps: %#v", steps)
	}
}
