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
	DeepAgentRunStatusFailed         = "failed"
	DeepAgentRunStatusBlocked        = "blocked"
	DeepAgentRunStatusBudgetExceeded = "budget_exceeded"

	// Tool modes
	DeepAgentToolModeModel         = "model"
	DeepAgentToolModeModelArtifact = "model_artifact"
	DeepAgentToolModeSkill         = "skill"
	DeepAgentToolModeRAGSearch     = "rag_search"
	DeepAgentToolModeTest          = "test"
	DeepAgentToolModeWeb           = "web"
	DeepAgentToolModeCodePatch     = "code_patch"
	DeepAgentToolModeMulti         = "multi"
	DeepAgentToolModeConnector     = "connector"

	DeepAgentErrorTransient     = "transient"
	DeepAgentErrorDeterministic = "deterministic"
	DeepAgentErrorConfig        = "config"
	DeepAgentErrorPermission    = "permission"
	DeepAgentErrorProvider      = "provider"
	DeepAgentErrorValidation    = "validation"

	// Defaults
	DeepAgentDefaultRAGSearchLimit  = 5
	DeepAgentDefaultChildJobPollMS  = 100
	DeepAgentDefaultMaxPlanSteps    = 8
	DeepAgentDefaultMaxActions      = 16
	DeepAgentDefaultMaxDurationMin  = 2
	DeepAgentDefaultNoProgressLimit = 3

	DeepAgentRunStatusReviewPending = "review_pending"
)

type DeepAgentPolicy struct {
	MaxSteps        int           `json:"max_steps"`
	MaxActions      int           `json:"max_actions"`
	MaxDuration     time.Duration `json:"max_duration"`
	StepTimeout     time.Duration `json:"step_timeout"`
	NoProgressLimit int           `json:"no_progress_limit"`
}

type DeepAgentRubric struct {
	AcceptanceCriteria []string `json:"acceptance_criteria,omitempty"`
	RequiredEvidence   []string `json:"required_evidence,omitempty"`
	RequiredArtifacts  []string `json:"required_artifacts,omitempty"`
	ForbiddenActions   []string `json:"forbidden_actions,omitempty"`
	QualityBar         string   `json:"quality_bar,omitempty"`
}

type DeepAgentTaskRequest struct {
	UserID           string          `json:"user_id,omitempty"`
	SessionID        string          `json:"session_id,omitempty"`
	JobID            string          `json:"job_id,omitempty"`
	LoopGoalID       string          `json:"loop_goal_id,omitempty"`
	Goal             string          `json:"goal"`
	ConnectorContext []string        `json:"connector_context,omitempty"`
	Plan             DeepAgentPlan   `json:"plan,omitempty"`
	Policy           DeepAgentPolicy `json:"policy,omitempty"`
	Rubric           DeepAgentRubric `json:"rubric,omitempty"`
	LoopGoal         *LoopGoal       `json:"loop_goal,omitempty"`
	State            map[string]any  `json:"state,omitempty"`
}

type DeepAgentResumeRequest struct {
	RunID            string                  `json:"run_id"`
	Policy           DeepAgentPolicy         `json:"policy,omitempty"`
	StatePatch       map[string]any          `json:"state_patch,omitempty"`
	AdditionalBudget DeepAgentResumeBudget   `json:"additional_budget,omitempty"`
	ReviewDecision   DeepAgentReviewDecision `json:"review_decision,omitempty"`
}

type DeepAgentResumeBudget struct {
	MaxActions    int   `json:"max_actions,omitempty"`
	MaxDurationMS int64 `json:"max_duration_ms,omitempty"`
	MaxSteps      int   `json:"max_steps,omitempty"`
}

type DeepAgentReviewDecision struct {
	Action     string         `json:"action,omitempty"`
	StepID     string         `json:"step_id,omitempty"`
	ActionHash string         `json:"action_hash,omitempty"`
	ArgsPatch  map[string]any `json:"args_patch,omitempty"`
	Reason     string         `json:"reason,omitempty"`
}

type DeepAgentTaskResult struct {
	Run   *WorkflowRun       `json:"run,omitempty"`
	State *DeepAgentState    `json:"state,omitempty"`
	Error string             `json:"error,omitempty"`
	Steps []*WorkflowStepRun `json:"steps,omitempty"`
}

