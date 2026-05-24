package agentruntime

import (
	"context"
	"encoding/json"
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

type Scope struct {
	UserID            string
	SessionID         string
	WorkingDir        string
	Prompt            string
	SkillName         string
	SkillRoot         string
	SkillScoped       bool
	SkillShell        skills.FrontmatterShell
	SkillShellEnv     map[string]string
	SkillShellSandbox SkillShellSandboxConfig
	AllowedTools      []string
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
	Type      string          `json:"type"`
	SessionID string          `json:"session_id,omitempty"`
	JobID     string          `json:"job_id,omitempty"`
	Job       *Job            `json:"job,omitempty"`
	JobReason string          `json:"job_reason,omitempty"`
	Role      string          `json:"role,omitempty"`
	Content   string          `json:"content,omitempty"`
	Error     string          `json:"error,omitempty"`
	Data      json.RawMessage `json:"data,omitempty"`
}

type LiveRequest struct {
	UserID    string
	SessionID string
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
	UserID         string
	SessionID      string
	Content        string
	AttachmentIDs  []string
	AttachmentURLs []ChatAttachmentURL
	ThinkingMode   bool
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
	JobStatusQueued    = "queued"
	JobStatusRunning   = "running"
	JobStatusSucceeded = "succeeded"
	JobStatusFailed    = "failed"
	JobStatusCancelled = "cancelled"
)

type Job struct {
	ID             string              `json:"id"`
	UserID         string              `json:"user_id,omitempty"`
	SessionID      string              `json:"session_id"`
	Type           string              `json:"type"`
	Status         string              `json:"status"`
	Content        string              `json:"content,omitempty"`
	AttachmentIDs  []string            `json:"attachment_ids,omitempty"`
	AttachmentURLs []ChatAttachmentURL `json:"attachment_urls,omitempty"`
	Error          string              `json:"error,omitempty"`
	CreatedAt      time.Time           `json:"created_at"`
	UpdatedAt      time.Time           `json:"updated_at"`
	StartedAt      *time.Time          `json:"started_at,omitempty"`
	FinishedAt     *time.Time          `json:"finished_at,omitempty"`
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
}

type RuntimeConfig struct {
	DefaultWorkingDir     string
	UserWorkspaceRoot     string
	AllowCustomWorkingDir bool
	TurnTimeout           time.Duration
	SkillShellTimeout     time.Duration
	SkillShellSandbox     SkillShellSandboxConfig
	MessageSearch         MessageSearchConfig
	Live                  LiveConfig
}

type LiveConfig struct {
	Enabled                    bool
	Provider                   string
	Model                      string
	VertexProjectID            string
	VertexLocation             string
	VertexBaseURL              string
	VertexAPIVersion           string
	InputAudioMIMEType         string
	OutputAudioMIMEType        string
	InputTranscriptionEnabled  bool
	OutputTranscriptionEnabled bool
	LiveVADStartSensitivity    string
	LiveVADEndSensitivity      string
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

	RRFK int
}

type ToolPolicy struct {
	AllowWriteExecute bool
	AllowedTools      []string
	SafeWriteTools    []string
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
