package agentruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"claude-codex/internal/harness/plannerapi"
	"claude-codex/internal/harness/state"
	toolkit "claude-codex/internal/harness/tools"
)

func TestMemoryPromptStoreLifecycle(t *testing.T) {
	store := NewMemoryPromptStore()
	ctx := context.Background()
	if _, err := store.UpsertPrompt(ctx, PromptTemplate{ID: "live_setup", Name: "Live Setup", Scope: "live"}); err != nil {
		t.Fatalf("upsert prompt: %v", err)
	}
	v1, err := store.CreatePromptVersion(ctx, PromptVersion{PromptID: "live_setup", Version: "v1", Status: PromptStatusPublished, Content: "v1 {{content}}"})
	if err != nil {
		t.Fatalf("create v1: %v", err)
	}
	v2, err := store.CreatePromptVersion(ctx, PromptVersion{PromptID: "live_setup", Version: "v2", Status: PromptStatusReviewPending, Content: "v2 {{content}}", BaseVersion: "v1"})
	if err != nil {
		t.Fatalf("create v2: %v", err)
	}
	if v1.ContentHash == v2.ContentHash {
		t.Fatalf("expected different hashes for different content")
	}
	if _, err := store.PublishPromptVersion(ctx, "live_setup", "v2", "admin", "ship v2"); err != nil {
		t.Fatalf("publish v2: %v", err)
	}
	published, err := store.GetPublishedPromptVersion(ctx, "live_setup")
	if err != nil {
		t.Fatalf("published: %v", err)
	}
	if published.Version != "v2" || published.Status != PromptStatusPublished {
		t.Fatalf("unexpected published version: %#v", published)
	}
	archived, err := store.GetPromptVersion(ctx, "live_setup", "v1")
	if err != nil {
		t.Fatalf("get v1: %v", err)
	}
	if archived.Status != PromptStatusArchived {
		t.Fatalf("expected v1 archived after publish, got %#v", archived)
	}
	rendered, err := RenderPrompt(PromptResolution{PromptID: "live_setup", Version: published}, map[string]any{"content": "hello"})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if rendered.Content != "v2 hello" || rendered.PromptHash != published.ContentHash {
		t.Fatalf("unexpected render: %#v", rendered)
	}
}

func TestAdminPromptAPI(t *testing.T) {
	runtime := NewRuntime(RuntimeConfig{}, nil, nil, nil, nil)
	server := NewServer(runtime, HeaderAuthenticator{UserHeader: "X-User-ID"}, NoopRateLimiter{}, nil)
	server.SetAdminToken("secret")
	server.SetPromptStore(NewMemoryPromptStore())

	admin := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		req.Header.Set("X-User-ID", "admin")
		req.Header.Set("X-Admin-Token", "secret")
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		rec := httptest.NewRecorder()
		server.ServeHTTP(rec, req)
		return rec
	}

	create := admin(http.MethodPost, "/v1/admin/ops/prompts", `{"prompt":{"id":"live_setup","name":"Live Setup","scope":"live"},"version":{"version":"v1","status":"published","content":"Hello {{content}}","variables_schema":{"required":["content"]}}}`)
	if create.Code != http.StatusCreated {
		t.Fatalf("create status = %d body=%s", create.Code, create.Body.String())
	}
	version := admin(http.MethodPost, "/v1/admin/ops/prompts/live_setup/versions", `{"version":"v2","status":"review_pending","content":"Hi {{content}}","base_version":"v1"}`)
	if version.Code != http.StatusCreated {
		t.Fatalf("version status = %d body=%s", version.Code, version.Body.String())
	}
	publish := admin(http.MethodPost, "/v1/admin/ops/prompts/live_setup/publish", `{"version":"v2","changelog":"release"}`)
	if publish.Code != http.StatusOK || !strings.Contains(publish.Body.String(), `"status":"published"`) {
		t.Fatalf("publish status = %d body=%s", publish.Code, publish.Body.String())
	}
	diff := admin(http.MethodGet, "/v1/admin/ops/prompts/live_setup/versions/diff?from_version=v1&to_version=current", "")
	if diff.Code != http.StatusOK || !strings.Contains(diff.Body.String(), `"field":"content"`) {
		t.Fatalf("diff status = %d body=%s", diff.Code, diff.Body.String())
	}
	render := admin(http.MethodPost, "/v1/admin/ops/prompts/live_setup/versions/v2/render-preview", `{"variables":{"content":"there"}}`)
	if render.Code != http.StatusOK || !strings.Contains(render.Body.String(), `"content":"Hi there"`) {
		t.Fatalf("render status = %d body=%s", render.Code, render.Body.String())
	}
}

