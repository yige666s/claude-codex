package agentruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"claude-codex/internal/harness/engine"
	"claude-codex/internal/harness/state"
)

func TestRuntimeDeepAgentPlannerUsesPromptRegistryMetadata(t *testing.T) {
	ctx := context.Background()
	runner := &deepAgentPromptCaptureRunner{output: `{"goal":"research product pricing","steps":[{"id":"step-1","title":"Research","intent":"Research the topic","depends_on":[],"done_condition":"Facts collected","risk_level":"low"}]}`}
	runtime := NewRuntime(RuntimeConfig{}, NewFileSessionStore(t.TempDir()), nil, nil, func(Scope) Runner { return runner })
	store := NewMemoryPromptStore()
	upsertPromptVersion(t, store, PromptIDRuntimeDeepAgentPlanner, "planner-v2", "REGISTRY_PLANNER %d\n%s\n%s\n%s\n%s")
	runtime.SetPromptStore(store)

	plan, err := NewRuntimeDeepAgentPlanner(runtime).CreatePlan(ctx, DeepAgentTaskRequest{
		UserID:    "alice",
		SessionID: "session-1",
		Goal:      "research product pricing",
		Policy:    DeepAgentPolicy{MaxSteps: 3},
	})
	if err != nil {
		t.Fatalf("CreatePlan() error = %v", err)
	}
	if len(plan.Steps) != 1 || !strings.Contains(runner.prompt, "REGISTRY_PLANNER") {
		t.Fatalf("planner did not use registry prompt, plan=%#v prompt=%q", plan, runner.prompt)
	}
	if runner.metadata.PromptID != PromptIDRuntimeDeepAgentPlanner || runner.metadata.PromptVersion != "planner-v2" || runner.metadata.PromptHash == "" {
		t.Fatalf("planner call missing prompt metadata: %#v", runner.metadata)
	}
}

func TestRuntimeDeepAgentRouterUsesPromptRegistryMetadata(t *testing.T) {
	ctx := context.Background()
	runner := &deepAgentPromptCaptureRunner{output: `{"mode":"model_artifact","executor":"artifact","requires_artifact":true,"deliverable_type":"markdown","allowed_tools":["WebSearch","WebFetch","Artifact"],"success_criteria":["done"],"reason":"test","confidence":"high"}`}
	runtime := NewRuntime(RuntimeConfig{}, NewFileSessionStore(t.TempDir()), nil, nil, func(Scope) Runner { return runner })
	store := NewMemoryPromptStore()
	upsertPromptVersion(t, store, PromptIDRuntimeDeepAgentRouter, "router-v2", "REGISTRY_ROUTER %s %s %s %s %s %s")
	runtime.SetPromptStore(store)

	route, err := NewRuntimeDeepAgentStepRouter(runtime).RouteStep(ctx, &DeepAgentState{
		Goal:          "prepare final deliverable",
		WorkingMemory: map[string]any{"user_id": "alice", "session_id": "session-1"},
	}, DeepAgentStep{ID: "final", Title: "Finalize answer", Intent: "Package final answer", DoneCondition: "done"})
	if err != nil {
		t.Fatalf("RouteStep() error = %v", err)
	}
	if route.Mode != DeepAgentToolModeModelArtifact || !strings.Contains(runner.prompt, "REGISTRY_ROUTER") {
		t.Fatalf("router did not use registry prompt, route=%#v prompt=%q", route, runner.prompt)
	}
	if runner.metadata.PromptID != PromptIDRuntimeDeepAgentRouter || runner.metadata.PromptVersion != "router-v2" || runner.metadata.PromptHash == "" {
		t.Fatalf("router call missing prompt metadata: %#v", runner.metadata)
	}
}

