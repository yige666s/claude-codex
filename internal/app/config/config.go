package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"claude-codex/internal/public/fsutil"
)

const (
	homeEnvVar                        = "CLAUDE_GO_HOME"
	CurrentSchemaVersion              = 3
	DefaultAnthropicAPIKeyPlaceholder = "YOUR_ANTHROPIC_API_KEY"
)

type MCPServerConfig struct {
	Name      string            `json:"name"`
	Transport string            `json:"transport"`
	Command   []string          `json:"command,omitempty"`
	URL       string            `json:"url,omitempty"`
	Headers   map[string]string `json:"headers,omitempty"`
}

type Config struct {
	SchemaVersion  int               `json:"schema_version"`
	Backend        string            `json:"backend"`
	Provider       string            `json:"provider,omitempty"` // LLM provider: anthropic, openai, qwen, gemini, vertex, custom
	Model          string            `json:"model"`
	PermissionMode string            `json:"permission_mode"`
	Theme          string            `json:"theme"`
	APIBaseURL     string            `json:"api_base_url"`
	APIKey         string            `json:"api_key,omitempty"`   // API key for provider
	APIToken       string            `json:"api_token,omitempty"` // Alternative to APIKey
	TimeoutSeconds int               `json:"timeout_seconds"`
	MaxTurns       int               `json:"max_turns"`
	SecretStore    string            `json:"secret_store"`
	Telemetry      TelemetryConfig   `json:"telemetry"`
	OAuth          OAuthConfig       `json:"oauth"`
	MCPServers     []MCPServerConfig `json:"mcp_servers,omitempty"`
	PluginDir      string            `json:"plugin_dir,omitempty"`
	BridgeSecret   string            `json:"bridge_secret,omitempty"`

	AppleTerminalSetupInProgress bool   `json:"appleTerminalSetupInProgress,omitempty"`
	AppleTerminalBackupPath      string `json:"appleTerminalBackupPath,omitempty"`
	ITerm2SetupInProgress        bool   `json:"iterm2SetupInProgress,omitempty"`
	ITerm2BackupPath             string `json:"iterm2BackupPath,omitempty"`
}

type TelemetryConfig struct {
	Enabled     bool   `json:"enabled"`
	Exporter    string `json:"exporter"`
	Endpoint    string `json:"endpoint,omitempty"`
	Insecure    bool   `json:"insecure,omitempty"`
	ServiceName string `json:"service_name,omitempty"`
}

type OAuthConfig struct {
	ClientID     string   `json:"client_id,omitempty"`
	ClientSecret string   `json:"client_secret,omitempty"`
	AuthURL      string   `json:"auth_url,omitempty"`
	TokenURL     string   `json:"token_url,omitempty"`
	Scopes       []string `json:"scopes,omitempty"`
	RedirectHost string   `json:"redirect_host,omitempty"`
	RedirectPort int      `json:"redirect_port,omitempty"`
}

type legacyConfig struct {
	Backend        string `json:"backend"`
	Model          string `json:"model"`
	PermissionMode string `json:"permission_mode"`
	Theme          string `json:"theme"`
	APIBaseURL     string `json:"api_base_url"`
	TimeoutSeconds int    `json:"timeout_seconds"`
	MaxTurns       int    `json:"max_turns"`
}

type LoadError struct {
	Path string
	Err  error
}

func (e *LoadError) Error() string {
	return fmt.Sprintf("load config %s: %v", e.Path, e.Err)
}

func (e *LoadError) Unwrap() error {
	return e.Err
}

func Default() Config {
	return Config{
		SchemaVersion:  CurrentSchemaVersion,
		Backend:        "anthropic",
		Provider:       "anthropic",
		Model:          "claude-sonnet-4-5",
		PermissionMode: "default",
		Theme:          "dark",
		APIBaseURL:     "https://api.anthropic.com",
		APIKey:         DefaultAnthropicAPIKeyPlaceholder,
		TimeoutSeconds: 600,
		MaxTurns:       0,
		SecretStore:    "auto",
		Telemetry: TelemetryConfig{
			Enabled:     false,
			Exporter:    "none",
			ServiceName: "claude-codex",
		},
		OAuth: OAuthConfig{
			Scopes:       []string{"openid", "profile"},
			RedirectHost: "127.0.0.1",
			RedirectPort: 0,
		},
		MCPServers: []MCPServerConfig{},
	}
}

func (cfg TelemetryConfig) ServiceNameOrDefault() string {
	if strings.TrimSpace(cfg.ServiceName) == "" {
		return "claude-codex"
	}
	return cfg.ServiceName
}