type DeepAgentLearningCandidate struct {
	ID                       string         `json:"id"`
	Type                     string         `json:"type"`
	Content                  string         `json:"content"`
	Status                   string         `json:"status"`
	Source                   string         `json:"source,omitempty"`
	UserID                   string         `json:"user_id,omitempty"`
	SessionID                string         `json:"session_id,omitempty"`
	RunID                    string         `json:"run_id,omitempty"`
	StepID                   string         `json:"step_id,omitempty"`
	EvidenceID               string         `json:"evidence_id,omitempty"`
	MemoryItemID             string         `json:"memory_item_id,omitempty"`
	RiskLevel                string         `json:"risk_level,omitempty"`
	Sensitivity              string         `json:"sensitivity,omitempty"`
	Visibility               string         `json:"visibility,omitempty"`
	RequiresUserConfirmation bool           `json:"requires_user_confirmation,omitempty"`
	PolicyReason             string         `json:"policy_reason,omitempty"`
	UserConfirmed            bool           `json:"user_confirmed,omitempty"`
	ReviewedBy               string         `json:"reviewed_by,omitempty"`
	ReviewedAt               *time.Time     `json:"reviewed_at,omitempty"`
	ExpiresAt                *time.Time     `json:"expires_at,omitempty"`
	Metadata                 map[string]any `json:"metadata,omitempty"`
	CreatedAt                time.Time      `json:"created_at"`
}

type DeepAgentLoadedContext struct {
	UserID            string                   `json:"user_id,omitempty"`
	SessionID         string                   `json:"session_id,omitempty"`
	JobID             string                   `json:"job_id,omitempty"`
	RecentMessages    []DeepAgentMessageRef    `json:"recent_messages,omitempty"`
	Attachments       []DeepAgentAttachmentRef `json:"attachments,omitempty"`
	ExistingArtifacts []DeepAgentArtifactRef   `json:"existing_artifacts,omitempty"`
	EvidencePack      DeepAgentEvidencePack    `json:"evidence_pack,omitempty"`
	SkillCatalog      []DeepAgentSkillRef      `json:"skill_catalog,omitempty"`
	ToolCatalog       []DeepAgentToolRef       `json:"tool_catalog,omitempty"`
	MemorySummary     string                   `json:"memory_summary,omitempty"`
	Issues            []string                 `json:"issues,omitempty"`
}

type DeepAgentEvidencePack struct {
	RunID             string                      `json:"run_id,omitempty"`
	UserID            string                      `json:"user_id,omitempty"`
	SessionID         string                      `json:"session_id,omitempty"`
	TokenBudget       int                         `json:"token_budget,omitempty"`
	TokenEstimate     int                         `json:"token_estimate,omitempty"`
	RecentMessages    []DeepAgentEvidencePackItem `json:"recent_messages,omitempty"`
	Attachments       []DeepAgentEvidencePackItem `json:"attachments,omitempty"`
	ExistingArtifacts []DeepAgentEvidencePackItem `json:"existing_artifacts,omitempty"`
	CurrentArtifacts  []DeepAgentEvidencePackItem `json:"current_artifacts,omitempty"`
	WorkingContext    []DeepAgentEvidencePackItem `json:"working_context,omitempty"`
	Memory            []DeepAgentEvidencePackItem `json:"memory,omitempty"`
	SearchCandidates  []DeepAgentEvidencePackItem `json:"search_candidates,omitempty"`
	SkillCatalog      []DeepAgentEvidencePackItem `json:"skill_catalog,omitempty"`
	ToolCatalog       []DeepAgentEvidencePackItem `json:"tool_catalog,omitempty"`
	Issues            []string                    `json:"issues,omitempty"`
}

type DeepAgentEvidencePackItem struct {
	ID            string         `json:"id,omitempty"`
	Kind          string         `json:"kind,omitempty"`
	Title         string         `json:"title,omitempty"`
	Summary       string         `json:"summary,omitempty"`
	Source        string         `json:"source,omitempty"`
	Visibility    string         `json:"visibility,omitempty"`
	PhaseScope    []string       `json:"phase_scope,omitempty"`
	TokenEstimate int            `json:"token_estimate,omitempty"`
	CurrentRun    bool           `json:"current_run,omitempty"`
	Metadata      map[string]any `json:"metadata,omitempty"`
}

