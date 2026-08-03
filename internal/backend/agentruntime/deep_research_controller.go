package agentruntime

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

type DeepResearchOrchestrator interface {
	Plan(ctx context.Context, req DeepAgentTaskRequest, cfg DeepResearchRuntimeConfig) (DeepResearchPlan, error)
}

type DeepResearchReplanner interface {
	Replan(ctx context.Context, req DeepAgentTaskRequest, run DeepResearchRunState, trigger DeepResearchReplanTrigger, cfg DeepResearchRuntimeConfig) (DeepResearchPlan, error)
}

type DeepResearchWorkerExecutor interface {
	ExecuteWorker(ctx context.Context, input DeepResearchWorkerInput) (DeepResearchWorkerResult, error)
}

type DeepResearchAggregator interface {
	Aggregate(ctx context.Context, run DeepResearchRunState) (DeepResearchAggregateResult, error)
}

type DeepResearchDeliverableDecider interface {
	DecideDeepResearchDeliverable(ctx context.Context, req DeepAgentTaskRequest, state *DeepAgentState, run DeepResearchRunState, aggregate DeepResearchAggregateResult) (DeepResearchDeliverableDecision, error)
}

type DeepResearchArtifactPublisher interface {
	PublishDeepResearchArtifact(ctx context.Context, req DeepAgentTaskRequest, state *DeepAgentState, run DeepResearchRunState, aggregate DeepResearchAggregateResult, decision DeepResearchDeliverableDecision) (DeepAgentArtifactRef, error)
}

type DeepResearchController struct {
	store              WorkflowStore
	events             WorkflowEventSink
	orchestrator       DeepResearchOrchestrator
	worker             DeepResearchWorkerExecutor
	aggregator         DeepResearchAggregator
	deliverableDecider DeepResearchDeliverableDecider
	artifactPublisher  DeepResearchArtifactPublisher
	contextLoader      DeepAgentContextLoader
	evidence           DeepAgentEvidenceStore
	config             DeepResearchRuntimeConfig
	clock              Clock
}

func NewDeepResearchController(store WorkflowStore, events WorkflowEventSink, orchestrator DeepResearchOrchestrator, worker DeepResearchWorkerExecutor, aggregator DeepResearchAggregator, cfg DeepResearchRuntimeConfig) *DeepResearchController {
	if store == nil {
		store = NewMemoryWorkflowStore()
	}
	if events == nil {
		events = NoopWorkflowEventSink{}
	}
	cfg = normalizeDeepResearchRuntimeConfig(cfg)
	if orchestrator == nil {
		orchestrator = ruleDeepResearchOrchestrator{}
	}
	if worker == nil {
		worker = noopDeepResearchWorker{}
	}
	if aggregator == nil {
		aggregator = ruleDeepResearchAggregator{requireSources: cfg.RequireSources}
	}
	return &DeepResearchController{
		store:         store,
		events:        events,
		orchestrator:  orchestrator,
		worker:        worker,
		aggregator:    aggregator,
		contextLoader: noopDeepAgentContextLoader{},
		evidence:      deepAgentDefaultEvidenceStore(),
		config:        cfg,
		clock:         systemClock{},
	}
}

func (c *DeepResearchController) SetContextLoader(loader DeepAgentContextLoader) {
	if c != nil && loader != nil {
		c.contextLoader = loader
	}
}

func (c *DeepResearchController) SetEvidenceStore(store DeepAgentEvidenceStore) {
	if c != nil && store != nil {
		c.evidence = store
	}
}

func (c *DeepResearchController) SetDeliverableDecider(decider DeepResearchDeliverableDecider) {
	if c != nil {
		c.deliverableDecider = decider
	}
}

func (c *DeepResearchController) SetArtifactPublisher(publisher DeepResearchArtifactPublisher) {
	if c != nil {
		c.artifactPublisher = publisher
	}
}