func (cfg OAuthConfig) IsConfigured() bool {
	return strings.TrimSpace(cfg.ClientID) != "" &&
		strings.TrimSpace(cfg.AuthURL) != "" &&
		strings.TrimSpace(cfg.TokenURL) != ""
}

func (cfg OAuthConfig) ListenAddress() string {
	host := cfg.RedirectHost
	if strings.TrimSpace(host) == "" {
		host = "127.0.0.1"
	}
	return fmt.Sprintf("%s:%d", host, cfg.RedirectPort)
}

func AppHome() (string, error) {
	if value := os.Getenv(homeEnvVar); value != "" {
		return value, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(home, ".claude-codex"), nil
}

func ConfigPath() (string, error) {
	home, err := AppHome()
	if err != nil {
		return "", err
	}

	return filepath.Join(home, "config.json"), nil
}

func Load() (Config, error) {
	defaults := Default()
	path, err := ConfigPath()
	if err != nil {
		return defaults, &LoadError{Path: "", Err: err}
	}

	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		cfg := defaults
		applyEnvOverrides(&cfg)
		return cfg, nil
	}
	if err != nil {
		return defaults, &LoadError{Path: path, Err: err}
	}

	cfg, migrated, err := decode(data)
	if err != nil {
		return defaults, &LoadError{Path: path, Err: err}
	}
	applyEnvOverrides(&cfg)
	if migrated {
		if err := Save(cfg); err != nil {
			return cfg, err
		}
	}

	return cfg, nil
}

func Save(cfg Config) error {
	cfg = normalize(cfg)

	path, err := ConfigPath()
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}

	return fsutil.WriteFileAtomic(path, data, 0o644)
}

func decode(data []byte) (Config, bool, error) {
	cfg := Default()
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return cfg, false, err
	}

	version := 0
	if value, ok := raw["schema_version"]; ok {
		_ = json.Unmarshal(value, &version)
	}

	switch version {
	case 0:
		var legacy legacyConfig
		if err := json.Unmarshal(data, &legacy); err != nil {
			return cfg, false, err
		}
		cfg.Backend = coalesceString(legacy.Backend, cfg.Backend)
		cfg.Model = coalesceString(legacy.Model, cfg.Model)
		cfg.PermissionMode = coalesceString(legacy.PermissionMode, cfg.PermissionMode)
		cfg.Theme = coalesceString(legacy.Theme, cfg.Theme)
		cfg.APIBaseURL = coalesceString(legacy.APIBaseURL, cfg.APIBaseURL)
		if legacy.TimeoutSeconds > 0 {
			cfg.TimeoutSeconds = legacy.TimeoutSeconds
		}
		if legacy.MaxTurns > 0 {
			cfg.MaxTurns = legacy.MaxTurns
		}
		return normalize(cfg), true, nil
	case 1, 2, CurrentSchemaVersion:
		if err := json.Unmarshal(data, &cfg); err != nil {
			return cfg, false, err
		}
		migrated := version != CurrentSchemaVersion
		return normalize(cfg), migrated, nil
	default:
		return cfg, false, fmt.Errorf("unsupported config schema version %d", version)
	}
}

func normalize(cfg Config) Config {
	defaults := Default()
	cfg.SchemaVersion = CurrentSchemaVersion
	cfg.Backend = coalesceString(cfg.Backend, defaults.Backend)
	cfg.Model = coalesceString(cfg.Model, defaults.Model)
	cfg.PermissionMode = coalesceString(cfg.PermissionMode, defaults.PermissionMode)
	cfg.Theme = coalesceString(cfg.Theme, defaults.Theme)
	cfg.APIBaseURL = coalesceString(cfg.APIBaseURL, defaults.APIBaseURL)
	if cfg.TimeoutSeconds <= 0 {
		cfg.TimeoutSeconds = defaults.TimeoutSeconds
	}
	if cfg.MaxTurns < 0 {
		cfg.MaxTurns = defaults.MaxTurns
	}
	cfg.SecretStore = strings.ToLower(strings.TrimSpace(coalesceString(cfg.SecretStore, defaults.SecretStore)))
	cfg.Telemetry.Exporter = coalesceString(cfg.Telemetry.Exporter, defaults.Telemetry.Exporter)
	cfg.Telemetry.ServiceName = cfg.Telemetry.ServiceNameOrDefault()
	if len(cfg.OAuth.Scopes) == 0 {
		cfg.OAuth.Scopes = append([]string{}, defaults.OAuth.Scopes...)
	}
	cfg.OAuth.RedirectHost = coalesceString(cfg.OAuth.RedirectHost, defaults.OAuth.RedirectHost)
	if cfg.MCPServers == nil {
		cfg.MCPServers = []MCPServerConfig{}
	}
	return cfg
}

