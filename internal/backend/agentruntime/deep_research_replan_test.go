package agentruntime

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestDeepResearchControllerReplansRequiredFailureAndCompletesReplacementGraph(t *testing.T) {
	store := NewMemoryWorkflowStore()
	orchestrator := &recordingDeepResearchReplanner{
		initial: DeepResearchPlan{
			Goal:           "research Acme pricing",
			MaxConcurrency: 1,
			Nodes: []DeepResearchTaskNode{
				{ID: "pricing", Title: "Pricing", Description: "find official pricing", WorkerRole: "researcher", AllowedTools: []string{"WebSearch"}, ExpectedOutput: "official pricing", Required: true, MaxAttempts: 1},
				{ID: "report", Title: "Report", Description: "write report", DependsOn: []string{"pricing"}, WorkerRole: "writer", AllowedTools: []string{"model"}, ExpectedOutput: "report", Required: true},
			},
		},
		replanned: DeepResearchPlan{
			Goal:           "research Acme pricing",
			MaxConcurrency: 1,
			Nodes: []DeepResearchTaskNode{
				{ID: "pricing-alternative", Title: "Alternative pricing evidence", Description: "use a different source path for official pricing", WorkerRole: "researcher", AllowedTools: []string{"WebFetch"}, ExpectedOutput: "official pricing evidence", Required: true},
				{ID: "report", Title: "Report", Description: "write report", DependsOn: []string{"pricing-alternative"}, WorkerRole: "writer", AllowedTools: []string{"model"}, ExpectedOutput: "report", Required: true},
			},
		},
	}
	worker := &recordingDeepResearchWorker{failUntil: map[string]int{"pricing": 1}}
	controller := NewDeepResearchController(store, ContextWorkflowEventSink{}, orchestrator, worker, nil, DeepResearchRuntimeConfig{
		OrchestratorWorkerEnabled: true,
		WorkerBackend:             DeepResearchWorkerBackendInline,
		MaxWorkers:                4,
		MaxConcurrency:            1,
		WorkerTimeout:             time.Second,
		TotalTimeout:              5 * time.Second,
		MaxRetries:                0,
		ReplanEnabled:             true,
		MaxReplans:                2,
		ReplanEveryBatches:        100,
		RequireSources:            true,
		MinSuccessfulWorkers:      1,
	})
	sink := &collectSink{}
	ctx := withJobEventEmitter(context.Background(), func(ctx context.Context, event Event) error {
		return sink.Send(ctx, event)
	})

	result, err := controller.Execute(ctx, DeepAgentTaskRequest{UserID: "alice", SessionID: "s", JobID: "j", Goal: "research Acme pricing"})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	drRun, ok := deepResearchRunStateFromAny(result.Run.State["deep_research"])
	if !ok {
		t.Fatalf("deep research state missing: %#v", result.Run.State)
	}
	if drRun.PlanRevision != 2 || drRun.ReplanCount != 1 || drRun.ReplanAttempts != 1 {
		t.Fatalf("unexpected replan state: revision=%d count=%d attempts=%d", drRun.PlanRevision, drRun.ReplanCount, drRun.ReplanAttempts)
	}
	if len(drRun.ReplanHistory) != 1 || !drRun.ReplanHistory[0].Changed || drRun.ReplanHistory[0].Trigger.Kind != DeepResearchReplanReasonRequiredFailure {
		t.Fatalf("unexpected replan history: %#v", drRun.ReplanHistory)
	}
	if !hasDeepResearchNode(drRun.ReplanHistory[0].PreviousPlan.Nodes, "pricing") || drRun.ReplanHistory[0].PreviousPlan.Nodes[0].Status != DeepResearchTaskStatusFailedFinal {
		t.Fatalf("replan history did not preserve the failed prior plan: %#v", drRun.ReplanHistory[0].PreviousPlan)
	}
	if _, exists := drRun.WorkerRuns["pricing"]; exists {
		t.Fatalf("failed obsolete node remained in active graph: %#v", drRun.WorkerRuns)
	}
	for _, id := range []string{"pricing-alternative", "report"} {
		if got := drRun.WorkerRuns[id].Status; got != DeepResearchTaskStatusSucceeded {
			t.Fatalf("%s status = %q, want succeeded", id, got)
		}
	}
	if orchestrator.replanCalls() != 1 {
		t.Fatalf("replan calls = %d, want 1", orchestrator.replanCalls())
	}
	for _, eventType := range []string{"deep_research_replan_started", "deep_research_replan_applied"} {
		if !sink.hasEvent(eventType) {
			t.Fatalf("missing event %s", eventType)
		}
	}
}

