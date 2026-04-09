package types

import "encoding/json"

// Command represents a slash command or CLI command.
type Command struct {
	Name        string   `json:"name"`
	Aliases     []string `json:"aliases,omitempty"`
	Description string   `json:"description"`
	Usage       string   `json:"usage,omitempty"`
	Category    string   `json:"category,omitempty"`
	Hidden      bool     `json:"hidden,omitempty"`
}

// ThinkingConfig configures extended thinking behavior.
type ThinkingConfig struct {
	Enabled      bool   `json:"enabled"`
	Type         string `json:"type"`          // "enabled", "disabled"
	BudgetTokens int    `json:"budget_tokens,omitempty"`
}

// MCPServerConnection represents a connection to an MCP server.
type MCPServerConnection struct {
	Name        string                 `json:"name"`
	Command     string                 `json:"command"`
	Args        []string               `json:"args,omitempty"`
	Env         map[string]string      `json:"env,omitempty"`
	Enabled     bool                   `json:"enabled"`
	Config      map[string]interface{} `json:"config,omitempty"`
	Transport   string                 `json:"transport,omitempty"` // "stdio", "sse"
	URL         string                 `json:"url,omitempty"`       // For SSE transport
}

// ServerResource represents a resource provided by an MCP server.
type ServerResource struct {
	URI         string                 `json:"uri"`
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	MimeType    string                 `json:"mime_type,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// AgentDefinition defines an agent template.
type AgentDefinition struct {
	AgentType    string   `json:"agent_type"`
	WhenToUse    string   `json:"when_to_use"`
	Tools        []string `json:"tools"`        // Tool names, or ["*"] for all tools
	MaxTurns     int      `json:"max_turns"`
	Model        string   `json:"model"`        // "inherit", "sonnet", "opus", "haiku"
	Permission   string   `json:"permission"`   // "default", "bubble", "allow", "deny"
	Source       string   `json:"source"`       // "built-in", "plugin", "user", "policySettings"
	BaseDir      string   `json:"base_dir,omitempty"`
	SystemPrompt string   `json:"system_prompt,omitempty"`
	MCPServers   []string `json:"mcp_servers,omitempty"`
	Skills       []string `json:"skills,omitempty"`
}

// AgentDefinitionsResult contains the result of loading agent definitions.
type AgentDefinitionsResult struct {
	Agents []AgentDefinition `json:"agents"`
	Errors []string          `json:"errors,omitempty"`
}

// QuerySource indicates where a query originated from.
type QuerySource string

const (
	QuerySourceCLI       QuerySource = "cli"
	QuerySourceTUI       QuerySource = "tui"
	QuerySourceSDK       QuerySource = "sdk"
	QuerySourceAgent     QuerySource = "agent"
	QuerySourceServer    QuerySource = "server"
	QuerySourceWebSocket QuerySource = "websocket"
)

// ModelConfig represents model configuration.
type ModelConfig struct {
	Provider    string  `json:"provider"`     // "anthropic", "openai", "gemini", "bedrock", "vertex"
	Model       string  `json:"model"`
	APIKey      string  `json:"api_key,omitempty"`
	BaseURL     string  `json:"base_url,omitempty"`
	MaxTokens   int     `json:"max_tokens,omitempty"`
	Temperature float64 `json:"temperature,omitempty"`
	TopP        float64 `json:"top_p,omitempty"`
	Timeout     int     `json:"timeout_seconds,omitempty"`
}

// ToolConfig represents tool configuration.
type ToolConfig struct {
	Name        string                 `json:"name"`
	Enabled     bool                   `json:"enabled"`
	Config      map[string]interface{} `json:"config,omitempty"`
	Permissions string                 `json:"permissions,omitempty"` // "read", "write", "execute"
}

// SessionConfig represents session-level configuration.
type SessionConfig struct {
	WorkingDir         string                 `json:"working_dir"`
	PermissionMode     string                 `json:"permission_mode"` // "default", "plan", "bypass", "auto"
	MaxTurns           int                    `json:"max_turns,omitempty"`
	MaxBudgetUSD       *float64               `json:"max_budget_usd,omitempty"`
	Model              string                 `json:"model,omitempty"`
	ThinkingConfig     *ThinkingConfig        `json:"thinking_config,omitempty"`
	Tools              []ToolConfig           `json:"tools,omitempty"`
	MCPServers         []MCPServerConnection  `json:"mcp_servers,omitempty"`
	Agents             []AgentDefinition      `json:"agents,omitempty"`
	CustomSystemPrompt string                 `json:"custom_system_prompt,omitempty"`
	Metadata           map[string]interface{} `json:"metadata,omitempty"`
}

// GlobalConfig represents global application configuration.
type GlobalConfig struct {
	DefaultModel       string                `json:"default_model"`
	DefaultProvider    string                `json:"default_provider"`
	PermissionMode     string                `json:"permission_mode"`
	MaxTurns           int                   `json:"max_turns"`
	Theme              string                `json:"theme,omitempty"`
	MCPServers         []MCPServerConnection `json:"mcp_servers,omitempty"`
	Agents             []AgentDefinition     `json:"agents,omitempty"`
	AutoUpdate         bool                  `json:"auto_update"`
	TelemetryEnabled   bool                  `json:"telemetry_enabled"`
	WorkingDirectories []string              `json:"working_directories,omitempty"`
}

// ParseThinkingConfig parses thinking configuration from various formats.
func ParseThinkingConfig(data interface{}) (*ThinkingConfig, error) {
	if data == nil {
		return nil, nil
	}

	// Handle boolean
	if enabled, ok := data.(bool); ok {
		if enabled {
			return &ThinkingConfig{
				Enabled: true,
				Type:    "enabled",
			}, nil
		}
		return &ThinkingConfig{
			Enabled: false,
			Type:    "disabled",
		}, nil
	}

	// Handle object
	if obj, ok := data.(map[string]interface{}); ok {
		config := &ThinkingConfig{}
		if enabled, ok := obj["enabled"].(bool); ok {
			config.Enabled = enabled
		}
		if typ, ok := obj["type"].(string); ok {
			config.Type = typ
		}
		if budget, ok := obj["budget_tokens"].(float64); ok {
			config.BudgetTokens = int(budget)
		}
		return config, nil
	}

	// Try JSON unmarshal as fallback
	jsonData, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}

	var config ThinkingConfig
	if err := json.Unmarshal(jsonData, &config); err != nil {
		return nil, err
	}

	return &config, nil
}

// NewDefaultThinkingConfig creates a default thinking configuration.
func NewDefaultThinkingConfig() *ThinkingConfig {
	return &ThinkingConfig{
		Enabled: false,
		Type:    "disabled",
	}
}
