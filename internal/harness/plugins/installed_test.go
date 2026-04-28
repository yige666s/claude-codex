package plugins

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInstalledPluginStoreRoundTrip(t *testing.T) {
	store := InstalledPluginStore{Path: filepath.Join(t.TempDir(), "installed_plugins.json")}
	registry := NewInstalledPluginRegistry()
	registry.Upsert(PluginInstallation{
		PluginID:    "demo@market",
		InstallPath: "/tmp/demo",
		Version:     "1.0.0",
		Scope:       PluginScopeProject,
		ProjectPath: "/workspace",
	})

	if err := store.Save(registry); err != nil {
		t.Fatalf("save installed plugins: %v", err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatalf("load installed plugins: %v", err)
	}
	installed, ok := got.Plugins["demo@market"]
	if !ok || installed.Scope != PluginScopeProject || installed.ProjectPath != "/workspace" {
		t.Fatalf("unexpected installed plugin: %#v", got)
	}
}

func TestInstalledPluginStoreMigratesLegacyMap(t *testing.T) {
	path := filepath.Join(t.TempDir(), "installed_plugins.json")
	if err := os.WriteFile(path, []byte(`{
		"demo@market": {"installPath": "/tmp/demo", "version": "1.0.0"}
	}`), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := (InstalledPluginStore{Path: path}).Load()
	if err != nil {
		t.Fatalf("load legacy installed plugins: %v", err)
	}
	installed, ok := got.Plugins["demo@market"]
	if !ok || installed.PluginID != "demo@market" || installed.Scope != PluginScopeUser {
		t.Fatalf("unexpected migrated plugin: %#v", got)
	}
}

func TestLoadInstalledPlugins(t *testing.T) {
	root := t.TempDir()
	pluginDir := filepath.Join(root, "demo")
	writePluginManifest(t, pluginDir, `{"name":"demo","version":"1.0.0"}`)
	registry := NewInstalledPluginRegistry()
	registry.Upsert(PluginInstallation{
		PluginID:    "demo@market",
		InstallPath: pluginDir,
		Version:     "1.0.0",
		Scope:       PluginScopeUser,
	})

	loaded, err := LoadInstalledPlugins(registry, map[string]bool{"demo@market": true})
	if err != nil {
		t.Fatalf("load installed plugins: %v", err)
	}
	if len(loaded) != 1 || loaded[0].Source != "demo@market" || loaded[0].Repository != "market" {
		t.Fatalf("unexpected installed plugins: %#v", loaded)
	}
}
