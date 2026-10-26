package agentruntime

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"claude-codex/internal/harness/engine"
	"claude-codex/internal/harness/state"
)

type routeDecisionRunner struct {
	output string
	err    error
	prompt string
}

func (r *routeDecisionRunner) Run(_ context.Context, session *state.Session, _ string) (engine.Result, error) {
	return engine.Result{Session: session}, nil
}
func (r *routeDecisionRunner) RunGeneratedPrompt(_ context.Context, session *state.Session, prompt string) (engine.Result, error) {
	r.prompt = prompt
	return engine.Result{Output: r.output, Session: session}, r.err
}

func TestModelDecidesMultiAgentRouteWithNoTools(t *testing.T) {
	root := t.TempDir()
	model := &routeDecisionRunner{output: `{"mode":"multi_agent","reason":"independent investigations","independent_tasks":["Inspect API A","Inspect API B"]}`}
	var scope Scope
	runtime := NewRuntimeWithContextEngineFactory(RuntimeConfig{DefaultWorkingDir: root, LLMGovernanceProvider: func() LLMGovernanceConfig { return LLMGovernanceConfig{MultiAgentAutoRouteEnabled: true} }}, NewFileSessionStore(root), nil, nil, func(_ context.Context, selected Scope) (Runner, error) {
		scope = selected
		return model, nil
	})
	runtime.SetJobStore(NewMemoryJobStore())
	session, err := runtime.CreateSession(context.Background(), "alice", "")
	if err != nil {
		t.Fatal(err)
	}
	session.AddUserMessage("The two services use different versions")
	if err := runtime.sessions.Save(context.Background(), "alice", session); err != nil {
		t.Fatal(err)
	}
	decision := runtime.decideMultiAgentRoute(context.Background(), ChatRequest{UserID: "alice", SessionID: session.ID, Content: "Compare their API behavior and migration risks"})
	if !decision.RunAsJob || decision.JobType != JobTypeMultiAgent {
		t.Fatalf("decision = %#v", decision)
	}
	if !scope.InternalToolScope || len(scope.AllowedTools) != 1 || scope.AllowedTools[0] != "__no_tools_allowed__" {
		t.Fatalf("classifier tools = %#v", scope)
	}
	if !strings.Contains(model.prompt, "The two services use different versions") {
		t.Fatal("classifier did not receive conversation context")
	}
}

func TestModelRouteFailsClosed(t *testing.T) {
	for _, tc := range []struct {
		name, output string
		err          error
	}{
		{name: "single", output: `{"mode":"single_agent","independent_tasks":[]}`},
		{name: "invalid", output: `not JSON`},
		{name: "one task", output: `{"mode":"multi_agent","independent_tasks":["only one"]}`},
		{name: "duplicate", output: `{"mode":"multi_agent","independent_tasks":["same","same"]}`},
		{name: "provider error", err: errors.New("unavailable")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			model := &routeDecisionRunner{output: tc.output, err: tc.err}
			runtime := NewRuntime(RuntimeConfig{DefaultWorkingDir: root, TurnTimeout: time.Minute, LLMGovernanceProvider: func() LLMGovernanceConfig { return LLMGovernanceConfig{MultiAgentAutoRouteEnabled: true} }}, NewFileSessionStore(root), nil, nil, func(Scope) Runner { return model })
			runtime.SetJobStore(NewMemoryJobStore())
			session, err := runtime.CreateSession(context.Background(), "alice", "")
			if err != nil {
				t.Fatal(err)
			}
			decision := runtime.decideMultiAgentRoute(context.Background(), ChatRequest{UserID: "alice", SessionID: session.ID, Content: "Compare these services"})
			if decision.RunAsJob {
				t.Fatalf("unsafe decision = %#v", decision)
			}
			model.prompt = ""
			decision = runtime.decideMultiAgentRoute(context.Background(), ChatRequest{UserID: "alice", SessionID: session.ID, Content: "不要使用多智能体，自己完成"})
			if decision.RunAsJob || model.prompt != "" {
				t.Fatal("opt-out must not call classifier")
			}
		})
	}
}

