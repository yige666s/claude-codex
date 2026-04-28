package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"claude-codex/internal/app/config"
	appsettings "claude-codex/internal/app/settings"
	"claude-codex/internal/harness/permissions"
)

func TestLoadPermissionToolContextFromSettings(t *testing.T) {
	workingDir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_HOME", filepath.Join(t.TempDir(), ".claude"))
	writeJSON(t, filepath.Join(workingDir, ".claude", "settings.json"), map[string]any{
		"permissions": map[string]any{
			"allow": []any{"Read", "Bash(git status)"},
			"deny":  []any{"Bash(rm:*)"},
			"ask":   []any{"Write"},
		},
	})
	writeJSON(t, filepath.Join(workingDir, ".claude", "settings.local.json"), map[string]any{
		"permissions": map[string]any{
			"allow": []any{"Bash(npm test)"},
		},
	})

	ctx := loadPermissionToolContext(workingDir, permissions.ModeDefault)
	if ctx.PermissionMode != permissions.ModeDefault {
		t.Fatalf("unexpected mode: %q", ctx.PermissionMode)
	}
	if got := ctx.AlwaysAllowRules[permissions.SourceProjectSettings]; len(got) != 2 || got[0] != "Read" {
		t.Fatalf("unexpected project allow rules: %#v", got)
	}
	if got := ctx.AlwaysAllowRules[permissions.SourceLocalSettings]; len(got) != 1 || got[0] != "Bash(npm test)" {
		t.Fatalf("unexpected local allow rules: %#v", got)
	}
	if got := ctx.AlwaysDenyRules[permissions.SourceProjectSettings]; len(got) != 1 || got[0] != "Bash(rm:*)" {
		t.Fatalf("unexpected deny rules: %#v", got)
	}
	if got := ctx.AlwaysAskRules[permissions.SourceProjectSettings]; len(got) != 1 || got[0] != "Write" {
		t.Fatalf("unexpected ask rules: %#v", got)
	}
}

func TestPersistPermissionUpdatesWritesSettings(t *testing.T) {
	workingDir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_HOME", filepath.Join(t.TempDir(), ".claude"))
	writeJSON(t, filepath.Join(workingDir, ".claude", "settings.local.json"), map[string]any{
		"permissions": map[string]any{
			"allow": []any{"Read"},
		},
	})

	err := persistPermissionUpdates(workingDir)(context.Background(), []permissions.PermissionUpdate{
		{
			Type:        permissions.UpdateAddRules,
			Destination: permissions.SourceLocalSettings,
			Behavior:    permissions.BehaviorAllow,
			Rules: []permissions.RuleValue{
				{ToolName: "Read"},
				{ToolName: "Bash", RuleContent: "git status"},
			},
		},
		{
			Type:        permissions.UpdateAddRules,
			Destination: permissions.SourceProjectSettings,
			Behavior:    permissions.BehaviorDeny,
			Rules:       []permissions.RuleValue{{ToolName: "Bash", RuleContent: "rm:*"}},
		},
	})
	if err != nil {
		t.Fatalf("persist updates: %v", err)
	}

	local := appsettings.LoadSettingsForSource(appsettings.SourceLocal, workingDir)
	localRules := local.Settings["permissions"].(map[string]any)["allow"].([]any)
	if len(localRules) != 2 || localRules[0] != "Read" || localRules[1] != "Bash(git status)" {
		t.Fatalf("unexpected local allow rules: %#v", localRules)
	}

	project := appsettings.LoadSettingsForSource(appsettings.SourceProject, workingDir)
	projectRules := project.Settings["permissions"].(map[string]any)["deny"].([]any)
	if len(projectRules) != 1 || projectRules[0] != "Bash(rm:*)" {
		t.Fatalf("unexpected project deny rules: %#v", projectRules)
	}
}

func TestNewPermissionRuntimeOptionsInjectsAutoClassifier(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	options, err := newPermissionRuntimeOptions(config.Config{
		Backend:        "anthropic",
		Model:          "claude-test",
		APIBaseURL:     "https://api.anthropic.com",
		APIKey:         config.DefaultAnthropicAPIKeyPlaceholder,
		TimeoutSeconds: 1,
	}, permissions.ModeAuto, t.TempDir())
	if err != nil {
		t.Fatalf("runtime options: %v", err)
	}
	checker := permissions.NewChecker(permissions.ModeAuto, nil, nil, options...)
	if checker == nil {
		t.Fatal("expected checker to be constructed with runtime options")
	}
}

func writeJSON(t *testing.T, path string, value any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatalf("marshal %s: %v", path, err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
