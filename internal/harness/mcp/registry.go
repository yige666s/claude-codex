package mcp

import (
	"context"
	"net/http"
	"strings"

	"claude-codex/internal/app/config"
)

func DiscoverTools(ctx context.Context, cfgs []config.MCPServerConfig, httpClient *http.Client) (map[string]*Client, map[string][]ToolDefinition, error) {
	clients := make(map[string]*Client, len(cfgs))
	definitions := make(map[string][]ToolDefinition, len(cfgs))
	closeDiscovered := func() {
		for _, client := range clients {
			if client != nil {
				_ = client.Close()
			}
		}
	}

	for _, cfg := range cfgs {
		client, err := NewClientFromConfig(cfg, httpClient)
		if err != nil {
			closeDiscovered()
			return nil, nil, err
		}
		if _, err := client.Initialize(ctx); err != nil {
			_ = client.Close()
			closeDiscovered()
			return nil, nil, err
		}
		tools, err := client.ListTools(ctx)
		if err != nil {
			_ = client.Close()
			closeDiscovered()
			return nil, nil, err
		}
		RegisterActiveClient(cfg.Name, client)
		clients[cfg.Name] = client
		definitions[cfg.Name] = tools
	}

	return clients, definitions, nil
}

// GetMCPInstructionsSection builds the mcp_instructions system prompt section
// from all connected clients that provided instructions during initialize.
// Returns empty string if no client has instructions.
func GetMCPInstructionsSection(clients []*Client) string {
	var blocks []string
	for _, c := range clients {
		if c == nil || c.Instructions == "" {
			continue
		}
		name := c.Name()
		blocks = append(blocks, "## "+name+"\n"+c.Instructions)
	}
	if len(blocks) == 0 {
		return ""
	}
	return "# MCP Server Instructions\n\nThe following MCP servers have provided instructions for how to use their tools and resources:\n\n" +
		strings.Join(blocks, "\n\n")
}
