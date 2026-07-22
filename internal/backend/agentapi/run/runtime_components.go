package run

import (
	"context"
	"errors"
	"strings"
	"sync"

	startupconfig "claude-codex/internal/backend/agentapi/config"
	"claude-codex/internal/harness/hooks"
	mcpcore "claude-codex/internal/harness/mcp"
	"claude-codex/internal/harness/plugins"
	"claude-codex/internal/harness/skills"
	toolkit "claude-codex/internal/harness/tools"
	mcptool "claude-codex/internal/harness/tools/mcp"
)

type runtimeComponents struct {
	cfg *startupconfig.Config

	mu           sync.Mutex
	mcpTools     []toolkit.Tool
	mcpClients   map[string]*mcpcore.Client
	mcpLoaded    bool
	pluginCount  int
	hookRegistry *hooks.Registry
}

func newRuntimeComponents(cfg *startupconfig.Config) *runtimeComponents {
	return &runtimeComponents{cfg: cfg, hookRegistry: hooks.NewRegistry()}
}

func (c *runtimeComponents) loadPlugins(skillManager *skills.SkillManager) error {
	if c == nil || c.cfg == nil {
		return nil
	}
	pluginDir := strings.TrimSpace(c.cfg.PluginDir)
	if pluginDir == "" {
		return nil
	}
	loaded, err := plugins.NewLoader(pluginDir).LoadDetailed(plugins.LoadOptions{
		Marketplace: plugins.InlineMarketplaceName,
		Repository:  "plugin_dir",
	})
	if err != nil {
		return err
	}
	if len(loaded) == 0 {
		return nil
	}
	report, err := plugins.LoadRuntimeComponents(plugins.RuntimeOptions{
		Plugins:        loaded,
		SkillManager:   skillManager,
		HookRegistry:   c.hookRegistry,
		RegisterAgents: true,
	})
	if err != nil {
		return err
	}
	c.pluginCount = report.PluginsLoaded
	c.cfg.MCPServers = append(c.cfg.MCPServers, plugins.MCPServerConfigs(loaded)...)
	return nil
}

func (c *runtimeComponents) ensureMCP(ctx context.Context) ([]string, error) {
	if c == nil || c.cfg == nil {
		return nil, nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.mcpLoaded {
		return c.mcpToolNamesLocked(), nil
	}
	if len(c.cfg.MCPServers) == 0 {
		c.mcpLoaded = true
		c.mcpTools = nil
		c.mcpClients = nil
		return nil, nil
	}
	configuredNames := make(map[string]bool, len(c.cfg.MCPServers))
	for _, server := range c.cfg.MCPServers {
		name := strings.TrimSpace(server.Name)
		if name == "" {
			return nil, errors.New("configured MCP server name is required")
		}
		if configuredNames[name] {
			return nil, errors.New("duplicate configured MCP server name: " + name)
		}
		configuredNames[name] = true
	}
	clients, definitions, err := mcpcore.DiscoverTools(ctx, c.cfg.MCPServers, nil)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool, len(c.cfg.MCPServers))
	seenToolNames := make(map[string]bool)
	discoveredTools := make([]toolkit.Tool, 0)
	for _, server := range c.cfg.MCPServers {
		name := strings.TrimSpace(server.Name)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		client := clients[name]
		if client == nil {
			continue
		}
		for _, definition := range definitions[name] {
			remoteTool := mcptool.NewRemoteTool(name, definition, client)
			if seenToolNames[remoteTool.Name()] {
				closeMCPClients(clients)
				return nil, errors.New("duplicate canonical MCP tool name: " + remoteTool.Name())
			}
			seenToolNames[remoteTool.Name()] = true
			discoveredTools = append(discoveredTools, remoteTool)
		}
	}
	c.mcpTools = discoveredTools
	c.mcpClients = clients
	c.mcpLoaded = true
	return c.mcpToolNamesLocked(), nil
}

func (c *runtimeComponents) mcpToolsSlice() []toolkit.Tool {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]toolkit.Tool(nil), c.mcpTools...)
}

func (c *runtimeComponents) mcpClientsSlice() []*mcpcore.Client {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.mcpClients) == 0 || len(c.cfg.MCPServers) == 0 {
		return nil
	}
	out := make([]*mcpcore.Client, 0, len(c.cfg.MCPServers))
	visited := make(map[string]bool, len(c.cfg.MCPServers))
	for _, server := range c.cfg.MCPServers {
		name := strings.TrimSpace(server.Name)
		if name == "" || visited[name] {
			continue
		}
		visited[name] = true
		if client := c.mcpClients[name]; client != nil {
			out = append(out, client)
		}
	}
	return out
}

func (c *runtimeComponents) mcpToolNamesLocked() []string {
	if len(c.mcpTools) == 0 {
		return nil
	}
	names := make([]string, 0, len(c.mcpTools))
	for _, tool := range c.mcpTools {
		if tool == nil {
			continue
		}
		names = append(names, tool.Name())
	}
	return names
}

func (c *runtimeComponents) Close() error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	clients := c.mcpClients
	c.mcpClients = nil
	c.mcpTools = nil
	c.mcpLoaded = false
	c.mu.Unlock()

	var closeErrors []error
	for _, client := range clients {
		if client == nil {
			continue
		}
		if err := client.Close(); err != nil {
			closeErrors = append(closeErrors, err)
		}
	}
	return errors.Join(closeErrors...)
}

func closeMCPClients(clients map[string]*mcpcore.Client) {
	for _, client := range clients {
		if client != nil {
			_ = client.Close()
		}
	}
}

func appendAllowedTools(existing, extra []string) []string {
	if len(extra) == 0 {
		return existing
	}
	seen := make(map[string]bool, len(existing)+len(extra))
	out := make([]string, 0, len(existing)+len(extra))
	for _, name := range existing {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	for _, name := range extra {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	return out
}
