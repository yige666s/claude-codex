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

	LoopContractVersion = "loop-contract/v1"
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

type LoopContract struct {
	ID              string                      `json:"id,omitempty"`
	Version         string                      `json:"version,omitempty"`
	Objective       string                      `json:"objective,omitempty"`
	TaskType        string                      `json:"task_type,omitempty"`
	Deliverable     LoopContractDeliverable     `json:"deliverable,omitempty"`
	Rubric          DeepAgentRubric             `json:"rubric,omitempty"`
	Budget          LoopContractBudget          `json:"budget,omitempty"`
	ToolPolicy      LoopContractToolPolicy      `json:"tool_policy,omitempty"`
	SourcePolicy    LoopContractSourcePolicy    `json:"source_policy,omitempty"`
	RiskPolicy      LoopContractRiskPolicy      `json:"risk_policy,omitempty"`
	StopPolicy      LoopContractStopPolicy      `json:"stop_policy,omitempty"`
	EvaluatorPolicy LoopContractEvaluatorPolicy `json:"evaluator_policy,omitempty"`
	CreatedFrom     string                      `json:"created_from,omitempty"`
	CreatedAt       time.Time                   `json:"created_at,omitempty"`
}

type LoopContractDeliverable struct {
	Type         string `json:"type,omitempty"`
	Format       string `json:"format,omitempty"`
	FilenameHint string `json:"filename_hint,omitempty"`
}

type LoopContractBudget struct {
	MaxSteps        int   `json:"max_steps,omitempty"`
	MaxActions      int   `json:"max_actions,omitempty"`
	MaxDurationMS   int64 `json:"max_duration_ms,omitempty"`
	StepTimeoutMS   int64 `json:"step_timeout_ms,omitempty"`
	NoProgressLimit int   `json:"no_progress_limit,omitempty"`
}

type LoopContractToolPolicy struct {
	AllowedModes     []string `json:"allowed_modes,omitempty"`
	ConnectorContext []string `json:"connector_context,omitempty"`
	WriteMode        string   `json:"write_mode,omitempty"`
}

type LoopContractSourcePolicy struct {
	RequiresSources      bool     `json:"requires_sources,omitempty"`
	MinSourceCount       int      `json:"min_source_count,omitempty"`
	PreferredSources     []string `json:"preferred_sources,omitempty"`
	PreferredDomains     []string `json:"preferred_domains,omitempty"`
	BlockedDomains       []string `json:"blocked_domains,omitempty"`
	MaxSourcesPerBranch  int      `json:"max_sources_per_branch,omitempty"`
	MaxDuplicateDomains  int      `json:"max_duplicate_domains,omitempty"`
	RequirePrimarySource bool     `json:"require_primary_source,omitempty"`
	RecencyRequirement   string   `json:"recency_requirement,omitempty"`
	MinSourceScore       float64  `json:"min_source_score,omitempty"`
	QualityBar           string   `json:"quality_bar,omitempty"`
}

type LoopContractRiskPolicy struct {
	RequiresReview   bool     `json:"requires_review,omitempty"`
	ForbiddenActions []string `json:"forbidden_actions,omitempty"`
	ReviewPolicy     string   `json:"review_policy,omitempty"`
}

type LoopContractStopPolicy struct {
	DoneWhen         []string `json:"done_when,omitempty"`
	MaxNoProgress    int      `json:"max_no_progress,omitempty"`
	OnBudgetExceeded string   `json:"on_budget_exceeded,omitempty"`
}

type LoopContractEvaluatorPolicy struct {
	RequiresFinalVerification bool     `json:"requires_final_verification,omitempty"`
	Verifier                  string   `json:"verifier,omitempty"`
	TimeoutMS                 int64    `json:"timeout_ms,omitempty"`
	ConflictTimeoutMS         int64    `json:"conflict_reconciliation_timeout_ms,omitempty"`
	EvidenceRequired          []string `json:"evidence_required,omitempty"`
	ArtifactRequired          []string `json:"artifact_required,omitempty"`
}

type GateDecision struct {
	Gate           string   `json:"gate,omitempty"`
	Allow          bool     `json:"allow"`
	BlockReason    string   `json:"block_reason,omitempty"`
	RequiresReview bool     `json:"requires_review,omitempty"`
	RepairHint     string   `json:"repair_hint,omitempty"`
	EvidenceRefs   []string `json:"evidence_refs,omitempty"`
	Category       string   `json:"category,omitempty"`
}