func (c *DeepResearchController) Execute(ctx context.Context, req DeepAgentTaskRequest) (*DeepAgentTaskResult, error) {
	if c == nil {
		return nil, fmt.Errorf("deep research controller is not configured")
	}
	req = applyDeepAgentTaskTemplateToTaskRequest(req)
	req.Policy = normalizeDeepAgentPolicy(req.Policy)
	contract := BuildLoopContractFromDeepAgentRequest(req, firstNonEmptyString(req.JobID, jobIDFromContext(ctx)), requestIDFromContext(ctx), c.now())
	req.LoopContract = contract
	if req.State == nil {
		req.State = map[string]any{}
	}
	req.State["loop_contract"] = contract
	req.State["loop_contract_id"] = contract.ID
	req.State["loop_contract_version"] = contract.Version
	req.State["deep_research_enabled"] = true

	engine := NewWorkflowEngine(c.store, c.events)
	var state *DeepAgentState
	var drRun DeepResearchRunState
	engine.RegisterStepHandler("initialize_task", func(ctx context.Context, run *WorkflowRun, input map[string]any) (map[string]any, error) {
		now := c.now()
		state = &DeepAgentState{
			Goal:           strings.TrimSpace(req.Goal),
			Rubric:         normalizeDeepAgentRubric(req.Rubric),
			LoopContract:   contract,
			Status:         DeepAgentRunStatusRunning,
			StartedAt:      now,
			UpdatedAt:      now,
			WorkingMemory:  cloneWorkflowMap(req.State),
			TriedActions:   map[string]int{},
			CompletedSteps: []string{},
			FailedSteps:    []string{},
			ActionHistory:  []DeepAgentAction{},
		}
		if state.Goal == "" {
			return nil, fmt.Errorf("deep research goal is required")
		}
		state.WorkingMemory["user_id"] = firstNonEmptyString(deepAgentWorkflowString(state.WorkingMemory, "user_id"), req.UserID)
		state.WorkingMemory["session_id"] = firstNonEmptyString(deepAgentWorkflowString(state.WorkingMemory, "session_id"), req.SessionID)
		state.WorkingMemory["job_id"] = firstNonEmptyString(deepAgentWorkflowString(state.WorkingMemory, "job_id"), req.JobID, jobIDFromContext(ctx))
		if connectors := normalizeConnectorScopes(req.ConnectorContext); len(connectors) > 0 {
			state.WorkingMemory["connector_context"] = connectors
		}
		if !deepAgentRubricEmpty(state.Rubric) {
			state.WorkingMemory["rubric"] = state.Rubric
		}
		hydrateLoopContractWorkingMemory(state.WorkingMemory, contract)
		drRun = DeepResearchRunState{
			Version:    deepResearchWorkflowVersion,
			Status:     DeepResearchRunStatusRunning,
			Goal:       state.Goal,
			WorkerRuns: map[string]DeepResearchTaskNode{},
			StartedAt:  now,
			Config:     deepResearchConfigSnapshot(c.config),
		}
		c.persistDeepResearchState(ctx, run, state, drRun)
		emitDeepResearchEvent(ctx, "deep_research_started", req.SessionID, firstNonEmptyString(req.JobID, jobIDFromContext(ctx)), "Deep research orchestrator-worker run started", map[string]any{
			"event_group": "deep_research",
			"version":     deepResearchWorkflowVersion,
			"backend":     c.config.WorkerBackend,
		})
		return map[string]any{"deep_agent_state": state, "deep_research": drRun, "loop_contract": contract}, nil
	})
	engine.RegisterStepHandler("load_context", func(ctx context.Context, run *WorkflowRun, input map[string]any) (map[string]any, error) {
		loader := c.contextLoader
		if loader == nil {
			loader = noopDeepAgentContextLoader{}
		}
		loaded, err := loader.LoadDeepAgentContext(ctx, req, state)
		if err != nil {
			return nil, err
		}
		if state.WorkingMemory == nil {
			state.WorkingMemory = map[string]any{}
		}
		if loaded.EvidencePack.TokenBudget == 0 {
			loaded.EvidencePack = buildDeepAgentEvidencePack(loaded, state, deepAgentEvidencePackTokenBudget)
		}
		state.WorkingMemory[deepAgentLoadedContextKey] = loaded
		state.WorkingMemory[deepAgentEvidencePackKey] = loaded.EvidencePack
		req.State = cloneWorkflowMap(state.WorkingMemory)
		state.UpdatedAt = c.now()
		c.persistDeepResearchState(ctx, run, state, drRun)
		return map[string]any{
			"working_memory_keys":      len(state.WorkingMemory),
			"has_job_context":          firstNonEmptyString(req.JobID, jobIDFromContext(ctx)) != "",
			"message_count":            len(loaded.RecentMessages),
			"attachment_count":         len(loaded.Attachments),
			"artifact_count":           len(loaded.ExistingArtifacts),
			"skill_count":              len(loaded.SkillCatalog),
			"tool_count":               len(loaded.ToolCatalog),
			"memory_loaded":            strings.TrimSpace(loaded.MemorySummary) != "",
			"evidence_pack_tokens":     loaded.EvidencePack.TokenEstimate,
			"evidence_pack_budget":     loaded.EvidencePack.TokenBudget,
			"issue_count":              len(loaded.Issues),
			"deep_agent_context":       loaded,
			"deep_agent_evidence_pack": loaded.EvidencePack,
			"deep_agent_state":         state,
			"deep_research":            drRun,
		}, nil
	})
	engine.RegisterStepHandler("plan_deep_research", func(ctx context.Context, run *WorkflowRun, input map[string]any) (map[string]any, error) {
		plan, err := c.orchestrator.Plan(ctx, req, c.config)
		if err != nil {
			return nil, err
		}
		plan = normalizeDeepResearchPlan(plan, state.Goal, c.config)
		allowedTools, _ := deepResearchOrchestratorAllowedTools(req.State)
		if err := canonicalizeDeepResearchPlanAllowedTools(&plan, allowedTools); err != nil {
			return nil, err
		}
		if err := validateDeepResearchPlan(plan); err != nil {
			return nil, err
		}
		drRun.Plan = plan
		drRun.PlanRevision = 1
		drRun.WorkerRuns = map[string]DeepResearchTaskNode{}
		for _, node := range plan.Nodes {
			node.Status = DeepResearchTaskStatusPending
			drRun.WorkerRuns[node.ID] = node
		}
		state.Plan = deepAgentPlanFromDeepResearchPlan(plan)
		c.persistDeepResearchState(ctx, run, state, drRun)
		emitDeepResearchEvent(ctx, "deep_research_plan_created", req.SessionID, firstNonEmptyString(req.JobID, jobIDFromContext(ctx)), "Deep research task graph created", map[string]any{
			"event_group":     "deep_research",
			"plan_revision":   drRun.PlanRevision,
			"node_count":      len(plan.Nodes),
			"max_concurrency": plan.MaxConcurrency,
			"task_graph":      plan,
		})
		return map[string]any{"deep_agent_state": state, "deep_research": drRun, "deep_research_plan": plan}, nil
	})
	engine.RegisterStepHandler("execute_deep_research_workers", func(ctx context.Context, run *WorkflowRun, input map[string]any) (map[string]any, error) {
		err := c.executeWorkerGraph(ctx, run, req, state, &drRun)
		output := map[string]any{"deep_agent_state": state, "deep_research": drRun}
		if err != nil {
			return output, err
		}
		return output, nil
	})
	engine.RegisterStepHandler("aggregate_deep_research", func(ctx context.Context, run *WorkflowRun, input map[string]any) (map[string]any, error) {
		emitDeepResearchEvent(ctx, "deep_research_aggregate_started", req.SessionID, firstNonEmptyString(req.JobID, jobIDFromContext(ctx)), "Deep research aggregation started", map[string]any{"event_group": "deep_research"})
		result, err := c.aggregator.Aggregate(ctx, drRun)
		drRun.Aggregate = result
		if err != nil {
			drRun.Status = DeepResearchRunStatusFailed
			state.Status = DeepAgentRunStatusBlocked
			state.Blocker = err.Error()
			c.persistDeepResearchState(ctx, run, state, drRun)
			return map[string]any{"deep_agent_state": state, "deep_research": drRun, "deep_research_aggregate": result}, err
		}
		decision, err := c.decideDeepResearchDeliverable(ctx, req, state, drRun, result)
		result.Deliverable = decision
		drRun.Aggregate = result
		state.WorkingMemory["deep_research_deliverable"] = decision
		if err != nil {
			drRun.Status = DeepResearchRunStatusFailed
			state.Status = DeepAgentRunStatusBlocked
			state.Blocker = err.Error()
			drRun.Aggregate = result
			c.persistDeepResearchState(ctx, run, state, drRun)
			return map[string]any{"deep_agent_state": state, "deep_research": drRun, "deep_research_aggregate": result}, err
		}
		if deepResearchDecisionRequiresArtifact(decision) {
			ref, publishErr := c.publishDeepResearchArtifact(ctx, req, state, drRun, result, decision)
			if publishErr != nil {
				err := fmt.Errorf("deep research deliverable artifact required but not created: %w", publishErr)
				drRun.Status = DeepResearchRunStatusFailed
				state.Status = DeepAgentRunStatusBlocked
				state.Blocker = err.Error()
				drRun.Aggregate = result
				c.persistDeepResearchState(ctx, run, state, drRun)
				return map[string]any{"deep_agent_state": state, "deep_research": drRun, "deep_research_aggregate": result}, err
			}
			result.Artifacts = dedupeDeepResearchArtifacts(append(result.Artifacts, ref))
			state.WorkingMemory["final_artifact_refs"] = result.Artifacts
			state.WorkingMemory["deep_research_artifact_refs"] = result.Artifacts
			drRun.Aggregate = result
		}
		if result.Partial {
			drRun.Status = DeepResearchRunStatusPartial
		} else {
			drRun.Status = DeepResearchRunStatusSucceeded
		}
		now := c.now()
		drRun.CompletedAt = &now
		state.Status = DeepAgentRunStatusSucceeded
		state.UpdatedAt = now
		state.WorkingMemory["deep_research_final_answer"] = result.FinalAnswer
		if result.Partial {
			state.WorkingMemory["deep_research_partial"] = true
		}
		c.evidence.PutStepEvidence(state, DeepAgentStepEvidence{
			StepID:    "deep_research_aggregate",
			Output:    result.FinalAnswer,
			Summary:   result.Summary,
			Sources:   result.Sources,
			Artifacts: result.Artifacts,
			Diagnostics: map[string]any{
				"deep_research": true,
				"partial":       result.Partial,
				"errors":        result.Errors,
			},
		})
		c.persistDeepResearchState(ctx, run, state, drRun)
		emitDeepResearchEvent(ctx, "deep_research_completed", req.SessionID, firstNonEmptyString(req.JobID, jobIDFromContext(ctx)), firstNonEmptyString(result.Summary, "Deep research completed"), map[string]any{
			"event_group":    "deep_research",
			"status":         drRun.Status,
			"partial":        result.Partial,
			"source_count":   len(result.Sources),
			"artifact_count": len(result.Artifacts),
			"worker_count":   len(result.WorkerResults),
			"deliverable":    decision,
			"aggregate":      result,
		})
		return map[string]any{"deep_agent_state": state, "deep_research": drRun, "deep_research_aggregate": result}, nil
	})

	run, err := engine.Execute(ctx, WorkflowRequest{
		Definition: deepResearchWorkflowDefinition(c.config.TotalTimeout),
		UserID:     req.UserID,
		SessionID:  req.SessionID,
		JobID:      firstNonEmptyString(req.JobID, jobIDFromContext(ctx)),
		State: map[string]any{
			"goal":                  strings.TrimSpace(req.Goal),
			"request_id":            requestIDFromContext(ctx),
			"loop_contract":         contract,
			"loop_contract_id":      contract.ID,
			"loop_contract_version": contract.Version,
			"deep_research_version": deepResearchWorkflowVersion,
		},
		Recoverable: true,
	})
	result := &DeepAgentTaskResult{Run: run, State: state}
	if run != nil {
		steps, stepErr := c.store.ListWorkflowStepRuns(ctx, run.ID)
		if stepErr == nil {
			result.Steps = steps
		}
	}
	if err != nil {
		result.Error = err.Error()
		return result, err
	}
	return result, nil
}

