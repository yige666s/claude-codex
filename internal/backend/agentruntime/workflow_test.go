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