type DeepAgentTaskRequest struct {
	UserID           string          `json:"user_id,omitempty"`
	SessionID        string          `json:"session_id,omitempty"`
	JobID            string          `json:"job_id,omitempty"`
	Goal             string          `json:"goal"`
	ConnectorContext []string        `json:"connector_context,omitempty"`
	Plan             DeepAgentPlan   `json:"plan,omitempty"`
	Policy           DeepAgentPolicy `json:"policy,omitempty"`
	Rubric           DeepAgentRubric `json:"rubric,omitempty"`
	State            map[string]any  `json:"state,omitempty"`
	LoopContract     LoopContract    `json:"loop_contract,omitempty"`
}

type DeepAgentResumeRequest struct {
	RunID            string                  `json:"run_id"`
	Policy           DeepAgentPolicy         `json:"policy,omitempty"`
	StatePatch       map[string]any          `json:"state_patch,omitempty"`
	HandoffPatch     LoopHandoff             `json:"handoff_patch,omitempty"`
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

type LoopHandoff struct {
	Type              string           `json:"type,omitempty"`
	Summary           string           `json:"summary,omitempty"`
	ResumePoint       string           `json:"resume_point,omitempty"`
	ResumeAvailable   bool             `json:"resume_available"`
	Workspace         WorkspaceHandoff `json:"workspace,omitempty"`
	Artifact          ArtifactHandoff  `json:"artifact,omitempty"`
	Connector         ConnectorHandoff `json:"connector,omitempty"`
	ReviewState       string           `json:"review_state,omitempty"`
	BlockingReason    string           `json:"blocking_reason,omitempty"`
	RecommendedAction string           `json:"recommended_action,omitempty"`
	Metadata          map[string]any   `json:"metadata,omitempty"`
	UpdatedAt         time.Time        `json:"updated_at,omitempty"`
}

type WorkspaceHandoff struct {
	Repo         string   `json:"repo,omitempty"`
	Branch       string   `json:"branch,omitempty"`
	Worktree     string   `json:"worktree,omitempty"`
	BaseCommit   string   `json:"base_commit,omitempty"`
	ChangedFiles []string `json:"changed_files,omitempty"`
	TestCommands []string `json:"test_commands,omitempty"`
	RollbackPlan string   `json:"rollback_plan,omitempty"`
}

type ArtifactHandoff struct {
	SourceArtifacts []DeepAgentArtifactRef `json:"source_artifacts,omitempty"`
	DraftArtifact   *DeepAgentArtifactRef  `json:"draft_artifact,omitempty"`
	FinalArtifact   *DeepAgentArtifactRef  `json:"final_artifact,omitempty"`
	ReviewState     string                 `json:"review_state,omitempty"`
}

type ConnectorHandoff struct {
	Provider            string            `json:"provider,omitempty"`
	Scopes              []string          `json:"scopes,omitempty"`
	RiskLevel           string            `json:"risk_level,omitempty"`
	PendingWriteActions []DeepAgentAction `json:"pending_write_actions,omitempty"`
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
	EvidenceRefs             []string       `json:"evidence_refs,omitempty"`
	SourceJob                string         `json:"source_job,omitempty"`
	Owner                    string         `json:"owner,omitempty"`
	MemoryItemID             string         `json:"memory_item_id,omitempty"`
	RiskLevel                string         `json:"risk_level,omitempty"`
	Sensitivity              string         `json:"sensitivity,omitempty"`
	Visibility               string         `json:"visibility,omitempty"`
	Confidence               float64        `json:"confidence,omitempty"`
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
	ID           string   `json:"id,omitempty"`
	URL          string   `json:"url,omitempty"`
	Title        string   `json:"title,omitempty"`
	Snippet      string   `json:"snippet,omitempty"`
	Provider     string   `json:"provider,omitempty"`
	Quality      string   `json:"quality,omitempty"`
	QualityScore float64  `json:"quality_score,omitempty"`
	SourceKind   string   `json:"source_kind,omitempty"`
	Domain       string   `json:"domain,omitempty"`
	ScoreReasons []string `json:"score_reasons,omitempty"`
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

type DeepAgentParallelBranchSpec struct {
	ID                string                        `json:"id"`
	Title             string                        `json:"title"`
	Task              string                        `json:"task"`
	Tool              string                        `json:"tool,omitempty"`
	AllowedTools      []string                      `json:"allowed_tools,omitempty"`
	SuccessCriteria   []string                      `json:"success_criteria,omitempty"`
	Kind              string                        `json:"kind,omitempty"`
	CoverageDimension string                        `json:"coverage_dimension,omitempty"`
	Budget            DeepAgentParallelBranchBudget `json:"budget,omitempty"`
	Metadata          map[string]any                `json:"metadata,omitempty"`
}

type DeepAgentParallelBranchResult struct {
	ID           string                      `json:"id"`
	Title        string                      `json:"title,omitempty"`
	Status       string                      `json:"status"`
	Output       string                      `json:"output,omitempty"`
	Error        string                      `json:"error,omitempty"`
	Sources      []DeepAgentSourceRef        `json:"sources,omitempty"`
	Artifacts    []DeepAgentArtifactRef      `json:"artifacts,omitempty"`
	ToolCalls    []DeepAgentToolCallRef      `json:"tool_calls,omitempty"`
	Contribution DeepAgentBranchContribution `json:"contribution,omitempty"`
	Metadata     map[string]any              `json:"metadata,omitempty"`
}

type DeepAgentParallelBranchBudget struct {
	TimeoutMS    int64 `json:"timeout_ms,omitempty"`
	MaxToolCalls int   `json:"max_tool_calls,omitempty"`
	MaxSources   int   `json:"max_sources,omitempty"`
	MaxTokens    int   `json:"max_tokens,omitempty"`
}

type DeepAgentBranchContribution struct {
	BranchID              string               `json:"branch_id,omitempty"`
	Title                 string               `json:"title,omitempty"`
	Kind                  string               `json:"kind,omitempty"`
	CoverageDimension     string               `json:"coverage_dimension,omitempty"`
	Status                string               `json:"status,omitempty"`
	Findings              []string             `json:"findings,omitempty"`
	Sources               []DeepAgentSourceRef `json:"sources,omitempty"`
	Confidence            string               `json:"confidence,omitempty"`
	Conflicts             []string             `json:"conflicts,omitempty"`
	MissingCoverage       []string             `json:"missing_coverage,omitempty"`
	RecommendedNextAction string               `json:"recommended_next_action,omitempty"`
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

const (
	DeepAgentEvaluatorPass        = "pass"
	DeepAgentEvaluatorFail        = "fail"
	DeepAgentEvaluatorNeedsRepair = "needs_repair"
	DeepAgentEvaluatorNeedsReview = "needs_review"
)

type DeepAgentEvaluatorInput struct {
	Contract     LoopContract            `json:"contract,omitempty"`
	Evidence     []DeepAgentStepEvidence `json:"evidence,omitempty"`
	Artifacts    []DeepAgentArtifactRef  `json:"artifacts,omitempty"`
	TraceSummary DeepAgentEvaluatorTrace `json:"trace_summary,omitempty"`
}

type DeepAgentEvaluatorTrace struct {
	Status         string                       `json:"status,omitempty"`
	CompletedSteps []string                     `json:"completed_steps,omitempty"`
	FailedSteps    []string                     `json:"failed_steps,omitempty"`
	ActionCount    int                          `json:"action_count,omitempty"`
	Blocker        string                       `json:"blocker,omitempty"`
	VerifierChecks []DeepAgentVerificationCheck `json:"verifier_checks,omitempty"`
}

type DeepAgentEvaluatorVerdict struct {
	Verdict        string                       `json:"verdict"`
	Passed         bool                         `json:"passed"`
	FailedCriteria []string                     `json:"failed_criteria,omitempty"`
	Confidence     string                       `json:"confidence,omitempty"`
	RepairPlan     []string                     `json:"repair_plan,omitempty"`
	Reason         string                       `json:"reason,omitempty"`
	Checks         []DeepAgentVerificationCheck `json:"checks,omitempty"`
	SourceCoverage map[string]any               `json:"source_coverage,omitempty"`
	RubricCoverage map[string]any               `json:"rubric_coverage,omitempty"`
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
	LoopContract     LoopContract                 `json:"loop_contract,omitempty"`
	Handoff          LoopHandoff                  `json:"handoff,omitempty"`
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
	GateDecisions    []GateDecision               `json:"gate_decisions,omitempty"`
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

type DeepAgentEvaluator interface {
	EvaluateProgress(ctx context.Context, input DeepAgentEvaluatorInput) (DeepAgentEvaluatorVerdict, error)
	EvaluateFinal(ctx context.Context, input DeepAgentEvaluatorInput) (DeepAgentEvaluatorVerdict, error)
	EvaluateSources(ctx context.Context, input DeepAgentEvaluatorInput) (DeepAgentEvaluatorVerdict, error)
	EvaluateArtifact(ctx context.Context, input DeepAgentEvaluatorInput) (DeepAgentEvaluatorVerdict, error)
	EvaluateConflicts(ctx context.Context, input DeepAgentEvaluatorInput) (DeepAgentEvaluatorVerdict, error)
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
