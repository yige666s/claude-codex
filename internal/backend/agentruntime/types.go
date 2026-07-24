package agentruntime

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"claude-codex/internal/harness/engine"
	"claude-codex/internal/harness/permissions"
	"claude-codex/internal/harness/skills"
	"claude-codex/internal/harness/state"
	publictypes "claude-codex/internal/public/types"
)

type SessionStore interface {
	Create(ctx context.Context, userID, workingDir string) (*state.Session, error)
	Get(ctx context.Context, userID, sessionID string) (*state.Session, error)
	List(ctx context.Context, userID string) ([]*state.Session, error)
	Save(ctx context.Context, userID string, session *state.Session) error
	Delete(ctx context.Context, userID, sessionID string) error
	DeleteUser(ctx context.Context, userID string) error
	PruneBefore(ctx context.Context, cutoff time.Time) (int, error)
}

type SessionMetadataStore interface {
	SaveSessionMetadata(ctx context.Context, userID string, session *state.Session) error
}

type MessageSearchResult struct {
	MessageID    string    `json:"message_id,omitempty"`
	SessionID    string    `json:"session_id"`
	MessageIndex int       `json:"message_index"`
	Role         string    `json:"role"`
	Content      string    `json:"content,omitempty"`
	Snippet      string    `json:"snippet"`
	SessionTitle string    `json:"session_title"`
	CreatedAt    time.Time `json:"created_at"`
	Score        float64   `json:"score,omitempty"`
	Source       string    `json:"source,omitempty"`
}

type MessageSearchStore interface {
	SearchMessages(ctx context.Context, userID, query string, limit, offset int) ([]MessageSearchResult, error)
}

type MemoryService interface {
	LoadContext(ctx context.Context, userID string, session *state.Session) (string, error)
	LoadUserMemory(ctx context.Context, userID string) (string, error)
	LoadSessionMemory(ctx context.Context, userID, sessionID string) (string, error)
	AfterTurn(ctx context.Context, userID string, session *state.Session) error
	DeleteSession(ctx context.Context, userID, sessionID string) error
	DeleteUser(ctx context.Context, userID string) error
	PruneBefore(ctx context.Context, cutoff time.Time) (int, error)
}

type MemoryRecallTraceStore interface {
	RecordMemoryRecallTrace(ctx context.Context, trace MemoryRecallTrace) error
	ListMemoryRecallTraces(ctx context.Context, userID, sessionID string, limit int) ([]MemoryRecallTrace, error)
}

type MemoryRecallTrace struct {
	ID                   string            `json:"id"`
	UserID               string            `json:"user_id,omitempty"`
	SessionID            string            `json:"session_id,omitempty"`
	TriggerReason        string            `json:"trigger_reason"`
	Query                string            `json:"query,omitempty"`
	QueryHash            string            `json:"query_hash,omitempty"`
	OriginalQuery        string            `json:"original_query,omitempty"`
	RewrittenQuery       string            `json:"rewritten_query,omitempty"`
	QueryRewriteUsed     bool              `json:"query_rewrite_used,omitempty"`
	QueryRewriteReason   string            `json:"query_rewrite_reason,omitempty"`
	QueryRewriteDegraded bool              `json:"query_rewrite_degraded,omitempty"`
	MemoryItemIDs        []string          `json:"memory_item_ids,omitempty"`
	EpisodeIDs           []string          `json:"episode_ids,omitempty"`
	SourceRefs           []MemorySourceRef `json:"source_refs,omitempty"`
	MemoryChars          int               `json:"memory_chars,omitempty"`
	EpisodeChars         int               `json:"episode_chars,omitempty"`
	Injected             bool              `json:"injected"`
	Degraded             bool              `json:"degraded"`
	DegradedReason       string            `json:"degraded_reason,omitempty"`
	LatencyMS            int64             `json:"latency_ms,omitempty"`
	Metadata             map[string]any    `json:"metadata,omitempty"`
	CreatedAt            time.Time         `json:"created_at"`
}

type SavedMemoryDeletionService interface {
	DeleteSavedMemory(ctx context.Context, userID string) error
}

type BrowserMemoryRequest struct {
	URL        string   `json:"url,omitempty"`
	Title      string   `json:"title,omitempty"`
	Content    string   `json:"content,omitempty"`
	SessionID  string   `json:"session_id,omitempty"`
	Visibility string   `json:"visibility,omitempty"`
	Tags       []string `json:"tags,omitempty"`
}

