package schemas

import (
	"testing"
)

func TestValidatePermissionRule(t *testing.T) {
	tests := []struct {
		name    string
		rule    string
		wantErr bool
	}{
		{
			name:    "empty rule",
			rule:    "",
			wantErr: true,
		},
		{
			name:    "simple tool name",
			rule:    "Read",
			wantErr: false,
		},
		{
			name:    "tool with file pattern",
			rule:    "Read(*.go)",
			wantErr: false,
		},
		{
			name:    "bash pattern",
			rule:    "Bash:ls",
			wantErr: false,
		},
		{
			name:    "bash wildcard",
			rule:    "Bash:*",
			wantErr: false,
		},
		{
			name:    "mcp pattern",
			rule:    "mcp:server:tool",
			wantErr: false,
		},
		{
			name:    "mcp with wildcard",
			rule:    "mcp:server:*",
			wantErr: true, // MCP doesn't support wildcards
		},
		{
			name:    "unmatched opening parenthesis",
			rule:    "Read(*.go",
			wantErr: true,
		},
		{
			name:    "unmatched closing parenthesis",
			rule:    "Read*.go)",
			wantErr: true,
		},
		{
			name:    "empty parentheses",
			rule:    "Read()",
			wantErr: true,
		},
		{
			name:    "lowercase tool name",
			rule:    "read",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePermissionRule(tt.rule)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidatePermissionRule() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateParentheses(t *testing.T) {
	tests := []struct {
		name    string
		rule    string
		wantErr bool
	}{
		{
			name:    "balanced",
			rule:    "Read(*.go)",
			wantErr: false,
		},
		{
			name:    "nested balanced",
			rule:    "Tool((nested))",
			wantErr: false,
		},
		{
			name:    "escaped parenthesis",
			rule:    "Tool\\(escaped\\)",
			wantErr: false,
		},
		{
			name:    "unmatched opening",
			rule:    "Tool(",
			wantErr: true,
		},
		{
			name:    "unmatched closing",
			rule:    "Tool)",
			wantErr: true,
		},
		{
			name:    "multiple unmatched",
			rule:    "Tool(((",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateParentheses(tt.rule)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateParentheses() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateMCPPattern(t *testing.T) {
	tests := []struct {
		name    string
		rule    string
		wantErr bool
	}{
		{
			name:    "server only",
			rule:    "mcp:myserver",
			wantErr: false,
		},
		{
			name:    "server and tool",
			rule:    "mcp:myserver:mytool",
			wantErr: false,
		},
		{
			name:    "empty server",
			rule:    "mcp::tool",
			wantErr: true,
		},
		{
			name:    "empty tool",
			rule:    "mcp:server:",
			wantErr: true,
		},
		{
			name:    "with wildcard",
			rule:    "mcp:server:*",
			wantErr: true,
		},
		{
			name:    "too many colons",
			rule:    "mcp:server:tool:extra",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateMCPPattern(tt.rule)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateMCPPattern() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateBashPattern(t *testing.T) {
	tests := []struct {
		name    string
		rule    string
		wantErr bool
	}{
		{
			name:    "bash only",
			rule:    "Bash",
			wantErr: false,
		},
		{
			name:    "bash wildcard",
			rule:    "Bash:*",
			wantErr: false,
		},
		{
			name:    "bash command",
			rule:    "Bash:ls",
			wantErr: false,
		},
		{
			name:    "bash with args",
			rule:    "Bash:ls -la",
			wantErr: false,
		},
		{
			name:    "empty command",
			rule:    "Bash:",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateBashPattern(tt.rule)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateBashPattern() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateSettings(t *testing.T) {
	tests := []struct {
		name       string
		settings   *Settings
		wantErrors int
	}{
		{
			name: "valid settings",
			settings: &Settings{
				Permissions: &Permissions{
					Allow: []PermissionRule{"Read", "Write"},
					Deny:  []PermissionRule{"Bash:rm"},
				},
			},
			wantErrors: 0,
		},
		{
			name: "invalid permission rule",
			settings: &Settings{
				Permissions: &Permissions{
					Allow: []PermissionRule{"read"}, // lowercase
				},
			},
			wantErrors: 1,
		},
		{
			name: "invalid permission mode",
			settings: &Settings{
				Permissions: &Permissions{
					DefaultMode: "invalid",
				},
			},
			wantErrors: 1,
		},
		{
			name: "duplicate keybindings",
			settings: &Settings{
				Keybindings: []Keybinding{
					{Context: KeybindingContextGlobal, Key: "ctrl+c", Action: "copy"},
					{Context: KeybindingContextGlobal, Key: "ctrl+c", Action: "cancel"},
				},
			},
			wantErrors: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ValidateSettings(tt.settings)
			if len(result.Errors) != tt.wantErrors {
				t.Errorf("ValidateSettings() got %d errors, want %d", len(result.Errors), tt.wantErrors)
				for _, err := range result.Errors {
					t.Logf("  Error: %s", err.Error())
				}
			}
		})
	}
}

func TestFilterInvalidPermissionRules(t *testing.T) {
	rules := []PermissionRule{
		"Read",           // valid
		"read",           // invalid (lowercase)
		"Write(*.go)",    // valid
		"Write(",         // invalid (unmatched)
		"Bash:ls",        // valid
		"mcp:server:*",   // invalid (wildcard)
	}

	filtered := FilterInvalidPermissionRules(rules)

	if len(filtered) != 3 {
		t.Errorf("FilterInvalidPermissionRules() got %d rules, want 3", len(filtered))
	}

	expected := []PermissionRule{"Read", "Write(*.go)", "Bash:ls"}
	for i, rule := range filtered {
		if rule != expected[i] {
			t.Errorf("FilterInvalidPermissionRules()[%d] = %s, want %s", i, rule, expected[i])
		}
	}
}

func TestNormalizePermissionRule(t *testing.T) {
	tests := []struct {
		name string
		rule string
		want string
	}{
		{
			name: "trim whitespace",
			rule: "  Read  ",
			want: "Read",
		},
		{
			name: "normalize spaces",
			rule: "Read   (*.go)",
			want: "Read (*.go)",
		},
		{
			name: "already normalized",
			rule: "Read(*.go)",
			want: "Read(*.go)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizePermissionRule(tt.rule)
			if got != tt.want {
				t.Errorf("NormalizePermissionRule() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestHookValidation(t *testing.T) {
	tests := []struct {
		name    string
		hook    Hook
		wantErr bool
	}{
		{
			name: "valid bash hook",
			hook: &BashCommandHook{
				BaseHook: BaseHook{Type: HookTypeBash},
				Command:  "echo hello",
			},
			wantErr: false,
		},
		{
			name: "bash hook without command",
			hook: &BashCommandHook{
				BaseHook: BaseHook{Type: HookTypeBash},
			},
			wantErr: true,
		},
		{
			name: "valid prompt hook",
			hook: &PromptHook{
				BaseHook: BaseHook{Type: HookTypePrompt},
				Prompt:   "Analyze this",
			},
			wantErr: false,
		},
		{
			name: "prompt hook without prompt",
			hook: &PromptHook{
				BaseHook: BaseHook{Type: HookTypePrompt},
			},
			wantErr: true,
		},
		{
			name: "valid http hook",
			hook: &HTTPHook{
				BaseHook: BaseHook{Type: HookTypeHTTP},
				URL:      "https://example.com/webhook",
			},
			wantErr: false,
		},
		{
			name: "http hook without url",
			hook: &HTTPHook{
				BaseHook: BaseHook{Type: HookTypeHTTP},
			},
			wantErr: true,
		},
		{
			name: "valid agent hook",
			hook: &AgentHook{
				BaseHook: BaseHook{Type: HookTypeAgent},
				Prompt:   "Verify this",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.hook.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Hook.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestMCPServerConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		config  MCPServerConfig
		wantErr bool
	}{
		{
			name: "valid stdio config",
			config: &MCPStdioServerConfig{
				Command: "node",
				Args:    []string{"server.js"},
			},
			wantErr: false,
		},
		{
			name:    "stdio config without command",
			config:  &MCPStdioServerConfig{},
			wantErr: true,
		},
		{
			name: "valid sse config",
			config: &MCPSSEServerConfig{
				URL: "https://example.com/sse",
			},
			wantErr: false,
		},
		{
			name:    "sse config without url",
			config:  &MCPSSEServerConfig{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("MCPServerConfig.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
