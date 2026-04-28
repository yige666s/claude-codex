package agent

import (
	"context"
	"time"
)

// AgentID uniquely identifies an agent instance
type AgentID string

// AgentType identifies the type/template of an agent
type AgentType string

// AgentSource indicates where the agent definition comes from
type AgentSource string

const (
	SourceBuiltIn         AgentSource = "built-in"
	SourcePlugin          AgentSource = "plugin"
	SourceUser            AgentSource = "user"
	SourceUserSettings    AgentSource = "userSettings"
	SourceProjectSettings AgentSource = "projectSettings"
	SourceLocalSettings   AgentSource = "localSettings"
	SourceFlagSettings    AgentSource = "flagSettings"
	SourcePolicySettings  AgentSource = "policySettings"
)

// PermissionMode controls how permissions are handled
type PermissionMode string

const (
	PermissionDefault PermissionMode = "default"
	PermissionBubble  PermissionMode = "bubble" // Surface to parent
	PermissionAllow   PermissionMode = "allow"  // Auto-allow
	PermissionDeny    PermissionMode = "deny"   // Auto-deny
)

// ModelOption specifies which model to use
type ModelOption string

const (
	ModelInherit ModelOption = "inherit" // Inherit from parent
	ModelSonnet  ModelOption = "sonnet"
	ModelOpus    ModelOption = "opus"
	ModelHaiku   ModelOption = "haiku"
)

// AgentDefinition defines an agent template
type AgentDefinition struct {
	AgentType       AgentType
	WhenToUse       string
	Tools           []string // Tool names/rule specs, or ["*"] for all tools
	DisallowedTools []string // Tool names explicitly denied
	MaxTurns        int
	Model           ModelOption
	Effort          string
	Permission      PermissionMode
	Source          AgentSource
	BaseDir         string
	Filename        string
	SystemPrompt    string
	InitialPrompt   string
	Background      bool   // Always run async
	Isolation       string // worktree or remote
	Memory          string // user, project, local
	OmitClaudeMd    bool   // Skip CLAUDE.md injection
	Color           string // UI color hint

	// Optional MCP servers
	MCPServers []string

	// Optional required MCP server patterns
	RequiredMCPServers []string

	// Optional skills
	Skills []string
}

// AgentInstance represents a running agent
type AgentInstance struct {
	ID        AgentID
	Type      AgentType
	ParentID  *AgentID
	Model     string // Resolved model string
	StartTime time.Time
	EndTime   *time.Time
	Status    AgentStatus
	TurnCount int
	MaxTurns  int

	// Context
	WorkingDir string
	Tools      []string

	// State
	Messages    []Message
	AbortSignal context.CancelFunc
}

// AgentStatus represents the current state of an agent
type AgentStatus string

const (
	StatusStarting  AgentStatus = "starting"
	StatusRunning   AgentStatus = "running"
	StatusCompleted AgentStatus = "completed"
	StatusFailed    AgentStatus = "failed"
	StatusAborted   AgentStatus = "aborted"
)

// Message represents a conversation message
type Message struct {
	ID        string
	Role      string // "user" or "assistant"
	Content   []ContentBlock
	Timestamp time.Time
}

// ContentBlock represents a piece of message content
type ContentBlock struct {
	Type string // "text", "tool_use", "tool_result"

	// For text blocks
	Text string

	// For tool_use blocks
	ToolName  string
	ToolInput interface{}
	ToolID    string

	// For tool_result blocks
	ToolUseID string
	Result    interface{}
	IsError   bool
}

// AgentResult represents the outcome of an agent execution
type AgentResult struct {
	AgentID      AgentID
	Success      bool
	Error        error
	TurnCount    int
	Duration     time.Duration
	FinalMessage *Message

	// Metrics
	InputTokens  int
	OutputTokens int
	TotalTokens  int
}

// AgentConfig contains configuration for running an agent
type AgentConfig struct {
	Definition    *AgentDefinition
	ParentID      *AgentID
	ParentModel   string
	WorkingDir    string
	InitialPrompt string

	// Fork-specific
	IsFork         bool
	InheritContext bool
	ParentMessages []Message

	// Overrides
	SystemPrompt *string
	MaxTurns     *int

	// Streaming
	StreamCallback StreamCallback
}

// StreamCallback is called for each streaming event
type StreamCallback func(event StreamEvent)

// StreamEvent represents a streaming event from the agent
type StreamEvent struct {
	Type      string // "text_delta", "tool_use_start", "tool_use_end", etc.
	Content   string // Text content for text_delta
	ToolName  string // Tool name for tool_use events
	ToolID    string // Tool ID for tool_use events
	Timestamp time.Time
}

// ProgressUpdate represents a progress notification from a running agent
type ProgressUpdate struct {
	AgentID    AgentID
	TurnNumber int
	Status     AgentStatus
	Summary    string
	Timestamp  time.Time
}