type MemoryItem struct {
	ID             string            `json:"id"`
	UserID         string            `json:"user_id,omitempty"`
	SessionID      string            `json:"session_id,omitempty"`
	Namespace      string            `json:"namespace,omitempty"`
	Kind           string            `json:"kind"`
	Level          string            `json:"level"`
	Category       string            `json:"category"`
	Tags           []string          `json:"tags,omitempty"`
	Source         string            `json:"source"`
	SourceRefs     []MemorySourceRef `json:"source_refs,omitempty"`
	Visibility     string            `json:"visibility"`
	Status         string            `json:"status"`
	Content        string            `json:"content"`
	RawHash        string            `json:"raw_hash,omitempty"`
	Confidence     float64           `json:"confidence"`
	Weight         float64           `json:"weight"`
	AccessCount    int64             `json:"access_count"`
	ParentID       string            `json:"parent_id,omitempty"`
	RelatedIDs     []string          `json:"related_ids,omitempty"`
	ConflictIDs    []string          `json:"conflict_ids,omitempty"`
	SupersedesID   string            `json:"supersedes_id,omitempty"`
	SupersededByID string            `json:"superseded_by_id,omitempty"`
	LastInjectedAt *time.Time        `json:"last_injected_at,omitempty"`
	Metadata       map[string]any    `json:"metadata,omitempty"`
	ExpiresAt      *time.Time        `json:"expires_at,omitempty"`
	CreatedAt      time.Time         `json:"created_at"`
	UpdatedAt      time.Time         `json:"updated_at"`
}

type MemoryItemFilter struct {
	SessionID  string
	Namespace  string
	Kind       string
	Level      string
	Category   string
	Visibility string
	Status     string
	Query      string
	SourceKind string
	SourceID   string
	Limit      int
}

type MemoryEpisode struct {
	ID             string            `json:"id"`
	UserID         string            `json:"user_id,omitempty"`
	SessionID      string            `json:"session_id,omitempty"`
	Title          string            `json:"title,omitempty"`
	Summary        string            `json:"summary"`
	L0Abstract     string            `json:"l0_abstract,omitempty"`
	KeyTopics      []string          `json:"key_topics,omitempty"`
	SourceType     string            `json:"source_type"`
	SourceID       string            `json:"source_id,omitempty"`
	SourceRefs     []MemorySourceRef `json:"source_refs,omitempty"`
	Status         string            `json:"status"`
	Visibility     string            `json:"visibility"`
	TurnCount      int64             `json:"turn_count"`
	TokenCount     int64             `json:"token_count"`
	Confidence     float64           `json:"confidence"`
	Weight         float64           `json:"weight"`
	RecallCount    int64             `json:"recall_count"`
	UseCount       int64             `json:"use_count"`
	RecallScore    float64           `json:"recall_score"`
	LastRecalledAt *time.Time        `json:"last_recalled_at,omitempty"`
	LastUsedAt     *time.Time        `json:"last_used_at,omitempty"`
	PromotedAt     *time.Time        `json:"promoted_at,omitempty"`
	Metadata       map[string]any    `json:"metadata,omitempty"`
	ExpiresAt      *time.Time        `json:"expires_at,omitempty"`
	CreatedAt      time.Time         `json:"created_at"`
	UpdatedAt      time.Time         `json:"updated_at"`
}

type MemoryEpisodeFilter struct {
	SessionID string
	Status    string
	Query     string
	Limit     int
	Offset    int
}

type MemoryEpisodeSearchOptions struct {
	Limit    int
	MinScore float64
}

type MemoryEpisodeSearchResult struct {
	Episode MemoryEpisode `json:"episode"`
	Score   float64       `json:"score"`
}

