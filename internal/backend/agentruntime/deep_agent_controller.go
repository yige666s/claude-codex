package agentruntime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

var (
	ErrDeepAgentBlocked        = errors.New("deep agent task blocked")
	ErrDeepAgentBudgetExceeded = errors.New("deep agent budget exceeded")
	ErrDeepAgentReviewRequired = errors.New("deep agent action requires human review")
)

type DeepAgentController struct {
	store         WorkflowStore
	events        WorkflowEventSink
	planner       DeepAgentPlanner
	executor      DeepAgentExecutor
	verifier      DeepAgentVerifier
	evaluator     DeepAgentEvaluator
	contextLoader DeepAgentContextLoader
	evidenceStore DeepAgentEvidenceStore
	evidenceRepo  DeepAgentEvidenceRepository
	riskGate      DeepAgentRiskGate
	learningSink  DeepAgentLearningSink
	clock         Clock
}

func NewDeepAgentController(store WorkflowStore, events WorkflowEventSink, planner DeepAgentPlanner, executor DeepAgentExecutor, verifier DeepAgentVerifier) *DeepAgentController {
	if store == nil {
		store = NewMemoryWorkflowStore()
	}
	if events == nil {
		events = NoopWorkflowEventSink{}
	}
	if planner == nil {
		planner = ruleDeepAgentPlanner{}
	}
	if executor == nil {
		executor = noopDeepAgentExecutor{}
	}
	if verifier == nil {
		verifier = ruleDeepAgentVerifier{}
	}
	return &DeepAgentController{
		store:         store,
		events:        events,
		planner:       planner,
		executor:      executor,
		verifier:      verifier,
		evaluator:     newRuleDeepAgentEvaluator(verifier),
		contextLoader: noopDeepAgentContextLoader{},
		evidenceStore: deepAgentDefaultEvidenceStore(),
		clock:         systemClock{},
	}
}

func (c *DeepAgentController) SetContextLoader(loader DeepAgentContextLoader) {
	if c != nil && loader != nil {
		c.contextLoader = loader
	}
}

func (c *DeepAgentController) SetEvaluator(evaluator DeepAgentEvaluator) {
	if c != nil && evaluator != nil {
		c.evaluator = evaluator
	}
}

func (c *DeepAgentController) SetEvidenceStore(store DeepAgentEvidenceStore) {
	if c != nil && store != nil {
		c.evidenceStore = store
	}
}

func (c *DeepAgentController) SetEvidenceRepository(repo DeepAgentEvidenceRepository) {
	if c != nil {
		c.evidenceRepo = repo
	}
}

func (c *DeepAgentController) SetRiskGate(gate DeepAgentRiskGate) {
	if c != nil {
		c.riskGate = gate
	}
}

func (c *DeepAgentController) SetLearningSink(sink DeepAgentLearningSink) {
	if c != nil {
		c.learningSink = sink
	}
}