func TestDeepResearchControllerReplansTerminalEvidenceGap(t *testing.T) {
	orchestrator := &recordingDeepResearchReplanner{
		initial: DeepResearchPlan{Goal: "research Acme", MaxConcurrency: 1, Nodes: []DeepResearchTaskNode{
			{ID: "overview", Title: "Overview", Description: "collect overview", WorkerRole: "researcher", AllowedTools: []string{"WebSearch"}, ExpectedOutput: "overview", Required: true},
		}},
		replanned: DeepResearchPlan{Goal: "research Acme", MaxConcurrency: 1, Nodes: []DeepResearchTaskNode{
			{ID: "official-source", Title: "Official source", Description: "close the source gap with verified evidence", DependsOn: []string{"overview"}, WorkerRole: "researcher", AllowedTools: []string{"WebFetch"}, ExpectedOutput: "verified source", Required: true},
		}},
	}
	controller := NewDeepResearchController(NewMemoryWorkflowStore(), ContextWorkflowEventSink{}, orchestrator, sourceGapDeepResearchWorker{}, nil, DeepResearchRuntimeConfig{
		OrchestratorWorkerEnabled: true,
		MaxWorkers:                3,
		MaxConcurrency:            1,
		WorkerTimeout:             time.Second,
		TotalTimeout:              5 * time.Second,
		ReplanEnabled:             true,
		MaxReplans:                2,
		ReplanEveryBatches:        100,
		RequireSources:            true,
		MinSuccessfulWorkers:      1,
	})

	result, err := controller.Execute(context.Background(), DeepAgentTaskRequest{Goal: "research Acme"})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	drRun, _ := deepResearchRunStateFromAny(result.Run.State["deep_research"])
	if drRun.PlanRevision != 2 || drRun.LastReplanReason != DeepResearchReplanReasonEvidenceGap {
		t.Fatalf("unexpected evidence-gap replan state: %#v", drRun)
	}
	if got := drRun.WorkerRuns["official-source"].Status; got != DeepResearchTaskStatusSucceeded {
		t.Fatalf("official-source status = %q, want succeeded", got)
	}
	if len(drRun.Aggregate.Sources) != 1 {
		t.Fatalf("trusted aggregate sources = %d, want 1", len(drRun.Aggregate.Sources))
	}
}

func TestDeepResearchShouldTriggerReplanAfterRequiredWorkerFailsFinally(t *testing.T) {
	run := DeepResearchRunState{
		Goal: "research Acme pricing",
		Plan: DeepResearchPlan{
			Goal: "research Acme pricing",
			Nodes: []DeepResearchTaskNode{
				{ID: "overview", Title: "Overview", Required: true},
				{ID: "pricing", Title: "Pricing", Required: true},
			},
		},
		WorkerRuns: map[string]DeepResearchTaskNode{
			"overview": succeededDeepResearchNode("overview"),
			"pricing": {
				ID:       "pricing",
				Title:    "Pricing",
				Required: true,
				Status:   DeepResearchTaskStatusFailedFinal,
				Error:    "official pricing page blocked",
				Result: &DeepResearchWorkerResult{
					Status: DeepAgentActionStatusFailed,
					Errors: []string{"official pricing page blocked"},
				},
			},
		},
	}

	reason, ok := shouldDeepResearchReplan(run)
	if !ok {
		t.Fatalf("shouldDeepResearchReplan() = false, want true")
	}
	if reason != DeepResearchReplanReasonRequiredFailure {
		t.Fatalf("replan reason = %q, want %q", reason, DeepResearchReplanReasonRequiredFailure)
	}
}

