package agentruntime

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

type LoopGoalRunResult struct {
	Goal      *LoopGoal                 `json:"goal,omitempty"`
	Job       *Job                      `json:"job,omitempty"`
	Run       *WorkflowRun              `json:"run,omitempty"`
	DeepAgent *DeepAgentWorkflowSummary `json:"deep_agent,omitempty"`
}

func (r *Runtime) CreateLoopGoal(ctx context.Context, goal *LoopGoal) (*LoopGoal, error) {
	if r == nil {
		return nil, fmt.Errorf("runtime is not configured")
	}
	if r.loopGoals == nil {
		return nil, fmt.Errorf("loop goal store is not configured")
	}
	goal = normalizeLoopGoal(goal)
	if goal == nil || strings.TrimSpace(goal.UserID) == "" {
		return nil, fmt.Errorf("user ID is required")
	}
	if strings.TrimSpace(goal.SessionID) == "" {
		return nil, fmt.Errorf("session ID is required")
	}
	if strings.TrimSpace(goal.Objective) == "" {
		return nil, fmt.Errorf("objective is required")
	}
	if _, err := r.GetSession(ctx, goal.UserID, goal.SessionID); err != nil {
		return nil, err
	}
	if err := r.loopGoals.UpsertLoopGoal(ctx, goal); err != nil {
		return nil, err
	}
	return cloneLoopGoal(goal), nil
}

func (r *Runtime) GetLoopGoal(ctx context.Context, userID, goalID string) (*LoopGoal, error) {
	if r == nil || r.loopGoals == nil {
		return nil, fmt.Errorf("loop goal store is not configured")
	}
	return r.loopGoals.GetLoopGoal(ctx, userID, goalID)
}

func (r *Runtime) ListLoopGoals(ctx context.Context, filter LoopGoalFilter) ([]*LoopGoal, error) {
	if r == nil || r.loopGoals == nil {
		return []*LoopGoal{}, nil
	}
	return r.loopGoals.ListLoopGoals(ctx, filter)
}

func (r *Runtime) StartLoopGoalRun(ctx context.Context, userID, goalID string) (*LoopGoalRunResult, error) {
	if r == nil {
		return nil, fmt.Errorf("runtime is not configured")
	}
	goal, err := r.GetLoopGoal(ctx, userID, goalID)
	if err != nil {
		return nil, err
	}
	job, err := r.CreateJob(ctx, ChatRequest{
		UserID:     goal.UserID,
		SessionID:  goal.SessionID,
		LoopGoalID: goal.ID,
		Content:    goal.Objective,
		AgentMode:  AgentModePlanExecute,
	}, JobTypeDeepAgent)
	if err != nil {
		return nil, err
	}
	if err := r.StartJob(ctx, job); err != nil {
		return nil, err
	}
	goal.JobID = job.ID
	goal.Status = LoopGoalStatusPending
	goal.UpdatedAt = time.Now().UTC()
	return &LoopGoalRunResult{Goal: goal, Job: job}, nil
}

func (r *Runtime) GetLoopRun(ctx context.Context, userID, runID string) (*LoopGoalRunResult, error) {
	if r == nil {
		return nil, fmt.Errorf("runtime is not configured")
	}
	run, err := r.GetWorkflowRun(ctx, runID)
	if err != nil {
		return nil, err
	}
	if run == nil || run.UserID != strings.TrimSpace(userID) {
		return nil, sql.ErrNoRows
	}
	var goal *LoopGoal
	if r.loopGoals != nil {
		if found, err := r.loopGoals.GetLoopGoalByWorkflowRun(ctx, userID, runID); err == nil {
			goal = found
		}
	}
	var job *Job
	if r.jobs != nil && strings.TrimSpace(run.JobID) != "" {
		if found, err := r.jobs.GetJob(ctx, userID, run.JobID); err == nil {
			job = found
		}
	}
	summary, _ := DeepAgentSummaryFromWorkflowRun(run)
	return &LoopGoalRunResult{Goal: goal, Job: job, Run: run, DeepAgent: summary}, nil
}

func (r *Runtime) ResumeLoopRun(ctx context.Context, userID, runID string) (*LoopGoalRunResult, error) {
	return r.ResumeLoopRunWithRequest(ctx, userID, DeepAgentResumeRequest{RunID: runID})
}

func (r *Runtime) ResumeLoopRunWithRequest(ctx context.Context, userID string, req DeepAgentResumeRequest) (*LoopGoalRunResult, error) {
	if r == nil {
		return nil, fmt.Errorf("runtime is not configured")
	}
	runID := strings.TrimSpace(req.RunID)
	if runID == "" {
		return nil, fmt.Errorf("loop run id is required")
	}
	run, err := r.GetWorkflowRun(ctx, runID)
	if err != nil {
		return nil, err
	}
	if run == nil || run.UserID != strings.TrimSpace(userID) {
		return nil, sql.ErrNoRows
	}
	req.RunID = runID
	result, err := r.ResumeDeepAgentTask(ctx, req, nil, nil, nil)
	if err != nil {
		return nil, err
	}
	var goal *LoopGoal
	if r.loopGoals != nil {
		if found, findErr := r.loopGoals.GetLoopGoalByWorkflowRun(ctx, userID, runID); findErr == nil {
			goal = found
			status := LoopGoalStatusRunning
			if result != nil && result.State != nil {
				status = loopGoalStatusFromDeepAgent(result.State.Status)
			}
			_ = r.loopGoals.UpdateLoopGoalRun(ctx, userID, goal.ID, run.JobID, runID, status, time.Now().UTC())
		}
	}
	summary, _ := DeepAgentSummaryFromWorkflowRun(result.Run)
	return &LoopGoalRunResult{Goal: goal, Run: result.Run, DeepAgent: summary}, nil
}