func deepResearchWorkflowDefinition(workerGraphTimeout time.Duration) WorkflowDefinition {
	return WorkflowDefinition{
		Name:    deepAgentTaskWorkflowName,
		Version: deepResearchWorkflowVersion,
		Steps: []WorkflowStepDefinition{
			{Name: "initialize_task"},
			{Name: "load_context"},
			{Name: "plan_deep_research"},
			{Name: "execute_deep_research_workers", Timeout: workerGraphTimeout},
			{Name: "aggregate_deep_research"},
		},
	}
}

func (c *DeepResearchController) executeWorkerGraph(ctx context.Context, run *WorkflowRun, req DeepAgentTaskRequest, state *DeepAgentState, drRun *DeepResearchRunState) error {
	if drRun == nil {
		return fmt.Errorf("deep research run state is required")
	}
	if len(drRun.Plan.Nodes) == 0 {
		return fmt.Errorf("deep research plan has no nodes")
	}
	if drRun.PlanRevision <= 0 {
		drRun.PlanRevision = 1
	}
	if strings.TrimSpace(drRun.Version) == "" || drRun.Version != deepResearchWorkflowVersion {
		drRun.Version = deepResearchWorkflowVersion
	}
	ctx, cancel := context.WithTimeout(ctx, c.config.TotalTimeout)
	defer cancel()
	completedBatches := 0
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if allDeepResearchTasksTerminal(drRun.WorkerRuns) {
			if reason, ok := shouldDeepResearchReplanForConfig(*drRun, c.config); ok {
				trigger := DeepResearchReplanTrigger{
					Kind:       reason,
					Reason:     deepResearchReplanTriggerDescription(reason, *drRun),
					Batch:      completedBatches,
					NodeIDs:    deepResearchReplanTriggerNodeIDs(reason, *drRun),
					Hard:       true,
					OccurredAt: c.now(),
				}
				if c.maybeReplan(ctx, run, req, state, drRun, trigger) {
					continue
				}
			}
			break
		}
		ready := c.readyDeepResearchNodes(drRun)
		if len(ready) == 0 {
			blocked := markDeepResearchDependencyBlocks(drRun)
			if blocked > 0 {
				c.persistDeepResearchState(ctx, run, state, *drRun)
				continue
			}
			if allDeepResearchTasksTerminal(drRun.WorkerRuns) {
				continue
			}
			trigger := DeepResearchReplanTrigger{
				Kind:       DeepResearchReplanReasonSchedulerStalled,
				Reason:     "no runnable node remains while unfinished tasks still exist",
				Batch:      completedBatches,
				Hard:       true,
				OccurredAt: c.now(),
			}
			if c.maybeReplan(ctx, run, req, state, drRun, trigger) {
				continue
			}
			return fmt.Errorf("deep research scheduler made no progress")
		}
		limit := c.config.MaxConcurrency
		if planned := drRun.Plan.MaxConcurrency; planned > 0 && (limit <= 0 || planned < limit) {
			limit = planned
		}
		if limit <= 0 || limit > len(ready) {
			limit = len(ready)
		}
		batch := ready[:limit]
		var wg sync.WaitGroup
		var mu sync.Mutex
		type workerInput struct {
			node             DeepResearchTaskNode
			goal             string
			dependencyOutput []DeepResearchWorkerResult
			workingMemory    map[string]any
		}
		inputs := make([]workerInput, 0, len(batch))
		// Snapshot every worker input before any goroutine starts. WorkingMemory
		// contains the live deep-research run, so cloning it after the first worker
		// starts would race with updates to drRun.WorkerRuns.
		for _, node := range batch {
			inputs = append(inputs, workerInput{
				node:             node,
				goal:             drRun.Goal,
				dependencyOutput: deepResearchDependencyResults(*drRun, node),
				workingMemory:    cloneWorkflowMap(state.WorkingMemory),
			})
		}
		for _, input := range inputs {
			input := input
			wg.Add(1)
			go func() {
				defer wg.Done()
				updated := c.executeSingleWorker(ctx, req, input.goal, input.dependencyOutput, input.workingMemory, input.node)
				mu.Lock()
				drRun.WorkerRuns[updated.ID] = updated
				for idx := range drRun.Plan.Nodes {
					if drRun.Plan.Nodes[idx].ID == updated.ID {
						drRun.Plan.Nodes[idx] = updated
						break
					}
				}
				mu.Unlock()
			}()
		}
		wg.Wait()
		completedBatches++
		for _, node := range batch {
			updated := drRun.WorkerRuns[node.ID]
			if updated.Status == DeepResearchTaskStatusSucceeded && updated.Result != nil {
				c.evidence.PutStepEvidence(state, deepResearchEvidenceFromWorker(updated, *updated.Result))
			}
		}
		c.syncDeepAgentPlanStatuses(state, drRun)
		c.persistDeepResearchState(ctx, run, state, *drRun)
		reason, hard := shouldDeepResearchReplanForConfig(*drRun, c.config)
		if !hard && c.config.ReplanEnabled && c.config.ReplanEveryBatches > 0 && completedBatches%c.config.ReplanEveryBatches == 0 && !allDeepResearchTasksTerminal(drRun.WorkerRuns) && !deepResearchHasRetryableFailure(drRun.WorkerRuns) {
			reason = DeepResearchReplanReasonBatchCompleted
		}
		if strings.TrimSpace(reason) != "" {
			trigger := DeepResearchReplanTrigger{
				Kind:       reason,
				Reason:     deepResearchReplanTriggerDescription(reason, *drRun),
				Batch:      completedBatches,
				NodeIDs:    deepResearchReplanTriggerNodeIDs(reason, *drRun),
				Hard:       hard,
				OccurredAt: c.now(),
			}
			if c.maybeReplan(ctx, run, req, state, drRun, trigger) {
				continue
			}
		}
	}
	successes, requiredFailures := countDeepResearchWorkerOutcomes(drRun.WorkerRuns)
	if successes < c.config.MinSuccessfulWorkers {
		return fmt.Errorf("deep research successful workers %d below required minimum %d", successes, c.config.MinSuccessfulWorkers)
	}
	if requiredFailures > 0 {
		return fmt.Errorf("deep research required workers failed: %d", requiredFailures)
	}
	return nil
}