func TestChatHandsModelSelectedMultiAgentToDurableJob(t *testing.T) {
	root := t.TempDir()
	model := &routeDecisionRunner{output: `{"mode":"multi_agent","reason":"parallel sources","independent_tasks":["Source A","Source B"]}`}
	runtime := NewRuntime(RuntimeConfig{DefaultWorkingDir: root, LLMGovernanceProvider: func() LLMGovernanceConfig { return LLMGovernanceConfig{MultiAgentAutoRouteEnabled: true} }}, NewFileSessionStore(root), nil, nil, func(Scope) Runner { return model })
	runtime.SetJobStore(NewMemoryJobStore())
	queue := &captureJobQueue{}
	runtime.SetJobQueue(queue)
	session, err := runtime.CreateSession(context.Background(), "alice", "")
	if err != nil {
		t.Fatal(err)
	}
	sink := &collectSink{}
	if err := runtime.Chat(context.Background(), ChatRequest{UserID: "alice", SessionID: session.ID, Content: "Compare the two independent products"}, sink); err != nil {
		t.Fatal(err)
	}
	if len(queue.items) != 1 || !sink.hasEvent("job_handoff") {
		t.Fatalf("job queue = %#v, events = %#v", queue.items, sink.events)
	}
	job, err := runtime.GetJob(context.Background(), "alice", queue.items[0].JobID)
	if err != nil || job.Type != JobTypeMultiAgent {
		t.Fatalf("job = %#v, err = %v", job, err)
	}
}

func TestMultiAgentRoutePrefersDBGovernanceSetting(t *testing.T) {
	runtime := NewRuntime(RuntimeConfig{LLMGovernanceProvider: func() LLMGovernanceConfig {
		return LLMGovernanceConfig{MultiAgentAutoRouteEnabled: false}
	}}, nil, nil, nil, nil)
	if runtime.multiAgentAutoRouteEnabled() {
		t.Fatal("DB false should disable automatic routing")
	}
	runtime.config.LLMGovernanceProvider = func() LLMGovernanceConfig {
		return LLMGovernanceConfig{MultiAgentAutoRouteEnabled: true}
	}
	if !runtime.multiAgentAutoRouteEnabled() {
		t.Fatal("DB setting was not applied")
	}
}

func TestModelRouteUsesPublishedAdminPrompt(t *testing.T) {
	root := t.TempDir()
	model := &routeDecisionRunner{output: `{"mode":"multi_agent","independent_tasks":["Inspect API","Inspect tests"]}`}
	runtime := NewRuntime(RuntimeConfig{DefaultWorkingDir: root, LLMGovernanceProvider: func() LLMGovernanceConfig {
		return LLMGovernanceConfig{MultiAgentAutoRouteEnabled: true}
	}}, NewFileSessionStore(root), nil, nil, func(Scope) Runner { return model })
	runtime.SetJobStore(NewMemoryJobStore())
	store := NewMemoryPromptStore()
	if _, err := store.UpsertPrompt(context.Background(), PromptTemplate{ID: PromptIDRuntimeMultiAgentRoute, Name: "Router"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreatePromptVersion(context.Background(), PromptVersion{PromptID: PromptIDRuntimeMultiAgentRoute, Version: "admin-v2", Status: PromptStatusPublished, Content: "Admin classification of {{user_request}} using {{recent_conversation}}"}); err != nil {
		t.Fatal(err)
	}
	runtime.SetPromptStore(store)
	session, err := runtime.CreateSession(context.Background(), "alice", "")
	if err != nil {
		t.Fatal(err)
	}
	session.AddUserMessage("previous context")
	if err := runtime.sessions.Save(context.Background(), "alice", session); err != nil {
		t.Fatal(err)
	}
	decision := runtime.decideMultiAgentRoute(context.Background(), ChatRequest{UserID: "alice", SessionID: session.ID, Content: "Compare APIs"})
	if !decision.RunAsJob || decision.JobType != JobTypeMultiAgent {
		t.Fatalf("decision=%#v", decision)
	}
	if !strings.Contains(model.prompt, "Admin classification of Compare APIs") || !strings.Contains(model.prompt, "previous context") || strings.Contains(model.prompt, "You are a routing classifier") {
		t.Fatalf("published route prompt not used: %s", model.prompt)
	}
}

func TestModelRouteFirstTurnUsesManagedPromptWithoutHistory(t *testing.T) {
	root := t.TempDir()
	model := &routeDecisionRunner{output: `{"mode":"multi_agent","independent_tasks":["Review SQLite","Review PostgreSQL"]}`}
	runtime := NewRuntime(RuntimeConfig{DefaultWorkingDir: root, LLMGovernanceProvider: func() LLMGovernanceConfig {
		return LLMGovernanceConfig{MultiAgentAutoRouteEnabled: true}
	}}, NewFileSessionStore(root), nil, nil, func(Scope) Runner { return model })
	runtime.SetJobStore(NewMemoryJobStore())
	session, err := runtime.CreateSession(context.Background(), "alice", "")
	if err != nil {
		t.Fatal(err)
	}
	result := runtime.decideMultiAgentRoute(context.Background(), ChatRequest{UserID: "alice", SessionID: session.ID, Content: "Compare SQLite and PostgreSQL deployments"})
	if !result.RunAsJob || !strings.Contains(model.prompt, "Recent conversation:\n(none)") {
		t.Fatalf("first-turn route=%#v prompt=%q", result, model.prompt)
	}
}