func applyEnvOverrides(cfg *Config) {
	if value := os.Getenv("CLAUDE_GO_BACKEND"); value != "" {
		cfg.Backend = value
	}
	if value := os.Getenv("CLAUDE_GO_MODEL"); value != "" {
		cfg.Model = value
	}
	if value := os.Getenv("CLAUDE_GO_PERMISSION_MODE"); value != "" {
		cfg.PermissionMode = value
	}
	if value := os.Getenv("CLAUDE_GO_THEME"); value != "" {
		cfg.Theme = value
	}
	if value := os.Getenv("CLAUDE_GO_API_BASE_URL"); value != "" {
		cfg.APIBaseURL = value
	}
	if value := os.Getenv("CLAUDE_GO_TIMEOUT_SECONDS"); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 {
			cfg.TimeoutSeconds = parsed
		}
	}
	if value := os.Getenv("CLAUDE_GO_MAX_TURNS"); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed >= 0 {
			cfg.MaxTurns = parsed
		}
	}
	if value := os.Getenv("CLAUDE_GO_SECRET_STORE"); value != "" {
		cfg.SecretStore = value
	}
	if value := os.Getenv("CLAUDE_GO_BRIDGE_SECRET"); value != "" {
		cfg.BridgeSecret = value
	}
	if value := os.Getenv("CLAUDE_GO_PLUGIN_DIR"); value != "" {
		cfg.PluginDir = value
	}
	if value := os.Getenv("CLAUDE_GO_TELEMETRY_ENABLED"); value != "" {
		cfg.Telemetry.Enabled = value == "1" || strings.EqualFold(value, "true")
	}
	if value := os.Getenv("CLAUDE_GO_TELEMETRY_EXPORTER"); value != "" {
		cfg.Telemetry.Exporter = value
	}
	if value := os.Getenv("CLAUDE_GO_TELEMETRY_ENDPOINT"); value != "" {
		cfg.Telemetry.Endpoint = value
	}
	if value := os.Getenv("CLAUDE_GO_TELEMETRY_INSECURE"); value != "" {
		cfg.Telemetry.Insecure = value == "1" || strings.EqualFold(value, "true")
	}
	if value := os.Getenv("CLAUDE_GO_TELEMETRY_SERVICE"); value != "" {
		cfg.Telemetry.ServiceName = value
	}
	if value := os.Getenv("CLAUDE_GO_OAUTH_CLIENT_ID"); value != "" {
		cfg.OAuth.ClientID = value
	}
	if value := os.Getenv("CLAUDE_GO_OAUTH_CLIENT_SECRET"); value != "" {
		cfg.OAuth.ClientSecret = value
	}
	if value := os.Getenv("CLAUDE_GO_OAUTH_AUTH_URL"); value != "" {
		cfg.OAuth.AuthURL = value
	}
	if value := os.Getenv("CLAUDE_GO_OAUTH_TOKEN_URL"); value != "" {
		cfg.OAuth.TokenURL = value
	}
	if value := os.Getenv("CLAUDE_GO_OAUTH_SCOPES"); value != "" {
		cfg.OAuth.Scopes = splitCSV(value)
	}
	if value := os.Getenv("CLAUDE_GO_OAUTH_REDIRECT_HOST"); value != "" {
		cfg.OAuth.RedirectHost = value
	}
	if value := os.Getenv("CLAUDE_GO_OAUTH_REDIRECT_PORT"); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed >= 0 {
			cfg.OAuth.RedirectPort = parsed
		}
	}
	*cfg = normalize(*cfg)
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}

func coalesceString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func parseTelemetryExporters(value string) []string {
	parts := strings.Split(value, ",")
	exporters := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(strings.ToLower(part))
		if part == "" {
			continue
		}
		exporters = append(exporters, part)
	}
	return exporters
}

func IsPlaceholderAPIKey(value string) bool {
	return strings.TrimSpace(value) == DefaultAnthropicAPIKeyPlaceholder
}

