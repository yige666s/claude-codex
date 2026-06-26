package agentruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"claude-codex/internal/harness/engine"
	"claude-codex/internal/harness/skills"
	"claude-codex/internal/harness/state"
	toolkit "claude-codex/internal/harness/tools"
	skilltool "claude-codex/internal/harness/tools/skill"
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
		artifactDetailDeepAgentExecutor{},
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
	var started, succeeded, artifactEvent, childEvent map[string]any
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
		case "deep_agent_artifact_output":
			if err := json.Unmarshal(event.Data, &artifactEvent); err != nil {
				t.Fatalf("unmarshal artifact event: %v", err)
			}
		case "deep_agent_child_job":
			if err := json.Unmarshal(event.Data, &childEvent); err != nil {
				t.Fatalf("unmarshal child event: %v", err)
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
	route, _ := succeeded["route"].(map[string]any)
	if route["mode"] != DeepAgentToolModeSkill || route["skill_name"] != "docx" {
		t.Fatalf("unexpected route detail: %#v", succeeded)
	}
	refs, _ := succeeded["artifact_refs"].([]any)
	if len(refs) != 1 {
		t.Fatalf("expected artifact refs in succeeded detail, got %#v", succeeded)
	}
	ref, _ := refs[0].(map[string]any)
	if firstNonEmptyString(fmt.Sprint(ref["id"]), fmt.Sprint(ref["artifact_id"])) != "artifact-1" || ref["filename"] != "report.docx" {
		t.Fatalf("unexpected artifact ref detail: %#v", succeeded)
	}
	if artifactEvent == nil || artifactEvent["event_group"] != "artifact_output" {
		t.Fatalf("expected artifact output event, got %#v", events)
	}
	if childEvent == nil || childEvent["event_group"] != "child_skill_job" {
		t.Fatalf("expected child job event, got %#v", events)
	}
	if got := fmt.Sprint(succeeded["error_class"]); got != "" && got != "<nil>" {
		t.Fatalf("successful action should not expose error class, got %#v", succeeded)
	}
	if got := fmt.Sprint(succeeded["tool_calls"]); !strings.Contains(got, "Artifact") {
		t.Fatalf("expected tool call detail in event, got %#v", succeeded)
	}
}