func TestRuntimeLiveSystemInstructionUsesPromptRegistry(t *testing.T) {
	session := state.NewSession(t.TempDir())
	session.ID = "session-live"
	store := NewFileSessionStore(t.TempDir())
	if err := store.Save(context.Background(), "alice", session); err != nil {
		t.Fatalf("save session: %v", err)
	}
	prompts := NewMemoryPromptStore()
	if _, err := prompts.UpsertPrompt(context.Background(), PromptTemplate{ID: PromptIDLiveSetup, Name: "Live Setup"}); err != nil {
		t.Fatalf("upsert prompt: %v", err)
	}
	if _, err := prompts.CreatePromptVersion(context.Background(), PromptVersion{PromptID: PromptIDLiveSetup, Version: "v-live", Status: PromptStatusPublished, Content: "PREFIX\n{{content}}"}); err != nil {
		t.Fatalf("create prompt version: %v", err)
	}
	runtime := NewRuntime(RuntimeConfig{}, store, nil, nil, nil)
	runtime.SetPromptStore(prompts)
	instruction := runtime.LiveSystemInstruction(context.Background(), "alice", session.ID)
	if !strings.HasPrefix(instruction, "PREFIX\n") || !strings.Contains(instruction, "Session history policy") {
		t.Fatalf("live instruction did not use prompt registry: %s", instruction)
	}
}

func TestGovernedPlannerRecordsPromptMetadata(t *testing.T) {
	usage := NewMemoryLLMUsageStore()
	planner, err := NewGovernedPlanner([]LLMBackend{{
		Name:     "test",
		Provider: "openai",
		Model:    "model",
		Planner:  staticPlanner{text: "ok"},
	}}, usage, LLMGovernanceConfig{MaxAttempts: 1})
	if err != nil {
		t.Fatalf("planner: %v", err)
	}
	ctx := WithLLMScope(context.Background(), LLMScope{UserID: "alice", SessionID: "s1"})
	ctx = WithPromptMetadata(ctx, PromptMetadata{PromptID: "memory_extract", PromptVersion: "v2", PromptHash: "hash"})
	if _, err := planner.Next(ctx, state.NewSession(""), nil); err != nil {
		t.Fatalf("next: %v", err)
	}
	summary, err := usage.SummarizeLLMUsage(context.Background(), LLMUsageAdminFilter{UserID: "alice", Since: time.Now().Add(-time.Hour), Limit: 10})
	if err != nil {
		t.Fatalf("usage summary: %v", err)
	}
	if len(summary.Recent) != 1 || summary.Recent[0].PromptID != "memory_extract" || summary.Recent[0].PromptVersion != "v2" {
		t.Fatalf("usage missing prompt metadata: %#v", summary.Recent)
	}
}