type DeepAgentStepRoute struct {
	StepID           string         `json:"step_id,omitempty"`
	Version          string         `json:"version,omitempty"`
	Mode             string         `json:"mode,omitempty"`
	Executor         string         `json:"executor,omitempty"`
	SkillName        string         `json:"skill_name,omitempty"`
	RequiresArtifact bool           `json:"requires_artifact,omitempty"`
	DeliverableType  string         `json:"deliverable_type,omitempty"`
	FilenameHint     string         `json:"filename_hint,omitempty"`
	AllowedTools     []string       `json:"allowed_tools,omitempty"`
	SearchScope      string         `json:"search_scope,omitempty"`
	SuccessCriteria  []string       `json:"success_criteria,omitempty"`
	Reason           string         `json:"reason,omitempty"`
	Confidence       string         `json:"confidence,omitempty"`
	ShadowRoute      map[string]any `json:"shadow_route,omitempty"`
	ShadowDiff       []string       `json:"shadow_diff,omitempty"`
}

type DeepAgentStepEvidence struct {
	StepID          string                 `json:"step_id,omitempty"`
	ActionID        string                 `json:"action_id,omitempty"`
	Route           DeepAgentStepRoute     `json:"route,omitempty"`
	Output          string                 `json:"output,omitempty"`
	Summary         string                 `json:"summary,omitempty"`
	Sources         []DeepAgentSourceRef   `json:"sources,omitempty"`
	Artifacts       []DeepAgentArtifactRef `json:"artifacts,omitempty"`
	ToolCalls       []DeepAgentToolCallRef `json:"tool_calls,omitempty"`
	ChildJobs       []DeepAgentChildJobRef `json:"child_jobs,omitempty"`
	Diagnostics     map[string]any         `json:"diagnostics,omitempty"`
	ErrorClass      string                 `json:"error_class,omitempty"`
	SideEffectLevel string                 `json:"side_effect_level,omitempty"`
	RollbackHint    string                 `json:"rollback_hint,omitempty"`
	VerifiedBy      []string               `json:"verified_by,omitempty"`
}

type DeepAgentMessageRef struct {
	ID        string    `json:"id,omitempty"`
	Role      string    `json:"role,omitempty"`
	Content   string    `json:"content,omitempty"`
	Snippet   string    `json:"snippet,omitempty"`
	CreatedAt time.Time `json:"created_at,omitempty"`
}

type DeepAgentAttachmentRef struct {
	ID          string `json:"id,omitempty"`
	URL         string `json:"url,omitempty"`
	Filename    string `json:"filename,omitempty"`
	ContentType string `json:"content_type,omitempty"`
	SizeBytes   int64  `json:"size_bytes,omitempty"`
	Source      string `json:"source,omitempty"`
}

type DeepAgentArtifactRef struct {
	ID          string    `json:"id,omitempty"`
	JobID       string    `json:"job_id,omitempty"`
	RunID       string    `json:"run_id,omitempty"`
	StepID      string    `json:"step_id,omitempty"`
	Filename    string    `json:"filename,omitempty"`
	ContentType string    `json:"content_type,omitempty"`
	SizeBytes   int64     `json:"size_bytes,omitempty"`
	Source      string    `json:"source,omitempty"`
	CreatedAt   time.Time `json:"created_at,omitempty"`
}

type DeepAgentSkillRef struct {
	Name              string `json:"name,omitempty"`
	Description       string `json:"description,omitempty"`
	WhenToUse         string `json:"when_to_use,omitempty"`
	ArgumentHint      string `json:"argument_hint,omitempty"`
	RunAsJob          bool   `json:"run_as_job,omitempty"`
	ProducesArtifacts bool   `json:"produces_artifacts,omitempty"`
}

type DeepAgentToolRef struct {
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	Permission  string `json:"permission,omitempty"`
}

