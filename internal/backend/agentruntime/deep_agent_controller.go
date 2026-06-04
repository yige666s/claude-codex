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
	store        WorkflowStore
	events       WorkflowEventSink
	planner      DeepAgentPlanner
	executor     DeepAgentExecutor
	verifier     DeepAgentVerifier
	riskGate     DeepAgentRiskGate
	learningSink DeepAgentLearningSink
	clock        Clock
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
		store:    store,
		events:   events,
		planner:  planner,
		executor: executor,
		verifier: verifier,
		clock:    systemClock{},
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
		return map[string]any{
			"working_memory_keys": len(state.WorkingMemory),
			"has_job_context":     firstNonEmptyString(req.JobID, jobIDFromContext(ctx)) != "",
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
		if !verification.Done {
			state.Status = DeepAgentRunStatusBlocked
			state.Blocker = firstNonEmptyString(verification.Reason, "final verification did not pass")
			state.UpdatedAt = c.now()
			c.persistState(ctx, run, state)
			return map[string]any{"deep_agent_state": state}, fmt.Errorf("%w: %s", ErrDeepAgentBlocked, state.Blocker)
		}
		state.Status = DeepAgentRunStatusSucceeded
		state.UpdatedAt = c.now()
		c.persistState(ctx, run, state)
		return map[string]any{
			"final_status":     state.Status,
			"verification":     verification.Reason,
			"deep_agent_state": state,
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
		if !verification.Done {
			state.Status = DeepAgentRunStatusBlocked
			state.Blocker = firstNonEmptyString(verification.Reason, "final verification did not pass")
			state.UpdatedAt = c.now()
			c.persistState(ctx, run, state)
			return map[string]any{"deep_agent_state": state}, fmt.Errorf("%w: %s", ErrDeepAgentBlocked, state.Blocker)
		}
		state.Status = DeepAgentRunStatusSucceeded
		state.Blocker = ""
		state.UpdatedAt = c.now()
		c.persistState(ctx, run, state)
		return map[string]any{
			"final_status":     state.Status,
			"verification":     verification.Reason,
			"deep_agent_state": state,
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
		stepIndex := nextDeepAgentStepIndex(state.Plan.Steps)
		if stepIndex < 0 {
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
		action.Tool = firstNonEmptyString(strings.TrimSpace(action.Tool), "model")
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
		if execErr != nil {
			result.Status = DeepAgentActionStatusFailed
			result.Error = execErr.Error()
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
		} else {
			state.NoProgressCount++
			state.Blocker = firstNonEmptyString(progress.Reason, result.Error, "no progress")
		}
		if progress.StepDone {
			state.Plan.Steps[stepIndex].Status = DeepAgentStepStatusSucceeded
			state.CompletedSteps = appendUniqueString(state.CompletedSteps, step.ID)
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

func (c *DeepAgentController) emitActionEvent(ctx context.Context, run *WorkflowRun, state *DeepAgentState, step DeepAgentStep, action DeepAgentAction, result DeepAgentActionResult, eventType, errorText string) {
	if c == nil || run == nil {
		return
	}
	tool := strings.TrimSpace(action.Tool)
	if tool == "" {
		tool = "model"
	}
	payload := map[string]any{
		"type":          eventType,
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
		"action_hash":   action.Hash,
		"action_count":  0,
	}
	if state != nil {
		payload["action_count"] = state.ActionCount
		payload["deep_agent_status"] = state.Status
	}
	if tool == "skill" {
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
		tool = "model"
	}
	if _, ok := args["prompt"]; !ok && tool == "model" {
		args["prompt"] = firstNonEmptyString(step.Title, state.Goal)
	}
	if _, ok := args["query"]; !ok && (tool == "rag_search" || tool == "search" || tool == "message_search") {
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
		if tool == "model" {
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
	return DeepAgentFinalVerification{Done: true, Reason: "all steps completed"}, nil
}

func normalizeDeepAgentPolicy(policy DeepAgentPolicy) DeepAgentPolicy {
	if policy.MaxSteps <= 0 {
		policy.MaxSteps = 8
	}
	if policy.MaxActions <= 0 {
		policy.MaxActions = 16
	}
	if policy.MaxDuration <= 0 {
		policy.MaxDuration = 2 * time.Minute
	}
	if policy.StepTimeout <= 0 {
		policy.StepTimeout = policy.MaxDuration
	}
	if policy.NoProgressLimit <= 0 {
		policy.NoProgressLimit = 3
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
	}
	return plan
}

func nextDeepAgentStepIndex(steps []DeepAgentStep) int {
	for idx, step := range steps {
		if step.Status == "" || step.Status == DeepAgentStepStatusPending || step.Status == DeepAgentStepStatusRunning {
			return idx
		}
	}
	return -1
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
