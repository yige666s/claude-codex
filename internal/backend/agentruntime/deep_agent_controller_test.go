package agentruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"claude-codex/internal/harness/engine"
	"claude-codex/internal/harness/skills"
	"claude-codex/internal/harness/state"
)

func TestDeepAgentControllerCompletesAndPersistsCheckpoints(t *testing.T) {
	store := NewMemoryWorkflowStore()
	controller := NewDeepAgentController(
		store,
		NoopWorkflowEventSink{},
		staticDeepAgentPlanner{plan: DeepAgentPlan{Steps: []DeepAgentStep{
			{ID: "research", Title: "Research", DoneCondition: "done"},
			{ID: "write", Title: "Write", DoneCondition: "done"},
		}}},
		completingDeepAgentExecutor{},
		nil,
	)
	result, err := controller.Execute(context.Background(), DeepAgentTaskRequest{
		UserID:    "alice",
		SessionID: "session-1",
		Goal:      "prepare report",
		Policy:    DeepAgentPolicy{MaxSteps: 4, MaxActions: 4, NoProgressLimit: 2, MaxDuration: time.Minute},
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.State == nil || result.State.Status != DeepAgentRunStatusSucceeded {
		t.Fatalf("unexpected deep agent state: %#v", result.State)
	}
	if result.State.ActionCount != 2 || len(result.State.CompletedSteps) != 2 {
		t.Fatalf("unexpected action/completion counts: %#v", result.State)
	}
	runs, err := store.ListWorkflowRuns(context.Background(), WorkflowRunFilter{Name: deepAgentTaskWorkflowName, Status: WorkflowStatusSucceeded})
	if err != nil {
		t.Fatalf("ListWorkflowRuns() error = %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("expected one deep agent workflow, got %#v", runs)
	}
	if runs[0].State["deep_agent_status"] != DeepAgentRunStatusSucceeded {
		t.Fatalf("expected persisted deep agent status, got %#v", runs[0].State)
	}
	steps, err := store.ListWorkflowStepRuns(context.Background(), runs[0].ID)
	if err != nil {
		t.Fatalf("ListWorkflowStepRuns() error = %v", err)
	}
	if len(steps) != len(deepAgentTaskWorkflowDefinition(time.Minute).Steps) {
		t.Fatalf("unexpected workflow steps: %#v", steps)
	}
}

func TestDeepAgentControllerEmitsActionDetailEvents(t *testing.T) {
	store := NewMemoryWorkflowStore()
	controller := NewDeepAgentController(
		store,
		NoopWorkflowEventSink{},
		staticDeepAgentPlanner{plan: DeepAgentPlan{Steps: []DeepAgentStep{{
			ID:            "write",
			Title:         "Write report",
			DoneCondition: "skill reports completed",
			Metadata: map[string]any{
				"tool": "skill",
				"args": map[string]any{"skill_name": "docx", "args": "write report"},
			},
		}}}},
		completingDeepAgentExecutor{},
		nil,
	)
	var events []Event
	ctx := withJobEventEmitter(context.Background(), func(_ context.Context, event Event) error {
		events = append(events, event)
		return nil
	})
	_, err := controller.Execute(ctx, DeepAgentTaskRequest{
		UserID:    "alice",
		SessionID: "session-1",
		JobID:     "job-1",
		Goal:      "prepare report",
		Policy:    DeepAgentPolicy{MaxSteps: 2, MaxActions: 2, NoProgressLimit: 2, MaxDuration: time.Minute},
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	var started, succeeded map[string]any
	for _, event := range events {
		switch event.Type {
		case "deep_agent_action_started":
			if err := json.Unmarshal(event.Data, &started); err != nil {
				t.Fatalf("unmarshal started event: %v", err)
			}
		case "deep_agent_action_succeeded":
			if err := json.Unmarshal(event.Data, &succeeded); err != nil {
				t.Fatalf("unmarshal succeeded event: %v", err)
			}
		}
	}
	if started == nil || succeeded == nil {
		t.Fatalf("expected action detail events, got %#v", events)
	}
	if started["step_id"] != "write" || started["tool"] != "skill" || started["skill_name"] != "docx" {
		t.Fatalf("unexpected started detail: %#v", started)
	}
	if succeeded["result_status"] != DeepAgentActionStatusSucceeded {
		t.Fatalf("unexpected succeeded detail: %#v", succeeded)
	}
}

func TestDeepAgentControllerBlocksRepeatedAction(t *testing.T) {
	store := NewMemoryWorkflowStore()
	controller := NewDeepAgentController(
		store,
		NoopWorkflowEventSink{},
		repeatingDeepAgentPlanner{},
		emptyDeepAgentExecutor{},
		nil,
	)
	result, err := controller.Execute(context.Background(), DeepAgentTaskRequest{
		UserID: "alice",
		Goal:   "looping task",
		Policy: DeepAgentPolicy{MaxSteps: 2, MaxActions: 4, NoProgressLimit: 2, MaxDuration: time.Minute},
	})
	if !errors.Is(err, ErrDeepAgentBlocked) {
		t.Fatalf("Execute() error = %v, want blocked", err)
	}
	if result == nil || result.State == nil || result.State.Status != DeepAgentRunStatusBlocked {
		t.Fatalf("unexpected blocked result: %#v", result)
	}
	if result.State.ActionCount != 1 {
		t.Fatalf("expected repeated action to be blocked before second execution, got %#v", result.State)
	}
	if result.State.Blocker == "" {
		t.Fatalf("expected blocker reason, got %#v", result.State)
	}
	runs, err := store.ListWorkflowRuns(context.Background(), WorkflowRunFilter{Name: deepAgentTaskWorkflowName, Status: WorkflowStatusFailed})
	if err != nil {
		t.Fatalf("ListWorkflowRuns() error = %v", err)
	}
	if len(runs) != 1 || runs[0].State["deep_agent_status"] != DeepAgentRunStatusBlocked {
		t.Fatalf("expected failed workflow with blocked deep agent state, got %#v", runs)
	}
}

func TestDeepAgentControllerStopsAtActionBudget(t *testing.T) {
	store := NewMemoryWorkflowStore()
	controller := NewDeepAgentController(
		store,
		NoopWorkflowEventSink{},
		countingDeepAgentPlanner{},
		emptyDeepAgentExecutor{},
		nil,
	)
	result, err := controller.Execute(context.Background(), DeepAgentTaskRequest{
		UserID: "alice",
		Goal:   "budgeted task",
		Policy: DeepAgentPolicy{MaxSteps: 2, MaxActions: 1, NoProgressLimit: 5, MaxDuration: time.Minute},
	})
	if !errors.Is(err, ErrDeepAgentBudgetExceeded) {
		t.Fatalf("Execute() error = %v, want budget exceeded", err)
	}
	if result == nil || result.State == nil || result.State.Status != DeepAgentRunStatusBudgetExceeded {
		t.Fatalf("unexpected budget result: %#v", result)
	}
	if result.State.ActionCount != 1 {
		t.Fatalf("expected one executed action, got %#v", result.State)
	}
}

func TestRuntimeExecuteDeepAgentTaskUsesWorkflowStore(t *testing.T) {
	runtime := NewRuntime(
		RuntimeConfig{},
		NewFileSessionStore(t.TempDir()),
		nil,
		nil,
		func(Scope) Runner { return echoRunner{} },
	)
	store := NewMemoryWorkflowStore()
	runtime.SetWorkflowStore(store)
	if _, err := runtime.ExecuteDeepAgentTask(context.Background(), DeepAgentTaskRequest{
		UserID: "alice",
		Goal:   "runtime deep task",
	}, staticDeepAgentPlanner{plan: DeepAgentPlan{Steps: []DeepAgentStep{{ID: "only", Title: "Only", DoneCondition: "done"}}}}, completingDeepAgentExecutor{}, nil); err != nil {
		t.Fatalf("ExecuteDeepAgentTask() error = %v", err)
	}
	if !memoryWorkflowStoreHasRun(t, store, deepAgentTaskWorkflowName, WorkflowStatusSucceeded) {
		t.Fatalf("expected deep agent workflow in runtime store")
	}
}

func TestRuntimeDeepAgentPlannerCreatesStructuredPlan(t *testing.T) {
	runtime := NewRuntime(
		RuntimeConfig{},
		NewFileSessionStore(t.TempDir()),
		nil,
		nil,
		func(Scope) Runner { return deepAgentPlanJSONRunner{} },
	)
	plan, err := NewRuntimeDeepAgentPlanner(runtime).CreatePlan(context.Background(), DeepAgentTaskRequest{
		UserID: "alice",
		Goal:   "summarize previous postgres issue",
		Policy: DeepAgentPolicy{MaxSteps: 3},
	})
	if err != nil {
		t.Fatalf("CreatePlan() error = %v", err)
	}
	if len(plan.Steps) != 2 {
		t.Fatalf("expected 2 planned steps, got %#v", plan)
	}
	if plan.Steps[0].DoneCondition == "" || plan.Steps[0].Intent == "" || deepAgentWorkflowString(plan.Steps[0].Metadata, "tool") != "" {
		t.Fatalf("unexpected first step: %#v", plan.Steps[0])
	}
}

func TestRuleDeepAgentPlannerVariesRetryAction(t *testing.T) {
	state := &DeepAgentState{
		Goal: "write report",
		ActionHistory: []DeepAgentAction{{
			StepID: "write",
			Tool:   "model",
			Args:   map[string]any{"prompt": "Write the report"},
		}},
	}
	step := DeepAgentStep{
		ID:            "write",
		Title:         "Write report",
		DoneCondition: "report is complete",
		Metadata:      map[string]any{"tool": "model", "args": map[string]any{"prompt": "Write the report"}},
	}
	action, err := ruleDeepAgentPlanner{}.NextAction(context.Background(), state, step)
	if err != nil {
		t.Fatalf("NextAction() error = %v", err)
	}
	if got := deepAgentAnyInt(action.Args["attempt"], -1); got != 2 {
		t.Fatalf("expected retry attempt 2, got %#v", action.Args)
	}
	if !strings.Contains(deepAgentWorkflowString(action.Args, "prompt"), "Retry instruction") {
		t.Fatalf("expected retry prompt, got %#v", action.Args)
	}
	if deepAgentActionHash(action) == deepAgentActionHash(state.ActionHistory[0]) {
		t.Fatalf("retry action hash should differ from previous action")
	}
}

func TestRuntimeDeepAgentSkillActionReportsArtifactCount(t *testing.T) {
	runtime := NewRuntime(
		RuntimeConfig{},
		NewFileSessionStore(t.TempDir()),
		nil,
		nil,
		func(Scope) Runner { return artifactReportingRunner{} },
	)
	runtime.skills = fakeSkillCatalog{skills: []*skills.SkillDefinition{{
		Name:          "report",
		UserInvocable: true,
		Metadata:      map[string]any{"produces_artifacts": true},
		GetPrompt: func(args string, _ *skills.SkillContext) ([]skills.ContentBlock, error) {
			return []skills.ContentBlock{{Type: "text", Text: "write report " + args}}, nil
		},
	}}}
	session, err := runtime.CreateSession(context.Background(), "alice", "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	result, err := NewRuntimeDeepAgentExecutor(runtime).ExecuteDeepAgentAction(context.Background(), DeepAgentAction{
		StepID: "write",
		Tool:   "skill",
		Args:   map[string]any{"skill_name": "report", "args": "Tolan AI"},
	}, &DeepAgentState{WorkingMemory: map[string]any{"user_id": "alice", "session_id": session.ID}})
	if err != nil {
		t.Fatalf("ExecuteDeepAgentAction() error = %v", err)
	}
	if result.Status != DeepAgentActionStatusSucceeded || !result.Completed {
		t.Fatalf("unexpected skill action result: %#v", result)
	}
	if got := deepAgentAnyInt(result.Metadata["artifact_count"], -1); got != 1 {
		t.Fatalf("artifact_count = %d, want 1 in %#v", got, result.Metadata)
	}
	if ok, _ := deepAgentMetadataBool(result.Metadata, "tool_result_valid"); !ok {
		t.Fatalf("expected valid tool result metadata, got %#v", result.Metadata)
	}
}

func TestRuntimeDeepAgentPlannerPromptIncludesSkillCatalog(t *testing.T) {
	runtime := NewRuntime(
		RuntimeConfig{},
		NewFileSessionStore(t.TempDir()),
		nil,
		nil,
		func(Scope) Runner { return deepAgentPlanJSONRunner{} },
	)
	runtime.skills = fakeSkillCatalog{skills: []*skills.SkillDefinition{{
		Name:          "docx",
		Description:   "Create documents and research reports. Triggers include: 生成文档, 调研报告, write report",
		WhenToUse:     "Use when the user needs a downloadable research report document.",
		UserInvocable: true,
		RunAsJob:      true,
		Metadata:      map[string]any{"produces_artifacts": true},
	}}}
	prompt := NewRuntimeDeepAgentPlanner(runtime).deepAgentPlannerPrompt(DeepAgentTaskRequest{
		UserID: "alice",
		Goal:   "帮我深入调查一下 Tolan AI，并生成一个完整调研报告文档",
		Policy: DeepAgentPolicy{MaxSteps: 4},
	})
	for _, want := range []string{
		"Published skills are available later to the Step Router",
		"name: docx",
		"produces_artifacts: true",
		"Do not put metadata.tool",
		"depends_on",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("planner prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestRuntimeDeepAgentRouterSelectsSkillForArtifactStep(t *testing.T) {
	runtime := NewRuntime(
		RuntimeConfig{},
		NewFileSessionStore(t.TempDir()),
		nil,
		nil,
		func(Scope) Runner { return skillSelectingDeepAgentPlanRunner{} },
	)
	runtime.skills = fakeSkillCatalog{skills: []*skills.SkillDefinition{{
		Name:          "docx",
		Description:   "Create documents and research reports.",
		UserInvocable: true,
		RunAsJob:      true,
		Metadata:      map[string]any{"produces_artifacts": true},
	}}}
	plan, err := NewRuntimeDeepAgentPlanner(runtime).CreatePlan(context.Background(), DeepAgentTaskRequest{
		UserID: "alice",
		Goal:   "帮我深入调查一下 Tolan AI，并生成一个完整调研报告文档",
		Policy: DeepAgentPolicy{MaxSteps: 4},
	})
	if err != nil {
		t.Fatalf("CreatePlan() error = %v", err)
	}
	if len(plan.Steps) == 0 {
		t.Fatalf("empty plan: %#v", plan)
	}
	final := plan.Steps[len(plan.Steps)-1]
	state := &DeepAgentState{
		Goal: "帮我深入调查一下 Tolan AI，并生成一个完整调研报告文档",
		Plan: plan,
		WorkingMemory: map[string]any{
			"step_context": map[string]any{
				"search": map[string]any{"summary": "Tolan AI is a consumer AI companion product."},
			},
		},
	}
	action, err := NewRuntimeDeepAgentPlanner(runtime).NextAction(context.Background(), state, final)
	if err != nil {
		t.Fatalf("NextAction() error = %v", err)
	}
	if action.Tool != "skill" {
		t.Fatalf("action tool = %q, want skill: %#v", action.Tool, action)
	}
	args := action.Args
	if got := deepAgentWorkflowString(args, "skill_name"); got != "docx" {
		t.Fatalf("skill = %q, want docx in %#v", got, action.Args)
	}
	if got := deepAgentWorkflowString(args, "args"); !strings.Contains(got, "Tolan AI") {
		t.Fatalf("skill args should include context and intent, got %#v", args)
	}
}

func TestRuntimeDeepAgentChildJobArtifactCount(t *testing.T) {
	runtime := NewRuntime(RuntimeConfig{}, NewFileSessionStore(t.TempDir()), nil, nil, func(Scope) Runner { return echoRunner{} })
	runtime.SetArtifactService(NewArtifactService(newMemoryArtifactStore(), NewFileObjectStore(t.TempDir()), "artifacts"))
	session, err := runtime.CreateSession(context.Background(), "alice", "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	job := &Job{ID: "job-doc", UserID: "alice", SessionID: session.ID}
	if _, err := runtime.artifacts.CreateWithJob(context.Background(), AssetKindArtifact, "alice", session.ID, job.ID, "report.md", "text/markdown", []byte("# report")); err != nil {
		t.Fatalf("create artifact: %v", err)
	}
	if got := NewRuntimeDeepAgentExecutor(runtime).deepAgentChildJobArtifactCount(context.Background(), job); got != 1 {
		t.Fatalf("artifact count = %d, want 1", got)
	}
}

func TestParseDeepAgentPlanValidatesStructuredSchema(t *testing.T) {
	_, err := parseDeepAgentPlan(`{
  "goal": "search",
  "steps": [
    {
      "id": "search",
      "title": "Search",
      "intent": "Search relevant prior messages",
      "done_condition": "results are available",
      "risk_level": "low",
      "metadata": {"tool": "rag_search", "args": {"query": "postgres"}}
    }
  ]
}`)
	if err == nil || !strings.Contains(err.Error(), "must not select metadata.tool") {
		t.Fatalf("expected plan-time tool selection validation error, got %v", err)
	}

	_, err = parseDeepAgentPlan(`{"goal":"x","steps":[{"id":"s","title":"S","intent":"Do S","done_condition":"done","risk_level":"critical"}]}`)
	if err == nil || !strings.Contains(err.Error(), "risk_level") {
		t.Fatalf("expected risk_level enum validation error, got %v", err)
	}
}

func TestRuntimeDeepAgentPlannerRepairsInvalidStructuredPlan(t *testing.T) {
	runner := &repairingDeepAgentPlanRunner{}
	runtime := NewRuntime(
		RuntimeConfig{},
		NewFileSessionStore(t.TempDir()),
		nil,
		nil,
		func(Scope) Runner { return runner },
	)
	plan, err := NewRuntimeDeepAgentPlanner(runtime).CreatePlan(context.Background(), DeepAgentTaskRequest{
		UserID: "alice",
		Goal:   "search postgres issue",
		Policy: DeepAgentPolicy{MaxSteps: 2},
	})
	if err != nil {
		t.Fatalf("CreatePlan() error = %v", err)
	}
	if runner.calls != 2 {
		t.Fatalf("expected initial planner call plus repair call, got %d", runner.calls)
	}
	if len(plan.Steps) != 1 || plan.Steps[0].Intent == "" || deepAgentWorkflowString(plan.Steps[0].Metadata, "tool") != "" {
		t.Fatalf("expected repaired intent-only plan, got %#v", plan)
	}
}

func TestRuntimeDeepAgentPlannerFallsBackToRulePlannerAfterRepairFailure(t *testing.T) {
	runtime := NewRuntime(
		RuntimeConfig{},
		NewFileSessionStore(t.TempDir()),
		nil,
		nil,
		func(Scope) Runner { return invalidDeepAgentPlanRunner{} },
	)
	plan, err := NewRuntimeDeepAgentPlanner(runtime).CreatePlan(context.Background(), DeepAgentTaskRequest{
		UserID: "alice",
		Goal:   "finish the report",
	})
	if err != nil {
		t.Fatalf("CreatePlan() should fall back to rule planner, got %v", err)
	}
	if len(plan.Steps) != 1 || plan.Steps[0].Title != "finish the report" {
		t.Fatalf("unexpected fallback plan: %#v", plan)
	}
}

func TestRuntimeDeepAgentExecutorRoutesRAGAndModel(t *testing.T) {
	store := NewFileSessionStore(t.TempDir())
	runtime := NewRuntime(
		RuntimeConfig{},
		store,
		nil,
		nil,
		func(Scope) Runner { return echoRunner{} },
	)
	session, err := runtime.CreateSession(context.Background(), "alice", "")
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	session.AddUserMessage("postgres timeout happened yesterday")
	if err := store.Save(context.Background(), "alice", session); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	plan := DeepAgentPlan{Steps: []DeepAgentStep{
		{
			ID:            "search",
			Title:         "Search history",
			DoneCondition: "search returns related messages",
			Metadata: map[string]any{
				"tool": "rag_search",
				"args": map[string]any{"query": "postgres timeout", "limit": 3},
			},
		},
		{
			ID:            "answer",
			Title:         "Summarize",
			DoneCondition: "model produces summary",
			Metadata: map[string]any{
				"tool": "model",
				"args": map[string]any{"prompt": "Summarize the retrieved postgres issue."},
			},
		},
	}}
	result, err := runtime.ExecuteDeepAgentTask(context.Background(), DeepAgentTaskRequest{
		UserID:    "alice",
		SessionID: session.ID,
		Goal:      "summarize previous postgres issue",
		Plan:      plan,
		Policy:    DeepAgentPolicy{MaxSteps: 3, MaxActions: 3, MaxDuration: time.Minute},
	}, staticDeepAgentPlanner{plan: plan}, nil, nil)
	if err != nil {
		t.Fatalf("ExecuteDeepAgentTask() error = %v", err)
	}
	if result.State.ActionCount != 2 || result.State.Status != DeepAgentRunStatusSucceeded {
		t.Fatalf("unexpected deep agent state: %#v", result.State)
	}
	loaded, err := store.Get(context.Background(), "alice", session.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if len(loaded.Messages) <= len(session.Messages) {
		t.Fatalf("expected model executor to persist assistant output, got %#v", loaded.Messages)
	}
}

func TestRuntimeDeepAgentExecutorRoutesSkillWorkflow(t *testing.T) {
	store := NewFileSessionStore(t.TempDir())
	catalog := fakeSkillCatalog{skills: []*skills.SkillDefinition{{
		Name:          "demo",
		UserInvocable: true,
		GetPrompt: func(_ string, _ *skills.SkillContext) ([]skills.ContentBlock, error) {
			return []skills.ContentBlock{{Type: "text", Text: "demo prompt"}}, nil
		},
	}}}
	runtime := NewRuntime(
		RuntimeConfig{DefaultWorkingDir: t.TempDir()},
		store,
		nil,
		catalog,
		func(Scope) Runner { return echoRunner{} },
	)
	session, err := runtime.CreateSession(context.Background(), "alice", "")
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	plan := DeepAgentPlan{Steps: []DeepAgentStep{{
		ID:            "skill",
		Title:         "Run demo skill",
		DoneCondition: "skill returns output",
		Metadata: map[string]any{
			"tool": "skill",
			"args": map[string]any{"skill_name": "demo", "args": "hello"},
		},
	}}}
	if _, err := runtime.ExecuteDeepAgentTask(context.Background(), DeepAgentTaskRequest{
		UserID:    "alice",
		SessionID: session.ID,
		Goal:      "run demo skill",
		Plan:      plan,
	}, staticDeepAgentPlanner{plan: plan}, nil, nil); err != nil {
		t.Fatalf("ExecuteDeepAgentTask() error = %v", err)
	}
	if !memoryWorkflowStoreHasRun(t, runtime.workflowStore, skillExecutionWorkflowName, WorkflowStatusSucceeded) {
		t.Fatalf("expected skill workflow to be routed through runtime executor")
	}
}

func TestRuleDeepAgentVerifierChecksStructuredConditions(t *testing.T) {
	step := DeepAgentStep{
		ID:            "verify",
		Title:         "Verify result",
		DoneCondition: "fields, citations, tests and artifact are present",
		Metadata: map[string]any{
			"verification": map[string]any{
				"require_tool_result_valid": true,
				"require_output":            true,
				"required_fields":           []any{"answer", "evidence.url"},
				"min_artifact_count":        1,
				"require_tests_passed":      true,
				"min_citations":             2,
			},
		},
	}
	progress, err := ruleDeepAgentVerifier{}.CheckProgress(context.Background(), nil, step, DeepAgentAction{}, DeepAgentActionResult{
		Status:    DeepAgentActionStatusSucceeded,
		Completed: true,
		Output:    `{"answer":"ok","evidence":{"url":"https://example.com"}}`,
		Metadata: map[string]any{
			"artifact_count":    1,
			"tests_passed":      true,
			"citation_count":    2,
			"tool_result_valid": true,
		},
	})
	if err != nil {
		t.Fatalf("CheckProgress() error = %v", err)
	}
	if !progress.StepDone || !progress.MadeProgress {
		t.Fatalf("expected verified progress, got %#v", progress)
	}

	progress, err = ruleDeepAgentVerifier{}.CheckProgress(context.Background(), nil, step, DeepAgentAction{}, DeepAgentActionResult{
		Status:    DeepAgentActionStatusSucceeded,
		Completed: true,
		Output:    `{"answer":"ok"}`,
		Metadata: map[string]any{
			"artifact_count":    1,
			"tests_passed":      true,
			"citation_count":    2,
			"tool_result_valid": true,
		},
	})
	if err != nil {
		t.Fatalf("CheckProgress() missing field error = %v", err)
	}
	if progress.StepDone || progress.MadeProgress || !strings.Contains(progress.Reason, "evidence.url") {
		t.Fatalf("expected missing field verification failure, got %#v", progress)
	}
}

func TestDeepAgentControllerResumeContinuesCheckpointedRun(t *testing.T) {
	store := NewMemoryWorkflowStore()
	initial := NewDeepAgentController(
		store,
		NoopWorkflowEventSink{},
		countingDeepAgentPlanner{},
		emptyDeepAgentExecutor{},
		nil,
	)
	failed, err := initial.Execute(context.Background(), DeepAgentTaskRequest{
		UserID: "alice",
		Goal:   "resume task",
		Policy: DeepAgentPolicy{MaxSteps: 1, MaxActions: 1, NoProgressLimit: 5, MaxDuration: time.Minute},
	})
	if !errors.Is(err, ErrDeepAgentBudgetExceeded) {
		t.Fatalf("Execute() error = %v, want budget exceeded", err)
	}
	if failed == nil || failed.Run == nil {
		t.Fatalf("expected failed workflow result, got %#v", failed)
	}

	resumer := NewDeepAgentController(
		store,
		NoopWorkflowEventSink{},
		countingDeepAgentPlanner{},
		completingDeepAgentExecutor{},
		nil,
	)
	resumed, err := resumer.Resume(context.Background(), DeepAgentResumeRequest{
		RunID:  failed.Run.ID,
		Policy: DeepAgentPolicy{MaxSteps: 1, MaxActions: 3, NoProgressLimit: 2, MaxDuration: time.Minute},
	})
	if err != nil {
		t.Fatalf("Resume() error = %v", err)
	}
	if resumed.State.Status != DeepAgentRunStatusSucceeded || len(resumed.State.CompletedSteps) != 1 {
		t.Fatalf("unexpected resumed state: %#v", resumed.State)
	}
	loaded, err := store.GetWorkflowRun(context.Background(), failed.Run.ID)
	if err != nil {
		t.Fatalf("GetWorkflowRun() error = %v", err)
	}
	if loaded.Status != WorkflowStatusSucceeded || loaded.State["deep_agent_status"] != DeepAgentRunStatusSucceeded {
		t.Fatalf("expected original run to be resumed successfully, got %#v", loaded)
	}
	steps, err := store.ListWorkflowStepRuns(context.Background(), failed.Run.ID)
	if err != nil {
		t.Fatalf("ListWorkflowStepRuns() error = %v", err)
	}
	if len(steps) <= len(deepAgentTaskWorkflowDefinition(time.Minute).Steps) {
		t.Fatalf("expected resume steps appended, got %#v", steps)
	}
}

func TestRuntimeDeepAgentHighRiskActionCreatesPendingReview(t *testing.T) {
	risk := NewMemoryRiskStore()
	runtime := NewRuntime(
		RuntimeConfig{},
		NewFileSessionStore(t.TempDir()),
		nil,
		nil,
		func(Scope) Runner { return echoRunner{} },
	)
	runtime.SetRiskRecorder(func(ctx context.Context, event RiskEvent) {
		if err := risk.RecordRiskEvent(ctx, event); err != nil {
			t.Errorf("RecordRiskEvent() error = %v", err)
		}
	})
	plan := DeepAgentPlan{Steps: []DeepAgentStep{{
		ID:            "delete",
		Title:         "Delete production data",
		DoneCondition: "approved destructive action completed",
		RiskLevel:     RiskLevelHigh,
		Metadata: map[string]any{
			"tool":        "model",
			"risk_reason": "destructive production action requires human review",
			"args":        map[string]any{"prompt": "delete production data"},
		},
	}}}
	result, err := runtime.ExecuteDeepAgentTask(context.Background(), DeepAgentTaskRequest{
		UserID: "alice",
		Goal:   "delete production data",
		Plan:   plan,
	}, staticDeepAgentPlanner{plan: plan}, completingDeepAgentExecutor{}, nil)
	if !errors.Is(err, ErrDeepAgentReviewRequired) {
		t.Fatalf("ExecuteDeepAgentTask() error = %v, want review required", err)
	}
	if result == nil || result.State == nil || result.State.Status != DeepAgentRunStatusReviewPending {
		t.Fatalf("unexpected review result: %#v", result)
	}
	reviews, err := risk.ListRiskReviews(context.Background(), RiskReviewFilter{Status: RiskReviewStatusPending, Limit: 10})
	if err != nil {
		t.Fatalf("ListRiskReviews() error = %v", err)
	}
	if len(reviews.Items) != 1 || reviews.Items[0].Operation != RiskOperationDeepAgentAction {
		t.Fatalf("expected pending deep agent risk review, got %#v", reviews.Items)
	}
}

func TestRuntimeDeepAgentPersistsLearningCandidateMemory(t *testing.T) {
	memory := NewFileMemoryService(t.TempDir())
	runtime := NewRuntime(
		RuntimeConfig{},
		NewFileSessionStore(t.TempDir()),
		memory,
		nil,
		func(Scope) Runner { return echoRunner{} },
	)
	plan := DeepAgentPlan{Steps: []DeepAgentStep{{
		ID:            "finish",
		Title:         "Finish task",
		DoneCondition: "done",
	}}}
	result, err := runtime.ExecuteDeepAgentTask(context.Background(), DeepAgentTaskRequest{
		UserID: "alice",
		Goal:   "learn successful path",
		Plan:   plan,
	}, staticDeepAgentPlanner{plan: plan}, completingDeepAgentExecutor{}, nil)
	if err != nil {
		t.Fatalf("ExecuteDeepAgentTask() error = %v", err)
	}
	if result.State == nil || len(result.State.Learnings) != 1 {
		t.Fatalf("expected learning candidate in state, got %#v", result.State)
	}
	items, err := runtime.ListMemoryItems(context.Background(), "alice", MemoryItemFilter{Status: MemoryStatusPendingConfirm})
	if err != nil {
		t.Fatalf("ListMemoryItems() error = %v", err)
	}
	if len(items) != 1 || items[0].Metadata["deep_agent_learning"] != true {
		t.Fatalf("expected pending deep agent memory candidate, got %#v", items)
	}
}

func TestDeepAgentWorkflowSummaryFromRun(t *testing.T) {
	store := NewMemoryWorkflowStore()
	controller := NewDeepAgentController(
		store,
		NoopWorkflowEventSink{},
		staticDeepAgentPlanner{plan: DeepAgentPlan{Steps: []DeepAgentStep{{ID: "only", Title: "Only", DoneCondition: "done"}}}},
		completingDeepAgentExecutor{},
		nil,
	)
	result, err := controller.Execute(context.Background(), DeepAgentTaskRequest{
		UserID: "alice",
		Goal:   "summarize admin state",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	loaded, err := store.GetWorkflowRun(context.Background(), result.Run.ID)
	if err != nil {
		t.Fatalf("GetWorkflowRun() error = %v", err)
	}
	summary, ok := DeepAgentSummaryFromWorkflowRun(loaded)
	if !ok || summary == nil || !summary.Present {
		t.Fatalf("expected deep agent summary, got %#v ok=%t", summary, ok)
	}
	if summary.Goal != "summarize admin state" || summary.ActionCount != 1 || len(summary.Plan.Steps) != 1 {
		t.Fatalf("unexpected summary: %#v", summary)
	}
}

type staticDeepAgentPlanner struct {
	plan DeepAgentPlan
}

func (p staticDeepAgentPlanner) CreatePlan(_ context.Context, req DeepAgentTaskRequest) (DeepAgentPlan, error) {
	plan := p.plan
	plan.Goal = req.Goal
	return plan, nil
}

func (p staticDeepAgentPlanner) NextAction(ctx context.Context, state *DeepAgentState, step DeepAgentStep) (DeepAgentAction, error) {
	return ruleDeepAgentPlanner{}.NextAction(ctx, state, step)
}

type repeatingDeepAgentPlanner struct{}

func (repeatingDeepAgentPlanner) CreatePlan(ctx context.Context, req DeepAgentTaskRequest) (DeepAgentPlan, error) {
	return ruleDeepAgentPlanner{}.CreatePlan(ctx, req)
}

func (repeatingDeepAgentPlanner) NextAction(_ context.Context, _ *DeepAgentState, step DeepAgentStep) (DeepAgentAction, error) {
	return DeepAgentAction{
		StepID: step.ID,
		Tool:   "repeat",
		Args:   map[string]any{"same": true},
	}, nil
}

type countingDeepAgentPlanner struct{}

func (countingDeepAgentPlanner) CreatePlan(ctx context.Context, req DeepAgentTaskRequest) (DeepAgentPlan, error) {
	return ruleDeepAgentPlanner{}.CreatePlan(ctx, req)
}

func (countingDeepAgentPlanner) NextAction(_ context.Context, state *DeepAgentState, step DeepAgentStep) (DeepAgentAction, error) {
	return DeepAgentAction{
		StepID: step.ID,
		Tool:   "count",
		Args:   map[string]any{"attempt": state.ActionCount + 1},
	}, nil
}

type completingDeepAgentExecutor struct{}

func (completingDeepAgentExecutor) ExecuteDeepAgentAction(_ context.Context, action DeepAgentAction, _ *DeepAgentState) (DeepAgentActionResult, error) {
	return DeepAgentActionResult{
		Status:    DeepAgentActionStatusSucceeded,
		Output:    fmt.Sprintf("completed %s", action.StepID),
		Completed: true,
	}, nil
}

type emptyDeepAgentExecutor struct{}

func (emptyDeepAgentExecutor) ExecuteDeepAgentAction(context.Context, DeepAgentAction, *DeepAgentState) (DeepAgentActionResult, error) {
	return DeepAgentActionResult{Status: DeepAgentActionStatusSucceeded}, nil
}

type deepAgentPlanJSONRunner struct{}

func (deepAgentPlanJSONRunner) Run(ctx context.Context, session *state.Session, prompt string) (engine.Result, error) {
	return deepAgentPlanJSONRunner{}.RunGeneratedPrompt(ctx, session, prompt)
}

func (deepAgentPlanJSONRunner) RunGeneratedPrompt(_ context.Context, session *state.Session, _ string) (engine.Result, error) {
	output := `{
  "goal": "summarize previous postgres issue",
  "steps": [
    {
      "id": "search",
      "title": "Search relevant history",
      "intent": "Search relevant history for the previous postgres issue",
      "depends_on": [],
      "done_condition": "related messages are retrieved",
      "risk_level": "low"
    },
    {
      "id": "summarize",
      "title": "Summarize findings",
      "intent": "Summarize the retrieved postgres issue",
      "depends_on": ["search"],
      "done_condition": "summary includes the likely cause and next step",
      "risk_level": "low"
    }
  ]
}`
	session.AddAssistantMessage(output)
	return engine.Result{Output: output, Session: session}, nil
}

type skillSelectingDeepAgentPlanRunner struct{}

func (skillSelectingDeepAgentPlanRunner) Run(ctx context.Context, session *state.Session, prompt string) (engine.Result, error) {
	return skillSelectingDeepAgentPlanRunner{}.RunGeneratedPrompt(ctx, session, prompt)
}

func (skillSelectingDeepAgentPlanRunner) RunGeneratedPrompt(_ context.Context, session *state.Session, _ string) (engine.Result, error) {
	output := `{
  "goal": "research Tolan AI",
  "steps": [
    {
      "id": "search",
      "title": "Search Tolan AI",
      "intent": "调查 Tolan AI 产品的相关信息",
      "depends_on": [],
      "done_condition": "relevant Tolan AI facts are collected",
      "risk_level": "low"
    },
    {
      "id": "write",
      "title": "Write research document",
      "intent": "生成一份可下载的 Tolan AI 调研报告文档",
      "depends_on": ["search"],
      "done_condition": "downloadable research report artifact is available",
      "risk_level": "low"
    }
  ]
}`
	session.AddAssistantMessage(output)
	return engine.Result{Output: output, Session: session}, nil
}

type artifactReportingRunner struct{}

func (artifactReportingRunner) Run(ctx context.Context, session *state.Session, prompt string) (engine.Result, error) {
	return artifactReportingRunner{}.RunGeneratedPrompt(ctx, session, prompt)
}

func (artifactReportingRunner) RunGeneratedPrompt(_ context.Context, session *state.Session, prompt string) (engine.Result, error) {
	session.AddToolResult("artifact-call-1", ArtifactToolName, json.RawMessage(`{"filename":"report.md"}`), "created report artifact")
	session.AddAssistantMessage("report generated: " + prompt)
	return engine.Result{Output: "report generated", Session: session}, nil
}

type repairingDeepAgentPlanRunner struct {
	calls int
}

func (r *repairingDeepAgentPlanRunner) Run(ctx context.Context, session *state.Session, prompt string) (engine.Result, error) {
	return r.RunGeneratedPrompt(ctx, session, prompt)
}

func (r *repairingDeepAgentPlanRunner) RunGeneratedPrompt(_ context.Context, session *state.Session, prompt string) (engine.Result, error) {
	r.calls++
	output := `{"goal":"search postgres issue","steps":[{"id":"search","title":"Search","done_condition":"results retrieved","risk_level":"low"}]}`
	if strings.Contains(prompt, "repairing a failed structured JSON") {
		output = `{"goal":"search postgres issue","steps":[{"id":"search","title":"Search","intent":"Search prior context for the postgres issue","depends_on":[],"done_condition":"results retrieved","risk_level":"low"}]}`
	}
	session.AddAssistantMessage(output)
	return engine.Result{Output: output, Session: session}, nil
}

type invalidDeepAgentPlanRunner struct{}

func (invalidDeepAgentPlanRunner) Run(ctx context.Context, session *state.Session, prompt string) (engine.Result, error) {
	return invalidDeepAgentPlanRunner{}.RunGeneratedPrompt(ctx, session, prompt)
}

func (invalidDeepAgentPlanRunner) RunGeneratedPrompt(_ context.Context, session *state.Session, _ string) (engine.Result, error) {
	output := `not json`
	session.AddAssistantMessage(output)
	return engine.Result{Output: output, Session: session}, nil
}