func TestDeepResearchShouldTriggerReplanForEvidenceGapWithoutProgress(t *testing.T) {
	run := DeepResearchRunState{
		Goal: "research Acme pricing",
		Plan: DeepResearchPlan{
			Goal: "research Acme pricing",
			Nodes: []DeepResearchTaskNode{
				{ID: "overview", Title: "Overview", Required: true},
			},
		},
		WorkerRuns: map[string]DeepResearchTaskNode{
			"overview": {
				ID:       "overview",
				Title:    "Overview",
				Required: true,
				Status:   DeepResearchTaskStatusSucceeded,
				Result: &DeepResearchWorkerResult{
					Status:        DeepAgentActionStatusSucceeded,
					Summary:       "Only found second-hand summaries",
					OpenQuestions: []string{"Need an official primary source for pricing."},
				},
			},
		},
	}

	reason, ok := shouldDeepResearchReplan(run)
	if !ok {
		t.Fatalf("shouldDeepResearchReplan() = false, want true")
	}
	if reason != DeepResearchReplanReasonEvidenceGap {
		t.Fatalf("replan reason = %q, want %q", reason, DeepResearchReplanReasonEvidenceGap)
	}
}

func TestApplyDeepResearchReplanPreservesCompletedNodesAndReplacesPendingGraph(t *testing.T) {
	run := DeepResearchRunState{
		Goal: "research Acme pricing",
		Plan: DeepResearchPlan{
			Goal: "research Acme pricing",
			Nodes: []DeepResearchTaskNode{
				{ID: "overview", Title: "Overview", Required: true, AllowedTools: []string{"WebSearch"}},
				{ID: "pricing", Title: "Pricing", Required: true, DependsOn: []string{"overview"}, AllowedTools: []string{"WebSearch"}},
				{ID: "report", Title: "Report", Required: true, DependsOn: []string{"pricing"}, AllowedTools: []string{"model"}},
			},
		},
		WorkerRuns: map[string]DeepResearchTaskNode{
			"overview": succeededDeepResearchNode("overview"),
			"pricing":  pendingDeepResearchNode("pricing", []string{"overview"}),
			"report":   pendingDeepResearchNode("report", []string{"pricing"}),
		},
	}

	proposal := DeepResearchReplan{
		Revision: 2,
		Reason:   DeepResearchReplanReasonRequiredFailure,
		Plan: DeepResearchPlan{
			Goal: "research Acme pricing",
			Nodes: []DeepResearchTaskNode{
				{ID: "official_docs", Title: "Official Docs", Required: true, DependsOn: []string{"overview"}, AllowedTools: []string{"WebFetch"}},
				{ID: "report", Title: "Report", Required: true, DependsOn: []string{"overview", "official_docs"}, AllowedTools: []string{"model"}},
			},
		},
	}

	if err := applyDeepResearchReplan(&run, proposal, map[string]string{
		"WebSearch": "search the web",
		"WebFetch":  "fetch a URL",
		"model":     "reason without external tools",
	}); err != nil {
		t.Fatalf("applyDeepResearchReplan() error = %v", err)
	}

	if got := run.WorkerRuns["overview"].Status; got != DeepResearchTaskStatusSucceeded {
		t.Fatalf("overview status = %q, want succeeded", got)
	}
	if got := run.WorkerRuns["overview"].Result; got == nil || got.Summary != "completed overview" {
		t.Fatalf("overview result not preserved: %#v", run.WorkerRuns["overview"].Result)
	}
	if _, ok := run.WorkerRuns["pricing"]; ok {
		t.Fatalf("obsolete pending node pricing should be removed after replan: %#v", run.WorkerRuns)
	}
	if got := run.WorkerRuns["official_docs"].Status; got != DeepResearchTaskStatusPending {
		t.Fatalf("official_docs status = %q, want pending", got)
	}
	if deps := run.WorkerRuns["report"].DependsOn; len(deps) != 2 || deps[0] != "overview" || deps[1] != "official_docs" {
		t.Fatalf("report deps = %#v, want [overview official_docs]", deps)
	}
}

