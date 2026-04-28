package plugins

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"claude-codex/internal/harness/agent"
	"claude-codex/internal/harness/hooks"
	"claude-codex/internal/harness/skills"
)

func TestRuntimeLoadsPluginComponents(t *testing.T) {
	root := t.TempDir()
	pluginDir := filepath.Join(root, "demo")
	if err := os.MkdirAll(filepath.Join(pluginDir, "commands"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(pluginDir, "skills", "review"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(pluginDir, "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "commands", "about.md"), []byte("---\ndescription: About\n---\n\nAbout $ARGUMENTS"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "skills", "review", "SKILL.md"), []byte("---\nname: Review\ndescription: Review things\n---\n\nReview body"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "agents", "helper.md"), []byte("---\nname: helper\nwhen_to_use: help\n---\n\nAgent body"), 0o644); err != nil {
		t.Fatal(err)
	}
	hookCommand := "printf plugin-hook"
	if runtime.GOOS == "windows" {
		hookCommand = "echo plugin-hook"
	}
	writePluginManifest(t, pluginDir, `{
		"name": "demo",
		"version": "1.0.0",
		"commands": {"inline": {"content": "Inline command"}},
		"hooks": {"SessionStart": [{"hooks": [{"type": "command", "command": "`+hookCommand+`"}]}]},
		"mcpServers": {"demo-mcp": {"type": "stdio", "command": "demo-server", "args": ["--stdio"]}}
	}`)

	loaded, err := NewLoader(root).LoadDetailed(LoadOptions{Marketplace: "inline"})
	if err != nil {
		t.Fatalf("load plugins: %v", err)
	}
	manager := skills.NewSkillManager()
	registry := hooks.NewRegistry()
	agent.ClearPluginDefinitions()
	t.Cleanup(agent.ClearPluginDefinitions)

	report, err := LoadRuntimeComponents(RuntimeOptions{
		Plugins:        loaded,
		SkillManager:   manager,
		HookRegistry:   registry,
		RegisterAgents: true,
	})
	if err != nil {
		t.Fatalf("load runtime components: %v", err)
	}
	if report.SkillsLoaded < 2 || report.CommandsLoaded < 2 || report.AgentsLoaded != 1 || report.HooksLoaded != 1 || report.MCPServersLoaded != 1 {
		t.Fatalf("unexpected report: %#v", report)
	}
	for _, name := range []string{"demo:about", "demo:inline", "demo:review"} {
		if _, ok := manager.GetSkill(name); !ok {
			t.Fatalf("expected plugin skill/command %q to be registered", name)
		}
	}
	agents := agent.GetPluginDefinitions()
	if len(agents) != 1 || agents[0].AgentType != "demo:helper" {
		t.Fatalf("unexpected plugin agents: %#v", agents)
	}
	mcpConfigs := MCPServerConfigs(loaded)
	if len(mcpConfigs) != 1 || mcpConfigs[0].Name != "demo:demo-mcp" || len(mcpConfigs[0].Command) != 2 {
		t.Fatalf("unexpected mcp configs: %#v", mcpConfigs)
	}
	result, err := hooks.NewExecutor(registry).Execute(context.Background(), hooks.EventSessionStart, &hooks.HookInput{Event: hooks.EventSessionStart})
	if err != nil {
		t.Fatalf("execute hook: %v", err)
	}
	if strings.TrimSpace(strings.Join(result.AdditionalContexts, "\n")) != "plugin-hook" {
		t.Fatalf("unexpected hook output: %#v", result)
	}
}