type DeepAgentSourceRef struct {
	ID           string  `json:"id,omitempty"`
	URL          string  `json:"url,omitempty"`
	Title        string  `json:"title,omitempty"`
	Snippet      string  `json:"snippet,omitempty"`
	Provider     string  `json:"provider,omitempty"`
	Quality      string  `json:"quality,omitempty"`
	QualityScore float64 `json:"quality_score,omitempty"`
	SourceKind   string  `json:"source_kind,omitempty"`
}

type DeepAgentToolCallRef struct {
	ID     string `json:"id,omitempty"`
	Name   string `json:"name,omitempty"`
	Status string `json:"status,omitempty"`
}

type DeepAgentChildJobRef struct {
	ID     string `json:"id,omitempty"`
	Type   string `json:"type,omitempty"`
	Status string `json:"status,omitempty"`
}

type DeepAgentPlan struct {
	Goal  string          `json:"goal"`
	Steps []DeepAgentStep `json:"steps"`
}

type DeepAgentStep struct {
	ID            string         `json:"id"`
	Title         string         `json:"title"`
	Intent        string         `json:"intent,omitempty"`
	DependsOn     []string       `json:"depends_on,omitempty"`
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
	Done            bool                            `json:"done"`
	Reason          string                          `json:"reason,omitempty"`
	Checks          []DeepAgentVerificationCheck    `json:"checks,omitempty"`
	Missing         []string                        `json:"missing,omitempty"`
	Confidence      string                          `json:"confidence,omitempty"`
	ResearchQuality *DeepAgentResearchQualityReport `json:"research_quality,omitempty"`
}

type DeepAgentVerificationCheck struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Reason string `json:"reason,omitempty"`
}

type DeepAgentResearchQualityReport struct {
	Required              bool                            `json:"required"`
	SourceCount           int                             `json:"source_count"`
	CitationCount         int                             `json:"citation_count"`
	SourceQuality         map[string]int                  `json:"source_quality,omitempty"`
	AverageSourceQuality  float64                         `json:"average_source_quality,omitempty"`
	CitationVerification  map[string]any                  `json:"citation_verification,omitempty"`
	Coverage              DeepAgentResearchCoverageReport `json:"coverage,omitempty"`
	EntityDisambiguation  map[string]any                  `json:"entity_disambiguation,omitempty"`
	UnresolvedGaps        []string                        `json:"unresolved_gaps,omitempty"`
	Confidence            string                          `json:"confidence,omitempty"`
	TraceableSourceTitles []string                        `json:"traceable_source_titles,omitempty"`
}

type DeepAgentResearchCoverageReport struct {
	Covered []string `json:"covered,omitempty"`
	Missing []string `json:"missing,omitempty"`
}

type DeepAgentState struct {
	Goal             string                       `json:"goal"`
	Rubric           DeepAgentRubric              `json:"rubric,omitempty"`
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

type DeepAgentStepRouter interface {
	RouteStep(ctx context.Context, state *DeepAgentState, step DeepAgentStep) (DeepAgentStepRoute, error)
}

type DeepAgentExecutor interface {
	ExecuteDeepAgentAction(ctx context.Context, action DeepAgentAction, state *DeepAgentState) (DeepAgentActionResult, error)
}

type DeepAgentStepExecutor interface {
	ExecuteStep(ctx context.Context, route DeepAgentStepRoute, action DeepAgentAction, state *DeepAgentState) (DeepAgentStepEvidence, error)
}

type DeepAgentVerifier interface {
	CheckProgress(ctx context.Context, state *DeepAgentState, step DeepAgentStep, action DeepAgentAction, result DeepAgentActionResult) (DeepAgentProgress, error)
	CheckFinal(ctx context.Context, state *DeepAgentState) (DeepAgentFinalVerification, error)
}

type DeepAgentContextLoader interface {
	LoadDeepAgentContext(ctx context.Context, req DeepAgentTaskRequest, state *DeepAgentState) (DeepAgentLoadedContext, error)
}

type DeepAgentRiskGate interface {
	ReviewDeepAgentAction(ctx context.Context, run *WorkflowRun, state *DeepAgentState, step DeepAgentStep, action DeepAgentAction) error
}

type DeepAgentLearningSink interface {
	PersistDeepAgentLearnings(ctx context.Context, run *WorkflowRun, state *DeepAgentState, candidates []DeepAgentLearningCandidate) error
}