func TestApplyDeepResearchReplanRejectsMutationOfCompletedNode(t *testing.T) {
	run := DeepResearchRunState{
		Goal: "research Acme pricing",
		Plan: DeepResearchPlan{
			Goal: "research Acme pricing",
			Nodes: []DeepResearchTaskNode{
				{ID: "overview", Title: "Overview", Required: true, AllowedTools: []string{"WebSearch"}},
			},
		},
		WorkerRuns: map[string]DeepResearchTaskNode{
			"overview": succeededDeepResearchNode("overview"),
		},
	}

	err := applyDeepResearchReplan(&run, DeepResearchReplan{
		Revision: 2,
		Reason:   DeepResearchReplanReasonRequiredFailure,
		Plan: DeepResearchPlan{
			Goal: "research Acme pricing",
			Nodes: []DeepResearchTaskNode{
				{ID: "overview", Title: "Overview rewritten", Required: true, AllowedTools: []string{"WebFetch"}},
			},
		},
	}, map[string]string{
		"WebSearch": "search the web",
		"WebFetch":  "fetch a URL",
	})
	if err == nil || !strings.Contains(err.Error(), "completed node") {
		t.Fatalf("applyDeepResearchReplan() error = %v, want completed node rejection", err)
	}
}

func TestApplyDeepResearchReplanRejectsCycleInReplacementGraph(t *testing.T) {
	run := DeepResearchRunState{
		Goal: "research Acme pricing",
		Plan: DeepResearchPlan{
			Goal: "research Acme pricing",
			Nodes: []DeepResearchTaskNode{
				{ID: "overview", Title: "Overview", Required: true},
			},
		},
		WorkerRuns: map[string]DeepResearchTaskNode{
			"overview": succeededDeepResearchNode("overview"),
		},
	}

	err := applyDeepResearchReplan(&run, DeepResearchReplan{
		Revision: 2,
		Reason:   DeepResearchReplanReasonEvidenceGap,
		Plan: DeepResearchPlan{
			Goal: "research Acme pricing",
			Nodes: []DeepResearchTaskNode{
				{ID: "a", Title: "A", Required: true, DependsOn: []string{"b"}},
				{ID: "b", Title: "B", Required: true, DependsOn: []string{"a"}},
			},
		},
	}, map[string]string{
		"WebSearch": "search the web",
	})
	if err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("applyDeepResearchReplan() error = %v, want cycle rejection", err)
	}
}

func TestApplyDeepResearchReplanRejectsUnavailableTools(t *testing.T) {
	run := DeepResearchRunState{
		Goal: "research Acme pricing",
		Plan: DeepResearchPlan{
			Goal: "research Acme pricing",
			Nodes: []DeepResearchTaskNode{
				{ID: "overview", Title: "Overview", Required: true},
			},
		},
		WorkerRuns: map[string]DeepResearchTaskNode{
			"overview": succeededDeepResearchNode("overview"),
		},
	}

	err := applyDeepResearchReplan(&run, DeepResearchReplan{
		Revision: 2,
		Reason:   DeepResearchReplanReasonEvidenceGap,
		Plan: DeepResearchPlan{
			Goal: "research Acme pricing",
			Nodes: []DeepResearchTaskNode{
				{ID: "repo_scan", Title: "Repo Scan", Required: true, AllowedTools: []string{"repo_search"}},
			},
		},
	}, map[string]string{
		"WebSearch": "search the web",
		"WebFetch":  "fetch a URL",
	})
	if err == nil || !strings.Contains(err.Error(), "repo_search") {
		t.Fatalf("applyDeepResearchReplan() error = %v, want unavailable tool rejection", err)
	}
}

func TestApplyDeepResearchReplanPersistsRevisionAndReason(t *testing.T) {
	run := DeepResearchRunState{
		Goal: "research Acme pricing",
		Plan: DeepResearchPlan{
			Goal: "research Acme pricing",
			Nodes: []DeepResearchTaskNode{
				{ID: "overview", Title: "Overview", Required: true},
			},
		},
		WorkerRuns: map[string]DeepResearchTaskNode{
			"overview": succeededDeepResearchNode("overview"),
		},
	}

	proposal := DeepResearchReplan{
		Revision: 3,
		Reason:   DeepResearchReplanReasonEvidenceGap,
		Plan: DeepResearchPlan{
			Goal: "research Acme pricing",
			Nodes: []DeepResearchTaskNode{
				{ID: "official_docs", Title: "Official Docs", Required: true, DependsOn: []string{"overview"}, AllowedTools: []string{"WebFetch"}},
			},
		},
	}

	if err := applyDeepResearchReplan(&run, proposal, map[string]string{
		"WebFetch": "fetch a URL",
	}); err != nil {
		t.Fatalf("applyDeepResearchReplan() error = %v", err)
	}

	if got := run.PlanRevision; got != 3 {
		t.Fatalf("plan revision = %d, want 3", got)
	}
	if got := run.LastReplanReason; got != DeepResearchReplanReasonEvidenceGap {
		t.Fatalf("last replan reason = %q, want %q", got, DeepResearchReplanReasonEvidenceGap)
	}
}