func TestRuntimeDeepAgentClassifierAndReminderUsePromptRegistry(t *testing.T) {
	ctx := context.Background()
	runner := &deepAgentPromptCaptureRunner{output: DeepAgentToolModeMulti}
	runtime := NewRuntime(RuntimeConfig{}, NewFileSessionStore(t.TempDir()), nil, nil, func(Scope) Runner { return runner })
	store := NewMemoryPromptStore()
	upsertPromptVersion(t, store, PromptIDRuntimeDeepAgentModeClassifier, "classifier-v2", "REGISTRY_CLASSIFIER %s %s %s %s %s %s %s %s %s %s %s %s %s %s %s %s %s %s %s %s %s %s")
	upsertPromptVersion(t, store, PromptIDRuntimeDeepAgentToolUsageReminder, "reminder-v2", "REGISTRY_REMINDER")
	runtime.SetPromptStore(store)
	planner := NewRuntimeDeepAgentPlanner(runtime)
	agentState := &DeepAgentState{
		Goal:          "coordinate broad research",
		WorkingMemory: map[string]any{"user_id": "alice", "session_id": "session-1"},
	}

	mode := planner.llmRouteStep(ctx, agentState, DeepAgentStep{ID: "broad", Title: "Research broadly", Intent: "Research broadly", DoneCondition: "done"})
	if mode != DeepAgentToolModeMulti || !strings.Contains(runner.prompt, "REGISTRY_CLASSIFIER") {
		t.Fatalf("classifier did not use registry prompt, mode=%q prompt=%q", mode, runner.prompt)
	}
	if runner.metadata.PromptID != PromptIDRuntimeDeepAgentModeClassifier || runner.metadata.PromptVersion != "classifier-v2" {
		t.Fatalf("classifier call missing prompt metadata: %#v", runner.metadata)
	}
	if prompt := planner.modelPromptForStep(agentState, DeepAgentStep{ID: "model", Intent: "do work"}); !strings.Contains(prompt, "REGISTRY_REMINDER") {
		t.Fatalf("model prompt did not use registry tool reminder: %s", prompt)
	}
}

func TestDeepAgentPromptGoldenSetsAndProductionGate(t *testing.T) {
	server := NewServer(NewRuntime(RuntimeConfig{}, nil, nil, nil, nil), HeaderAuthenticator{UserHeader: "X-User-ID"}, NoopRateLimiter{}, nil)
	evals := NewMemoryEvaluationStore()
	server.SetEvaluationStore(evals)
	set, err := server.getGoldenSetVersion(context.Background(), "deep_agent_prompt_planner", DeepAgentPromptEvalSetVersion)
	if err != nil {
		t.Fatalf("builtin golden set lookup: %v", err)
	}
	if len(set.Cases) == 0 || set.Metadata["phase"] != "phase3" {
		t.Fatalf("unexpected builtin planner golden set: %#v", set)
	}
	if err := server.validatePromptEnvironmentPinGate(context.Background(), PromptIDRuntimeDeepAgentPlanner, "planner-v2", PromptEnvironmentProduction, ""); err == nil {
		t.Fatal("production prompt pin without eval_run_id should be rejected")
	}
	now := time.Now().UTC()
	if _, err := evals.CreateEvaluationRun(context.Background(), EvaluationRun{ID: "eval-wrong", Status: EvaluationRunStatusCompleted, StartedAt: now, CompletedAt: &now, Metrics: map[string]any{"prompt_id": PromptIDRuntimeDeepAgentPlanner, "prompt_version": "other"}}); err != nil {
		t.Fatalf("create eval run: %v", err)
	}
	if err := server.validatePromptEnvironmentPinGate(context.Background(), PromptIDRuntimeDeepAgentPlanner, "planner-v2", PromptEnvironmentProduction, "eval-wrong"); err == nil {
		t.Fatal("production gate should reject eval run bound to another version")
	}
	if _, err := evals.CreateEvaluationRun(context.Background(), EvaluationRun{ID: "eval-failed", Status: EvaluationRunStatusCompleted, StartedAt: now, CompletedAt: &now, Failed: 1, Metrics: map[string]any{"prompt_id": PromptIDRuntimeDeepAgentPlanner, "prompt_version": "planner-v2"}}); err != nil {
		t.Fatalf("create failed eval run: %v", err)
	}
	if err := server.validatePromptEnvironmentPinGate(context.Background(), PromptIDRuntimeDeepAgentPlanner, "planner-v2", PromptEnvironmentProduction, "eval-failed"); err == nil {
		t.Fatal("production gate should reject eval run with failed results")
	}
	if _, err := evals.CreateEvaluationRun(context.Background(), EvaluationRun{ID: "eval-1", Status: EvaluationRunStatusCompleted, StartedAt: now, CompletedAt: &now, Metrics: map[string]any{"prompt_id": PromptIDRuntimeDeepAgentPlanner, "prompt_version": "planner-v2"}}); err != nil {
		t.Fatalf("create eval run: %v", err)
	}
	if err := server.validatePromptEnvironmentPinGate(context.Background(), PromptIDRuntimeDeepAgentPlanner, "planner-v2", PromptEnvironmentProduction, "eval-1"); err != nil {
		t.Fatalf("matching completed eval run should satisfy production gate: %v", err)
	}
	if _, err := evals.CreateEvaluationRun(context.Background(), EvaluationRun{ID: "eval-result-bound", Status: EvaluationRunStatusCompleted, StartedAt: now, CompletedAt: &now}); err != nil {
		t.Fatalf("create result-bound eval run: %v", err)
	}
	if _, err := evals.CreateEvaluationResult(context.Background(), EvaluationResult{
		ID:            "eval-result-bound-1",
		RunID:         "eval-result-bound",
		SubjectType:   "prompt",
		SubjectID:     PromptIDRuntimeDeepAgentPlanner,
		PromptID:      PromptIDRuntimeDeepAgentPlanner,
		PromptVersion: "planner-v2",
		Status:        EvaluationResultStatusPassed,
		Score:         1,
		CreatedAt:     now,
	}); err != nil {
		t.Fatalf("create result-bound eval result: %v", err)
	}
	if err := server.validatePromptEnvironmentPinGate(context.Background(), PromptIDRuntimeDeepAgentPlanner, "planner-v2", PromptEnvironmentProduction, "eval-result-bound"); err != nil {
		t.Fatalf("result-level prompt metadata should satisfy production gate: %v", err)
	}
	if err := server.validatePromptEnvironmentPinGate(context.Background(), PromptIDRuntimeDeepAgentPlanner, "planner-v2", PromptEnvironmentStaging, ""); err != nil {
		t.Fatalf("staging should not require eval gate: %v", err)
	}
}

