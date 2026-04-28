package schemas

import "time"

// PermissionMode represents the permission mode
type PermissionMode string

const (
	PermissionModeDefault     PermissionMode = "default"
	PermissionModePlan        PermissionMode = "plan"
	PermissionModeAcceptEdits PermissionMode = "acceptEdits"
	PermissionModeDontAsk     PermissionMode = "dontAsk"
	PermissionModeAsk         PermissionMode = "ask"
	PermissionModeAllow       PermissionMode = "allow"
	PermissionModeAuto        PermissionMode = "auto"
	PermissionModeYolo        PermissionMode = "yolo"
	PermissionModeBypass      PermissionMode = "bypass"
)

// PermissionRule represents a permission rule pattern
type PermissionRule string

// HookType represents the type of hook
type HookType string

const (
	HookTypeBash   HookType = "bash"
	HookTypePrompt HookType = "prompt"
	HookTypeHTTP   HookType = "http"
	HookTypeAgent  HookType = "agent"
)

// HookEvent represents when a hook should be triggered
type HookEvent string

const (
	HookEventUserPromptSubmit   HookEvent = "UserPromptSubmit"
	HookEventPreToolUse         HookEvent = "PreToolUse"
	HookEventPostToolUse        HookEvent = "PostToolUse"
	HookEventPostToolUseFailure HookEvent = "PostToolUseFailure"
	HookEventStop               HookEvent = "Stop"
	HookEventTaskCompleted      HookEvent = "TaskCompleted"
	HookEventTeammateIdle       HookEvent = "TeammateIdle"
	HookEventSessionStart       HookEvent = "SessionStart"
)

// MCPTransportType represents MCP server transport type
type MCPTransportType string

const (
	MCPTransportStdio     MCPTransportType = "stdio"
	MCPTransportSSE       MCPTransportType = "sse"
	MCPTransportHTTP      MCPTransportType = "http"
	MCPTransportWebSocket MCPTransportType = "ws"
	MCPTransportSDK       MCPTransportType = "sdk"
)

// MarketplaceSourceType represents marketplace source type
type MarketplaceSourceType string

const (
	MarketplaceSourceGitHub MarketplaceSourceType = "github"
	MarketplaceSourceGit    MarketplaceSourceType = "git"
	MarketplaceSourceLocal  MarketplaceSourceType = "local"
)

// KeybindingContext represents keybinding context
type KeybindingContext string

const (
	KeybindingContextGlobal       KeybindingContext = "Global"
	KeybindingContextChat         KeybindingContext = "Chat"
	KeybindingContextAutocomplete KeybindingContext = "Autocomplete"
	KeybindingContextConfirmation KeybindingContext = "Confirmation"
	KeybindingContextHelp         KeybindingContext = "Help"
	KeybindingContextTranscript   KeybindingContext = "Transcript"
	// Add more contexts as needed
)

// ValidationError represents a validation error with context
type ValidationError struct {
	Path       string
	Message    string
	Suggestion string
	DocLink    string
}

func (e *ValidationError) Error() string {
	if e.Suggestion != "" {
		return e.Path + ": " + e.Message + " (suggestion: " + e.Suggestion + ")"
	}
	return e.Path + ": " + e.Message
}

// ValidationResult represents the result of validation
type ValidationResult struct {
	Valid  bool
	Errors []ValidationError
}

// Hook represents a base hook configuration
type Hook interface {
	GetType() HookType
	GetTimeout() *int
	GetStatusMessage() *string
	GetOnce() bool
	GetIf() *string
	Validate() error
}

// BaseHook contains common hook fields
type BaseHook struct {
	Type          HookType `json:"type" validate:"required"`
	Timeout       *int     `json:"timeout,omitempty"`
	StatusMessage *string  `json:"statusMessage,omitempty"`
	Once          bool     `json:"once,omitempty"`
	If            *string  `json:"if,omitempty"`
}

func (h *BaseHook) GetType() HookType {
	return h.Type
}

func (h *BaseHook) GetTimeout() *int {
	return h.Timeout
}

func (h *BaseHook) GetStatusMessage() *string {
	return h.StatusMessage
}

func (h *BaseHook) GetOnce() bool {
	return h.Once
}

func (h *BaseHook) GetIf() *string {
	return h.If
}

// BashCommandHook represents a bash command hook
type BashCommandHook struct {
	BaseHook
	Command     string  `json:"command" validate:"required"`
	Shell       *string `json:"shell,omitempty"`
	Async       bool    `json:"async,omitempty"`
	AsyncRewake bool    `json:"asyncRewake,omitempty"`
}

func (h *BashCommandHook) Validate() error {
	if h.Command == "" {
		return &ValidationError{
			Path:    "command",
			Message: "command is required for bash hooks",
		}
	}
	return nil
}

