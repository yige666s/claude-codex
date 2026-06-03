package agentruntime

import (
	"encoding/json"
	"fmt"
)

func deepAgentStateFromWorkflowRun(run *WorkflowRun) (*DeepAgentState, error) {
	if run == nil {
		return nil, fmt.Errorf("workflow run is required")
	}
	raw, ok := run.State["deep_agent_state"]
	if !ok || raw == nil {
		return nil, fmt.Errorf("workflow run %s has no deep_agent_state checkpoint", run.ID)
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var state DeepAgentState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}
	if state.TriedActions == nil {
		state.TriedActions = map[string]int{}
	}
	if state.WorkingMemory == nil {
		state.WorkingMemory = map[string]any{}
	}
	if state.CompletedSteps == nil {
		state.CompletedSteps = []string{}
	}
	if state.FailedSteps == nil {
		state.FailedSteps = []string{}
	}
	if state.ActionHistory == nil {
		state.ActionHistory = []DeepAgentAction{}
	}
	if state.Learnings == nil {
		state.Learnings = []DeepAgentLearningCandidate{}
	}
	return &state, nil
}
