package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"claude-codex/internal/app/config"
	mcpcore "claude-codex/internal/harness/mcp"
	"claude-codex/internal/harness/permissions"
	toolkit "claude-codex/internal/harness/tools"
)

type RemoteTool struct {
	serverName string
	definition mcpcore.ToolDefinition
	client     *mcpcore.Client
}

func NewRemoteTool(serverName string, definition mcpcore.ToolDefinition, client *mcpcore.Client) toolkit.Tool {
	return &RemoteTool{
		serverName: serverName,
		definition: definition,
		client:     client,
	}
}

func NewRemoteTools(ctx context.Context, server config.MCPServerConfig) ([]toolkit.Tool, error) {
	client, err := mcpcore.NewClientFromConfig(server, nil)
	if err != nil {
		return nil, err
	}
	if _, err := client.Initialize(ctx); err != nil {
		_ = client.Close()
		return nil, err
	}
	definitions, err := client.ListTools(ctx)
	if err != nil {
		_ = client.Close()
		return nil, err
	}
	mcpcore.RegisterActiveClient(server.Name, client)

	tools := make([]toolkit.Tool, 0, len(definitions))
	for _, definition := range definitions {
		tools = append(tools, NewRemoteTool(server.Name, definition, client))
	}
	return tools, nil
}

func (t *RemoteTool) Name() string {
	return fmt.Sprintf("mcp__%s__%s", mcpNamePart(t.serverName), mcpNamePart(t.definition.Name))
}

var invalidMCPToolNamePart = regexp.MustCompile(`[^A-Za-z0-9_-]+`)

func mcpNamePart(value string) string {
	value = invalidMCPToolNamePart.ReplaceAllString(strings.TrimSpace(value), "_")
	value = strings.Trim(value, "_")
	if value == "" {
		return "unnamed"
	}
	return value
}

func (t *RemoteTool) Description() string {
	return fmt.Sprintf("[%s] %s", t.serverName, t.definition.Description)
}

func (t *RemoteTool) InputSchema() json.RawMessage {
	return t.definition.InputSchema
}

func (t *RemoteTool) Permission() permissions.Level {
	if t.definition.Annotations != nil && t.definition.Annotations.ReadOnlyHint {
		return permissions.LevelRead
	}
	return permissions.LevelExecute
}

func (t *RemoteTool) IsConcurrencySafe() bool {
	// MCP tools are external and we don't know their safety characteristics
	// Default to false to be conservative
	return false
}

func (t *RemoteTool) Execute(ctx context.Context, input json.RawMessage) (toolkit.Result, error) {
	output, err := t.client.CallTool(ctx, t.definition.Name, input)
	if err != nil {
		return toolkit.Result{}, err
	}
	return toolkit.Result{Output: output}, nil
}
