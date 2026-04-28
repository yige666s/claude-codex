package plugins

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInstallLocalMarketplacePluginCopiesAndRegisters(t *testing.T) {
	tmp := t.TempDir()
	source := filepath.Join(tmp, "source")
	writePluginManifest(t, source, `{"name":"demo","version":"1.0.0"}`)
	if err := os.WriteFile(filepath.Join(source, "README.md"), []byte("plugin"), 0o644); err != nil {
		t.Fatal(err)
	}

	cacheDir := filepath.Join(tmp, "cache")
	installedPath := filepath.Join(tmp, "installed_plugins.json")
	installer := Installer{
		CacheDir:       cacheDir,
		InstalledStore: InstalledPluginStore{Path: installedPath},
	}
	install, err := installer.InstallLocal("demo@local", source, PluginScopeProject, "/workspace")
	if err != nil {
		t.Fatalf("install local plugin: %v", err)
	}
	if install.InstallPath == "" {
		t.Fatal("expected install path")
	}
	if _, err := os.Stat(filepath.Join(install.InstallPath, "plugin.json")); err != nil {
		t.Fatalf("expected copied plugin manifest: %v", err)
	}
	registry, err := installer.InstalledStore.Load()
	if err != nil {
		t.Fatalf("load installed registry: %v", err)
	}
	if registry.Plugins["demo@local"].InstallPath != install.InstallPath {
		t.Fatalf("unexpected installed registry: %#v", registry)
	}
}

func TestUninstallRemovesInstallAndSetting(t *testing.T) {
	tmp := t.TempDir()
	source := filepath.Join(tmp, "source")
	writePluginManifest(t, source, `{"name":"demo","version":"1.0.0"}`)
	installer := Installer{
		CacheDir:       filepath.Join(tmp, "cache"),
		InstalledStore: InstalledPluginStore{Path: filepath.Join(tmp, "installed_plugins.json")},
	}
	install, err := installer.InstallLocal("demo@local", source, PluginScopeUser, "")
	if err != nil {
		t.Fatal(err)
	}
	settingsPath := filepath.Join(tmp, "settings.json")
	if err := UpdateEnabledPluginSetting(settingsPath, "demo@local", true); err != nil {
		t.Fatal(err)
	}
	if err := installer.Uninstall("demo@local", settingsPath); err != nil {
		t.Fatalf("uninstall plugin: %v", err)
	}
	if _, err := os.Stat(install.InstallPath); !os.IsNotExist(err) {
		t.Fatalf("expected install path removed, stat err=%v", err)
	}
	registry, err := installer.InstalledStore.Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := registry.Plugins["demo@local"]; ok {
		t.Fatalf("expected registry entry removed: %#v", registry)
	}
}
