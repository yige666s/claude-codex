package agentruntime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
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
	contextLoader DeepAgentContextLoader
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
		contextLoader: noopDeepAgentContextLoader{},
		clock:         systemClock{},
	}
}

func (c *DeepAgentController) SetContextLoader(loader DeepAgentContextLoader) {
	if c != nil && loader != nil {
		c.contextLoader = loader
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
	req.Policy = normalizeDeepAgentPolicy(req.Policy)
	engine := NewWorkflowEngine(c.store, c.events)
	var state *DeepAgentState
	engine.RegisterStepHandler("initialize_task", func(ctx context.Context, run *WorkflowRun, input map[string]any) (map[string]any, error) {
		now := c.now()
		state = &DeepAgentState{
			Goal:           strings.TrimSpace(req.Goal),
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
		c.persistState(ctx, run, state)
		return map[string]any{
			"goal":             state.Goal,
			"deep_agent_state": state,
			"max_steps":        req.Policy.MaxSteps,
			"max_actions":      req.Policy.MaxActions,
			"max_duration_ms":  req.Policy.MaxDuration.Milliseconds(),
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
		state.WorkingMemory[deepAgentLoadedContextKey] = loaded
		req.State = cloneWorkflowMap(state.WorkingMemory)
		state.UpdatedAt = c.now()
		c.persistState(ctx, run, state)
		return map[string]any{
			"working_memory_keys": len(state.WorkingMemory),
			"has_job_context":     firstNonEmptyString(req.JobID, jobIDFromContext(ctx)) != "",
			"message_count":       len(loaded.RecentMessages),
			"attachment_count":    len(loaded.Attachments),
			"artifact_count":      len(loaded.ExistingArtifacts),
			"skill_count":         len(loaded.SkillCatalog),
			"tool_count":          len(loaded.ToolCatalog),
			"memory_loaded":       strings.TrimSpace(loaded.MemorySummary) != "",
			"issue_count":         len(loaded.Issues),
			"deep_agent_context":  loaded,
			"deep_agent_state":    state,
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
		if len(plan.Steps) > req.Policy.MaxSteps {
			state.Status = DeepAgentRunStatusBudgetExceeded
			state.Blocker = fmt.Sprintf("plan has %d steps, max is %d", len(plan.Steps), req.Policy.MaxSteps)
			c.persistState(ctx, run, state)
			return nil, fmt.Errorf("%w: %s", ErrDeepAgentBudgetExceeded, state.Blocker)
		}
		state.Plan = plan
		state.UpdatedAt = c.now()
		c.persistState(ctx, run, state)
		return map[string]any{
			"planned_step_count": len(plan.Steps),
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
		verification, err := c.verifier.CheckFinal(ctx, state)
		if err != nil {
			return nil, err
		}
		recordDeepAgentFinalVerification(state, verification)
		if !verification.Done {
			state.Status = DeepAgentRunStatusBlocked
			state.Blocker = firstNonEmptyString(verification.Reason, "final verification did not pass")
			state.UpdatedAt = c.now()
			c.persistState(ctx, run, state)
			return map[string]any{"deep_agent_state": state, "final_verification": verification}, fmt.Errorf("%w: %s", ErrDeepAgentBlocked, state.Blocker)
		}
		state.Status = DeepAgentRunStatusSucceeded
		state.UpdatedAt = c.now()
		c.persistState(ctx, run, state)
		return map[string]any{
			"final_status":        state.Status,
			"verification":        verification.Reason,
			"final_verification":  verification,
			"final_artifact_refs": state.WorkingMemory["final_artifact_refs"],
			"deep_agent_state":    state,
		}, nil
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
			"goal":       strings.TrimSpace(req.Goal),
			"request_id": requestIDFromContext(ctx),
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
	policy := normalizeDeepAgentPolicy(req.Policy)
	c.prepareStateForResume(req, state)
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
		verification, err := c.verifier.CheckFinal(ctx, state)
		if err != nil {
			return nil, err
		}
		recordDeepAgentFinalVerification(state, verification)
		if !verification.Done {
			state.Status = DeepAgentRunStatusBlocked
			state.Blocker = firstNonEmptyString(verification.Reason, "final verification did not pass")
			state.UpdatedAt = c.now()
			c.persistState(ctx, run, state)
			return map[string]any{"deep_agent_state": state, "final_verification": verification}, fmt.Errorf("%w: %s", ErrDeepAgentBlocked, state.Blocker)
		}
		state.Status = DeepAgentRunStatusSucceeded
		state.Blocker = ""
		state.UpdatedAt = c.now()
		c.persistState(ctx, run, state)
		return map[string]any{
			"final_status":        state.Status,
			"verification":        verification.Reason,
			"final_verification":  verification,
			"final_artifact_refs": state.WorkingMemory["final_artifact_refs"],
			"deep_agent_state":    state,
		}, nil
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
	state.StartedAt = now
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
	for idx := range state.Plan.Steps {
		switch state.Plan.Steps[idx].Status {
		case DeepAgentStepStatusFailed, DeepAgentStepStatusRunning, "":
			state.Plan.Steps[idx].Status = DeepAgentStepStatusPending
		}
	}
	state.TriedActions = deepAgentTriedActionsForCompletedSteps(state)
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
	state.WorkingMemory["final_verification"] = map[string]any{
		"done":   verification.Done,
		"reason": verification.Reason,
	}
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
		action.Hash = firstNonEmptyString(action.Hash, deepAgentActionHash(action))
		if state.TriedActions == nil {
			state.TriedActions = map[string]int{}
		}
		if err := c.reviewActionRisk(ctx, run, state, step, action); err != nil {
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
			c.emitActionEvent(ctx, run, state, step, action, result, "deep_agent_action_failed", result.Error)
			if !result.Retryable {
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
			c.persistState(ctx, run, state)
			return fmt.Errorf("%w: %s", ErrDeepAgentBlocked, state.Blocker)
		}
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
	output := strings.TrimSpace(result.Output)
	summary := output
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
		"action_hash":   action.Hash,
		"action_count":  0,
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
			if len(evidence.Diagnostics) > 0 {
				payload["diagnostics"] = evidence.Diagnostics
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
	return c.riskGate.ReviewDeepAgentAction(ctx, run, state, step, action)
}

func (c *DeepAgentController) persistState(ctx context.Context, run *WorkflowRun, state *DeepAgentState) {
	if c == nil || c.store == nil || run == nil || state == nil {
		return
	}
	if run.State == nil {
		run.State = map[string]any{}
	}
	run.State["deep_agent_state"] = state
	run.State["deep_agent_status"] = state.Status
	run.State["deep_agent_action_count"] = state.ActionCount
	run.UpdatedAt = c.now()
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
			Title:         "生成并保存最终调研报告文档",
			Intent:        "Create the final research report document for the user goal and save it as a downloadable artifact: " + goal,
			DependsOn:     []string{"step-1", "step-2"},
			Status:        DeepAgentStepStatusPending,
			DoneCondition: "最终调研报告文档已通过 Artifact 工具生成并可下载",
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
	return DeepAgentAction{
		StepID: step.ID,
		Tool:   tool,
		Args: mergeDeepAgentActionArgs(args, map[string]any{
			"goal":           state.Goal,
			"step_id":        step.ID,
			"step_title":     step.Title,
			"done_condition": step.DoneCondition,
		}),
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
	for _, step := range state.Plan.Steps {
		if step.Status != DeepAgentStepStatusSucceeded && step.Status != DeepAgentStepStatusSkipped {
			return DeepAgentFinalVerification{Done: false, Reason: "not all steps completed"}, nil
		}
	}
	if deepAgentTextRequiresArtifact(firstNonEmptyString(state.Goal, state.Plan.Goal)) {
		refs := deepAgentStateCurrentArtifactRefs(state)
		if len(refs) == 0 {
			return DeepAgentFinalVerification{Done: false, Reason: "required final artifact is missing"}, nil
		}
		deliverable := deepAgentGoalDeliverableType(firstNonEmptyString(state.Goal, state.Plan.Goal))
		matching := deepAgentArtifactRefsMatchFinalDeliverable(refs, deliverable)
		if len(matching) == 0 {
			return DeepAgentFinalVerification{Done: false, Reason: fmt.Sprintf("final artifact does not match deliverable type %s", deliverable)}, nil
		}
		if state.WorkingMemory == nil {
			state.WorkingMemory = map[string]any{}
		}
		state.WorkingMemory["final_artifact_refs"] = matching
		return DeepAgentFinalVerification{Done: true, Reason: "all steps completed and final artifact verified"}, nil
	}
	return DeepAgentFinalVerification{Done: true, Reason: "all steps completed"}, nil
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
