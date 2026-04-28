package settings

import (
	"os"
	"path/filepath"
	"reflect"
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
	for _, want := range []string{"strictPluginOnlyCustomization", "permissions", "allowedMcpServers", "apiKeyHelper", "pluginConfigs"} {
		if !strings.Contains(schema, want) {
			t.Fatalf("expected generated schema to contain %q, got: %s", want, schema)
		}
	}
}

func TestValidateSettingsFileContentStrictlyMatchesTSWritePath(t *testing.T) {
	valid := `{
		"$schema": "https://json.schemastore.org/claude-code-settings.json",
		"apiKeyHelper": "/usr/local/bin/claude-key",
		"permissions": {
			"defaultMode": "auto",
			"disableBypassPermissionsMode": "disable",
			"additionalDirectories": ["/work/shared"]
		},
		"allowedMcpServers": [
			{"serverName": "github"},
			{"serverCommand": ["node", "server.js"]},
			{"serverUrl": "https://mcp.example.com/*"}
		],
		"autoMode": {"allow": ["Read"], "soft_deny": ["Bash(rm:*)"]},
		"pluginConfigs": {
			"formatter@corp": {
				"options": {"level": 2, "enabled": true, "patterns": ["*.go"]}
			}
		}
	}`
	if result := ValidateSettingsFileContent(valid); !result.IsValid {
		t.Fatalf("expected valid TS-shaped settings, got %s", result.Error)
	}

	unknown := ValidateSettingsFileContent(`{"unknownFromTypo": true}`)
	if unknown.IsValid || !strings.Contains(unknown.Error, "unknownFromTypo") {
		t.Fatalf("expected strict unknown-key error, got %+v", unknown)
	}
}

func TestParseSettingsFilePreservesUnknownPassthroughFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(`{"futureSetting": true, "verbose": true}`), 0o644); err != nil {
		t.Fatalf("write settings: %v", err)
	}
	result := ParseSettingsFile(path)
	if len(result.Errors) != 0 {
		t.Fatalf("expected passthrough parse to accept future fields, got %#v", result.Errors)
	}
	if result.Settings["futureSetting"] != true {
		t.Fatalf("expected future field to be preserved, got %#v", result.Settings)
	}
}

func TestValidateMCPPolicyEntries(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "entry needs exactly one matcher",
			body: `{"allowedMcpServers":[{"serverName":"github","serverUrl":"https://example.com"}]}`,
			want: "exactly one",
		},
		{
			name: "server name shape",
			body: `{"deniedMcpServers":[{"serverName":"bad name"}]}`,
			want: "letters, numbers, hyphens, and underscores",
		},
		{
			name: "server command min items",
			body: `{"allowedMcpServers":[{"serverCommand":[]}]}`,
			want: "at least one",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ValidateSettingsFileContent(tt.body)
			if result.IsValid || !strings.Contains(result.Error, tt.want) {
				t.Fatalf("expected error containing %q, got %+v", tt.want, result)
			}
		})
	}
}

func TestManagedSettingsDangerousReview(t *testing.T) {
	settings := Document{
		"apiKeyHelper": "/tmp/key-helper",
		"env": Document{
			"AWS_PROFILE":        "work",
			"ANTHROPIC_BASE_URL": "https://proxy.example.com",
		},
		"hooks": Document{"PreToolUse": []any{Document{"matcher": "Bash"}}},
	}
	review := ReviewManagedSettingsSecurity(settings)
	if !review.RequiresApproval {
		t.Fatalf("expected dangerous settings to require approval: %+v", review)
	}
	wantItems := []string{"ANTHROPIC_BASE_URL", "apiKeyHelper", "hooks"}
	if !reflect.DeepEqual(review.Items, wantItems) {
		t.Fatalf("expected items %v, got %v", wantItems, review.Items)
	}

	safe := ReviewManagedSettingsSecurity(Document{"env": Document{"AWS_PROFILE": "work"}})
	if safe.RequiresApproval {
		t.Fatalf("expected safe env-only settings not to require approval: %+v", safe)
	}
}

