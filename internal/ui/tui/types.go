package tui

import (
	"context"
	"io"

	"claude-codex/internal/harness/engine"
	"claude-codex/internal/harness/permissions"
	"claude-codex/internal/harness/state"
	"claude-codex/internal/harness/tools"
)

// Command represents a slash command
type Command struct {
	Name        string
	Aliases     []string
	Description string
	Usage       string
}

// CommandRegistry provides access to registered commands
type CommandRegistry interface {
	List() []Command
	Execute(ctx context.Context, name string, args []string) (output string, err error)
}

// Runner is the non-streaming runner used for non-interactive and fallback cases.
type Runner func(context.Context, *state.Session, string) (engine.Result, error)

// GeneratedRunner executes an internal prompt that should not be recorded as a
// visible user message in the transcript.
type GeneratedRunner func(context.Context, *state.Session, string) (engine.Result, error)

// StreamRunner is called when streaming is available. onChunk is invoked for each
// text delta from the model; the final engine.Result is returned when done.
type StreamRunner func(ctx context.Context, session *state.Session, prompt string, onChunk func(string)) (engine.Result, error)

type SaveTheme func(string) error

type AuthViewData struct {
	Status           string
	Authenticated    bool
	HasTrustedDevice bool
	ExpiresAt        string
	Scopes           []string
	SubscriptionType string
	RateLimitTier    string
}

type SandboxViewData struct {
	Mode           string
	ExecutionEnv   string
	WorkingDir     string
	ApprovalPolicy string
	WritableRoots  []string
	Notes          []string
}

type MCPServerViewData struct {
	Name      string
	Transport string
	Target    string
	Source    string
}

type MCPViewData struct {
	Servers  []MCPServerViewData
	LoadedAt string
}

type TeamMemberViewData struct {
	Name      string
	AgentID   string
	AgentType string
	Model     string
	Mode      string
	Backend   string
	CWD       string
	Active    bool
}

type TeamViewData struct {
	Name               string
	Source             string
	Description        string
	CreatedAt          string
	PendingPermissions int
	Members            []TeamMemberViewData
}

type TeamsViewData struct {
	Teams    []TeamViewData
	LoadedAt string
}

type SkillStatsViewData struct {
	Total         int
	UserInvocable int
	Dynamic       int
	Conditional   int
}

type ContextBudgetViewData struct {
	Model                   string
	ContextWindowTokens     int
	CompressionSoftTokens   int
	CompressionTargetTokens int
}

type Options struct {
	Title              string
	WorkingDir         string
	Theme              string
	Session            *state.Session
	PermissionMode     string
	AuthStatus         AuthViewData
	PermissionBroker   *PermissionBroker
	Runner             Runner
	GeneratedRunner    GeneratedRunner
	StreamRunner       StreamRunner
	SaveTheme          SaveTheme
	Input              io.Reader
	Output             io.Writer
	Err                io.Writer
	Context            context.Context
	Registry           CommandRegistry
	ProgressCh         chan tools.ProgressEvent
	PromptSuggestionCh chan string
	LoadSandboxView    func() SandboxViewData
	LoadMCPView        func() MCPViewData
	LoadTeamsView      func() TeamsViewData
	SkillStats         SkillStatsViewData
	ContextBudget      ContextBudgetViewData
}

type permissionResult struct {
	decision permissions.Decision
	err      error
}

type permissionEnvelope struct {
	request permissions.Request
	reply   chan permissionResult
}
