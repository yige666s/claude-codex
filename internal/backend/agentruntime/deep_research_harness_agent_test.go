package agentruntime

import (
	"context"
	"strings"
	"testing"

	"claude-codex/internal/harness/engine"
	"claude-codex/internal/harness/state"
)

func TestRuntimeDeepResearchWorkerUsesConfiguredHarnessAgentRunner(t *testing.T) {
	runtime := NewRuntime(RuntimeConfig{}, nil, nil, nil, nil)
	fake := &fakeDeepResearchHarnessAgentRunner{
		result: DeepResearchWorkerResult{
			Status:     DeepAgentActionStatusSucceeded,
			Summary:    "harness result",
			Output:     "harness output",
			AgentRunID: "child-run-1",
			Metadata:   map[string]any{"from": "fake"},
		},
	}
	runtime.SetDeepResearchHarnessAgentRunner(fake)
	executor := newRuntimeDeepResearchWorkerExecutor(runtime, nil)

	result, err := executor.ExecuteWorker(context.Background(), DeepResearchWorkerInput{
		UserID:    "user-1",
		SessionID: "session-1",
		JobID:     "job-1",
		Goal:      "research a product",
		Backend:   DeepResearchWorkerBackendHarnessAgent,
		Node: DeepResearchTaskNode{
			ID:           "overview",
			Title:        "Overview",
			AllowedTools: []string{"WebSearch", "WebFetch"},
			Attempt:      1,
		},
	})
	if err != nil {
		t.Fatalf("ExecuteWorker returned error: %v", err)
	}
	if fake.calls != 1 {
		t.Fatalf("expected harness runner to be called once, got %d", fake.calls)
	}
	if result.AgentRunID != "child-run-1" {
		t.Fatalf("expected harness agent run id, got %q", result.AgentRunID)
	}
	if result.Metadata["worker_backend"] != DeepResearchWorkerBackendHarnessAgent {
		t.Fatalf("expected harness backend metadata, got %#v", result.Metadata)
	}
	if _, ok := result.Metadata["worker_backend_fallback"]; ok {
		t.Fatalf("did not expect inline fallback metadata: %#v", result.Metadata)
	}
}

func TestEngineDeepResearchHarnessAgentRunnerUsesEngineFactoryScope(t *testing.T) {
	root := t.TempDir()
	var captured Scope
	runtime := NewRuntime(RuntimeConfig{DefaultWorkingDir: root}, nil, nil, nil, func(scope Scope) Runner {
		captured = scope
		return fakeDeepResearchEngineRunner{
			output: `{"summary":"found pricing","output":"Official pricing: https://example.com/pricing","sources":[{"url":"https://example.com/pricing","title":"Pricing","provider":"WebFetch"}]}`,
		}
	})
	runner := NewEngineDeepResearchHarnessAgentRunner(runtime)

	result, err := runner.RunDeepResearchAgent(context.Background(), DeepResearchWorkerInput{
		UserID:           "user-1",
		SessionID:        "parent-session",
		JobID:            "job-1",
		Goal:             "research a product",
		ConnectorContext: []string{"github connected"},
		Node: DeepResearchTaskNode{
			ID:           "overview",
			Title:        "Overview",
			WorkerRole:   "researcher",
			AllowedTools: []string{"WebSearch", "WebFetch", "repo_search"},
			Attempt:      2,
		},
	})
	if err != nil {
		t.Fatalf("RunDeepResearchAgent returned error: %v", err)
	}
	if result.Status != DeepAgentActionStatusSucceeded {
		t.Fatalf("expected succeeded status, got %q", result.Status)
	}
	if result.AgentRunID == "" || !strings.HasPrefix(result.AgentRunID, "drw-overview-a2-") {
		t.Fatalf("expected generated child agent run id, got %q", result.AgentRunID)
	}
	if result.Summary != "found pricing" {
		t.Fatalf("expected parsed JSON summary, got %q", result.Summary)
	}
	if len(result.Sources) != 1 || result.Sources[0].URL != "https://example.com/pricing" {
		t.Fatalf("expected deduped parsed source, got %#v", result.Sources)
	}
	if result.Sources[0].SourceKind != "unverified_model_report" {
		t.Fatalf("model-reported source without tool evidence must stay unverified: %#v", result.Sources[0])
	}
	if captured.UserID != "user-1" || captured.SessionID != "parent-session" {
		t.Fatalf("unexpected scope identity: %#v", captured)
	}
	if captured.WorkingDir != root {
		t.Fatalf("expected working dir %q, got %q", root, captured.WorkingDir)
	}
	if !captured.InternalToolScope {
		t.Fatalf("expected internal tool scope for deep research child runner")
	}
	if !containsString(captured.AllowedTools, "WebSearch") || !containsString(captured.AllowedTools, "WebFetch") || !containsString(captured.AllowedTools, "Grep") {
		t.Fatalf("expected mapped allowed tools, got %#v", captured.AllowedTools)
	}
	if len(captured.ConnectorContext) != 1 || captured.ConnectorContext[0] != "github connected" {
		t.Fatalf("expected connector context to pass through, got %#v", captured.ConnectorContext)
	}
	if result.Metadata["runner"] != "engine_factory" {
		t.Fatalf("expected engine factory metadata, got %#v", result.Metadata)
	}
}