func TestPromptVersionGoldenEvalAddsPromptDimensions(t *testing.T) {
	runtime := NewRuntime(RuntimeConfig{}, nil, nil, nil, nil)
	server := NewServer(runtime, HeaderAuthenticator{UserHeader: "X-User-ID"}, NoopRateLimiter{}, nil)
	server.SetAdminToken("secret")
	prompts := NewMemoryPromptStore()
	evals := NewMemoryEvaluationStore()
	server.SetPromptStore(prompts)
	server.SetEvaluationStore(evals)
	ctx := context.Background()
	if _, err := prompts.UpsertPrompt(ctx, PromptTemplate{ID: "rag_answer", Name: "RAG Answer"}); err != nil {
		t.Fatalf("upsert prompt: %v", err)
	}
	version, err := prompts.CreatePromptVersion(ctx, PromptVersion{PromptID: "rag_answer", Version: "v1", Status: PromptStatusPublished, Content: "Answer with evidence"})
	if err != nil {
		t.Fatalf("create prompt version: %v", err)
	}
	if _, err := evals.UpsertGoldenSet(ctx, GoldenSet{
		ID:      "support-rag",
		Version: "v1",
		Name:    "Support RAG",
		Cases: []GoldenCase{{
			ID:            "case-1",
			Query:         "How to improve retrieval?",
			ExpectedFacts: []string{"hybrid"},
		}},
	}); err != nil {
		t.Fatalf("upsert golden set: %v", err)
	}
	body := `{"set_id":"support-rag","set_version":"v1","candidates":[{"case_id":"case-1","output":"Use hybrid retrieval."}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/admin/ops/prompts/rag_answer/versions/v1/eval", bytes.NewBufferString(body))
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
	if len(payload.Results) != 1 || payload.Results[0].PromptID != "rag_answer" || payload.Results[0].PromptVersion != "v1" || payload.Results[0].PromptHash != version.ContentHash {
		t.Fatalf("result missing prompt fields: %#v", payload.Results)
	}
	filtered, err := evals.ListEvaluationResults(ctx, EvaluationResultFilter{PromptID: "rag_answer", PromptVersion: "v1"})
	if err != nil {
		t.Fatalf("filter results: %v", err)
	}
	if len(filtered) != 1 {
		t.Fatalf("expected one filtered result, got %#v", filtered)
	}
}

func TestPromptExperimentAPIAndResolverAssignment(t *testing.T) {
	runtime := NewRuntime(RuntimeConfig{}, nil, nil, nil, nil)
	server := NewServer(runtime, HeaderAuthenticator{UserHeader: "X-User-ID"}, NoopRateLimiter{}, nil)
	server.SetAdminToken("secret")
	prompts := NewMemoryPromptStore()
	server.SetPromptStore(prompts)
	ctx := context.Background()
	if _, err := prompts.UpsertPrompt(ctx, PromptTemplate{ID: PromptIDLiveSetup, Name: "Live Setup"}); err != nil {
		t.Fatalf("upsert prompt: %v", err)
	}
	if _, err := prompts.CreatePromptVersion(ctx, PromptVersion{PromptID: PromptIDLiveSetup, Version: "control", Status: PromptStatusPublished, Content: "control {{content}}"}); err != nil {
		t.Fatalf("create control: %v", err)
	}
	if _, err := prompts.CreatePromptVersion(ctx, PromptVersion{PromptID: PromptIDLiveSetup, Version: "candidate", Status: PromptStatusReviewPending, Content: "candidate {{content}}"}); err != nil {
		t.Fatalf("create candidate: %v", err)
	}
	body := `{"experiment":{"id":"exp-live","name":"Live AB","prompt_id":"live_setup","status":"running","traffic_scope":"user"},"variants":[{"variant_id":"control","prompt_version":"control","weight":0},{"variant_id":"candidate","prompt_version":"candidate","weight":100}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/admin/ops/prompt-experiments", bytes.NewBufferString(body))
	req.Header.Set("X-User-ID", "admin")
	req.Header.Set("X-Admin-Token", "secret")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("experiment create status = %d body=%s", rec.Code, rec.Body.String())
	}
	resolution, err := NewPromptResolver(prompts, nil).Resolve(ctx, PromptResolveRequest{PromptID: PromptIDLiveSetup, UserID: "alice", SessionID: "s1"})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if resolution.Version.Version != "candidate" || resolution.Assignment == nil || resolution.Assignment.ExperimentID != "exp-live" || resolution.Assignment.VariantID != "candidate" {
		t.Fatalf("unexpected assignment: %#v", resolution)
	}
	rendered, err := RenderPrompt(resolution, map[string]any{"content": "hello"})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	meta := PromptMetadataFromRender(rendered)
	if meta.ExperimentID != "exp-live" || meta.VariantID != "candidate" {
		t.Fatalf("missing experiment metadata: %#v", meta)
	}
}

func TestPromptOptimizationWorkflowCreatesReviewVersion(t *testing.T) {
	runtime := NewRuntime(RuntimeConfig{}, nil, nil, nil, nil)
	runtime.SetWorkflowStore(NewMemoryWorkflowStore())
	server := NewServer(runtime, HeaderAuthenticator{UserHeader: "X-User-ID"}, NoopRateLimiter{}, nil)
	server.SetAdminToken("secret")
	prompts := NewMemoryPromptStore()
	evals := NewMemoryEvaluationStore()
	server.SetPromptStore(prompts)
	server.SetEvaluationStore(evals)
	ctx := context.Background()
	if _, err := prompts.UpsertPrompt(ctx, PromptTemplate{ID: "rag_answer", Name: "RAG Answer"}); err != nil {
		t.Fatalf("upsert prompt: %v", err)
	}
	if _, err := prompts.CreatePromptVersion(ctx, PromptVersion{PromptID: "rag_answer", Version: "v1", Status: PromptStatusPublished, Content: "Answer with evidence."}); err != nil {
		t.Fatalf("create prompt version: %v", err)
	}
	run, err := evals.CreateEvaluationRun(ctx, EvaluationRun{Name: "baseline", Status: EvaluationRunStatusCompleted, Trigger: "test"})
	if err != nil {
		t.Fatalf("create eval run: %v", err)
	}
	if _, err := evals.CreateEvaluationResult(ctx, EvaluationResult{
		RunID:         run.ID,
		SubjectType:   EvaluationSubjectJob,
		SubjectID:     "job-1",
		PromptID:      "rag_answer",
		PromptVersion: "v1",
		Status:        EvaluationResultStatusFailed,
		Score:         0.2,
		Findings:      []EvaluationFinding{{Severity: "error", Code: "faithfulness", Message: "missing evidence"}},
	}); err != nil {
		t.Fatalf("create eval result: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/admin/ops/prompts/rag_answer/optimize", bytes.NewBufferString(`{"baseline_version":"v1","max_badcases":5}`))
	req.Header.Set("X-User-ID", "admin")
	req.Header.Set("X-Admin-Token", "secret")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("optimize status = %d body=%s", rec.Code, rec.Body.String())
	}
	versions, err := prompts.ListPromptVersions(ctx, "rag_answer")
	if err != nil {
		t.Fatalf("list versions: %v", err)
	}
	found := false
	for _, version := range versions {
		if version.BaseVersion == "v1" && version.Status == PromptStatusReviewPending && strings.Contains(version.Content, "faithfulness=1") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("review candidate version not found: %#v", versions)
	}
}

type staticPlanner struct {
	text string
}

func (p staticPlanner) Next(context.Context, *state.Session, []toolkit.Descriptor) (plannerapi.Plan, error) {
	return plannerapi.Plan{AssistantText: p.text}, nil
}
