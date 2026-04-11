package state

import (
	"sync"

	"claude-codex/internal/harness/tasks"
)

// AppState represents the backend application state
type AppState struct {
	mu sync.RWMutex

	// Task management
	Tasks            map[string]tasks.TaskState
	AgentNameRegistry map[string]string // name -> agentId

	// Permission system
	ToolPermissionContext ToolPermissionContext

	// Agent system
	AgentDefinitions AgentDefinitionsResult

	// File history
	FileHistory FileHistoryState

	// MCP integration
	MCP MCPState

	// Plugin system
	Plugins PluginState

	// Notifications
	Notifications NotificationState

	// Session hooks
	SessionHooks map[string]SessionHook

	// Settings
	Settings map[string]interface{}
	Verbose  bool

	// Model configuration
	MainLoopModel          *string
	MainLoopModelForSession *string

	// Remote connection
	RemoteSessionURL        *string
	RemoteConnectionStatus  string
	RemoteBackgroundTaskCount int

	// Agent context
	Agent *string

	// Auth version
	AuthVersion int

	// Fast mode
	FastMode bool

	// Effort value
	EffortValue *string

	// Thinking enabled
	ThinkingEnabled *bool

	// Prompt suggestion enabled
	PromptSuggestionEnabled bool
}

// ToolPermissionContext represents tool permission configuration
type ToolPermissionContext struct {
	Mode                            string
	IsBypassPermissionsModeAvailable bool
	AllowedTools                    []string
	DeniedTools                     []string
}

// AgentDefinitionsResult contains agent definitions
type AgentDefinitionsResult struct {
	ActiveAgents []AgentDefinition
	AllAgents    []AgentDefinition
}

// AgentDefinition represents an agent definition
type AgentDefinition struct {
	AgentType       string
	WhenToUse       string
	Source          string
	GetSystemPrompt func() string
}

// FileHistoryState tracks file history
type FileHistoryState struct {
	Snapshots        []FileSnapshot
	TrackedFiles     map[string]bool
	SnapshotSequence int
}

// FileSnapshot represents a file snapshot
type FileSnapshot struct {
	Path      string
	Content   string
	Timestamp int64
	Sequence  int
}

// MCPState represents MCP integration state
type MCPState struct {
	Clients             []MCPServerConnection
	Tools               []MCPTool
	Commands            []MCPCommand
	Resources           map[string][]ServerResource
	PluginReconnectKey  int
}

// MCPServerConnection represents an MCP server connection
type MCPServerConnection struct {
	Name   string
	Status string
	// Add more fields as needed
}

// MCPTool represents an MCP tool
type MCPTool struct {
	Name        string
	Description string
	Schema      map[string]interface{}
}

// MCPCommand represents an MCP command
type MCPCommand struct {
	Name        string
	Description string
}

// ServerResource represents an MCP server resource
type ServerResource struct {
	URI         string
	Name        string
	Description string
	MimeType    string
}

// PluginState represents plugin system state
type PluginState struct {
	Enabled            []LoadedPlugin
	Disabled           []LoadedPlugin
	Commands           []PluginCommand
	Errors             []PluginError
	InstallationStatus InstallationStatus
	NeedsRefresh       bool
}

// LoadedPlugin represents a loaded plugin
type LoadedPlugin struct {
	ID      string
	Name    string
	Version string
	Path    string
}

// PluginCommand represents a plugin command
type PluginCommand struct {
	Name        string
	Description string
	Handler     func(args []string) error
}

// PluginError represents a plugin error
type PluginError struct {
	PluginID string
	Message  string
	Context  map[string]interface{}
}

// InstallationStatus tracks plugin installation status
type InstallationStatus struct {
	Marketplaces []MarketplaceStatus
	Plugins      []PluginInstallStatus
}

// MarketplaceStatus represents marketplace installation status
type MarketplaceStatus struct {
	Name   string
	Status string // "pending", "installing", "installed", "failed"
	Error  *string
}

// PluginInstallStatus represents plugin installation status
type PluginInstallStatus struct {
	ID     string
	Name   string
	Status string // "pending", "installing", "installed", "failed"
	Error  *string
}

// NotificationState manages notifications
type NotificationState struct {
	Current *Notification
	Queue   []Notification
}

// Notification represents a notification
type Notification struct {
	ID        string
	Type      string
	Message   string
	Timestamp int64
}

// SessionHook represents a session hook
type SessionHook struct {
	Name    string
	Command string
	Enabled bool
}

// NewAppState creates a new application state with default values
func NewAppState() *AppState {
	return &AppState{
		Tasks:                     make(map[string]tasks.TaskState),
		AgentNameRegistry:         make(map[string]string),
		ToolPermissionContext:     GetEmptyToolPermissionContext(),
		AgentDefinitions:          AgentDefinitionsResult{},
		FileHistory:               NewFileHistoryState(),
		MCP:                       NewMCPState(),
		Plugins:                   NewPluginState(),
		Notifications:             NewNotificationState(),
		SessionHooks:              make(map[string]SessionHook),
		Settings:                  make(map[string]interface{}),
		Verbose:                   false,
		RemoteConnectionStatus:    "connecting",
		RemoteBackgroundTaskCount: 0,
		AuthVersion:               0,
		FastMode:                  false,
		PromptSuggestionEnabled:   true,
	}
}

// GetEmptyToolPermissionContext returns an empty tool permission context
func GetEmptyToolPermissionContext() ToolPermissionContext {
	return ToolPermissionContext{
		Mode:                            "default",
		IsBypassPermissionsModeAvailable: false,
		AllowedTools:                    []string{},
		DeniedTools:                     []string{},
	}
}

// NewFileHistoryState creates a new file history state
func NewFileHistoryState() FileHistoryState {
	return FileHistoryState{
		Snapshots:        []FileSnapshot{},
		TrackedFiles:     make(map[string]bool),
		SnapshotSequence: 0,
	}
}

// NewMCPState creates a new MCP state
func NewMCPState() MCPState {
	return MCPState{
		Clients:             []MCPServerConnection{},
		Tools:               []MCPTool{},
		Commands:            []MCPCommand{},
		Resources:           make(map[string][]ServerResource),
		PluginReconnectKey:  0,
	}
}

// NewPluginState creates a new plugin state
func NewPluginState() PluginState {
	return PluginState{
		Enabled:  []LoadedPlugin{},
		Disabled: []LoadedPlugin{},
		Commands: []PluginCommand{},
		Errors:   []PluginError{},
		InstallationStatus: InstallationStatus{
			Marketplaces: []MarketplaceStatus{},
			Plugins:      []PluginInstallStatus{},
		},
		NeedsRefresh: false,
	}
}

// NewNotificationState creates a new notification state
func NewNotificationState() NotificationState {
	return NotificationState{
		Current: nil,
		Queue:   []Notification{},
	}
}

// GetState returns a copy of the current state (thread-safe read)
func (s *AppState) GetState() *AppState {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Return a shallow copy for read access
	// Deep copy would be needed for full immutability
	stateCopy := *s
	return &stateCopy
}

// SetState updates the state using an updater function (thread-safe write)
func (s *AppState) SetState(updater func(prev *AppState) *AppState) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Apply the updater function
	newState := updater(s)

	// Update the state
	*s = *newState
}
