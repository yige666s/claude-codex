package agentruntime

import (
	"context"
	"fmt"
	"strings"
)

type RuntimeDeepAgentRiskGate struct {
	runtime *Runtime
}

func NewRuntimeDeepAgentRiskGate(runtime *Runtime) *RuntimeDeepAgentRiskGate {
	return &RuntimeDeepAgentRiskGate{runtime: runtime}
}

func (g *RuntimeDeepAgentRiskGate) ReviewDeepAgentAction(ctx context.Context, run *WorkflowRun, state *DeepAgentState, step DeepAgentStep, action DeepAgentAction) error {
	level := normalizeDeepAgentRiskLevel(firstNonEmptyString(
		step.RiskLevel,
		deepAgentWorkflowString(step.Metadata, "risk_level"),
		deepAgentActionString(action, "risk_level"),
	))
	if level != RiskLevelHigh {
		return nil
	}
	reason := firstNonEmptyString(
		deepAgentWorkflowString(step.Metadata, "risk_reason"),
		deepAgentActionString(action, "risk_reason"),
		fmt.Sprintf("deep agent step %s is marked high risk", firstNonEmptyString(step.ID, step.Title)),
	)
	userID := ""
	sessionID := ""
	if state != nil && state.WorkingMemory != nil {
		userID = deepAgentWorkflowString(state.WorkingMemory, "user_id")
		sessionID = deepAgentWorkflowString(state.WorkingMemory, "session_id")
	}
	runID := ""
	jobID := ""
	if run != nil {
		runID = run.ID
		jobID = run.JobID
		userID = firstNonEmptyString(userID, run.UserID)
		sessionID = firstNonEmptyString(sessionID, run.SessionID)
	}
	if g != nil && g.runtime != nil && g.runtime.riskRecorder != nil {
		g.runtime.riskRecorder(ctx, RiskEvent{
			UserID:     userID,
			SessionID:  sessionID,
			JobID:      firstNonEmptyString(jobID, jobIDFromContext(ctx)),
			RequestID:  requestIDFromContext(ctx),
			Operation:  RiskOperationDeepAgentAction,
			Reason:     reason,
			RiskLevel:  RiskLevelHigh,
			ScoreDelta: riskScoreDelta(RiskLevelHigh),
			Metadata: map[string]any{
				"category":        "deep_agent_human_review",
				"workflow_run_id": runID,
				"step_id":         step.ID,
				"step_title":      step.Title,
				"tool":            action.Tool,
				"action_hash":     action.Hash,
			},
		})
	}
	return fmt.Errorf("%w: %s", ErrDeepAgentReviewRequired, reason)
}

func normalizeDeepAgentRiskLevel(level string) string {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case RiskLevelHigh:
		return RiskLevelHigh
	case RiskLevelMedium:
		return RiskLevelMedium
	case RiskLevelLow:
		return RiskLevelLow
	default:
		return ""
	}
}
