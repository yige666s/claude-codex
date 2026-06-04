package agentruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
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
			Title:         "Run skill action",
			DoneCondition: "skill action completed",
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
		Goal:      "exercise action detail events",
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

func TestRuntimeDeepAgentModelActionCreatesArtifactFallback(t *testing.T) {
	runtime := NewRuntime(
		RuntimeConfig{},
		NewFileSessionStore(t.TempDir()),
		nil,
		nil,
		func(Scope) Runner { return markdownReportRunner{} },
	)
	runtime.SetArtifactService(NewArtifactService(newMemoryArtifactStore(), NewFileObjectStore(t.TempDir()), "artifacts"))
	session, err := runtime.CreateSession(context.Background(), "alice", "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	result, err := NewRuntimeDeepAgentExecutor(runtime).ExecuteDeepAgentAction(context.Background(), DeepAgentAction{
		StepID: "step-3",
		Tool:   "model_artifact",
		Args: map[string]any{
			"user_id":          "alice",
			"session_id":       session.ID,
			"prompt":           "撰写并生成 Markdown 格式的调查报告",
			"step_title":       "撰写并生成Markdown格式的调查报告",
			"step_intent":      "生成一份 md 格式调查报告",
			"done_condition":   "Markdown report artifact is available",
			"success_criteria": "artifact count is at least 1",
		},
	}, &DeepAgentState{WorkingMemory: map[string]any{"user_id": "alice", "session_id": session.ID}})
	if err != nil {
		t.Fatalf("ExecuteDeepAgentAction() error = %v", err)
	}
	if result.Status != DeepAgentActionStatusSucceeded || !result.Completed {
		t.Fatalf("unexpected model_artifact action result: %#v", result)
	}
	if got := deepAgentAnyInt(result.Metadata["artifact_count"], -1); got != 1 {
		t.Fatalf("artifact_count = %d, want 1 in %#v", got, result.Metadata)
	}
	if fallback, _ := deepAgentMetadataBool(result.Metadata, "artifact_fallback"); !fallback {
		t.Fatalf("expected artifact fallback metadata, got %#v", result.Metadata)
	}
	artifacts, err := runtime.ListArtifacts(context.Background(), "alice", session.ID)
	if err != nil {
		t.Fatalf("list artifacts: %v", err)
	}
	if len(artifacts) != 1 || artifacts[0].Filename != "step-3.md" {
		t.Fatalf("unexpected artifacts: %#v", artifacts)
	}
	saved, err := runtime.sessions.Get(context.Background(), "alice", session.ID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	foundToolResult := false
	for _, message := range saved.Messages {
		if strings.EqualFold(message.ToolName, ArtifactToolName) {
			foundToolResult = true
			break
		}
	}
	if !foundToolResult {
		t.Fatalf("expected saved session to include Artifact tool result, got %#v", saved.Messages)
	}
}

func TestRuntimeDeepAgentModelActionDoesNotRequireArtifactFromGoalOrPrompt(t *testing.T) {
	runtime := NewRuntime(
		RuntimeConfig{},
		NewFileSessionStore(t.TempDir()),
		nil,
		nil,
		func(Scope) Runner { return noOutputRunner{} },
	)
	runtime.SetArtifactService(NewArtifactService(newMemoryArtifactStore(), NewFileObjectStore(t.TempDir()), "artifacts"))
	session, err := runtime.CreateSession(context.Background(), "alice", "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	result, err := NewRuntimeDeepAgentExecutor(runtime).ExecuteDeepAgentAction(context.Background(), DeepAgentAction{
		StepID: "step-1",
		Tool:   "model",
		Args: map[string]any{
			"user_id":        "alice",
			"session_id":     session.ID,
			"goal":           "帮我调研一下tolan这个产品，然后生成一个调研报告",
			"prompt":         deepAgentToolUsageReminder() + "\n\nCurrent step intent:\n收集并整理tolan产品信息",
			"step_title":     "收集并整理tolan产品信息",
			"step_intent":    "收集并整理tolan产品信息",
			"done_condition": "相关 Tolan 产品事实已收集整理",
		},
	}, &DeepAgentState{WorkingMemory: map[string]any{"user_id": "alice", "session_id": session.ID}})
	if err != nil {
		t.Fatalf("ExecuteDeepAgentAction() should not require artifact for research model step, got %v", err)
	}
	if result.Status != DeepAgentActionStatusSucceeded || result.Metadata == nil {
		t.Fatalf("unexpected model action result: %#v", result)
	}
	if got := deepAgentAnyInt(result.Metadata["artifact_count"], -1); got != 0 {
		t.Fatalf("artifact_count = %d, want 0 in %#v", got, result.Metadata)
	}
	artifacts, err := runtime.ListArtifacts(context.Background(), "alice", session.ID)
	if err != nil {
		t.Fatalf("list artifacts: %v", err)
	}
	if len(artifacts) != 0 {
		t.Fatalf("research model step should not create artifacts, got %#v", artifacts)
	}
}

