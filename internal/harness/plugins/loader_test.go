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

func TestLoaderReadsTypeScriptStyleManifest(t *testing.T) {
	root := t.TempDir()
	pluginDir := filepath.Join(root, "example")
	for _, dir := range []string{"commands", "agents", "skills", "output-styles"} {
		if err := os.MkdirAll(filepath.Join(pluginDir, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writePluginManifest(t, pluginDir, `{
		"name": "example",
		"description": "Example plugin",
		"version": "1.2.3",
		"author": {"name": "ACME"},
		"commands": {
			"about": {"source": "./commands/about.md", "description": "About this plugin"},
			"inline": {"content": "Inline command body"}
		},
		"agents": ["./agents/helper.md"],
		"skills": "./skills",
		"outputStyles": ["./output-styles"],
		"hooks": "./hooks/hooks.json",
		"mcpServers": {"demo": {"command": "demo-mcp"}},
		"lspServers": {"go": {"command": "gopls"}},
		"settings": {"env": {"PLUGIN_MODE": "demo"}},
		"dependencies": ["dep@market"]
	}`)

	loaded, err := NewLoader(root).LoadDetailed(LoadOptions{
		Marketplace:     "inline",
		Repository:      "session",
		EnabledPlugins:  map[string]bool{"example@inline": false},
		IncludeDisabled: true,
	})
	if err != nil {
		t.Fatalf("load detailed plugins: %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("expected one plugin, got %#v", loaded)
	}
	plugin := loaded[0]
	if plugin.Source != "example@inline" || plugin.Repository != "session" {
		t.Fatalf("unexpected source/repository: %#v", plugin)
	}
	if plugin.Enabled {
		t.Fatalf("expected enabledPlugins override to disable plugin")
	}
	if plugin.CommandsPath != filepath.Join(pluginDir, "commands") {
		t.Fatalf("unexpected commands path: %q", plugin.CommandsPath)
	}
	if plugin.SkillsPath != filepath.Join(pluginDir, "skills") {
		t.Fatalf("unexpected skills path: %q", plugin.SkillsPath)
	}
	if len(plugin.CommandsMetadata) != 2 || plugin.CommandsMetadata["about"].Source != "./commands/about.md" {
		t.Fatalf("unexpected command metadata: %#v", plugin.CommandsMetadata)
	}
	if len(plugin.AgentsPaths) != 1 || plugin.AgentsPaths[0] != filepath.Join(pluginDir, "agents/helper.md") {
		t.Fatalf("unexpected agents paths: %#v", plugin.AgentsPaths)
	}
	if len(plugin.MCPServers) != 1 || len(plugin.LSPServers) != 1 {
		t.Fatalf("expected MCP/LSP servers: %#v %#v", plugin.MCPServers, plugin.LSPServers)
	}
}

func TestLoaderRejectsEscapingManifestPaths(t *testing.T) {
	root := t.TempDir()
	pluginDir := filepath.Join(root, "bad")
	writePluginManifest(t, pluginDir, `{
		"name": "bad",
		"version": "1.0.0",
		"commands": "./../escape.md"
	}`)

	_, err := NewLoader(root).Load()
	if err == nil {
		t.Fatal("expected path traversal error")
	}
}

func TestLoaderRejectsDuplicatePluginIDs(t *testing.T) {
	root := t.TempDir()
	writePluginManifest(t, filepath.Join(root, "one"), `{"name":"dup","version":"1.0.0"}`)
	writePluginManifest(t, filepath.Join(root, "two"), `{"name":"dup","version":"2.0.0"}`)

	_, err := NewLoader(root).LoadDetailed(LoadOptions{Marketplace: "inline"})
	if err == nil {
		t.Fatal("expected duplicate plugin ID error")
	}
}

func writePluginManifest(t *testing.T, pluginDir string, content string) {
	t.Helper()
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