func (c *DeepAgentController) Execute(ctx context.Context, req DeepAgentTaskRequest) (*DeepAgentTaskResult, error) {
	if c == nil {
		return nil, fmt.Errorf("deep agent controller is not configured")
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
	req.State["loop_goal_id"] = firstNonEmptyString(deepAgentWorkflowString(req.State, "loop_goal_id"), contract.ID)
	engine := NewWorkflowEngine(c.store, c.events)
	var state *DeepAgentState
	engine.RegisterStepHandler("initialize_task", func(ctx context.Context, run *WorkflowRun, input map[string]any) (map[string]any, error) {
		now := c.now()
		state = &DeepAgentState{
			Goal:           strings.TrimSpace(req.Goal),
			Rubric:         normalizeDeepAgentRubric(req.Rubric),
			LoopContract:   contract,
			Handoff:        LoopHandoff{},
			TriedActions:   map[string]int{},
			Status:         DeepAgentRunStatusRunning,
			StartedAt:      now,
			UpdatedAt:      now,
			WorkingMemory:  cloneWorkflowMap(req.State),
			CompletedSteps: []string{},
			FailedSteps:    []string{},
			ActionHistory:  []DeepAgentAction{},
		}
		if state.Goal == "" {
			return nil, fmt.Errorf("deep agent goal is required")
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
		updateDeepAgentHandoff(state, now)
		emitJobEventFromContext(ctx, Event{
			Type:      "deep_agent_loop_contract",
			SessionID: req.SessionID,
			JobID:     firstNonEmptyString(req.JobID, jobIDFromContext(ctx)),
			Role:      "workflow",
			Content:   "Loop contract established",
			Data: deepAgentEventData(map[string]any{
				"event_group":           "run",
				"loop_contract":         contract,
				"loop_contract_id":      contract.ID,
				"loop_contract_version": contract.Version,
				"task_type":             contract.TaskType,
				"deliverable":           contract.Deliverable,
				"budget":                contract.Budget,
				"stop_policy":           contract.StopPolicy,
			}),
		})
		c.persistState(ctx, run, state)
		return map[string]any{
			"goal":                  state.Goal,
			"deep_agent_state":      state,
			"loop_contract":         contract,
			"loop_contract_id":      contract.ID,
			"loop_contract_version": contract.Version,
			"max_steps":             req.Policy.MaxSteps,
			"max_actions":           req.Policy.MaxActions,
			"max_duration_ms":       req.Policy.MaxDuration.Milliseconds(),
		}, nil
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
		c.persistState(ctx, run, state)
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
		}, nil
	})
	engine.RegisterStepHandler("plan_task", func(ctx context.Context, run *WorkflowRun, input map[string]any) (map[string]any, error) {
		plan := req.Plan
		if len(plan.Steps) == 0 {
			var err error
			plan, err = c.planner.CreatePlan(ctx, req)
			if err != nil {
				return nil, err
			}
		}
		plan = normalizeDeepAgentPlan(state.Goal, plan)
		if len(plan.Steps) == 0 {
			return nil, fmt.Errorf("deep agent plan has no steps")
		}
		state.Plan = plan
		decision := c.planGateDecision(state, plan, req.Policy)
		c.recordGateDecision(ctx, run, state, decision)
		if !decision.Allow {
			state.Status = DeepAgentRunStatusBudgetExceeded
			if decision.RequiresReview {
				state.Status = DeepAgentRunStatusReviewPending
			} else if decision.Category != "budget" {
				state.Status = DeepAgentRunStatusBlocked
			}
			state.Blocker = decision.BlockReason
			c.persistState(ctx, run, state)
			if decision.RequiresReview {
				return map[string]any{"deep_agent_state": state, "gate_decision": decision}, fmt.Errorf("%w: %s", ErrDeepAgentReviewRequired, state.Blocker)
			}
			if decision.Category == "budget" {
				return map[string]any{"deep_agent_state": state, "gate_decision": decision}, fmt.Errorf("%w: %s", ErrDeepAgentBudgetExceeded, state.Blocker)
			}
			return map[string]any{"deep_agent_state": state, "gate_decision": decision}, fmt.Errorf("%w: %s", ErrDeepAgentBlocked, state.Blocker)
		}
		plannedConnectors := deepAgentPlannedConnectors(req, state, plan)
		if len(plannedConnectors) > 0 {
			state.WorkingMemory["planned_connectors"] = plannedConnectors
			emitJobEventFromContext(ctx, Event{
				Type:      "deep_agent_connectors_planned",
				SessionID: req.SessionID,
				JobID:     firstNonEmptyString(req.JobID, jobIDFromContext(ctx)),
				Role:      "workflow",
				Content:   "DeepAgent will use connector context",
				Data:      deepAgentEventData(map[string]any{"connectors": plannedConnectors, "connector_context": normalizeConnectorScopes(req.ConnectorContext)}),
			})
		}
		state.UpdatedAt = c.now()
		c.persistState(ctx, run, state)
		return map[string]any{
			"planned_step_count": len(plan.Steps),
			"planned_connectors": plannedConnectors,
			"deep_agent_state":   state,
		}, nil
	})
	engine.RegisterStepHandler("execute_controller_loop", func(ctx context.Context, run *WorkflowRun, input map[string]any) (map[string]any, error) {
		err := c.executeLoop(ctx, run, state, req.Policy)
		output := map[string]any{
			"action_count":       state.ActionCount,
			"completed_count":    len(state.CompletedSteps),
			"failed_count":       len(state.FailedSteps),
			"no_progress_count":  state.NoProgressCount,
			"deep_agent_status":  state.Status,
			"deep_agent_blocker": state.Blocker,
			"deep_agent_state":   state,
		}
		if err != nil {
			return output, err
		}
		return output, nil
	})
	engine.RegisterStepHandler("verify_final_result", func(ctx context.Context, run *WorkflowRun, input map[string]any) (map[string]any, error) {
		return c.evaluateFinalResult(ctx, run, state)
	})
	engine.RegisterStepHandler("persist_learnings", func(ctx context.Context, run *WorkflowRun, input map[string]any) (map[string]any, error) {
		candidates := c.buildLearningCandidates(run, state)
		if err := c.persistLearnings(ctx, run, state, candidates); err != nil {
			return nil, err
		}
		return map[string]any{
			"learning_candidate_count": len(candidates),
			"deep_agent_learnings":     state.Learnings,
			"deep_agent_state":         state,
		}, nil
	})

	run, err := engine.Execute(ctx, WorkflowRequest{
		Definition: deepAgentTaskWorkflowDefinition(req.Policy.StepTimeout),
		UserID:     req.UserID,
		SessionID:  req.SessionID,
		JobID:      firstNonEmptyString(req.JobID, jobIDFromContext(ctx)),
		State: map[string]any{
			"goal":                  strings.TrimSpace(req.Goal),
			"request_id":            requestIDFromContext(ctx),
			"loop_contract":         contract,
			"loop_contract_id":      contract.ID,
			"loop_contract_version": contract.Version,
			"task_type":             contract.TaskType,
			"deliverable":           contract.Deliverable,
		},
	})
	result := &DeepAgentTaskResult{Run: run, State: state}
	if run != nil {
		steps, stepErr := c.store.ListWorkflowStepRuns(ctx, run.ID)
		if stepErr == nil {
			result.Steps = steps
		}
	}
	if err != nil {
		c.persistFailedLearnings(ctx, run, state)
		result.Error = err.Error()
		return result, err
	}
	return result, nil
}

func (c *DeepAgentController) Resume(ctx context.Context, req DeepAgentResumeRequest) (*DeepAgentTaskResult, error) {
	if c == nil {
		return nil, fmt.Errorf("deep agent controller is not configured")
	}
	runID := strings.TrimSpace(req.RunID)
	if runID == "" {
		return nil, fmt.Errorf("deep agent workflow run id is required")
	}
	run, err := c.store.GetWorkflowRun(ctx, runID)
	if err != nil {
		return nil, err
	}
	if run.Name != deepAgentTaskWorkflowName {
		return nil, fmt.Errorf("workflow run %s is not a deep agent task", runID)
	}
	state, err := deepAgentStateFromWorkflowRun(run)
	if err != nil {
		return &DeepAgentTaskResult{Run: run, Error: err.Error()}, err
	}
	c.prepareStateForResume(req, state)
	policy := c.resumePolicyForState(req, state)
	now := c.now()
	run.Status = WorkflowStatusRunning
	run.Error = ""
	run.UpdatedAt = now
	run.FinishedAt = nil
	if run.StartedAt == nil {
		run.StartedAt = &now
	}
	c.persistState(ctx, run, state)

	loopErr := c.recordDeepAgentResumeStep(ctx, run, "resume_controller_loop", func(step *WorkflowStepRun) (map[string]any, error) {
		err := c.executeLoop(ctx, run, state, policy)
		output := map[string]any{
			"action_count":       state.ActionCount,
			"completed_count":    len(state.CompletedSteps),
			"failed_count":       len(state.FailedSteps),
			"no_progress_count":  state.NoProgressCount,
			"deep_agent_status":  state.Status,
			"deep_agent_blocker": state.Blocker,
			"deep_agent_state":   state,
		}
		return output, err
	})
	if loopErr != nil {
		return c.finishResume(ctx, run, state, loopErr)
	}
	verifyErr := c.recordDeepAgentResumeStep(ctx, run, "resume_verify_final_result", func(step *WorkflowStepRun) (map[string]any, error) {
		return c.evaluateFinalResult(ctx, run, state)
	})
	if verifyErr != nil {
		return c.finishResume(ctx, run, state, verifyErr)
	}
	learnErr := c.recordDeepAgentResumeStep(ctx, run, "resume_persist_learnings", func(step *WorkflowStepRun) (map[string]any, error) {
		candidates := c.buildLearningCandidates(run, state)
		if err := c.persistLearnings(ctx, run, state, candidates); err != nil {
			return nil, err
		}
		return map[string]any{
			"learning_candidate_count": len(candidates),
			"deep_agent_learnings":     state.Learnings,
			"deep_agent_state":         state,
		}, nil
	})
	if learnErr != nil {
		return c.finishResume(ctx, run, state, learnErr)
	}
	return c.finishResume(ctx, run, state, nil)
}

func deepAgentTaskWorkflowDefinition(stepTimeout time.Duration) WorkflowDefinition {
	return WorkflowDefinition{
		Name:    deepAgentTaskWorkflowName,
		Version: deepAgentTaskWorkflowVersion,
		Steps: []WorkflowStepDefinition{
			{Name: "initialize_task"},
			{Name: "load_context"},
			{Name: "plan_task"},
			{Name: "execute_controller_loop", Timeout: stepTimeout},
			{Name: "verify_final_result"},
			{Name: "persist_learnings"},
		},
	}
}

func (c *DeepAgentController) prepareStateForResume(req DeepAgentResumeRequest, state *DeepAgentState) {
	now := c.now()
	state.Status = DeepAgentRunStatusRunning
	state.Blocker = ""
	state.NoProgressCount = 0
	if state.StartedAt.IsZero() {
		state.StartedAt = now
	}
	state.UpdatedAt = now
	if state.TriedActions == nil {
		state.TriedActions = map[string]int{}
	}
	if state.WorkingMemory == nil {
		state.WorkingMemory = map[string]any{}
	}
	for key, value := range req.StatePatch {
		state.WorkingMemory[key] = value
	}
	if !loopHandoffEmpty(req.HandoffPatch) {
		state.Handoff = mergeDeepAgentHandoffPatch(state.Handoff, req.HandoffPatch)
		state.WorkingMemory["loop_handoff"] = state.Handoff
	}
	if raw := state.WorkingMemory["loop_handoff"]; raw != nil && loopHandoffEmpty(state.Handoff) {
		state.Handoff = loopHandoffFromAny(raw)
	}
	applyDeepAgentReviewDecision(state, deepAgentReviewDecisionFromResume(req, state))
	resumeCount := deepAgentAnyInt(state.WorkingMemory["resume_count"], 0) + 1
	state.WorkingMemory["resume_count"] = resumeCount
	if budget := deepAgentResumeBudgetMap(req.AdditionalBudget); len(budget) > 0 {
		state.WorkingMemory["resume_additional_budget"] = budget
	}
	for idx := range state.Plan.Steps {
		switch state.Plan.Steps[idx].Status {
		case DeepAgentStepStatusFailed, DeepAgentStepStatusRunning, "":
			state.Plan.Steps[idx].Status = DeepAgentStepStatusPending
		}
	}
	tried := deepAgentTriedActionsForCompletedSteps(state)
	compressDeepAgentActionHistoryForResume(state, resumeCount)
	state.TriedActions = tried
}

func (c *DeepAgentController) resumePolicyForState(req DeepAgentResumeRequest, state *DeepAgentState) DeepAgentPolicy {
	policy := normalizeDeepAgentPolicy(req.Policy)
	if state == nil {
		return policy
	}
	if req.AdditionalBudget.MaxActions > 0 {
		policy.MaxActions = state.ActionCount + req.AdditionalBudget.MaxActions
	}
	if req.AdditionalBudget.MaxDurationMS > 0 {
		elapsed := c.now().Sub(state.StartedAt)
		if elapsed < 0 {
			elapsed = 0
		}
		policy.MaxDuration = elapsed + time.Duration(req.AdditionalBudget.MaxDurationMS)*time.Millisecond
	}
	if req.AdditionalBudget.MaxSteps > 0 {
		policy.MaxSteps = len(state.Plan.Steps) + req.AdditionalBudget.MaxSteps
	}
	if state.WorkingMemory != nil {
		state.WorkingMemory["resume_policy"] = map[string]any{
			"max_actions":       policy.MaxActions,
			"max_duration_ms":   policy.MaxDuration.Milliseconds(),
			"max_steps":         policy.MaxSteps,
			"no_progress_limit": policy.NoProgressLimit,
		}
	}
	return policy
}

func compressDeepAgentActionHistoryForResume(state *DeepAgentState, resumeCount int) {
	if state == nil || len(state.ActionHistory) == 0 {
		return
	}
	const keepActions = 16
	if resumeCount < 5 && len(state.ActionHistory) <= keepActions*2 {
		return
	}
	if state.WorkingMemory == nil {
		state.WorkingMemory = map[string]any{}
	}
	total := len(state.ActionHistory)
	toolCounts := map[string]int{}
	stepCounts := map[string]int{}
	for _, action := range state.ActionHistory {
		toolCounts[firstNonEmptyString(action.Tool, DeepAgentToolModeModel)]++
		stepCounts[firstNonEmptyString(action.StepID, "unknown")]++
	}
	previous := strings.TrimSpace(deepAgentWorkflowString(state.WorkingMemory, "action_history_summary"))
	summary := fmt.Sprintf("Compressed %d DeepAgent actions before resume %d. Tools=%s. Steps=%s.", total, resumeCount, deepAgentCountMapSummary(toolCounts), deepAgentCountMapSummary(stepCounts))
	if previous != "" && !strings.Contains(previous, summary) {
		summary = previous + "\n" + summary
	}
	state.WorkingMemory["action_history_summary"] = summary
	state.WorkingMemory["action_history_compressed_count"] = total - keepActions
	if total > keepActions {
		state.ActionHistory = append([]DeepAgentAction(nil), state.ActionHistory[total-keepActions:]...)
	}
}

func deepAgentCountMapSummary(values map[string]int) string {
	if len(values) == 0 {
		return "none"
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s:%d", key, values[key]))
	}
	return strings.Join(parts, ",")
}

func deepAgentTriedActionsForCompletedSteps(state *DeepAgentState) map[string]int {
	out := map[string]int{}
	if state == nil {
		return out
	}
	completed := map[string]struct{}{}
	for _, stepID := range state.CompletedSteps {
		if strings.TrimSpace(stepID) != "" {
			completed[stepID] = struct{}{}
		}
	}
	for _, step := range state.Plan.Steps {
		if step.Status == DeepAgentStepStatusSucceeded || step.Status == DeepAgentStepStatusSkipped {
			if strings.TrimSpace(step.ID) != "" {
				completed[step.ID] = struct{}{}
			}
		}
	}
	for _, action := range state.ActionHistory {
		if _, ok := completed[action.StepID]; !ok {
			continue
		}
		hash := firstNonEmptyString(action.Hash, deepAgentActionHash(action))
		if strings.TrimSpace(hash) == "" {
			continue
		}
		count := 1
		if state.TriedActions != nil && state.TriedActions[hash] > 0 {
			count = state.TriedActions[hash]
		}
		out[hash] = count
	}
	return out
}

func withDeepAgentAttemptStrategy(action DeepAgentAction, state *DeepAgentState) DeepAgentAction {
	if state == nil || state.NoProgressCount <= 0 || strings.TrimSpace(state.Blocker) == "" {
		return action
	}
	errorClass := deepAgentWorkflowString(state.WorkingMemory, "last_retryable_error_class")
	if !deepAgentErrorRetryable(errorClass) {
		return action
	}
	if action.Args == nil {
		action.Args = map[string]any{}
	}
	strategy := fmt.Sprintf(
		"retry-%d: previous attempt was blocked by %s; use a different safe strategy, refresh tool calls when applicable, and do not repeat the exact same action.",
		state.NoProgressCount+1,
		truncateDeepAgentDiagnosticText(state.Blocker, 240),
	)
	if deepAgentMissingSourceEvidenceReason(state.Blocker) {
		strategy += " This research step must call WebSearch and/or WebFetch and return source evidence before summarizing; do not answer from memory or write the final report in this step."
	}
	action.Args["attempt_strategy"] = strategy
	if prompt := strings.TrimSpace(deepAgentWorkflowString(action.Args, "prompt")); prompt != "" && !strings.Contains(prompt, "Retry instruction:") {
		action.Args["prompt"] = prompt + "\n\nRetry instruction: " + strategy
	}
	return action
}

func deepAgentMissingSourceEvidenceReason(reason string) bool {
	return strings.Contains(strings.ToLower(strings.TrimSpace(reason)), "source evidence is missing")
}

func recordDeepAgentLastRetryableError(state *DeepAgentState, result DeepAgentActionResult) {
	if state == nil {
		return
	}
	if state.WorkingMemory == nil {
		state.WorkingMemory = map[string]any{}
	}
	errorClass := deepAgentWorkflowString(result.Metadata, "error_class")
	if result.Retryable && deepAgentErrorRetryable(errorClass) {
		state.WorkingMemory["last_retryable_error_class"] = errorClass
		return
	}
	delete(state.WorkingMemory, "last_retryable_error_class")
}

func clearDeepAgentLastRetryableError(state *DeepAgentState) {
	if state == nil || state.WorkingMemory == nil {
		return
	}
	delete(state.WorkingMemory, "last_retryable_error_class")
}

func recordDeepAgentFinalVerification(state *DeepAgentState, verification DeepAgentFinalVerification) {
	if state == nil {
		return
	}
	if state.WorkingMemory == nil {
		state.WorkingMemory = map[string]any{}
	}
	record := map[string]any{
		"done":       verification.Done,
		"reason":     verification.Reason,
		"checks":     verification.Checks,
		"missing":    verification.Missing,
		"confidence": verification.Confidence,
	}
	if verification.ResearchQuality != nil {
		record["research_quality"] = verification.ResearchQuality
	}
	state.WorkingMemory["final_verification"] = record
}

func stateSessionID(run *WorkflowRun) string {
	if run == nil {
		return ""
	}
	return run.SessionID
}

func stateJobID(run *WorkflowRun) string {
	if run == nil {
		return ""
	}
	return run.JobID
}

func (c *DeepAgentController) evaluateFinalResult(ctx context.Context, run *WorkflowRun, state *DeepAgentState) (map[string]any, error) {
	verification, err := c.verifier.CheckFinal(ctx, state)
	if err != nil {
		return nil, err
	}
	recordDeepAgentFinalVerification(state, verification)
	input := deepAgentEvaluatorInputFromState(state)
	input.TraceSummary.VerifierChecks = append([]DeepAgentVerificationCheck(nil), verification.Checks...)
	verdict, err := c.evaluator.EvaluateFinal(ctx, input)
	if err != nil {
		return nil, err
	}
	recordDeepAgentEvaluatorVerdict(state, verdict)
	emitJobEventFromContext(ctx, Event{
		Type:      "deep_agent_evaluator_verdict",
		SessionID: stateSessionID(run),
		JobID:     stateJobID(run),
		Role:      "workflow",
		Content:   firstNonEmptyString(verdict.Reason, verdict.Verdict),
		Data: deepAgentEventData(map[string]any{
			"event_group":       "run",
			"evaluator_verdict": verdict,
			"verdict":           verdict.Verdict,
			"passed":            verdict.Passed,
			"failed_criteria":   verdict.FailedCriteria,
			"repair_plan":       verdict.RepairPlan,
			"source_coverage":   verdict.SourceCoverage,
			"rubric_coverage":   verdict.RubricCoverage,
		}),
	})
	decision := c.verifyGateDecision(state, verification)
	if !verdict.Passed && decision.Allow {
		decision = blockGateDecision(
			DeepAgentGateVerify,
			"evaluator",
			firstNonEmptyString(verdict.Reason, "final evaluator did not pass"),
			strings.Join(verdict.RepairPlan, " "),
			"evaluator_verdict",
		)
	}
	c.recordGateDecision(ctx, run, state, decision)
	if !decision.Allow || !verdict.Passed {
		state.Status = DeepAgentRunStatusBlocked
		state.Blocker = firstNonEmptyString(decision.BlockReason, verdict.Reason, verification.Reason, "final evaluator did not pass")
		state.UpdatedAt = c.now()
		c.persistState(ctx, run, state)
		return map[string]any{
			"deep_agent_state":   state,
			"final_verification": verification,
			"evaluator_verdict":  verdict,
			"gate_decision":      decision,
		}, fmt.Errorf("%w: %s", ErrDeepAgentBlocked, state.Blocker)
	}
	state.Status = DeepAgentRunStatusSucceeded
	state.Blocker = ""
	state.UpdatedAt = c.now()
	c.persistState(ctx, run, state)
	return map[string]any{
		"final_status":        state.Status,
		"verification":        verification.Reason,
		"final_verification":  verification,
		"evaluator_verdict":   verdict,
		"final_artifact_refs": state.WorkingMemory["final_artifact_refs"],
		"deep_agent_state":    state,
	}, nil
}

func (c *DeepAgentController) recordDeepAgentResumeStep(ctx context.Context, run *WorkflowRun, name string, handler func(*WorkflowStepRun) (map[string]any, error)) error {
	now := c.now()
	stepIndex := 0
	if c.store != nil && run != nil {
		steps, err := c.store.ListWorkflowStepRuns(ctx, run.ID)
		if err != nil {
			return err
		}
		stepIndex = nextWorkflowStepIndex(steps)
	}
	step := &WorkflowStepRun{
		ID:             NewWorkflowStepRunID(),
		RunID:          run.ID,
		StepIndex:      stepIndex,
		StepName:       name,
		IdempotencyKey: workflowStepIdempotencyKey(run, stepIndex, name),
		Attempt:        1,
		Status:         WorkflowStepStatusRunning,
		Input:          cloneWorkflowMap(run.State),
		StartedAt:      now,
	}
	if err := c.store.AddWorkflowStepRun(ctx, step); err != nil {
		return err
	}
	_ = c.events.EmitWorkflowEvent(ctx, WorkflowEvent{Run: cloneWorkflowRun(run), Step: cloneWorkflowStepRun(step), Status: step.Status, Type: "workflow_step_started"})
	output, err := handler(step)
	finished := c.now()
	step.FinishedAt = &finished
	step.Output = cloneWorkflowMap(output)
	step.Status = WorkflowStepStatusSucceeded
	if err != nil {
		step.Status = WorkflowStepStatusFailed
		step.Error = err.Error()
	}
	if updateErr := c.store.UpdateWorkflowStepRun(ctx, step); updateErr != nil {
		return updateErr
	}
	eventType := "workflow_step_succeeded"
	if err != nil {
		eventType = "workflow_step_failed"
	}
	_ = c.events.EmitWorkflowEvent(ctx, WorkflowEvent{Run: cloneWorkflowRun(run), Step: cloneWorkflowStepRun(step), Status: step.Status, Type: eventType, Error: step.Error})
	return err
}

func (c *DeepAgentController) finishResume(ctx context.Context, run *WorkflowRun, state *DeepAgentState, err error) (*DeepAgentTaskResult, error) {
	finished := c.now()
	run.UpdatedAt = finished
	run.FinishedAt = &finished
	if err != nil {
		run.Status = WorkflowStatusFailed
		run.Error = err.Error()
	} else {
		run.Status = WorkflowStatusSucceeded
		run.Error = ""
		state.Status = DeepAgentRunStatusSucceeded
		state.Blocker = ""
		state.UpdatedAt = finished
	}
	c.persistState(ctx, run, state)
	if updateErr := c.store.UpdateWorkflowRun(ctx, run); updateErr != nil {
		return &DeepAgentTaskResult{Run: run, State: state, Error: updateErr.Error()}, updateErr
	}
	steps, _ := c.store.ListWorkflowStepRuns(ctx, run.ID)
	result := &DeepAgentTaskResult{Run: cloneWorkflowRun(run), State: state, Steps: steps}
	if err != nil {
		result.Error = err.Error()
		return result, err
	}
	return result, nil
}

func (c *DeepAgentController) executeLoop(ctx context.Context, run *WorkflowRun, state *DeepAgentState, policy DeepAgentPolicy) error {
	deadline := state.StartedAt.Add(policy.MaxDuration)
	for {
		if c.now().After(deadline) {
			state.Status = DeepAgentRunStatusBudgetExceeded
			state.Blocker = "max duration exceeded"
			c.persistState(ctx, run, state)
			return fmt.Errorf("%w: %s", ErrDeepAgentBudgetExceeded, state.Blocker)
		}
		if state.ActionCount >= policy.MaxActions {
			state.Status = DeepAgentRunStatusBudgetExceeded
			state.Blocker = "max action count exceeded"
			c.persistState(ctx, run, state)
			return fmt.Errorf("%w: %s", ErrDeepAgentBudgetExceeded, state.Blocker)
		}
		stepIndex := nextExecutableDeepAgentStepIndex(state)
		if stepIndex < 0 {
			if hasPendingDeepAgentSteps(state.Plan.Steps) {
				state.Status = DeepAgentRunStatusBlocked
				state.Blocker = "pending steps have unmet dependencies"
				state.UpdatedAt = c.now()
				c.persistState(ctx, run, state)
				return fmt.Errorf("%w: %s", ErrDeepAgentBlocked, state.Blocker)
			}
			state.Status = DeepAgentRunStatusSucceeded
			state.UpdatedAt = c.now()
			c.persistState(ctx, run, state)
			return nil
		}
		state.CurrentStepIndex = stepIndex
		state.Plan.Steps[stepIndex].Status = DeepAgentStepStatusRunning
		step := state.Plan.Steps[stepIndex]
		action, err := c.planner.NextAction(ctx, state, step)
		if err != nil {
			state.Status = DeepAgentRunStatusBlocked
			state.Blocker = err.Error()
			c.persistState(ctx, run, state)
			return fmt.Errorf("%w: %s", ErrDeepAgentBlocked, state.Blocker)
		}
		action.StepID = firstNonEmptyString(action.StepID, step.ID)
		action.Tool = firstNonEmptyString(strings.TrimSpace(action.Tool), DeepAgentToolModeModel)
		action = withDeepAgentAttemptStrategy(action, state)
		action = applyDeepAgentActionOverride(state, action)
		action.Hash = firstNonEmptyString(action.Hash, deepAgentActionHash(action))
		if state.TriedActions == nil {
			state.TriedActions = map[string]int{}
		}
		decision := c.executionGateDecision(state, step, action, policy)
		c.recordGateDecision(ctx, run, state, decision)
		if !decision.Allow {
			state.Plan.Steps[stepIndex].Status = DeepAgentStepStatusFailed
			state.FailedSteps = appendUniqueString(state.FailedSteps, step.ID)
			state.Status = DeepAgentRunStatusBlocked
			if decision.RequiresReview {
				state.Status = DeepAgentRunStatusReviewPending
				recordDeepAgentPendingReview(state, step, action, decision.BlockReason, c.now())
			}
			if decision.Category == "budget" {
				state.Status = DeepAgentRunStatusBudgetExceeded
			}
			state.Blocker = decision.BlockReason
			state.UpdatedAt = c.now()
			c.persistState(ctx, run, state)
			if decision.RequiresReview {
				return fmt.Errorf("%w: %s", ErrDeepAgentReviewRequired, state.Blocker)
			}
			if decision.Category == "budget" {
				return fmt.Errorf("%w: %s", ErrDeepAgentBudgetExceeded, state.Blocker)
			}
			return fmt.Errorf("%w: %s", ErrDeepAgentBlocked, state.Blocker)
		}
		if err := c.reviewActionRisk(ctx, run, state, step, action); err != nil {
			if errors.Is(err, ErrDeepAgentPolicyBlocked) {
				state.Plan.Steps[stepIndex].Status = DeepAgentStepStatusFailed
				state.FailedSteps = appendUniqueString(state.FailedSteps, step.ID)
				state.Status = DeepAgentRunStatusBlocked
				state.Blocker = err.Error()
				state.UpdatedAt = c.now()
				c.persistState(ctx, run, state)
				return fmt.Errorf("%w: %s", ErrDeepAgentBlocked, state.Blocker)
			}
			recordDeepAgentPendingReview(state, step, action, err.Error(), c.now())
			state.Plan.Steps[stepIndex].Status = DeepAgentStepStatusFailed
			state.FailedSteps = appendUniqueString(state.FailedSteps, step.ID)
			state.Status = DeepAgentRunStatusReviewPending
			state.Blocker = err.Error()
			state.UpdatedAt = c.now()
			c.persistState(ctx, run, state)
			return fmt.Errorf("%w: %s", ErrDeepAgentReviewRequired, state.Blocker)
		}
		if state.TriedActions[action.Hash] > 0 {
			state.NoProgressCount++
			state.Blocker = fmt.Sprintf("repeated action %s", action.Hash)
			state.UpdatedAt = c.now()
			c.persistState(ctx, run, state)
			if state.NoProgressCount >= policy.NoProgressLimit {
				state.Status = DeepAgentRunStatusBlocked
				c.persistState(ctx, run, state)
				return fmt.Errorf("%w: %s", ErrDeepAgentBlocked, state.Blocker)
			}
			continue
		}
		state.TriedActions[action.Hash]++
		state.ActionHistory = append(state.ActionHistory, action)
		state.ActionCount++
		c.emitActionEvent(ctx, run, state, step, action, DeepAgentActionResult{}, "deep_agent_action_started", "")
		result, execErr := c.executor.ExecuteDeepAgentAction(ctx, action, state)
		if result.Status == "" {
			result.Status = DeepAgentActionStatusSucceeded
		}
		c.enrichActionResultMetadata(run, step, action, &result)
		if execErr != nil {
			result.Status = DeepAgentActionStatusFailed
			result.Error = execErr.Error()
			c.enrichActionResultMetadata(run, step, action, &result)
		}
		evidence := c.ensureActionEvidence(run, step, action, &result, execErr)
		c.inheritPriorResearchSourceEvidence(state, step, action, &result, &evidence)
		if execErr != nil {
			c.emitActionEvent(ctx, run, state, step, action, result, "deep_agent_action_failed", result.Error)
			if !result.Retryable {
				c.storeActionEvidence(state, evidence, DeepAgentProgress{})
				state.Plan.Steps[stepIndex].Status = DeepAgentStepStatusFailed
				state.FailedSteps = appendUniqueString(state.FailedSteps, step.ID)
				state.Status = DeepAgentRunStatusBlocked
				state.Blocker = result.Error
				state.UpdatedAt = c.now()
				c.persistState(ctx, run, state)
				return fmt.Errorf("%w: %s", ErrDeepAgentBlocked, state.Blocker)
			}
		} else if result.Status == DeepAgentActionStatusFailed {
			c.emitActionEvent(ctx, run, state, step, action, result, "deep_agent_action_failed", result.Error)
		} else {
			c.emitActionEvent(ctx, run, state, step, action, result, "deep_agent_action_succeeded", "")
		}
		progress, err := c.verifier.CheckProgress(ctx, state, step, action, result)
		if err != nil {
			state.Status = DeepAgentRunStatusBlocked
			state.Blocker = err.Error()
			c.storeActionEvidence(state, evidence, DeepAgentProgress{MadeProgress: false, StepDone: false, Reason: err.Error()})
			c.persistState(ctx, run, state)
			return fmt.Errorf("%w: %s", ErrDeepAgentBlocked, state.Blocker)
		}
		c.storeActionEvidence(state, evidence, progress)
		if progress.MadeProgress {
			state.NoProgressCount = 0
			state.Blocker = ""
			clearDeepAgentLastRetryableError(state)
		} else {
			state.NoProgressCount++
			state.Blocker = firstNonEmptyString(progress.Reason, result.Error, "no progress")
			recordDeepAgentLastRetryableError(state, result)
			if deepAgentMissingSourceEvidenceReason(state.Blocker) {
				if state.WorkingMemory == nil {
					state.WorkingMemory = map[string]any{}
				}
				state.WorkingMemory["last_retryable_error_class"] = DeepAgentErrorTransient
			}
		}
		if progress.StepDone {
			state.Plan.Steps[stepIndex].Status = DeepAgentStepStatusSucceeded
			state.CompletedSteps = appendUniqueString(state.CompletedSteps, step.ID)
			c.storeStepContext(state, step, action, result, progress)
			state.NoProgressCount = 0
			state.Blocker = ""
		}
		if !progress.StepDone && state.NoProgressCount >= policy.NoProgressLimit {
			state.Plan.Steps[stepIndex].Status = DeepAgentStepStatusFailed
			state.FailedSteps = appendUniqueString(state.FailedSteps, step.ID)
			state.Status = DeepAgentRunStatusBlocked
			state.UpdatedAt = c.now()
			c.persistState(ctx, run, state)
			return fmt.Errorf("%w: %s", ErrDeepAgentBlocked, state.Blocker)
		}
		state.UpdatedAt = c.now()
		c.persistState(ctx, run, state)
	}
}

func (c *DeepAgentController) storeStepContext(state *DeepAgentState, step DeepAgentStep, action DeepAgentAction, result DeepAgentActionResult, progress DeepAgentProgress) {
	if state == nil {
		return
	}
	if state.WorkingMemory == nil {
		state.WorkingMemory = map[string]any{}
	}
	store, _ := state.WorkingMemory["step_context"].(map[string]any)
	if store == nil {
		store = map[string]any{}
		state.WorkingMemory["step_context"] = store
	}
	evidence, evidenceOK := c.stepEvidence(state, step.ID)
	output := strings.TrimSpace(result.Output)
	if output == "" && evidenceOK {
		output = strings.TrimSpace(firstNonEmptyString(evidence.Output, evidence.Summary))
	}
	summary := output
	if summary == "" && evidenceOK {
		summary = strings.TrimSpace(evidence.Summary)
	}
	if len([]rune(summary)) > 800 {
		runes := []rune(summary)
		summary = string(runes[:800])
	}
	record := map[string]any{
		"step_id":          step.ID,
		"step_title":       step.Title,
		"intent":           step.Intent,
		"success_criteria": step.DoneCondition,
		"tool":             action.Tool,
		"status":           result.Status,
		"completed":        result.Completed,
		"made_progress":    progress.MadeProgress,
		"progress_reason":  progress.Reason,
		"summary":          summary,
	}
	if output != "" {
		record["output"] = output
	}
	if evidenceOK {
		record["step_evidence"] = deepAgentStepEvidenceMap(evidence)
		if len(evidence.Artifacts) > 0 {
			record["artifact_refs"] = evidence.Artifacts
			record["artifact_count"] = len(evidence.Artifacts)
		}
		if len(evidence.Sources) > 0 {
			record["sources"] = evidence.Sources
		}
		if len(evidence.ToolCalls) > 0 {
			record["tool_calls"] = evidence.ToolCalls
		}
		if len(evidence.ChildJobs) > 0 {
			record["child_jobs"] = evidence.ChildJobs
		}
	}
	if len(result.Metadata) > 0 {
		record["metadata"] = cloneWorkflowMap(result.Metadata)
		if count := deepAgentAnyInt(result.Metadata["artifact_count"], 0); count > 0 {
			record["artifact_count"] = count
		}
		if refs := deepAgentArtifactRefsFromMetadata(result.Metadata); len(refs) > 0 {
			record["artifact_refs"] = refs
		}
		if jobID := deepAgentWorkflowString(result.Metadata, "job_id"); jobID != "" {
			record["job_id"] = jobID
		}
	}
	store[step.ID] = record
}

func (c *DeepAgentController) ensureActionEvidence(run *WorkflowRun, step DeepAgentStep, action DeepAgentAction, result *DeepAgentActionResult, execErr error) DeepAgentStepEvidence {
	if result == nil {
		result = &DeepAgentActionResult{Status: DeepAgentActionStatusFailed}
	}
	if result.Metadata == nil {
		result.Metadata = map[string]any{}
	}
	var evidence DeepAgentStepEvidence
	if existing, ok := deepAgentStepEvidenceFromAny(result.Metadata["step_evidence"]); ok {
		evidence = existing
	} else {
		route, ok := deepAgentStepRouteFromMap(action.Args)
		if !ok {
			route = DeepAgentStepRoute{
				StepID:           firstNonEmptyString(action.StepID, step.ID),
				Mode:             normalizeDeepAgentRouteMode(action.Tool),
				Executor:         deepAgentExecutorForMode(action.Tool),
				RequiresArtifact: deepAgentActionRequiresArtifact(action),
				DeliverableType:  firstNonEmptyString(deepAgentActionString(action, "deliverable_type"), deepAgentDeliverableTypeForStep(step), deepAgentDeliverableNone),
				AllowedTools:     deepAgentStringSlice(action.Args["allowed_tools"]),
				SearchScope:      deepAgentActionString(action, "search_scope"),
				SuccessCriteria:  deepAgentStringSlice(action.Args["success_criteria"]),
				Reason:           "controller evidence synthesis",
				Confidence:       "medium",
			}
		}
		evidence = deepAgentEvidenceFromActionResult(route, action, *result, execErr)
	}
	evidence.StepID = firstNonEmptyString(evidence.StepID, evidence.Route.StepID, step.ID, action.StepID)
	evidence.ActionID = firstNonEmptyString(evidence.ActionID, action.ID, action.Hash)
	evidence.Route.StepID = firstNonEmptyString(evidence.Route.StepID, evidence.StepID)
	evidence.Route.Mode = normalizeDeepAgentRouteMode(firstNonEmptyString(evidence.Route.Mode, action.Tool, DeepAgentToolModeModel))
	evidence.Route.Executor = firstNonEmptyString(evidence.Route.Executor, deepAgentExecutorForMode(evidence.Route.Mode))
	if evidence.Route.DeliverableType == "" {
		evidence.Route.DeliverableType = firstNonEmptyString(deepAgentDeliverableTypeForStep(step), deepAgentDeliverableNone)
	}
	if strings.TrimSpace(evidence.Output) == "" {
		evidence.Output = result.Output
	}
	if strings.TrimSpace(evidence.Summary) == "" {
		evidence.Summary = truncateDeepAgentDiagnosticText(firstNonEmptyString(evidence.Output, result.Output), 800)
	}
	if evidence.Diagnostics == nil {
		evidence.Diagnostics = map[string]any{}
	}
	evidence.Diagnostics["result_status"] = firstNonEmptyString(deepAgentWorkflowString(evidence.Diagnostics, "result_status"), result.Status)
	evidence.Diagnostics["completed"] = deepAgentBool(evidence.Diagnostics, "completed", result.Completed)
	if run != nil {
		evidence.Diagnostics["run_id"] = run.ID
		evidence.Diagnostics["job_id"] = run.JobID
	}
	evidence.Diagnostics["step_id"] = evidence.StepID
	if len(evidence.Artifacts) == 0 {
		evidence.Artifacts = deepAgentArtifactRefsFromMetadata(result.Metadata)
	}
	for idx := range evidence.Artifacts {
		if run != nil {
			evidence.Artifacts[idx].RunID = firstNonEmptyString(evidence.Artifacts[idx].RunID, run.ID)
			evidence.Artifacts[idx].JobID = firstNonEmptyString(evidence.Artifacts[idx].JobID, run.JobID)
		}
		evidence.Artifacts[idx].StepID = firstNonEmptyString(evidence.Artifacts[idx].StepID, evidence.StepID)
	}
	if len(evidence.Sources) == 0 {
		evidence.Sources = deepAgentSourceRefsFromAny(result.Metadata["sources"])
	}
	if len(evidence.ToolCalls) == 0 {
		evidence.ToolCalls = deepAgentToolCallRefsFromMetadata(result.Metadata)
	}
	if len(evidence.ChildJobs) == 0 {
		evidence.ChildJobs = deepAgentChildJobRefsFromMetadata(result.Metadata)
	}
	if evidence.ErrorClass == "" {
		evidence.ErrorClass = firstNonEmptyString(deepAgentWorkflowString(result.Metadata, "error_class"), classifyDeepAgentError(execErr, *result))
	}
	if evidence.SideEffectLevel == "" {
		evidence.SideEffectLevel = deepAgentWorkflowString(result.Metadata, "side_effect_level")
	}
	if evidence.RollbackHint == "" {
		evidence.RollbackHint = deepAgentWorkflowString(result.Metadata, "rollback_hint")
	}
	result.Metadata["step_evidence"] = deepAgentStepEvidenceMap(evidence)
	if len(evidence.Artifacts) > 0 {
		result.Metadata["artifact_refs"] = evidence.Artifacts
		result.Metadata["artifact_count"] = len(evidence.Artifacts)
	}
	if len(evidence.Sources) > 0 {
		result.Metadata["sources"] = evidence.Sources
	}
	if len(evidence.ToolCalls) > 0 {
		result.Metadata["tool_calls"] = evidence.ToolCalls
	}
	if len(evidence.ChildJobs) > 0 {
		result.Metadata["child_jobs"] = evidence.ChildJobs
	}
	if evidence.ErrorClass != "" {
		result.Metadata["error_class"] = evidence.ErrorClass
	}
	return evidence
}

func (c *DeepAgentController) inheritPriorResearchSourceEvidence(state *DeepAgentState, step DeepAgentStep, action DeepAgentAction, result *DeepAgentActionResult, evidence *DeepAgentStepEvidence) {
	if state == nil || result == nil || evidence == nil {
		return
	}
	if !deepAgentRouteLooksLikeResearch(evidence.Route, step) {
		return
	}
	output := firstNonEmptyString(evidence.Output, result.Output, evidence.Summary)
	if strings.TrimSpace(output) == "" {
		return
	}
	if len(evidence.Sources) > 0 || len(evidence.ToolCalls) > 0 || countDeepAgentCitationMarkers(output) > 0 {
		return
	}
	prior := deepAgentStateEvidenceSummary(state)
	if len(prior.sources) == 0 {
		return
	}
	evidence.Sources = append([]DeepAgentSourceRef(nil), prior.sources...)
	if evidence.Diagnostics == nil {
		evidence.Diagnostics = map[string]any{}
	}
	evidence.Diagnostics["inherited_source_evidence"] = true
	evidence.Diagnostics["inherited_source_count"] = len(evidence.Sources)
	evidence.Diagnostics["inherited_source_reason"] = "current research step reused prior verified source evidence"
	if result.Metadata == nil {
		result.Metadata = map[string]any{}
	}
	result.Metadata["sources"] = evidence.Sources
	result.Metadata["step_evidence"] = deepAgentStepEvidenceMap(*evidence)
}

func (c *DeepAgentController) storeActionEvidence(state *DeepAgentState, evidence DeepAgentStepEvidence, progress DeepAgentProgress) {
	if state == nil {
		return
	}
	if evidence.Diagnostics == nil {
		evidence.Diagnostics = map[string]any{}
	}
	if progress.Reason != "" {
		evidence.Diagnostics["progress_reason"] = progress.Reason
	}
	evidence.Diagnostics["made_progress"] = progress.MadeProgress
	evidence.Diagnostics["step_done"] = progress.StepDone
	if progress.StepDone {
		evidence.VerifiedBy = appendUniqueString(evidence.VerifiedBy, "progress_verifier")
	}
	store := c.evidenceStore
	if store == nil {
		store = deepAgentDefaultEvidenceStore()
	}
	store.PutStepEvidence(state, evidence)
}

func (c *DeepAgentController) stepEvidence(state *DeepAgentState, stepID string) (DeepAgentStepEvidence, bool) {
	store := c.evidenceStore
	if store == nil {
		store = deepAgentDefaultEvidenceStore()
	}
	return store.GetStepEvidence(state, stepID)
}

func (c *DeepAgentController) emitActionEvent(ctx context.Context, run *WorkflowRun, state *DeepAgentState, step DeepAgentStep, action DeepAgentAction, result DeepAgentActionResult, eventType, errorText string) {
	if c == nil || run == nil {
		return
	}
	tool := strings.TrimSpace(action.Tool)
	if tool == "" {
		tool = DeepAgentToolModeModel
	}
	payload := map[string]any{
		"type":          eventType,
		"event_group":   deepAgentEventGroup(eventType),
		"workflow_name": run.Name,
		"version":       run.Version,
		"run_id":        run.ID,
		"job_id":        run.JobID,
		"session_id":    run.SessionID,
		"user_id":       run.UserID,
		"step_id":       step.ID,
		"step_title":    step.Title,
		"step_status":   step.Status,
		"tool":          tool,
		"route":         deepAgentActionRoutePayload(tool, action),
		"route_version": deepAgentActionString(action, "route_version"),
		"action_id":     firstNonEmptyString(action.ID, action.Hash),
		"action_hash":   action.Hash,
		"action_count":  0,
		"attempt":       deepAgentIntFromMap(action.Args, "attempt", 1),
	}
	if promptPreview := truncateDeepAgentDiagnosticText(deepAgentActionString(action, "prompt"), 700); promptPreview != "" {
		payload["prompt_preview"] = promptPreview
	}
	if attemptStrategy := deepAgentActionString(action, "attempt_strategy"); attemptStrategy != "" {
		payload["attempt_strategy"] = attemptStrategy
	}
	if route, ok := deepAgentStepRouteFromMap(action.Args); ok {
		if len(route.AllowedTools) > 0 {
			payload["allowed_tools"] = append([]string(nil), route.AllowedTools...)
		}
		if route.DeliverableType != "" {
			payload["deliverable_type"] = route.DeliverableType
		}
		if route.SearchScope != "" {
			payload["search_scope"] = route.SearchScope
		}
		if len(route.ShadowDiff) > 0 {
			payload["route_shadow_diff"] = append([]string(nil), route.ShadowDiff...)
			payload["route_shadow"] = route.ShadowRoute
		}
	}
	if state != nil {
		payload["action_count"] = state.ActionCount
		payload["deep_agent_status"] = state.Status
	}
	if tool == DeepAgentToolModeSkill {
		payload["skill_name"] = firstNonEmptyString(deepAgentActionString(action, "skill"), deepAgentActionString(action, "skill_name"))
	}
	if query := deepAgentActionString(action, "query"); query != "" {
		payload["query"] = query
	}
	if result.Status != "" {
		payload["result_status"] = result.Status
	}
	if result.Completed {
		payload["completed"] = result.Completed
	}
	if len(result.Metadata) > 0 {
		payload["result_metadata"] = result.Metadata
		deepAgentCopyTraceMetric(payload, result.Metadata, "duration_ms")
		deepAgentCopyTraceMetric(payload, result.Metadata, "token_estimate")
		deepAgentCopyTraceMetric(payload, result.Metadata, "estimated_cost_usd")
		deepAgentCopyTraceMetric(payload, result.Metadata, "cost")
		if evidenceID := deepAgentWorkflowString(result.Metadata, "evidence_id"); evidenceID != "" {
			payload["evidence_id"] = evidenceID
		}
		if refs := deepAgentArtifactRefsFromMetadata(result.Metadata); len(refs) > 0 {
			payload["artifact_refs"] = refs
		}
		if sources := deepAgentSourceRefsFromAny(result.Metadata["sources"]); len(sources) > 0 {
			payload["sources"] = sources
		}
		if toolCalls := deepAgentToolCallRefsFromMetadata(result.Metadata); len(toolCalls) > 0 {
			payload["tool_calls"] = toolCalls
		}
		if childJobs := deepAgentChildJobRefsFromMetadata(result.Metadata); len(childJobs) > 0 {
			payload["child_jobs"] = childJobs
		}
		if errorClass := deepAgentWorkflowString(result.Metadata, "error_class"); errorClass != "" {
			payload["error_class"] = errorClass
		}
		if evidence, ok := deepAgentStepEvidenceFromAny(result.Metadata["step_evidence"]); ok {
			payload["evidence"] = evidence
			payload["evidence_id"] = firstNonEmptyString(deepAgentWorkflowString(result.Metadata, "evidence_id"), evidence.ActionID, action.ID, action.Hash)
			if len(evidence.Diagnostics) > 0 {
				payload["diagnostics"] = evidence.Diagnostics
				deepAgentCopyTraceMetric(payload, evidence.Diagnostics, "duration_ms")
				deepAgentCopyTraceMetric(payload, evidence.Diagnostics, "token_estimate")
				deepAgentCopyTraceMetric(payload, evidence.Diagnostics, "estimated_cost_usd")
				deepAgentCopyTraceMetric(payload, evidence.Diagnostics, "cost")
			}
			if len(evidence.Sources) > 0 {
				payload["sources"] = evidence.Sources
			}
			if len(evidence.ToolCalls) > 0 {
				payload["tool_calls"] = evidence.ToolCalls
			}
			if len(evidence.ChildJobs) > 0 {
				payload["child_jobs"] = evidence.ChildJobs
			}
			if evidence.ErrorClass != "" {
				payload["error_class"] = evidence.ErrorClass
			}
		}
	}
	if errorText != "" {
		payload["error"] = errorText
	}
	content := strings.TrimSpace(fmt.Sprintf("%s %s", firstNonEmptyString(step.ID, step.Title), tool))
	if eventType != "deep_agent_action_started" && result.Status != "" {
		content = strings.TrimSpace(content + " " + result.Status)
	}
	emitJobEventFromContext(ctx, Event{
		Type:      eventType,
		SessionID: run.SessionID,
		JobID:     run.JobID,
		Role:      "workflow",
		Content:   content,
		Error:     errorText,
		Data:      deepAgentEventData(payload),
	})
	if eventType != "deep_agent_action_started" {
		c.emitDeepAgentChildJobEvents(ctx, run, step, action, payload)
		c.emitDeepAgentArtifactEvents(ctx, run, step, action, payload)
	}
}

func deepAgentEventGroup(eventType string) string {
	switch {
	case strings.HasPrefix(eventType, "workflow_run"):
		return "workflow_run"
	case strings.HasPrefix(eventType, "workflow_step"):
		return "workflow_step"
	case eventType == "deep_agent_child_job":
		return "child_skill_job"
	case eventType == "deep_agent_artifact_output":
		return "artifact_output"
	case strings.HasPrefix(eventType, "deep_agent_action"):
		return "deep_agent_action"
	default:
		return "event"
	}
}

func deepAgentCopyTraceMetric(dst map[string]any, src map[string]any, key string) {
	if dst == nil || src == nil || strings.TrimSpace(key) == "" {
		return
	}
	value, ok := src[key]
	if !ok || value == nil {
		return
	}
	if text, ok := value.(string); ok && strings.TrimSpace(text) == "" {
		return
	}
	if _, exists := dst[key]; exists {
		return
	}
	dst[key] = value
}

func deepAgentPlannedConnectors(req DeepAgentTaskRequest, state *DeepAgentState, plan DeepAgentPlan) []map[string]any {
	selected := normalizeConnectorScopes(req.ConnectorContext)
	if len(selected) == 0 && state != nil {
		selected = normalizeConnectorScopes(deepAgentStringSlice(state.WorkingMemory["connector_context"]))
	}
	if len(selected) == 0 {
		return nil
	}
	stepConnectors := map[string][]string{}
	for _, step := range plan.Steps {
		route, ok := deepAgentStepRouteFromMap(step.Metadata)
		if !ok {
			text := deepAgentRouteText(step)
			if strings.Contains(text, "github") {
				stepConnectors["github"] = appendUniqueString(stepConnectors["github"], step.ID)
			}
			continue
		}
		if normalizeDeepAgentRouteMode(route.Mode) == DeepAgentToolModeConnector || route.Executor == deepAgentRouteExecutorConnector || route.SearchScope == "github" {
			for _, tool := range route.AllowedTools {
				if strings.Contains(strings.ToLower(tool), "github") {
					stepConnectors["github"] = appendUniqueString(stepConnectors["github"], step.ID)
				}
			}
			if len(route.AllowedTools) == 0 {
				stepConnectors["github"] = appendUniqueString(stepConnectors["github"], step.ID)
			}
		}
	}
	out := make([]map[string]any, 0, len(selected))
	for _, provider := range selected {
		item := map[string]any{
			"provider": provider,
			"selected": true,
		}
		if steps := stepConnectors[provider]; len(steps) > 0 {
			item["step_ids"] = steps
			item["planned_tools"] = []string{provider + "_repo_reader", provider + "_issue_reader"}
		}
		out = append(out, item)
	}
	return out
}

func (c *DeepAgentController) emitDeepAgentChildJobEvents(ctx context.Context, run *WorkflowRun, step DeepAgentStep, action DeepAgentAction, parent map[string]any) {
	childJobs := deepAgentChildJobRefsFromAny(parent["child_jobs"])
	for _, child := range childJobs {
		if strings.TrimSpace(child.ID) == "" {
			continue
		}
		payload := cloneWorkflowMap(parent)
		payload["type"] = "deep_agent_child_job"
		payload["event_group"] = "child_skill_job"
		payload["child_job"] = child
		content := strings.TrimSpace(fmt.Sprintf("%s child job %s %s", firstNonEmptyString(step.ID, step.Title), child.ID, child.Status))
		emitJobEventFromContext(ctx, Event{
			Type:      "deep_agent_child_job",
			SessionID: run.SessionID,
			JobID:     run.JobID,
			Role:      "workflow",
			Content:   content,
			Data:      deepAgentEventData(payload),
		})
	}
	_ = action
}

func (c *DeepAgentController) emitDeepAgentArtifactEvents(ctx context.Context, run *WorkflowRun, step DeepAgentStep, action DeepAgentAction, parent map[string]any) {
	refs := deepAgentArtifactRefsFromAny(parent["artifact_refs"])
	for _, ref := range refs {
		if strings.TrimSpace(firstNonEmptyString(ref.ID, ref.Filename)) == "" {
			continue
		}
		payload := cloneWorkflowMap(parent)
		payload["type"] = "deep_agent_artifact_output"
		payload["event_group"] = "artifact_output"
		payload["artifact"] = ref
		content := strings.TrimSpace(fmt.Sprintf("%s artifact %s", firstNonEmptyString(step.ID, step.Title), firstNonEmptyString(ref.Filename, ref.ID)))
		emitJobEventFromContext(ctx, Event{
			Type:      "deep_agent_artifact_output",
			SessionID: run.SessionID,
			JobID:     run.JobID,
			Role:      "workflow",
			Content:   content,
			Data:      deepAgentEventData(payload),
		})
	}
	_ = action
}

func (c *DeepAgentController) enrichActionResultMetadata(run *WorkflowRun, step DeepAgentStep, action DeepAgentAction, result *DeepAgentActionResult) {
	if run == nil || result == nil {
		return
	}
	if result.Metadata == nil {
		result.Metadata = map[string]any{}
	}
	result.Metadata["run_id"] = firstNonEmptyString(deepAgentWorkflowString(result.Metadata, "run_id"), run.ID)
	result.Metadata["job_id"] = firstNonEmptyString(deepAgentWorkflowString(result.Metadata, "job_id"), run.JobID, deepAgentActionString(action, "job_id"))
	result.Metadata["step_id"] = firstNonEmptyString(deepAgentWorkflowString(result.Metadata, "step_id"), step.ID, action.StepID)
	refs := deepAgentArtifactRefsFromMetadata(result.Metadata)
	if len(refs) == 0 {
		return
	}
	for idx := range refs {
		refs[idx].RunID = firstNonEmptyString(refs[idx].RunID, run.ID)
		refs[idx].JobID = firstNonEmptyString(refs[idx].JobID, run.JobID, deepAgentWorkflowString(result.Metadata, "job_id"))
		refs[idx].StepID = firstNonEmptyString(refs[idx].StepID, step.ID, action.StepID)
	}
	result.Metadata["artifact_refs"] = refs
	if len(refs) > 0 {
		result.Metadata["artifact_count"] = len(refs)
	}
	if rawEvidence, ok := result.Metadata["step_evidence"]; ok && rawEvidence != nil {
		evidence, ok := deepAgentStepEvidenceFromAny(rawEvidence)
		if ok {
			evidence.Route.StepID = firstNonEmptyString(evidence.Route.StepID, step.ID, action.StepID)
			evidence.Artifacts = refs
			result.Metadata["step_evidence"] = deepAgentStepEvidenceMap(evidence)
		}
	}
}

func deepAgentActionRoutePayload(tool string, action DeepAgentAction) map[string]any {
	if route, ok := deepAgentStepRouteFromMap(action.Args); ok {
		return deepAgentStepRouteMap(route)
	}
	route := map[string]any{
		"mode": tool,
		"tool": tool,
	}
	if action.StepID != "" {
		route["step_id"] = action.StepID
	}
	if skill := firstNonEmptyString(deepAgentActionString(action, "skill"), deepAgentActionString(action, "skill_name")); skill != "" {
		route["skill_name"] = skill
	}
	if query := deepAgentActionString(action, "query"); query != "" {
		route["query"] = query
	}
	if allowedTools, ok := action.Args["allowed_tools"]; ok {
		route["allowed_tools"] = allowedTools
	}
	return route
}

func deepAgentArtifactRefsFromMetadata(metadata map[string]any) []DeepAgentArtifactRef {
	if len(metadata) == 0 {
		return nil
	}
	if raw, ok := metadata["artifact_refs"]; ok {
		switch refs := raw.(type) {
		case []DeepAgentArtifactRef:
			return append([]DeepAgentArtifactRef(nil), refs...)
		case []any:
			out := make([]DeepAgentArtifactRef, 0, len(refs))
			for _, item := range refs {
				data, err := json.Marshal(item)
				if err != nil {
					continue
				}
				var ref DeepAgentArtifactRef
				if err := json.Unmarshal(data, &ref); err == nil && (ref.ID != "" || ref.Filename != "") {
					out = append(out, ref)
				}
			}
			if len(out) > 0 {
				return out
			}
		}
	}
	artifactID := deepAgentWorkflowString(metadata, "artifact_id")
	if artifactID == "" {
		return nil
	}
	return []DeepAgentArtifactRef{{
		ID:          artifactID,
		Filename:    deepAgentWorkflowString(metadata, "artifact_filename"),
		ContentType: deepAgentWorkflowString(metadata, "artifact_content_type"),
		JobID:       firstNonEmptyString(deepAgentWorkflowString(metadata, "job_id"), deepAgentWorkflowString(metadata, "child_job_id")),
		RunID:       deepAgentWorkflowString(metadata, "run_id"),
		StepID:      deepAgentWorkflowString(metadata, "step_id"),
	}}
}

func deepAgentEventData(value any) json.RawMessage {
	data, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	return data
}

func (c *DeepAgentController) reviewActionRisk(ctx context.Context, run *WorkflowRun, state *DeepAgentState, step DeepAgentStep, action DeepAgentAction) error {
	if c == nil || c.riskGate == nil {
		return nil
	}
	level := normalizeDeepAgentRiskLevel(firstNonEmptyString(
		step.RiskLevel,
		deepAgentWorkflowString(step.Metadata, "risk_level"),
		deepAgentActionString(action, "risk_level"),
	))
	if level == RiskLevelHigh {
		if err := deepAgentCheckGovernancePolicy(state, action); err != nil {
			return err
		}
	}
	if deepAgentActionReviewApproved(state, action) {
		return nil
	}
	return c.riskGate.ReviewDeepAgentAction(ctx, run, state, step, action)
}

func deepAgentReviewDecisionFromResume(req DeepAgentResumeRequest, state *DeepAgentState) DeepAgentReviewDecision {
	decision := req.ReviewDecision
	if strings.TrimSpace(decision.Action) == "" && state != nil && req.StatePatch != nil {
		if parsed, ok := deepAgentReviewDecisionFromAny(req.StatePatch["review_decision"]); ok {
			decision = parsed
		}
	}
	return normalizeDeepAgentReviewDecision(decision)
}

func normalizeDeepAgentReviewDecision(decision DeepAgentReviewDecision) DeepAgentReviewDecision {
	decision.Action = strings.ToLower(strings.TrimSpace(decision.Action))
	decision.StepID = strings.TrimSpace(decision.StepID)
	decision.ActionHash = strings.TrimSpace(decision.ActionHash)
	decision.Reason = strings.TrimSpace(decision.Reason)
	if decision.ArgsPatch != nil {
		decision.ArgsPatch = cloneWorkflowMap(decision.ArgsPatch)
	}
	return decision
}

func deepAgentReviewDecisionFromAny(raw any) (DeepAgentReviewDecision, bool) {
	record, ok := raw.(map[string]any)
	if !ok {
		return DeepAgentReviewDecision{}, false
	}
	decision := DeepAgentReviewDecision{
		Action:     deepAgentWorkflowString(record, "action"),
		StepID:     deepAgentWorkflowString(record, "step_id"),
		ActionHash: deepAgentWorkflowString(record, "action_hash"),
		Reason:     deepAgentWorkflowString(record, "reason"),
	}
	if patch, ok := record["args_patch"].(map[string]any); ok {
		decision.ArgsPatch = cloneWorkflowMap(patch)
	}
	return normalizeDeepAgentReviewDecision(decision), decision.Action != ""
}

func applyDeepAgentReviewDecision(state *DeepAgentState, decision DeepAgentReviewDecision) {
	if state == nil || strings.TrimSpace(decision.Action) == "" {
		return
	}
	if state.WorkingMemory == nil {
		state.WorkingMemory = map[string]any{}
	}
	pending, _ := state.WorkingMemory["pending_review_action"].(map[string]any)
	stepID := firstNonEmptyString(decision.StepID, deepAgentWorkflowString(pending, "step_id"))
	actionHash := firstNonEmptyString(decision.ActionHash, deepAgentWorkflowString(pending, "action_hash"))
	record := map[string]any{
		"action":      decision.Action,
		"step_id":     stepID,
		"action_hash": actionHash,
		"reason":      decision.Reason,
	}
	switch decision.Action {
	case "approve":
		deepAgentStoreReviewActionMap(state, "approved_review_actions", actionHash, record)
		delete(state.WorkingMemory, "pending_review_action")
	case "reject":
		record["planner_instruction"] = "The reviewed action was rejected. Choose a different route and avoid the rejected action hash."
		deepAgentStoreReviewActionMap(state, "rejected_review_actions", actionHash, record)
		state.WorkingMemory["review_feedback"] = record
		delete(state.WorkingMemory, "pending_review_action")
	case "edit":
		record["planner_instruction"] = "The reviewed action was edited by a human reviewer. Apply the action override and continue."
		if len(decision.ArgsPatch) > 0 {
			record["args_patch"] = cloneWorkflowMap(decision.ArgsPatch)
			overrides, _ := state.WorkingMemory["action_overrides"].(map[string]any)
			if overrides == nil {
				overrides = map[string]any{}
				state.WorkingMemory["action_overrides"] = overrides
			}
			overrides[stepID] = cloneWorkflowMap(decision.ArgsPatch)
		}
		deepAgentStoreReviewActionMap(state, "approved_review_actions", actionHash, record)
		delete(state.WorkingMemory, "pending_review_action")
	default:
		return
	}
	state.WorkingMemory["last_review_decision"] = record
	if stepID != "" {
		state.FailedSteps = removeDeepAgentString(state.FailedSteps, stepID)
		for idx := range state.Plan.Steps {
			if state.Plan.Steps[idx].ID == stepID {
				state.Plan.Steps[idx].Status = DeepAgentStepStatusPending
			}
		}
	}
}

func deepAgentStoreReviewActionMap(state *DeepAgentState, key, actionHash string, record map[string]any) {
	if state == nil || state.WorkingMemory == nil || strings.TrimSpace(actionHash) == "" {
		return
	}
	store, _ := state.WorkingMemory[key].(map[string]any)
	if store == nil {
		store = map[string]any{}
		state.WorkingMemory[key] = store
	}
	store[actionHash] = cloneWorkflowMap(record)
}

func deepAgentActionReviewApproved(state *DeepAgentState, action DeepAgentAction) bool {
	if state == nil || state.WorkingMemory == nil || strings.TrimSpace(action.Hash) == "" {
		return false
	}
	store, _ := state.WorkingMemory["approved_review_actions"].(map[string]any)
	if _, ok := store[action.Hash]; ok {
		return true
	}
	values := deepAgentStringSlice(state.WorkingMemory["approved_review_actions"])
	for _, value := range values {
		if value == action.Hash {
			return true
		}
	}
	return false
}

func recordDeepAgentPendingReview(state *DeepAgentState, step DeepAgentStep, action DeepAgentAction, reason string, now time.Time) {
	if state == nil {
		return
	}
	if state.WorkingMemory == nil {
		state.WorkingMemory = map[string]any{}
	}
	state.WorkingMemory["pending_review_action"] = map[string]any{
		"step_id":     step.ID,
		"step_title":  step.Title,
		"tool":        action.Tool,
		"action_hash": action.Hash,
		"args":        cloneWorkflowMap(action.Args),
		"reason":      strings.TrimSpace(reason),
		"created_at":  now.Format(time.RFC3339Nano),
	}
}

func applyDeepAgentActionOverride(state *DeepAgentState, action DeepAgentAction) DeepAgentAction {
	if state == nil || state.WorkingMemory == nil || strings.TrimSpace(action.StepID) == "" {
		return action
	}
	overrides, _ := state.WorkingMemory["action_overrides"].(map[string]any)
	raw, ok := overrides[action.StepID]
	if !ok {
		return action
	}
	patch, ok := raw.(map[string]any)
	if !ok || len(patch) == 0 {
		return action
	}
	args := cloneWorkflowMap(action.Args)
	for key, value := range patch {
		args[key] = value
	}
	action.Args = args
	action.Hash = ""
	return action
}

func removeDeepAgentString(values []string, value string) []string {
	if strings.TrimSpace(value) == "" || len(values) == 0 {
		return values
	}
	out := values[:0]
	for _, existing := range values {
		if existing != value {
			out = append(out, existing)
		}
	}
	return out
}

func deepAgentResumeBudgetMap(budget DeepAgentResumeBudget) map[string]any {
	out := map[string]any{}
	if budget.MaxActions > 0 {
		out["max_actions"] = budget.MaxActions
	}
	if budget.MaxDurationMS > 0 {
		out["max_duration_ms"] = budget.MaxDurationMS
	}
	if budget.MaxSteps > 0 {
		out["max_steps"] = budget.MaxSteps
	}
	return out
}

func (c *DeepAgentController) persistState(ctx context.Context, run *WorkflowRun, state *DeepAgentState) {
	if c == nil || c.store == nil || run == nil || state == nil {
		return
	}
	if run.State == nil {
		run.State = map[string]any{}
	}
	previousHandoff := loopHandoffFromAny(run.State["loop_handoff"])
	handoff := updateDeepAgentHandoff(state, c.now())
	run.State["deep_agent_state"] = state
	run.State["deep_agent_status"] = state.Status
	run.State["loop_handoff"] = handoff
	run.State["deep_agent_action_count"] = state.ActionCount
	run.State["deep_agent_recovery"] = deepAgentRecoveryStateForSummary(state)
	run.State["deep_agent_metrics"] = deepAgentLoopMetricsForRun(run, state)
	run.State["deep_agent_timeline"] = deepAgentTimelineForState(state)
	run.State["deep_agent_governance"] = deepAgentGovernanceStateForRun(state)
	run.State["deep_agent_gate_decisions"] = append([]GateDecision(nil), state.GateDecisions...)
	if len(state.GateDecisions) > 0 {
		run.State["last_gate_decision"] = state.GateDecisions[len(state.GateDecisions)-1]
	}
	if !loopHandoffEmpty(handoff) && handoff.Summary != previousHandoff.Summary {
		emitJobEventFromContext(ctx, Event{
			Type:    "deep_agent_handoff",
			Role:    "workflow",
			Content: handoff.Summary,
			Data: deepAgentEventData(map[string]any{
				"event_group":  "run",
				"loop_handoff": handoff,
				"handoff":      handoff,
				"resume_point": handoff.ResumePoint,
			}),
		})
	}
	if state.LoopContract.ID != "" {
		run.State["loop_contract"] = state.LoopContract
		run.State["loop_contract_id"] = state.LoopContract.ID
		run.State["loop_contract_version"] = state.LoopContract.Version
	}
	run.UpdatedAt = c.now()
	if c.evidenceRepo != nil {
		if err := c.evidenceRepo.UpsertRunEvidence(ctx, run, state); err != nil {
			run.State["deep_agent_evidence_store_error"] = deepAgentEvidenceRepositoryError(err)
		} else {
			delete(run.State, "deep_agent_evidence_store_error")
		}
	}
	_ = c.store.UpdateWorkflowRun(ctx, run)
}

func (c *DeepAgentController) now() time.Time {
	if c != nil && c.clock != nil {
		return c.clock.Now().UTC()
	}
	return time.Now().UTC()
}

type ruleDeepAgentPlanner struct{}

func (ruleDeepAgentPlanner) CreatePlan(_ context.Context, req DeepAgentTaskRequest) (DeepAgentPlan, error) {
	goal := strings.TrimSpace(req.Goal)
	if goal == "" {
		return DeepAgentPlan{}, fmt.Errorf("deep agent goal is required")
	}
	if len(req.Plan.Steps) > 0 {
		return normalizeDeepAgentPlan(goal, req.Plan), nil
	}
	if steps := ruleDeepAgentFallbackSteps(goal); len(steps) > 0 {
		return DeepAgentPlan{Goal: goal, Steps: steps}, nil
	}
	return DeepAgentPlan{
		Goal: goal,
		Steps: []DeepAgentStep{{
			ID:            "step-1",
			Title:         goal,
			Intent:        "complete_goal",
			Status:        DeepAgentStepStatusPending,
			DoneCondition: "executor reports completed",
		}},
	}, nil
}

func ruleDeepAgentFallbackSteps(goal string) []DeepAgentStep {
	text := strings.ToLower(strings.TrimSpace(goal))
	if text == "" {
		return nil
	}
	needsArtifact := deepAgentTextRequiresArtifact(text)
	needsResearch := deepAgentContainsAny(text,
		"调研", "调查", "研究", "搜索", "搜集", "收集", "查询", "检索", "产品", "竞品", "市场",
		"research", "investigate", "search", "collect", "gather", "product", "competitor", "market",
	)
	if !needsResearch || !needsArtifact {
		return nil
	}
	finalTitle := "生成并保存最终调研报告文档"
	finalIntent := "Create the final research report document for the user goal and save it as a downloadable artifact: " + goal
	finalDoneCondition := "最终调研报告文档已通过 Artifact 工具生成并可下载"
	if deepAgentTextRequestsDocx(text) {
		finalTitle = "生成并保存最终 Word 调研报告文档"
		finalIntent = "Use the documents or docx skill to create the final .docx Word research report artifact for the user goal: " + goal
		finalDoneCondition = "最终 Word .docx 调研报告已通过 documents/docx skill 生成并可下载"
	}
	return []DeepAgentStep{
		{
			ID:            "step-1",
			Title:         "收集并整理相关公开信息",
			Intent:        "Use web/product research tools to collect factual notes, source URLs, and key evidence.",
			Status:        DeepAgentStepStatusPending,
			DoneCondition: "已收集与目标相关的公开资料、来源链接和关键事实",
			RiskLevel:     "low",
		},
		{
			ID:            "step-2",
			Title:         "分析资料并形成调研报告结构",
			Intent:        "Analyze the collected product information and organize findings into a report outline, positioning, features, competitors, strengths, weaknesses, and risks.",
			DependsOn:     []string{"step-1"},
			Status:        DeepAgentStepStatusPending,
			DoneCondition: "已形成清晰的调研分析和大纲结构",
			RiskLevel:     "low",
		},
		{
			ID:            "step-3",
			Title:         finalTitle,
			Intent:        finalIntent,
			DependsOn:     []string{"step-1", "step-2"},
			Status:        DeepAgentStepStatusPending,
			DoneCondition: finalDoneCondition,
			RiskLevel:     "medium",
		},
	}
}

func (ruleDeepAgentPlanner) NextAction(_ context.Context, state *DeepAgentState, step DeepAgentStep) (DeepAgentAction, error) {
	if state == nil {
		return DeepAgentAction{}, fmt.Errorf("deep agent state is required")
	}
	tool := deepAgentWorkflowString(step.Metadata, "tool")
	args := map[string]any{}
	if rawArgs, ok := step.Metadata["args"].(map[string]any); ok {
		args = cloneWorkflowMap(rawArgs)
	}
	if tool == "" {
		tool = DeepAgentToolModeModel
	}
	if _, ok := args["prompt"]; !ok && tool == DeepAgentToolModeModel {
		args["prompt"] = firstNonEmptyString(step.Title, state.Goal)
	}
	if _, ok := args["query"]; !ok && (tool == DeepAgentToolModeRAGSearch || tool == "search" || tool == "message_search") {
		args["query"] = firstNonEmptyString(step.Title, state.Goal)
	}
	if _, ok := args["query"]; !ok && tool == DeepAgentToolModeModel && strings.EqualFold(deepAgentWorkflowString(args, "search_scope"), "web") {
		args["query"] = state.Goal
	}
	if state.WorkingMemory != nil {
		if userID := deepAgentWorkflowString(state.WorkingMemory, "user_id"); userID != "" {
			args["user_id"] = firstNonEmptyString(deepAgentWorkflowString(args, "user_id"), userID)
		}
		if sessionID := deepAgentWorkflowString(state.WorkingMemory, "session_id"); sessionID != "" {
			args["session_id"] = firstNonEmptyString(deepAgentWorkflowString(args, "session_id"), sessionID)
		}
	}
	attempt := deepAgentStepAttemptCount(state, step.ID) + 1
	if attempt > 1 {
		args["attempt"] = attempt
		args["retry_instruction"] = fmt.Sprintf("Previous attempt %d for step %q did not satisfy the done condition. Use a different strategy and produce evidence that directly satisfies: %s", attempt-1, firstNonEmptyString(step.Title, step.ID), step.DoneCondition)
		if tool == DeepAgentToolModeModel {
			currentPrompt := firstNonEmptyString(deepAgentWorkflowString(args, "prompt"), deepAgentWorkflowString(args, "instruction"), firstNonEmptyString(step.Title, state.Goal))
			args["prompt"] = strings.TrimSpace(currentPrompt + "\n\nRetry instruction: " + deepAgentWorkflowString(args, "retry_instruction"))
		}
	}
	defaults := map[string]any{
		"goal":           state.Goal,
		"step_id":        step.ID,
		"step_title":     step.Title,
		"done_condition": step.DoneCondition,
	}
	if route, ok := deepAgentStepRouteFromMap(step.Metadata); ok {
		route.Mode = normalizeDeepAgentRouteMode(firstNonEmptyString(route.Mode, tool))
		route.Executor = firstNonEmptyString(route.Executor, deepAgentExecutorForMode(route.Mode))
		route.Version = firstNonEmptyString(route.Version, "v1")
		defaults["step_route"] = deepAgentStepRouteMap(route)
		defaults["route_version"] = route.Version
	}
	return DeepAgentAction{
		StepID: step.ID,
		Tool:   tool,
		Args:   mergeDeepAgentActionArgs(args, defaults),
	}, nil
}

func deepAgentStepAttemptCount(state *DeepAgentState, stepID string) int {
	if state == nil || strings.TrimSpace(stepID) == "" {
		return 0
	}
	count := 0
	for _, action := range state.ActionHistory {
		if action.StepID == stepID {
			count++
		}
	}
	return count
}

type noopDeepAgentExecutor struct{}

func (noopDeepAgentExecutor) ExecuteDeepAgentAction(_ context.Context, action DeepAgentAction, _ *DeepAgentState) (DeepAgentActionResult, error) {
	return DeepAgentActionResult{
		Status: DeepAgentActionStatusFailed,
		Error:  "deep agent executor is not configured",
	}, fmt.Errorf("deep agent executor is not configured")
}

type ruleDeepAgentVerifier struct{}

func (ruleDeepAgentVerifier) CheckProgress(_ context.Context, _ *DeepAgentState, step DeepAgentStep, _ DeepAgentAction, result DeepAgentActionResult) (DeepAgentProgress, error) {
	if result.Status == DeepAgentActionStatusFailed {
		return DeepAgentProgress{MadeProgress: false, StepDone: false, Reason: firstNonEmptyString(result.Error, "action failed")}, nil
	}
	if ok, reason := verifyDeepAgentStepResult(step, result); !ok {
		return DeepAgentProgress{MadeProgress: false, StepDone: false, Reason: reason}, nil
	}
	if result.Completed {
		return DeepAgentProgress{MadeProgress: true, StepDone: true, Reason: "action completed step"}, nil
	}
	if strings.TrimSpace(result.Output) != "" {
		return DeepAgentProgress{MadeProgress: true, StepDone: false, Reason: "action produced output"}, nil
	}
	return DeepAgentProgress{MadeProgress: false, StepDone: false, Reason: "action produced no output"}, nil
}

func (ruleDeepAgentVerifier) CheckFinal(_ context.Context, state *DeepAgentState) (DeepAgentFinalVerification, error) {
	if state == nil {
		return DeepAgentFinalVerification{}, fmt.Errorf("deep agent state is required")
	}
	return verifyDeepAgentFinalState(state), nil
}

func normalizeDeepAgentPolicy(policy DeepAgentPolicy) DeepAgentPolicy {
	if policy.MaxSteps <= 0 {
		policy.MaxSteps = DeepAgentDefaultMaxPlanSteps
	}
	if policy.MaxActions <= 0 {
		policy.MaxActions = DeepAgentDefaultMaxActions
	}
	if policy.MaxDuration <= 0 {
		policy.MaxDuration = DeepAgentDefaultMaxDurationMin * time.Minute
	}
	if policy.StepTimeout <= 0 {
		policy.StepTimeout = policy.MaxDuration
	}
	if policy.NoProgressLimit <= 0 {
		policy.NoProgressLimit = DeepAgentDefaultNoProgressLimit
	}
	return policy
}

func normalizeDeepAgentPlan(goal string, plan DeepAgentPlan) DeepAgentPlan {
	plan.Goal = firstNonEmptyString(strings.TrimSpace(plan.Goal), goal)
	for idx := range plan.Steps {
		step := &plan.Steps[idx]
		if strings.TrimSpace(step.ID) == "" {
			step.ID = fmt.Sprintf("step-%d", idx+1)
		}
		if strings.TrimSpace(step.Title) == "" {
			step.Title = step.ID
		}
		if strings.TrimSpace(step.Status) == "" {
			step.Status = DeepAgentStepStatusPending
		}
		normalizeDeepAgentStepDeliverableFormat(goal, step)
	}
	return plan
}

func normalizeDeepAgentStepDeliverableFormat(goal string, step *DeepAgentStep) {
	if step == nil || deepAgentExplicitDocxText(goal) {
		return
	}
	replacer := strings.NewReplacer(
		"Word格式", "Markdown格式",
		"Word 格式", "Markdown 格式",
		"Word文档", "Markdown文档",
		"Word 文档", "Markdown 文档",
		"word文档", "Markdown文档",
		"word 文档", "Markdown 文档",
		"Word document", "Markdown document",
		"word document", "Markdown document",
		".docx", ".md",
		"docx", "Markdown",
		"Word", "Markdown",
	)
	step.Title = replacer.Replace(step.Title)
	step.Intent = replacer.Replace(step.Intent)
	step.DoneCondition = replacer.Replace(step.DoneCondition)
}

func nextDeepAgentStepIndex(steps []DeepAgentStep) int {
	for idx, step := range steps {
		if step.Status == "" || step.Status == DeepAgentStepStatusPending || step.Status == DeepAgentStepStatusRunning {
			return idx
		}
	}
	return -1
}

func nextExecutableDeepAgentStepIndex(state *DeepAgentState) int {
	if state == nil {
		return -1
	}
	blockedByDependencies := false
	for idx, step := range state.Plan.Steps {
		if step.Status != "" && step.Status != DeepAgentStepStatusPending && step.Status != DeepAgentStepStatusRunning {
			continue
		}
		if deepAgentStepDependenciesMet(state, step) {
			return idx
		}
		blockedByDependencies = true
	}
	if blockedByDependencies {
		return -1
	}
	return -1
}

func deepAgentStepDependenciesMet(state *DeepAgentState, step DeepAgentStep) bool {
	if len(step.DependsOn) == 0 {
		return true
	}
	for _, dep := range step.DependsOn {
		dep = strings.TrimSpace(dep)
		if dep == "" {
			continue
		}
		found := false
		for _, completed := range state.CompletedSteps {
			if completed == dep {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func hasPendingDeepAgentSteps(steps []DeepAgentStep) bool {
	for _, step := range steps {
		if step.Status == "" || step.Status == DeepAgentStepStatusPending || step.Status == DeepAgentStepStatusRunning {
			return true
		}
	}
	return false
}

func deepAgentActionHash(action DeepAgentAction) string {
	record := map[string]any{
		"step_id": action.StepID,
		"tool":    action.Tool,
		"args":    action.Args,
	}
	data, err := json.Marshal(record)
	if err != nil {
		data = []byte(action.StepID + ":" + action.Tool)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func appendUniqueString(values []string, value string) []string {
	if strings.TrimSpace(value) == "" {
		return values
	}
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func mergeDeepAgentActionArgs(primary map[string]any, defaults map[string]any) map[string]any {
	out := cloneWorkflowMap(primary)
	for key, value := range defaults {
		if _, ok := out[key]; !ok {
			out[key] = value
		}
	}
	return out
}
