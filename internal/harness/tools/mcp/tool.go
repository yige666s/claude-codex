package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ding/claude-code/claude-go/internal/app/config"
	mcpcore "github.com/ding/claude-code/claude-go/internal/harness/mcp"
	"github.com/ding/claude-code/claude-go/internal/harness/permissions"
	toolkit "github.com/ding/claude-code/claude-go/internal/harness/tools"
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
	definitions, err := client.ListTools(ctx)
	if err != nil {
		return nil, err
	}

	tools := make([]toolkit.Tool, 0, len(definitions))
	for _, definition := range definitions {
		tools = append(tools, NewRemoteTool(server.Name, definition, client))
	}
	return tools, nil
}

func (t *RemoteTool) Name() string {
	return fmt.Sprintf("mcp.%s.%s", t.serverName, t.definition.Name)
}

func (t *RemoteTool) Description() string {
	return fmt.Sprintf("[%s] %s", t.serverName, t.definition.Description)
}

func (t *RemoteTool) InputSchema() json.RawMessage {
	return t.definition.InputSchema
}

func (t *RemoteTool) Permission() permissions.Level {
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
