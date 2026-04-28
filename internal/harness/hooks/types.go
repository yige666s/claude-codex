package hooks

import (
	"context"
	"time"
)

// HookEvent defines when a hook can be triggered.
type HookEvent string

const (
	// Tool-related events
	EventPreToolUse         HookEvent = "PreToolUse"
	EventPostToolUse        HookEvent = "PostToolUse"
	EventPostToolUseFailure HookEvent = "PostToolUseFailure"

	// Session events
	EventSessionStart HookEvent = "SessionStart"
	EventSessionEnd   HookEvent = "SessionEnd"
	EventSetup        HookEvent = "Setup"

	// User interaction events
	EventUserPromptSubmit HookEvent = "UserPromptSubmit"
	EventNotification     HookEvent = "Notification"

	// Permission events
	EventPermissionRequest HookEvent = "PermissionRequest"
	EventPermissionDenied  HookEvent = "PermissionDenied"

	// Stop events
	EventStop        HookEvent = "Stop"
	EventStopFailure HookEvent = "StopFailure"

	// Subagent events
	EventSubagentStart HookEvent = "SubagentStart"
	EventSubagentStop  HookEvent = "SubagentStop"

	// Task events
	EventTaskCreated   HookEvent = "TaskCreated"
	EventTaskCompleted HookEvent = "TaskCompleted"

	// Configuration events
	EventConfigChange HookEvent = "ConfigChange"
	EventCwdChanged   HookEvent = "CwdChanged"
	EventFileChanged  HookEvent = "FileChanged"

	// Compact events
	EventPreCompact  HookEvent = "PreCompact"
	EventPostCompact HookEvent = "PostCompact"
)

// Hook defines the interface that all hooks must implement.
type Hook interface {
	// Name returns the unique name of this hook.
	Name() string

	// Event returns the event type this hook handles.
	Event() HookEvent

	// Execute runs the hook with the given input.
	Execute(ctx context.Context, input *HookInput) (*HookResult, error)

	// IsAsync returns true if this hook should run asynchronously.
	IsAsync() bool

	// Timeout returns the maximum execution time for this hook.
	Timeout() time.Duration
}

// HookInput contains the context passed to hooks.
type HookInput struct {
	Event      HookEvent
	SessionID  string
	WorkingDir string
	AgentID    string

	// Tool-related fields (for tool events)
	Tool *ToolInfo

	// Message-related fields (for message events)
	Message *MessageInfo

	// Permission-related fields (for permission events)
	Permission *PermissionInfo

	// Task-related fields (for task events)
	Task *TaskInfo

	// Generic metadata
	Metadata map[string]any
}

// ToolInfo contains information about a tool being executed.
type ToolInfo struct {
	Name        string
	Input       map[string]any
	Output      string
	Error       error
	IsMCP       bool
	Description string
}

// MessageInfo contains information about a message.
type MessageInfo struct {
	Role    string
	Content string
	ID      string
}

// PermissionInfo contains information about a permission request.
type PermissionInfo struct {
	ToolName    string
	Description string
	Input       map[string]any
	Reason      string
}

// TaskInfo contains information about a task.
type TaskInfo struct {
	ID          string
	Description string
	Status      string
}

// HookResult contains the result of hook execution.
type HookResult struct {
	// Continue indicates whether execution should continue after this hook.
	Continue bool

	// SuppressOutput hides stdout from transcript.
	SuppressOutput bool

	// StopReason is the message shown when Continue is false.
	StopReason string

	// SystemMessage is a warning message shown to the user.
	SystemMessage string

	// AdditionalContext is additional context to add to the system prompt.
	AdditionalContext string

	// PermissionDecision is the permission decision for PreToolUse hooks.
	PermissionDecision *PermissionDecision

	// UpdatedInput contains modified tool input (for PreToolUse hooks).
	UpdatedInput map[string]any

	// UpdatedMCPToolOutput contains modified MCP tool output (for PostToolUse hooks).
	UpdatedMCPToolOutput any

	// BlockingError is an error that blocks execution.
	BlockingError string

	// InitialUserMessage is the initial message to send (for SessionStart hooks).
	InitialUserMessage string

	// WatchPaths are paths to watch for FileChanged hooks.
	WatchPaths []string

	// Retry indicates whether to retry after permission denial.
	Retry bool
}

// PermissionDecision represents a permission decision.
type PermissionDecision struct {
	Behavior           string // "allow", "deny", "ask", "passthrough"
	Reason             string
	UpdatedInput       map[string]any
	UpdatedPermissions []PermissionUpdate
	Message            string
	Interrupt          bool
}

// PermissionUpdate represents a permission update.
type PermissionUpdate struct {
	Tool     string
	Behavior string
	Reason   string
}

// AggregatedResult contains the aggregated results from multiple hooks.
type AggregatedResult struct {
	// Continue indicates whether execution should continue.
	Continue bool

	// StopReason is the reason for stopping (if Continue is false).
	StopReason string

	// SystemMessage is a system message to display.
	SystemMessage string

	// AdditionalContexts are additional contexts from all hooks.
	AdditionalContexts []string

	// PermissionBehavior is the final permission decision.
	PermissionBehavior string

	// PermissionDecisionReason is the reason for the permission decision.
	PermissionDecisionReason string

	// PermissionUpdates are rule updates suggested by permission hooks.
	PermissionUpdates []PermissionUpdate

	// UpdatedInput contains the final modified input.
	UpdatedInput map[string]any

	// UpdatedMCPToolOutput contains the final modified MCP tool output.
	UpdatedMCPToolOutput any

	// BlockingErrors are errors that blocked execution.
	BlockingErrors []string

	// InitialUserMessage is the initial message to send.
	InitialUserMessage string

	// WatchPaths are paths to watch for FileChanged hooks.
	WatchPaths []string

	// Retry indicates whether to retry.
	Retry bool
}

// HookConfig contains configuration for a hook.
type HookConfig struct {
	Name     string
	Event    HookEvent
	Command  string
	Timeout  time.Duration
	Async    bool
	Enabled  bool
	Matcher  string
	Internal bool
}

// DefaultTimeout is the default timeout for hook execution.
const DefaultTimeout = 30 * time.Second

// MaxAsyncHooks is the maximum number of concurrent async hooks.
const MaxAsyncHooks = 10