func TestDeepResearchWorkerResultTrustsOnlyToolBackedHarnessSources(t *testing.T) {
	session := state.NewSession(t.TempDir())
	session.AddAssistantMessageWithTools("", []state.ToolCall{{ID: "call-web", Name: "WebFetch"}})
	session.AddToolResult("call-web", "WebFetch", nil, "Fetched official pricing from https://example.com/pricing")
	output := `{"summary":"found pricing","sources":[{"url":"https://example.com/pricing","title":"Pricing","provider":"WebFetch"},{"url":"https://attacker.example/fabricated","title":"Fabricated","provider":"WebFetch"}]}`
	session.AddAssistantMessage(output)

	result := deepResearchWorkerResultFromHarnessOutput(DeepResearchTaskNode{ID: "pricing"}, engine.Result{Output: output, Session: session}, nil)
	if len(result.ToolCalls) != 1 || result.ToolCalls[0].Name != "WebFetch" || result.ToolCalls[0].Status != "result" {
		t.Fatalf("actual harness tool calls were not captured: %#v", result.ToolCalls)
	}
	byURL := map[string]DeepAgentSourceRef{}
	for _, source := range result.Sources {
		byURL[source.URL] = source
	}
	if byURL["https://example.com/pricing"].SourceKind != "tool_verified" {
		t.Fatalf("tool-backed source was not verified: %#v", byURL)
	}
	if byURL["https://attacker.example/fabricated"].SourceKind != "unverified_model_report" {
		t.Fatalf("fabricated source was trusted: %#v", byURL)
	}
	trusted := deepResearchTrustedSources(result)
	if len(trusted) != 1 || trusted[0].URL != "https://example.com/pricing" {
		t.Fatalf("trusted sources = %#v, want only tool-backed URL", trusted)
	}
}

func TestDeepResearchWorkerResultFromHarnessOutputMarksBareTextURLsUnverified(t *testing.T) {
	result := deepResearchWorkerResultFromHarnessOutput(DeepResearchTaskNode{
		ID:    "overview",
		Title: "Overview",
	}, engine.Result{
		Output: "Official pricing might be at https://example.com/pricing",
	}, nil)

	if len(result.Sources) != 1 {
		t.Fatalf("source count = %d, want 1", len(result.Sources))
	}
	if got := result.Sources[0].Provider; got != "model_text" {
		t.Fatalf("source provider = %q, want model_text", got)
	}
	if got := result.Sources[0].SourceKind; got != "unverified_model_text" {
		t.Fatalf("source kind = %q, want unverified_model_text", got)
	}
}

type fakeDeepResearchHarnessAgentRunner struct {
	calls  int
	result DeepResearchWorkerResult
	err    error
}

func (r *fakeDeepResearchHarnessAgentRunner) RunDeepResearchAgent(context.Context, DeepResearchWorkerInput) (DeepResearchWorkerResult, error) {
	r.calls++
	return r.result, r.err
}

type fakeDeepResearchEngineRunner struct {
	output string
}

func (r fakeDeepResearchEngineRunner) Run(_ context.Context, session *state.Session, prompt string) (engine.Result, error) {
	session.AddUserMessage(prompt)
	session.AddAssistantMessage(r.output)
	return engine.Result{Output: r.output, Session: session}, nil
}

func (r fakeDeepResearchEngineRunner) RunGeneratedPrompt(ctx context.Context, session *state.Session, prompt string) (engine.Result, error) {
	return r.Run(ctx, session, prompt)
}
