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
	if captured.UserID != "user-1" || captured.SessionID != "parent-session" {
		t.Fatalf("unexpected scope identity: %#v", captured)
	}
	if captured.WorkingDir != root {
		t.Fatalf("expected working dir %q, got %q", root, captured.WorkingDir)
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
