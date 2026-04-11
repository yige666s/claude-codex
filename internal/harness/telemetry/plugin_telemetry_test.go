package telemetry

import (
	"testing"

	"claude-codex/internal/harness/plugins"
)

func TestHashPluginIDStable(t *testing.T) {
	a := HashPluginID("demo", "builtin")
	b := HashPluginID("demo", "builtin")
	if a != b {
		t.Fatalf("expected stable hash, got %q vs %q", a, b)
	}
	if len(a) != 16 {
		t.Fatalf("expected 16-char hash, got %q", a)
	}
}

func TestBuildPluginTelemetryFieldsRedactsThirdParty(t *testing.T) {
	fields := BuildPluginTelemetryFields("demo", "custom-market", nil)
	if fields["plugin_name_redacted"] != "third-party" {
		t.Fatalf("expected third-party redaction, got %#v", fields)
	}
	if fields["plugin_scope"] != string(PluginScopeUserLocal) {
		t.Fatalf("expected user-local scope, got %#v", fields)
	}
}

func TestBuildPluginEventIncludesCounts(t *testing.T) {
	event := BuildPluginEvent(plugins.Manifest{
		Name:       "demo@builtin",
		Version:    "1.0.0",
		Path:       "/tmp/plugin.json",
		MCPServers: nil,
	}, nil)
	if event.Name != "plugin.loaded" {
		t.Fatalf("expected plugin.loaded event, got %#v", event)
	}
	if event.Attrs["plugin_scope"] != string(PluginScopeDefaultBundle) {
		t.Fatalf("expected default-bundle scope, got %#v", event.Attrs)
	}
}
