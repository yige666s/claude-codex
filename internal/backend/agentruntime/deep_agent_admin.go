package agentruntime

type DeepAgentWorkflowSummary struct {
	Present        bool                         `json:"present"`
	Goal           string                       `json:"goal,omitempty"`
	Status         string                       `json:"status,omitempty"`
	Blocker        string                       `json:"blocker,omitempty"`
	CurrentStepID  string                       `json:"current_step_id,omitempty"`
	CurrentStep    *DeepAgentStep               `json:"current_step,omitempty"`
	Plan           DeepAgentPlan                `json:"plan,omitempty"`
	ActionHistory  []DeepAgentAction            `json:"action_history,omitempty"`
	Learnings      []DeepAgentLearningCandidate `json:"learnings,omitempty"`
	CompletedCount int                          `json:"completed_count"`
	FailedCount    int                          `json:"failed_count"`
	ActionCount    int                          `json:"action_count"`
	NoProgress     int                          `json:"no_progress_count"`
}

func DeepAgentSummaryFromWorkflowRun(run *WorkflowRun) (*DeepAgentWorkflowSummary, bool) {
	if run == nil || run.Name != deepAgentTaskWorkflowName {
		return nil, false
	}
	state, err := deepAgentStateFromWorkflowRun(run)
	if err != nil || state == nil {
		return &DeepAgentWorkflowSummary{Present: false}, true
	}
	summary := &DeepAgentWorkflowSummary{
		Present:        true,
		Goal:           state.Goal,
		Status:         state.Status,
		Blocker:        state.Blocker,
		Plan:           state.Plan,
		ActionHistory:  state.ActionHistory,
		Learnings:      state.Learnings,
		CompletedCount: len(state.CompletedSteps),
		FailedCount:    len(state.FailedSteps),
		ActionCount:    state.ActionCount,
		NoProgress:     state.NoProgressCount,
	}
	if state.CurrentStepIndex >= 0 && state.CurrentStepIndex < len(state.Plan.Steps) {
		step := state.Plan.Steps[state.CurrentStepIndex]
		summary.CurrentStepID = step.ID
		summary.CurrentStep = &step
	}
	return summary, true
}
