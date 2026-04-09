package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMigratesLegacyConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CLAUDE_GO_HOME", home)

	legacy := []byte(`{
  "backend": "anthropic",
  "model": "legacy-model",
  "permission_mode": "bypass",
  "theme": "light",
  "api_base_url": "https://example.test",
  "timeout_seconds": 33,
  "max_turns": 4
}`)
	if err := os.WriteFile(filepath.Join(home, "config.json"), legacy, 0o644); err != nil {
		t.Fatalf("write legacy config: %v", err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if cfg.SchemaVersion != CurrentSchemaVersion {
		t.Fatalf("expected schema version %d, got %d", CurrentSchemaVersion, cfg.SchemaVersion)
	}
	if cfg.Telemetry.ServiceName == "" || cfg.Telemetry.Exporter == "" {
		t.Fatalf("expected telemetry defaults after migration, got %#v", cfg.Telemetry)
	}
	if len(cfg.OAuth.Scopes) == 0 || cfg.OAuth.RedirectHost == "" {
		t.Fatalf("expected oauth defaults after migration, got %#v", cfg.OAuth)
	}
	if cfg.Theme != "light" {
		t.Fatalf("expected theme to persist, got %q", cfg.Theme)
	}
	if cfg.MCPServers == nil {
		t.Fatal("expected mcp server slice to be initialized")
	}
}

func TestLoadRejectsFutureSchemaVersion(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CLAUDE_GO_HOME", home)

	if err := os.WriteFile(filepath.Join(home, "config.json"), []byte(`{"schema_version":999}`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if _, err := Load(); err == nil {
		t.Fatal("expected future schema version to fail")
	}
}
