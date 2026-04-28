package cli

import (
	"bytes"
	"os"
	"reflect"
	"strings"
	"testing"

	"claude-codex/internal/app/config"
	"claude-codex/internal/harness/skills"
)

func TestSetConfigValue_ExtendedKeys(t *testing.T) {
	tests := []struct {
		name   string
		key    string
		value  string
		assert func(t *testing.T, cfg *config.Config)
	}{
		{
			name:  "secret store",
			key:   "secret_store",
			value: "keychain",
			assert: func(t *testing.T, cfg *config.Config) {
				t.Helper()
				if cfg.SecretStore != "keychain" {
					t.Fatalf("expected secret_store to be updated, got %q", cfg.SecretStore)
				}
			},
		},
		{
			name:  "plugin dir",
			key:   "plugin_dir",
			value: "/tmp/plugins",
			assert: func(t *testing.T, cfg *config.Config) {
				t.Helper()
				if cfg.PluginDir != "/tmp/plugins" {
					t.Fatalf("expected plugin_dir to be updated, got %q", cfg.PluginDir)
				}
			},
		},
		{
			name:  "bridge secret",
			key:   "bridge_secret",
			value: "bridge-secret",
			assert: func(t *testing.T, cfg *config.Config) {
				t.Helper()
				if cfg.BridgeSecret != "bridge-secret" {
					t.Fatalf("expected bridge_secret to be updated, got %q", cfg.BridgeSecret)
				}
			},
		},
		{
			name:  "telemetry insecure",
			key:   "telemetry.insecure",
			value: "true",
			assert: func(t *testing.T, cfg *config.Config) {
				t.Helper()
				if !cfg.Telemetry.Insecure {
					t.Fatal("expected telemetry.insecure to be true")
				}
			},
		},
		{
			name:  "oauth scopes",
			key:   "oauth.scopes",
			value: "openid, profile , email",
			assert: func(t *testing.T, cfg *config.Config) {
				t.Helper()
				want := []string{"openid", "profile", "email"}
				if !reflect.DeepEqual(cfg.OAuth.Scopes, want) {
					t.Fatalf("expected oauth.scopes %v, got %v", want, cfg.OAuth.Scopes)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.Default()
			if err := setConfigValue(&cfg, tt.key, tt.value); err != nil {
				t.Fatalf("setConfigValue(%q): %v", tt.key, err)
			}
			tt.assert(t, &cfg)
		})
	}
}

func TestSplitAndTrimCSV(t *testing.T) {
	got := splitAndTrimCSV(" a, ,b,c ,, d ")
	want := []string{"a", "b", "c", "d"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
}

func TestParseMCPServerArgs_RejectsIncompleteFlags(t *testing.T) {
	tests := [][]string{
		{"--url"},
		{"--"},
	}

	for _, args := range tests {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			_, err := parseMCPServerArgs("demo", args)
			if err == nil {
				t.Fatal("expected error for incomplete MCP args")
			}
			if !strings.Contains(err.Error(), "usage: /mcp add demo") {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestHandleModelCommand(t *testing.T) {
	t.Setenv("CLAUDE_GO_HOME", t.TempDir())

	cfg := config.Default()
	var out bytes.Buffer
	sc := slashContext{
		cfg:     &cfg,
		streams: IO{Out: &out},
	}

	if err := handleModelCommand(nil, sc); err != nil {
		t.Fatalf("show current model: %v", err)
	}
	if !strings.Contains(out.String(), cfg.Model) {
		t.Fatalf("expected current model in output, got %q", out.String())
	}

	out.Reset()
	if err := handleModelCommand([]string{"claude-opus-test"}, sc); err != nil {
		t.Fatalf("set model: %v", err)
	}
	if cfg.Model != "claude-opus-test" {
		t.Fatalf("expected config model to change, got %q", cfg.Model)
	}
	if !strings.Contains(out.String(), "updated model to claude-opus-test") {
		t.Fatalf("unexpected set output: %q", out.String())
	}
}

func TestHandleModeCommand(t *testing.T) {
	t.Setenv("CLAUDE_GO_HOME", t.TempDir())

	cfg := config.Default()
	var out bytes.Buffer
	sc := slashContext{
		cfg:     &cfg,
		streams: IO{Out: &out},
	}

	if err := handleModeCommand(nil, sc); err != nil {
		t.Fatalf("show current mode: %v", err)
	}
	if !strings.Contains(out.String(), "current mode: "+cfg.PermissionMode) {
		t.Fatalf("expected current mode in output, got %q", out.String())
	}

	out.Reset()
	if err := handleModeCommand([]string{"plan"}, sc); err != nil {
		t.Fatalf("set mode: %v", err)
	}
	if cfg.PermissionMode != "plan" {
		t.Fatalf("expected permission mode to change, got %q", cfg.PermissionMode)
	}
	if !strings.Contains(out.String(), "updated mode to plan") {
		t.Fatalf("unexpected set output: %q", out.String())
	}

	out.Reset()
	if err := handleModeCommand([]string{"bogus"}, sc); err == nil {
		t.Fatal("expected invalid mode to fail")
	}
}

func TestHandleConfigSettingsCommandSetAndGet(t *testing.T) {
	home := t.TempDir()
	workDir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_HOME", home)

	cfg := config.Default()
	var out bytes.Buffer
	sc := slashContext{
		cfg:            &cfg,
		defaultWorkDir: workDir,
		streams:        IO{Out: &out},
	}

	if err := handleConfigCommand([]string{"settings", "set", "permissions.defaultMode", "plan"}, sc); err != nil {
		t.Fatalf("set settings key: %v", err)
	}
	if !strings.Contains(out.String(), "updated settings permissions.defaultMode") {
		t.Fatalf("unexpected set output: %q", out.String())
	}

	out.Reset()
	if err := handleConfigCommand([]string{"settings", "get", "permissions.defaultMode"}, sc); err != nil {
		t.Fatalf("get settings key: %v", err)
	}
	if !strings.Contains(out.String(), `permissions.defaultMode = "plan"`) {
		t.Fatalf("unexpected get output: %q", out.String())
	}
}

func TestHandleConfigSettingsCommandRejectsInvalidOption(t *testing.T) {
	cfg := config.Default()
	var out bytes.Buffer
	sc := slashContext{
		cfg:            &cfg,
		defaultWorkDir: t.TempDir(),
		streams:        IO{Out: &out},
	}

	err := handleConfigCommand([]string{"settings", "set", "permissions.defaultMode", "invalid"}, sc)
	if err == nil || !strings.Contains(err.Error(), "options:") {
		t.Fatalf("expected invalid option error, got %v", err)
	}
}

func TestListCommandsForHelpIncludesSkills(t *testing.T) {
	manager := skills.NewSkillManager()
	dir := t.TempDir()
	skillDir := dir + "/demo"
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir skill dir: %v", err)
	}
	content := `---
name: Demo Skill
description: Demo skill
---

Demo content
`
	if err := os.WriteFile(skillDir+"/SKILL.md", []byte(content), 0o644); err != nil {
		t.Fatalf("write skill: %v", err)
	}
	if err := manager.LoadSkillsFromDirectory(dir, skills.SourceFile); err != nil {
		t.Fatalf("load skills: %v", err)
	}

	commands := listCommandsForHelp(slashContext{
		skillManager:   manager,
		defaultWorkDir: t.TempDir(),
	})

	names := make(map[string]bool, len(commands))
	for _, cmd := range commands {
		names[cmd.Name] = true
	}
	if !names["/skills"] {
		t.Fatal("expected /skills in help command list")
	}
	if !names["/mode"] {
		t.Fatal("expected /mode in help command list")
	}
	if !names["/demo"] {
		t.Fatal("expected dynamically loaded skill command in help command list")
	}
}