func TestRuntimeDeepAgentModelArtifactSavesGeneratedSessionWithoutSessionID(t *testing.T) {
	store := newTestSessionStore()
	runtime := NewRuntime(
		RuntimeConfig{},
		store,
		nil,
		nil,
		func(Scope) Runner { return markdownReportRunner{} },
	)
	runtime.SetArtifactService(NewArtifactService(newMemoryArtifactStore(), NewFileObjectStore(t.TempDir()), "artifacts"))

	result, err := NewRuntimeDeepAgentExecutor(runtime).ExecuteDeepAgentAction(context.Background(), DeepAgentAction{
		StepID: "step-ephemeral",
		Tool:   "model_artifact",
		Args: map[string]any{
			"user_id":          "alice",
			"prompt":           "生成 Markdown 格式的调查报告",
			"step_title":       "生成 Markdown 格式的调查报告",
			"step_intent":      "生成一份 md 格式调查报告",
			"done_condition":   "Markdown report artifact is available",
			"success_criteria": "artifact count is at least 1",
		},
	}, &DeepAgentState{WorkingMemory: map[string]any{"user_id": "alice"}})
	if err != nil {
		t.Fatalf("ExecuteDeepAgentAction() error = %v", err)
	}
	sessionID := deepAgentWorkflowString(result.Metadata, "session_id")
	if strings.TrimSpace(sessionID) == "" {
		t.Fatalf("expected generated session id in metadata, got %#v", result.Metadata)
	}
	saved, err := runtime.sessions.Get(context.Background(), "alice", sessionID)
	if err != nil {
		t.Fatalf("expected generated session to be saved, get error = %v", err)
	}
	if saved == nil || saved.ID != sessionID {
		t.Fatalf("unexpected saved session: %#v", saved)
	}
	artifacts, err := runtime.ListArtifacts(context.Background(), "alice", sessionID)
	if err != nil {
		t.Fatalf("expected generated artifact to be listed, got error = %v", err)
	}
	if len(artifacts) != 1 || artifacts[0].Filename != "step-ephemeral.md" {
		t.Fatalf("unexpected generated artifacts: %#v", artifacts)
	}
}

func TestRuntimeDeepAgentModelArtifactUsesAssistantMessageWhenOutputEmpty(t *testing.T) {
	runtime := NewRuntime(
		RuntimeConfig{},
		NewFileSessionStore(t.TempDir()),
		nil,
		nil,
		func(Scope) Runner { return emptyOutputAssistantReportRunner{} },
	)
	runtime.SetArtifactService(NewArtifactService(newMemoryArtifactStore(), NewFileObjectStore(t.TempDir()), "artifacts"))
	session, err := runtime.CreateSession(context.Background(), "alice", "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	result, err := NewRuntimeDeepAgentExecutor(runtime).ExecuteDeepAgentAction(context.Background(), DeepAgentAction{
		StepID: "assistant-output",
		Tool:   "model_artifact",
		Args: map[string]any{
			"user_id":        "alice",
			"session_id":     session.ID,
			"prompt":         "生成 Markdown 格式的调查报告",
			"done_condition": "Markdown report artifact is available",
		},
	}, &DeepAgentState{WorkingMemory: map[string]any{"user_id": "alice", "session_id": session.ID}})
	if err != nil {
		t.Fatalf("ExecuteDeepAgentAction() error = %v", err)
	}
	if got := deepAgentAnyInt(result.Metadata["artifact_count"], -1); got != 1 {
		t.Fatalf("artifact_count = %d, want 1 in %#v", got, result.Metadata)
	}
	if !strings.Contains(result.Output, "报告正文") {
		t.Fatalf("expected assistant message to be used as output, got %q", result.Output)
	}
	artifacts, err := runtime.ListArtifacts(context.Background(), "alice", session.ID)
	if err != nil {
		t.Fatalf("list artifacts: %v", err)
	}
	if len(artifacts) != 1 || artifacts[0].Filename != "assistant-output.md" {
		t.Fatalf("unexpected artifacts: %#v", artifacts)
	}
}