// PromptHook represents a prompt evaluation hook
type PromptHook struct {
	BaseHook
	Prompt string  `json:"prompt" validate:"required"`
	Model  *string `json:"model,omitempty"`
}

func (h *PromptHook) Validate() error {
	if h.Prompt == "" {
		return &ValidationError{
			Path:    "prompt",
			Message: "prompt is required for prompt hooks",
		}
	}
	return nil
}

// HTTPHook represents an HTTP POST hook
type HTTPHook struct {
	BaseHook
	URL            string            `json:"url" validate:"required,url"`
	Headers        map[string]string `json:"headers,omitempty"`
	AllowedEnvVars []string          `json:"allowedEnvVars,omitempty"`
}

func (h *HTTPHook) Validate() error {
	if h.URL == "" {
		return &ValidationError{
			Path:    "url",
			Message: "url is required for http hooks",
		}
	}
	return nil
}

// AgentHook represents an agentic verification hook
type AgentHook struct {
	BaseHook
	Prompt string  `json:"prompt" validate:"required"`
	Model  *string `json:"model,omitempty"`
}

func (h *AgentHook) Validate() error {
	if h.Prompt == "" {
		return &ValidationError{
			Path:    "prompt",
			Message: "prompt is required for agent hooks",
		}
	}
	// Default timeout for agent hooks is 60 seconds
	if h.Timeout == nil {
		timeout := 60000
		h.Timeout = &timeout
	}
	return nil
}

// HookMatcher represents a hook configuration with matchers
type HookMatcher struct {
	Hook       Hook
	ToolName   *string `json:"toolName,omitempty"`
	ToolInput  *string `json:"toolInput,omitempty"`
	ToolOutput *string `json:"toolOutput,omitempty"`
}

// Permissions represents permission configuration
type Permissions struct {
	Allow                        []PermissionRule `json:"allow,omitempty"`
	Deny                         []PermissionRule `json:"deny,omitempty"`
	Ask                          []PermissionRule `json:"ask,omitempty"`
	DefaultMode                  PermissionMode   `json:"defaultMode,omitempty"`
	DisableBypassPermissionsMode string           `json:"disableBypassPermissionsMode,omitempty"`
	DisableAutoMode              string           `json:"disableAutoMode,omitempty"`
	AdditionalDirectories        []string         `json:"additionalDirectories,omitempty"`
}

// EnvironmentVariables represents environment variable configuration
type EnvironmentVariables map[string]string

// MCPServerConfig represents MCP server configuration
type MCPServerConfig interface {
	GetTransport() MCPTransportType
	Validate() error
}