func (c *DeepResearchController) maybeReplan(ctx context.Context, run *WorkflowRun, req DeepAgentTaskRequest, state *DeepAgentState, drRun *DeepResearchRunState, trigger DeepResearchReplanTrigger) bool {
	if c == nil || drRun == nil || !c.config.ReplanEnabled {
		return false
	}
	replanner, ok := c.orchestrator.(DeepResearchReplanner)
	if !ok || replanner == nil {
		return false
	}
	attemptLimit := c.config.MaxReplans
	if !trigger.Hard {
		// Keep one model call in reserve for a terminal failure/evidence gap so
		// periodic checkpoints cannot consume the entire recovery budget.
		attemptLimit--
	}
	if attemptLimit < 0 {
		attemptLimit = 0
	}
	if drRun.ReplanAttempts >= attemptLimit {
		emitDeepResearchEvent(ctx, "deep_research_replan_limit_reached", req.SessionID, firstNonEmptyString(req.JobID, jobIDFromContext(ctx)), "Deep research replan limit reached", map[string]any{
			"event_group":     "deep_research",
			"plan_revision":   drRun.PlanRevision,
			"replan_attempts": drRun.ReplanAttempts,
			"max_replans":     c.config.MaxReplans,
			"trigger":         trigger,
		})
		return false
	}
	if trigger.OccurredAt.IsZero() {
		trigger.OccurredAt = c.now()
	}
	drRun.ReplanAttempts++
	drRun.LastReplanReason = trigger.Kind
	attempt := drRun.ReplanAttempts
	fromRevision := drRun.PlanRevision
	c.persistDeepResearchState(ctx, run, state, *drRun)
	emitDeepResearchEvent(ctx, "deep_research_replan_started", req.SessionID, firstNonEmptyString(req.JobID, jobIDFromContext(ctx)), "Deep research execution checkpoint is revising the remaining plan", map[string]any{
		"event_group":   "deep_research",
		"attempt":       attempt,
		"from_revision": fromRevision,
		"trigger":       trigger,
	})

	callReq := req
	callReq.State = cloneWorkflowMap(state.WorkingMemory)
	callReq.State["deep_research"] = *drRun
	plan, err := replanner.Replan(ctx, callReq, *drRun, trigger, c.config)
	record := DeepResearchReplanRecord{
		Attempt:      attempt,
		FromRevision: fromRevision,
		ToRevision:   fromRevision,
		Trigger:      trigger,
		Reason:       trigger.Kind,
		PreviousPlan: drRun.Plan,
		CreatedAt:    c.now(),
	}
	if err != nil {
		record.Error = err.Error()
		drRun.ReplanHistory = append(drRun.ReplanHistory, record)
		c.persistDeepResearchState(ctx, run, state, *drRun)
		emitDeepResearchEvent(ctx, "deep_research_replan_failed", req.SessionID, firstNonEmptyString(req.JobID, jobIDFromContext(ctx)), "Deep research kept the current plan because replanning failed", map[string]any{
			"event_group":   "deep_research",
			"attempt":       attempt,
			"plan_revision": fromRevision,
			"trigger":       trigger,
			"error":         err.Error(),
		})
		return false
	}

	candidate := *drRun
	candidate.WorkerRuns = make(map[string]DeepResearchTaskNode, len(drRun.WorkerRuns))
	for id, node := range drRun.WorkerRuns {
		candidate.WorkerRuns[id] = node
	}
	proposal := DeepResearchReplan{
		Revision: fromRevision + 1,
		Reason:   trigger.Kind,
		Plan:     plan,
	}
	allowedTools, _ := deepResearchOrchestratorAllowedTools(callReq.State)
	if err := applyDeepResearchReplan(&candidate, proposal, allowedTools); err != nil {
		record.Error = err.Error()
		record.Plan = plan
		drRun.ReplanHistory = append(drRun.ReplanHistory, record)
		c.persistDeepResearchState(ctx, run, state, *drRun)
		emitDeepResearchEvent(ctx, "deep_research_replan_failed", req.SessionID, firstNonEmptyString(req.JobID, jobIDFromContext(ctx)), "Deep research rejected an invalid replacement plan", map[string]any{
			"event_group":   "deep_research",
			"attempt":       attempt,
			"plan_revision": fromRevision,
			"trigger":       trigger,
			"error":         err.Error(),
		})
		return false
	}
	if len(candidate.Plan.Nodes) > c.config.MaxWorkers {
		err := fmt.Errorf("deep research replan has %d active nodes, maximum is %d", len(candidate.Plan.Nodes), c.config.MaxWorkers)
		record.Error = err.Error()
		record.Plan = candidate.Plan
		drRun.ReplanHistory = append(drRun.ReplanHistory, record)
		c.persistDeepResearchState(ctx, run, state, *drRun)
		emitDeepResearchEvent(ctx, "deep_research_replan_failed", req.SessionID, firstNonEmptyString(req.JobID, jobIDFromContext(ctx)), "Deep research rejected an oversized replacement plan", map[string]any{
			"event_group": "deep_research",
			"attempt":     attempt,
			"trigger":     trigger,
			"error":       err.Error(),
		})
		return false
	}
	if trigger.Hard && !deepResearchHasUnfinishedNode(candidate.WorkerRuns) {
		err := fmt.Errorf("deep research hard replan did not add a runnable recovery path")
		record.Error = err.Error()
		record.Plan = candidate.Plan
		drRun.ReplanHistory = append(drRun.ReplanHistory, record)
		c.persistDeepResearchState(ctx, run, state, *drRun)
		emitDeepResearchEvent(ctx, "deep_research_replan_failed", req.SessionID, firstNonEmptyString(req.JobID, jobIDFromContext(ctx)), "Deep research rejected a replacement without a recovery path", map[string]any{
			"event_group": "deep_research",
			"attempt":     attempt,
			"trigger":     trigger,
			"error":       err.Error(),
		})
		return false
	}
	if equivalentDeepResearchPlans(drRun.Plan, candidate.Plan) {
		record.Plan = candidate.Plan
		drRun.ReplanHistory = append(drRun.ReplanHistory, record)
		c.persistDeepResearchState(ctx, run, state, *drRun)
		emitDeepResearchEvent(ctx, "deep_research_replan_skipped", req.SessionID, firstNonEmptyString(req.JobID, jobIDFromContext(ctx)), "Deep research kept the current remaining plan", map[string]any{
			"event_group":   "deep_research",
			"attempt":       attempt,
			"plan_revision": fromRevision,
			"trigger":       trigger,
		})
		return false
	}

	candidate.ReplanCount = drRun.ReplanCount + 1
	record.Changed = true
	record.ToRevision = candidate.PlanRevision
	record.Plan = candidate.Plan
	candidate.ReplanHistory = append(candidate.ReplanHistory, record)
	*drRun = candidate
	state.Plan = deepAgentPlanFromDeepResearchPlan(drRun.Plan)
	c.syncDeepAgentPlanStatuses(state, drRun)
	c.persistDeepResearchState(ctx, run, state, *drRun)
	emitDeepResearchEvent(ctx, "deep_research_replan_applied", req.SessionID, firstNonEmptyString(req.JobID, jobIDFromContext(ctx)), "Deep research applied a revised remaining task graph", map[string]any{
		"event_group":   "deep_research",
		"attempt":       attempt,
		"from_revision": fromRevision,
		"to_revision":   drRun.PlanRevision,
		"replan_count":  drRun.ReplanCount,
		"node_count":    len(drRun.Plan.Nodes),
		"trigger":       trigger,
		"task_graph":    drRun.Plan,
	})
	return true
}

