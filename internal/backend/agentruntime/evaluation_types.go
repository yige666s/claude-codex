package agentruntime

import "time"

const (
	EvaluationRunStatusPending   = "pending"
	EvaluationRunStatusRunning   = "running"
	EvaluationRunStatusCompleted = "completed"
	EvaluationRunStatusFailed    = "failed"

	EvaluationResultStatusPassed  = "passed"
	EvaluationResultStatusFailed  = "failed"
	EvaluationResultStatusWarning = "warning"

	EvaluationReviewStatusPending = "pending"
	EvaluationReviewStatusPassed  = "passed"
	EvaluationReviewStatusFailed  = "failed"
	EvaluationReviewStatusIgnored = "ignored"

	EvaluationSubjectJob            = "job"
	EvaluationSubjectSession        = "session"
	EvaluationSubjectSkillExecution = "skill_execution"
)

type EvaluationScope struct {
	From          *time.Time `json:"from,omitempty"`
	To            *time.Time `json:"to,omitempty"`
	SubjectType   string     `json:"subject_type,omitempty"`
	UserID        string     `json:"user_id,omitempty"`
	SessionID     string     `json:"session_id,omitempty"`
	JobID         string     `json:"job_id,omitempty"`
	JobStatus     string     `json:"job_status,omitempty"`
	SkillName     string     `json:"skill_name,omitempty"`
	Provider      string     `json:"provider,omitempty"`
	Model         string     `json:"model,omitempty"`
	PromptID      string     `json:"prompt_id,omitempty"`
	PromptVersion string     `json:"prompt_version,omitempty"`
	PromptHash    string     `json:"prompt_hash,omitempty"`
	ExperimentID  string     `json:"experiment_id,omitempty"`
	VariantID     string     `json:"variant_id,omitempty"`
}

type EvaluationRun struct {
	ID              string          `json:"id"`
	Name            string          `json:"name"`
	Status          string          `json:"status"`
	Trigger         string          `json:"trigger,omitempty"`
	Scope           EvaluationScope `json:"scope"`
	StartedAt       time.Time       `json:"started_at"`
	CompletedAt     *time.Time      `json:"completed_at,omitempty"`
	Total           int             `json:"total"`
	Passed          int             `json:"passed"`
	Failed          int             `json:"failed"`
	Warning         int             `json:"warning"`
	Metrics         map[string]any  `json:"metrics,omitempty"`
	ThresholdStatus string          `json:"threshold_status,omitempty"`
	Summary         string          `json:"summary,omitempty"`
}

type EvaluationResult struct {
	ID            string              `json:"id"`
	RunID         string              `json:"run_id"`
	SubjectType   string              `json:"subject_type"`
	SubjectID     string              `json:"subject_id"`
	UserID        string              `json:"user_id,omitempty"`
	SessionID     string              `json:"session_id,omitempty"`
	JobID         string              `json:"job_id,omitempty"`
	SkillName     string              `json:"skill_name,omitempty"`
	Provider      string              `json:"provider,omitempty"`
	Model         string              `json:"model,omitempty"`
	PromptID      string              `json:"prompt_id,omitempty"`
	PromptVersion string              `json:"prompt_version,omitempty"`
	PromptHash    string              `json:"prompt_hash,omitempty"`
	ExperimentID  string              `json:"experiment_id,omitempty"`
	VariantID     string              `json:"variant_id,omitempty"`
	Status        string              `json:"status"`
	Score         float64             `json:"score"`
	Input         string              `json:"input,omitempty"`
	Output        string              `json:"output,omitempty"`
	Metrics       map[string]any      `json:"metrics,omitempty"`
	Findings      []EvaluationFinding `json:"findings,omitempty"`
	CreatedAt     time.Time           `json:"created_at"`
}

type EvaluationFinding struct {
	Severity string         `json:"severity"`
	Code     string         `json:"code"`
	Message  string         `json:"message"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

type EvaluationReview struct {
	ID        string    `json:"id"`
	ResultID  string    `json:"result_id"`
	Status    string    `json:"status"`
	Reviewer  string    `json:"reviewer,omitempty"`
	Note      string    `json:"note,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type EvaluationRunFilter struct {
	Status  string
	Trigger string
	Limit   int
}

type EvaluationResultFilter struct {
	RunID         string
	Status        string
	SubjectType   string
	UserID        string
	SessionID     string
	JobID         string
	SkillName     string
	Provider      string
	Model         string
	PromptID      string
	PromptVersion string
	PromptHash    string
	ExperimentID  string
	VariantID     string
	Limit         int
}

type EvaluationReviewFilter struct {
	ResultID string
	Status   string
	Limit    int
}

type EvaluationRunSummary struct {
	RunID           string         `json:"run_id"`
	Total           int            `json:"total"`
	Passed          int            `json:"passed"`
	Failed          int            `json:"failed"`
	Warning         int            `json:"warning"`
	PassRate        float64        `json:"pass_rate"`
	FailureRate     float64        `json:"failure_rate"`
	WarningRate     float64        `json:"warning_rate"`
	Metrics         map[string]any `json:"metrics,omitempty"`
	ThresholdStatus string         `json:"threshold_status,omitempty"`
}