// MCPStdioServerConfig represents stdio transport configuration
type MCPStdioServerConfig struct {
	Command string            `json:"command" validate:"required"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
}

func (c *MCPStdioServerConfig) GetTransport() MCPTransportType {
	return MCPTransportStdio
}

func (c *MCPStdioServerConfig) Validate() error {
	if c.Command == "" {
		return &ValidationError{
			Path:    "command",
			Message: "command is required for stdio transport",
		}
	}
	return nil
}

// MCPSSEServerConfig represents SSE transport configuration
type MCPSSEServerConfig struct {
	URL     string            `json:"url" validate:"required,url"`
	Headers map[string]string `json:"headers,omitempty"`
}

func (c *MCPSSEServerConfig) GetTransport() MCPTransportType {
	return MCPTransportSSE
}

func (c *MCPSSEServerConfig) Validate() error {
	if c.URL == "" {
		return &ValidationError{
			Path:    "url",
			Message: "url is required for sse transport",
		}
	}
	return nil
}

// MCPHTTPServerConfig represents HTTP transport configuration
type MCPHTTPServerConfig struct {
	URL     string            `json:"url" validate:"required,url"`
	Headers map[string]string `json:"headers,omitempty"`
}

func (c *MCPHTTPServerConfig) GetTransport() MCPTransportType {
	return MCPTransportHTTP
}

func (c *MCPHTTPServerConfig) Validate() error {
	if c.URL == "" {
		return &ValidationError{
			Path:    "url",
			Message: "url is required for http transport",
		}
	}
	return nil
}

// MCPWebSocketServerConfig represents WebSocket transport configuration
type MCPWebSocketServerConfig struct {
	URL     string            `json:"url" validate:"required,url"`
	Headers map[string]string `json:"headers,omitempty"`
}

func (c *MCPWebSocketServerConfig) GetTransport() MCPTransportType {
	return MCPTransportWebSocket
}

func (c *MCPWebSocketServerConfig) Validate() error {
	if c.URL == "" {
		return &ValidationError{
			Path:    "url",
			Message: "url is required for websocket transport",
		}
	}
	return nil
}

// AllowedMCPServerEntry represents an allowed MCP server entry
type AllowedMCPServerEntry struct {
	ServerName    string   `json:"serverName,omitempty"`
	ServerCommand []string `json:"serverCommand,omitempty"`
	ServerURL     string   `json:"serverUrl,omitempty"`
	ToolName      string   `json:"toolName,omitempty"`
}

// DeniedMCPServerEntry represents a denied MCP server entry
type DeniedMCPServerEntry struct {
	ServerName    string   `json:"serverName,omitempty"`
	ServerCommand []string `json:"serverCommand,omitempty"`
	ServerURL     string   `json:"serverUrl,omitempty"`
	ToolName      string   `json:"toolName,omitempty"`
}

// SandboxNetworkConfig represents sandbox network configuration
type SandboxNetworkConfig struct {
	AllowedDomains          []string `json:"allowedDomains,omitempty"`
	AllowManagedDomainsOnly bool     `json:"allowManagedDomainsOnly,omitempty"`
	AllowUnixSockets        []string `json:"allowUnixSockets,omitempty"`
	AllowLocalBinding       bool     `json:"allowLocalBinding,omitempty"`
	HTTPProxyPort           *int     `json:"httpProxyPort,omitempty"`
}

// SandboxFilesystemConfig represents sandbox filesystem configuration
type SandboxFilesystemConfig struct {
	AllowWrite []string `json:"allowWrite,omitempty"`
	DenyWrite  []string `json:"denyWrite,omitempty"`
	DenyRead   []string `json:"denyRead,omitempty"`
	AllowRead  []string `json:"allowRead,omitempty"`
}

// SandboxSettings represents sandbox configuration
type SandboxSettings struct {
	Enabled                  bool                     `json:"enabled,omitempty"`
	FailIfUnavailable        bool                     `json:"failIfUnavailable,omitempty"`
	AutoAllowBashIfSandboxed bool                     `json:"autoAllowBashIfSandboxed,omitempty"`
	AllowUnsandboxedCommands bool                     `json:"allowUnsandboxedCommands,omitempty"`
	Network                  *SandboxNetworkConfig    `json:"network,omitempty"`
	Filesystem               *SandboxFilesystemConfig `json:"filesystem,omitempty"`
}

// MarketplaceSource represents a marketplace source configuration
type MarketplaceSource struct {
	Type  MarketplaceSourceType `json:"type" validate:"required"`
	Owner string                `json:"owner,omitempty"`
	Repo  string                `json:"repo,omitempty"`
	Ref   string                `json:"ref,omitempty"`
	URL   string                `json:"url,omitempty"`
	Path  string                `json:"path,omitempty"`
}

// PluginManifest represents plugin metadata
type PluginManifest struct {
	Name        string            `json:"name" validate:"required"`
	Version     string            `json:"version" validate:"required"`
	Description string            `json:"description,omitempty"`
	Author      string            `json:"author,omitempty"`
	License     string            `json:"license,omitempty"`
	Homepage    string            `json:"homepage,omitempty"`
	Repository  string            `json:"repository,omitempty"`
	Keywords    []string          `json:"keywords,omitempty"`
	Main        string            `json:"main,omitempty"`
	Commands    []CommandMetadata `json:"commands,omitempty"`
}

// CommandMetadata represents plugin command metadata
type CommandMetadata struct {
	Name        string `json:"name" validate:"required"`
	Description string `json:"description,omitempty"`
	Usage       string `json:"usage,omitempty"`
}

// Keybinding represents a keybinding configuration
type Keybinding struct {
	Context KeybindingContext `json:"context" validate:"required"`
	Key     string            `json:"key" validate:"required"`
	Action  string            `json:"action" validate:"required"`
}

// Settings represents the root settings.json structure
type Settings struct {
	Permissions       *Permissions                 `json:"permissions,omitempty"`
	Hooks             map[HookEvent][]HookMatcher  `json:"hooks,omitempty"`
	MCPServers        map[string]any               `json:"mcpServers,omitempty"`
	Env               EnvironmentVariables         `json:"env,omitempty"`
	Marketplaces      map[string]MarketplaceSource `json:"marketplaces,omitempty"`
	Sandbox           *SandboxSettings             `json:"sandbox,omitempty"`
	Keybindings       []Keybinding                 `json:"keybindings,omitempty"`
	AllowedMCPServers []AllowedMCPServerEntry      `json:"allowedMcpServers,omitempty"`
	DeniedMCPServers  []DeniedMCPServerEntry       `json:"deniedMcpServers,omitempty"`
	Model             *string                      `json:"model,omitempty"`
	Verbose           bool                         `json:"verbose,omitempty"`
	FastMode          bool                         `json:"fastMode,omitempty"`
	AutoUpdates       *bool                        `json:"autoUpdates,omitempty"`
	Telemetry         *bool                        `json:"telemetry,omitempty"`
	CreatedAt         *time.Time                   `json:"createdAt,omitempty"`
	UpdatedAt         *time.Time                   `json:"updatedAt,omitempty"`
}