func TestRuntimeDeepAgentModelArtifactCountsStoreArtifactWithoutToolResult(t *testing.T) {
	var runtime *Runtime
	runtime = NewRuntime(
		RuntimeConfig{},
		NewFileSessionStore(t.TempDir()),
		nil,
		nil,
		func(scope Scope) Runner {
			return storeOnlyArtifactRunner{runtime: runtime, userID: scope.UserID}
		},
	)
	runtime.SetArtifactService(NewArtifactService(newMemoryArtifactStore(), NewFileObjectStore(t.TempDir()), "artifacts"))

	result, err := NewRuntimeDeepAgentExecutor(runtime).ExecuteDeepAgentAction(context.Background(), DeepAgentAction{
		StepID: "store-artifact",
		Tool:   "model_artifact",
		Args: map[string]any{
			"user_id":        "alice",
			"prompt":         "生成 Markdown 格式的调查报告",
			"done_condition": "Markdown report artifact is available",
		},
	}, &DeepAgentState{WorkingMemory: map[string]any{"user_id": "alice"}})
	if err != nil {
		t.Fatalf("ExecuteDeepAgentAction() error = %v", err)
	}
	if got := deepAgentAnyInt(result.Metadata["artifact_count"], -1); got != 1 {
		t.Fatalf("artifact_count = %d, want 1 in %#v", got, result.Metadata)
	}
	if detected, _ := deepAgentMetadataBool(result.Metadata, "artifact_detected_from_store"); !detected {
		t.Fatalf("expected store artifact detection metadata, got %#v", result.Metadata)
	}
	if fallback, _ := deepAgentMetadataBool(result.Metadata, "artifact_fallback"); fallback {
		t.Fatalf("store-created artifact should not use fallback, got %#v", result.Metadata)
	}
	sessionID := deepAgentWorkflowString(result.Metadata, "session_id")
	artifacts, err := runtime.ListArtifacts(context.Background(), "alice", sessionID)
	if err != nil {
		t.Fatalf("list artifacts: %v", err)
	}
	if len(artifacts) != 1 || artifacts[0].Filename != "runner-report.md" {
		t.Fatalf("unexpected artifacts: %#v", artifacts)
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

func TestRuntimeDeepAgentRouterLeavesResearchAndArtifactStepsForModelTools(t *testing.T) {
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
	planner := NewRuntimeDeepAgentPlanner(runtime)
	searchAction, err := planner.NextAction(context.Background(), state, plan.Steps[0])
	if err != nil {
		t.Fatalf("NextAction() error = %v", err)
	}
	if searchAction.Tool != "model" {
		t.Fatalf("search action tool = %q, want model with web tools: %#v", searchAction.Tool, searchAction)
	}
	if got := deepAgentWorkflowString(searchAction.Args, "prompt"); !strings.Contains(got, "WebSearch") || !strings.Contains(got, "WebFetch") {
		t.Fatalf("search model prompt should mention web tools, got %#v", searchAction.Args)
	}

	action, err := planner.NextAction(context.Background(), state, final)
	if err != nil {
		t.Fatalf("NextAction() error = %v", err)
	}
	if action.Tool != "model_artifact" {
		t.Fatalf("artifact action tool = %q, want model_artifact: %#v", action.Tool, action)
	}
	args := action.Args
	if got := deepAgentWorkflowString(args, "prompt"); !strings.Contains(got, "Artifact") || !strings.Contains(got, "Tolan AI") {
		t.Fatalf("model prompt should include artifact guidance and context, got %#v", args)
	}
}

func TestRuntimeDeepAgentRouterDoesNotTreatReportOutlineAsArtifact(t *testing.T) {
	runtime := NewRuntime(
		RuntimeConfig{},
		NewFileSessionStore(t.TempDir()),
		nil,
		nil,
		func(Scope) Runner { return echoRunner{} },
	)
	planner := NewRuntimeDeepAgentPlanner(runtime)
	state := &DeepAgentState{
		Goal:          "帮我调研一下tolan这个产品，然后生成一个调研报告",
		WorkingMemory: map[string]any{"user_id": "alice", "session_id": "session-1"},
	}
	outlineAction, err := planner.NextAction(context.Background(), state, DeepAgentStep{
		ID:            "outline",
		Title:         "构建调研报告大纲结构",
		Intent:        "分析已收集信息并形成报告大纲",
		DoneCondition: "报告大纲结构清晰，覆盖产品定位、功能、竞品和技术特点",
	})
	if err != nil {
		t.Fatalf("NextAction() error = %v", err)
	}
	if outlineAction.Tool != DeepAgentToolModeModel {
		t.Fatalf("outline action tool = %q, want model: %#v", outlineAction.Tool, outlineAction)
	}
	if required, _ := outlineAction.Args["requires_artifact"].(bool); required {
		t.Fatalf("outline step should not require artifact: %#v", outlineAction.Args)
	}

	finalAction, err := planner.NextAction(context.Background(), state, DeepAgentStep{
		ID:            "write",
		Title:         "生成专业的产品调研报告文档",
		Intent:        "生成并交付 Tolan 产品调研报告文档",
		DoneCondition: "调研报告文档已生成并可下载",
	})
	if err != nil {
		t.Fatalf("NextAction() final error = %v", err)
	}
	if finalAction.Tool != DeepAgentToolModeModelArtifact {
		t.Fatalf("final action tool = %q, want model_artifact: %#v", finalAction.Tool, finalAction)
	}
	if required, _ := finalAction.Args["requires_artifact"].(bool); !required {
		t.Fatalf("final step should require artifact: %#v", finalAction.Args)
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
	if _, err := runtime.artifacts.Create(context.Background(), AssetKindArtifact, "alice", session.ID, "old-report.md", "text/markdown", []byte("# old")); err != nil {
		t.Fatalf("create session artifact without job: %v", err)
	}
	if got := NewRuntimeDeepAgentExecutor(runtime).deepAgentChildJobArtifactCount(context.Background(), job); got != 1 {
		t.Fatalf("artifact count = %d, want 1", got)
	}
}

func TestRuntimeDeepAgentLLMRouteStepPreservesArtifactIntentOnMulti(t *testing.T) {
	runtime := NewRuntime(
		RuntimeConfig{},
		NewFileSessionStore(t.TempDir()),
		nil,
		nil,
		func(Scope) Runner { return multiClassifyingDeepAgentRunner{} },
	)
	planner := NewRuntimeDeepAgentPlanner(runtime)
	mode := planner.llmRouteStep(context.Background(), &DeepAgentState{WorkingMemory: map[string]any{"user_id": "alice"}}, DeepAgentStep{
		ID:            "write",
		Title:         "Write report",
		Intent:        "Deliver the final Tolan AI report",
		DoneCondition: "downloadable markdown artifact is available",
	})
	if mode != "model_artifact" {
		t.Fatalf("llmRouteStep() = %q, want model_artifact", mode)
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

type markdownReportRunner struct{}

func (markdownReportRunner) Run(ctx context.Context, session *state.Session, prompt string) (engine.Result, error) {
	return markdownReportRunner{}.RunGeneratedPrompt(ctx, session, prompt)
}

func (markdownReportRunner) RunGeneratedPrompt(_ context.Context, session *state.Session, _ string) (engine.Result, error) {
	output := "# Tolan AI 调查报告\n\n## 摘要\n\nTolan AI 是一个 AI 产品。"
	session.AddAssistantMessage(output)
	return engine.Result{Output: output, Session: session}, nil
}

type noOutputRunner struct{}

func (noOutputRunner) Run(ctx context.Context, session *state.Session, prompt string) (engine.Result, error) {
	return noOutputRunner{}.RunGeneratedPrompt(ctx, session, prompt)
}

func (noOutputRunner) RunGeneratedPrompt(context.Context, *state.Session, string) (engine.Result, error) {
	return engine.Result{}, nil
}

type emptyOutputAssistantReportRunner struct{}

func (emptyOutputAssistantReportRunner) Run(ctx context.Context, session *state.Session, prompt string) (engine.Result, error) {
	return emptyOutputAssistantReportRunner{}.RunGeneratedPrompt(ctx, session, prompt)
}

func (emptyOutputAssistantReportRunner) RunGeneratedPrompt(_ context.Context, session *state.Session, _ string) (engine.Result, error) {
	session.AddAssistantMessage("# 调研报告\n\n报告正文")
	return engine.Result{Session: session}, nil
}

type storeOnlyArtifactRunner struct {
	runtime *Runtime
	userID  string
}

func (r storeOnlyArtifactRunner) Run(ctx context.Context, session *state.Session, prompt string) (engine.Result, error) {
	return r.RunGeneratedPrompt(ctx, session, prompt)
}

func (r storeOnlyArtifactRunner) RunGeneratedPrompt(ctx context.Context, session *state.Session, _ string) (engine.Result, error) {
	if r.runtime == nil {
		return engine.Result{}, fmt.Errorf("runtime is required")
	}
	if _, err := r.runtime.CreateArtifact(ctx, r.userID, session.ID, "runner-report.md", "text/markdown", []byte("# runner report")); err != nil {
		return engine.Result{}, err
	}
	return engine.Result{Session: session}, nil
}

type testSessionStore struct {
	sessions map[string]*state.Session
}

func newTestSessionStore() *testSessionStore {
	return &testSessionStore{sessions: map[string]*state.Session{}}
}

func (s *testSessionStore) key(userID, sessionID string) string {
	return userID + ":" + sessionID
}

func (s *testSessionStore) Create(_ context.Context, userID, workingDir string) (*state.Session, error) {
	session := state.NewSession(workingDir)
	if err := s.Save(context.Background(), userID, session); err != nil {
		return nil, err
	}
	return session, nil
}

func (s *testSessionStore) Get(_ context.Context, userID, sessionID string) (*state.Session, error) {
	session, ok := s.sessions[s.key(userID, sessionID)]
	if !ok {
		return nil, os.ErrNotExist
	}
	clone := *session
	return &clone, nil
}

func (s *testSessionStore) List(_ context.Context, userID string) ([]*state.Session, error) {
	out := make([]*state.Session, 0, len(s.sessions))
	for key, session := range s.sessions {
		if strings.HasPrefix(key, userID+":") {
			clone := *session
			out = append(out, &clone)
		}
	}
	return out, nil
}

func (s *testSessionStore) Save(_ context.Context, userID string, session *state.Session) error {
	if session == nil {
		return fmt.Errorf("session is required")
	}
	clone := *session
	s.sessions[s.key(userID, session.ID)] = &clone
	return nil
}

func (s *testSessionStore) Delete(_ context.Context, userID, sessionID string) error {
	delete(s.sessions, s.key(userID, sessionID))
	return nil
}

func (s *testSessionStore) DeleteUser(_ context.Context, userID string) error {
	for key := range s.sessions {
		if strings.HasPrefix(key, userID+":") {
			delete(s.sessions, key)
		}
	}
	return nil
}

func (s *testSessionStore) PruneBefore(_ context.Context, _ time.Time) (int, error) {
	return 0, nil
}

type multiClassifyingDeepAgentRunner struct{}

func (multiClassifyingDeepAgentRunner) Run(ctx context.Context, session *state.Session, prompt string) (engine.Result, error) {
	return multiClassifyingDeepAgentRunner{}.RunGeneratedPrompt(ctx, session, prompt)
}

func (multiClassifyingDeepAgentRunner) RunGeneratedPrompt(_ context.Context, session *state.Session, _ string) (engine.Result, error) {
	session.AddAssistantMessage("multi")
	return engine.Result{Output: "multi", Session: session}, nil
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