// Validate checks if the configuration is valid
func (cfg *Config) Validate() error {
	if cfg.Backend == "" {
		return fmt.Errorf("backend cannot be empty")
	}
	if cfg.Model == "" {
		return fmt.Errorf("model cannot be empty")
	}
	if cfg.PermissionMode == "" {
		return fmt.Errorf("permission_mode cannot be empty")
	}
	validModes := map[string]bool{"default": true, "plan": true, "bypass": true, "auto": true}
	if !validModes[cfg.PermissionMode] {
		return fmt.Errorf("invalid permission_mode: %s (must be default, plan, bypass, or auto)", cfg.PermissionMode)
	}
	if cfg.TimeoutSeconds < 0 {
		return fmt.Errorf("timeout_seconds cannot be negative")
	}
	if cfg.MaxTurns < 0 {
		return fmt.Errorf("max_turns cannot be negative")
	}
	if cfg.Theme != "" && cfg.Theme != "light" && cfg.Theme != "dark" {
		return fmt.Errorf("invalid theme: %s (must be light or dark)", cfg.Theme)
	}
	if cfg.SecretStore != "" {
		validSecretStores := map[string]bool{"auto": true, "plaintext": true, "keychain": true}
		if !validSecretStores[cfg.SecretStore] {
			return fmt.Errorf("invalid secret_store: %s (must be auto, plaintext, or keychain)", cfg.SecretStore)
		}
	}

	// Validate telemetry config
	if cfg.Telemetry.Enabled {
		if cfg.Telemetry.Exporter == "" {
			return fmt.Errorf("telemetry.exporter is required when telemetry is enabled")
		}
		validExporters := map[string]bool{
			"otlp": true, "stdout": true, "jaeger": true,
			"jsonl": true, "perfetto": true, "bigquery": true,
		}
		for _, exporter := range parseTelemetryExporters(cfg.Telemetry.Exporter) {
			if !validExporters[exporter] {
				return fmt.Errorf("invalid telemetry.exporter: %s (must be comma-separated values from otlp, stdout, jaeger, jsonl, perfetto, bigquery)", cfg.Telemetry.Exporter)
			}
		}
	}

	// Validate OAuth config
	if cfg.OAuth.ClientID != "" || cfg.OAuth.AuthURL != "" || cfg.OAuth.TokenURL != "" {
		if cfg.OAuth.ClientID == "" {
			return fmt.Errorf("oauth.client_id is required when OAuth is configured")
		}
		if cfg.OAuth.AuthURL == "" {
			return fmt.Errorf("oauth.auth_url is required when OAuth is configured")
		}
		if cfg.OAuth.TokenURL == "" {
			return fmt.Errorf("oauth.token_url is required when OAuth is configured")
		}
	}

	return nil
}

// LoadWithWorkspace loads global config and merges with workspace-specific config
func LoadWithWorkspace(workspaceDir string) (Config, error) {
	// Load global config
	cfg, err := Load()
	if err != nil {
		return Config{}, err
	}

	// Try to load workspace config
	workspaceCfgPath := filepath.Join(workspaceDir, ".claude-codex", "config.json")
	if _, err := os.Stat(workspaceCfgPath); err == nil {
		workspaceCfg, err := loadFromPath(workspaceCfgPath)
		if err != nil {
			return Config{}, fmt.Errorf("load workspace config: %w", err)
		}

		// Merge workspace config into global config
		cfg = mergeConfigs(cfg, *workspaceCfg)
	}

	return cfg, nil
}

// mergeConfigs merges workspace config into base config (workspace takes precedence)
func mergeConfigs(base, workspace Config) Config {
	result := base

	if workspace.Backend != "" {
		result.Backend = workspace.Backend
	}
	if workspace.Model != "" {
		result.Model = workspace.Model
	}
	if workspace.PermissionMode != "" {
		result.PermissionMode = workspace.PermissionMode
	}
	if workspace.Theme != "" {
		result.Theme = workspace.Theme
	}
	if workspace.APIBaseURL != "" {
		result.APIBaseURL = workspace.APIBaseURL
	}
	if workspace.TimeoutSeconds > 0 {
		result.TimeoutSeconds = workspace.TimeoutSeconds
	}
	if workspace.MaxTurns > 0 {
		result.MaxTurns = workspace.MaxTurns
	}
	if workspace.SecretStore != "" {
		result.SecretStore = workspace.SecretStore
	}
	if workspace.PluginDir != "" {
		result.PluginDir = workspace.PluginDir
	}
	if workspace.BridgeSecret != "" {
		result.BridgeSecret = workspace.BridgeSecret
	}

	// Merge telemetry config
	if workspace.Telemetry.Exporter != "" {
		result.Telemetry = workspace.Telemetry
	}

	// Merge OAuth config
	if workspace.OAuth.ClientID != "" {
		result.OAuth = workspace.OAuth
	}

	// Merge MCP servers (workspace servers are added to global servers)
	if len(workspace.MCPServers) > 0 {
		result.MCPServers = append(result.MCPServers, workspace.MCPServers...)
	}

	return result
}

// loadFromPath loads config from a specific path
func loadFromPath(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}
