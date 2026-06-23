package agentruntime

import (
	"context"
	"encoding/json"
	"strings"
	"time"
	"unicode"

	"claude-codex/internal/harness/permissions"
	toolkit "claude-codex/internal/harness/tools"
)

const connectorMCPToolDiscoveryTimeout = 15 * time.Second

type connectorMCPRuntimeTool struct {
	runtime     *Runtime
	userID      string
	workspaceID string
	provider    ConnectorProvider
	remoteName  string
	name        string
	description string
	inputSchema json.RawMessage
	permission  permissions.Level
}

func (r *Runtime) ConnectorMCPTools(ctx context.Context, scope Scope) []toolkit.Tool {
	if r == nil || strings.TrimSpace(scope.UserID) == "" {
		return nil
	}
	selected := normalizeConnectorScopes(scope.ConnectorContext)
	if len(selected) == 0 {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	callCtx, cancel := context.WithTimeout(ctx, connectorMCPToolDiscoveryTimeout)
	defer cancel()

	want := make(map[string]bool, len(selected))
	for _, provider := range selected {
		want[normalizeConnectorProviderID(provider)] = true
	}
	statuses, err := r.ListConnectorStatus(callCtx, scope.UserID, "")
	if err != nil {
		return nil
	}
	var out []toolkit.Tool
	seen := map[string]bool{}
	for _, status := range statuses {
		providerID := normalizeConnectorProviderID(status.Provider.ID)
		if !want[providerID] || status.Connection == nil || status.Connection.Status != ConnectorStatusConnected || status.Connection.PermissionPolicy == ConnectorPolicyDisabled {
			continue
		}
		if status.MCPServer == nil || status.MCPServer.Status != MCPServerStatusConnected {
			continue
		}
		definitions, err := r.connectorMCPToolDefinitions(callCtx, *status.MCPServer)
		if err != nil {
			continue
		}
		policies := make(map[string]MCPToolPolicy, len(status.MCPTools))
		for _, policy := range status.MCPTools {
			policies[strings.TrimSpace(policy.ToolName)] = policy
		}
		for _, definition := range definitions {
			remoteName := strings.TrimSpace(definition.Name)
			if remoteName == "" {
				continue
			}
			policy := policies[remoteName]
			if !connectorMCPToolCallable(policy) {
				continue
			}
			name := connectorRuntimeToolName(providerID, remoteName)
			if name == "" || seen[name] {
				continue
			}
			seen[name] = true
			out = append(out, connectorMCPRuntimeTool{
				runtime:     r,
				userID:      scope.UserID,
				workspaceID: status.Connection.WorkspaceID,
				provider:    status.Provider,
				remoteName:  remoteName,
				name:        name,
				description: connectorRuntimeToolDescription(status.Provider, remoteName, definition.Description, policy),
				inputSchema: connectorRuntimeToolSchema(definition.InputSchema),
				permission:  connectorRuntimeToolPermission(policy),
			})
		}
	}
	return out
}

func (r *Runtime) connectorMCPToolDefinitions(ctx context.Context, server MCPServerBinding) ([]mcpcoreToolDefinition, error) {
	cfg, err := r.mcpRuntimeConfigForServer(ctx, server)
	if err != nil {
		return nil, err
	}
	definitions, err := r.connectorMCPHost().DiscoverTools(ctx, cfg)
	if err != nil {
		return nil, err
	}
	out := make([]mcpcoreToolDefinition, 0, len(definitions))
	for _, definition := range definitions {
		out = append(out, mcpcoreToolDefinition{
			Name:        definition.Name,
			Description: definition.Description,
			InputSchema: definition.InputSchema,
		})
	}
	return out, nil
}

type mcpcoreToolDefinition struct {
	Name        string
	Description string
	InputSchema json.RawMessage
}

func connectorMCPToolCallable(policy MCPToolPolicy) bool {
	if strings.TrimSpace(policy.ToolName) == "" {
		return false
	}
	return policy.Allowed && policy.PermissionPolicy != ConnectorPolicyDisabled
}

func connectorRuntimeToolName(provider, remoteName string) string {
	provider = sanitizeConnectorToolNamePart(provider)
	remote := sanitizeConnectorToolNamePart(remoteName)
	if provider == "" || remote == "" {
		return ""
	}
	if strings.HasPrefix(remote, provider+"_") {
		return remote
	}
	return provider + "_" + remote
}

func sanitizeConnectorToolNamePart(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastUnderscore := false
	for _, r := range value {
		ok := r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
		if !ok {
			r = '_'
		}
		if r == '_' {
			if lastUnderscore {
				continue
			}
			lastUnderscore = true
		} else {
			lastUnderscore = false
		}
		b.WriteRune(r)
	}
	return strings.Trim(b.String(), "_")
}

func connectorRuntimeToolDescription(provider ConnectorProvider, remoteName, description string, policy MCPToolPolicy) string {
	parts := []string{
		"Use the user's connected " + provider.Name + " account through its MCP server.",
		"Remote MCP tool: " + remoteName + ".",
	}
	if strings.TrimSpace(description) != "" {
		parts = append(parts, strings.TrimSpace(description))
	}
	if strings.TrimSpace(policy.PermissionPolicy) != "" {
		parts = append(parts, "Connector policy: "+policy.PermissionPolicy+".")
	}
	return strings.Join(parts, " ")
}

func connectorRuntimeToolSchema(schema json.RawMessage) json.RawMessage {
	if len(schema) > 0 && json.Valid(schema) {
		return connectorRuntimeToolUserSchema(schema)
	}
	return json.RawMessage(`{"type":"object","properties":{}}`)
}

func connectorRuntimeToolUserSchema(schema json.RawMessage) json.RawMessage {
	var payload map[string]any
	if err := json.Unmarshal(schema, &payload); err != nil {
		return schema
	}
	if properties, ok := payload["properties"].(map[string]any); ok {
		delete(properties, "user_id")
		delete(properties, "workspace_id")
		payload["properties"] = properties
	}
	if required, ok := payload["required"].([]any); ok {
		filtered := make([]any, 0, len(required))
		for _, item := range required {
			name, _ := item.(string)
			if name == "user_id" || name == "workspace_id" {
				continue
			}
			filtered = append(filtered, item)
		}
		if len(filtered) > 0 {
			payload["required"] = filtered
		} else {
			delete(payload, "required")
		}
	}
	out, err := json.Marshal(payload)
	if err != nil {
		return schema
	}
	return json.RawMessage(out)
}

func connectorRuntimeToolPermission(policy MCPToolPolicy) permissions.Level {
	switch policy.SideEffectLevel {
	case MCPToolSideEffectWrite, MCPToolSideEffectUnknown:
		return permissions.LevelWrite
	default:
		return permissions.LevelRead
	}
}

func (t connectorMCPRuntimeTool) Name() string {
	return t.name
}

func (t connectorMCPRuntimeTool) Description() string {
	return t.description
}

func (t connectorMCPRuntimeTool) InputSchema() json.RawMessage {
	return t.inputSchema
}

func (t connectorMCPRuntimeTool) Permission() permissions.Level {
	return t.permission
}

func (t connectorMCPRuntimeTool) IsConcurrencySafe() bool {
	return t.permission == permissions.LevelRead
}

func (t connectorMCPRuntimeTool) Execute(ctx context.Context, input json.RawMessage) (toolkit.Result, error) {
	if len(input) == 0 {
		input = json.RawMessage(`{}`)
	}
	result, _, _, err := t.runtime.CallConnectorMCPTool(ctx, MCPConnectorToolCall{
		UserID:      t.userID,
		WorkspaceID: t.workspaceID,
		Provider:    t.provider.ID,
		ToolName:    t.remoteName,
		Args:        input,
	})
	if err != nil {
		return toolkit.Result{}, err
	}
	return toolkit.Result{Output: result.Output}, nil
}