func TestDeepAgentControllerEmitsFailedActionDetailEvent(t *testing.T) {
	store := NewMemoryWorkflowStore()
	controller := NewDeepAgentController(
		store,
		NoopWorkflowEventSink{},
		staticDeepAgentPlanner{plan: DeepAgentPlan{Steps: []DeepAgentStep{{
			ID:            "research",
			Title:         "Run model action",
			DoneCondition: "model action completed",
			Metadata: map[string]any{
				"tool": "model",
				"args": map[string]any{"prompt": "research Tolan"},
			},
		}}}},
		failingDeepAgentExecutor{err: "queryengine empty response: no assistant text or tool calls"},
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
		Goal:      "exercise failed action detail events",
		Policy:    DeepAgentPolicy{MaxSteps: 1, MaxActions: 1, NoProgressLimit: 1, MaxDuration: time.Minute},
	})
	if !errors.Is(err, ErrDeepAgentBlocked) {
		t.Fatalf("Execute() error = %v, want blocked", err)
	}
	var failed map[string]any
	for _, event := range events {
		if event.Type == "deep_agent_action_failed" {
			if err := json.Unmarshal(event.Data, &failed); err != nil {
				t.Fatalf("unmarshal failed event: %v", err)
			}
		}
	}
	if failed == nil {
		t.Fatalf("expected failed action detail event, got %#v", events)
	}
	if failed["result_status"] != DeepAgentActionStatusFailed || !strings.Contains(fmt.Sprint(failed["error"]), "empty response") {
		t.Fatalf("unexpected failed detail: %#v", failed)
	}
	if failed["error_class"] != DeepAgentErrorTransient {
		t.Fatalf("expected transient error class, got %#v", failed)
	}
	route, _ := failed["route"].(map[string]any)
	if route["mode"] != DeepAgentToolModeModel || route["step_id"] != "research" {
		t.Fatalf("unexpected failed route detail: %#v", failed)
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

func TestRuntimeDeepAgentLoadContextCapturesSessionAssetsAndCapabilities(t *testing.T) {
	sessionStore := NewFileSessionStore(t.TempDir())
	runtime := NewRuntime(
		RuntimeConfig{},
		sessionStore,
		nil,
		nil,
		func(Scope) Runner { return contextCatalogRunner{} },
	)
	runtime.SetArtifactService(NewArtifactService(newMemoryArtifactStore(), NewFileObjectStore(t.TempDir()), "artifacts"))
	runtime.skills = fakeSkillCatalog{skills: []*skills.SkillDefinition{{
		Name:          "docx",
		Description:   "Create downloadable documents.",
		WhenToUse:     "Use for Word reports.",
		UserInvocable: true,
		RunAsJob:      true,
		Metadata:      map[string]any{"produces_artifacts": true},
	}}}
	session, err := runtime.CreateSession(context.Background(), "alice", "")
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	session.AddUserMessage("之前提到 Tolan AI 是一个陪伴类 AI 产品")
	session.AddAssistantMessage("需要调研定位、功能、竞品和风险")
	if err := sessionStore.Save(context.Background(), "alice", session); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	attachment, err := runtime.CreateAttachment(context.Background(), "alice", session.ID, "brief.txt", "text/plain", []byte("uploaded brief"))
	if err != nil {
		t.Fatalf("CreateAttachment() error = %v", err)
	}
	artifact, err := runtime.CreateArtifact(context.Background(), "alice", session.ID, "old-report.md", "text/markdown", []byte("# old report"))
	if err != nil {
		t.Fatalf("CreateArtifact() error = %v", err)
	}
	store := NewMemoryWorkflowStore()
	runtime.SetWorkflowStore(store)

	result, err := runtime.ExecuteDeepAgentTask(context.Background(), DeepAgentTaskRequest{
		UserID:    "alice",
		SessionID: session.ID,
		JobID:     "job-deep",
		Goal:      "根据上下文生成调研计划",
		Plan:      DeepAgentPlan{Steps: []DeepAgentStep{{ID: "only", Title: "Only", DoneCondition: "done"}}},
		State: map[string]any{
			"attachment_ids": []string{attachment.ID},
			"attachment_urls": []ChatAttachmentURL{{
				URL:         "https://example.com/tolan",
				Filename:    "tolan.html",
				ContentType: "text/html",
			}},
		},
	}, staticDeepAgentPlanner{plan: DeepAgentPlan{Steps: []DeepAgentStep{{ID: "only", Title: "Only", DoneCondition: "done"}}}}, completingDeepAgentExecutor{}, nil)
	if err != nil {
		t.Fatalf("ExecuteDeepAgentTask() error = %v", err)
	}
	loaded, ok := deepAgentLoadedContextFromMap(result.State.WorkingMemory)
	if !ok {
		t.Fatalf("loaded context missing from working memory: %#v", result.State.WorkingMemory)
	}
	if loaded.UserID != "alice" || loaded.SessionID != session.ID || loaded.JobID != "job-deep" {
		t.Fatalf("unexpected loaded identity: %#v", loaded)
	}
	if len(loaded.RecentMessages) < 2 {
		t.Fatalf("expected recent messages in loaded context, got %#v", loaded.RecentMessages)
	}
	if len(loaded.Attachments) < 2 {
		t.Fatalf("expected uploaded and URL attachments, got %#v", loaded.Attachments)
	}
	if len(loaded.ExistingArtifacts) != 1 || loaded.ExistingArtifacts[0].ID != artifact.ID {
		t.Fatalf("unexpected artifacts in loaded context: %#v", loaded.ExistingArtifacts)
	}
	pack, ok := deepAgentEvidencePackFromMap(result.State.WorkingMemory)
	if !ok {
		t.Fatalf("evidence pack missing from working memory: %#v", result.State.WorkingMemory)
	}
	if pack.TokenBudget <= 0 || pack.TokenEstimate <= 0 {
		t.Fatalf("unexpected evidence pack budget: %#v", pack)
	}
	if len(pack.ExistingArtifacts) != 1 || pack.ExistingArtifacts[0].CurrentRun {
		t.Fatalf("historical artifacts should be tagged as non-current: %#v", pack.ExistingArtifacts)
	}
	if len(loaded.SkillCatalog) != 1 || loaded.SkillCatalog[0].Name != "docx" || !loaded.SkillCatalog[0].ProducesArtifacts {
		t.Fatalf("unexpected skills in loaded context: %#v", loaded.SkillCatalog)
	}
	if len(loaded.ToolCatalog) < 3 {
		t.Fatalf("expected tool catalog from runner descriptors, got %#v", loaded.ToolCatalog)
	}
	steps, err := store.ListWorkflowStepRuns(context.Background(), result.Run.ID)
	if err != nil {
		t.Fatalf("ListWorkflowStepRuns() error = %v", err)
	}
	var loadOutput map[string]any
	for _, step := range steps {
		if step.StepName == "load_context" {
			loadOutput = step.Output
			break
		}
	}
	if loadOutput == nil {
		t.Fatalf("load_context workflow step missing from %#v", steps)
	}
	for key, wantPositive := range map[string]bool{
		"message_count":    true,
		"attachment_count": true,
		"artifact_count":   true,
		"skill_count":      true,
		"tool_count":       true,
	} {
		if got := deepAgentAnyInt(loadOutput[key], 0); wantPositive && got <= 0 {
			t.Fatalf("load_context output %s = %d, want positive in %#v", key, got, loadOutput)
		}
	}
	if got := deepAgentAnyInt(loadOutput["evidence_pack_tokens"], 0); got <= 0 {
		t.Fatalf("expected evidence_pack_tokens in load_context output: %#v", loadOutput)
	}
}

func TestRuntimeDeepAgentPlannerPromptIncludesLoadedContext(t *testing.T) {
	req := DeepAgentTaskRequest{
		UserID: "alice",
		Goal:   "帮我继续完善 Tolan AI 调研报告",
		State: map[string]any{
			deepAgentLoadedContextKey: DeepAgentLoadedContext{
				RecentMessages: []DeepAgentMessageRef{{Role: "user", Snippet: "之前已经收集过 Tolan AI 的产品定位"}},
				Attachments:    []DeepAgentAttachmentRef{{ID: "att-1", Filename: "brief.txt", ContentType: "text/plain", Source: "request"}},
				ExistingArtifacts: []DeepAgentArtifactRef{{
					ID:          "art-1",
					Filename:    "old-report.md",
					ContentType: "text/markdown",
				}},
				ToolCatalog:  []DeepAgentToolRef{{Name: "WebSearch"}, {Name: "Artifact"}},
				SkillCatalog: []DeepAgentSkillRef{{Name: "docx", ProducesArtifacts: true}},
			},
		},
	}
	prompt := deepAgentPlannerPromptWithSkills(req, "- name: docx\n  produces_artifacts: true")
	for _, want := range []string{
		"Loaded task context",
		"Tolan AI 的产品定位",
		"brief.txt",
		"old-report.md",
		"Available tools: WebSearch, Artifact",
		"Available skills: docx",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("planner prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestRuntimeDeepAgentPlannerPromptUsesEvidencePackAndSanitizesMemory(t *testing.T) {
	pack := buildDeepAgentEvidencePack(DeepAgentLoadedContext{
		UserID: "alice",
		RecentMessages: []DeepAgentMessageRef{{
			Role:    "user",
			Snippet: "继续完善 Tolan AI 报告",
		}},
		MemorySummary: "喜欢中文简洁回答\napi key: should-not-leak\nsecret token=hidden",
		ExistingArtifacts: []DeepAgentArtifactRef{{
			ID:          "old",
			Filename:    "old-report.md",
			ContentType: "text/markdown",
			SizeBytes:   128,
			Source:      "session_artifact",
		}},
	}, nil, 1000)
	prompt := deepAgentPlannerPromptWithSkills(DeepAgentTaskRequest{
		UserID: "alice",
		Goal:   "继续完善 Tolan AI 报告",
		State:  map[string]any{deepAgentEvidencePackKey: pack},
	}, "")
	for _, want := range []string{"Memory summary", "喜欢中文简洁回答", "Existing artifacts (historical", "old-report.md"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("planner prompt missing %q:\n%s", want, prompt)
		}
	}
	for _, forbidden := range []string{"should-not-leak", "token=hidden"} {
		if strings.Contains(prompt, forbidden) {
			t.Fatalf("planner prompt leaked hidden memory %q:\n%s", forbidden, prompt)
		}
	}
}

func TestRuntimeDeepAgentPlanExecuteResearchReportCreatesMarkdownArtifact(t *testing.T) {
	runtime := NewRuntime(
		RuntimeConfig{},
		NewFileSessionStore(t.TempDir()),
		nil,
		nil,
		func(Scope) Runner { return contextCatalogRunner{} },
	)
	runtime.SetArtifactService(NewArtifactService(newMemoryArtifactStore(), NewFileObjectStore(t.TempDir()), "artifacts"))
	session, err := runtime.CreateSession(context.Background(), "alice", "")
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	plan := DeepAgentPlan{Steps: []DeepAgentStep{
		{
			ID:            "research",
			Title:         "调研 Tolan AI 产品信息",
			Intent:        "使用公开网页信息收集 Tolan AI 产品定位、功能和竞品事实",
			DoneCondition: "已收集 Tolan AI 相关事实和来源链接，用于后续报告撰写",
		},
		{
			ID:            "write-report",
			Title:         "生成最终 Markdown 调研报告",
			Intent:        "生成并保存一份 Tolan AI Markdown 调研报告 artifact",
			DependsOn:     []string{"research"},
			DoneCondition: "Markdown 调研报告 artifact 已生成并可下载",
		},
	}}
	result, err := runtime.ExecuteDeepAgentTask(context.Background(), DeepAgentTaskRequest{
		UserID:    "alice",
		SessionID: session.ID,
		Goal:      "帮我调研一下 tolan 这个 AI 产品，然后生成一个调研报告",
		Plan:      plan,
		Policy:    DeepAgentPolicy{MaxSteps: 3, MaxActions: 4, MaxDuration: time.Minute, NoProgressLimit: 2},
	}, runtimeRoutingStaticPlanner{runtime: runtime, plan: plan}, nil, nil)
	if err != nil {
		t.Fatalf("ExecuteDeepAgentTask() error = %v", err)
	}
	if result.State == nil || result.State.Status != DeepAgentRunStatusSucceeded {
		t.Fatalf("unexpected deep agent state: %#v", result.State)
	}
	artifacts, err := runtime.ListArtifacts(context.Background(), "alice", session.ID)
	if err != nil {
		t.Fatalf("ListArtifacts() error = %v", err)
	}
	if len(artifacts) != 1 {
		t.Fatalf("artifact count = %d, want 1: %#v", len(artifacts), artifacts)
	}
	if !strings.HasSuffix(artifacts[0].Filename, ".md") || artifacts[0].ContentType != "text/markdown" {
		t.Fatalf("unexpected report artifact: %#v", artifacts[0])
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
	saved, err := runtime.sessions.Get(context.Background(), "alice", session.ID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	visibleAssistant := false
	hiddenAssistant := false
	for _, message := range saved.Messages {
		if message.Role != state.MessageRoleAssistant || !strings.HasPrefix(strings.TrimSpace(message.Content), "report generated:") {
			continue
		}
		if message.Hidden {
			hiddenAssistant = true
		} else {
			visibleAssistant = true
		}
	}
	if !hiddenAssistant || visibleAssistant {
		t.Fatalf("DeepAgent skill assistant reply should be hidden, hidden=%v visible=%v messages=%#v", hiddenAssistant, visibleAssistant, saved.Messages)
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

func TestRuntimeDeepAgentModelActionRunsSelectedRunAsJobSkill(t *testing.T) {
	workspace := t.TempDir()
	runtime := NewRuntime(
		RuntimeConfig{DefaultWorkingDir: workspace, TurnTimeout: time.Minute},
		NewFileSessionStore(t.TempDir()),
		nil,
		fakeSkillCatalog{skills: []*skills.SkillDefinition{{
			Name:          "docx",
			UserInvocable: true,
			RunAsJob:      true,
			Metadata:      map[string]any{"agentapi": map[string]any{"produces_artifacts": true}},
			GetPrompt: func(args string, _ *skills.SkillContext) ([]skills.ContentBlock, error) {
				return []skills.ContentBlock{{Type: "text", Text: "generate docx: " + args}}, nil
			},
		}}},
		func(scope Scope) Runner {
			if scope.SkillScoped {
				return generatedArtifactFileRunner{workspace: workspace}
			}
			return runAsJobDocxMarkerRunner{}
		},
	)
	runtime.SetJobStore(NewMemoryJobStore())
	runtime.SetArtifactService(NewArtifactService(newMemoryArtifactStore(), NewFileObjectStore(t.TempDir()), "artifacts"))
	session, err := runtime.CreateSession(context.Background(), "alice", "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	result, err := NewRuntimeDeepAgentExecutor(runtime).ExecuteDeepAgentAction(context.Background(), DeepAgentAction{
		StepID: "write",
		Tool:   "model_artifact",
		Args: map[string]any{
			"user_id":        "alice",
			"session_id":     session.ID,
			"prompt":         "生成 Tolan 调研文档",
			"done_condition": "downloadable report artifact is available",
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
	if got := deepAgentWorkflowString(result.Metadata, "skill_name"); got != "docx" {
		t.Fatalf("skill_name = %q, want docx in %#v", got, result.Metadata)
	}
	if got := deepAgentWorkflowString(result.Metadata, "child_job_id"); got == "" {
		t.Fatalf("expected child job id in metadata: %#v", result.Metadata)
	}
	artifacts, err := runtime.ListArtifacts(context.Background(), "alice", session.ID)
	if err != nil {
		t.Fatalf("list artifacts: %v", err)
	}
	if len(artifacts) != 1 || artifacts[0].Filename != "runner-report.docx" || strings.TrimSpace(artifacts[0].JobID) == "" {
		t.Fatalf("unexpected artifacts: %#v", artifacts)
	}
}

func TestRuntimeDeepAgentModelArtifactRestrictsGenericDocumentGoalToArtifactTools(t *testing.T) {
	var captured Scope
	runtime := NewRuntime(
		RuntimeConfig{},
		NewFileSessionStore(t.TempDir()),
		nil,
		nil,
		func(scope Scope) Runner {
			captured = scope
			return markdownReportRunner{}
		},
	)
	runtime.SetArtifactService(NewArtifactService(newMemoryArtifactStore(), NewFileObjectStore(t.TempDir()), "artifacts"))
	session, err := runtime.CreateSession(context.Background(), "alice", "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	result, err := NewRuntimeDeepAgentExecutor(runtime).ExecuteDeepAgentAction(context.Background(), DeepAgentAction{
		StepID: "write",
		Tool:   "model_artifact",
		Args: map[string]any{
			"user_id":        "alice",
			"session_id":     session.ID,
			"prompt":         "生成 Tolan 调研文档",
			"done_condition": "downloadable report artifact is available",
		},
	}, &DeepAgentState{
		Goal:          "帮我调研一下tolan这个产品，然后生成一个调研文档",
		WorkingMemory: map[string]any{"user_id": "alice", "session_id": session.ID},
	})
	if err != nil {
		t.Fatalf("ExecuteDeepAgentAction() error = %v", err)
	}
	if result.Status != DeepAgentActionStatusSucceeded {
		t.Fatalf("unexpected action result: %#v", result)
	}
	want := []string{"WebSearch", "WebFetch", ArtifactToolName}
	if strings.Join(captured.AllowedTools, ",") != strings.Join(want, ",") {
		t.Fatalf("AllowedTools = %#v, want %#v", captured.AllowedTools, want)
	}
	wantResearch := []string{"WebSearch", "WebFetch"}
	if got := deepAgentModelActionAllowedTools(DeepAgentAction{}, &DeepAgentState{Goal: "帮我调研一下tolan这个产品，然后生成一个调研文档"}, false); strings.Join(got, ",") != strings.Join(wantResearch, ",") {
		t.Fatalf("research AllowedTools = %#v, want %#v", got, wantResearch)
	}
	if got := deepAgentModelActionAllowedTools(DeepAgentAction{}, &DeepAgentState{Goal: "请生成 Word 文档"}, true); got != nil {
		t.Fatalf("explicit Word request should keep full tool access, got %#v", got)
	}
}

func TestRuntimeDeepAgentModelActionDoesNotRequireArtifactFromGoalOrPrompt(t *testing.T) {
	var captured Scope
	runtime := NewRuntime(
		RuntimeConfig{},
		NewFileSessionStore(t.TempDir()),
		nil,
		nil,
		func(scope Scope) Runner {
			captured = scope
			return noOutputRunner{}
		},
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
	wantAllowed := []string{"WebSearch", "WebFetch"}
	if strings.Join(captured.AllowedTools, ",") != strings.Join(wantAllowed, ",") {
		t.Fatalf("AllowedTools = %#v, want %#v", captured.AllowedTools, wantAllowed)
	}
	rawAllowed := deepAgentStringSlice(result.Metadata["allowed_tools"])
	if strings.Join(rawAllowed, ",") != strings.Join(wantAllowed, ",") {
		t.Fatalf("metadata allowed_tools = %#v, want %#v", result.Metadata["allowed_tools"], wantAllowed)
	}
	artifacts, err := runtime.ListArtifacts(context.Background(), "alice", session.ID)
	if err != nil {
		t.Fatalf("list artifacts: %v", err)
	}
	if len(artifacts) != 0 {
		t.Fatalf("research model step should not create artifacts, got %#v", artifacts)
	}
}

func TestRuntimeDeepAgentModelActionUsesHiddenUserTurn(t *testing.T) {
	runner := &deepAgentExecutionPromptRunner{}
	runtime := NewRuntime(
		RuntimeConfig{},
		NewFileSessionStore(t.TempDir()),
		nil,
		nil,
		func(Scope) Runner { return runner },
	)
	session, err := runtime.CreateSession(context.Background(), "alice", "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	result, err := NewRuntimeDeepAgentExecutor(runtime).ExecuteDeepAgentAction(context.Background(), DeepAgentAction{
		StepID: "gather-tolan-info",
		Tool:   DeepAgentToolModeModel,
		Args: map[string]any{
			"user_id":    "alice",
			"session_id": session.ID,
			"prompt":     "Gather Tolan Product Information",
		},
	}, &DeepAgentState{WorkingMemory: map[string]any{"user_id": "alice", "session_id": session.ID}})
	if err != nil {
		t.Fatalf("ExecuteDeepAgentAction() error = %v", err)
	}
	if result.Status != DeepAgentActionStatusSucceeded || !result.Completed {
		t.Fatalf("unexpected model action result: %#v", result)
	}
	if runner.generatedCalls != 0 {
		t.Fatalf("DeepAgent execution step must not use generated/meta prompt path, generatedCalls=%d", runner.generatedCalls)
	}
	if runner.runCalls != 1 {
		t.Fatalf("Run calls = %d, want 1", runner.runCalls)
	}
	if got := deepAgentWorkflowString(result.Metadata, "prompt_mode"); got != "hidden_user_turn" {
		t.Fatalf("prompt_mode = %q, want hidden_user_turn in %#v", got, result.Metadata)
	}
	if got := deepAgentAnyInt(result.Metadata["hidden_user_prompts"], -1); got != 1 {
		t.Fatalf("hidden_user_prompts = %d, want 1 in %#v", got, result.Metadata)
	}
	saved, err := runtime.sessions.Get(context.Background(), "alice", session.ID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	hiddenPrompt := false
	visibleInternalPrompt := false
	hiddenAssistant := false
	visibleAssistant := false
	for _, message := range saved.Messages {
		switch {
		case message.Role == state.MessageRoleUser && strings.TrimSpace(message.Content) == "Gather Tolan Product Information":
			if message.Hidden {
				hiddenPrompt = true
			} else {
				visibleInternalPrompt = true
			}
		case message.Role == state.MessageRoleAssistant && strings.TrimSpace(message.Content) == "Collected Tolan product information with cited sources.":
			if message.Hidden {
				hiddenAssistant = true
			} else {
				visibleAssistant = true
			}
		}
	}
	if !hiddenPrompt || visibleInternalPrompt {
		t.Fatalf("DeepAgent internal prompt should be hidden, hidden=%v visible=%v messages=%#v", hiddenPrompt, visibleInternalPrompt, saved.Messages)
	}
	if !hiddenAssistant || visibleAssistant {
		t.Fatalf("DeepAgent internal assistant reply should be hidden, hidden=%v visible=%v messages=%#v", hiddenAssistant, visibleAssistant, saved.Messages)
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
	if strings.Contains(result.Output, "报告正文") || !strings.Contains(result.Output, "Artifacts") {
		t.Fatalf("expected concise artifact pointer output, got %q", result.Output)
	}
	artifacts, err := runtime.ListArtifacts(context.Background(), "alice", session.ID)
	if err != nil {
		t.Fatalf("list artifacts: %v", err)
	}
	if len(artifacts) != 1 || artifacts[0].Filename != "assistant-output.md" {
		t.Fatalf("unexpected artifacts: %#v", artifacts)
	}
	_, data, err := runtime.GetArtifact(context.Background(), "alice", artifacts[0].ID)
	if err != nil {
		t.Fatalf("get artifact: %v", err)
	}
	if !strings.Contains(string(data), "报告正文") {
		t.Fatalf("expected assistant message to be saved as artifact content, got %q", string(data))
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

func TestRuntimeDeepAgentModelArtifactUsesPriorArtifactInsteadOfSavingToolError(t *testing.T) {
	runner := &countingErrorRunner{err: context.DeadlineExceeded}
	runtime := NewRuntime(
		RuntimeConfig{},
		NewFileSessionStore(t.TempDir()),
		nil,
		nil,
		func(Scope) Runner { return runner },
	)
	runtime.SetArtifactService(NewArtifactService(newMemoryArtifactStore(), NewFileObjectStore(t.TempDir()), "artifacts"))
	session, err := runtime.CreateSession(context.Background(), "alice", "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if _, err := runtime.CreateArtifact(context.Background(), "alice", session.ID, "Tolan产品调研报告.md", "text/markdown", []byte("# Tolan")); err != nil {
		t.Fatalf("create prior artifact: %v", err)
	}
	agentState := &DeepAgentState{
		Goal: "帮我调研一下tolan这个产品，然后生成一个调研文档",
		WorkingMemory: map[string]any{
			"user_id":    "alice",
			"session_id": session.ID,
			"step_context": map[string]any{
				"step-1": map[string]any{
					"artifact_count": 1,
					"summary":        "已生成 Tolan 产品调研报告 artifact。",
				},
			},
		},
	}

	result, err := NewRuntimeDeepAgentExecutor(runtime).ExecuteDeepAgentAction(context.Background(), DeepAgentAction{
		StepID: "step-2",
		Tool:   "model_artifact",
		Args: map[string]any{
			"user_id":        "alice",
			"session_id":     session.ID,
			"prompt":         "生成 Tolan 调研文档",
			"done_condition": "调研文档已生成并可下载",
		},
	}, agentState)
	if err != nil {
		t.Fatalf("ExecuteDeepAgentAction() should accept prior artifact, got %v", err)
	}
	if runner.calls != 0 {
		t.Fatalf("prior artifact should satisfy before invoking model, calls = %d", runner.calls)
	}
	if result.Status != DeepAgentActionStatusSucceeded || !result.Completed {
		t.Fatalf("unexpected result: %#v", result)
	}
	if got := deepAgentAnyInt(result.Metadata["artifact_count"], -1); got != 1 {
		t.Fatalf("artifact_count = %d, want prior count 1 in %#v", got, result.Metadata)
	}
	if satisfied, _ := deepAgentMetadataBool(result.Metadata, "artifact_satisfied_by_prior_step"); !satisfied {
		t.Fatalf("expected prior artifact satisfaction metadata, got %#v", result.Metadata)
	}
	if fallback, _ := deepAgentMetadataBool(result.Metadata, "artifact_fallback"); fallback {
		t.Fatalf("tool error output must not be saved as fallback artifact: %#v", result.Metadata)
	}
	artifacts, err := runtime.ListArtifacts(context.Background(), "alice", session.ID)
	if err != nil {
		t.Fatalf("list artifacts: %v", err)
	}
	if len(artifacts) != 1 || artifacts[0].Filename != "Tolan产品调研报告.md" {
		t.Fatalf("unexpected artifacts after tool error: %#v", artifacts)
	}
	if strings.Contains(result.Output, "工具未找到") {
		t.Fatalf("tool error should not become final action output: %q", result.Output)
	}
}

func TestRuntimeDeepAgentPriorArtifactSkipsDifferentJob(t *testing.T) {
	runtime := NewRuntime(
		RuntimeConfig{},
		NewFileSessionStore(t.TempDir()),
		nil,
		nil,
		func(Scope) Runner { return &countingErrorRunner{err: context.DeadlineExceeded} },
	)
	runtime.SetArtifactService(NewArtifactService(newMemoryArtifactStore(), NewFileObjectStore(t.TempDir()), "artifacts"))
	session, err := runtime.CreateSession(context.Background(), "alice", "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if _, err := runtime.CreateArtifact(WithJobID(context.Background(), "job-old"), "alice", session.ID, "Chance_AI_调研报告.md", "text/markdown", []byte("# old")); err != nil {
		t.Fatalf("create old job artifact: %v", err)
	}

	refs := NewRuntimeDeepAgentExecutor(runtime).deepAgentPriorArtifactRefs(context.Background(), "alice", session.ID, &DeepAgentState{
		Goal: "帮我调研一下chance ai这款产品",
		WorkingMemory: map[string]any{
			"user_id":    "alice",
			"session_id": session.ID,
		},
	}, DeepAgentAction{
		StepID: "artifact",
		Tool:   DeepAgentToolModeModelArtifact,
		Args: map[string]any{
			"job_id": "job-current",
		},
	})
	if len(refs) != 0 {
		t.Fatalf("old job artifact must not satisfy current job artifact step: %#v", refs)
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
	if got := deepAgentWorkflowString(searchAction.Args, "prompt"); !strings.Contains(got, "This is not a deliverable-file step") {
		t.Fatalf("search model prompt should include step boundary, got %#v", searchAction.Args)
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

func TestRuleDeepAgentPlannerSplitsResearchReportFallback(t *testing.T) {
	plan, err := ruleDeepAgentPlanner{}.CreatePlan(context.Background(), DeepAgentTaskRequest{
		Goal: "帮我调研一下tolan这个产品，然后生成一个调研文档",
	})
	if err != nil {
		t.Fatalf("CreatePlan() error = %v", err)
	}
	if len(plan.Steps) != 3 {
		t.Fatalf("fallback steps = %#v, want 3 steps", plan.Steps)
	}
	if deepAgentStepRequiresArtifact(plan.Steps[0]) {
		t.Fatalf("research step should not require artifact: %#v", plan.Steps[0])
	}
	if deepAgentStepRequiresArtifact(plan.Steps[1]) {
		t.Fatalf("analysis step should not require artifact: %#v", plan.Steps[1])
	}
	if !deepAgentStepRequiresArtifact(plan.Steps[2]) {
		t.Fatalf("final step should require artifact: %#v", plan.Steps[2])
	}
	if got := plan.Steps[2].DependsOn; len(got) != 2 || got[0] != "step-1" || got[1] != "step-2" {
		t.Fatalf("final dependencies = %#v, want step-1 and step-2", got)
	}
}

func TestRuntimeDeepAgentPlannerFallsBackOnEmptyModelResponse(t *testing.T) {
	runtime := NewRuntime(
		RuntimeConfig{},
		NewFileSessionStore(t.TempDir()),
		nil,
		nil,
		func(Scope) Runner { return emptyResponseDeepAgentPlanRunner{} },
	)
	plan, err := NewRuntimeDeepAgentPlanner(runtime).CreatePlan(context.Background(), DeepAgentTaskRequest{
		UserID: "alice",
		Goal:   "帮我调研一下tolan这个产品，然后生成一个调研文档",
	})
	if err != nil {
		t.Fatalf("CreatePlan() error = %v", err)
	}
	if len(plan.Steps) != 3 {
		t.Fatalf("fallback steps = %#v, want 3", plan.Steps)
	}
	if !deepAgentStepRequiresArtifact(plan.Steps[2]) {
		t.Fatalf("final fallback step should require artifact: %#v", plan.Steps[2])
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

func TestRuntimeDeepAgentRouterUsesImageSkillForImageGeneration(t *testing.T) {
	runtime := NewRuntime(
		RuntimeConfig{},
		NewFileSessionStore(t.TempDir()),
		nil,
		nil,
		func(Scope) Runner { return echoRunner{} },
	)
	runtime.skills = fakeSkillCatalog{skills: []*skills.SkillDefinition{{
		Name:          "vertex-image-artifact",
		Description:   "Generate one image with Vertex AI Imagen and save it as a generated artifact. Triggers include: 生成图片, 画一张, 生图, generate image, create image, render image.",
		UserInvocable: true,
		Metadata:      map[string]any{"produces_artifacts": true},
	}}}
	planner := NewRuntimeDeepAgentPlanner(runtime)
	action, err := planner.NextAction(context.Background(), &DeepAgentState{
		Goal:          "帮我画一只中华田园犬",
		WorkingMemory: map[string]any{"user_id": "alice", "session_id": "session-1"},
	}, DeepAgentStep{
		ID:            "generate-dog-image",
		Title:         "Generate dog image",
		Intent:        "Generate an image of a Chinese Rural Dog.",
		DoneCondition: "An image of a Chinese Rural Dog has been generated and provided to the user.",
	})
	if err != nil {
		t.Fatalf("NextAction() error = %v", err)
	}
	if action.Tool != DeepAgentToolModeSkill {
		t.Fatalf("image generation action tool = %q, want skill: %#v", action.Tool, action)
	}
	if got := deepAgentWorkflowString(action.Args, "skill_name"); got != "vertex-image-artifact" {
		t.Fatalf("skill_name = %q, want vertex-image-artifact in %#v", got, action.Args)
	}
}

func TestDeepAgentSourceRefsFromTextAcceptsSourceTitles(t *testing.T) {
	text := `## Tolan AI 调研报告

* 来源:
  * "How Tolan builds voice-first AI with GPT-5.1" - OpenAI
  * "AI companionship app Tolan raises $20M" - GeekWire`

	refs := deepAgentSourceRefsFromText(text)
	if len(refs) < 2 {
		t.Fatalf("source refs = %#v, want title refs", refs)
	}
	if refs[0].Title == "" || refs[0].Provider == "" {
		t.Fatalf("source title/provider not parsed: %#v", refs[0])
	}
}

func TestDeepAgentModelActionEvidenceMetadataIncludesResearchTools(t *testing.T) {
	session := state.NewSession("")
	session.Messages = []state.Message{
		{Role: state.MessageRoleUser, Content: "调研 Tolan"},
		{
			Role: state.MessageRoleAssistant,
			ToolCalls: []state.ToolCall{{
				ID:   "call-1",
				Name: "WebSearch",
			}},
		},
		{
			Role:       state.MessageRoleTool,
			ToolCallID: "call-1",
			ToolName:   "WebSearch",
			ToolOutput: "Tolan: Your alien best friend - https://www.producthunt.com/products/tolan\nAbout Tolan - https://www.tolan.com/about",
		},
		{
			Role:    state.MessageRoleAssistant,
			Content: "Tolan 是一个 AI 陪伴产品，已完成原始资料汇总。",
		},
	}

	metadata := deepAgentModelActionEvidenceMetadata("Tolan 是一个 AI 陪伴产品，已完成原始资料汇总。", session, 0)
	evidence := deepAgentEvidenceFromActionResult(DeepAgentStepRoute{
		Mode:         DeepAgentToolModeModel,
		Executor:     deepAgentRouteExecutorModel,
		SearchScope:  "web",
		AllowedTools: []string{"WebSearch", "WebFetch"},
	}, DeepAgentAction{StepID: "research"}, DeepAgentActionResult{
		Status:    DeepAgentActionStatusSucceeded,
		Completed: true,
		Output:    "Tolan 是一个 AI 陪伴产品，已完成原始资料汇总。",
		Metadata:  metadata,
	}, nil)

	if len(evidence.ToolCalls) == 0 {
		t.Fatalf("expected research tool call evidence, got %#v", evidence)
	}
	if len(evidence.Sources) < 2 {
		t.Fatalf("expected sources from WebSearch output, got %#v", evidence.Sources)
	}
	ok, reason := verifyDeepAgentStepEvidence(DeepAgentStep{
		ID:     "research",
		Intent: "联网调研 Tolan AI 产品",
	}, DeepAgentActionResult{
		Status:    DeepAgentActionStatusSucceeded,
		Completed: true,
		Output:    evidence.Output,
		Metadata: map[string]any{
			"step_evidence": evidence,
		},
	}, evidence)
	if !ok {
		t.Fatalf("expected research evidence to pass verification, got %q with %#v", reason, evidence)
	}
}

func TestDeepAgentModelArtifactFallbackExtractsMarkdownReport(t *testing.T) {
	output := "在尝试生成 Markdown 调研报告时，系统工具 Artifact 未找到，因此无法生成文件。\n\n## Tolan AI 调研报告\n\n正文内容足够保存为 artifact。"

	got := deepAgentModelArtifactFallbackOutput(output, nil, 0)
	if strings.Contains(got, "Artifact 未找到") {
		t.Fatalf("fallback kept tool failure preamble: %q", got)
	}
	if !strings.HasPrefix(got, "## Tolan AI 调研报告") {
		t.Fatalf("fallback = %q, want markdown report body", got)
	}
	if deepAgentModelArtifactFallbackLooksInvalid(got) {
		t.Fatalf("extracted markdown report should be valid fallback: %q", got)
	}
}

func TestDeepAgentModelArtifactFallbackRejectsDocxSkillApology(t *testing.T) {
	output := "我明白了，看来 `docx` 技能暂时无法使用。不过没关系，我可以将完整的研究报告内容保存为 Markdown 格式的文档。\n\n我已经将关于 Browserless 产品的研究报告保存为 `Browserless产品研究报告.md` 文件。"
	if !deepAgentModelArtifactFallbackLooksInvalid(deepAgentModelArtifactFallbackOutput(output, nil, 0)) {
		t.Fatalf("docx skill apology should not be accepted as artifact fallback: %q", output)
	}
}

func TestDeepAgentModelArtifactFallbackDecodesNotionMCPViewOutput(t *testing.T) {
	output := `Here is the result of "view" for the Page with URL https://app.notion.com/p/example as of 2026-06-22T11:19:37Z:\n<page url=\"https://app.notion.com/p/example\">\n\n{"title":"Memory System 技术设计文档"}\n\n---\nVersion: 1.0\nAuthor: Architecture Team\n---\n\n# 1. 背景\n\n系统 LLM 存在 Context Window 限制。\n</page>`

	got := deepAgentModelArtifactFallbackOutput(output, nil, 0)
	if strings.Contains(got, `\n`) {
		t.Fatalf("fallback kept literal escaped newlines: %q", got)
	}
	for _, unwanted := range []string{"Here is the result", "<page", "</page>", `{"title"`} {
		if strings.Contains(got, unwanted) {
			t.Fatalf("fallback kept Notion wrapper %q in %q", unwanted, got)
		}
	}
	if !strings.HasPrefix(got, "# Memory System 技术设计文档\n\n") {
		t.Fatalf("fallback should promote Notion title to markdown heading, got %q", got)
	}
	if !strings.Contains(got, "# 1. 背景\n\n系统 LLM") {
		t.Fatalf("fallback lost markdown body: %q", got)
	}
}

func TestDeepAgentModelActionUserOutputUsesArtifactPointer(t *testing.T) {
	metadata := map[string]any{
		"artifact_refs": []DeepAgentArtifactRef{{
			ID:       "artifact-1",
			Filename: "tolan-report.md",
		}},
	}
	longReport := "# Tolan AI 调研报告\n\n## 摘要\n\n这是一段很长的 artifact 正文。"

	got := deepAgentModelActionUserOutput("", longReport, "", metadata, 1)
	if strings.Contains(got, "## 摘要") || strings.Contains(got, "artifact 正文") {
		t.Fatalf("artifact user output should not include artifact body: %q", got)
	}
	if !strings.Contains(got, "tolan-report.md") || !strings.Contains(got, "Artifacts") {
		t.Fatalf("artifact user output should point to artifact preview, got %q", got)
	}
}

func TestRuntimeDeepAgentRouterUsesDocxSkillWhenExplicitlyRequested(t *testing.T) {
	runtime := NewRuntime(
		RuntimeConfig{},
		NewFileSessionStore(t.TempDir()),
		nil,
		nil,
		func(Scope) Runner { return echoRunner{} },
	)
	runtime.skills = fakeSkillCatalog{skills: []*skills.SkillDefinition{{
		Name:          "docx",
		Description:   "Use this skill to create Word documents, .docx files, and formatted reports.",
		UserInvocable: true,
		RunAsJob:      true,
		Metadata:      map[string]any{"produces_artifacts": true},
	}}}
	planner := NewRuntimeDeepAgentPlanner(runtime)
	action, err := planner.NextAction(context.Background(), &DeepAgentState{
		Goal:          "使用 docx skill 生成 Word 文档 artifact",
		WorkingMemory: map[string]any{"user_id": "alice", "session_id": "session-1"},
	}, DeepAgentStep{
		ID:            "write-docx",
		Title:         "生成 Word 文档",
		Intent:        "使用 docx skill 生成一份 Word 文档 artifact。",
		DoneCondition: "成功生成 .docx artifact。",
	})
	if err != nil {
		t.Fatalf("NextAction() error = %v", err)
	}
	if action.Tool != DeepAgentToolModeSkill {
		t.Fatalf("docx action tool = %q, want skill: %#v", action.Tool, action)
	}
	if got := deepAgentWorkflowString(action.Args, "skill_name"); got != "docx" {
		t.Fatalf("skill_name = %q, want docx in %#v", got, action.Args)
	}
	if got := deepAgentWorkflowString(action.Args, "deliverable_type"); got != deepAgentDeliverableDocx {
		t.Fatalf("deliverable_type = %q, want docx in %#v", got, action.Args)
	}
}

func TestRuntimeDeepAgentRouterPrefersDocumentsSkillForWordResearchDocument(t *testing.T) {
	runtime := NewRuntime(
		RuntimeConfig{},
		NewFileSessionStore(t.TempDir()),
		nil,
		nil,
		func(Scope) Runner { return echoRunner{} },
	)
	runtime.skills = fakeSkillCatalog{skills: []*skills.SkillDefinition{
		{
			Name:          "docx",
			Description:   "Use this skill to create Word documents and .docx files.",
			UserInvocable: true,
			RunAsJob:      true,
			Metadata:      map[string]any{"produces_artifacts": true},
		},
		{
			Name:          "documents",
			Description:   "Create, edit, render, and verify Word documents and .docx artifacts.",
			UserInvocable: true,
			RunAsJob:      true,
			Metadata:      map[string]any{"produces_artifacts": true},
		},
	}}
	steps := ruleDeepAgentFallbackSteps("帮我调研一下 Tolan 这款 AI 产品，然后生成一个word调研文档")
	if len(steps) != 3 {
		t.Fatalf("expected fallback research plan, got %#v", steps)
	}
	planner := NewRuntimeDeepAgentPlanner(runtime)
	action, err := planner.NextAction(context.Background(), &DeepAgentState{
		Goal:          "帮我调研一下 Tolan 这款 AI 产品，然后生成一个word调研文档",
		WorkingMemory: map[string]any{"user_id": "alice", "session_id": "session-1"},
	}, steps[2])
	if err != nil {
		t.Fatalf("NextAction() error = %v", err)
	}
	if action.Tool != DeepAgentToolModeSkill {
		t.Fatalf("word research document action tool = %q, want skill: %#v", action.Tool, action)
	}
	if got := deepAgentWorkflowString(action.Args, "skill_name"); got != "documents" {
		t.Fatalf("skill_name = %q, want documents in %#v", got, action.Args)
	}
	if got := deepAgentWorkflowString(action.Args, "deliverable_type"); got != deepAgentDeliverableDocx {
		t.Fatalf("deliverable_type = %q, want docx in %#v", got, action.Args)
	}
	if got := deepAgentWorkflowString(action.Args, "filename_hint"); !strings.HasSuffix(got, ".docx") {
		t.Fatalf("filename_hint = %q, want .docx in %#v", got, action.Args)
	}
}

func TestDeepAgentModelArtifactFallbackRejectsDocxDeliverable(t *testing.T) {
	runtime := NewRuntime(
		RuntimeConfig{},
		NewFileSessionStore(t.TempDir()),
		nil,
		nil,
		func(Scope) Runner { return echoRunner{} },
	)
	runtime.SetArtifactService(NewArtifactService(newMemoryArtifactStore(), NewFileObjectStore(t.TempDir()), "artifacts"))
	executor := NewRuntimeDeepAgentExecutor(runtime)
	_, err := executor.createDeepAgentModelArtifact(context.Background(), "alice", "session-1", DeepAgentAction{
		StepID: "write-docx",
		Args: map[string]any{
			"deliverable_type": deepAgentDeliverableDocx,
			"filename_hint":    "tolan-report.docx",
		},
	}, "The Word document was created.")
	if err == nil || !strings.Contains(err.Error(), "refusing markdown fallback") {
		t.Fatalf("expected docx markdown fallback rejection, got %v", err)
	}
}

func TestRuntimeDeepAgentRouterUsesDiagramSkillForSVGArtifact(t *testing.T) {
	runtime := NewRuntime(
		RuntimeConfig{},
		NewFileSessionStore(t.TempDir()),
		nil,
		nil,
		func(Scope) Runner { return echoRunner{} },
	)
	runtime.skills = fakeSkillCatalog{skills: []*skills.SkillDefinition{{
		Name:          "fireworks-tech-graph",
		Description:   "Generate technical diagrams, architecture diagrams, flowcharts, and SVG artifacts.",
		UserInvocable: true,
		RunAsJob:      true,
		Metadata:      map[string]any{"produces_artifacts": true},
	}}}
	planner := NewRuntimeDeepAgentPlanner(runtime)
	action, err := planner.NextAction(context.Background(), &DeepAgentState{
		Goal:          "使用 fireworks-tech-graph skill 生成 SVG 架构图 artifact",
		WorkingMemory: map[string]any{"user_id": "alice", "session_id": "session-1"},
	}, DeepAgentStep{
		ID:            "draw-svg",
		Title:         "生成 DeepAgent 架构流程图",
		Intent:        "使用 fireworks-tech-graph skill 创建 DeepAgent 流程架构图。",
		DoneCondition: "成功生成 SVG artifact。",
	})
	if err != nil {
		t.Fatalf("NextAction() error = %v", err)
	}
	if action.Tool != DeepAgentToolModeSkill {
		t.Fatalf("diagram action tool = %q, want skill: %#v", action.Tool, action)
	}
	if got := deepAgentWorkflowString(action.Args, "skill_name"); got != "fireworks-tech-graph" {
		t.Fatalf("skill_name = %q, want fireworks-tech-graph in %#v", got, action.Args)
	}
	if got := deepAgentWorkflowString(action.Args, "deliverable_type"); got != deepAgentDeliverableSVG {
		t.Fatalf("deliverable_type = %q, want svg in %#v", got, action.Args)
	}
}

func TestRuntimeDeepAgentStepRouterParsesLLMJSONRoute(t *testing.T) {
	runtime := NewRuntime(
		RuntimeConfig{},
		NewFileSessionStore(t.TempDir()),
		nil,
		nil,
		func(Scope) Runner { return routeJSONRunner{} },
	)
	route, err := NewRuntimeDeepAgentStepRouter(runtime).RouteStep(context.Background(), &DeepAgentState{
		Goal:          "prepare final customer deliverable",
		WorkingMemory: map[string]any{"user_id": "alice", "session_id": "session-1"},
	}, DeepAgentStep{
		ID:            "finalize",
		Title:         "Finalize customer deliverable",
		Intent:        "Package the final answer",
		DoneCondition: "The final answer is ready",
	})
	if err != nil {
		t.Fatalf("RouteStep() error = %v", err)
	}
	if route.Mode != DeepAgentToolModeModelArtifact || route.Executor != deepAgentRouteExecutorArtifact || !route.RequiresArtifact {
		t.Fatalf("unexpected LLM JSON route: %#v", route)
	}
	if route.DeliverableType != deepAgentDeliverableMarkdown || strings.Join(route.AllowedTools, ",") != "WebSearch,WebFetch,Artifact" {
		t.Fatalf("unexpected route deliverable/tools: %#v", route)
	}
}

func TestRuntimeDeepAgentStepRouterFallsBackOnInvalidLLMRoute(t *testing.T) {
	runtime := NewRuntime(
		RuntimeConfig{},
		NewFileSessionStore(t.TempDir()),
		nil,
		nil,
		func(Scope) Runner { return invalidRouteRunner{} },
	)
	route, err := NewRuntimeDeepAgentStepRouter(runtime).RouteStep(context.Background(), &DeepAgentState{
		Goal:          "handle a generic step",
		WorkingMemory: map[string]any{"user_id": "alice", "session_id": "session-1"},
	}, DeepAgentStep{
		ID:            "generic",
		Title:         "Handle generic task",
		Intent:        "Do the next thing",
		DoneCondition: "The next thing is done",
	})
	if err != nil {
		t.Fatalf("RouteStep() error = %v", err)
	}
	if route.Mode != DeepAgentToolModeModel || !strings.Contains(route.Reason, "llm router failed") {
		t.Fatalf("unexpected fallback route: %#v", route)
	}
}

func TestRuntimeDeepAgentStepRouterRecordsShadowDiff(t *testing.T) {
	runtime := NewRuntime(
		RuntimeConfig{DeepAgent: DeepAgentRuntimeConfig{V2Enabled: true, V2ShadowRoute: true}},
		NewFileSessionStore(t.TempDir()),
		nil,
		nil,
		func(Scope) Runner { return echoRunner{} },
	)
	state := &DeepAgentState{Goal: "调研 Tolan AI", WorkingMemory: map[string]any{"user_id": "alice", "session_id": "session-1"}}
	step := DeepAgentStep{
		ID:            "research",
		Title:         "调研 Tolan AI 产品",
		Intent:        "联网调研 Tolan AI 产品信息",
		DoneCondition: "收集公开资料和来源",
	}
	route, err := NewRuntimeDeepAgentStepRouter(runtime).RouteStep(context.Background(), state, step)
	if err != nil {
		t.Fatalf("RouteStep() error = %v", err)
	}
	if route.Version != "v2" {
		t.Fatalf("route version = %q, want v2: %#v", route.Version, route)
	}
	if len(route.ShadowRoute) == 0 {
		t.Fatalf("expected shadow route: %#v", route)
	}
	if len(route.ShadowDiff) == 0 {
		t.Fatalf("expected route shadow diff: %#v", route)
	}
	action, err := NewRuntimeDeepAgentPlanner(runtime).actionForRoute(state, step, route)
	if err != nil {
		t.Fatalf("actionForRoute() error = %v", err)
	}
	if got := deepAgentWorkflowString(action.Args, "route_version"); got != "v2" {
		t.Fatalf("action route_version = %q, want v2 in %#v", got, action.Args)
	}
	actionRoute, _ := deepAgentStepRouteFromMap(action.Args)
	if len(actionRoute.ShadowDiff) == 0 {
		t.Fatalf("action route should carry shadow diff: %#v", action.Args)
	}
}

func TestRuntimeDeepAgentExecutorRegistryReturnsArtifactEvidence(t *testing.T) {
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
	route := DeepAgentStepRoute{
		StepID:           "write-report",
		Mode:             DeepAgentToolModeModelArtifact,
		Executor:         deepAgentRouteExecutorArtifact,
		RequiresArtifact: true,
		DeliverableType:  deepAgentDeliverableMarkdown,
		AllowedTools:     []string{"WebSearch", "WebFetch", ArtifactToolName},
		Reason:           "test artifact route",
		Confidence:       "high",
	}
	result, err := NewRuntimeDeepAgentExecutor(runtime).ExecuteDeepAgentAction(context.Background(), DeepAgentAction{
		StepID: "write-report",
		Tool:   DeepAgentToolModeModelArtifact,
		Args: map[string]any{
			"user_id":           "alice",
			"session_id":        session.ID,
			"prompt":            "生成 Markdown 调研报告",
			"requires_artifact": true,
			"step_route":        deepAgentStepRouteMap(route),
		},
	}, &DeepAgentState{Goal: "生成调研报告", WorkingMemory: map[string]any{"user_id": "alice", "session_id": session.ID}})
	if err != nil {
		t.Fatalf("ExecuteDeepAgentAction() error = %v", err)
	}
	if result.Status != DeepAgentActionStatusSucceeded || !result.Completed {
		t.Fatalf("unexpected registry result: %#v", result)
	}
	evidence, ok := deepAgentStepEvidenceFromAny(result.Metadata["step_evidence"])
	if !ok {
		t.Fatalf("missing step evidence: %#v", result.Metadata)
	}
	if evidence.Route.Executor != deepAgentRouteExecutorArtifact || len(evidence.Artifacts) != 1 {
		t.Fatalf("unexpected evidence: %#v", evidence)
	}
	if evidence.Artifacts[0].StepID != "write-report" || !strings.HasSuffix(evidence.Artifacts[0].Filename, ".md") {
		t.Fatalf("unexpected artifact evidence: %#v", evidence.Artifacts)
	}
	if strings.Contains(result.Output, "## 摘要") || strings.Contains(result.Output, "Tolan AI 是一个 AI 产品") {
		t.Fatalf("artifact step should return a concise artifact pointer, got full report output: %q", result.Output)
	}
	if !strings.Contains(result.Output, "Artifacts") {
		t.Fatalf("artifact step should point to the Artifacts panel, got %q", result.Output)
	}
}

func TestDedicatedTestExecutorRunsAllowlistedCommand(t *testing.T) {
	runtime := testRuntime(t)
	registry := NewRuntimeDeepAgentExecutorRegistry(runtime, nil)
	evidence, err := registry.ExecuteStep(context.Background(), DeepAgentStepRoute{
		StepID:   "verify",
		Mode:     DeepAgentToolModeTest,
		Executor: deepAgentRouteExecutorTest,
	}, DeepAgentAction{
		StepID: "verify",
		Tool:   DeepAgentToolModeTest,
		Args: map[string]any{
			"command_args":     []string{"go", "test", ".", "-run", "TestDedicatedTestExecutorRunsAllowlistedCommand", "-count=0"},
			"working_dir":      ".",
			"allowed_commands": []string{"go test *"},
			"timeout_ms":       120000,
		},
	}, &DeepAgentState{})
	if err != nil {
		t.Fatalf("ExecuteStep() error = %v evidence=%#v", err, evidence)
	}
	if evidence.ErrorClass != "" || deepAgentWorkflowString(evidence.Diagnostics, "command") == "" || evidence.SideEffectLevel != deepAgentSideEffectReadonly {
		t.Fatalf("unexpected test evidence: %#v", evidence)
	}
	if got := deepAgentWorkflowString(evidence.Diagnostics, "dedicated_executor"); got != deepAgentRouteExecutorTest {
		t.Fatalf("dedicated executor = %q", got)
	}
}

func TestResearchReportVerifyTemplateUsesStateVerification(t *testing.T) {
	req := applyDeepAgentTaskTemplateToTaskRequest(DeepAgentTaskRequest{
		Goal:  "帮我调研一下chance ai这款产品",
		State: map[string]any{"template_id": DeepAgentTemplateResearchReport},
	})
	var verifyStep DeepAgentStep
	for i := range req.Plan.Steps {
		if req.Plan.Steps[i].ID == "verify" {
			verifyStep = req.Plan.Steps[i]
			break
		}
		req.Plan.Steps[i].Status = DeepAgentStepStatusSucceeded
	}
	if verifyStep.ID == "" {
		t.Fatalf("research template missing verify step: %#v", req.Plan.Steps)
	}
	state := &DeepAgentState{
		Goal:          req.Goal,
		Plan:          req.Plan,
		Rubric:        req.Rubric,
		WorkingMemory: req.State,
	}
	state.WorkingMemory["step_context"] = map[string]any{
		"artifact": map[string]any{
			"artifact_refs": []DeepAgentArtifactRef{{
				ID:          "artifact-1",
				Filename:    "chance-ai-report.md",
				ContentType: "text/markdown",
				SizeBytes:   1024,
				StepID:      "artifact",
			}},
		},
	}
	(StateDeepAgentEvidenceStore{}).PutStepEvidence(state, DeepAgentStepEvidence{
		StepID:  "gather",
		Summary: "Collected findings, caveats, and next steps from multiple sources. Coverage includes company team, product features, pricing availability, user reviews, competitors, and risks uncertainty.",
		Sources: []DeepAgentSourceRef{
			{URL: "https://example.com/chance-ai/about", Title: "Chance AI company team and product features", Snippet: "Official notes about company team, product features, and pricing availability.", Provider: "WebSearch"},
			{URL: "https://example.com/chance-ai-review", Title: "Chance AI user reviews and competitors", Snippet: "User reviews compare competitors and note risks uncertainty.", Provider: "WebSearch"},
		},
	})
	(StateDeepAgentEvidenceStore{}).PutStepEvidence(state, DeepAgentStepEvidence{
		StepID:  "artifact",
		Summary: "Final report artifact states findings, caveats, next steps, source quality and coverage verification.",
		Artifacts: []DeepAgentArtifactRef{{
			ID:          "artifact-1",
			Filename:    "chance-ai-report.md",
			ContentType: "text/markdown",
			SizeBytes:   1024,
			StepID:      "artifact",
		}},
	})
	action, err := ruleDeepAgentPlanner{}.NextAction(context.Background(), state, verifyStep)
	if err != nil {
		t.Fatalf("NextAction() error = %v", err)
	}
	if !deepAgentBool(action.Args, "state_verification", false) {
		t.Fatalf("verify action missing state_verification arg: %#v", action.Args)
	}
	registry := NewRuntimeDeepAgentExecutorRegistry(testRuntime(t), nil)
	evidence, err := registry.ExecuteStep(context.Background(), DeepAgentStepRoute{StepID: "verify", Mode: DeepAgentToolModeTest, Executor: deepAgentRouteExecutorTest}, action, state)
	if err != nil {
		t.Fatalf("state verification should pass without command args: %v evidence=%#v", err, evidence)
	}
	if got := deepAgentWorkflowString(evidence.Diagnostics, "verification_source"); got != "deep_agent_state" {
		t.Fatalf("verification_source = %q, want deep_agent_state; evidence=%#v", got, evidence)
	}
}

func TestRuntimePlannerPreservesResearchVerifyStateVerification(t *testing.T) {
	req := applyDeepAgentTaskTemplateToTaskRequest(DeepAgentTaskRequest{
		Goal:  "帮我调研一下chance ai这款产品",
		State: map[string]any{"template_id": DeepAgentTemplateResearchReport},
	})
	var verifyStep DeepAgentStep
	for i := range req.Plan.Steps {
		if req.Plan.Steps[i].ID == "verify" {
			verifyStep = req.Plan.Steps[i]
			break
		}
		req.Plan.Steps[i].Status = DeepAgentStepStatusSucceeded
	}
	if verifyStep.ID == "" {
		t.Fatalf("research template missing verify step: %#v", req.Plan.Steps)
	}
	state := &DeepAgentState{
		Goal:          req.Goal,
		Plan:          req.Plan,
		Rubric:        req.Rubric,
		WorkingMemory: req.State,
	}
	state.WorkingMemory["step_context"] = map[string]any{
		"artifact": map[string]any{
			"artifact_refs": []DeepAgentArtifactRef{{
				ID:          "artifact-1",
				Filename:    "chance-ai-report.md",
				ContentType: "text/markdown",
				SizeBytes:   1024,
				StepID:      "artifact",
			}},
		},
	}
	(StateDeepAgentEvidenceStore{}).PutStepEvidence(state, DeepAgentStepEvidence{
		StepID:  "gather",
		Summary: "Collected findings, caveats, and next steps from multiple sources. Coverage includes company team, product features, pricing availability, user reviews, competitors, and risks uncertainty.",
		Sources: []DeepAgentSourceRef{
			{URL: "https://example.com/chance-ai/about", Title: "Chance AI company team and product features", Snippet: "Official notes about company team, product features, and pricing availability.", Provider: "WebSearch"},
			{URL: "https://example.com/chance-ai-review", Title: "Chance AI user reviews and competitors", Snippet: "User reviews compare competitors and note risks uncertainty.", Provider: "WebSearch"},
		},
	})
	(StateDeepAgentEvidenceStore{}).PutStepEvidence(state, DeepAgentStepEvidence{
		StepID:  "artifact",
		Summary: "Final report artifact states findings, caveats, next steps, source quality and coverage verification.",
		Artifacts: []DeepAgentArtifactRef{{
			ID:          "artifact-1",
			Filename:    "chance-ai-report.md",
			ContentType: "text/markdown",
			SizeBytes:   1024,
			StepID:      "artifact",
		}},
	})
	runtime := testRuntime(t)
	route, err := NewRuntimeDeepAgentStepRouter(runtime).RouteStep(context.Background(), state, verifyStep)
	if err != nil {
		t.Fatalf("RouteStep() error = %v", err)
	}
	action, err := NewRuntimeDeepAgentPlanner(runtime).actionForRoute(state, verifyStep, route)
	if err != nil {
		t.Fatalf("actionForRoute() error = %v", err)
	}
	if !deepAgentBool(action.Args, "state_verification", false) {
		t.Fatalf("runtime planner dropped state_verification arg: %#v", action.Args)
	}
	registry := NewRuntimeDeepAgentExecutorRegistry(runtime, nil)
	evidence, err := registry.ExecuteStep(context.Background(), route, action, state)
	if err != nil {
		t.Fatalf("runtime-planned state verification should pass without command args: %v evidence=%#v", err, evidence)
	}
	if got := deepAgentWorkflowString(evidence.Diagnostics, "verification_source"); got != "deep_agent_state" {
		t.Fatalf("verification_source = %q, want deep_agent_state; evidence=%#v", got, evidence)
	}
}

func TestResearchReportStateVerificationRequiresMultipleSources(t *testing.T) {
	req := applyDeepAgentTaskTemplateToTaskRequest(DeepAgentTaskRequest{
		Goal:  "帮我调研一下chance ai这款产品",
		State: map[string]any{"template_id": DeepAgentTemplateResearchReport},
	})
	var verifyStep DeepAgentStep
	for i := range req.Plan.Steps {
		if req.Plan.Steps[i].ID == "verify" {
			verifyStep = req.Plan.Steps[i]
			break
		}
		req.Plan.Steps[i].Status = DeepAgentStepStatusSucceeded
	}
	state := &DeepAgentState{
		Goal:          req.Goal,
		Plan:          req.Plan,
		Rubric:        req.Rubric,
		WorkingMemory: req.State,
	}
	state.WorkingMemory["step_context"] = map[string]any{
		"artifact": map[string]any{
			"artifact_refs": []DeepAgentArtifactRef{{
				ID:          "artifact-1",
				Filename:    "chance-ai-report.md",
				ContentType: "text/markdown",
				SizeBytes:   1024,
				StepID:      "artifact",
			}},
		},
	}
	(StateDeepAgentEvidenceStore{}).PutStepEvidence(state, DeepAgentStepEvidence{
		StepID:  "gather",
		Summary: "Collected findings, caveats, and next steps from one source.",
		Sources: []DeepAgentSourceRef{{URL: "https://example.com/chance-ai", Title: "Chance AI source", Provider: "WebSearch"}},
	})
	(StateDeepAgentEvidenceStore{}).PutStepEvidence(state, DeepAgentStepEvidence{
		StepID:  "artifact",
		Summary: "Final report artifact states findings, caveats, and next steps.",
		Artifacts: []DeepAgentArtifactRef{{
			ID:          "artifact-1",
			Filename:    "chance-ai-report.md",
			ContentType: "text/markdown",
			SizeBytes:   1024,
			StepID:      "artifact",
		}},
	})
	action, err := ruleDeepAgentPlanner{}.NextAction(context.Background(), state, verifyStep)
	if err != nil {
		t.Fatalf("NextAction() error = %v", err)
	}
	registry := NewRuntimeDeepAgentExecutorRegistry(testRuntime(t), nil)
	evidence, err := registry.ExecuteStep(context.Background(), DeepAgentStepRoute{StepID: "verify", Mode: DeepAgentToolModeTest, Executor: deepAgentRouteExecutorTest}, action, state)
	if err == nil || !strings.Contains(evidence.Output, "multiple source URLs or citations") {
		t.Fatalf("expected multiple-source verification failure, err=%v evidence=%#v", err, evidence)
	}
}

func TestResearchReportFinalVerificationRequiresCriticalCoverage(t *testing.T) {
	state := testResearchReportVerificationState(t,
		"Collected two traceable sources for Chance AI.",
		[]DeepAgentSourceRef{
			{URL: "https://example.com/chance-ai/about", Title: "Chance AI company page", Snippet: "Company and team notes.", Provider: "WebSearch"},
			{URL: "https://example.com/chance-ai/features", Title: "Chance AI product features", Snippet: "Product feature notes.", Provider: "WebSearch"},
		},
	)
	final, err := ruleDeepAgentVerifier{}.CheckFinal(context.Background(), state)
	if err != nil {
		t.Fatalf("CheckFinal() error = %v", err)
	}
	if final.Done || !deepAgentVerificationHasCheck(final.Checks, "coverage_verifier", false) {
		t.Fatalf("research report should fail when critical coverage is missing, got %#v", final)
	}
	if final.ResearchQuality == nil || len(final.ResearchQuality.Coverage.Missing) == 0 {
		t.Fatalf("research quality should report missing coverage, got %#v", final.ResearchQuality)
	}
}

func TestResearchReportFinalVerificationRecordsQualityMetadata(t *testing.T) {
	state := testResearchReportVerificationState(t,
		"Collected multiple source URLs and citations. Coverage includes company team, product features, pricing availability, user reviews, competitors, and risks uncertainty.",
		[]DeepAgentSourceRef{
			{URL: "https://example.com/chance-ai/about", Title: "Chance AI company team and product features", Snippet: "Official notes about company team, product features, and pricing availability.", Provider: "WebSearch"},
			{URL: "https://example.com/chance-ai-review", Title: "Chance AI user reviews and competitors", Snippet: "User reviews compare competitors and note risks uncertainty.", Provider: "WebSearch"},
		},
	)
	final, err := ruleDeepAgentVerifier{}.CheckFinal(context.Background(), state)
	if err != nil {
		t.Fatalf("CheckFinal() error = %v", err)
	}
	if !final.Done {
		t.Fatalf("research report with coverage should pass, got %#v", final)
	}
	if final.ResearchQuality == nil {
		t.Fatalf("research quality metadata missing")
	}
	if final.ResearchQuality.SourceCount != 2 || len(final.ResearchQuality.Coverage.Missing) != 0 || final.ResearchQuality.Confidence == "" {
		t.Fatalf("unexpected research quality metadata: %#v", final.ResearchQuality)
	}
	recordDeepAgentFinalVerification(state, final)
	summary := deepAgentFinalAnswerEvidenceForSummary(state)
	if summary.ResearchQuality == nil || summary.ResearchQuality.SourceCount != 2 {
		t.Fatalf("summary should expose research quality metadata, got %#v", summary.ResearchQuality)
	}
}

func TestResearchReportFinalVerificationBlocksEntityAmbiguity(t *testing.T) {
	state := testResearchReportVerificationState(t,
		"Collected multiple source URLs and citations. Coverage includes company team, product features, pricing availability, user reviews, competitors, and risks uncertainty.",
		[]DeepAgentSourceRef{
			{URL: "https://example.com/chance-ai/about", Title: "Chance AI company team and product features", Snippet: "Official notes about company team, product features, and pricing availability.", Provider: "WebSearch"},
			{URL: "https://example.com/chance-ai-review", Title: "Chance AI user reviews and competitors", Snippet: "User reviews compare competitors and note risks uncertainty.", Provider: "WebSearch"},
		},
	)
	state.WorkingMemory["entity_ambiguity"] = true
	final, err := ruleDeepAgentVerifier{}.CheckFinal(context.Background(), state)
	if err != nil {
		t.Fatalf("CheckFinal() error = %v", err)
	}
	if final.Done || !deepAgentVerificationHasCheck(final.Checks, "entity_verifier", false) {
		t.Fatalf("entity ambiguity should block research report completion, got %#v", final)
	}
}

func testResearchReportVerificationState(t *testing.T, gatherSummary string, sources []DeepAgentSourceRef) *DeepAgentState {
	t.Helper()
	req := applyDeepAgentTaskTemplateToTaskRequest(DeepAgentTaskRequest{
		Goal:  "帮我调研一下chance ai这款产品",
		State: map[string]any{"template_id": DeepAgentTemplateResearchReport},
	})
	for i := range req.Plan.Steps {
		req.Plan.Steps[i].Status = DeepAgentStepStatusSucceeded
	}
	state := &DeepAgentState{
		Goal:          req.Goal,
		Plan:          req.Plan,
		Rubric:        req.Rubric,
		WorkingMemory: req.State,
	}
	state.WorkingMemory["step_context"] = map[string]any{}
	(StateDeepAgentEvidenceStore{}).PutStepEvidence(state, DeepAgentStepEvidence{
		StepID:  "gather",
		Summary: gatherSummary,
		Sources: sources,
	})
	(StateDeepAgentEvidenceStore{}).PutStepEvidence(state, DeepAgentStepEvidence{
		StepID:  "artifact",
		Summary: "Final report artifact states findings, caveats, next steps, source quality and coverage verification.",
		Artifacts: []DeepAgentArtifactRef{{
			ID:          "artifact-1",
			Filename:    "chance-ai-report.md",
			ContentType: "text/markdown",
			SizeBytes:   1024,
			StepID:      "artifact",
		}},
	})
	return state
}

func TestDeepAgentSafeSideEffectLevelAllowsReadonly(t *testing.T) {
	for _, level := range []string{"", "none", "read", "readonly", "read_only", "read-only", "low"} {
		if !deepAgentSafeSideEffectLevel(level) {
			t.Fatalf("side effect level %q should be safe", level)
		}
	}
	if deepAgentSafeSideEffectLevel(deepAgentSideEffectWrite) {
		t.Fatalf("side effect level %q should require review", deepAgentSideEffectWrite)
	}
}

func TestVerifyStepAcceptsStateVerifiedUpstreamArtifact(t *testing.T) {
	step := DeepAgentStep{
		ID:            "verify",
		Title:         "校验来源、结论和交付物",
		DoneCondition: "来源、结论和 artifact 均通过校验",
	}
	evidence := DeepAgentStepEvidence{
		StepID: "verify",
		Route: DeepAgentStepRoute{
			StepID:           "verify",
			Mode:             DeepAgentToolModeTest,
			Executor:         deepAgentRouteExecutorTest,
			RequiresArtifact: true,
			DeliverableType:  deepAgentDeliverableMarkdown,
		},
		Output:  "DeepAgent state verification passed.",
		Summary: "DeepAgent state verification passed.",
		Artifacts: []DeepAgentArtifactRef{{
			ID:          "artifact-1",
			Filename:    "Chance_AI_调研报告.md",
			ContentType: "text/markdown",
			SizeBytes:   1024,
			StepID:      "artifact",
			RunID:       "run-1",
			JobID:       "job-1",
		}},
		Diagnostics: map[string]any{
			"verification_source": "deep_agent_state",
		},
	}
	ok, reason := verifyDeepAgentStepEvidence(step, DeepAgentActionResult{
		Status:    DeepAgentActionStatusSucceeded,
		Completed: true,
		Metadata: map[string]any{
			"run_id":        "run-1",
			"job_id":        "job-1",
			"step_evidence": evidence,
		},
	}, evidence)
	if !ok {
		t.Fatalf("verify step should accept upstream artifact after state verification: %s", reason)
	}
}

func TestDedicatedTestExecutorBlocksUnlistedCommand(t *testing.T) {
	registry := NewRuntimeDeepAgentExecutorRegistry(testRuntime(t), nil)
	evidence, err := registry.ExecuteStep(context.Background(), DeepAgentStepRoute{StepID: "verify", Mode: DeepAgentToolModeTest, Executor: deepAgentRouteExecutorTest}, DeepAgentAction{
		StepID: "verify",
		Tool:   DeepAgentToolModeTest,
		Args: map[string]any{
			"command":          "rm -rf tmp",
			"allowed_commands": []string{"go test *"},
		},
	}, &DeepAgentState{})
	if err == nil || evidence.ErrorClass != deepAgentErrorPolicyBlocked {
		t.Fatalf("expected policy blocked evidence, err=%v evidence=%#v", err, evidence)
	}
}

func TestDedicatedWebExecutorFetchesHTTPEvidence(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><head><title>DeepAgent Test</title></head><body>ok</body></html>"))
	}))
	defer server.Close()
	registry := NewRuntimeDeepAgentExecutorRegistry(testRuntime(t), nil)
	evidence, err := registry.ExecuteStep(context.Background(), DeepAgentStepRoute{StepID: "web", Mode: DeepAgentToolModeWeb, Executor: deepAgentRouteExecutorWeb}, DeepAgentAction{
		StepID: "web",
		Tool:   DeepAgentToolModeWeb,
		Args:   map[string]any{"url": server.URL},
	}, &DeepAgentState{})
	if err != nil {
		t.Fatalf("ExecuteStep() error = %v evidence=%#v", err, evidence)
	}
	if len(evidence.Sources) != 1 || deepAgentWorkflowString(evidence.Diagnostics, "status_code") != "200" {
		t.Fatalf("unexpected web evidence: %#v", evidence)
	}
}

func TestDedicatedWebExecutorClassifiesNetworkFailureTransient(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	url := server.URL
	server.Close()
	registry := NewRuntimeDeepAgentExecutorRegistry(testRuntime(t), nil)
	evidence, err := registry.ExecuteStep(context.Background(), DeepAgentStepRoute{StepID: "web", Mode: DeepAgentToolModeWeb, Executor: deepAgentRouteExecutorWeb}, DeepAgentAction{
		StepID: "web",
		Tool:   DeepAgentToolModeWeb,
		Args:   map[string]any{"url": url, "timeout_ms": 1000},
	}, &DeepAgentState{})
	if err == nil || evidence.ErrorClass != DeepAgentErrorTransient {
		t.Fatalf("expected transient network failure, err=%v evidence=%#v", err, evidence)
	}
}

func TestDedicatedCodePatchExecutorRecordsRollbackEvidence(t *testing.T) {
	registry := NewRuntimeDeepAgentExecutorRegistry(testRuntime(t), nil)
	diff := "diff --git a/example.txt b/example.txt\n--- a/example.txt\n+++ b/example.txt\n@@ -1 +1 @@\n-old\n+new\n"
	evidence, err := registry.ExecuteStep(context.Background(), DeepAgentStepRoute{StepID: "patch", Mode: DeepAgentToolModeCodePatch, Executor: deepAgentRouteExecutorCodePatch}, DeepAgentAction{
		StepID: "patch",
		Tool:   DeepAgentToolModeCodePatch,
		Args:   map[string]any{"diff": diff},
	}, &DeepAgentState{})
	if err != nil {
		t.Fatalf("ExecuteStep() error = %v", err)
	}
	if evidence.RollbackHint == "" || evidence.SideEffectLevel != deepAgentSideEffectReadonly {
		t.Fatalf("unexpected code patch evidence: %#v", evidence)
	}
	files := deepAgentStringSlice(evidence.Diagnostics["changed_files"])
	if len(files) != 1 || files[0] != "example.txt" {
		t.Fatalf("changed files = %#v evidence=%#v", files, evidence)
	}
}

func TestDedicatedSubplanExecutorMergesChildEvidence(t *testing.T) {
	registry := NewRuntimeDeepAgentExecutorRegistry(testRuntime(t), nil)
	evidence, err := registry.ExecuteStep(context.Background(), DeepAgentStepRoute{StepID: "sub", Mode: DeepAgentToolModeMulti, Executor: deepAgentRouteExecutorSubPlan}, DeepAgentAction{
		StepID: "sub",
		Tool:   DeepAgentToolModeMulti,
		Args: map[string]any{
			"task":             "read-only review",
			"child_job_id":     "job-child",
			"child_job_status": JobStatusSucceeded,
		},
	}, &DeepAgentState{})
	if err != nil {
		t.Fatalf("ExecuteStep() error = %v", err)
	}
	if len(evidence.ChildJobs) != 1 || evidence.ChildJobs[0].ID != "job-child" || evidence.SideEffectLevel != deepAgentSideEffectReadonly {
		t.Fatalf("unexpected subplan evidence: %#v", evidence)
	}
}

func TestDedicatedSubplanExecutorJoinsParallelBranchEvidence(t *testing.T) {
	registry := NewRuntimeDeepAgentExecutorRegistry(testRuntime(t), nil)
	var events []Event
	ctx := withJobEventEmitter(context.Background(), func(_ context.Context, event Event) error {
		events = append(events, event)
		return nil
	})
	evidence, err := registry.ExecuteStep(ctx, DeepAgentStepRoute{StepID: "research", Mode: DeepAgentToolModeMulti, Executor: deepAgentRouteExecutorSubPlan}, DeepAgentAction{
		StepID: "research",
		Tool:   DeepAgentToolModeMulti,
		Args: map[string]any{
			"branch_specs": []map[string]any{
				{"id": "company", "title": "Company", "task": "research company"},
				{"id": "market", "title": "Market", "task": "research market"},
			},
			"branch_results": []map[string]any{
				{
					"id":     "company",
					"title":  "Company",
					"status": DeepAgentActionStatusSucceeded,
					"output": "Company facts\nSources:\n- Browserless docs - https://docs.browserless.io/",
					"sources": []map[string]any{{
						"url":   "https://docs.browserless.io/",
						"title": "Browserless docs",
					}},
				},
				{
					"id":     "market",
					"title":  "Market",
					"status": DeepAgentActionStatusSucceeded,
					"output": "Market facts",
				},
			},
		},
	}, &DeepAgentState{})
	if err != nil {
		t.Fatalf("ExecuteStep() error = %v", err)
	}
	if evidence.Route.Mode != DeepAgentToolModeMulti || evidence.Diagnostics["parallel"] != true {
		t.Fatalf("expected parallel evidence, got %#v", evidence)
	}
	if got := deepAgentAnyInt(evidence.Diagnostics["succeeded_branch_count"], 0); got != 2 {
		t.Fatalf("succeeded branches = %d evidence=%#v", got, evidence)
	}
	if len(evidence.Sources) != 1 || evidence.Sources[0].URL != "https://docs.browserless.io/" {
		t.Fatalf("expected merged source evidence, got %#v", evidence.Sources)
	}
	if !strings.Contains(evidence.Output, "Parallel group completed: 2/2") {
		t.Fatalf("unexpected joined output: %s", evidence.Output)
	}
	var joined bool
	for _, event := range events {
		if event.Type == "deep_agent_parallel_group_joined" {
			joined = true
		}
	}
	if !joined {
		t.Fatalf("expected parallel join event, got %#v", events)
	}
}

func TestDedicatedSubplanExecutorRecordsParallelCoverageAndConflicts(t *testing.T) {
	registry := NewRuntimeDeepAgentExecutorRegistry(testRuntime(t), nil)
	evidence, err := registry.ExecuteStep(context.Background(), DeepAgentStepRoute{StepID: "research", Mode: DeepAgentToolModeMulti, Executor: deepAgentRouteExecutorSubPlan}, DeepAgentAction{
		StepID: "research",
		Tool:   DeepAgentToolModeMulti,
		Args: map[string]any{
			"branch_specs": []map[string]any{
				{"id": "overview", "title": "Overview", "task": "research company team product features pricing"},
				{"id": "market", "title": "Market", "task": "research pricing competitors users risks"},
			},
			"branch_results": []map[string]any{
				{
					"id":     "overview",
					"title":  "Overview",
					"status": DeepAgentActionStatusSucceeded,
					"output": "Company team: Browserless builds browser automation. Product features include hosted browsers. Pricing starts at $49 per month.",
					"sources": []map[string]any{{
						"url":   "https://www.browserless.io/pricing",
						"title": "Browserless pricing",
					}},
				},
				{
					"id":     "market",
					"title":  "Market",
					"status": DeepAgentActionStatusSucceeded,
					"output": "Competitors include Playwright cloud alternatives. User reviews mention reliability. Risks and uncertainty remain around scale. Pricing starts at $99 per month.",
					"sources": []map[string]any{{
						"url":   "https://docs.browserless.io/",
						"title": "Browserless docs",
					}},
				},
			},
		},
	}, &DeepAgentState{})
	if err != nil {
		t.Fatalf("ExecuteStep() error = %v", err)
	}
	if score := deepAgentAnyFloat(evidence.Diagnostics["coverage_score"], 0); score < 1 {
		t.Fatalf("expected complete coverage, score=%v diagnostics=%#v", score, evidence.Diagnostics)
	}
	if missing := deepAgentStringSlice(evidence.Diagnostics["missing_coverage"]); len(missing) != 0 {
		t.Fatalf("expected no missing coverage, got %#v", missing)
	}
	if got := deepAgentAnyInt(evidence.Diagnostics["conflict_count"], 0); got == 0 {
		t.Fatalf("expected pricing conflict, diagnostics=%#v output=%s", evidence.Diagnostics, evidence.Output)
	}
	if !strings.Contains(evidence.Output, "Conflicts detected") {
		t.Fatalf("expected conflict summary in output: %s", evidence.Output)
	}
}

func TestParallelCoverageMissingCreatesSupplementalSpec(t *testing.T) {
	executor := &runtimeDeepAgentSubplanExecutor{}
	action := DeepAgentAction{
		StepID: "research",
		Tool:   DeepAgentToolModeMulti,
		Args: map[string]any{
			"task":         "research Browserless",
			"max_branches": 6,
		},
	}
	specs := []DeepAgentParallelBranchSpec{{ID: "overview", Title: "Overview", Task: "research company and product"}}
	results := []DeepAgentParallelBranchResult{{
		ID:      "overview",
		Title:   "Overview",
		Status:  DeepAgentActionStatusSucceeded,
		Output:  "Company team and product features are documented.",
		Sources: []DeepAgentSourceRef{{URL: "https://docs.browserless.io/", Title: "Browserless docs"}},
	}}
	quality := deepAgentParallelQualityReportFor(specs, results)
	if len(quality.Coverage.Missing) == 0 {
		t.Fatalf("expected missing coverage, got %#v", quality)
	}
	supplemental := executor.deepAgentParallelSupplementalBranchSpecs(action, specs, results, quality)
	if len(supplemental) != 1 {
		t.Fatalf("expected one budgeted supplemental branch, got %#v", supplemental)
	}
	if !strings.Contains(supplemental[0].Task, quality.Coverage.Missing[0]) {
		t.Fatalf("supplement task should name missing coverage: %#v quality=%#v", supplemental[0], quality)
	}
}

func TestParallelConflictCreatesReconciliationSupplement(t *testing.T) {
	executor := &runtimeDeepAgentSubplanExecutor{}
	longTask := "research Browserless " + strings.Repeat("large context sentence. ", 240)
	action := DeepAgentAction{
		StepID: "research",
		Tool:   DeepAgentToolModeMulti,
		Args: map[string]any{
			"task":         longTask,
			"max_branches": 6,
		},
	}
	specs := []DeepAgentParallelBranchSpec{
		{ID: "pricing-a", Title: "Pricing A", Task: "research pricing"},
		{ID: "pricing-b", Title: "Pricing B", Task: "research pricing"},
		{ID: "coverage", Title: "Coverage", Task: "research company team product features users competitors risks"},
	}
	results := []DeepAgentParallelBranchResult{
		{ID: "pricing-a", Title: "Pricing A", Status: DeepAgentActionStatusSucceeded, Output: "Pricing starts at $49 per month."},
		{ID: "pricing-b", Title: "Pricing B", Status: DeepAgentActionStatusSucceeded, Output: "Pricing starts at $99 per month."},
		{ID: "coverage", Title: "Coverage", Status: DeepAgentActionStatusSucceeded, Output: "Company team product features users reviews competitors risks uncertainty are documented."},
	}
	quality := deepAgentParallelQualityReportFor(specs, results)
	if len(quality.Conflicts) == 0 {
		t.Fatalf("expected pricing conflict, got %#v", quality)
	}
	supplemental := executor.deepAgentParallelSupplementalBranchSpecs(action, specs, results, quality)
	if len(supplemental) == 0 || supplemental[len(supplemental)-1].ID != "supplement-conflict-reconciliation" {
		t.Fatalf("expected conflict reconciliation supplement, got %#v", supplemental)
	}
	conflictSpec := supplemental[len(supplemental)-1]
	if len(conflictSpec.Task) > 2500 {
		t.Fatalf("conflict reconciliation task should stay compact, len=%d task=%q", len(conflictSpec.Task), conflictSpec.Task)
	}
	if !strings.Contains(conflictSpec.Task, "Conflicts:") || !strings.Contains(conflictSpec.Task, "$49") || !strings.Contains(conflictSpec.Task, "$99") {
		t.Fatalf("conflict reconciliation task should include compact conflict evidence, got %q", conflictSpec.Task)
	}
	if !strings.Contains(deepAgentParallelPromptForBranch(action, conflictSpec), "Verify only the listed conflicting claims") {
		t.Fatalf("conflict branch should use specialized compact reconciliation prompt")
	}
	if strings.Contains(deepAgentParallelPromptForBranch(action, conflictSpec), "Do not create another multi-agent plan") {
		t.Fatalf("conflict branch prompt should not use the generic branch prompt")
	}
}

func TestParallelBranchTimeoutUsesGovernanceTimeout(t *testing.T) {
	runtime := NewRuntime(RuntimeConfig{
		LLMGovernanceProvider: func() LLMGovernanceConfig {
			return LLMGovernanceConfig{ChatTimeout: 1000 * time.Second, SkillTimeout: 1000 * time.Second}
		},
	}, nil, nil, nil, nil)
	executor := &runtimeDeepAgentSubplanExecutor{runtime: runtime}
	if got := executor.deepAgentParallelBranchTimeout(DeepAgentAction{}); got != 1000*time.Second {
		t.Fatalf("branch timeout should follow governance timeout, got %s", got)
	}
	if got := executor.deepAgentParallelBranchTimeout(DeepAgentAction{Args: map[string]any{"branch_timeout_ms": "120000"}}); got != 120*time.Second {
		t.Fatalf("explicit branch_timeout_ms should override governance timeout, got %s", got)
	}
}

func TestRuntimeDeepAgentJobPolicyUsesGovernanceTimeout(t *testing.T) {
	runtime := NewRuntime(RuntimeConfig{
		LLMGovernanceProvider: func() LLMGovernanceConfig {
			return LLMGovernanceConfig{ChatTimeout: 1000 * time.Second, SkillTimeout: 900 * time.Second}
		},
	}, nil, nil, nil, nil)
	policy := runtime.deepAgentJobPolicy()
	if policy.StepTimeout != 1000*time.Second {
		t.Fatalf("step timeout should follow governance timeout, got %s", policy.StepTimeout)
	}
	if policy.MaxDuration < 2000*time.Second {
		t.Fatalf("max duration should leave room for multi-step work, got %s", policy.MaxDuration)
	}
}

func TestParallelSupplementalSkippedWhenPrimaryBranchesAllFail(t *testing.T) {
	executor := &runtimeDeepAgentSubplanExecutor{}
	action := DeepAgentAction{Args: map[string]any{"max_branches": 6}}
	specs := []DeepAgentParallelBranchSpec{
		{ID: "overview", Title: "Overview", Task: "research overview"},
		{ID: "market", Title: "Market", Task: "research market"},
	}
	results := []DeepAgentParallelBranchResult{
		{ID: "overview", Title: "Overview", Status: DeepAgentActionStatusFailed, Error: "context deadline exceeded"},
		{ID: "market", Title: "Market", Status: DeepAgentActionStatusFailed, Error: "context deadline exceeded"},
	}
	quality := deepAgentParallelQualityReportFor(specs, results)
	if got := executor.deepAgentParallelSupplementalBranchSpecs(action, specs, results, quality); len(got) != 0 {
		t.Fatalf("all-failed primary groups should not spawn supplemental branches, got %#v", got)
	}
}

func TestParallelConflictFallbackResultIsNonBlocking(t *testing.T) {
	spec := DeepAgentParallelBranchSpec{
		ID:    "supplement-conflict-reconciliation",
		Title: "Conflict reconciliation",
		Task:  "Resolve conflicts:\n- pricing/default: $49 vs $99",
		Metadata: map[string]any{
			"conflict_reconcile": true,
			"supplemental":       true,
		},
	}
	result, ok := deepAgentParallelConflictFallbackResult(spec, "query loop failed: context deadline exceeded")
	if !ok {
		t.Fatalf("expected conflict timeout fallback")
	}
	if result.Status != DeepAgentActionStatusSucceeded {
		t.Fatalf("fallback should be non-blocking success, got %#v", result)
	}
	if result.Metadata["unresolved_conflicts"] != true || !strings.Contains(result.Output, "unresolved uncertainty") {
		t.Fatalf("fallback should preserve unresolved conflict uncertainty, got %#v", result)
	}
}

func TestParallelConflictReconciliationDefaultsToInlineResult(t *testing.T) {
	executor := &runtimeDeepAgentSubplanExecutor{}
	spec := DeepAgentParallelBranchSpec{
		ID:    "supplement-conflict-reconciliation",
		Title: "Conflict reconciliation",
		Task:  "Resolve conflicts:\n- pricing/default: $49 vs $99",
		Metadata: map[string]any{
			"conflict_reconcile": true,
			"supplemental":       true,
			"conflict_lines":     []string{"pricing/default: $49 vs $99"},
		},
	}
	result, err := executor.executeParallelBranch(context.Background(), nil, DeepAgentStepRoute{}, DeepAgentAction{}, spec, nil)
	if err != nil {
		t.Fatalf("executeParallelBranch() error = %v", err)
	}
	if result.Status != DeepAgentActionStatusSucceeded {
		t.Fatalf("inline conflict reconciliation should succeed, got %#v", result)
	}
	if result.Metadata["inline_conflict_reconciliation"] != true || result.Metadata["parallel_branch_skipped_deep_tools"] != true {
		t.Fatalf("expected inline conflict metadata, got %#v", result.Metadata)
	}
	if !strings.Contains(result.Output, "$49 vs $99") {
		t.Fatalf("inline result should preserve conflict evidence, got %q", result.Output)
	}
}

func TestParallelConflictReconciliationDeepModeIsExplicitOptIn(t *testing.T) {
	if deepAgentParallelDeepConflictReconciliationEnabled(DeepAgentAction{}) {
		t.Fatalf("deep conflict reconciliation should be disabled by default")
	}
	if !deepAgentParallelDeepConflictReconciliationEnabled(DeepAgentAction{Args: map[string]any{"run_conflict_reconciliation_branch": true}}) {
		t.Fatalf("run_conflict_reconciliation_branch should opt into deep reconciliation")
	}
	if !deepAgentParallelDeepConflictReconciliationEnabled(DeepAgentAction{Args: map[string]any{"deep_conflict_reconciliation": true}}) {
		t.Fatalf("deep_conflict_reconciliation should opt into deep reconciliation")
	}
}

func TestParallelBranchRouteForcesSingleBranchModelExecution(t *testing.T) {
	route := deepAgentParallelBranchRoute("research/overview", DeepAgentToolModeMulti, []string{"WebSearch", "WebFetch"})
	if route.Mode != DeepAgentToolModeModel || route.Executor != deepAgentRouteExecutorModel {
		t.Fatalf("branch route should force model execution, got %#v", route)
	}
	if route.RequiresArtifact {
		t.Fatalf("branch route should not require artifacts: %#v", route)
	}
	prompt := deepAgentParallelBranchPrompt(DeepAgentAction{Args: map[string]any{"goal": "使用 multi-agent 调研 Browserless"}}, DeepAgentParallelBranchSpec{
		ID:    "overview",
		Title: "Overview",
		Task:  "并行调研公司信息",
	})
	if !strings.Contains(prompt, "Do not create another multi-agent plan") {
		t.Fatalf("branch prompt should prohibit nested multi-agent execution")
	}
	if !strings.Contains(prompt, "Use at most 3 search queries") || !strings.Contains(prompt, "fetch at most 4 pages") {
		t.Fatalf("branch prompt should cap search/fetch budget, got %q", prompt)
	}
}

func TestDeepAgentSourceCurationNormalizesAndLimitsSources(t *testing.T) {
	refs := []DeepAgentSourceRef{
		{URL: "https://example.com/pricing?utm_source=newsletter#plans", Title: "Pricing", Provider: "WebSearch"},
		{URL: "https://example.com/pricing?utm_campaign=launch", Title: "Pricing duplicate", Provider: "WebFetch"},
		{URL: "https://docs.example.com/guide", Title: "Docs", Provider: "WebFetch"},
		{URL: "https://example.com/blog/update", Title: "Blog", Provider: "WebSearch"},
		{URL: "https://www.youtube.com/watch?v=abc", Title: "Video review", Provider: "WebSearch"},
		{URL: "https://www.producthunt.com/products/example", Title: "Product Hunt", Provider: "WebSearch"},
	}

	got := curateDeepAgentSourceRefs(refs, 3)
	if len(got) != 3 {
		t.Fatalf("curated sources len = %d, got %#v", len(got), got)
	}
	seen := map[string]bool{}
	for _, ref := range got {
		key := normalizeDeepAgentSourceURL(ref.URL)
		if seen[key] {
			t.Fatalf("duplicate normalized URL retained: %#v", got)
		}
		seen[key] = true
	}
	if !seen["https://example.com/pricing"] {
		t.Fatalf("expected normalized pricing source to be retained, got %#v", got)
	}
	exampleHostCount := 0
	for _, ref := range got {
		if deepAgentSourceRefHost(ref) == "example.com" {
			exampleHostCount++
		}
	}
	if exampleHostCount > deepAgentSourceMaxPerHost {
		t.Fatalf("expected host cap <= %d, got %d in %#v", deepAgentSourceMaxPerHost, exampleHostCount, got)
	}
}

func TestParallelJoinTreatsSupplementalFailureAsNonBlocking(t *testing.T) {
	registry := NewRuntimeDeepAgentExecutorRegistry(testRuntime(t), nil)
	evidence, err := registry.ExecuteStep(context.Background(), DeepAgentStepRoute{StepID: "research", Mode: DeepAgentToolModeMulti, Executor: deepAgentRouteExecutorSubPlan}, DeepAgentAction{
		StepID: "research",
		Tool:   DeepAgentToolModeMulti,
		Args: map[string]any{
			"min_successful_branches": 1,
			"branch_specs": []map[string]any{
				{"id": "overview", "title": "Overview", "task": "research company team product features pricing users competitors risks"},
				{"id": "supplement-conflict-reconciliation", "title": "Conflict reconciliation", "task": "resolve conflicts", "metadata": map[string]any{"supplemental": true}},
			},
			"branch_results": []map[string]any{
				{
					"id":     "overview",
					"title":  "Overview",
					"status": DeepAgentActionStatusSucceeded,
					"output": "Company team product features pricing users reviews competitors risks uncertainty are documented.",
				},
				{
					"id":     "supplement-conflict-reconciliation",
					"title":  "Conflict reconciliation",
					"status": DeepAgentActionStatusFailed,
					"error":  "context deadline exceeded",
					"metadata": map[string]any{
						"supplemental": true,
					},
				},
			},
		},
	}, &DeepAgentState{})
	if err != nil {
		t.Fatalf("supplemental failure should not fail join when primary branches succeeded: %v", err)
	}
	if evidence.Diagnostics["tool_result_valid"] != true {
		t.Fatalf("expected valid tool result, diagnostics=%#v", evidence.Diagnostics)
	}
	if got := deepAgentAnyInt(evidence.Diagnostics["primary_succeeded_count"], 0); got != 1 {
		t.Fatalf("primary succeeded count = %d, diagnostics=%#v", got, evidence.Diagnostics)
	}
	if got := deepAgentAnyInt(evidence.Diagnostics["failed_branch_count"], 0); got != 1 {
		t.Fatalf("total failed count should still include supplemental failure, got %d diagnostics=%#v", got, evidence.Diagnostics)
	}
}

func TestDeepAgentRouterRoutesExplicitParallelResearchToParallel(t *testing.T) {
	router := NewRuntimeDeepAgentStepRouter(testRuntime(t))
	route, err := router.RouteStep(context.Background(), &DeepAgentState{Goal: "research browserless"}, DeepAgentStep{
		ID:            "research",
		Title:         "并行多方向调研 Browserless 产品",
		Intent:        "拆成多个子任务并行收集公司/团队、产品功能、定价/可用性、用户评价、竞品、风险和不确定性",
		DoneCondition: "收集多来源证据，支撑后续报告写作",
	})
	if err != nil {
		t.Fatalf("RouteStep() error = %v", err)
	}
	if route.Mode != DeepAgentToolModeMulti || route.Executor != deepAgentRouteExecutorSubPlan {
		t.Fatalf("expected multi route, got %#v", route)
	}
}

func TestDeepAgentRouterRoutesReadonlyAuditButNotImplementationToParallel(t *testing.T) {
	if !deepAgentStepLooksParallelizable(DeepAgentStep{
		Title:         "代码库只读架构审查",
		Intent:        "并行分析多个模块的风险、边界和测试覆盖",
		DoneCondition: "完成只读审查，不产生代码变更",
	}) {
		t.Fatalf("expected readonly audit to be parallelizable")
	}
	if deepAgentStepLooksParallelizable(DeepAgentStep{
		Title:         "实现 multi-agent 调度器",
		Intent:        "修改后端代码并补测试",
		DoneCondition: "代码变更完成",
	}) {
		t.Fatalf("implementation task should not auto-route to parallel research")
	}
}

func TestDeepAgentStepRequiresArtifactIgnoresLaterDocumentSupport(t *testing.T) {
	step := DeepAgentStep{
		ID:            "research",
		Title:         "调研tolan产品",
		Intent:        "通过网络搜索，调研tolan产品的相关信息",
		DoneCondition: "收集了足够信息，能够支撑后续文档的撰写。",
	}
	if deepAgentStepRequiresArtifact(step) {
		t.Fatalf("research support step should not require artifact: %#v", step)
	}

	step.DoneCondition = "已获取 Tolan 产品事实，可以进行下一步的文档生成。"
	if deepAgentStepRequiresArtifact(step) {
		t.Fatalf("next-step support wording should not require artifact: %#v", step)
	}

	step.DoneCondition = "获取了关于Tolan AI产品的足够信息，可以用于生成调研文档。"
	if deepAgentStepRequiresArtifact(step) {
		t.Fatalf("used-for-generation wording should not require artifact: %#v", step)
	}

	step.DoneCondition = "找到了关于 Tolan 的详细信息，为撰写调研报告提供了充足的素材。"
	if deepAgentStepRequiresArtifact(step) {
		t.Fatalf("material-for-writing wording should not require artifact: %#v", step)
	}
}

func TestNormalizeDeepAgentPlanRewritesUnrequestedWordDeliverable(t *testing.T) {
	plan := normalizeDeepAgentPlan("帮我调研一下tolan这个产品，然后生成一个调研文档", DeepAgentPlan{Steps: []DeepAgentStep{{
		ID:            "write",
		Title:         "生成Word调研文档",
		Intent:        "生成一份Word格式的调研文档",
		DoneCondition: "成功生成一份Word调研文档，并将其作为产出物提供给用户。",
	}}})
	step := plan.Steps[0]
	joined := strings.Join([]string{step.Title, step.Intent, step.DoneCondition}, "\n")
	if strings.Contains(strings.ToLower(joined), "word") || strings.Contains(strings.ToLower(joined), "docx") {
		t.Fatalf("unrequested Word/docx should be normalized away: %#v", step)
	}
	if !strings.Contains(joined, "Markdown") {
		t.Fatalf("expected Markdown replacement: %#v", step)
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

func TestRuleDeepAgentVerifierRejectsArtifactStepWithoutRefs(t *testing.T) {
	step := DeepAgentStep{
		ID:            "write-report",
		Title:         "生成 Markdown 调研报告",
		Intent:        "生成调研报告 artifact",
		DoneCondition: "调研报告已生成并可下载",
	}
	result := DeepAgentActionResult{
		Status:    DeepAgentActionStatusSucceeded,
		Completed: true,
		Output:    "报告已完成",
		Metadata: map[string]any{
			"run_id":  "run-1",
			"job_id":  "job-1",
			"step_id": "write-report",
			"step_evidence": DeepAgentStepEvidence{
				StepID: "write-report",
				Route: DeepAgentStepRoute{
					Mode:             DeepAgentToolModeModelArtifact,
					Executor:         deepAgentRouteExecutorArtifact,
					RequiresArtifact: true,
					DeliverableType:  deepAgentDeliverableMarkdown,
				},
				Output: "报告已完成",
			},
		},
	}
	progress, err := ruleDeepAgentVerifier{}.CheckProgress(context.Background(), nil, step, DeepAgentAction{}, result)
	if err != nil {
		t.Fatalf("CheckProgress() error = %v", err)
	}
	if progress.StepDone || progress.MadeProgress || !strings.Contains(progress.Reason, "artifact refs") {
		t.Fatalf("expected missing artifact refs failure, got %#v", progress)
	}
}

func TestRuleDeepAgentVerifierRejectsHistoricalArtifactForFinalGoal(t *testing.T) {
	state := &DeepAgentState{
		Goal: "帮我调研一下 Tolan AI，并生成一个调研报告",
		Plan: DeepAgentPlan{Steps: []DeepAgentStep{{
			ID:     "write-report",
			Title:  "生成调研报告",
			Status: DeepAgentStepStatusSucceeded,
		}}},
		CompletedSteps: []string{"write-report"},
		WorkingMemory: map[string]any{
			deepAgentLoadedContextKey: DeepAgentLoadedContext{
				ExistingArtifacts: []DeepAgentArtifactRef{{
					ID:          "old-artifact",
					Filename:    "old-report.md",
					ContentType: "text/markdown",
					SizeBytes:   128,
					Source:      "session_artifact",
				}},
			},
		},
	}
	final, err := ruleDeepAgentVerifier{}.CheckFinal(context.Background(), state)
	if err != nil {
		t.Fatalf("CheckFinal() error = %v", err)
	}
	if final.Done || !strings.Contains(final.Reason, "required final artifact") {
		t.Fatalf("historical artifact should not satisfy final verification, got %#v", final)
	}
}

func TestRuleDeepAgentVerifierAcceptsCurrentMarkdownArtifactForFinalGoal(t *testing.T) {
	state := &DeepAgentState{
		Goal: "帮我调研一下 Tolan AI，并生成一个调研报告",
		Plan: DeepAgentPlan{Steps: []DeepAgentStep{{
			ID:     "write-report",
			Title:  "生成 Markdown 调研报告",
			Status: DeepAgentStepStatusSucceeded,
		}}},
		CompletedSteps: []string{"write-report"},
		WorkingMemory: map[string]any{
			"step_context": map[string]any{
				"write-report": map[string]any{
					"step_id": "write-report",
					"metadata": map[string]any{
						"run_id":  "run-1",
						"job_id":  "job-1",
						"step_id": "write-report",
						"step_evidence": DeepAgentStepEvidence{
							StepID: "write-report",
							Route: DeepAgentStepRoute{
								Mode:             DeepAgentToolModeModelArtifact,
								RequiresArtifact: true,
								DeliverableType:  deepAgentDeliverableMarkdown,
							},
							Artifacts: []DeepAgentArtifactRef{{
								ID:          "artifact-1",
								RunID:       "run-1",
								JobID:       "job-1",
								StepID:      "write-report",
								Filename:    "tolan-report.md",
								ContentType: "text/markdown",
								SizeBytes:   512,
							}},
						},
					},
				},
			},
		},
	}
	final, err := ruleDeepAgentVerifier{}.CheckFinal(context.Background(), state)
	if err != nil {
		t.Fatalf("CheckFinal() error = %v", err)
	}
	if !final.Done {
		t.Fatalf("current artifact should satisfy final verification, got %#v", final)
	}
	if refs := deepAgentArtifactRefsFromAny(state.WorkingMemory["final_artifact_refs"]); len(refs) != 1 || refs[0].ID != "artifact-1" {
		t.Fatalf("final artifact refs not persisted: %#v", state.WorkingMemory["final_artifact_refs"])
	}
}

func TestRuleDeepAgentVerifierRequiresSourcesForResearchTask(t *testing.T) {
	state := &DeepAgentState{
		Goal: "research Chance AI",
		Rubric: DeepAgentRubric{
			AcceptanceCriteria: []string{"Chance AI summary"},
		},
		Plan: DeepAgentPlan{Goal: "research Chance AI", Steps: []DeepAgentStep{{
			ID:            "research",
			Title:         "Research Chance AI",
			Intent:        "Collect Chance AI facts",
			DoneCondition: "Chance AI summary is ready",
			Status:        DeepAgentStepStatusSucceeded,
		}}},
		CompletedSteps: []string{"research"},
		WorkingMemory: map[string]any{
			"task_type":   "research",
			"deliverable": "answer",
			"step_context": map[string]any{
				"research": map[string]any{
					"step_evidence": DeepAgentStepEvidence{
						StepID:  "research",
						Summary: "Chance AI summary",
					},
				},
			},
		},
	}
	final, err := ruleDeepAgentVerifier{}.CheckFinal(context.Background(), state)
	if err != nil {
		t.Fatalf("CheckFinal() error = %v", err)
	}
	if final.Done || !deepAgentVerificationHasCheck(final.Checks, "source_verifier", false) {
		t.Fatalf("research task without sources should fail source verifier, got %#v", final)
	}
	evidence := state.WorkingMemory["step_context"].(map[string]any)["research"].(map[string]any)["step_evidence"].(DeepAgentStepEvidence)
	evidence.Sources = []DeepAgentSourceRef{{URL: "https://example.com/chance", Title: "Chance source", Provider: "WebSearch"}}
	state.WorkingMemory["step_context"].(map[string]any)["research"].(map[string]any)["step_evidence"] = evidence
	final, err = ruleDeepAgentVerifier{}.CheckFinal(context.Background(), state)
	if err != nil {
		t.Fatalf("CheckFinal() with sources error = %v", err)
	}
	if !final.Done || !deepAgentVerificationHasCheck(final.Checks, "source_verifier", true) || !deepAgentVerificationHasCheck(final.Checks, "content_verifier", true) {
		t.Fatalf("research task with sources should pass layered verifiers, got %#v", final)
	}
}

func TestRuleDeepAgentVerifierRequiresTestsOrNotTestedReasonForCodeFix(t *testing.T) {
	state := &DeepAgentState{
		Goal: "fix login bug",
		Plan: DeepAgentPlan{Goal: "fix login bug", Steps: []DeepAgentStep{{
			ID:            "fix",
			Title:         "Fix login bug",
			Intent:        "Patch login failure",
			DoneCondition: "Patch is ready",
			Status:        DeepAgentStepStatusSucceeded,
		}}},
		CompletedSteps: []string{"fix"},
		WorkingMemory: map[string]any{
			"task_type":   "code_fix",
			"deliverable": "answer",
			"step_context": map[string]any{
				"fix": map[string]any{
					"step_evidence": DeepAgentStepEvidence{
						StepID:  "fix",
						Summary: "Patched login bug",
					},
				},
			},
		},
	}
	final, err := ruleDeepAgentVerifier{}.CheckFinal(context.Background(), state)
	if err != nil {
		t.Fatalf("CheckFinal() error = %v", err)
	}
	if final.Done || !deepAgentVerificationHasCheck(final.Checks, "test_verifier", false) {
		t.Fatalf("code fix without test evidence should fail test verifier, got %#v", final)
	}
	evidence := state.WorkingMemory["step_context"].(map[string]any)["fix"].(map[string]any)["step_evidence"].(DeepAgentStepEvidence)
	evidence.Diagnostics = map[string]any{"not_tested_reason": "no test harness exists for this integration"}
	state.WorkingMemory["step_context"].(map[string]any)["fix"].(map[string]any)["step_evidence"] = evidence
	final, err = ruleDeepAgentVerifier{}.CheckFinal(context.Background(), state)
	if err != nil {
		t.Fatalf("CheckFinal() with not-tested reason error = %v", err)
	}
	if !final.Done || !deepAgentVerificationHasCheck(final.Checks, "test_verifier", true) {
		t.Fatalf("code fix with not-tested reason should pass test verifier, got %#v", final)
	}
	evidence.Diagnostics = map[string]any{"tests_passed": true}
	state.WorkingMemory["step_context"].(map[string]any)["fix"].(map[string]any)["step_evidence"] = evidence
	final, err = ruleDeepAgentVerifier{}.CheckFinal(context.Background(), state)
	if err != nil {
		t.Fatalf("CheckFinal() with passed tests error = %v", err)
	}
	if !final.Done || !deepAgentVerificationHasCheck(final.Checks, "test_verifier", true) {
		t.Fatalf("code fix with passed tests should pass test verifier, got %#v", final)
	}
}

func TestDeepAgentControllerStoresEvidenceFirstStepEvidence(t *testing.T) {
	store := NewMemoryWorkflowStore()
	controller := NewDeepAgentController(
		store,
		NoopWorkflowEventSink{},
		staticDeepAgentPlanner{plan: DeepAgentPlan{Goal: "research empty output", Steps: []DeepAgentStep{{
			ID:            "research",
			Title:         "Research with evidence",
			Intent:        "Collect sourced notes",
			DoneCondition: "source evidence exists",
		}}}},
		evidenceOnlyDeepAgentExecutor{},
		nil,
	)
	result, err := controller.Execute(context.Background(), DeepAgentTaskRequest{
		UserID:    "alice",
		SessionID: "session-1",
		Goal:      "research empty output",
		Policy:    DeepAgentPolicy{MaxSteps: 2, MaxActions: 2, NoProgressLimit: 2, MaxDuration: time.Minute},
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	evidence := (StateDeepAgentEvidenceStore{}).ListStepEvidence(result.State)
	if len(evidence) != 1 || evidence[0].StepID != "research" || len(evidence[0].Sources) != 1 {
		t.Fatalf("expected evidence store item with source, got %#v", evidence)
	}
	if got := deepAgentWorkflowString(result.State.WorkingMemory["step_context"].(map[string]any)["research"].(map[string]any), "summary"); got == "" {
		t.Fatalf("step context should summarize evidence even with empty action output: %#v", result.State.WorkingMemory)
	}
	summary, ok := DeepAgentSummaryFromWorkflowRun(result.Run)
	if !ok || summary == nil || len(summary.Evidence) != 1 || summary.Evidence[0].StepID != "research" {
		t.Fatalf("admin summary should expose evidence store, got %#v ok=%v", summary, ok)
	}
}

func TestDeepAgentEvidenceRepositoryPersistsRunEvidence(t *testing.T) {
	runtime := testRuntime(t)
	repo := NewMemoryDeepAgentEvidenceRepository()
	runtime.SetDeepAgentEvidenceRepository(repo)
	session, err := runtime.CreateSession(context.Background(), "alice", "")
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	result, err := runtime.ExecuteDeepAgentTask(context.Background(), DeepAgentTaskRequest{
		UserID:    "alice",
		SessionID: session.ID,
		Goal:      "research persistent evidence",
		State: map[string]any{
			"template_id":  DeepAgentTemplateWebMonitor,
			"task_type":    DeepAgentTemplateWebMonitor,
			"deliverable":  "answer",
			"trigger_type": "schedule",
		},
	}, staticDeepAgentPlanner{plan: DeepAgentPlan{Steps: []DeepAgentStep{{ID: "observe", Title: "Observe", DoneCondition: "done"}}}}, evidenceOnlyDeepAgentExecutor{}, nil)
	if err != nil {
		t.Fatalf("ExecuteDeepAgentTask() error = %v", err)
	}
	records, err := repo.ListDeepAgentEvidence(context.Background(), DeepAgentEvidenceFilter{
		UserID:     "alice",
		RunID:      result.Run.ID,
		TemplateID: DeepAgentTemplateWebMonitor,
	})
	if err != nil {
		t.Fatalf("ListDeepAgentEvidence() error = %v", err)
	}
	var observed bool
	for _, record := range records {
		if record.StepID == "observe" && record.SourceCount == 1 && record.TriggerType == "schedule" {
			observed = true
			break
		}
	}
	if !observed {
		t.Fatalf("unexpected persisted evidence records: %#v", records)
	}
}

func TestRuleDeepAgentVerifierReadsEvidenceStoreForFinalSources(t *testing.T) {
	state := &DeepAgentState{
		Goal: "research AgentAPI",
		Plan: DeepAgentPlan{Goal: "research AgentAPI", Steps: []DeepAgentStep{{
			ID:            "research",
			Title:         "Research AgentAPI",
			DoneCondition: "source evidence exists",
			Status:        DeepAgentStepStatusSucceeded,
		}}},
		CompletedSteps: []string{"research"},
		WorkingMemory: map[string]any{
			"task_type":   "research",
			"deliverable": "answer",
		},
	}
	(StateDeepAgentEvidenceStore{}).PutStepEvidence(state, DeepAgentStepEvidence{
		StepID:  "research",
		Summary: "AgentAPI sourced summary",
		Sources: []DeepAgentSourceRef{{URL: "https://example.com/agentapi", Title: "AgentAPI source"}},
	})
	final, err := ruleDeepAgentVerifier{}.CheckFinal(context.Background(), state)
	if err != nil {
		t.Fatalf("CheckFinal() error = %v", err)
	}
	if !final.Done || !deepAgentVerificationHasCheck(final.Checks, "source_verifier", true) {
		t.Fatalf("final verifier should read evidence store sources, got %#v", final)
	}
}

func TestRuleDeepAgentVerifierRejectsLongOutputWithoutResearchEvidence(t *testing.T) {
	step := DeepAgentStep{
		ID:            "research",
		Title:         "Research current product",
		Intent:        "Use web research",
		DoneCondition: "source evidence exists",
	}
	route := DeepAgentStepRoute{
		StepID:       "research",
		Mode:         DeepAgentToolModeModel,
		Executor:     deepAgentRouteExecutorModel,
		SearchScope:  "web",
		AllowedTools: []string{"WebSearch", "WebFetch"},
	}
	longOutput := strings.Repeat("This is a long unsourced summary. ", 80)
	progress, err := ruleDeepAgentVerifier{}.CheckProgress(context.Background(), nil, step, DeepAgentAction{}, DeepAgentActionResult{
		Status:    DeepAgentActionStatusSucceeded,
		Output:    longOutput,
		Completed: true,
		Metadata: map[string]any{
			"step_evidence": DeepAgentStepEvidence{
				StepID:  "research",
				Route:   route,
				Output:  longOutput,
				Summary: longOutput,
			},
		},
	})
	if err != nil {
		t.Fatalf("CheckProgress() error = %v", err)
	}
	if progress.StepDone || progress.MadeProgress || !strings.Contains(progress.Reason, "source evidence") {
		t.Fatalf("long output without evidence should fail research verifier, got %#v", progress)
	}
}

func deepAgentVerificationHasCheck(checks []DeepAgentVerificationCheck, name string, passed bool) bool {
	for _, check := range checks {
		if check.Name == name && check.Passed == passed {
			return true
		}
	}
	return false
}

func TestDeepAgentSummaryIncludesRoutesEvidenceAndFinalVerifier(t *testing.T) {
	route := DeepAgentStepRoute{StepID: "write-report", Version: "v2", Mode: DeepAgentToolModeModelArtifact, Executor: deepAgentRouteExecutorArtifact, RequiresArtifact: true, DeliverableType: deepAgentDeliverableMarkdown}
	evidence := DeepAgentStepEvidence{
		StepID:  "write-report",
		Route:   route,
		Summary: "created report",
		Artifacts: []DeepAgentArtifactRef{{
			ID:          "artifact-1",
			StepID:      "write-report",
			Filename:    "tolan-report.md",
			ContentType: "text/markdown",
			SizeBytes:   256,
		}},
	}
	state := &DeepAgentState{
		Goal: "生成调研报告",
		Plan: DeepAgentPlan{Steps: []DeepAgentStep{{
			ID:     "write-report",
			Title:  "写报告",
			Status: DeepAgentStepStatusSucceeded,
		}}},
		CompletedSteps: []string{"write-report"},
		ActionHistory: []DeepAgentAction{{
			StepID: "write-report",
			Tool:   DeepAgentToolModeModelArtifact,
			Args:   map[string]any{"step_route": route},
			Hash:   "hash-1",
		}},
		WorkingMemory: map[string]any{
			"final_verification": map[string]any{"done": true, "reason": "verified"},
			"step_context": map[string]any{
				"write-report": map[string]any{
					"artifact_refs": evidence.Artifacts,
					"metadata":      map[string]any{"step_evidence": evidence},
				},
			},
		},
	}
	summary, ok := DeepAgentSummaryFromWorkflowRun(&WorkflowRun{
		Name: deepAgentTaskWorkflowName,
		State: map[string]any{
			"deep_agent_state": state,
		},
	})
	if !ok || summary == nil || !summary.Present {
		t.Fatalf("expected deep agent summary, got %#v ok=%v", summary, ok)
	}
	if len(summary.Routes) != 1 || summary.Routes[0]["version"] != "v2" {
		t.Fatalf("missing route summary: %#v", summary)
	}
	if len(summary.Evidence) != 1 || summary.Evidence[0].StepID != "write-report" {
		t.Fatalf("missing evidence summary: %#v", summary)
	}
	if len(summary.ArtifactRefs) != 1 || summary.ArtifactRefs[0].ID != "artifact-1" {
		t.Fatalf("missing artifact refs: %#v", summary)
	}
	if summary.FinalVerifier["reason"] != "verified" {
		t.Fatalf("missing final verifier: %#v", summary.FinalVerifier)
	}
}

func TestDeepAgentClassifiesEmptyResponseAsTransientRetryable(t *testing.T) {
	runner := &countingErrorRunner{err: fmt.Errorf("queryengine empty response: no assistant text or tool calls")}
	runtime := NewRuntime(
		RuntimeConfig{},
		NewFileSessionStore(t.TempDir()),
		nil,
		nil,
		func(Scope) Runner { return runner },
	)
	session, err := runtime.CreateSession(context.Background(), "alice", "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	result, err := NewRuntimeDeepAgentExecutor(runtime).ExecuteDeepAgentAction(context.Background(), DeepAgentAction{
		StepID: "step-1",
		Tool:   DeepAgentToolModeModel,
		Args: map[string]any{
			"user_id":    "alice",
			"session_id": session.ID,
			"prompt":     "调研 Tolan AI",
		},
	}, &DeepAgentState{WorkingMemory: map[string]any{"user_id": "alice", "session_id": session.ID}})
	if err == nil {
		t.Fatalf("ExecuteDeepAgentAction() expected error")
	}
	if !result.Retryable {
		t.Fatalf("empty response should stay retryable: %#v", result)
	}
	if got := deepAgentWorkflowString(result.Metadata, "error_class"); got != DeepAgentErrorTransient {
		t.Fatalf("error_class = %q, want transient in %#v", got, result.Metadata)
	}
	if runner.calls != 1 {
		t.Fatalf("runner calls = %d, want 1", runner.calls)
	}
}

func TestClassifyDeepAgentErrorCategories(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		result    DeepAgentActionResult
		wantClass string
		retryable bool
	}{
		{
			name:      "permission",
			err:       fmt.Errorf("permission denied connecting to /var/run/docker.sock"),
			wantClass: DeepAgentErrorPermission,
		},
		{
			name:      "config",
			err:       fmt.Errorf("skill not found: docx"),
			wantClass: DeepAgentErrorConfig,
		},
		{
			name:      "transient",
			err:       fmt.Errorf("rate limit 429: try again"),
			wantClass: DeepAgentErrorTransient,
			retryable: true,
		},
		{
			name:      "empty response",
			err:       fmt.Errorf("queryengine empty response: no assistant text or tool calls"),
			wantClass: DeepAgentErrorTransient,
			retryable: true,
		},
		{
			name:      "validation",
			err:       fmt.Errorf("artifact count 0 below required 1"),
			wantClass: DeepAgentErrorValidation,
		},
		{
			name:      "provider",
			result:    DeepAgentActionResult{Error: "upstream provider model overloaded"},
			wantClass: DeepAgentErrorProvider,
			retryable: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyDeepAgentError(tt.err, tt.result)
			if got != tt.wantClass {
				t.Fatalf("classifyDeepAgentError() = %q, want %q", got, tt.wantClass)
			}
			if deepAgentErrorRetryable(got) != tt.retryable {
				t.Fatalf("retryable(%q) = %v, want %v", got, deepAgentErrorRetryable(got), tt.retryable)
			}
		})
	}
}

func TestDeepAgentControllerResumeClearsFailedStepTriedActions(t *testing.T) {
	failedAction := DeepAgentAction{StepID: "failed", Tool: DeepAgentToolModeModel, Args: map[string]any{"prompt": "retry me"}}
	failedAction.Hash = deepAgentActionHash(failedAction)
	doneAction := DeepAgentAction{StepID: "done", Tool: DeepAgentToolModeModel, Args: map[string]any{"prompt": "already done"}}
	doneAction.Hash = deepAgentActionHash(doneAction)
	state := &DeepAgentState{
		Plan: DeepAgentPlan{Steps: []DeepAgentStep{
			{ID: "done", Status: DeepAgentStepStatusSucceeded},
			{ID: "failed", Status: DeepAgentStepStatusFailed},
		}},
		CompletedSteps: []string{"done"},
		TriedActions: map[string]int{
			doneAction.Hash:   1,
			failedAction.Hash: 1,
		},
		ActionHistory: []DeepAgentAction{doneAction, failedAction},
		WorkingMemory: map[string]any{
			"step_context": map[string]any{
				"done": map[string]any{"summary": "kept evidence"},
			},
		},
	}
	controller := NewDeepAgentController(NewMemoryWorkflowStore(), NoopWorkflowEventSink{}, countingDeepAgentPlanner{}, completingDeepAgentExecutor{}, nil)
	controller.prepareStateForResume(DeepAgentResumeRequest{StatePatch: map[string]any{"extra_context": "fresh hint"}}, state)
	if _, exists := state.TriedActions[failedAction.Hash]; exists {
		t.Fatalf("failed step hash should be cleared: %#v", state.TriedActions)
	}
	if state.TriedActions[doneAction.Hash] != 1 {
		t.Fatalf("completed step hash should be preserved: %#v", state.TriedActions)
	}
	if state.Plan.Steps[1].Status != DeepAgentStepStatusPending {
		t.Fatalf("failed step status should reset to pending: %#v", state.Plan.Steps[1])
	}
	if _, ok := state.WorkingMemory["step_context"].(map[string]any)["done"]; !ok {
		t.Fatalf("completed step evidence should be preserved: %#v", state.WorkingMemory)
	}
	if got := deepAgentWorkflowString(state.WorkingMemory, "extra_context"); got != "fresh hint" {
		t.Fatalf("state patch not applied: %#v", state.WorkingMemory)
	}
}

func TestDeepAgentControllerResumeCompressesLongActionHistory(t *testing.T) {
	state := &DeepAgentState{
		Plan:           DeepAgentPlan{Steps: []DeepAgentStep{{ID: "step", Status: DeepAgentStepStatusSucceeded}}},
		CompletedSteps: []string{"step"},
		TriedActions:   map[string]int{},
		WorkingMemory:  map[string]any{"resume_count": 4},
	}
	for i := 0; i < 36; i++ {
		hash := fmt.Sprintf("hash-%02d", i)
		state.ActionHistory = append(state.ActionHistory, DeepAgentAction{
			StepID: "step",
			Tool:   DeepAgentToolModeModel,
			Hash:   hash,
		})
		state.TriedActions[hash] = 1
	}
	controller := NewDeepAgentController(NewMemoryWorkflowStore(), NoopWorkflowEventSink{}, nil, nil, nil)
	controller.prepareStateForResume(DeepAgentResumeRequest{}, state)
	if got := deepAgentAnyInt(state.WorkingMemory["resume_count"], 0); got != 5 {
		t.Fatalf("resume_count = %d, want 5", got)
	}
	if len(state.ActionHistory) > 16 {
		t.Fatalf("action history was not compressed: len=%d", len(state.ActionHistory))
	}
	if summary := deepAgentWorkflowString(state.WorkingMemory, "action_history_summary"); !strings.Contains(summary, "Compressed 36 DeepAgent actions") {
		t.Fatalf("missing action history summary: %#v", state.WorkingMemory)
	}
	if len(state.TriedActions) != 36 {
		t.Fatalf("tried actions for completed steps should be preserved, got %d", len(state.TriedActions))
	}
}

func TestDeepAgentControllerResumeAdditionalBudgetPreservesAudit(t *testing.T) {
	started := time.Date(2026, 6, 18, 10, 0, 0, 0, time.UTC)
	now := started.Add(10 * time.Minute)
	state := &DeepAgentState{
		Plan:          DeepAgentPlan{Steps: []DeepAgentStep{{ID: "retry", Status: DeepAgentStepStatusFailed}}},
		FailedSteps:   []string{"retry"},
		TriedActions:  map[string]int{"failed-hash": 1},
		ActionHistory: []DeepAgentAction{{StepID: "retry", Tool: DeepAgentToolModeModel, Hash: "failed-hash"}},
		ActionCount:   7,
		Status:        DeepAgentRunStatusBudgetExceeded,
		Blocker:       "max action count exceeded",
		StartedAt:     started,
		WorkingMemory: map[string]any{},
	}
	controller := NewDeepAgentController(NewMemoryWorkflowStore(), NoopWorkflowEventSink{}, nil, nil, nil)
	controller.clock = fixedClock{at: now}
	controller.prepareStateForResume(DeepAgentResumeRequest{
		AdditionalBudget: DeepAgentResumeBudget{MaxActions: 3, MaxDurationMS: int64((5 * time.Minute).Milliseconds())},
	}, state)
	policy := controller.resumePolicyForState(DeepAgentResumeRequest{
		AdditionalBudget: DeepAgentResumeBudget{MaxActions: 3, MaxDurationMS: int64((5 * time.Minute).Milliseconds())},
	}, state)
	if !state.StartedAt.Equal(started) {
		t.Fatalf("resume should preserve started_at, got %s want %s", state.StartedAt, started)
	}
	if state.ActionCount != 7 {
		t.Fatalf("resume should preserve action count, got %d", state.ActionCount)
	}
	if policy.MaxActions != 10 {
		t.Fatalf("max actions should include previous audit count, got %d", policy.MaxActions)
	}
	if policy.MaxDuration != 15*time.Minute {
		t.Fatalf("max duration should include elapsed plus additional budget, got %s", policy.MaxDuration)
	}
	if state.Plan.Steps[0].Status != DeepAgentStepStatusPending || len(state.FailedSteps) != 1 {
		t.Fatalf("resume should reset executable status without dropping audit lists: %#v", state)
	}
}

func TestDeepAgentRecoverySummaryShowsBlockedResumeHints(t *testing.T) {
	state := &DeepAgentState{
		Goal:    "finish report",
		Status:  DeepAgentRunStatusBlocked,
		Blocker: "missing source evidence",
		Plan:    DeepAgentPlan{Steps: []DeepAgentStep{{ID: "research", Title: "Research", Status: DeepAgentStepStatusFailed}}},
		ActionHistory: []DeepAgentAction{{
			StepID: "research",
			Tool:   DeepAgentToolModeWeb,
			Hash:   "hash-research",
		}},
		ActionCount: 1,
		WorkingMemory: map[string]any{
			"final_verification": map[string]any{"missing": []string{"source URL"}},
		},
	}
	run := &WorkflowRun{
		ID:      "run-recovery",
		Name:    deepAgentTaskWorkflowName,
		Version: deepAgentTaskWorkflowVersion,
		State:   map[string]any{"deep_agent_state": state},
	}
	summary, ok := DeepAgentSummaryFromWorkflowRun(run)
	if !ok || summary == nil || !summary.Present {
		t.Fatalf("expected deep agent summary, got %#v ok=%t", summary, ok)
	}
	if !summary.Recovery.ResumeAvailable || summary.Recovery.BlockedReason != "missing source evidence" {
		t.Fatalf("unexpected recovery state: %#v", summary.Recovery)
	}
	if len(summary.Recovery.MissingInfo) != 1 || summary.Recovery.MissingInfo[0] != "source URL" {
		t.Fatalf("missing info not surfaced: %#v", summary.Recovery)
	}
	if summary.Recovery.LastAction == nil || summary.Recovery.LastAction.Hash != "hash-research" {
		t.Fatalf("last action not surfaced: %#v", summary.Recovery.LastAction)
	}
}

func TestDeepAgentReviewDecisionApproveSkipsPendingRisk(t *testing.T) {
	controller := NewDeepAgentController(NewMemoryWorkflowStore(), NoopWorkflowEventSink{}, nil, nil, nil)
	controller.SetRiskGate(NewRuntimeDeepAgentRiskGate(nil))
	state := &DeepAgentState{WorkingMemory: map[string]any{}}
	step := DeepAgentStep{ID: "danger", Title: "Dangerous step", RiskLevel: RiskLevelHigh}
	action := DeepAgentAction{StepID: step.ID, Tool: DeepAgentToolModeCodePatch, Args: map[string]any{"path": "prod"}}
	action.Hash = deepAgentActionHash(action)
	err := controller.reviewActionRisk(context.Background(), &WorkflowRun{ID: "run-review"}, state, step, action)
	if !errors.Is(err, ErrDeepAgentReviewRequired) {
		t.Fatalf("expected review required, got %v", err)
	}
	recordDeepAgentPendingReview(state, step, action, err.Error(), time.Now())
	applyDeepAgentReviewDecision(state, DeepAgentReviewDecision{Action: "approve", StepID: step.ID, ActionHash: action.Hash})
	if err := controller.reviewActionRisk(context.Background(), &WorkflowRun{ID: "run-review"}, state, step, action); err != nil {
		t.Fatalf("approved review action should skip risk gate, got %v", err)
	}
	if _, ok := state.WorkingMemory["pending_review_action"]; ok {
		t.Fatalf("pending review should be cleared: %#v", state.WorkingMemory)
	}
}

func TestDeepAgentReviewDecisionEditOverridesActionArgs(t *testing.T) {
	state := &DeepAgentState{WorkingMemory: map[string]any{}}
	step := DeepAgentStep{ID: "edit-step", Title: "Edit step"}
	action := DeepAgentAction{StepID: step.ID, Tool: DeepAgentToolModeModel, Args: map[string]any{"prompt": "old"}}
	action.Hash = deepAgentActionHash(action)
	recordDeepAgentPendingReview(state, step, action, "needs edit", time.Now())
	applyDeepAgentReviewDecision(state, DeepAgentReviewDecision{
		Action:     "edit",
		StepID:     step.ID,
		ActionHash: action.Hash,
		ArgsPatch:  map[string]any{"prompt": "new", "reviewed": true},
	})
	updated := applyDeepAgentActionOverride(state, action)
	if got := deepAgentWorkflowString(updated.Args, "prompt"); got != "new" {
		t.Fatalf("override prompt = %q", got)
	}
	if !deepAgentBool(updated.Args, "reviewed", false) {
		t.Fatalf("override flag missing: %#v", updated.Args)
	}
	if updated.Hash != "" {
		t.Fatalf("override should clear hash so edited args are re-hashed, got %q", updated.Hash)
	}
}

func TestDeepAgentObservabilityMetricsTimelineAndReplay(t *testing.T) {
	store := NewMemoryWorkflowStore()
	controller := NewDeepAgentController(
		store,
		NoopWorkflowEventSink{},
		staticDeepAgentPlanner{plan: DeepAgentPlan{Steps: []DeepAgentStep{{ID: "research", Title: "Research", DoneCondition: "done"}}}},
		researchReportCompletingDeepAgentExecutor{},
		nil,
	)
	result, err := controller.Execute(context.Background(), DeepAgentTaskRequest{
		UserID: "alice",
		Goal:   "research observability",
		State: map[string]any{
			"task_type":       "research_report",
			"trigger_type":    "schedule",
			"trigger_source":  "cron",
			"trigger_payload": map[string]any{"cron": "0 * * * *"},
		},
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
		t.Fatalf("expected deep agent summary, got %#v", summary)
	}
	if summary.Metrics.TaskType != "research_report" || summary.Metrics.TriggerType != "schedule" || summary.Metrics.ActionCount != 1 {
		t.Fatalf("unexpected metrics: %#v", summary.Metrics)
	}
	if len(summary.Timeline) == 0 {
		t.Fatalf("expected timeline in summary")
	}
	runtime := testRuntime(t)
	runtime.SetWorkflowStore(store)
	replay, err := runtime.ReplayDeepAgentRun(context.Background(), loaded.ID)
	if err != nil {
		t.Fatalf("ReplayDeepAgentRun() error = %v", err)
	}
	if replay.TaskType != "research_report" || len(replay.PlannerDecisions) == 0 || len(replay.ExecutorDecisions) == 0 {
		t.Fatalf("unexpected replay: %#v", replay)
	}
}

func TestEvaluationEngineSupportsDeepAgentSubject(t *testing.T) {
	store := NewMemoryWorkflowStore()
	controller := NewDeepAgentController(
		store,
		NoopWorkflowEventSink{},
		staticDeepAgentPlanner{plan: DeepAgentPlan{Steps: []DeepAgentStep{{ID: "summarize", Title: "Summarize", DoneCondition: "done"}}}},
		completingDeepAgentExecutor{},
		nil,
	)
	_, err := controller.Execute(context.Background(), DeepAgentTaskRequest{
		UserID: "alice",
		Goal:   "summarize note",
		State:  map[string]any{"task_type": "general", "trigger_type": "manual"},
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	runtime := testRuntime(t)
	runtime.SetWorkflowStore(store)
	engine := NewEvaluationEngine(RuntimeEvaluationTraceSource{Runtime: runtime})
	report, err := engine.Evaluate(context.Background(), EvaluationRunRequest{
		Scope: EvaluationScope{SubjectType: EvaluationSubjectDeepAgent, UserID: "alice", TaskType: "general"},
	})
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if report.Run.Total != 1 || len(report.Results) != 1 {
		t.Fatalf("unexpected report: %#v", report)
	}
	if got := mapInt(report.Results[0].Metrics, "deep_agent_action_count"); got != 1 {
		t.Fatalf("deep agent action count metric = %d", got)
	}
	if report.Results[0].SubjectType != EvaluationSubjectDeepAgent {
		t.Fatalf("subject type = %q", report.Results[0].SubjectType)
	}
}

func TestDeepAgentTaskTemplatesContainRubricBudgetAndExecutorHints(t *testing.T) {
	templates := DefaultDeepAgentTaskTemplates()
	if len(templates) != 6 {
		t.Fatalf("template count = %d, want 6", len(templates))
	}
	for _, tmpl := range templates {
		if tmpl.ID == "" || tmpl.TaskType == "" || len(tmpl.Steps) == 0 {
			t.Fatalf("template missing identity or steps: %#v", tmpl)
		}
		if len(tmpl.Rubric.AcceptanceCriteria) == 0 || strings.TrimSpace(tmpl.Rubric.QualityBar) == "" {
			t.Fatalf("template %s missing rubric: %#v", tmpl.ID, tmpl.Rubric)
		}
		if tmpl.Budget.MaxSteps <= 0 || tmpl.Budget.MaxActions <= 0 || tmpl.Budget.MaxDuration <= 0 {
			t.Fatalf("template %s missing budget: %#v", tmpl.ID, tmpl.Budget)
		}
		if len(tmpl.ExecutorHints) == 0 {
			t.Fatalf("template %s missing executor hints", tmpl.ID)
		}
	}
}

func TestDeepAgentTaskTemplateAppliesPlanRubricAndBudget(t *testing.T) {
	req := applyDeepAgentTaskTemplateToTaskRequest(DeepAgentTaskRequest{
		Goal:   "修复 CI 失败",
		State:  map[string]any{"template_id": DeepAgentTemplateCIFailureFix},
		Policy: DeepAgentPolicy{MaxActions: 3},
		Rubric: DeepAgentRubric{RequiredEvidence: []string{"local rerun output"}},
	})
	if got := deepAgentWorkflowString(req.State, "template_id"); got != DeepAgentTemplateCIFailureFix {
		t.Fatalf("template_id = %q", got)
	}
	if len(req.Plan.Steps) != 3 || req.Plan.Steps[0].ID != "logs" {
		t.Fatalf("unexpected template plan: %#v", req.Plan)
	}
	if req.Policy.MaxActions != 3 || req.Policy.MaxSteps != 4 {
		t.Fatalf("template budget merge = %#v", req.Policy)
	}
	if !containsString(req.Rubric.RequiredEvidence, "CI log excerpt") || !containsString(req.Rubric.RequiredEvidence, "local rerun output") {
		t.Fatalf("rubric was not merged: %#v", req.Rubric)
	}
	action, err := ruleDeepAgentPlanner{}.NextAction(context.Background(), &DeepAgentState{Goal: req.Goal, WorkingMemory: req.State}, req.Plan.Steps[0])
	if err != nil {
		t.Fatalf("NextAction() error = %v", err)
	}
	if action.Tool != DeepAgentToolModeRAGSearch || deepAgentWorkflowString(action.Args, "template_id") != DeepAgentTemplateCIFailureFix {
		t.Fatalf("unexpected template action: %#v", action)
	}
}

func TestResearchReportTemplateGatherUsesWebResearchModelRoute(t *testing.T) {
	goal := "帮我调研一下chance ai这款ai产品"
	req := applyDeepAgentTaskTemplateToTaskRequest(DeepAgentTaskRequest{
		Goal:  goal,
		State: map[string]any{"template_id": DeepAgentTemplateResearchReport},
	})
	if len(req.Plan.Steps) == 0 || req.Plan.Steps[0].ID != "gather" {
		t.Fatalf("unexpected research template plan: %#v", req.Plan)
	}
	action, err := ruleDeepAgentPlanner{}.NextAction(context.Background(), &DeepAgentState{Goal: req.Goal, WorkingMemory: req.State}, req.Plan.Steps[0])
	if err != nil {
		t.Fatalf("NextAction() error = %v", err)
	}
	if action.Tool != DeepAgentToolModeModel {
		t.Fatalf("research gather tool = %q, want %q; action=%#v", action.Tool, DeepAgentToolModeModel, action)
	}
	if got := deepAgentWorkflowString(action.Args, "query"); got != goal {
		t.Fatalf("query = %q, want %q; action=%#v", got, goal, action)
	}
	if got := deepAgentWorkflowString(action.Args, "search_scope"); got != "web" {
		t.Fatalf("search_scope = %q, want web; action=%#v", got, action)
	}
	wantTools := []string{"WebSearch", "WebFetch"}
	if got := deepAgentStringSlice(action.Args["allowed_tools"]); strings.Join(got, ",") != strings.Join(wantTools, ",") {
		t.Fatalf("allowed_tools = %#v, want %#v; action=%#v", got, wantTools, action)
	}
	route, ok := deepAgentStepRouteFromMap(action.Args)
	if !ok {
		t.Fatalf("missing step_route in action args: %#v", action.Args)
	}
	if route.Executor != deepAgentRouteExecutorModel || route.Mode != DeepAgentToolModeModel || route.SearchScope != "web" {
		t.Fatalf("unexpected route: %#v", route)
	}
	if strings.Join(route.AllowedTools, ",") != strings.Join(wantTools, ",") {
		t.Fatalf("route allowed tools = %#v, want %#v", route.AllowedTools, wantTools)
	}
}

func TestDeepAgentRouterDoesNotTreatBrowserlessResearchAsWebExecutor(t *testing.T) {
	step := DeepAgentStep{
		ID:            "step-1",
		Title:         "搜集 Browserless 的核心信息",
		Intent:        "通过网络搜索，查找并整理关于 Browserless 公司/团队、产品功能和定价/可用性的基本信息。",
		DoneCondition: "已收集到关于 Browserless 公司、产品功能和定价的关键信息，并记录了信息来源。",
	}
	route, ok := (&RuntimeDeepAgentStepRouter{}).deterministicRoute(step)
	if !ok {
		t.Fatal("expected deterministic research route")
	}
	if route.Mode != DeepAgentToolModeModel || route.Executor != deepAgentRouteExecutorModel || route.SearchScope != "web" {
		t.Fatalf("route = %#v, want web research model route", route)
	}
	wantTools := []string{"WebSearch", "WebFetch"}
	if strings.Join(route.AllowedTools, ",") != strings.Join(wantTools, ",") {
		t.Fatalf("route allowed tools = %#v, want %#v", route.AllowedTools, wantTools)
	}
}

func TestDeepAgentRouterDoesNotTreatBrowserlessBrowserAutomationResearchAsWebExecutor(t *testing.T) {
	step := DeepAgentStep{
		ID:            "step-1",
		Title:         "搜集 Browserless 浏览器自动化产品信息",
		Intent:        "通过网络搜索调研 Browserless 浏览器自动化平台，整理公司、产品功能、定价和风险。",
		DoneCondition: "已收集到关于 Browserless 的关键信息，并记录了信息来源。",
	}
	route, ok := (&RuntimeDeepAgentStepRouter{}).deterministicRoute(step)
	if !ok {
		t.Fatal("expected deterministic web research route")
	}
	if route.Mode != DeepAgentToolModeModel || route.Executor != deepAgentRouteExecutorModel || route.SearchScope != "web" {
		t.Fatalf("route = %#v, want web research model route", route)
	}
	wantTools := []string{"WebSearch", "WebFetch"}
	if strings.Join(route.AllowedTools, ",") != strings.Join(wantTools, ",") {
		t.Fatalf("route allowed tools = %#v, want %#v", route.AllowedTools, wantTools)
	}
}

func TestDeepAgentRouterStillRoutesBrowserVerificationToWebExecutor(t *testing.T) {
	step := DeepAgentStep{
		ID:            "verify-page",
		Title:         "Open the browser and take a screenshot",
		Intent:        "Use browser verification for https://example.com and capture DOM evidence.",
		DoneCondition: "screenshot and DOM evidence captured",
	}
	route, ok := (&RuntimeDeepAgentStepRouter{}).deterministicRoute(step)
	if !ok {
		t.Fatal("expected deterministic web route")
	}
	if route.Mode != DeepAgentToolModeWeb || route.Executor != deepAgentRouteExecutorWeb {
		t.Fatalf("route = %#v, want dedicated web route", route)
	}
}

func TestRuntimeDeepAgentPlannerFallsBackFromWebExecutorToWebToolsWithoutURL(t *testing.T) {
	runtime := testRuntime(t)
	planner := NewRuntimeDeepAgentPlanner(runtime)
	state := &DeepAgentState{
		Goal:          "调研 Browserless 浏览器自动化平台",
		WorkingMemory: map[string]any{"user_id": "alice", "session_id": "session-1"},
	}
	step := DeepAgentStep{
		ID:            "step-1",
		Title:         "搜集 Browserless 浏览器自动化产品信息",
		Intent:        "通过网络搜索调研 Browserless 浏览器自动化平台，整理公司、产品功能、定价和风险。",
		DoneCondition: "已收集到可追溯来源和关键事实。",
	}
	route := DeepAgentStepRoute{
		StepID:      step.ID,
		Mode:        DeepAgentToolModeWeb,
		Executor:    deepAgentRouteExecutorWeb,
		SearchScope: "web",
		Reason:      "test web route without URL",
		Confidence:  "high",
	}
	action, err := planner.actionForRoute(state, step, route)
	if err != nil {
		t.Fatalf("actionForRoute() error = %v", err)
	}
	if action.Tool != DeepAgentToolModeModel {
		t.Fatalf("action tool = %q, want model: %#v", action.Tool, action)
	}
	wantTools := []string{"WebSearch", "WebFetch"}
	if got := strings.Join(deepAgentStringSlice(action.Args["allowed_tools"]), ","); got != strings.Join(wantTools, ",") {
		t.Fatalf("allowed tools = %q, want %#v in %#v", got, wantTools, action.Args)
	}
	actionRoute, ok := deepAgentStepRouteFromMap(action.Args)
	if !ok {
		t.Fatalf("missing action route in args: %#v", action.Args)
	}
	if actionRoute.Mode != DeepAgentToolModeModel || actionRoute.Executor != deepAgentRouteExecutorModel || actionRoute.SearchScope != "web" {
		t.Fatalf("action route = %#v, want model web-search route", actionRoute)
	}
}

func TestRuntimeDeepAgentPlannerKeepsWebExecutorForExplicitURL(t *testing.T) {
	runtime := testRuntime(t)
	planner := NewRuntimeDeepAgentPlanner(runtime)
	state := &DeepAgentState{Goal: "验证页面", WorkingMemory: map[string]any{"user_id": "alice", "session_id": "session-1"}}
	step := DeepAgentStep{
		ID:            "verify-page",
		Title:         "打开页面并截图",
		Intent:        "Use browser verification for https://example.com/docs and capture DOM evidence.",
		DoneCondition: "screenshot and DOM evidence captured",
	}
	route := DeepAgentStepRoute{
		StepID:      step.ID,
		Mode:        DeepAgentToolModeWeb,
		Executor:    deepAgentRouteExecutorWeb,
		SearchScope: "web",
		Reason:      "test web route with URL",
		Confidence:  "high",
	}
	action, err := planner.actionForRoute(state, step, route)
	if err != nil {
		t.Fatalf("actionForRoute() error = %v", err)
	}
	if action.Tool != DeepAgentToolModeWeb {
		t.Fatalf("action tool = %q, want web: %#v", action.Tool, action)
	}
	if got := deepAgentWorkflowString(action.Args, "url"); got != "https://example.com/docs" {
		t.Fatalf("action url = %q, want explicit URL in %#v", got, action.Args)
	}
	actionRoute, ok := deepAgentStepRouteFromMap(action.Args)
	if !ok {
		t.Fatalf("missing action route in args: %#v", action.Args)
	}
	if actionRoute.Mode != DeepAgentToolModeWeb || actionRoute.Executor != deepAgentRouteExecutorWeb {
		t.Fatalf("action route = %#v, want dedicated web route", actionRoute)
	}
}

func TestDeepAgentEvaluationFiltersAndAggregatesByTemplate(t *testing.T) {
	store := NewMemoryWorkflowStore()
	now := time.Now().UTC()
	for _, item := range []struct {
		id         string
		templateID string
		taskType   string
	}{
		{id: "run-code", templateID: DeepAgentTemplateCodeFix, taskType: DeepAgentTemplateCodeFix},
		{id: "run-doc", templateID: DeepAgentTemplateDocGeneration, taskType: DeepAgentTemplateDocGeneration},
	} {
		state := &DeepAgentState{
			Goal:          item.id,
			Status:        DeepAgentRunStatusSucceeded,
			StartedAt:     now,
			UpdatedAt:     now,
			WorkingMemory: map[string]any{"template_id": item.templateID, "task_type": item.taskType},
		}
		if err := store.CreateWorkflowRun(context.Background(), &WorkflowRun{
			ID:        item.id,
			UserID:    "alice",
			Name:      deepAgentTaskWorkflowName,
			Version:   "v1",
			Status:    WorkflowStatusSucceeded,
			State:     map[string]any{"deep_agent_state": state, "deep_agent_status": state.Status},
			CreatedAt: now,
			UpdatedAt: now,
			StartedAt: &now,
		}); err != nil {
			t.Fatalf("CreateWorkflowRun() error = %v", err)
		}
	}
	runtime := testRuntime(t)
	runtime.SetWorkflowStore(store)
	engine := NewEvaluationEngine(RuntimeEvaluationTraceSource{Runtime: runtime})
	report, err := engine.Evaluate(context.Background(), EvaluationRunRequest{
		Scope: EvaluationScope{SubjectType: EvaluationSubjectDeepAgent, UserID: "alice", TemplateID: DeepAgentTemplateCodeFix},
	})
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if report.Run.Total != 1 || report.Results[0].SubjectID != "run-code" {
		t.Fatalf("unexpected template-filtered report: %#v", report)
	}
	if got := mapStringInt(report.Run.Metrics, "deep_agent_by_template")[DeepAgentTemplateCodeFix]; got != 1 {
		t.Fatalf("deep_agent_by_template metric = %d in %#v", got, report.Run.Metrics)
	}
	if got := deepAgentWorkflowString(report.Results[0].Metrics, "deep_agent_template_id"); got != DeepAgentTemplateCodeFix {
		t.Fatalf("result template metric = %q in %#v", got, report.Results[0].Metrics)
	}
}

func TestDeepAgentGovernanceBlocksDisallowedHighRiskTool(t *testing.T) {
	controller := NewDeepAgentController(
		NewMemoryWorkflowStore(),
		NoopWorkflowEventSink{},
		staticDeepAgentPlanner{plan: DeepAgentPlan{Steps: []DeepAgentStep{{
			ID:            "patch",
			Title:         "Patch production",
			DoneCondition: "patched",
			RiskLevel:     RiskLevelHigh,
			Metadata: map[string]any{
				"tool": DeepAgentToolModeCodePatch,
				"args": map[string]any{"path": "production"},
			},
		}}}},
		completingDeepAgentExecutor{},
		nil,
	)
	controller.SetRiskGate(NewRuntimeDeepAgentRiskGate(nil))
	result, err := controller.Execute(context.Background(), DeepAgentTaskRequest{
		UserID: "alice",
		Goal:   "patch production",
		State: map[string]any{
			"deep_agent_governance": map[string]any{
				"allowed_high_risk_tools": []string{DeepAgentToolModeModel},
			},
		},
	})
	if !errors.Is(err, ErrDeepAgentBlocked) {
		t.Fatalf("expected blocked error, got %v", err)
	}
	if result == nil || result.State == nil || result.State.Status != DeepAgentRunStatusBlocked {
		t.Fatalf("unexpected result: %#v", result)
	}
	governance := deepAgentGovernanceStateForRun(result.State)
	if !governance.PolicyBlocked || !strings.Contains(governance.PolicyBlockReason, "not allowed") {
		t.Fatalf("expected policy block governance state, got %#v", governance)
	}
}

func TestDeepAgentGovernanceKillSwitchBlocksHighRiskAction(t *testing.T) {
	controller := NewDeepAgentController(
		NewMemoryWorkflowStore(),
		NoopWorkflowEventSink{},
		staticDeepAgentPlanner{plan: DeepAgentPlan{Steps: []DeepAgentStep{{
			ID:            "patch",
			Title:         "Patch production",
			DoneCondition: "patched",
			RiskLevel:     RiskLevelHigh,
			Metadata: map[string]any{
				"tool": DeepAgentToolModeCodePatch,
				"args": map[string]any{"path": "production"},
			},
		}}}},
		completingDeepAgentExecutor{},
		nil,
	)
	controller.SetRiskGate(NewRuntimeDeepAgentRiskGate(nil))
	result, err := controller.Execute(context.Background(), DeepAgentTaskRequest{
		UserID: "alice",
		Goal:   "patch production",
		State: map[string]any{
			"deep_agent_governance": map[string]any{"kill_switch": true},
		},
	})
	if !errors.Is(err, ErrDeepAgentBlocked) {
		t.Fatalf("expected blocked error, got %v", err)
	}
	if result == nil || result.State == nil || result.State.Status != DeepAgentRunStatusBlocked || !strings.Contains(result.State.Blocker, "kill switch") {
		t.Fatalf("unexpected kill switch result: %#v", result)
	}
	if _, ok := result.State.WorkingMemory["pending_review_action"]; ok {
		t.Fatalf("kill switch should block immediately, not create pending review: %#v", result.State.WorkingMemory)
	}
}

func TestDeepAgentAttemptStrategyChangesRetryHash(t *testing.T) {
	action := DeepAgentAction{
		StepID: "step-1",
		Tool:   DeepAgentToolModeModel,
		Args:   map[string]any{"prompt": "search product information"},
	}
	firstHash := deepAgentActionHash(action)
	retry := withDeepAgentAttemptStrategy(action, &DeepAgentState{
		NoProgressCount: 1,
		Blocker:         "rate limit 429: try again",
		WorkingMemory:   map[string]any{"last_retryable_error_class": DeepAgentErrorTransient},
	})
	secondHash := deepAgentActionHash(retry)
	if firstHash == secondHash {
		t.Fatalf("retry hash should differ after attempt strategy")
	}
	if got := deepAgentWorkflowString(retry.Args, "attempt_strategy"); !strings.Contains(got, "retry-2") {
		t.Fatalf("missing attempt_strategy: %#v", retry.Args)
	}
	if got := deepAgentWorkflowString(retry.Args, "prompt"); !strings.Contains(got, "Retry instruction:") || !strings.Contains(got, "rate limit") {
		t.Fatalf("retry prompt should include attempt instruction, got %q", got)
	}
}

func TestDeepAgentAttemptStrategyRequiresResearchToolsAfterMissingSources(t *testing.T) {
	action := DeepAgentAction{
		StepID: "research",
		Tool:   DeepAgentToolModeModel,
		Args:   map[string]any{"prompt": "调研 Chance AI 产品"},
	}
	retry := withDeepAgentAttemptStrategy(action, &DeepAgentState{
		NoProgressCount: 1,
		Blocker:         "research step source evidence is missing",
		WorkingMemory:   map[string]any{"last_retryable_error_class": DeepAgentErrorTransient},
	})
	prompt := deepAgentWorkflowString(retry.Args, "prompt")
	if !strings.Contains(prompt, "WebSearch") || !strings.Contains(prompt, "WebFetch") || !strings.Contains(prompt, "do not answer from memory") {
		t.Fatalf("missing-source retry should force research tools, got %q", prompt)
	}
}

func TestDeepAgentControllerRetriesResearchWithoutSourceEvidence(t *testing.T) {
	executor := &sourceEvidenceRetryExecutor{}
	controller := NewDeepAgentController(
		NewMemoryWorkflowStore(),
		NoopWorkflowEventSink{},
		sourceEvidenceRetryPlanner{},
		executor,
		nil,
	)
	result, err := controller.Execute(context.Background(), DeepAgentTaskRequest{
		UserID: "alice",
		Goal:   "调研 Chance AI 产品",
		Policy: DeepAgentPolicy{MaxSteps: 1, MaxActions: 3, NoProgressLimit: 2, MaxDuration: time.Minute},
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result == nil || result.State == nil || result.State.Status != DeepAgentRunStatusSucceeded {
		t.Fatalf("expected successful retry, got %#v", result)
	}
	if executor.calls != 2 {
		t.Fatalf("executor calls = %d, want 2", executor.calls)
	}
	if !strings.Contains(executor.retryPrompt, "WebSearch") || !strings.Contains(executor.retryPrompt, "WebFetch") {
		t.Fatalf("retry prompt should force web research tools, got %q", executor.retryPrompt)
	}
}

func TestDeepAgentControllerCarriesPriorResearchSourcesToFollowupStep(t *testing.T) {
	controller := NewDeepAgentController(
		NewMemoryWorkflowStore(),
		NoopWorkflowEventSink{},
		twoStepResearchPlanner{},
		twoStepResearchExecutor{},
		nil,
	)
	result, err := controller.Execute(context.Background(), DeepAgentTaskRequest{
		UserID: "alice",
		Goal:   "调研 Browserless 并整理资料",
		Policy: DeepAgentPolicy{MaxSteps: 2, MaxActions: 3, NoProgressLimit: 2, MaxDuration: time.Minute},
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result == nil || result.State == nil || result.State.Status != DeepAgentRunStatusSucceeded {
		t.Fatalf("expected successful source carry-forward, got %#v", result)
	}
	evidence, ok := (StateDeepAgentEvidenceStore{}).GetStepEvidence(result.State, "step-2")
	if !ok {
		t.Fatalf("missing step-2 evidence: %#v", result.State.WorkingMemory)
	}
	if len(evidence.Sources) < 2 {
		t.Fatalf("step-2 should inherit prior sources, got %#v", evidence)
	}
	if !deepAgentBool(evidence.Diagnostics, "inherited_source_evidence", false) {
		t.Fatalf("step-2 evidence should mark inherited sources, got %#v", evidence.Diagnostics)
	}
}

func TestFormatDeepAgentResultMessageIncludesArtifactRefs(t *testing.T) {
	state := &DeepAgentState{
		Goal: "生成调研报告",
		Plan: DeepAgentPlan{Steps: []DeepAgentStep{{
			ID:     "write-report",
			Title:  "写报告",
			Status: DeepAgentStepStatusSucceeded,
		}}},
		CompletedSteps: []string{"write-report"},
		WorkingMemory: map[string]any{
			"step_context": map[string]any{
				"write-report": map[string]any{
					"artifact_refs": []DeepAgentArtifactRef{{
						ID:          "artifact-1",
						StepID:      "write-report",
						Filename:    "tolan-report.md",
						ContentType: "text/markdown",
						SizeBytes:   256,
					}},
					"step_evidence": DeepAgentStepEvidence{
						StepID:  "write-report",
						Summary: "final report evidence",
						Sources: []DeepAgentSourceRef{{URL: "https://example.com/source", Title: "Source"}},
						Diagnostics: map[string]any{
							"command":    "go test ./...",
							"exit_code":  0,
							"not_tested": "browser screenshot not captured",
						},
					},
				},
			},
		},
	}
	message := formatDeepAgentResultMessage(&DeepAgentTaskResult{State: state, Run: &WorkflowRun{ID: "run-1"}}, nil)
	for _, want := range []string{"Artifacts", "tolan-report.md", "artifact-1", "Sources", "https://example.com/source", "Test results", "go test ./...", "Known gaps", "browser screenshot not captured"} {
		if !strings.Contains(message, want) {
			t.Fatalf("final message missing %q, got:\n%s", want, message)
		}
	}
}

func TestFormatDeepAgentResultMessageLimitsSources(t *testing.T) {
	sources := make([]DeepAgentSourceRef, 0, deepAgentResultMessageSourceLimit+3)
	for idx := 0; idx < deepAgentResultMessageSourceLimit+3; idx++ {
		sources = append(sources, DeepAgentSourceRef{
			URL:   fmt.Sprintf("https://example.com/source-%d", idx+1),
			Title: fmt.Sprintf("Source %d", idx+1),
		})
	}
	state := &DeepAgentState{
		Goal: "生成调研报告",
		Plan: DeepAgentPlan{Steps: []DeepAgentStep{{
			ID:     "write-report",
			Title:  "写报告",
			Status: DeepAgentStepStatusSucceeded,
		}}},
		CompletedSteps: []string{"write-report"},
		WorkingMemory: map[string]any{
			"step_context": map[string]any{
				"write-report": map[string]any{
					"step_evidence": DeepAgentStepEvidence{
						StepID:  "write-report",
						Summary: "final report evidence",
						Sources: sources,
					},
				},
			},
		},
	}
	message := formatDeepAgentResultMessage(&DeepAgentTaskResult{State: state, Run: &WorkflowRun{ID: "run-1"}}, nil)
	if !strings.Contains(message, "https://example.com/source-8") {
		t.Fatalf("final message should include the display limit source, got:\n%s", message)
	}
	if strings.Contains(message, "https://example.com/source-9") {
		t.Fatalf("final message should not include sources beyond the display limit, got:\n%s", message)
	}
	if !strings.Contains(message, "还有 3 条来源已保留在 Job trace 中。") {
		t.Fatalf("final message should summarize hidden sources, got:\n%s", message)
	}
}

func TestFormatDeepAgentResultMessageLabelsRepeatedStepActions(t *testing.T) {
	state := &DeepAgentState{
		Goal: "调研 Chance AI",
		Plan: DeepAgentPlan{Steps: []DeepAgentStep{
			{ID: "step-1", Title: "收集资料", Status: DeepAgentStepStatusSucceeded},
			{ID: "step-2", Title: "分析产品", Status: DeepAgentStepStatusSucceeded},
			{ID: "step-3", Title: "生成文档", Status: DeepAgentStepStatusSucceeded},
		}},
		ActionCount: 4,
		ActionHistory: []DeepAgentAction{
			{StepID: "step-1", Tool: DeepAgentToolModeModel},
			{StepID: "step-2", Tool: DeepAgentToolModeModel},
			{StepID: "step-2", Tool: DeepAgentToolModeModel},
			{StepID: "step-3", Tool: DeepAgentToolModeModelArtifact},
		},
		WorkingMemory: map[string]any{},
	}
	store := StateDeepAgentEvidenceStore{}
	for idx, action := range state.ActionHistory {
		store.PutStepEvidence(state, DeepAgentStepEvidence{
			ActionID: fmt.Sprintf("action-%d", idx+1),
			StepID:   action.StepID,
			Diagnostics: map[string]any{
				"status": "succeeded",
			},
		})
	}

	message := formatDeepAgentResultMessage(&DeepAgentTaskResult{State: state, Run: &WorkflowRun{ID: "run-1"}}, nil)
	for _, want := range []string{
		"action-1 · step-1 · model：succeeded",
		"action-2 · step-2 · model：succeeded",
		"action-3 · step-2 · model：succeeded",
		"action-4 · step-3 · model_artifact：succeeded",
	} {
		if !strings.Contains(message, want) {
			t.Fatalf("final message missing %q, got:\n%s", want, message)
		}
	}
	if strings.Contains(message, "\n- step-2：succeeded") {
		t.Fatalf("repeated step result should be action-labeled, got:\n%s", message)
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
	learning := result.State.Learnings[0]
	if learning.Status != deepAgentLearningStatusPending || !learning.RequiresUserConfirmation || learning.MemoryItemID == "" || learning.RunID == "" || learning.StepID != "finish" || learning.EvidenceID == "" {
		t.Fatalf("learning should be governed before entering state, got %#v", learning)
	}
	items, err := runtime.ListMemoryItems(context.Background(), "alice", MemoryItemFilter{Status: MemoryStatusPendingConfirm})
	if err != nil {
		t.Fatalf("ListMemoryItems() error = %v", err)
	}
	if len(items) != 1 || items[0].Metadata["deep_agent_learning"] != true {
		t.Fatalf("expected pending deep agent memory candidate, got %#v", items)
	}
	if items[0].Level != MemoryLevelAtomic || items[0].Status != MemoryStatusPendingConfirm || items[0].Metadata["l3_profile_allowed"] != false || items[0].RawHash == "" {
		t.Fatalf("learning candidate should stay pending atomic with dedupe metadata, got %#v", items[0])
	}
	if items[0].Metadata["review_status"] != deepAgentLearningStatusPending || items[0].Metadata["workflow_run_id"] == "" || items[0].Metadata["step_id"] != "finish" || items[0].Metadata["evidence_id"] == "" || len(items[0].SourceRefs) < 2 {
		t.Fatalf("learning candidate should carry review provenance, got %#v", items[0])
	}
	_, err = runtime.ExecuteDeepAgentTask(context.Background(), DeepAgentTaskRequest{
		UserID: "alice",
		Goal:   "learn successful path",
		Plan:   plan,
	}, staticDeepAgentPlanner{plan: plan}, completingDeepAgentExecutor{}, nil)
	if err != nil {
		t.Fatalf("second ExecuteDeepAgentTask() error = %v", err)
	}
	items, err = runtime.ListMemoryItems(context.Background(), "alice", MemoryItemFilter{Status: MemoryStatusPendingConfirm})
	if err != nil {
		t.Fatalf("ListMemoryItems() after duplicate run error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("duplicate learning candidate should be deduped, got %#v", items)
	}
}

func TestRuntimeDeepAgentLearningGovernancePolicy(t *testing.T) {
	memory := NewFileMemoryService(t.TempDir())
	runtime := NewRuntime(
		RuntimeConfig{},
		NewFileSessionStore(t.TempDir()),
		memory,
		nil,
		func(Scope) Runner { return echoRunner{} },
	)
	sink := NewRuntimeDeepAgentLearningSink(runtime)
	now := time.Now()
	err := sink.PersistDeepAgentLearnings(context.Background(), nil, &DeepAgentState{}, []DeepAgentLearningCandidate{
		{
			ID:          "fact",
			Type:        MemoryCategoryFact,
			Status:      deepAgentLearningStatusCandidate,
			UserID:      "alice",
			SessionID:   "sess-1",
			RunID:       "run-1",
			StepID:      "step-1",
			EvidenceID:  "ev-1",
			RiskLevel:   deepAgentLearningRiskLow,
			Sensitivity: deepAgentLearningSensitivityLow,
			Visibility:  MemoryVisibilityUser,
			Content:     "The project uses Vitest for the web test runner.",
			Metadata:    map[string]any{"confidence": 0.9},
			CreatedAt:   now,
		},
		{
			ID:          "pref",
			Type:        MemoryCategoryPreference,
			Status:      deepAgentLearningStatusCandidate,
			UserID:      "alice",
			SessionID:   "sess-1",
			RunID:       "run-1",
			StepID:      "step-2",
			EvidenceID:  "ev-2",
			RiskLevel:   deepAgentLearningRiskLow,
			Sensitivity: deepAgentLearningSensitivityLow,
			Visibility:  MemoryVisibilityUser,
			Content:     "The user prefers very terse implementation reports.",
			Metadata:    map[string]any{"confidence": 0.95, "preference_level": "L3"},
			CreatedAt:   now,
		},
	})
	if err != nil {
		t.Fatalf("PersistDeepAgentLearnings() error = %v", err)
	}
	active, err := runtime.ListMemoryItems(context.Background(), "alice", MemoryItemFilter{Status: MemoryStatusActive})
	if err != nil {
		t.Fatalf("ListMemoryItems(active) error = %v", err)
	}
	if len(active) != 1 || active[0].ID != deepAgentLearningMemoryItemID("fact") || active[0].Metadata["review_status"] != deepAgentLearningStatusAccepted {
		t.Fatalf("low-risk fact should auto-accept, got %#v", active)
	}
	pending, err := runtime.ListMemoryItems(context.Background(), "alice", MemoryItemFilter{Status: MemoryStatusPendingConfirm})
	if err != nil {
		t.Fatalf("ListMemoryItems(pending) error = %v", err)
	}
	if len(pending) != 1 || pending[0].ID != deepAgentLearningMemoryItemID("pref") || pending[0].Metadata["review_status"] != deepAgentLearningStatusPending || pending[0].Metadata["user_confirmation_required"] != true {
		t.Fatalf("L3 preference should require user confirmation, got %#v", pending)
	}
}

func TestRuntimeReviewDeepAgentLearningCandidate(t *testing.T) {
	memory := NewFileMemoryService(t.TempDir())
	runtime := NewRuntime(
		RuntimeConfig{},
		NewFileSessionStore(t.TempDir()),
		memory,
		nil,
		func(Scope) Runner { return echoRunner{} },
	)
	sink := NewRuntimeDeepAgentLearningSink(runtime)
	candidate := DeepAgentLearningCandidate{
		ID:         "review-me",
		Type:       deepAgentLearningTypeSuccessPath,
		Status:     deepAgentLearningStatusCandidate,
		UserID:     "alice",
		SessionID:  "sess-1",
		RunID:      "run-1",
		StepID:     "step-1",
		EvidenceID: "ev-1",
		Content:    "DeepAgent success path can be reused after explicit review.",
		Metadata:   map[string]any{"confidence": 0.9},
		CreatedAt:  time.Now(),
	}
	if err := sink.PersistDeepAgentLearnings(context.Background(), nil, &DeepAgentState{}, []DeepAgentLearningCandidate{candidate}); err != nil {
		t.Fatalf("PersistDeepAgentLearnings() error = %v", err)
	}
	accepted, err := runtime.ReviewDeepAgentLearningCandidate(context.Background(), "alice", candidate.ID, "accept", "alice", "looks correct")
	if err != nil {
		t.Fatalf("ReviewDeepAgentLearningCandidate(accept) error = %v", err)
	}
	if accepted.Status != MemoryStatusActive || accepted.Metadata["review_status"] != deepAgentLearningStatusAccepted || accepted.Metadata["user_confirmed"] != true {
		t.Fatalf("accept should activate candidate, got %#v", accepted)
	}
	rolledBack, err := runtime.ReviewDeepAgentLearningCandidate(context.Background(), "alice", candidate.ID, "rollback", "alice", "undo")
	if err != nil {
		t.Fatalf("ReviewDeepAgentLearningCandidate(rollback) error = %v", err)
	}
	if rolledBack.Status != MemoryStatusDeleted || rolledBack.Metadata["review_status"] != deepAgentLearningStatusRollback || rolledBack.Metadata["review_reason"] != "undo" {
		t.Fatalf("rollback should mark candidate deleted with audit metadata, got %#v", rolledBack)
	}

	rejectCandidate := candidate
	rejectCandidate.ID = "reject-me"
	rejectCandidate.Content = "DeepAgent candidate to reject."
	if err := sink.PersistDeepAgentLearnings(context.Background(), nil, &DeepAgentState{}, []DeepAgentLearningCandidate{rejectCandidate}); err != nil {
		t.Fatalf("PersistDeepAgentLearnings(reject) error = %v", err)
	}
	rejected, err := runtime.ReviewDeepAgentLearningCandidate(context.Background(), "alice", rejectCandidate.ID, "reject", "alice", "not useful")
	if err != nil {
		t.Fatalf("ReviewDeepAgentLearningCandidate(reject) error = %v", err)
	}
	if rejected.Status != MemoryStatusArchived || rejected.Metadata["review_status"] != deepAgentLearningStatusRejected || rejected.Metadata["review_reason"] != "not useful" {
		t.Fatalf("reject should archive candidate, got %#v", rejected)
	}
}

func TestRuntimeDeepAgentLearningSinkFiltersUnsafeOrLowConfidenceCandidates(t *testing.T) {
	memory := NewFileMemoryService(t.TempDir())
	runtime := NewRuntime(
		RuntimeConfig{},
		NewFileSessionStore(t.TempDir()),
		memory,
		nil,
		func(Scope) Runner { return echoRunner{} },
	)
	sink := NewRuntimeDeepAgentLearningSink(runtime)
	err := sink.PersistDeepAgentLearnings(context.Background(), nil, &DeepAgentState{}, []DeepAgentLearningCandidate{
		{
			ID:        "low",
			Type:      deepAgentLearningTypeSuccessPath,
			Status:    deepAgentLearningStatusCandidate,
			UserID:    "alice",
			Content:   "Low confidence learning",
			Metadata:  map[string]any{"confidence": 0.2},
			CreatedAt: time.Now(),
		},
		{
			ID:        "secret",
			Type:      deepAgentLearningTypeSuccessPath,
			Status:    deepAgentLearningStatusCandidate,
			UserID:    "alice",
			Content:   "api key: should never enter memory",
			Metadata:  map[string]any{"confidence": 0.9},
			CreatedAt: time.Now(),
		},
	})
	if err != nil {
		t.Fatalf("PersistDeepAgentLearnings() error = %v", err)
	}
	items, err := runtime.ListMemoryItems(context.Background(), "alice", MemoryItemFilter{Status: ""})
	if err != nil {
		t.Fatalf("ListMemoryItems() error = %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("unsafe or low-confidence candidates should not enter memory, got %#v", items)
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
	blocked := DeepAgentRecoveryState{BlockedReason: "source evidence is missing", MissingInfo: []string{"source"}}
	if got := deepAgentBlockedCategory(&DeepAgentState{Status: DeepAgentRunStatusBlocked, Blocker: blocked.BlockedReason}, blocked); got != "missing_user_info" {
		t.Fatalf("blocked category = %q", got)
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

type runtimeRoutingStaticPlanner struct {
	runtime *Runtime
	plan    DeepAgentPlan
}

func (p runtimeRoutingStaticPlanner) CreatePlan(_ context.Context, req DeepAgentTaskRequest) (DeepAgentPlan, error) {
	plan := p.plan
	plan.Goal = req.Goal
	return plan, nil
}

func (p runtimeRoutingStaticPlanner) NextAction(ctx context.Context, state *DeepAgentState, step DeepAgentStep) (DeepAgentAction, error) {
	return NewRuntimeDeepAgentPlanner(p.runtime).NextAction(ctx, state, step)
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

type researchReportCompletingDeepAgentExecutor struct{}

func (researchReportCompletingDeepAgentExecutor) ExecuteDeepAgentAction(_ context.Context, action DeepAgentAction, _ *DeepAgentState) (DeepAgentActionResult, error) {
	evidence := DeepAgentStepEvidence{
		StepID:  action.StepID,
		Output:  "Research report evidence collected.",
		Summary: "Collected multiple sources. Coverage includes company team, product features, pricing availability, user reviews, competitors, and risks uncertainty.",
		Sources: []DeepAgentSourceRef{
			{URL: "https://example.com/research/about", Title: "Company team and product features", Snippet: "Company team, product features, and pricing availability.", Provider: "WebSearch"},
			{URL: "https://example.com/research/reviews", Title: "User reviews and competitors", Snippet: "User reviews compare competitors and identify risks uncertainty.", Provider: "WebSearch"},
		},
		Artifacts: []DeepAgentArtifactRef{{
			ID:          "artifact-1",
			Filename:    "research-report.md",
			ContentType: "text/markdown",
			SizeBytes:   1024,
			StepID:      action.StepID,
		}},
	}
	return DeepAgentActionResult{
		Status:    DeepAgentActionStatusSucceeded,
		Output:    evidence.Summary,
		Completed: true,
		Metadata: map[string]any{
			"step_evidence":  evidence,
			"artifact_refs":  evidence.Artifacts,
			"sources":        evidence.Sources,
			"citation_count": 2,
		},
	}, nil
}

type sourceEvidenceRetryPlanner struct{}

func (sourceEvidenceRetryPlanner) CreatePlan(_ context.Context, req DeepAgentTaskRequest) (DeepAgentPlan, error) {
	return DeepAgentPlan{
		Goal: req.Goal,
		Steps: []DeepAgentStep{{
			ID:            "research",
			Title:         "调研 Chance AI 产品",
			Intent:        "通过网络搜索调研 Chance AI 产品",
			DoneCondition: "收集到带来源的研究信息",
		}},
	}, nil
}

func (sourceEvidenceRetryPlanner) NextAction(_ context.Context, _ *DeepAgentState, step DeepAgentStep) (DeepAgentAction, error) {
	route := DeepAgentStepRoute{
		StepID:       step.ID,
		Mode:         DeepAgentToolModeModel,
		Executor:     deepAgentRouteExecutorModel,
		SearchScope:  "web",
		AllowedTools: []string{"WebSearch", "WebFetch"},
	}
	return DeepAgentAction{
		StepID: step.ID,
		Tool:   DeepAgentToolModeModel,
		Args: map[string]any{
			"prompt":     "调研 Chance AI 产品",
			"step_route": deepAgentStepRouteMap(route),
		},
	}, nil
}

type twoStepResearchPlanner struct{}

func (twoStepResearchPlanner) CreatePlan(_ context.Context, req DeepAgentTaskRequest) (DeepAgentPlan, error) {
	return DeepAgentPlan{
		Goal: req.Goal,
		Steps: []DeepAgentStep{
			{
				ID:            "step-1",
				Title:         "初步研究 Browserless",
				Intent:        "通过网络搜索调研 Browserless 产品",
				DoneCondition: "收集到带来源的研究信息",
			},
			{
				ID:            "step-2",
				Title:         "整理深入调研材料",
				Intent:        "整理 Browserless 研究信息并保留来源",
				DoneCondition: "研究信息有来源证据",
			},
		},
	}, nil
}

func (twoStepResearchPlanner) NextAction(_ context.Context, _ *DeepAgentState, step DeepAgentStep) (DeepAgentAction, error) {
	route := DeepAgentStepRoute{
		StepID:       step.ID,
		Mode:         DeepAgentToolModeModel,
		Executor:     deepAgentRouteExecutorModel,
		SearchScope:  "web",
		AllowedTools: []string{"WebSearch", "WebFetch"},
	}
	return DeepAgentAction{
		StepID: step.ID,
		Tool:   DeepAgentToolModeModel,
		Args: map[string]any{
			"prompt":     step.Intent,
			"step_route": deepAgentStepRouteMap(route),
		},
	}, nil
}

type sourceEvidenceRetryExecutor struct {
	calls       int
	retryPrompt string
}

func (e *sourceEvidenceRetryExecutor) ExecuteDeepAgentAction(_ context.Context, action DeepAgentAction, _ *DeepAgentState) (DeepAgentActionResult, error) {
	e.calls++
	route, _ := deepAgentStepRouteFromMap(action.Args)
	evidence := DeepAgentStepEvidence{
		StepID:  action.StepID,
		Route:   route,
		Output:  "Chance AI research summary",
		Summary: "Chance AI research summary",
	}
	if e.calls > 1 {
		e.retryPrompt = deepAgentWorkflowString(action.Args, "prompt")
		evidence.Sources = []DeepAgentSourceRef{{URL: "https://example.com/chance-ai", Title: "Chance AI source", Provider: "WebSearch"}}
		evidence.ToolCalls = []DeepAgentToolCallRef{{Name: "WebSearch", Status: "succeeded"}}
	}
	return DeepAgentActionResult{
		Status:    DeepAgentActionStatusSucceeded,
		Output:    evidence.Output,
		Completed: true,
		Metadata: map[string]any{
			"step_evidence": deepAgentStepEvidenceMap(evidence),
		},
	}, nil
}

type twoStepResearchExecutor struct{}

func (twoStepResearchExecutor) ExecuteDeepAgentAction(_ context.Context, action DeepAgentAction, _ *DeepAgentState) (DeepAgentActionResult, error) {
	route, _ := deepAgentStepRouteFromMap(action.Args)
	evidence := DeepAgentStepEvidence{
		StepID:  action.StepID,
		Route:   route,
		Output:  "Browserless research notes",
		Summary: "Browserless research notes",
	}
	if action.StepID == "step-1" {
		evidence.Sources = []DeepAgentSourceRef{
			{URL: "https://www.browserless.io/", Title: "Browserless official site", Provider: "WebSearch"},
			{URL: "https://www.browserless.io/pricing", Title: "Browserless pricing", Provider: "WebFetch"},
		}
		evidence.ToolCalls = []DeepAgentToolCallRef{
			{Name: "WebSearch", Status: "succeeded"},
			{Name: "WebFetch", Status: "succeeded"},
		}
	} else {
		evidence.Output = "已根据上一步研究结果整理 Browserless 深入调研材料。"
		evidence.Summary = evidence.Output
	}
	return DeepAgentActionResult{
		Status:    DeepAgentActionStatusSucceeded,
		Output:    evidence.Output,
		Completed: true,
		Metadata: map[string]any{
			"step_evidence": deepAgentStepEvidenceMap(evidence),
		},
	}, nil
}

type artifactDetailDeepAgentExecutor struct{}

func (artifactDetailDeepAgentExecutor) ExecuteDeepAgentAction(_ context.Context, action DeepAgentAction, _ *DeepAgentState) (DeepAgentActionResult, error) {
	return DeepAgentActionResult{
		Status:    DeepAgentActionStatusSucceeded,
		Output:    fmt.Sprintf("completed %s with artifact", action.StepID),
		Completed: true,
		Metadata: map[string]any{
			"artifact_count":        1,
			"artifact_id":           "artifact-1",
			"artifact_filename":     "report.docx",
			"artifact_content_type": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
			"assistant_tool_names":  []string{"Artifact"},
			"sources":               []DeepAgentSourceRef{{URL: "https://example.com/report-source", Title: "Source"}},
			"child_jobs":            []DeepAgentChildJobRef{{ID: "job-child", Type: "skill", Status: JobStatusSucceeded}},
		},
	}, nil
}

type failingDeepAgentExecutor struct {
	err string
}

func (e failingDeepAgentExecutor) ExecuteDeepAgentAction(context.Context, DeepAgentAction, *DeepAgentState) (DeepAgentActionResult, error) {
	err := fmt.Errorf("%s", e.err)
	return DeepAgentActionResult{Status: DeepAgentActionStatusFailed, Error: err.Error(), Metadata: map[string]any{"error_class": classifyDeepAgentError(err, DeepAgentActionResult{Error: err.Error()})}}, err
}

type emptyDeepAgentExecutor struct{}

func (emptyDeepAgentExecutor) ExecuteDeepAgentAction(context.Context, DeepAgentAction, *DeepAgentState) (DeepAgentActionResult, error) {
	return DeepAgentActionResult{Status: DeepAgentActionStatusSucceeded}, nil
}

type evidenceOnlyDeepAgentExecutor struct{}

func (evidenceOnlyDeepAgentExecutor) ExecuteDeepAgentAction(_ context.Context, action DeepAgentAction, _ *DeepAgentState) (DeepAgentActionResult, error) {
	route, _ := deepAgentStepRouteFromMap(action.Args)
	if route.StepID == "" {
		route = DeepAgentStepRoute{
			StepID:      action.StepID,
			Mode:        DeepAgentToolModeModel,
			Executor:    deepAgentRouteExecutorModel,
			SearchScope: "web",
		}
	}
	return DeepAgentActionResult{
		Status:    DeepAgentActionStatusSucceeded,
		Completed: true,
		Metadata: map[string]any{
			"step_evidence": DeepAgentStepEvidence{
				StepID:  action.StepID,
				Route:   route,
				Summary: "sourced evidence without free-form output",
				Sources: []DeepAgentSourceRef{{URL: "https://example.com/source", Title: "Source", Provider: "WebSearch"}},
			},
		},
	}, nil
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

type contextCatalogRunner struct{}

func (contextCatalogRunner) Run(ctx context.Context, session *state.Session, prompt string) (engine.Result, error) {
	return contextCatalogRunner{}.RunGeneratedPrompt(ctx, session, prompt)
}

func (contextCatalogRunner) RunGeneratedPrompt(_ context.Context, session *state.Session, prompt string) (engine.Result, error) {
	output := "Tolan AI research notes with sources: https://example.com/tolan"
	if !strings.Contains(prompt, "This is not a deliverable-file step") {
		output = "# Tolan AI 调研报告\n\n## 摘要\n\nTolan AI 是一个 AI 产品。\n\n## 来源\n\n- https://example.com/tolan"
	}
	session.AddAssistantMessage(output)
	return engine.Result{Output: output, Session: session}, nil
}

func (contextCatalogRunner) Descriptors() []toolkit.Descriptor {
	return []toolkit.Descriptor{
		{Name: "WebSearch", Description: "Search the web for current information."},
		{Name: "WebFetch", Description: "Fetch a web page."},
		{Name: ArtifactToolName, Description: "Create downloadable artifacts."},
	}
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

type deepAgentExecutionPromptRunner struct {
	runCalls       int
	generatedCalls int
}

func (r *deepAgentExecutionPromptRunner) Run(_ context.Context, session *state.Session, prompt string) (engine.Result, error) {
	r.runCalls++
	session.AddUserMessage(prompt)
	output := "Collected Tolan product information with cited sources."
	session.AddAssistantMessage(output)
	return engine.Result{Output: output, Session: session}, nil
}

func (r *deepAgentExecutionPromptRunner) RunGeneratedPrompt(context.Context, *state.Session, string) (engine.Result, error) {
	r.generatedCalls++
	return engine.Result{}, fmt.Errorf("queryengine empty response: no assistant text or tool calls")
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

type toolNotFoundArtifactRunner struct{}

func (toolNotFoundArtifactRunner) Run(ctx context.Context, session *state.Session, prompt string) (engine.Result, error) {
	return toolNotFoundArtifactRunner{}.RunGeneratedPrompt(ctx, session, prompt)
}

func (toolNotFoundArtifactRunner) RunGeneratedPrompt(_ context.Context, session *state.Session, _ string) (engine.Result, error) {
	output := `工具未找到：Skill。抱歉，无法生成 Word 格式文档。`
	session.AddAssistantMessage(output)
	return engine.Result{Output: output, Session: session}, nil
}

type countingErrorRunner struct {
	calls int
	err   error
}

func (r *countingErrorRunner) Run(ctx context.Context, session *state.Session, prompt string) (engine.Result, error) {
	return r.RunGeneratedPrompt(ctx, session, prompt)
}

func (r *countingErrorRunner) RunGeneratedPrompt(context.Context, *state.Session, string) (engine.Result, error) {
	r.calls++
	return engine.Result{}, r.err
}

type runAsJobDocxMarkerRunner struct{}

func (runAsJobDocxMarkerRunner) Run(ctx context.Context, session *state.Session, prompt string) (engine.Result, error) {
	return runAsJobDocxMarkerRunner{}.RunGeneratedPrompt(ctx, session, prompt)
}

func (runAsJobDocxMarkerRunner) RunGeneratedPrompt(_ context.Context, session *state.Session, _ string) (engine.Result, error) {
	input := json.RawMessage(`{"skill":"docx","args":"Tolan AI 调研报告"}`)
	session.AddAssistantMessageWithTools("", []state.ToolCall{{
		ID:    "skill-call-1",
		Name:  skilltool.ToolName,
		Input: input,
	}})
	session.AddToolResult("skill-call-1", skilltool.ToolName, input, skilltool.RunAsJobMarkerPrefix+`{"skill":"docx","args":"Tolan AI 调研报告","run_as_job":true}`)
	return engine.Result{Session: session}, nil
}

type routeJSONRunner struct{}

func (routeJSONRunner) Run(ctx context.Context, session *state.Session, prompt string) (engine.Result, error) {
	return routeJSONRunner{}.RunGeneratedPrompt(ctx, session, prompt)
}

func (routeJSONRunner) RunGeneratedPrompt(_ context.Context, session *state.Session, _ string) (engine.Result, error) {
	output := `{
  "mode": "model_artifact",
  "executor": "artifact",
  "skill_name": "",
  "requires_artifact": true,
  "deliverable_type": "markdown",
  "filename_hint": "final-report.md",
  "allowed_tools": ["WebSearch", "WebFetch", "Artifact"],
  "search_scope": "web",
  "success_criteria": ["downloadable markdown artifact exists"],
  "reason": "final deliverable should be saved as an artifact",
  "confidence": "high"
}`
	session.AddAssistantMessage(output)
	return engine.Result{Output: output, Session: session}, nil
}

type invalidRouteRunner struct{}

func (invalidRouteRunner) Run(ctx context.Context, session *state.Session, prompt string) (engine.Result, error) {
	return invalidRouteRunner{}.RunGeneratedPrompt(ctx, session, prompt)
}

func (invalidRouteRunner) RunGeneratedPrompt(_ context.Context, session *state.Session, _ string) (engine.Result, error) {
	output := "not json"
	session.AddAssistantMessage(output)
	return engine.Result{Output: output, Session: session}, nil
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

type emptyResponseDeepAgentPlanRunner struct{}

func (emptyResponseDeepAgentPlanRunner) Run(ctx context.Context, session *state.Session, prompt string) (engine.Result, error) {
	return emptyResponseDeepAgentPlanRunner{}.RunGeneratedPrompt(ctx, session, prompt)
}

func (emptyResponseDeepAgentPlanRunner) RunGeneratedPrompt(context.Context, *state.Session, string) (engine.Result, error) {
	return engine.Result{}, fmt.Errorf("queryengine empty response: no assistant text or tool calls")
}