func (c *DeepResearchController) executeSingleWorker(ctx context.Context, req DeepAgentTaskRequest, goal string, dependencyOutput []DeepResearchWorkerResult, workingMemory map[string]any, node DeepResearchTaskNode) DeepResearchTaskNode {
	now := c.now()
	node.Status = DeepResearchTaskStatusRunning
	node.Attempt++
	node.StartedAt = &now
	node.LastHeartbeatAt = &now
	emitDeepResearchEvent(ctx, "deep_research_worker_started", req.SessionID, firstNonEmptyString(req.JobID, jobIDFromContext(ctx)), firstNonEmptyString(node.Title, node.ID), map[string]any{
		"event_group":  "deep_research",
		"worker_id":    node.ID,
		"worker_title": node.Title,
		"worker_role":  node.WorkerRole,
		"attempt":      node.Attempt,
		"node":         node,
	})
	timeout := c.config.WorkerTimeout
	if node.TimeoutMS > 0 {
		timeout = time.Duration(node.TimeoutMS) * time.Millisecond
	}
	workerCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	result, err := c.worker.ExecuteWorker(workerCtx, DeepResearchWorkerInput{
		UserID:           req.UserID,
		SessionID:        req.SessionID,
		JobID:            firstNonEmptyString(req.JobID, jobIDFromContext(ctx)),
		Goal:             goal,
		Node:             node,
		DependencyOutput: dependencyOutput,
		ConnectorContext: normalizeConnectorScopes(req.ConnectorContext),
		WorkingMemory:    workingMemory,
		Backend:          c.config.WorkerBackend,
	})
	completed := c.now()
	node.CompletedAt = &completed
	if err != nil {
		result.Status = DeepAgentActionStatusFailed
		result.Errors = append(result.Errors, err.Error())
		node.Error = err.Error()
	}
	node.AgentRunID = firstNonEmptyString(result.AgentRunID, fmt.Sprintf("worker-%s-attempt-%d", node.ID, node.Attempt))
	node.Result = &result
	if err != nil || result.Status == DeepAgentActionStatusFailed {
		maxAttempts := node.MaxAttempts
		if maxAttempts <= 0 {
			maxAttempts = c.config.MaxRetries + 1
		}
		if maxAttempts <= 0 {
			maxAttempts = 1
		}
		if node.Attempt < maxAttempts {
			node.Status = DeepResearchTaskStatusRetrying
			emitDeepResearchEvent(ctx, "deep_research_worker_retrying", req.SessionID, firstNonEmptyString(req.JobID, jobIDFromContext(ctx)), firstNonEmptyString(node.Error, "Deep research worker retrying"), map[string]any{
				"event_group":  "deep_research",
				"worker_id":    node.ID,
				"worker_title": node.Title,
				"attempt":      node.Attempt,
				"error":        node.Error,
			})
			node.Status = DeepResearchTaskStatusPending
			return node
		}
		node.Status = DeepResearchTaskStatusFailedFinal
		emitDeepResearchEvent(ctx, "deep_research_worker_failed", req.SessionID, firstNonEmptyString(req.JobID, jobIDFromContext(ctx)), firstNonEmptyString(node.Error, "Deep research worker failed"), map[string]any{
			"event_group":  "deep_research",
			"worker_id":    node.ID,
			"worker_title": node.Title,
			"attempt":      node.Attempt,
			"error":        node.Error,
			"result":       result,
		})
		return node
	}
	node.Status = DeepResearchTaskStatusSucceeded
	emitDeepResearchEvent(ctx, "deep_research_worker_succeeded", req.SessionID, firstNonEmptyString(req.JobID, jobIDFromContext(ctx)), firstNonEmptyString(result.Summary, node.Title), map[string]any{
		"event_group":     "deep_research",
		"worker_id":       node.ID,
		"worker_title":    node.Title,
		"worker_role":     node.WorkerRole,
		"attempt":         node.Attempt,
		"agent_run_id":    node.AgentRunID,
		"source_count":    len(result.Sources),
		"artifact_count":  len(result.Artifacts),
		"tool_call_count": len(result.ToolCalls),
		"result":          result,
	})
	return node
}