func TestDangerousSettingsChanged(t *testing.T) {
	oldSettings := Document{"env": Document{"ANTHROPIC_BASE_URL": "https://a.example"}}
	sameSettings := Document{"env": Document{"ANTHROPIC_BASE_URL": "https://a.example"}}
	newSettings := Document{"env": Document{"ANTHROPIC_BASE_URL": "https://b.example"}}

	if HasDangerousSettingsChanged(oldSettings, sameSettings) {
		t.Fatal("expected identical dangerous settings not to be treated as changed")
	}
	if !HasDangerousSettingsChanged(oldSettings, newSettings) {
		t.Fatal("expected changed dangerous settings to require approval")
	}
	if HasDangerousSettingsChanged(nil, Document{"env": Document{"AWS_PROFILE": "work"}}) {
		t.Fatal("expected safe new settings not to require approval")
	}
}

func TestValidateSettingsDocumentNonStrictAllowsFutureKeys(t *testing.T) {
	errs := ValidateSettingsDocument(Document{
		"futureSetting": true,
		"permissions": Document{
			"defaultMode": "plan",
		},
	}, false)
	if len(errs) != 0 {
		t.Fatalf("expected non-strict validation to allow future keys, got %#v", errs)
	}

	errs = ValidateSettingsDocument(Document{"futureSetting": true}, true)
	if len(errs) == 0 || !strings.Contains(errs[0].Message, "unrecognized field") {
		t.Fatalf("expected strict validation to reject future key, got %#v", errs)
	}
}

func TestLoadManagedFileSettingsMergesBaseAndDropIns(t *testing.T) {
	root := t.TempDir()
	t.Setenv(managedSettingsEnvOverride, root)

	if err := os.WriteFile(ManagedSettingsFilePath(), []byte(`{"verbose":true,"env":{"A":"1"},"permissions":{"allow":["Read"]}}`), 0o644); err != nil {
		t.Fatalf("write base managed settings: %v", err)
	}
	if err := os.MkdirAll(ManagedSettingsDropInDir(), 0o755); err != nil {
		t.Fatalf("mkdir dropin: %v", err)
	}
	if err := os.WriteFile(filepath.Join(ManagedSettingsDropInDir(), "20-extra.json"), []byte(`{"env":{"B":"2"},"fastMode":true,"permissions":{"allow":["Write","Read"]}}`), 0o644); err != nil {
		t.Fatalf("write dropin: %v", err)
	}

	result := LoadManagedFileSettings()
	env := result.Settings["env"].(Document)
	if env["A"] != "1" || env["B"] != "2" || result.Settings["fastMode"] != true {
		t.Fatalf("unexpected managed settings merge: %#v", result.Settings)
	}
	perms, _ := asDocument(result.Settings["permissions"])
	allow := perms["allow"].([]any)
	if !reflect.DeepEqual(allow, []any{"Read", "Write"}) {
		t.Fatalf("expected arrays to merge and dedupe across sources, got %#v", allow)
	}
}

func TestUpdateSettingsForSourceReplacesArrays(t *testing.T) {
	workingDir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_HOME", t.TempDir())
	path := SettingsFilePathForSource(SourceUser, workingDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir settings dir: %v", err)
	}
	if err := os.WriteFile(path, []byte(`{"permissions":{"allow":["Read","Write"]}}`), 0o644); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	err := UpdateSettingsForSource(EditableUser, workingDir, Document{
		"permissions": Document{"allow": []any{"Edit"}},
	})
	if err != nil {
		t.Fatalf("update settings: %v", err)
	}

	result := ParseSettingsFile(path)
	perms, _ := asDocument(result.Settings["permissions"])
	allow := perms["allow"].([]any)
	if !reflect.DeepEqual(allow, []any{"Edit"}) {
		t.Fatalf("expected arrays to be replaced on write, got %#v", allow)
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
