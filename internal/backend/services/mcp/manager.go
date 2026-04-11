package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	appauth "claude-codex/internal/app/auth"
	"claude-codex/internal/app/config"
	mcpcore "claude-codex/internal/harness/mcp"
	toolkit "claude-codex/internal/harness/tools"
	mcptool "claude-codex/internal/harness/tools/mcp"
)

type Manager struct {
	auth       *appauth.Manager
	httpClient *http.Client
}

func NewManager(cfg config.Config) (*Manager, error) {
	auth, err := appauth.NewManager(cfg, nil)
	if err != nil {
		return nil, err
	}
	return &Manager{
		auth:       auth,
		httpClient: &http.Client{},
	}, nil
}

func (m *Manager) ConnectServer(ctx context.Context, name string, cfg config.MCPServerConfig) ServerConnection {
	client, err := mcpcore.NewClientFromConfig(cfg, m.httpClient)
	if err != nil {
		return ServerConnection{Name: name, Type: ConnectionTypeFailed, Config: cfg, Error: err.Error()}
	}
	if _, err := client.Initialize(ctx); err != nil {
		return ServerConnection{Name: name, Type: ConnectionTypeFailed, Config: cfg, Error: err.Error()}
	}
	return ServerConnection{
		Name:         name,
		Type:         ConnectionTypeConnected,
		Config:       cfg,
		Client:       client,
		Instructions: client.Instructions,
	}
}

func (m *Manager) ReconnectServer(ctx context.Context, name string, cfg config.MCPServerConfig) ServerConnection {
	if client, ok := mcpcore.GetActiveClient(name); ok && client != nil {
		_ = client.Close()
	}
	return m.ConnectServer(ctx, name, cfg)
}

func (m *Manager) FetchToolsAndResources(ctx context.Context, name string, cfg config.MCPServerConfig) (FetchResult, error) {
	connection := m.ConnectServer(ctx, name, cfg)
	if connection.Type != ConnectionTypeConnected {
		return FetchResult{Connection: connection}, nil
	}
	remoteTools, err := mcptool.NewRemoteTools(ctx, cfg)
	if err != nil {
		return FetchResult{}, err
	}
	resources, err := connection.Client.ListResources(ctx)
	if err != nil && !strings.Contains(strings.ToLower(err.Error()), "resource") {
		return FetchResult{}, err
	}
	if resources == nil {
		resources = []mcpcore.ResourceDefinition{}
	}
	filteredTools := make([]toolkit.Tool, 0, len(remoteTools))
	for _, tool := range remoteTools {
		if IsIncludedTool(name, tool.Name()) {
			filteredTools = append(filteredTools, tool)
		}
	}
	return FetchResult{
		Connection: connection,
		Tools:      filteredTools,
		Resources:  resources,
	}, nil
}

func (m *Manager) ProcessToolOutput(serverName, toolName, output string) (string, error) {
	if !ContentNeedsTruncation(output, DefaultMaxMCPOutputChars) {
		return output, nil
	}
	result, err := PersistToolResult(output, serverName+"-"+toolName)
	if err != nil {
		return "", err
	}
	return BuildLargeOutputInstructions(result, "Plain text"), nil
}

func (m *Manager) CallTool(ctx context.Context, connection ServerConnection, toolName string, input json.RawMessage) (string, error) {
	if connection.Client == nil {
		return "", fmt.Errorf("mcp server %s is not connected", connection.Name)
	}
	output, err := connection.Client.CallTool(ctx, toolName, input)
	if err != nil {
		return "", err
	}
	return m.ProcessToolOutput(connection.Name, toolName, output)
}

func (m *Manager) IsOfficialURL(url string) bool {
	return mcpcore.IsOfficialMCPURL(strings.TrimRight(strings.TrimSpace(url), "/"))
}