func TestDeepResearchWorkerPromptIncludesPreviousFailureContextOnRetry(t *testing.T) {
	prompt := deepResearchWorkerPrompt(DeepResearchWorkerInput{
		Goal: "research Acme pricing",
		Node: DeepResearchTaskNode{
			ID:             "pricing",
			Title:          "Pricing",
			WorkerRole:     "researcher",
			ExpectedOutput: "official pricing with source",
			Attempt:        2,
			Error:          "official pricing page blocked",
			Result: &DeepResearchWorkerResult{
				Status:  DeepAgentActionStatusFailed,
				Summary: "first attempt only found mirrored sources",
				Errors:  []string{"official pricing page blocked"},
			},
		},
	})

	if !strings.Contains(prompt, "Previous attempt failed") {
		t.Fatalf("prompt missing retry preamble: %s", prompt)
	}
	if !strings.Contains(prompt, "official pricing page blocked") {
		t.Fatalf("prompt missing prior error: %s", prompt)
	}
	if !strings.Contains(prompt, "first attempt only found mirrored sources") {
		t.Fatalf("prompt missing prior failure summary: %s", prompt)
	}
}

func succeededDeepResearchNode(id string) DeepResearchTaskNode {
	return DeepResearchTaskNode{
		ID:       id,
		Title:    strings.Title(strings.ReplaceAll(id, "_", " ")),
		Required: true,
		Status:   DeepResearchTaskStatusSucceeded,
		Result: &DeepResearchWorkerResult{
			Status:  DeepAgentActionStatusSucceeded,
			Summary: "completed " + id,
			Output:  "completed " + id,
			Sources: []DeepAgentSourceRef{{
				URL:        "https://example.com/" + id,
				Title:      "Source " + id,
				Provider:   "test",
				SourceKind: "tool_verified",
			}},
		},
	}
}

func pendingDeepResearchNode(id string, dependsOn []string) DeepResearchTaskNode {
	return DeepResearchTaskNode{
		ID:        id,
		Title:     strings.Title(strings.ReplaceAll(id, "_", " ")),
		Required:  true,
		Status:    DeepResearchTaskStatusPending,
		DependsOn: append([]string(nil), dependsOn...),
	}
}

type recordingDeepResearchReplanner struct {
	mu        sync.Mutex
	initial   DeepResearchPlan
	replanned DeepResearchPlan
	triggers  []DeepResearchReplanTrigger
}

type sourceGapDeepResearchWorker struct{}

func (sourceGapDeepResearchWorker) ExecuteWorker(_ context.Context, input DeepResearchWorkerInput) (DeepResearchWorkerResult, error) {
	result := DeepResearchWorkerResult{Status: DeepAgentActionStatusSucceeded, Summary: "completed " + input.Node.ID, Output: "completed " + input.Node.ID}
	if input.Node.ID == "overview" {
		result.OpenQuestions = []string{"Need a verified primary source."}
		return result, nil
	}
	result.Sources = []DeepAgentSourceRef{{URL: "https://example.com/official", Title: "Official", Provider: "test", SourceKind: "tool_verified"}}
	result.ToolCalls = []DeepAgentToolCallRef{{Name: "WebFetch", Status: "succeeded"}}
	return result, nil
}

func (o *recordingDeepResearchReplanner) Plan(context.Context, DeepAgentTaskRequest, DeepResearchRuntimeConfig) (DeepResearchPlan, error) {
	return o.initial, nil
}

func (o *recordingDeepResearchReplanner) Replan(_ context.Context, _ DeepAgentTaskRequest, _ DeepResearchRunState, trigger DeepResearchReplanTrigger, _ DeepResearchRuntimeConfig) (DeepResearchPlan, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.triggers = append(o.triggers, trigger)
	return o.replanned, nil
}

func (o *recordingDeepResearchReplanner) replanCalls() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return len(o.triggers)
}
