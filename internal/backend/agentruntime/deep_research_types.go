package agentruntime

import "time"

const (
	deepResearchWorkflowVersionV1 = "deep_research_orchestrator_worker_v1"
	deepResearchWorkflowVersion   = "deep_research_orchestrator_worker_v2"

	DeepResearchWorkerBackendInline       = "inline"
	DeepResearchWorkerBackendHarnessAgent = "harness_agent"

	DeepResearchTaskStatusPending             = "pending"
	DeepResearchTaskStatusReady               = "ready"
	DeepResearchTaskStatusRunning             = "running"
	DeepResearchTaskStatusSucceeded           = "succeeded"
	DeepResearchTaskStatusFailed              = "failed"
	DeepResearchTaskStatusFailedFinal         = "failed_final"
	DeepResearchTaskStatusRetrying            = "retrying"
	DeepResearchTaskStatusBlockedByDependency = "blocked_by_dependency"
	DeepResearchTaskStatusSkipped             = "skipped"
	DeepResearchTaskStatusCancelled           = "cancelled"

	DeepResearchRunStatusRunning   = "running"
	DeepResearchRunStatusSucceeded = "succeeded"
	DeepResearchRunStatusFailed    = "failed"
	DeepResearchRunStatusPartial   = "partial"
	DeepResearchRunStatusCancelled = "cancelled"
)

func isDeepResearchWorkflowVersion(version string) bool {
	switch version {
	case deepResearchWorkflowVersion, deepResearchWorkflowVersionV1:
		return true
	default:
		return false
	}
}

type DeepResearchPlan struct {
	Goal           string                 `json:"goal"`
	MaxConcurrency int                    `json:"max_concurrency,omitempty"`
	Nodes          []DeepResearchTaskNode `json:"nodes"`
}

type DeepResearchTaskNode struct {
	ID              string                    `json:"id"`
	Title           string                    `json:"title"`
	Description     string                    `json:"description,omitempty"`
	DependsOn       []string                  `json:"depends_on,omitempty"`
	WorkerRole      string                    `json:"worker_role,omitempty"`
	AllowedTools    []string                  `json:"allowed_tools,omitempty"`
	ExpectedOutput  string                    `json:"expected_output,omitempty"`
	Required        bool                      `json:"required"`
	MaxAttempts     int                       `json:"max_attempts,omitempty"`
	TimeoutMS       int64                     `json:"timeout_ms,omitempty"`
	Status          string                    `json:"status,omitempty"`
	Attempt         int                       `json:"attempt,omitempty"`
	StartedAt       *time.Time                `json:"started_at,omitempty"`
	CompletedAt     *time.Time                `json:"completed_at,omitempty"`
	AgentRunID      string                    `json:"agent_run_id,omitempty"`
	Result          *DeepResearchWorkerResult `json:"result,omitempty"`
	Error           string                    `json:"error,omitempty"`
	Metadata        map[string]any            `json:"metadata,omitempty"`
	BlockedBy       []string                  `json:"blocked_by,omitempty"`
	LastHeartbeatAt *time.Time                `json:"last_heartbeat_at,omitempty"`
}

type DeepResearchRunState struct {
	Version      string                            `json:"version"`
	Status       string                            `json:"status"`
	Goal         string                            `json:"goal"`
	Plan         DeepResearchPlan                  `json:"plan"`
	WorkerRuns   map[string]DeepResearchTaskNode   `json:"worker_runs,omitempty"`
	Aggregate    DeepResearchAggregateResult       `json:"aggregate,omitempty"`
	StartedAt    time.Time                         `json:"started_at,omitempty"`
	CompletedAt  *time.Time                        `json:"completed_at,omitempty"`
	Config       DeepResearchRuntimeConfigSnapshot `json:"config,omitempty"`
	RecoveryHint string                            `json:"recovery_hint,omitempty"`
}

type DeepResearchRuntimeConfigSnapshot struct {
	WorkerBackend        string `json:"worker_backend,omitempty"`
	MaxWorkers           int    `json:"max_workers,omitempty"`
	MaxConcurrency       int    `json:"max_concurrency,omitempty"`
	WorkerTimeoutMS      int64  `json:"worker_timeout_ms,omitempty"`
	TotalTimeoutMS       int64  `json:"total_timeout_ms,omitempty"`
	MaxRetries           int    `json:"max_retries,omitempty"`
	RequireSources       bool   `json:"require_sources,omitempty"`
	MinSuccessfulWorkers int    `json:"min_successful_workers,omitempty"`
}

type DeepResearchWorkerInput struct {
	UserID           string                     `json:"user_id,omitempty"`
	SessionID        string                     `json:"session_id,omitempty"`
	JobID            string                     `json:"job_id,omitempty"`
	Goal             string                     `json:"goal"`
	Node             DeepResearchTaskNode       `json:"node"`
	DependencyOutput []DeepResearchWorkerResult `json:"dependency_output,omitempty"`
	ConnectorContext []string                   `json:"connector_context,omitempty"`
	WorkingMemory    map[string]any             `json:"working_memory,omitempty"`
	Backend          string                     `json:"backend,omitempty"`
}

type DeepResearchWorkerResult struct {
	Status        string                 `json:"status"`
	Summary       string                 `json:"summary,omitempty"`
	Output        string                 `json:"output,omitempty"`
	Findings      []DeepResearchFinding  `json:"findings,omitempty"`
	Sources       []DeepAgentSourceRef   `json:"sources,omitempty"`
	Artifacts     []DeepAgentArtifactRef `json:"artifacts,omitempty"`
	ToolCalls     []DeepAgentToolCallRef `json:"tool_calls,omitempty"`
	OpenQuestions []string               `json:"open_questions,omitempty"`
	Errors        []string               `json:"errors,omitempty"`
	AgentRunID    string                 `json:"agent_run_id,omitempty"`
	Metadata      map[string]any         `json:"metadata,omitempty"`
}

type DeepResearchFinding struct {
	Claim      string `json:"claim,omitempty"`
	Evidence   string `json:"evidence,omitempty"`
	SourceURL  string `json:"source_url,omitempty"`
	Confidence string `json:"confidence,omitempty"`
}

type DeepResearchAggregateResult struct {
	Status        string                              `json:"status,omitempty"`
	Summary       string                              `json:"summary,omitempty"`
	FinalAnswer   string                              `json:"final_answer,omitempty"`
	Deliverable   DeepResearchDeliverableDecision     `json:"deliverable,omitempty"`
	Findings      []DeepResearchFinding               `json:"findings,omitempty"`
	Sources       []DeepAgentSourceRef                `json:"sources,omitempty"`
	Artifacts     []DeepAgentArtifactRef              `json:"artifacts,omitempty"`
	Conflicts     []deepAgentParallelConflict         `json:"conflicts,omitempty"`
	WorkerResults map[string]DeepResearchWorkerResult `json:"worker_results,omitempty"`
	Partial       bool                                `json:"partial,omitempty"`
	Errors        []string                            `json:"errors,omitempty"`
}

type DeepResearchDeliverableDecision struct {
	Action           string `json:"action,omitempty"`
	RequiresArtifact bool   `json:"requires_artifact,omitempty"`
	DeliverableType  string `json:"deliverable_type,omitempty"`
	FilenameHint     string `json:"filename_hint,omitempty"`
	ContentType      string `json:"content_type,omitempty"`
	Reason           string `json:"reason,omitempty"`
	Confidence       string `json:"confidence,omitempty"`
}
