package agentruntime

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"claude-codex/internal/harness/engine"
	"claude-codex/internal/harness/state"
)

func TestRuntimeDeepResearchOrchestratorCreatesTaskSpecificDAG(t *testing.T) {
	runner := &sequenceDeepResearchRunner{outputs: []string{validDeepResearchPlanJSON("CustomResearch")}}
	runtime := newDeepResearchTestRuntime(t, runner)
	orchestrator := NewRuntimeDeepResearchOrchestrator(runtime)

	plan, err := orchestrator.Plan(context.Background(), DeepAgentTaskRequest{
		UserID:    "alice",
		SessionID: "session-1",
		Goal:      "调研 Acme 的企业安全能力并与 Beta 对比",
		State: map[string]any{
			deepAgentLoadedContextKey: DeepAgentLoadedContext{
				MemorySummary: "The user prioritizes enterprise access controls.",
				ToolCatalog: []DeepAgentToolRef{{
					Name:        "CustomResearch",
					Description: "Search the approved enterprise research index.",
					Permission:  "read",
				}},
			},
		},
	}, DeepResearchRuntimeConfig{
		MaxWorkers:     6,
		MaxConcurrency: 3,
		WorkerTimeout:  time.Minute,
		MaxRetries:     1,
		RequireSources: true,
	})
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if len(plan.Nodes) != 3 {
		t.Fatalf("node count = %d, want 3", len(plan.Nodes))
	}
	if got := plan.Nodes[2].DependsOn; len(got) != 2 || got[0] != "security" || got[1] != "comparison" {
		t.Fatalf("synthesis dependencies = %#v", got)
	}
	if got := plan.Nodes[0].AllowedTools; len(got) != 1 || got[0] != "CustomResearch" {
		t.Fatalf("allowed tools = %#v, want canonical CustomResearch", got)
	}
	if got := deepAgentWorkflowString(plan.Nodes[0].Metadata, "orchestrator"); got != deepResearchLLMOrchestratorVersion {
		t.Fatalf("orchestrator metadata = %q", got)
	}
	if got := deepAgentWorkflowString(plan.Nodes[0].Metadata, "prompt_id"); got != PromptIDRuntimeDeepResearchOrchestrator {
		t.Fatalf("prompt metadata id = %q", got)
	}
	prompts := runner.recordedPrompts()
	if len(prompts) != 1 || !strings.Contains(prompts[0], "The user prioritizes enterprise access controls") || !strings.Contains(prompts[0], "CustomResearch") {
		t.Fatalf("planner prompt missing loaded context or tool catalog: %#v", prompts)
	}
}

func TestRuntimeDeepResearchOrchestratorRepairsInvalidPlan(t *testing.T) {
	runner := &sequenceDeepResearchRunner{outputs: []string{
		`{"goal":"research","nodes":[]}`,
		validDeepResearchPlanJSON("WebSearch"),
	}}
	orchestrator := NewRuntimeDeepResearchOrchestrator(newDeepResearchTestRuntime(t, runner))

	plan, err := orchestrator.Plan(context.Background(), DeepAgentTaskRequest{Goal: "research"}, DeepResearchRuntimeConfig{
		MaxWorkers:     5,
		MaxConcurrency: 2,
		WorkerTimeout:  time.Minute,
	})
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if len(plan.Nodes) != 3 || deepAgentWorkflowString(plan.Nodes[0].Metadata, "orchestrator") != deepResearchLLMOrchestratorVersion {
		t.Fatalf("repaired plan was not used: %#v", plan)
	}
	if got := len(runner.recordedPrompts()); got != 2 {
		t.Fatalf("model calls = %d, want initial call plus one repair", got)
	}
}

func TestRuntimeDeepResearchOrchestratorFallsBackAfterModelFailure(t *testing.T) {
	runner := &sequenceDeepResearchRunner{errs: []error{errors.New("provider unavailable")}}
	orchestrator := NewRuntimeDeepResearchOrchestrator(newDeepResearchTestRuntime(t, runner))

	plan, err := orchestrator.Plan(context.Background(), DeepAgentTaskRequest{Goal: "调研 Acme 产品"}, DeepResearchRuntimeConfig{
		MaxWorkers:     4,
		MaxConcurrency: 2,
		WorkerTimeout:  time.Minute,
	})
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if len(plan.Nodes) == 0 {
		t.Fatal("rule fallback returned no nodes")
	}
	if got, _ := plan.Nodes[0].Metadata["orchestrator_fallback"].(bool); !got {
		t.Fatalf("fallback metadata missing: %#v", plan.Nodes[0].Metadata)
	}
	if got := deepAgentWorkflowString(plan.Nodes[0].Metadata, "orchestrator"); got != "rule_v1" {
		t.Fatalf("fallback orchestrator = %q, want rule_v1", got)
	}
}

