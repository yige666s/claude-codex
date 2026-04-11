package settings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSettingsFilePathForSource(t *testing.T) {
	workingDir := "/tmp/project"
	userHome := t.TempDir()
	t.Setenv("HOME", userHome)

	if got := SettingsFilePathForSource(SourceUser, workingDir); !strings.Contains(got, ".claude/settings.json") {
		t.Fatalf("unexpected user settings path: %s", got)
	}
	if got := SettingsFilePathForSource(SourceProject, workingDir); got != filepath.Join(workingDir, ".claude", "settings.json") {
		t.Fatalf("unexpected project settings path: %s", got)
	}
	if got := SettingsFilePathForSource(SourceLocal, workingDir); got != filepath.Join(workingDir, ".claude", "settings.local.json") {
		t.Fatalf("unexpected local settings path: %s", got)
	}
}

func TestFilterInvalidPermissionRules(t *testing.T) {
	input := map[string]any{
		"permissions": map[string]any{
			"allow": []any{"Read(foo.go)", 42, "Bash:"},
		},
	}
	filtered, warnings := FilterInvalidPermissionRules(input, "settings.json")
	doc := filtered.(map[string]any)
	rules := doc["permissions"].(map[string]any)["allow"].([]any)
	if len(rules) != 1 || rules[0] != "Read(foo.go)" {
		t.Fatalf("unexpected filtered rules: %#v", rules)
	}
	if len(warnings) != 2 {
		t.Fatalf("expected 2 warnings, got %#v", warnings)
	}
}

func TestPluginOnlyPolicy(t *testing.T) {
	policy := Document{"strictPluginOnlyCustomization": []any{"skills", "hooks"}}
	if !IsRestrictedToPluginOnly(policy, "skills") {
		t.Fatal("expected skills to be restricted")
	}
	if IsRestrictedToPluginOnly(policy, "mcp") {
		t.Fatal("expected mcp not to be restricted")
	}
	if !IsSourceAdminTrusted("plugin") || !IsSourceAdminTrusted("bundled") || IsSourceAdminTrusted("userSettings") {
		t.Fatal("unexpected source trust evaluation")
	}
}

func TestToolValidationConfig(t *testing.T) {
	if !IsFilePatternTool("Read") || IsFilePatternTool("WebSearch") {
		t.Fatal("unexpected file pattern tool detection")
	}
	if !IsBashPrefixTool("Bash") || IsBashPrefixTool("Read") {
		t.Fatal("unexpected bash prefix tool detection")
	}
	if result := CustomToolValidation("WebFetch", "https://example.com"); result.Valid {
		t.Fatalf("expected webfetch URL validation failure: %+v", result)
	}
}

func TestGenerateSettingsJSONSchema(t *testing.T) {
	schema := GenerateSettingsJSONSchema()
	if !strings.Contains(schema, "strictPluginOnlyCustomization") || !strings.Contains(schema, "permissions") {
		t.Fatalf("unexpected schema output: %s", schema)
	}
}

func TestLoadManagedFileSettingsMergesBaseAndDropIns(t *testing.T) {
	root := t.TempDir()
	t.Setenv(managedSettingsEnvOverride, root)

	if err := os.WriteFile(ManagedSettingsFilePath(), []byte(`{"verbose":true,"env":{"A":"1"}}`), 0o644); err != nil {
		t.Fatalf("write base managed settings: %v", err)
	}
	if err := os.MkdirAll(ManagedSettingsDropInDir(), 0o755); err != nil {
		t.Fatalf("mkdir dropin: %v", err)
	}
	if err := os.WriteFile(filepath.Join(ManagedSettingsDropInDir(), "20-extra.json"), []byte(`{"env":{"B":"2"},"fastMode":true}`), 0o644); err != nil {
		t.Fatalf("write dropin: %v", err)
	}

	result := LoadManagedFileSettings()
	env := result.Settings["env"].(Document)
	if env["A"] != "1" || env["B"] != "2" || result.Settings["fastMode"] != true {
		t.Fatalf("unexpected managed settings merge: %#v", result.Settings)
	}
}

func TestChangeDetectorEmitsChangeEvents(t *testing.T) {
	workingDir := t.TempDir()
	settingsPath := filepath.Join(workingDir, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		t.Fatalf("mkdir settings dir: %v", err)
	}
	if err := os.WriteFile(settingsPath, []byte(`{"verbose":true}`), 0o644); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	detector := NewChangeDetector(workingDir, 50*time.Millisecond)
	events := detector.Subscribe()
	detector.Start()
	defer detector.Stop()

	time.Sleep(100 * time.Millisecond)
	if err := os.WriteFile(settingsPath, []byte(`{"verbose":false}`), 0o644); err != nil {
		t.Fatalf("update settings: %v", err)
	}

	timeout := time.After(500 * time.Millisecond)
	for {
		select {
		case event, ok := <-events:
			if !ok {
				t.Fatal("change detector channel closed before event")
			}
			if event.Source == SourceProject && event.Type == "change" {
				return
			}
		case <-timeout:
			t.Fatal("expected change event")
		}
	}
}