func (c *DeepResearchController) readyDeepResearchNodes(run *DeepResearchRunState) []DeepResearchTaskNode {
	if run == nil {
		return nil
	}
	out := []DeepResearchTaskNode{}
	for _, node := range run.WorkerRuns {
		switch node.Status {
		case "", DeepResearchTaskStatusPending:
			if deepResearchDependenciesSucceeded(run.WorkerRuns, node) {
				node.Status = DeepResearchTaskStatusReady
				run.WorkerRuns[node.ID] = node
				out = append(out, node)
			}
		case DeepResearchTaskStatusReady:
			if deepResearchDependenciesSucceeded(run.WorkerRuns, node) {
				out = append(out, node)
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (c *DeepResearchController) persistDeepResearchState(ctx context.Context, run *WorkflowRun, state *DeepAgentState, drRun DeepResearchRunState) {
	if run == nil || state == nil {
		return
	}
	if state.WorkingMemory == nil {
		state.WorkingMemory = map[string]any{}
	}
	state.WorkingMemory["deep_research"] = drRun
	state.WorkingMemory["deep_research_version"] = drRun.Version
	run.State = cloneWorkflowMap(run.State)
	if run.State == nil {
		run.State = map[string]any{}
	}
	run.State["deep_agent_state"] = state
	run.State["deep_research"] = drRun
	run.State["deep_research_version"] = drRun.Version
	run.UpdatedAt = c.now()
	_ = c.store.UpdateWorkflowRun(ctx, run)
}

func (c *DeepResearchController) syncDeepAgentPlanStatuses(state *DeepAgentState, drRun *DeepResearchRunState) {
	if state == nil || drRun == nil {
		return
	}
	completed := []string{}
	failed := []string{}
	for idx := range state.Plan.Steps {
		node, ok := drRun.WorkerRuns[state.Plan.Steps[idx].ID]
		if !ok {
			continue
		}
		switch node.Status {
		case DeepResearchTaskStatusSucceeded:
			state.Plan.Steps[idx].Status = DeepAgentStepStatusSucceeded
			completed = append(completed, node.ID)
		case DeepResearchTaskStatusFailedFinal, DeepResearchTaskStatusBlockedByDependency:
			state.Plan.Steps[idx].Status = DeepAgentStepStatusFailed
			failed = append(failed, node.ID)
		case DeepResearchTaskStatusRunning, DeepResearchTaskStatusReady:
			state.Plan.Steps[idx].Status = DeepAgentStepStatusRunning
		default:
			state.Plan.Steps[idx].Status = DeepAgentStepStatusPending
		}
	}
	state.CompletedSteps = completed
	state.FailedSteps = failed
	state.ActionCount = deepResearchActionCount(drRun.WorkerRuns)
	state.UpdatedAt = c.now()
}

func (c *DeepResearchController) now() time.Time {
	if c == nil || c.clock == nil {
		return time.Now().UTC()
	}
	return c.clock.Now().UTC()
}
