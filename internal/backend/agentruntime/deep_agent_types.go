package agentruntime

import (
	"context"
	"time"
)

const (
	deepAgentTaskWorkflowName    = "deep_agent_task"
	deepAgentTaskWorkflowVersion = "v1"

	DeepAgentStepStatusPending   = "pending"
	DeepAgentStepStatusRunning   = "running"
	DeepAgentStepStatusSucceeded = "succeeded"
	DeepAgentStepStatusFailed    = "failed"
	DeepAgentStepStatusSkipped   = "skipped"

	DeepAgentActionStatusSucceeded = "succeeded"
	DeepAgentActionStatusFailed    = "failed"

	DeepAgentRunStatusRunning        = "running"
	DeepAgentRunStatusSucceeded      = "succeeded"
	DeepAgentRunStatusBlocked        = "blocked"
	DeepAgentRunStatusBudgetExceeded = "budget_exceeded"
	DeepAgentRunStatusReviewPending  = "review_pending"
)

type DeepAgentPolicy struct {
	MaxSteps        int           `json:"max_steps"`
	MaxActions      int           `json:"max_actions"`
	MaxDuration     time.Duration `json:"max_duration"`
	StepTimeout     time.Duration `json:"step_timeout"`
	NoProgressLimit int           `json:"no_progress_limit"`
}

type DeepAgentTaskRequest struct {
	UserID    string          `json:"user_id,omitempty"`
	SessionID string          `json:"session_id,omitempty"`
	JobID     string          `json:"job_id,omitempty"`
	Goal      string          `json:"goal"`
	Plan      DeepAgentPlan   `json:"plan,omitempty"`
	Policy    DeepAgentPolicy `json:"policy,omitempty"`
	State     map[string]any  `json:"state,omitempty"`
}

type DeepAgentResumeRequest struct {
	RunID      string          `json:"run_id"`
	Policy     DeepAgentPolicy `json:"policy,omitempty"`
	StatePatch map[string]any  `json:"state_patch,omitempty"`
}

type DeepAgentTaskResult struct {
	Run   *WorkflowRun       `json:"run,omitempty"`
	State *DeepAgentState    `json:"state,omitempty"`
	Error string             `json:"error,omitempty"`
	Steps []*WorkflowStepRun `json:"steps,omitempty"`
}

type DeepAgentLearningCandidate struct {
	ID        string         `json:"id"`
	Type      string         `json:"type"`
	Content   string         `json:"content"`
	Status    string         `json:"status"`
	Source    string         `json:"source,omitempty"`
	UserID    string         `json:"user_id,omitempty"`
	SessionID string         `json:"session_id,omitempty"`
	RunID     string         `json:"run_id,omitempty"`
	StepID    string         `json:"step_id,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
}

type DeepAgentPlan struct {
	Goal  string          `json:"goal"`
	Steps []DeepAgentStep `json:"steps"`
}

type DeepAgentStep struct {
	ID            string         `json:"id"`
	Title         string         `json:"title"`
	Intent        string         `json:"intent,omitempty"`
	Status        string         `json:"status"`
	DoneCondition string         `json:"done_condition,omitempty"`
	RiskLevel     string         `json:"risk_level,omitempty"`
	Metadata      map[string]any `json:"metadata,omitempty"`
}

type DeepAgentAction struct {
	ID     string         `json:"id,omitempty"`
	StepID string         `json:"step_id"`
	Tool   string         `json:"tool"`
	Args   map[string]any `json:"args,omitempty"`
	Hash   string         `json:"hash,omitempty"`
}

type DeepAgentActionResult struct {
	Status    string         `json:"status"`
	Output    string         `json:"output,omitempty"`
	Completed bool           `json:"completed,omitempty"`
	Retryable bool           `json:"retryable,omitempty"`
	Error     string         `json:"error,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

type DeepAgentProgress struct {
	MadeProgress bool   `json:"made_progress"`
	StepDone     bool   `json:"step_done"`
	Reason       string `json:"reason,omitempty"`
}

type DeepAgentFinalVerification struct {
	Done   bool   `json:"done"`
	Reason string `json:"reason,omitempty"`
}

type DeepAgentState struct {
	Goal             string                       `json:"goal"`
	Plan             DeepAgentPlan                `json:"plan"`
	CurrentStepIndex int                          `json:"current_step_index"`
	CompletedSteps   []string                     `json:"completed_steps"`
	FailedSteps      []string                     `json:"failed_steps"`
	TriedActions     map[string]int               `json:"tried_actions"`
	ActionHistory    []DeepAgentAction            `json:"action_history"`
	ActionCount      int                          `json:"action_count"`
	NoProgressCount  int                          `json:"no_progress_count"`
	Status           string                       `json:"status"`
	Blocker          string                       `json:"blocker,omitempty"`
	StartedAt        time.Time                    `json:"started_at"`
	UpdatedAt        time.Time                    `json:"updated_at"`
	WorkingMemory    map[string]any               `json:"working_memory,omitempty"`
	Learnings        []DeepAgentLearningCandidate `json:"learnings,omitempty"`
}

type DeepAgentPlanner interface {
	CreatePlan(ctx context.Context, req DeepAgentTaskRequest) (DeepAgentPlan, error)
	NextAction(ctx context.Context, state *DeepAgentState, step DeepAgentStep) (DeepAgentAction, error)
}

type DeepAgentExecutor interface {
	ExecuteDeepAgentAction(ctx context.Context, action DeepAgentAction, state *DeepAgentState) (DeepAgentActionResult, error)
}

type DeepAgentVerifier interface {
	CheckProgress(ctx context.Context, state *DeepAgentState, step DeepAgentStep, action DeepAgentAction, result DeepAgentActionResult) (DeepAgentProgress, error)
	CheckFinal(ctx context.Context, state *DeepAgentState) (DeepAgentFinalVerification, error)
}

type DeepAgentRiskGate interface {
	ReviewDeepAgentAction(ctx context.Context, run *WorkflowRun, state *DeepAgentState, step DeepAgentStep, action DeepAgentAction) error
}

type DeepAgentLearningSink interface {
	PersistDeepAgentLearnings(ctx context.Context, run *WorkflowRun, state *DeepAgentState, candidates []DeepAgentLearningCandidate) error
}
