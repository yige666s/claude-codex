package agentruntime

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestDeepResearchPlanValidationRejectsCycles(t *testing.T) {
	plan := DeepResearchPlan{
		Goal: "cycle",
		Nodes: []DeepResearchTaskNode{
			{ID: "a", Title: "A", DependsOn: []string{"b"}, Required: true},
			{ID: "b", Title: "B", DependsOn: []string{"a"}, Required: true},
		},
	}
	if err := validateDeepResearchPlan(plan); err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("validateDeepResearchPlan() error = %v, want cycle", err)
	}
}

func TestDeepResearchControllerRunsDAGAndPersistsWorkerState(t *testing.T) {
	store := NewMemoryWorkflowStore()
	worker := &recordingDeepResearchWorker{}
	controller := NewDeepResearchController(store, ContextWorkflowEventSink{}, staticDeepResearchOrchestrator{plan: DeepResearchPlan{
		Goal:           "调研 Tolan AI",
		MaxConcurrency: 2,
		Nodes: []DeepResearchTaskNode{
			{ID: "overview", Title: "Overview", Required: true},
			{ID: "pricing", Title: "Pricing", Required: true},
			{ID: "report", Title: "Report", DependsOn: []string{"overview", "pricing"}, Required: true, WorkerRole: "writer"},
		},
	}}, worker, nil, DeepResearchRuntimeConfig{
		OrchestratorWorkerEnabled: true,
		WorkerBackend:             DeepResearchWorkerBackendInline,
		MaxWorkers:                6,
		MaxConcurrency:            2,
		WorkerTimeout:             time.Second,
		TotalTimeout:              5 * time.Second,
		MaxRetries:                1,
		RequireSources:            true,
		MinSuccessfulWorkers:      1,
	})
	sink := &collectSink{}
	ctx := withJobEventEmitter(context.Background(), func(ctx context.Context, event Event) error {
		return sink.Send(ctx, event)
	})
	result, err := controller.Execute(ctx, DeepAgentTaskRequest{
		UserID:    "alice",
		SessionID: "session-1",
		JobID:     "job-1",
		Goal:      "调研 Tolan AI",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result == nil || result.Run == nil || result.State == nil {
		t.Fatalf("missing result: %#v", result)
	}
	if result.Run.Version != deepResearchWorkflowVersion {
		t.Fatalf("workflow version = %q, want %q", result.Run.Version, deepResearchWorkflowVersion)
	}
	drRun, ok := deepResearchRunStateFromAny(result.Run.State["deep_research"])
	if !ok {
		t.Fatalf("deep_research state missing: %#v", result.Run.State)
	}
	if drRun.Status != DeepResearchRunStatusSucceeded {
		t.Fatalf("deep research status = %q", drRun.Status)
	}
	if len(drRun.WorkerRuns) != 3 {
		t.Fatalf("worker count = %d", len(drRun.WorkerRuns))
	}
	if drRun.WorkerRuns["report"].Status != DeepResearchTaskStatusSucceeded {
		t.Fatalf("report node = %#v", drRun.WorkerRuns["report"])
	}
	if !worker.sawDependenciesFor("report", 2) {
		t.Fatalf("report did not receive dependency outputs: %#v", worker.inputs)
	}
	if len((StateDeepAgentEvidenceStore{}).ListStepEvidence(result.State)) < 3 {
		t.Fatalf("expected worker evidence in deep agent state: %#v", result.State.WorkingMemory)
	}
	for _, want := range []string{"deep_research_plan_created", "deep_research_worker_started", "deep_research_worker_succeeded", "deep_research_completed", "deep_agent_parallel_group_joined"} {
		if !sink.hasEvent(want) {
			t.Fatalf("missing event %s", want)
		}
	}
}

func TestDeepResearchControllerRetriesFailedWorker(t *testing.T) {
	store := NewMemoryWorkflowStore()
	worker := &recordingDeepResearchWorker{failFirst: map[string]bool{"overview": true}}
	controller := NewDeepResearchController(store, ContextWorkflowEventSink{}, staticDeepResearchOrchestrator{plan: DeepResearchPlan{
		Goal: "retry",
		Nodes: []DeepResearchTaskNode{
			{ID: "overview", Title: "Overview", Required: true},
			{ID: "report", Title: "Report", DependsOn: []string{"overview"}, Required: true},
		},
	}}, worker, nil, DeepResearchRuntimeConfig{
		OrchestratorWorkerEnabled: true,
		WorkerBackend:             DeepResearchWorkerBackendInline,
		MaxWorkers:                4,
		MaxConcurrency:            1,
		WorkerTimeout:             time.Second,
		TotalTimeout:              5 * time.Second,
		MaxRetries:                1,
		RequireSources:            true,
		MinSuccessfulWorkers:      1,
	})
	result, err := controller.Execute(context.Background(), DeepAgentTaskRequest{UserID: "alice", SessionID: "s", JobID: "j", Goal: "retry"})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	drRun, _ := deepResearchRunStateFromAny(result.Run.State["deep_research"])
	if got := drRun.WorkerRuns["overview"].Attempt; got != 2 {
		t.Fatalf("overview attempts = %d, want 2", got)
	}
	if drRun.WorkerRuns["overview"].Status != DeepResearchTaskStatusSucceeded {
		t.Fatalf("overview status = %#v", drRun.WorkerRuns["overview"])
	}
}

func TestDeepResearchControllerSchedulesReadyNodesAcrossBatches(t *testing.T) {
	store := NewMemoryWorkflowStore()
	worker := &recordingDeepResearchWorker{}
	controller := NewDeepResearchController(store, ContextWorkflowEventSink{}, staticDeepResearchOrchestrator{plan: DeepResearchPlan{
		Goal:           "batched ready scheduling",
		MaxConcurrency: 2,
		Nodes: []DeepResearchTaskNode{
			{ID: "a", Title: "A", Required: true},
			{ID: "b", Title: "B", Required: true},
			{ID: "c", Title: "C", Required: true},
			{ID: "report", Title: "Report", DependsOn: []string{"a", "b", "c"}, Required: true, WorkerRole: "writer"},
		},
	}}, worker, nil, DeepResearchRuntimeConfig{
		OrchestratorWorkerEnabled: true,
		WorkerBackend:             DeepResearchWorkerBackendInline,
		MaxWorkers:                4,
		MaxConcurrency:            2,
		WorkerTimeout:             time.Second,
		TotalTimeout:              5 * time.Second,
		RequireSources:            true,
		MinSuccessfulWorkers:      1,
	})

	result, err := controller.Execute(context.Background(), DeepAgentTaskRequest{
		UserID:    "alice",
		SessionID: "session-1",
		JobID:     "job-1",
		Goal:      "batched ready scheduling",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	drRun, _ := deepResearchRunStateFromAny(result.Run.State["deep_research"])
	for _, id := range []string{"a", "b", "c", "report"} {
		if got := drRun.WorkerRuns[id].Status; got != DeepResearchTaskStatusSucceeded {
			t.Fatalf("%s status = %q, want succeeded", id, got)
		}
	}
	if got := worker.callCount("c"); got != 1 {
		t.Fatalf("worker c calls = %d, want 1", got)
	}
	if !worker.sawDependenciesFor("report", 3) {
		t.Fatalf("report did not receive all dependency outputs: %#v", worker.inputs)
	}
}

func TestDeepResearchControllerHonorsPerNodeMaxAttempts(t *testing.T) {
	store := NewMemoryWorkflowStore()
	worker := &recordingDeepResearchWorker{failUntil: map[string]int{"overview": 2}}
	controller := NewDeepResearchController(store, ContextWorkflowEventSink{}, staticDeepResearchOrchestrator{plan: DeepResearchPlan{
		Goal: "per-node max attempts",
		Nodes: []DeepResearchTaskNode{
			{ID: "overview", Title: "Overview", Required: true, MaxAttempts: 3},
		},
	}}, worker, nil, DeepResearchRuntimeConfig{
		OrchestratorWorkerEnabled: true,
		WorkerBackend:             DeepResearchWorkerBackendInline,
		MaxWorkers:                2,
		MaxConcurrency:            1,
		WorkerTimeout:             time.Second,
		TotalTimeout:              5 * time.Second,
		MaxRetries:                0,
		RequireSources:            true,
		MinSuccessfulWorkers:      1,
	})

	result, err := controller.Execute(context.Background(), DeepAgentTaskRequest{
		UserID:    "alice",
		SessionID: "session-1",
		JobID:     "job-1",
		Goal:      "per-node max attempts",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	drRun, _ := deepResearchRunStateFromAny(result.Run.State["deep_research"])
	if got := drRun.WorkerRuns["overview"].Attempt; got != 3 {
		t.Fatalf("overview attempts = %d, want 3", got)
	}
	if got := drRun.WorkerRuns["overview"].Status; got != DeepResearchTaskStatusSucceeded {
		t.Fatalf("overview status = %q, want succeeded", got)
	}
	if got := worker.callCount("overview"); got != 3 {
		t.Fatalf("worker overview calls = %d, want 3", got)
	}
}

func TestRuntimeExecuteDeepResearchTaskUsesWorkflowState(t *testing.T) {
	runtime := testRuntime(t)
	runtime.config.DeepResearch = DeepResearchRuntimeConfig{
		OrchestratorWorkerEnabled: true,
		WorkerBackend:             DeepResearchWorkerBackendInline,
		MaxWorkers:                4,
		MaxConcurrency:            2,
		WorkerTimeout:             time.Second,
		TotalTimeout:              5 * time.Second,
		MaxRetries:                0,
		RequireSources:            true,
		MinSuccessfulWorkers:      1,
	}
	result, err := runtime.ExecuteDeepResearchTask(context.Background(), DeepAgentTaskRequest{
		UserID:    "alice",
		SessionID: "session-1",
		JobID:     "job-1",
		Goal:      "调研 Tolan AI 并生成调研报告",
	}, staticDeepResearchOrchestrator{plan: DeepResearchPlan{
		Goal: "调研 Tolan AI 并生成调研报告",
		Nodes: []DeepResearchTaskNode{
			{ID: "overview", Title: "Overview", Required: true},
			{ID: "report", Title: "Report", DependsOn: []string{"overview"}, Required: true},
		},
	}}, &recordingDeepResearchWorker{}, nil)
	if err != nil {
		t.Fatalf("ExecuteDeepResearchTask() error = %v", err)
	}
	if result.Run.Name != deepAgentTaskWorkflowName || result.Run.Version != deepResearchWorkflowVersion {
		t.Fatalf("unexpected workflow identity: %#v", result.Run)
	}
	if !strings.Contains(formatDeepAgentResultMessage(result, nil), "Workflow Run") {
		t.Fatalf("formatted result missing workflow: %s", formatDeepAgentResultMessage(result, nil))
	}
}

func TestDeepResearchAggregateCreatesArtifactOnlyWhenDeliverableDecisionRequiresIt(t *testing.T) {
	store := NewMemoryWorkflowStore()
	publisher := &recordingDeepResearchArtifactPublisher{ref: DeepAgentArtifactRef{ID: "artifact-1", Filename: "tolan-report.md", ContentType: "text/markdown"}}
	controller := NewDeepResearchController(store, ContextWorkflowEventSink{}, staticDeepResearchOrchestrator{plan: DeepResearchPlan{
		Goal: "调研 Tolan AI 并生成调研报告",
		Nodes: []DeepResearchTaskNode{
			{ID: "overview", Title: "Overview", Required: true},
		},
	}}, &recordingDeepResearchWorker{}, nil, DeepResearchRuntimeConfig{
		OrchestratorWorkerEnabled: true,
		WorkerBackend:             DeepResearchWorkerBackendInline,
		MaxWorkers:                2,
		MaxConcurrency:            1,
		WorkerTimeout:             time.Second,
		TotalTimeout:              5 * time.Second,
		RequireSources:            true,
		MinSuccessfulWorkers:      1,
	})
	controller.SetDeliverableDecider(staticDeepResearchDeliverableDecider{decision: DeepResearchDeliverableDecision{
		Action:           DeepResearchDeliverableActionCreateArtifact,
		RequiresArtifact: true,
		DeliverableType:  "report",
		FilenameHint:     "tolan-report.md",
		ContentType:      "text/markdown",
		Reason:           "user asked to generate a report",
		Confidence:       "high",
	}})
	controller.SetArtifactPublisher(publisher)

	result, err := controller.Execute(context.Background(), DeepAgentTaskRequest{UserID: "alice", SessionID: "s", JobID: "j", Goal: "调研 Tolan AI 并生成调研报告"})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if publisher.calls != 1 {
		t.Fatalf("artifact publisher calls = %d, want 1", publisher.calls)
	}
	refs := deepAgentArtifactRefsFromAny(result.State.WorkingMemory["final_artifact_refs"])
	if len(refs) != 1 || refs[0].ID != "artifact-1" {
		t.Fatalf("final artifact refs not persisted: %#v", result.State.WorkingMemory["final_artifact_refs"])
	}
	drRun, _ := deepResearchRunStateFromAny(result.Run.State["deep_research"])
	if len(drRun.Aggregate.Artifacts) != 1 || drRun.Aggregate.Deliverable.Action != DeepResearchDeliverableActionCreateArtifact {
		t.Fatalf("aggregate did not capture artifact deliverable: %#v", drRun.Aggregate)
	}
}

func TestDeepResearchAggregateInlineDecisionDoesNotRequireArtifact(t *testing.T) {
	store := NewMemoryWorkflowStore()
	publisher := &recordingDeepResearchArtifactPublisher{ref: DeepAgentArtifactRef{ID: "artifact-1"}}
	controller := NewDeepResearchController(store, ContextWorkflowEventSink{}, staticDeepResearchOrchestrator{plan: DeepResearchPlan{
		Goal: "调研 Tolan AI",
		Nodes: []DeepResearchTaskNode{
			{ID: "overview", Title: "Overview", Required: true},
		},
	}}, &recordingDeepResearchWorker{}, nil, DeepResearchRuntimeConfig{
		OrchestratorWorkerEnabled: true,
		WorkerBackend:             DeepResearchWorkerBackendInline,
		MaxWorkers:                2,
		MaxConcurrency:            1,
		WorkerTimeout:             time.Second,
		TotalTimeout:              5 * time.Second,
		RequireSources:            true,
		MinSuccessfulWorkers:      1,
	})
	controller.SetDeliverableDecider(staticDeepResearchDeliverableDecider{decision: DeepResearchDeliverableDecision{
		Action:           DeepResearchDeliverableActionRespondInline,
		RequiresArtifact: false,
		DeliverableType:  "answer",
		ContentType:      "text/markdown",
		Reason:           "user only asked for research",
		Confidence:       "high",
	}})
	controller.SetArtifactPublisher(publisher)

	result, err := controller.Execute(context.Background(), DeepAgentTaskRequest{UserID: "alice", SessionID: "s", JobID: "j", Goal: "调研 Tolan AI"})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if publisher.calls != 0 {
		t.Fatalf("artifact publisher calls = %d, want 0", publisher.calls)
	}
	drRun, _ := deepResearchRunStateFromAny(result.Run.State["deep_research"])
	if len(drRun.Aggregate.Artifacts) != 0 || drRun.Aggregate.Deliverable.Action != DeepResearchDeliverableActionRespondInline {
		t.Fatalf("aggregate should remain inline-only: %#v", drRun.Aggregate)
	}
}

func TestParseDeepResearchDeliverableDecisionToleratesToolLikeOutput(t *testing.T) {
	decision, err := parseDeepResearchDeliverableDecision(`{
		"action": "create_file",
		"requires_artifact": true,
		"deliverable_type": "report",
		"format": "markdown",
		"filename_hint": "tolan-report.md"
	}`)
	if err != nil {
		t.Fatalf("parseDeepResearchDeliverableDecision() error = %v", err)
	}
	normalized := normalizeDeepResearchDeliverableDecision(decision, DeepResearchDeliverableDecision{
		Action:       DeepResearchDeliverableActionRespondInline,
		ContentType:  "text/markdown",
		Reason:       "fallback",
		Confidence:   "low",
		FilenameHint: "fallback.md",
	})
	if normalized.Action != DeepResearchDeliverableActionCreateArtifact || !normalized.RequiresArtifact {
		t.Fatalf("normalized decision = %#v, want create artifact", normalized)
	}
	if normalized.FilenameHint != "tolan-report.md" {
		t.Fatalf("filename hint = %q", normalized.FilenameHint)
	}
}

func TestParseDeepResearchDeliverableDecisionToleratesNullOptionalFields(t *testing.T) {
	decision, err := parseDeepResearchDeliverableDecision(`{
		"action": "respond_inline",
		"requires_artifact": false,
		"deliverable_type": "answer",
		"filename_hint": null,
		"content_type": null,
		"reason": "ordinary research response",
		"confidence": "high"
	}`)
	if err != nil {
		t.Fatalf("parseDeepResearchDeliverableDecision() error = %v", err)
	}
	normalized := normalizeDeepResearchDeliverableDecision(decision, DeepResearchDeliverableDecision{
		Action:       DeepResearchDeliverableActionCreateArtifact,
		FilenameHint: "fallback.md",
		ContentType:  "text/markdown",
	})
	if normalized.Action != DeepResearchDeliverableActionRespondInline || normalized.RequiresArtifact {
		t.Fatalf("normalized decision = %#v, want inline", normalized)
	}
	if normalized.FilenameHint != "fallback.md" {
		t.Fatalf("filename hint = %q, want fallback", normalized.FilenameHint)
	}
}

func TestDeepResearchWorkflowDefinitionUsesTotalTimeoutForWorkerGraph(t *testing.T) {
	def := deepResearchWorkflowDefinition(7 * time.Second)
	if len(def.Steps) != 4 {
		t.Fatalf("step count = %d, want 4", len(def.Steps))
	}
	if got := def.Steps[2].Timeout; got != 7*time.Second {
		t.Fatalf("worker graph timeout = %s, want total timeout", got)
	}
}

func TestRuleDeepResearchOrchestratorTruncatesToDeliverableDAG(t *testing.T) {
	plan, err := (ruleDeepResearchOrchestrator{}).Plan(context.Background(), DeepAgentTaskRequest{
		Goal: "调研 Tolan AI 并生成报告",
	}, DeepResearchRuntimeConfig{
		MaxWorkers:     2,
		MaxConcurrency: 2,
		WorkerTimeout:  time.Second,
	})
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if len(plan.Nodes) != 2 {
		t.Fatalf("node count = %d, want 2", len(plan.Nodes))
	}
	if got := plan.Nodes[len(plan.Nodes)-1].ID; got != "synthesis" {
		t.Fatalf("last node id = %q, want synthesis", got)
	}
	deps := plan.Nodes[len(plan.Nodes)-1].DependsOn
	if len(deps) != 1 || deps[0] != plan.Nodes[0].ID {
		t.Fatalf("synthesis deps = %#v, want only retained prerequisite", deps)
	}
}

func TestRuleDeepResearchOrchestratorSingleWorkerKeepsSourceGathering(t *testing.T) {
	plan, err := (ruleDeepResearchOrchestrator{}).Plan(context.Background(), DeepAgentTaskRequest{
		Goal: "调研 Tolan AI 并生成报告",
	}, DeepResearchRuntimeConfig{
		MaxWorkers:     1,
		MaxConcurrency: 1,
		WorkerTimeout:  time.Second,
	})
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if len(plan.Nodes) != 1 {
		t.Fatalf("node count = %d, want 1", len(plan.Nodes))
	}
	if deepResearchNodeLooksDeliverable(plan.Nodes[0]) || !containsString(plan.Nodes[0].AllowedTools, "WebSearch") {
		t.Fatalf("single worker must gather evidence before aggregation: %#v", plan.Nodes[0])
	}
}

func TestRuleDeepResearchOrchestratorCategorizesCodeGoals(t *testing.T) {
	plan, err := (ruleDeepResearchOrchestrator{}).Plan(context.Background(), DeepAgentTaskRequest{
		Goal: "分析这个 repo 的代码架构并输出总结",
	}, DeepResearchRuntimeConfig{
		MaxWorkers:     4,
		MaxConcurrency: 2,
		WorkerTimeout:  time.Second,
	})
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if got := plan.Nodes[0].Metadata["goal_category"]; got != "codebase" {
		t.Fatalf("goal category = %#v, want codebase", got)
	}
	if !hasDeepResearchNode(plan.Nodes, "codebase") {
		t.Fatalf("expected codebase node in plan: %#v", plan.Nodes)
	}
}

func TestDeepResearchAggregatorRejectsUnverifiedModelTextSources(t *testing.T) {
	aggregate, err := (ruleDeepResearchAggregator{requireSources: true}).Aggregate(context.Background(), DeepResearchRunState{
		Goal: "sources required",
		WorkerRuns: map[string]DeepResearchTaskNode{
			"overview": {
				ID:       "overview",
				Required: true,
				Status:   DeepResearchTaskStatusSucceeded,
				Result: &DeepResearchWorkerResult{
					Status:  DeepAgentActionStatusSucceeded,
					Summary: "found a URL in plain text",
					Output:  "see https://example.com/pricing",
					Sources: []DeepAgentSourceRef{{
						URL:      "https://example.com/pricing",
						Title:    "https://example.com/pricing",
						Provider: "model_text",
					}},
				},
			},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "trusted source evidence") {
		t.Fatalf("Aggregate() error = %v, want trusted source evidence failure", err)
	}
	if !aggregate.Partial {
		t.Fatalf("expected partial aggregate when trusted sources are missing: %#v", aggregate)
	}
}

func TestDeepResearchAggregatorRemovesUntrustedFindingCitations(t *testing.T) {
	aggregate, err := (ruleDeepResearchAggregator{requireSources: true}).Aggregate(context.Background(), DeepResearchRunState{
		Goal: "source integrity",
		WorkerRuns: map[string]DeepResearchTaskNode{
			"overview": {
				ID:       "overview",
				Required: true,
				Status:   DeepResearchTaskStatusSucceeded,
				Result: &DeepResearchWorkerResult{
					Status:  DeepAgentActionStatusSucceeded,
					Sources: []DeepAgentSourceRef{{URL: "https://example.com/verified", Title: "Verified", Provider: "test"}},
					Findings: []DeepResearchFinding{
						{Claim: "verified claim", SourceURL: "https://example.com/verified"},
						{Claim: "uncited claim", SourceURL: "https://attacker.example/fabricated"},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Aggregate() error = %v", err)
	}
	if len(aggregate.Findings) != 2 || aggregate.Findings[0].SourceURL != "https://example.com/verified" || aggregate.Findings[1].SourceURL != "" {
		t.Fatalf("untrusted finding citation was retained: %#v", aggregate.Findings)
	}
	if strings.Contains(aggregate.FinalAnswer, "https://attacker.example/fabricated") {
		t.Fatalf("final answer contains an untrusted citation: %s", aggregate.FinalAnswer)
	}
}

type staticDeepResearchOrchestrator struct {
	plan DeepResearchPlan
	err  error
}

func (o staticDeepResearchOrchestrator) Plan(context.Context, DeepAgentTaskRequest, DeepResearchRuntimeConfig) (DeepResearchPlan, error) {
	if o.err != nil {
		return DeepResearchPlan{}, o.err
	}
	return o.plan, nil
}

type recordingDeepResearchWorker struct {
	mu        sync.Mutex
	inputs    []DeepResearchWorkerInput
	failFirst map[string]bool
	failUntil map[string]int
	calls     map[string]int
}

type staticDeepResearchDeliverableDecider struct {
	decision DeepResearchDeliverableDecision
	err      error
}

func (d staticDeepResearchDeliverableDecider) DecideDeepResearchDeliverable(context.Context, DeepAgentTaskRequest, *DeepAgentState, DeepResearchRunState, DeepResearchAggregateResult) (DeepResearchDeliverableDecision, error) {
	return d.decision, d.err
}

type recordingDeepResearchArtifactPublisher struct {
	calls int
	ref   DeepAgentArtifactRef
	err   error
}

func (p *recordingDeepResearchArtifactPublisher) PublishDeepResearchArtifact(context.Context, DeepAgentTaskRequest, *DeepAgentState, DeepResearchRunState, DeepResearchAggregateResult, DeepResearchDeliverableDecision) (DeepAgentArtifactRef, error) {
	p.calls++
	return p.ref, p.err
}

func (w *recordingDeepResearchWorker) ExecuteWorker(_ context.Context, input DeepResearchWorkerInput) (DeepResearchWorkerResult, error) {
	w.mu.Lock()
	if w.calls == nil {
		w.calls = map[string]int{}
	}
	w.calls[input.Node.ID]++
	call := w.calls[input.Node.ID]
	w.inputs = append(w.inputs, input)
	shouldFail := (w.failFirst != nil && w.failFirst[input.Node.ID] && call == 1) || (w.failUntil != nil && call <= w.failUntil[input.Node.ID])
	w.mu.Unlock()
	if shouldFail {
		return DeepResearchWorkerResult{Status: DeepAgentActionStatusFailed, Summary: "temporary failure"}, fmt.Errorf("temporary failure")
	}
	return DeepResearchWorkerResult{
		Status:  DeepAgentActionStatusSucceeded,
		Summary: "completed " + input.Node.ID,
		Output:  "completed " + input.Node.ID,
		Findings: []DeepResearchFinding{{
			Claim:      "claim " + input.Node.ID,
			Evidence:   "evidence " + input.Node.ID,
			SourceURL:  "https://example.com/" + input.Node.ID,
			Confidence: "high",
		}},
		Sources:   []DeepAgentSourceRef{{URL: "https://example.com/" + input.Node.ID, Title: "Source " + input.Node.ID, Provider: "test"}},
		ToolCalls: []DeepAgentToolCallRef{{Name: "WebSearch", Status: "succeeded"}},
	}, nil
}

func (w *recordingDeepResearchWorker) sawDependenciesFor(nodeID string, count int) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, input := range w.inputs {
		if input.Node.ID == nodeID && len(input.DependencyOutput) == count {
			return true
		}
	}
	return false
}

func (w *recordingDeepResearchWorker) callCount(nodeID string) int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.calls[nodeID]
}

func hasDeepResearchNode(nodes []DeepResearchTaskNode, id string) bool {
	for _, node := range nodes {
		if node.ID == id {
			return true
		}
	}
	return false
}