func TestRuntimeDeepResearchOrchestratorRejectsUnavailableTools(t *testing.T) {
	invalid := validDeepResearchPlanJSON("Bash")
	runner := &sequenceDeepResearchRunner{outputs: []string{invalid, invalid}}
	orchestrator := NewRuntimeDeepResearchOrchestrator(newDeepResearchTestRuntime(t, runner))

	plan, err := orchestrator.Plan(context.Background(), DeepAgentTaskRequest{Goal: "调研 Acme 产品"}, DeepResearchRuntimeConfig{
		MaxWorkers:     4,
		MaxConcurrency: 2,
		WorkerTimeout:  time.Minute,
	})
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if got, _ := plan.Nodes[0].Metadata["orchestrator_fallback"].(bool); !got {
		t.Fatalf("unavailable tool should force rule fallback: %#v", plan.Nodes[0].Metadata)
	}
}

func TestParseRuntimeDeepResearchPlanEnforcesGraphAndRuntimeLimits(t *testing.T) {
	allowed, _ := deepResearchOrchestratorAllowedTools(nil)
	valid := validDeepResearchPlanJSON("WebSearch")
	tests := []struct {
		name   string
		output string
		cfg    DeepResearchRuntimeConfig
		want   string
	}{
		{
			name:   "worker budget",
			output: valid,
			cfg:    DeepResearchRuntimeConfig{MaxWorkers: 2, MaxConcurrency: 2, WorkerTimeout: time.Minute},
			want:   "maximum is 2",
		},
		{
			name:   "concurrency budget",
			output: valid,
			cfg:    DeepResearchRuntimeConfig{MaxWorkers: 4, MaxConcurrency: 1, WorkerTimeout: time.Minute},
			want:   "between 1 and 1",
		},
		{
			name:   "duplicate node",
			output: strings.Replace(valid, `"id":"comparison"`, `"id":"security"`, 1),
			cfg:    DeepResearchRuntimeConfig{MaxWorkers: 4, MaxConcurrency: 2, WorkerTimeout: time.Minute},
			want:   "duplicate",
		},
		{
			name:   "cycle",
			output: strings.Replace(valid, `"depends_on":[]`, `"depends_on":["synthesis"]`, 1),
			cfg:    DeepResearchRuntimeConfig{MaxWorkers: 4, MaxConcurrency: 2, WorkerTimeout: time.Minute},
			want:   "cycle",
		},
		{
			name:   "runtime owned retry field",
			output: strings.Replace(valid, `"required":true`, `"required":true,"max_attempts":99`, 1),
			cfg:    DeepResearchRuntimeConfig{MaxWorkers: 4, MaxConcurrency: 2, WorkerTimeout: time.Minute},
			want:   "additional property",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseRuntimeDeepResearchPlan(tc.output, "goal", tc.cfg, allowed)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("parseRuntimeDeepResearchPlan() error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestRuntimeExecuteDeepResearchTaskUsesLLMOrchestratorAndLoadedContext(t *testing.T) {
	runner := &sequenceDeepResearchRunner{outputs: []string{
		`{
			"goal":"research Acme",
			"max_concurrency":1,
			"nodes":[{
				"id":"research",
				"title":"Research Acme",
				"description":"Collect current Acme facts with traceable sources.",
				"depends_on":[],
				"worker_role":"researcher",
				"allowed_tools":["WebSearch"],
				"expected_output":"facts with trusted sources",
				"required":true
			}]
		}`,
		`{"action":"respond_inline","requires_artifact":false,"deliverable_type":"answer","filename_hint":"","content_type":"text/markdown","reason":"inline research answer","confidence":"high"}`,
	}}
	runtime := newDeepResearchTestRuntime(t, runner)
	runtime.config.DeepResearch = DeepResearchRuntimeConfig{
		OrchestratorWorkerEnabled: true,
		WorkerBackend:             DeepResearchWorkerBackendInline,
		MaxWorkers:                4,
		MaxConcurrency:            2,
		WorkerTimeout:             time.Minute,
		TotalTimeout:              2 * time.Minute,
		MaxRetries:                1,
		RequireSources:            true,
		MinSuccessfulWorkers:      1,
	}
	session, err := runtime.CreateSession(context.Background(), "alice", "")
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	session.AddUserMessage("Earlier context about Acme security requirements")
	if err := runtime.sessions.Save(context.Background(), "alice", session); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	result, err := runtime.ExecuteDeepResearchTask(context.Background(), DeepAgentTaskRequest{
		UserID:    "alice",
		SessionID: session.ID,
		JobID:     "job-1",
		Goal:      "research Acme",
	}, nil, &recordingDeepResearchWorker{}, nil)
	if err != nil {
		t.Fatalf("ExecuteDeepResearchTask() error = %v", err)
	}
	drRun, ok := deepResearchRunStateFromAny(result.Run.State["deep_research"])
	if !ok || len(drRun.Plan.Nodes) != 1 {
		t.Fatalf("deep research plan missing: %#v", result.Run.State["deep_research"])
	}
	if got := deepAgentWorkflowString(drRun.Plan.Nodes[0].Metadata, "orchestrator"); got != deepResearchLLMOrchestratorVersion {
		t.Fatalf("runtime did not use LLM orchestrator: %q", got)
	}
	if _, ok := deepAgentLoadedContextFromMap(result.State.WorkingMemory); !ok {
		t.Fatalf("loaded context missing from working memory: %#v", result.State.WorkingMemory)
	}
	if prompts := runner.recordedPrompts(); len(prompts) < 1 || !strings.Contains(prompts[0], "Earlier context about Acme security requirements") {
		t.Fatalf("orchestrator did not receive loaded session context: %#v", prompts)
	}
}

func newDeepResearchTestRuntime(t *testing.T, runner Runner) *Runtime {
	t.Helper()
	root := t.TempDir()
	return NewRuntime(
		RuntimeConfig{DefaultWorkingDir: root, TurnTimeout: time.Minute},
		NewFileSessionStore(root),
		NewFileMemoryService(root),
		nil,
		func(Scope) Runner { return runner },
	)
}

type sequenceDeepResearchRunner struct {
	mu      sync.Mutex
	outputs []string
	errs    []error
	prompts []string
	calls   int
}

func (r *sequenceDeepResearchRunner) Run(ctx context.Context, session *state.Session, prompt string) (engine.Result, error) {
	return r.RunGeneratedPrompt(ctx, session, prompt)
}

func (r *sequenceDeepResearchRunner) RunGeneratedPrompt(_ context.Context, session *state.Session, prompt string) (engine.Result, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.prompts = append(r.prompts, prompt)
	idx := r.calls
	r.calls++
	if idx < len(r.errs) && r.errs[idx] != nil {
		return engine.Result{}, r.errs[idx]
	}
	output := ""
	if idx < len(r.outputs) {
		output = r.outputs[idx]
	}
	session.AddAssistantMessage(output)
	return engine.Result{Output: output, Session: session}, nil
}

func (r *sequenceDeepResearchRunner) recordedPrompts() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.prompts...)
}

func validDeepResearchPlanJSON(tool string) string {
	return `{
		"goal":"task-specific enterprise comparison",
		"max_concurrency":2,
		"nodes":[
			{
				"id":"security",
				"title":"Enterprise security evidence",
				"description":"Collect evidence about Acme enterprise access and security controls.",
				"depends_on":[],
				"worker_role":"researcher",
				"allowed_tools":["` + tool + `"],
				"expected_output":"security findings with traceable sources",
				"required":true
			},
			{
				"id":"comparison",
				"title":"Beta comparison",
				"description":"Compare Beta against the requested Acme enterprise requirements.",
				"depends_on":[],
				"worker_role":"analyst",
				"allowed_tools":["` + tool + `"],
				"expected_output":"comparison matrix with evidence",
				"required":true
			},
			{
				"id":"synthesis",
				"title":"Decision synthesis",
				"description":"Synthesize the evidence into a recommendation for the stated enterprise priorities.",
				"depends_on":["security","comparison"],
				"worker_role":"writer",
				"allowed_tools":["model"],
				"expected_output":"evidence-backed recommendation",
				"required":true
			}
		]
	}`
}
