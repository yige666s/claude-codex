package plugins

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoaderFindsPluginManifest(t *testing.T) {
	root := t.TempDir()
	pluginDir := filepath.Join(root, "example")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(`{"name":"example","version":"1.0.0"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	manifests, err := NewLoader(root).Load()
	if err != nil {
		t.Fatalf("load plugins: %v", err)
	}
	if len(manifests) != 1 || manifests[0].Name != "example" {
		t.Fatalf("unexpected manifests: %#v", manifests)
	}
}
