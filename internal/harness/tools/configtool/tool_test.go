package configtool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	appconfig "claude-codex/internal/app/config"
	appsettings "claude-codex/internal/app/settings"
	toolkit "claude-codex/internal/harness/tools"
)

func newTestTool(t *testing.T) (toolkit.Tool, string) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("CLAUDE_GO_HOME", filepath.Join(root, "claude-codex-home"))
	t.Setenv("CLAUDE_CONFIG_HOME", filepath.Join(root, "claude-home"))
	return NewTool(root), root
}

func executeConfigTool(t *testing.T, tool toolkit.Tool, payload string) output {
	t.Helper()
	result, err := tool.Execute(context.Background(), json.RawMessage(payload))
	if err != nil {
		t.Fatalf("Execute(%s) error = %v", payload, err)
	}
	var out output
	if err := json.Unmarshal([]byte(result.Output), &out); err != nil {
		t.Fatalf("unmarshal output %q: %v", result.Output, err)
	}
	return out
}

func TestConfigToolGetsGlobalConfigValue(t *testing.T) {
	tool, _ := newTestTool(t)
	if err := appconfig.Save(appconfig.Config{
		SchemaVersion:  appconfig.CurrentSchemaVersion,
		Backend:        "anthropic",
		Provider:       "anthropic",
		Model:          "claude-opus-test",
		PermissionMode: "default",
		Theme:          "light",
		APIBaseURL:     "https://api.anthropic.com",
		APIKey:         appconfig.DefaultAnthropicAPIKeyPlaceholder,
		TimeoutSeconds: 600,
		MaxTurns:       0,
		SecretStore:    "auto",
		Telemetry:      appconfig.Default().Telemetry,
		OAuth:          appconfig.Default().OAuth,
	}); err != nil {
		t.Fatalf("save config: %v", err)
	}

	out := executeConfigTool(t, tool, `{"setting":"theme"}`)
	if !out.Success || out.Operation != "get" || out.Setting != "theme" || out.Value != "light" {
		t.Fatalf("unexpected output: %#v", out)
	}
}

func TestConfigToolSetsGlobalConfigValue(t *testing.T) {
	tool, _ := newTestTool(t)
	if err := appconfig.Save(appconfig.Default()); err != nil {
		t.Fatalf("save default config: %v", err)
	}

	out := executeConfigTool(t, tool, `{"setting":"theme","value":"light"}`)
	if !out.Success || out.Operation != "set" || out.Setting != "theme" || out.NewValue != "light" {
		t.Fatalf("unexpected output: %#v", out)
	}

	cfg, err := appconfig.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.Theme != "light" {
		t.Fatalf("expected theme light, got %q", cfg.Theme)
	}
}

func TestConfigToolSetsUserSetting(t *testing.T) {
	tool, root := newTestTool(t)

	out := executeConfigTool(t, tool, `{"setting":"permissions.defaultMode","value":"plan"}`)
	if !out.Success || out.Operation != "set" || out.Setting != "permissions.defaultMode" || out.NewValue != "plan" {
		t.Fatalf("unexpected output: %#v", out)
	}

	path := appsettings.SettingsFilePathForSource(appsettings.SourceUser, root)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read user settings: %v", err)
	}
	if string(data) == "" {
		t.Fatalf("expected user settings file to be written")
	}

	read := executeConfigTool(t, tool, `{"setting":"permissions.defaultMode"}`)
	if !read.Success || read.Operation != "get" || read.Value != "plan" {
		t.Fatalf("unexpected read output: %#v", read)
	}
}

func TestConfigToolRejectsUnknownSetting(t *testing.T) {
	tool, _ := newTestTool(t)

	out := executeConfigTool(t, tool, `{"setting":"does.not.exist"}`)
	if out.Success || out.Error == "" {
		t.Fatalf("expected structured unknown-setting failure, got %#v", out)
	}
}

func TestConfigToolRejectsInvalidOption(t *testing.T) {
	tool, _ := newTestTool(t)

	out := executeConfigTool(t, tool, `{"setting":"theme","value":"blue"}`)
	if out.Success || out.Operation != "set" || out.Error == "" {
		t.Fatalf("expected structured invalid-option failure, got %#v", out)
	}
}