func TestPromptVersionEvalUsesBuiltinDeepAgentPromptGoldenSet(t *testing.T) {
	runtime := NewRuntime(RuntimeConfig{}, nil, nil, nil, nil)
	server := NewServer(runtime, HeaderAuthenticator{UserHeader: "X-User-ID"}, NoopRateLimiter{}, nil)
	server.SetAdminToken("secret")
	prompts := NewMemoryPromptStore()
	evals := NewMemoryEvaluationStore()
	server.SetPromptStore(prompts)
	server.SetEvaluationStore(evals)
	versionID := "planner-candidate"
	upsertPromptVersion(t, prompts, PromptIDRuntimeDeepAgentPlanner, versionID, "Candidate planner prompt.")

	body := `{"set_id":"deep_agent_prompt_planner","set_version":"phase3-v1","candidates":[{"case_id":"planner-research-report","output":"research pricing sources and create final artifact with cited facts"},{"case_id":"planner-code-fix","output":"diagnose issue implement fix run tests and summarize verification"}]}`
	path := "/v1/admin/ops/prompts/" + url.PathEscape(PromptIDRuntimeDeepAgentPlanner) + "/versions/" + url.PathEscape(versionID) + "/eval"
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
	req.Header.Set("X-User-ID", "admin")
	req.Header.Set("X-Admin-Token", "secret")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("eval status = %d body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Results []EvaluationResult `json:"results"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(payload.Results) != 2 {
		t.Fatalf("expected two builtin DeepAgent prompt eval results, got %#v", payload.Results)
	}
	for _, result := range payload.Results {
		if result.PromptID != PromptIDRuntimeDeepAgentPlanner || result.PromptVersion != versionID || result.PromptHash == "" {
			t.Fatalf("result missing DeepAgent prompt metadata: %#v", result)
		}
	}
}

type deepAgentPromptCaptureRunner struct {
	output   string
	prompt   string
	metadata PromptMetadata
}

func (r *deepAgentPromptCaptureRunner) Run(ctx context.Context, session *state.Session, prompt string) (engine.Result, error) {
	return r.RunGeneratedPrompt(ctx, session, prompt)
}

func (r *deepAgentPromptCaptureRunner) RunGeneratedPrompt(ctx context.Context, session *state.Session, prompt string) (engine.Result, error) {
	r.prompt = prompt
	r.metadata = promptMetadataFromContext(ctx)
	session.AddAssistantMessage(r.output)
	return engine.Result{Output: r.output, Session: session}, nil
}

func upsertPromptVersion(t *testing.T, store *MemoryPromptStore, promptID, version, content string) {
	t.Helper()
	if _, err := store.UpsertPrompt(context.Background(), PromptTemplate{ID: promptID, Name: promptID}); err != nil {
		t.Fatalf("upsert prompt %s: %v", promptID, err)
	}
	if _, err := store.CreatePromptVersion(context.Background(), PromptVersion{PromptID: promptID, Version: version, Status: PromptStatusPublished, Content: content}); err != nil {
		t.Fatalf("create prompt version %s@%s: %v", promptID, version, err)
	}
}