type MemorySettings struct {
	Enabled        bool      `json:"enabled"`
	CaptureEnabled bool      `json:"capture_enabled"`
	ContextEnabled bool      `json:"context_enabled"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type PersonalizationProfile struct {
	Nickname   string `json:"nickname,omitempty"`
	Occupation string `json:"occupation,omitempty"`
	About      string `json:"about,omitempty"`
}

type PersonalizationStyle struct {
	Preset string `json:"preset"`
	Tone   string `json:"tone"`
}

type PersonalizationTraits struct {
	Warmth           string `json:"warmth"`
	Enthusiasm       string `json:"enthusiasm"`
	HeadingsAndLists string `json:"headings_and_lists"`
	Emoji            string `json:"emoji"`
}

type PersonalizationFeatureFlags struct {
	QuickAnswers     bool `json:"quick_answers"`
	UseSavedMemory   bool `json:"use_saved_memory"`
	UseChatHistory   bool `json:"use_chat_history"`
	UseBrowserMemory bool `json:"use_browser_memory"`
}

type PersonalizationSettings struct {
	Profile            PersonalizationProfile      `json:"profile"`
	Style              PersonalizationStyle        `json:"style"`
	Traits             PersonalizationTraits       `json:"traits"`
	CustomInstructions string                      `json:"custom_instructions"`
	FeatureFlags       PersonalizationFeatureFlags `json:"feature_flags"`
	Version            int64                       `json:"version"`
	UpdatedAt          time.Time                   `json:"updated_at"`
}

type MemoryItemService interface {
	GetMemoryItem(ctx context.Context, userID, itemID string) (MemoryItem, error)
	ListMemoryItems(ctx context.Context, userID string, filter MemoryItemFilter) ([]MemoryItem, error)
	UpdateMemoryItem(ctx context.Context, userID string, item MemoryItem) (MemoryItem, error)
	DeleteMemoryItem(ctx context.Context, userID, itemID string) error
}

type MemoryEpisodeService interface {
	UpsertMemoryEpisode(ctx context.Context, userID string, episode MemoryEpisode) (MemoryEpisode, error)
	GetMemoryEpisode(ctx context.Context, userID, episodeID string) (MemoryEpisode, error)
	ListMemoryEpisodes(ctx context.Context, userID string, filter MemoryEpisodeFilter) ([]MemoryEpisode, error)
	UpdateMemoryEpisode(ctx context.Context, userID string, episode MemoryEpisode) (MemoryEpisode, error)
	DeleteMemoryEpisode(ctx context.Context, userID, episodeID string) error
	SearchMemoryEpisodes(ctx context.Context, userID, query string, opts MemoryEpisodeSearchOptions) ([]MemoryEpisodeSearchResult, error)
	RecordMemoryEpisodeRecall(ctx context.Context, userID, episodeID string, score float64) error
	RecordMemoryEpisodeUse(ctx context.Context, userID, episodeID string) error
	ListUnpromotedMemoryEpisodes(ctx context.Context, userID string, limit int) ([]MemoryEpisode, error)
	MarkMemoryEpisodesPromoted(ctx context.Context, userID string, episodeIDs []string) error
	DeleteMemoryEpisodesForSession(ctx context.Context, userID, sessionID string) error
}

type MemoryEpisodePromoter interface {
	PromoteEpisodes(ctx context.Context, userID string, episodes []MemoryEpisode) ([]MemoryItem, error)
}

type MemorySettingsService interface {
	GetMemorySettings(ctx context.Context, userID string) (MemorySettings, error)
	UpdateMemorySettings(ctx context.Context, userID string, settings MemorySettings) (MemorySettings, error)
}

type PersonalizationSettingsService interface {
	GetPersonalizationSettings(ctx context.Context, userID string) (PersonalizationSettings, error)
	UpdatePersonalizationSettings(ctx context.Context, userID string, settings PersonalizationSettings) (PersonalizationSettings, error)
	DeletePersonalizationSettings(ctx context.Context, userID string) error
}

type MemoryMaintenanceAction struct {
	ID         string    `json:"id"`
	UserID     string    `json:"user_id,omitempty"`
	Type       string    `json:"type"`
	MemoryIDs  []string  `json:"memory_ids"`
	Reason     string    `json:"reason"`
	Confidence float64   `json:"confidence"`
	Status     string    `json:"status"`
	CreatedAt  time.Time `json:"created_at"`
}

type MemoryMaintenanceRunReport struct {
	Actions []MemoryMaintenanceAction `json:"actions"`
	Applied []MemoryMaintenanceAction `json:"applied"`
	Planned []MemoryMaintenanceAction `json:"planned"`
}

type MemorySourceRef struct {
	Kind        string `json:"kind"`
	ID          string `json:"id"`
	Filename    string `json:"filename,omitempty"`
	ContentType string `json:"content_type,omitempty"`
	SessionID   string `json:"session_id,omitempty"`
	JobID       string `json:"job_id,omitempty"`
	URI         string `json:"uri,omitempty"`
}

const (
	MemoryNamespaceDefault         = "default"
	MemoryNamespacePersonalization = "personalization"
	MemoryNamespaceBrowser         = "browser"
	MemoryKindSession              = "session"
	MemoryLevelAtomic              = "atomic"
	MemoryLevelConcept             = "concept"
	MemoryLevelProfile             = "profile"
	MemoryCategoryFact             = "fact"
	MemoryCategoryPreference       = "preference"
	MemoryCategoryEvent            = "event"
	MemoryCategorySkill            = "skill"
	MemorySourceConversation       = "conversation"
	MemorySourceAttachment         = "attachment"
	MemorySourceArtifact           = "artifact"
	MemorySourceVision             = "vision"
	MemorySourceBrowser            = "browser"
	MemorySourceUserEdit           = "user_edit"
	MemorySourceSystem             = "system"
	MemoryVisibilityUser           = "user"
	MemoryVisibilityPrivate        = "private"
	MemoryVisibilitySession        = "session_only"
	MemoryVisibilityShared         = "shared"
	MemoryStatusActive             = "active"
	MemoryStatusDormant            = "dormant"
	MemoryStatusArchived           = "archived"
	MemoryStatusDeleted            = "deleted"
	MemoryStatusConflicted         = "conflicted"
	MemoryStatusPendingConfirm     = "pending_confirm"
	MemoryEpisodeStatusActive      = "active"
	MemoryEpisodeStatusArchived    = "archived"
	MemoryEpisodeStatusDeleted     = "deleted"
	MemoryEpisodeStatusPending     = "pending"
	MemoryEpisodeSourceSession     = "session"
	MemoryEpisodeSourceJob         = "job"
	MemoryEpisodeSourceManual      = "manual"

	MemoryMaintenancePending   = "pending"
	MemoryMaintenanceApplied   = "applied"
	MemoryMaintenanceDismissed = "dismissed"
)

type Runner interface {
	Run(ctx context.Context, session *state.Session, prompt string) (engine.Result, error)
	RunGeneratedPrompt(ctx context.Context, session *state.Session, prompt string) (engine.Result, error)
}

type ContentRunner interface {
	Runner
	RunContent(ctx context.Context, session *state.Session, prompt []publictypes.ContentBlock) (engine.Result, error)
}

type StreamingContentRunner interface {
	ContentRunner
	RunContentStream(ctx context.Context, session *state.Session, prompt []publictypes.ContentBlock, onToken func(string)) (engine.Result, error)
}

type StreamingRunner interface {
	Runner
	RunStream(ctx context.Context, session *state.Session, prompt string, onToken func(string)) (engine.Result, error)
	RunGeneratedPromptStream(ctx context.Context, session *state.Session, prompt string, onToken func(string)) (engine.Result, error)
}

type EngineFactory func(scope Scope) Runner

// ContextEngineFactory constructs a runner for one execution scope. Request-bound
// callers should use this form so connector discovery and other setup work can be
// cancelled without terminating the process on a per-turn configuration error.
type ContextEngineFactory func(ctx context.Context, scope Scope) (Runner, error)

// AdaptContextEngineFactory keeps background services that still accept the
// legacy factory shape failure-safe. New request paths should retain and pass a
// ContextEngineFactory directly.
func AdaptContextEngineFactory(factory ContextEngineFactory) EngineFactory {
	if factory == nil {
		return nil
	}
	return func(scope Scope) Runner {
		runner, err := factory(context.Background(), scope)
		if err != nil {
			return factoryErrorRunner{err: err}
		}
		if runner == nil {
			return factoryErrorRunner{err: errors.New("engine factory returned nil runner")}
		}
		return runner
	}
}

type factoryErrorRunner struct {
	err error
}

func (r factoryErrorRunner) result(session *state.Session) (engine.Result, error) {
	err := r.err
	if err == nil {
		err = errors.New("engine factory failed")
	}
	return engine.Result{Session: session}, err
}

func (r factoryErrorRunner) Run(_ context.Context, session *state.Session, _ string) (engine.Result, error) {
	return r.result(session)
}

func (r factoryErrorRunner) RunGeneratedPrompt(_ context.Context, session *state.Session, _ string) (engine.Result, error) {
	return r.result(session)
}

func (r factoryErrorRunner) RunStream(_ context.Context, session *state.Session, _ string, _ func(string)) (engine.Result, error) {
	return r.result(session)
}

func (r factoryErrorRunner) RunGeneratedPromptStream(_ context.Context, session *state.Session, _ string, _ func(string)) (engine.Result, error) {
	return r.result(session)
}

func (r factoryErrorRunner) RunContent(_ context.Context, session *state.Session, _ []publictypes.ContentBlock) (engine.Result, error) {
	return r.result(session)
}

func (r factoryErrorRunner) RunContentStream(_ context.Context, session *state.Session, _ []publictypes.ContentBlock, _ func(string)) (engine.Result, error) {
	return r.result(session)
}

type Scope struct {
	UserID     string
	SessionID  string
	WorkingDir string
	Prompt     string
	// InternalToolScope marks a server-created child execution whose explicit
	// AllowedTools list may include filesystem readers and sandboxed Bash. User
	// chat requests must never set this flag directly.
	InternalToolScope bool
	SkillName         string
	SkillRoot         string
	SkillScoped       bool
	SkillShell        skills.FrontmatterShell
	SkillShellEnv     map[string]string
	SkillShellSandbox SkillShellSandboxConfig
	AllowedTools      []string
	ConnectorContext  []string
	AllowedEnv        []string
	NetworkAllowlist  []string
	ArtifactTypes     []string
	Artifacts         ArtifactWriter
	ArtifactMaxBytes  int64
}

type ArtifactWriter interface {
	Write(ctx context.Context, filename, contentType string, data []byte) (*Artifact, error)
}

type Authenticator interface {
	Authenticate(*http.Request) (User, error)
}

type User struct {
	ID string `json:"id"`
}

type EventSink interface {
	Send(ctx context.Context, event Event) error
}

type Event struct {
	Type       string          `json:"type"`
	ID         string          `json:"id,omitempty"`
	SessionID  string          `json:"session_id,omitempty"`
	RunID      string          `json:"run_id,omitempty"`
	JobID      string          `json:"job_id,omitempty"`
	Job        *Job            `json:"job,omitempty"`
	JobReason  string          `json:"job_reason,omitempty"`
	Role       string          `json:"role,omitempty"`
	Content    string          `json:"content,omitempty"`
	Text       string          `json:"text,omitempty"`
	Tool       string          `json:"tool,omitempty"`
	Input      any             `json:"input,omitempty"`
	Summary    string          `json:"summary,omitempty"`
	Sources    []any           `json:"sources,omitempty"`
	SourceID   string          `json:"source_id,omitempty"`
	AnswerSpan string          `json:"answer_span,omitempty"`
	Message    string          `json:"message,omitempty"`
	Error      string          `json:"error,omitempty"`
	Data       json.RawMessage `json:"data,omitempty"`
}

type LiveRequest struct {
	UserID       string
	SessionID    string
	ResumeHandle string
}

type LiveClientEvent struct {
	Type     string `json:"type"`
	MIMEType string `json:"mime_type,omitempty"`
	Data     string `json:"data,omitempty"`
	Content  string `json:"content,omitempty"`
}

type LiveClientStream interface {
	ReceiveLiveClientEvent(context.Context) (LiveClientEvent, error)
}

type ChatRequest struct {
	UserID                   string
	SessionID                string
	RunID                    string
	IdempotencyKey           string
	ClientUserMessageID      string
	ClientAssistantMessageID string
	Content                  string
	AttachmentIDs            []string
	AttachmentURLs           []ChatAttachmentURL
	ThinkingMode             bool
	AgentMode                string
	ConnectorContext         []string
	// TurnReserved is set by transports that already acquired the durable
	// session-turn reservation before entering Runtime.Chat.
	TurnReserved bool
}

type ChatAttachmentURL struct {
	URL         string `json:"url"`
	ContentType string `json:"content_type,omitempty"`
	Filename    string `json:"filename,omitempty"`
}

type JobRoutingDecision struct {
	RunAsJob bool   `json:"run_as_job"`
	JobType  string `json:"job_type,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

const (
	AgentModeChat        = "chat"
	AgentModePlanExecute = "plan_execute"
	AgentModeWebSearch   = "web_search"
)

const (
	JobTypeChat      = "chat"
	JobTypeSkill     = "skill"
	JobTypeDeepAgent = "deep_agent"
)

const (
	JobStatusQueued    = "queued"
	JobStatusRunning   = "running"
	JobStatusSucceeded = "succeeded"
	JobStatusFailed    = "failed"
	JobStatusCancelled = "cancelled"
)

type Job struct {
	ID               string              `json:"id"`
	UserID           string              `json:"user_id,omitempty"`
	SessionID        string              `json:"session_id"`
	LoopGoalID       string              `json:"loop_goal_id,omitempty"`
	Type             string              `json:"type"`
	Status           string              `json:"status"`
	Content          string              `json:"content,omitempty"`
	AttachmentIDs    []string            `json:"attachment_ids,omitempty"`
	AttachmentURLs   []ChatAttachmentURL `json:"attachment_urls,omitempty"`
	ConnectorContext []string            `json:"connector_context,omitempty"`
	Error            string              `json:"error,omitempty"`
	CreatedAt        time.Time           `json:"created_at"`
	UpdatedAt        time.Time           `json:"updated_at"`
	StartedAt        *time.Time          `json:"started_at,omitempty"`
	FinishedAt       *time.Time          `json:"finished_at,omitempty"`
	// Execution ownership is internal fencing state for distributed workers.
	ExecutionOwner          string     `json:"-"`
	ExecutionEpoch          int64      `json:"-"`
	ExecutionLeaseExpiresAt *time.Time `json:"-"`
}

type JobEvent struct {
	ID        string    `json:"id"`
	JobID     string    `json:"job_id"`
	UserID    string    `json:"user_id,omitempty"`
	SessionID string    `json:"session_id,omitempty"`
	Type      string    `json:"type"`
	Event     Event     `json:"event"`
	CreatedAt time.Time `json:"created_at"`
}

type JobStore interface {
	Init(ctx context.Context) error
	CreateJob(ctx context.Context, job *Job) error
	GetJob(ctx context.Context, userID, jobID string) (*Job, error)
	ListJobs(ctx context.Context, userID, sessionID string) ([]*Job, error)
	UpdateJobStatus(ctx context.Context, userID, jobID, status, errorText string, at time.Time) error
	AddJobEvent(ctx context.Context, event *JobEvent) error
	ListJobEvents(ctx context.Context, userID, jobID, afterID string, limit int) ([]*JobEvent, error)
	DeleteSession(ctx context.Context, userID, sessionID string) error
	DeleteUser(ctx context.Context, userID string) error
	PruneBefore(ctx context.Context, cutoff time.Time) (int, error)
}

// JobStatusTransitionStore provides compare-and-set status updates for job
// workers and API replicas that may race on cancellation or completion.
type JobStatusTransitionStore interface {
	TransitionJobStatus(ctx context.Context, userID, jobID, expectedStatus, status, errorText string, at time.Time) (bool, error)
}

// JobExecutionLeaseStore fences distributed job execution. Workers must hold
// an unexpired owner lease both to execute and to publish a terminal status.
type JobExecutionLeaseStore interface {
	AcquireJobExecutionLease(ctx context.Context, userID, jobID, owner string, now, expiresAt time.Time) (bool, error)
	RefreshJobExecutionLease(ctx context.Context, userID, jobID, owner string, now, expiresAt time.Time) (bool, error)
	ReleaseJobExecutionLease(ctx context.Context, userID, jobID, owner string, at time.Time) error
	TransitionOwnedJobStatus(ctx context.Context, userID, jobID, owner, status, errorText string, at time.Time) (bool, error)
}

type JobTerminalEventStore interface {
	TransitionJobStatusWithEvent(ctx context.Context, userID, jobID, expectedStatus, status, errorText string, at time.Time, event *JobEvent) (bool, error)
	TransitionOwnedJobStatusWithEvent(ctx context.Context, userID, jobID, owner, status, errorText string, at time.Time, event *JobEvent) (bool, error)
}

type JobEventStreamStore interface {
	AppendJobEvent(ctx context.Context, event *JobEvent) error
	BlockReadJobEvents(ctx context.Context, userID, jobID, afterID string, limit int, block time.Duration) ([]*JobEvent, error)
}

const (
	LoopDiscoveryManual         = "manual"
	LoopDiscoverySchedule       = "schedule"
	LoopDiscoveryWebhook        = "webhook"
	LoopDiscoveryMonitor        = "monitor"
	LoopDiscoveryEvalFailure    = "eval_failure"
	LoopDiscoveryConnectorEvent = "connector_event"

	LoopTriggerStatusStarted = "started"
	LoopTriggerStatusSkipped = "skipped"
	LoopTriggerStatusBlocked = "blocked"
	LoopTriggerStatusFailed  = "failed"
)

type LoopDiscoveryConfig struct {
	AutomationEnabled         bool
	ScheduleTriggersEnabled   bool
	WebhookTriggersEnabled    bool
	MonitorTriggersEnabled    bool
	EvalRepairTriggersEnabled bool
	ConnectorTriggersEnabled  bool
	TriggerTTL                time.Duration
}

type LoopDiscoveryEvent struct {
	UserID      string         `json:"user_id,omitempty"`
	SessionID   string         `json:"session_id,omitempty"`
	TriggerType string         `json:"trigger_type"`
	Source      string         `json:"source,omitempty"`
	DedupeKey   string         `json:"dedupe_key,omitempty"`
	Objective   string         `json:"objective,omitempty"`
	Payload     map[string]any `json:"payload,omitempty"`
}

type LoopTriggerRecord struct {
	ID            string         `json:"id"`
	UserID        string         `json:"user_id,omitempty"`
	SessionID     string         `json:"session_id,omitempty"`
	DedupeKey     string         `json:"dedupe_key"`
	TriggerType   string         `json:"trigger_type"`
	Source        string         `json:"source,omitempty"`
	Payload       map[string]any `json:"payload,omitempty"`
	JobID         string         `json:"job_id,omitempty"`
	LoopGoalID    string         `json:"loop_goal_id,omitempty"`
	Status        string         `json:"status"`
	FailureReason string         `json:"failure_reason,omitempty"`
	CreatedAt     time.Time      `json:"created_at"`
	ExpiresAt     time.Time      `json:"expires_at"`
}

type LoopDiscoveryResult struct {
	Trigger   LoopTriggerRecord `json:"trigger"`
	Job       *Job              `json:"job,omitempty"`
	Duplicate bool              `json:"duplicate"`
}

type LoopTriggerStore interface {
	Init(ctx context.Context) error
	CreateTrigger(ctx context.Context, trigger LoopTriggerRecord) (LoopTriggerRecord, bool, error)
	UpdateTriggerJob(ctx context.Context, userID, triggerID, sessionID, jobID, loopGoalID, status, failureReason string) error
	ListTriggers(ctx context.Context, userID, sessionID string, limit int) ([]LoopTriggerRecord, error)
	DeleteSession(ctx context.Context, userID, sessionID string) error
	DeleteUser(ctx context.Context, userID string) error
}

type UserDataExport struct {
	ExportedAt      time.Time                  `json:"exported_at"`
	User            *UserProfile               `json:"user,omitempty"`
	Sessions        []*state.Session           `json:"sessions"`
	Messages        map[string][]state.Message `json:"messages,omitempty"`
	Memory          MemoryExport               `json:"memory"`
	Personalization PersonalizationSettings    `json:"personalization"`
	Attachments     []*Artifact                `json:"attachments"`
	Artifacts       []*Artifact                `json:"artifacts"`
	Jobs            []*Job                     `json:"jobs,omitempty"`
	JobEvents       map[string][]*JobEvent     `json:"job_events,omitempty"`
}

type MemoryExport struct {
	User     string            `json:"user,omitempty"`
	Sessions map[string]string `json:"sessions"`
	Items    []MemoryItem      `json:"items,omitempty"`
	Episodes []MemoryEpisode   `json:"episodes,omitempty"`
}

type RuntimeConfig struct {
	DefaultWorkingDir     string
	UserWorkspaceRoot     string
	AllowCustomWorkingDir bool
	Timezone              string
	Locale                string
	TurnTimeout           time.Duration
	SkillShellTimeout     time.Duration
	DeepAgent             DeepAgentRuntimeConfig
	DeepResearch          DeepResearchRuntimeConfig
	LoopDiscovery         LoopDiscoveryConfig
	SkillShellSandbox     SkillShellSandboxConfig
	MessageSearch         MessageSearchConfig
	MemoryPolicy          MemoryPolicy
	MemoryPolicyProvider  MemoryPolicyProvider `json:"-"`
	MemoryVector          MemoryVectorConfig
	MemoryRecall          MemoryRecallConfig
	EpisodicMemory        EpisodicMemoryConfig
	Live                  LiveConfig
	CacheStore            CacheStore    `json:"-"`
	CacheMetrics          *CacheMetrics `json:"-"`
	CacheDefaultTTL       time.Duration
	CacheFailOpen         bool
	LLMGovernanceProvider func() LLMGovernanceConfig `json:"-"`
	Logger                *slog.Logger               `json:"-"`
}

type EpisodicMemoryConfig struct {
	Configured       bool
	Enabled          bool
	CaptureEnabled   bool
	ContextEnabled   bool
	MinMessages      int
	MaxMessages      int
	InjectLimit      int
	TTL              time.Duration
	SummarizeTimeout time.Duration
}

type MemoryRecallConfig struct {
	Configured                   bool
	Enabled                      bool
	ConditionalEnabled           bool
	AsyncEnabled                 bool
	Timeout                      time.Duration
	MinQueryRunes                int
	RecentContextMessages        int
	RecentContextMaxRunes        int
	ForceInterval                int
	ComplexTokenThreshold        int
	EmbeddingEnabled             bool
	EmbeddingSimilarityThreshold float64
	EmbeddingWindow              int
	IntentClassifierEnabled      bool
	IntentClassifierThreshold    float64
	IntentClassifierContextTurns int
	QueryRewriteEnabled          bool
	LLMTriggerEnabled            bool
	LLMTriggerTimeout            time.Duration
}

type DeepAgentRuntimeConfig struct {
	V2Enabled     bool
	V2ShadowRoute bool
}

type DeepResearchRuntimeConfig struct {
	OrchestratorWorkerEnabled bool
	WorkerBackend             string
	MaxWorkers                int
	MaxConcurrency            int
	WorkerTimeout             time.Duration
	TotalTimeout              time.Duration
	MaxRetries                int
	ReplanEnabled             bool
	MaxReplans                int
	ReplanEveryBatches        int
	FallbackLegacy            bool
	RequireSources            bool
	MinSuccessfulWorkers      int
}

type DeepResearchHarnessAgentRunner interface {
	RunDeepResearchAgent(ctx context.Context, input DeepResearchWorkerInput) (DeepResearchWorkerResult, error)
}

type LiveConfig struct {
	Enabled                    bool
	Provider                   string
	Model                      string
	VertexProjectID            string
	VertexLocation             string
	VertexBaseURL              string
	VertexAPIVersion           string
	XAIAPIKey                  string
	XAIBaseURL                 string
	InputAudioMIMEType         string
	OutputAudioMIMEType        string
	VoiceName                  string
	LanguageCode               string
	InputTranscriptionEnabled  bool
	OutputTranscriptionEnabled bool
	LiveVADStartSensitivity    string
	LiveVADEndSensitivity      string
	LiveVADThreshold           float64
	LiveVADPrefixPadding       time.Duration
	LiveVADSilenceDuration     time.Duration
	SessionTimeout             time.Duration
}

type MessageSearchConfig struct {
	Backend string

	Endpoint string
	Index    string
	APIKey   string
	Username string
	Password string
	Timeout  time.Duration

	IndexManagementEnabled     bool
	IndexLifecyclePolicy       string
	IndexTemplateName          string
	IndexWriteAlias            string
	IndexAnalyzer              string
	IndexSearchAnalyzer        string
	IndexDowngradeAfter        time.Duration
	IndexCloseAfter            time.Duration
	IndexMaintenanceInterval   time.Duration
	IndexMaintenanceBatchLimit int

	QdrantEndpoint       string
	QdrantCollection     string
	EpisodeCollection    string
	QdrantAPIKey         string
	QdrantScoreThreshold float64

	EmbeddingProvider      string
	EmbeddingEndpoint      string
	EmbeddingAPIKey        string
	EmbeddingAccessToken   string
	EmbeddingModel         string
	EmbeddingDimensions    int
	EmbeddingTimeout       time.Duration
	EmbeddingProjectID     string
	EmbeddingLocation      string
	EmbeddingTaskType      string
	EmbeddingIndexTaskType string
	EmbeddingAutoTruncate  bool

	RRFK                 int
	QueryRewriteEnabled  bool
	DynamicTopKEnabled   bool
	MinRecallWindow      int
	MaxRecallWindow      int
	MultiTurnEnabled     bool
	RerankEnabled        bool
	RerankCandidateLimit int
	LowConfidenceScore   float64

	CacheStore      CacheStore    `json:"-"`
	CacheMetrics    *CacheMetrics `json:"-"`
	CacheDefaultTTL time.Duration
	CacheFailOpen   bool
}

type MemoryVectorConfig struct {
	Enabled bool

	QdrantEndpoint       string
	QdrantCollection     string
	EpisodeCollection    string
	QdrantAPIKey         string
	QdrantScoreThreshold float64

	EmbeddingProvider      string
	EmbeddingEndpoint      string
	EmbeddingAPIKey        string
	EmbeddingAccessToken   string
	EmbeddingModel         string
	EmbeddingDimensions    int
	EmbeddingTimeout       time.Duration
	EmbeddingProjectID     string
	EmbeddingLocation      string
	EmbeddingTaskType      string
	EmbeddingIndexTaskType string
	EmbeddingAutoTruncate  bool

	Timeout time.Duration
	RRFK    int

	RerankEnabled        bool
	RerankEndpoint       string
	RerankAPIKey         string
	RerankModel          string
	RerankCandidateLimit int
	RerankResultLimit    int
	RerankTimeout        time.Duration
	RerankTruncate       string

	CacheStore      CacheStore    `json:"-"`
	CacheMetrics    *CacheMetrics `json:"-"`
	CacheDefaultTTL time.Duration
	CacheFailOpen   bool
}

type ToolPolicy struct {
	AllowWriteExecute bool
	AllowedTools      []string
	SafeWriteTools    []string
	SafeExecuteTools  []string
}

type ToolDenialRecord struct {
	ToolName string            `json:"tool_name"`
	Level    string            `json:"level"`
	Summary  string            `json:"summary,omitempty"`
	Reason   string            `json:"reason"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

type ToolDenialReporter func(context.Context, ToolDenialRecord)

func NewProductPermissionChecker(policy ToolPolicy) *permissions.Checker {
	return NewProductPermissionCheckerWithReporter(policy, nil)
}

func NewProductPermissionCheckerWithReporter(policy ToolPolicy, reporter ToolDenialReporter) *permissions.Checker {
	allowed := make(map[string]bool, len(policy.AllowedTools))
	for _, name := range policy.AllowedTools {
		if name != "" {
			allowed[name] = true
		}
	}
	safeWrite := make(map[string]bool, len(policy.SafeWriteTools))
	for _, name := range policy.SafeWriteTools {
		if name != "" {
			safeWrite[name] = true
		}
	}
	safeExecute := make(map[string]bool, len(policy.SafeExecuteTools))
	for _, name := range policy.SafeExecuteTools {
		if name != "" {
			safeExecute[name] = true
		}
	}

	return permissions.NewChecker(permissions.ModeDefault, nil, nil, permissions.WithDecisionHandler(func(ctx context.Context, request permissions.Request) (permissions.Decision, error) {
		if len(allowed) > 0 && !allowed[request.ToolName] {
			reportToolDenial(ctx, reporter, request, "tool is not enabled for this runtime scope")
			return permissions.Decision{
				Behavior: permissions.BehaviorDeny,
				Reason:   "tool is not enabled for this runtime scope",
			}, nil
		}
		if request.Level == permissions.LevelNone || request.Level == permissions.LevelRead {
			return permissions.Decision{Behavior: permissions.BehaviorAllow}, nil
		}
		if request.Level == permissions.LevelWrite && safeWrite[request.ToolName] {
			return permissions.Decision{Behavior: permissions.BehaviorAllow}, nil
		}
		if request.Level == permissions.LevelExecute && safeExecute[request.ToolName] {
			return permissions.Decision{Behavior: permissions.BehaviorAllow}, nil
		}
		if policy.AllowWriteExecute {
			return permissions.Decision{Behavior: permissions.BehaviorAllow}, nil
		}
		reportToolDenial(ctx, reporter, request, "write and execute tools are disabled by the product policy")
		return permissions.Decision{
			Behavior: permissions.BehaviorDeny,
			Reason:   "write and execute tools are disabled by the product policy",
		}, nil
	}))
}

func reportToolDenial(ctx context.Context, reporter ToolDenialReporter, request permissions.Request, reason string) {
	if reporter == nil {
		return
	}
	reporter(ctx, ToolDenialRecord{
		ToolName: request.ToolName,
		Level:    string(request.Level),
		Summary:  request.Summary,
		Reason:   reason,
		Metadata: cloneStringMap(request.Metadata),
	})
}

type SkillCatalog interface {
	GetSkill(name string) (*skills.SkillDefinition, bool)
	ListUserInvocableSkills() []*skills.SkillDefinition
	MatchUserInvocableSkill(prompt string) (*skills.SkillDefinition, bool)
}
