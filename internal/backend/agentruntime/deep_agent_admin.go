package agentruntime

type DeepAgentWorkflowSummary struct {
	Present        bool                         `json:"present"`
	Goal           string                       `json:"goal,omitempty"`
	Status         string                       `json:"status,omitempty"`
	Blocker        string                       `json:"blocker,omitempty"`
	CurrentStepID  string                       `json:"current_step_id,omitempty"`
	CurrentStep    *DeepAgentStep               `json:"current_step,omitempty"`
	Plan           DeepAgentPlan                `json:"plan,omitempty"`
	StepContext    map[string]any               `json:"step_context,omitempty"`
	Routes         []map[string]any             `json:"routes,omitempty"`
	Evidence       []DeepAgentStepEvidence      `json:"evidence,omitempty"`
	ArtifactRefs   []DeepAgentArtifactRef       `json:"artifact_refs,omitempty"`
	FinalVerifier  map[string]any               `json:"final_verifier,omitempty"`
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
		StepContext:    deepAgentStepContextForSummary(state),
		Routes:         deepAgentRoutesForSummary(state),
		Evidence:       deepAgentEvidenceForSummary(state),
		ArtifactRefs:   deepAgentStateCurrentArtifactRefs(state),
		FinalVerifier:  deepAgentFinalVerifierForSummary(state),
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

func deepAgentStepContextForSummary(state *DeepAgentState) map[string]any {
	if state == nil || state.WorkingMemory == nil {
		return nil
	}
	store, _ := state.WorkingMemory["step_context"].(map[string]any)
	return cloneWorkflowMap(store)
}

func deepAgentFinalVerifierForSummary(state *DeepAgentState) map[string]any {
	if state == nil || state.WorkingMemory == nil {
		return nil
	}
	if raw, ok := state.WorkingMemory["final_verification"].(map[string]any); ok {
		return cloneWorkflowMap(raw)
	}
	return nil
}

func deepAgentRoutesForSummary(state *DeepAgentState) []map[string]any {
	if state == nil {
		return nil
	}
	out := make([]map[string]any, 0, len(state.ActionHistory))
	for _, action := range state.ActionHistory {
		route, ok := deepAgentStepRouteFromMap(action.Args)
		if !ok {
			continue
		}
		item := deepAgentStepRouteMap(route)
		item["action_hash"] = action.Hash
		item["action_tool"] = action.Tool
		out = append(out, item)
	}
	return out
}

func deepAgentEvidenceForSummary(state *DeepAgentState) []DeepAgentStepEvidence {
	if state == nil || state.WorkingMemory == nil {
		return nil
	}
	store, _ := state.WorkingMemory["step_context"].(map[string]any)
	out := make([]DeepAgentStepEvidence, 0, len(store))
	for _, raw := range store {
		record, _ := raw.(map[string]any)
		if len(record) == 0 {
			continue
		}
		if metadata, _ := record["metadata"].(map[string]any); len(metadata) > 0 {
			if evidence, ok := deepAgentStepEvidenceFromAny(metadata["step_evidence"]); ok {
				out = append(out, evidence)
			}
		}
		if evidence, ok := deepAgentStepEvidenceFromAny(record["step_evidence"]); ok {
			out = append(out, evidence)
		}
	}
	return out
}
