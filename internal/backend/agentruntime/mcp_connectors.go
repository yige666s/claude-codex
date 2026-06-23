package agentruntime

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"claude-codex/internal/harness/engine"
	mcpcore "claude-codex/internal/harness/mcp"
)

const (
	MCPServerStatusConnected    = "connected"
	MCPServerStatusDisconnected = "disconnected"
	MCPServerStatusDisabled     = "disabled"
	MCPServerStatusError        = "error"

	MCPConnectionKindRemote         = "mcp_remote"
	MCPConnectionKindBuiltinAdapter = "mcp_builtin_adapter"
	MCPConnectionKindSyncedIndex    = "synced_index"

	MCPToolSideEffectRead    = "read"
	MCPToolSideEffectWrite   = "write"
	MCPToolSideEffectUnknown = "unknown"
)

type MCPServerBinding struct {
	ID               string         `json:"server_id"`
	UserID           string         `json:"user_id"`
	WorkspaceID      string         `json:"workspace_id,omitempty"`
	Provider         string         `json:"provider"`
	DisplayName      string         `json:"display_name"`
	Transport        string         `json:"transport"`
	URL              string         `json:"url,omitempty"`
	Command          []string       `json:"command,omitempty"`
	HeadersRef       string         `json:"headers_ref,omitempty"`
	OAuthTokenRef    string         `json:"oauth_token_ref,omitempty"`
	Status           string         `json:"status"`
	LastDiscoveredAt *time.Time     `json:"last_discovered_at,omitempty"`
	Instructions     string         `json:"instructions,omitempty"`
	Metadata         map[string]any `json:"metadata,omitempty"`
	CreatedAt        time.Time      `json:"created_at"`
	UpdatedAt        time.Time      `json:"updated_at"`
}

type MCPToolPolicy struct {
	ID               string         `json:"policy_id"`
	UserID           string         `json:"user_id"`
	WorkspaceID      string         `json:"workspace_id,omitempty"`
	ServerID         string         `json:"server_id"`
	Provider         string         `json:"provider"`
	ToolName         string         `json:"tool_name"`
	PermissionPolicy string         `json:"permission_policy"`
	RequiresReview   bool           `json:"requires_review"`
	SideEffectLevel  string         `json:"side_effect_level"`
	Allowed          bool           `json:"allowed"`
	Metadata         map[string]any `json:"metadata,omitempty"`
	CreatedAt        time.Time      `json:"created_at"`
	UpdatedAt        time.Time      `json:"updated_at"`
}

type MCPConnectorStore interface {
	Init(context.Context) error
	UpsertServer(ctx context.Context, server MCPServerBinding) (MCPServerBinding, error)
	GetServer(ctx context.Context, userID, workspaceID, provider string) (*MCPServerBinding, error)
	DisableServer(ctx context.Context, userID, workspaceID, provider string, at time.Time) error
	ListToolPolicies(ctx context.Context, userID, workspaceID, serverID string) ([]MCPToolPolicy, error)
	GetToolPolicy(ctx context.Context, userID, workspaceID, serverID, toolName string) (*MCPToolPolicy, error)
	UpsertToolPolicy(ctx context.Context, policy MCPToolPolicy) (MCPToolPolicy, error)
}

type MemoryMCPConnectorStore struct {
	mu       sync.Mutex
	servers  map[string]MCPServerBinding
	policies map[string]MCPToolPolicy
}

func NewMemoryMCPConnectorStore() *MemoryMCPConnectorStore {
	return &MemoryMCPConnectorStore{
		servers:  map[string]MCPServerBinding{},
		policies: map[string]MCPToolPolicy{},
	}
}

func (s *MemoryMCPConnectorStore) Init(context.Context) error {
	if s == nil {
		return fmt.Errorf("mcp connector store is not configured")
	}
	return nil
}

func (s *MemoryMCPConnectorStore) UpsertServer(_ context.Context, server MCPServerBinding) (MCPServerBinding, error) {
	if s == nil {
		return server, fmt.Errorf("mcp connector store is not configured")
	}
	server = normalizeMCPServerBinding(server, time.Now().UTC())
	s.mu.Lock()
	defer s.mu.Unlock()
	key := mcpServerKey(server.UserID, server.WorkspaceID, server.Provider)
	if existing, ok := s.servers[key]; ok && server.CreatedAt.IsZero() {
		server.CreatedAt = existing.CreatedAt
	}
	s.servers[key] = cloneMCPServerBinding(server)
	return cloneMCPServerBinding(server), nil
}

func (s *MemoryMCPConnectorStore) GetServer(_ context.Context, userID, workspaceID, provider string) (*MCPServerBinding, error) {
	if s == nil {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	server, ok := s.servers[mcpServerKey(userID, workspaceID, provider)]
	if !ok {
		return nil, nil
	}
	cloned := cloneMCPServerBinding(server)
	return &cloned, nil
}

func (s *MemoryMCPConnectorStore) DisableServer(_ context.Context, userID, workspaceID, provider string, at time.Time) error {
	if s == nil {
		return fmt.Errorf("mcp connector store is not configured")
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := mcpServerKey(userID, workspaceID, provider)
	server, ok := s.servers[key]
	if !ok {
		return nil
	}
	server.Status = MCPServerStatusDisabled
	server.UpdatedAt = at
	s.servers[key] = cloneMCPServerBinding(server)
	return nil
}

func (s *MemoryMCPConnectorStore) ListToolPolicies(_ context.Context, userID, workspaceID, serverID string) ([]MCPToolPolicy, error) {
	if s == nil {
		return []MCPToolPolicy{}, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []MCPToolPolicy{}
	for _, policy := range s.policies {
		if policy.UserID == strings.TrimSpace(userID) && policy.WorkspaceID == strings.TrimSpace(workspaceID) && policy.ServerID == strings.TrimSpace(serverID) {
			out = append(out, cloneMCPToolPolicy(policy))
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ToolName < out[j].ToolName })
	return out, nil
}

func (s *MemoryMCPConnectorStore) GetToolPolicy(_ context.Context, userID, workspaceID, serverID, toolName string) (*MCPToolPolicy, error) {
	if s == nil {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	policy, ok := s.policies[mcpToolPolicyKey(userID, workspaceID, serverID, toolName)]
	if !ok {
		return nil, nil
	}
	cloned := cloneMCPToolPolicy(policy)
	return &cloned, nil
}

func (s *MemoryMCPConnectorStore) UpsertToolPolicy(_ context.Context, policy MCPToolPolicy) (MCPToolPolicy, error) {
	if s == nil {
		return policy, fmt.Errorf("mcp connector store is not configured")
	}
	policy = normalizeMCPToolPolicy(policy, time.Now().UTC())
	s.mu.Lock()
	defer s.mu.Unlock()
	s.policies[mcpToolPolicyKey(policy.UserID, policy.WorkspaceID, policy.ServerID, policy.ToolName)] = cloneMCPToolPolicy(policy)
	return cloneMCPToolPolicy(policy), nil
}

type SQLMCPConnectorStore struct {
	db      *sql.DB
	dialect SQLDialect
}

func NewSQLMCPConnectorStoreWithDialect(db *sql.DB, dialect SQLDialect) *SQLMCPConnectorStore {
	if dialect == "" {
		dialect = SQLDialectQuestion
	}
	return &SQLMCPConnectorStore{db: db, dialect: dialect}
}

func (s *SQLMCPConnectorStore) Init(ctx context.Context) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("mcp connector store is not configured")
	}
	if err := requireSQLColumns(ctx, s.db, "agent_mcp_servers",
		"server_id", "user_id", "workspace_id", "provider", "display_name", "transport", "url",
		"command_json", "headers_ref", "oauth_token_ref", "status", "last_discovered_at",
		"instructions", "metadata_json", "created_at", "updated_at",
	); err != nil {
		return err
	}
	return requireSQLColumns(ctx, s.db, "agent_mcp_tool_policies",
		"policy_id", "user_id", "workspace_id", "server_id", "provider", "tool_name",
		"permission_policy", "requires_review", "side_effect_level", "allowed",
		"metadata_json", "created_at", "updated_at",
	)
}

func (s *SQLMCPConnectorStore) UpsertServer(ctx context.Context, server MCPServerBinding) (MCPServerBinding, error) {
	server = normalizeMCPServerBinding(server, time.Now().UTC())
	commandJSON, _ := json.Marshal(server.Command)
	metadataJSON, _ := json.Marshal(server.Metadata)
	if s.dialect == SQLDialectPostgres {
		query := `INSERT INTO agent_mcp_servers (
server_id, user_id, workspace_id, provider, display_name, transport, url, command_json,
headers_ref, oauth_token_ref, status, last_discovered_at, instructions, metadata_json, created_at, updated_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)
ON CONFLICT (user_id, workspace_id, provider) DO UPDATE SET
display_name = EXCLUDED.display_name,
transport = EXCLUDED.transport,
url = EXCLUDED.url,
command_json = EXCLUDED.command_json,
headers_ref = EXCLUDED.headers_ref,
oauth_token_ref = EXCLUDED.oauth_token_ref,
status = EXCLUDED.status,
last_discovered_at = EXCLUDED.last_discovered_at,
instructions = EXCLUDED.instructions,
metadata_json = EXCLUDED.metadata_json,
updated_at = EXCLUDED.updated_at`
		_, err := s.db.ExecContext(ctx, query, server.ID, server.UserID, server.WorkspaceID, server.Provider, server.DisplayName, server.Transport, server.URL, string(commandJSON), server.HeadersRef, server.OAuthTokenRef, server.Status, server.LastDiscoveredAt, server.Instructions, string(metadataJSON), server.CreatedAt, server.UpdatedAt)
		return server, err
	}
	query := `INSERT OR REPLACE INTO agent_mcp_servers (
server_id, user_id, workspace_id, provider, display_name, transport, url, command_json,
headers_ref, oauth_token_ref, status, last_discovered_at, instructions, metadata_json, created_at, updated_at
) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`
	_, err := s.db.ExecContext(ctx, query, server.ID, server.UserID, server.WorkspaceID, server.Provider, server.DisplayName, server.Transport, server.URL, string(commandJSON), server.HeadersRef, server.OAuthTokenRef, server.Status, server.LastDiscoveredAt, server.Instructions, string(metadataJSON), server.CreatedAt, server.UpdatedAt)
	return server, err
}

func (s *SQLMCPConnectorStore) GetServer(ctx context.Context, userID, workspaceID, provider string) (*MCPServerBinding, error) {
	query := s.dialect.Bind(`SELECT server_id, user_id, workspace_id, provider, display_name, transport, url, command_json, headers_ref, oauth_token_ref, status, last_discovered_at, instructions, metadata_json, created_at, updated_at
FROM agent_mcp_servers
WHERE user_id = ? AND workspace_id = ? AND provider = ?
LIMIT 1`)
	row := s.db.QueryRowContext(ctx, query, strings.TrimSpace(userID), strings.TrimSpace(workspaceID), normalizeConnectorProviderID(provider))
	server, err := scanMCPServerBinding(row)
	if err != nil {
		if errorsIsSQLNoRows(err) {
			return nil, nil
		}
		return nil, err
	}
	return &server, nil
}

func (s *SQLMCPConnectorStore) DisableServer(ctx context.Context, userID, workspaceID, provider string, at time.Time) error {
	if at.IsZero() {
		at = time.Now().UTC()
	}
	query := s.dialect.Bind(`UPDATE agent_mcp_servers SET status = ?, updated_at = ? WHERE user_id = ? AND workspace_id = ? AND provider = ?`)
	_, err := s.db.ExecContext(ctx, query, MCPServerStatusDisabled, at, strings.TrimSpace(userID), strings.TrimSpace(workspaceID), normalizeConnectorProviderID(provider))
	return err
}

func (s *SQLMCPConnectorStore) ListToolPolicies(ctx context.Context, userID, workspaceID, serverID string) ([]MCPToolPolicy, error) {
	query := s.dialect.Bind(`SELECT policy_id, user_id, workspace_id, server_id, provider, tool_name, permission_policy, requires_review, side_effect_level, allowed, metadata_json, created_at, updated_at
FROM agent_mcp_tool_policies
WHERE user_id = ? AND workspace_id = ? AND server_id = ?
ORDER BY tool_name ASC`)
	rows, err := s.db.QueryContext(ctx, query, strings.TrimSpace(userID), strings.TrimSpace(workspaceID), strings.TrimSpace(serverID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []MCPToolPolicy{}
	for rows.Next() {
		policy, err := scanMCPToolPolicy(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, policy)
	}
	return out, rows.Err()
}

func (s *SQLMCPConnectorStore) GetToolPolicy(ctx context.Context, userID, workspaceID, serverID, toolName string) (*MCPToolPolicy, error) {
	query := s.dialect.Bind(`SELECT policy_id, user_id, workspace_id, server_id, provider, tool_name, permission_policy, requires_review, side_effect_level, allowed, metadata_json, created_at, updated_at
FROM agent_mcp_tool_policies
WHERE user_id = ? AND workspace_id = ? AND server_id = ? AND tool_name = ?
LIMIT 1`)
	row := s.db.QueryRowContext(ctx, query, strings.TrimSpace(userID), strings.TrimSpace(workspaceID), strings.TrimSpace(serverID), strings.TrimSpace(toolName))
	policy, err := scanMCPToolPolicy(row)
	if err != nil {
		if errorsIsSQLNoRows(err) {
			return nil, nil
		}
		return nil, err
	}
	return &policy, nil
}

func (s *SQLMCPConnectorStore) UpsertToolPolicy(ctx context.Context, policy MCPToolPolicy) (MCPToolPolicy, error) {
	policy = normalizeMCPToolPolicy(policy, time.Now().UTC())
	metadataJSON, _ := json.Marshal(policy.Metadata)
	if s.dialect == SQLDialectPostgres {
		query := `INSERT INTO agent_mcp_tool_policies (
policy_id, user_id, workspace_id, server_id, provider, tool_name, permission_policy,
requires_review, side_effect_level, allowed, metadata_json, created_at, updated_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
ON CONFLICT (user_id, workspace_id, server_id, tool_name) DO UPDATE SET
provider = EXCLUDED.provider,
permission_policy = EXCLUDED.permission_policy,
requires_review = EXCLUDED.requires_review,
side_effect_level = EXCLUDED.side_effect_level,
allowed = EXCLUDED.allowed,
metadata_json = EXCLUDED.metadata_json,
updated_at = EXCLUDED.updated_at`
		_, err := s.db.ExecContext(ctx, query, policy.ID, policy.UserID, policy.WorkspaceID, policy.ServerID, policy.Provider, policy.ToolName, policy.PermissionPolicy, policy.RequiresReview, policy.SideEffectLevel, policy.Allowed, string(metadataJSON), policy.CreatedAt, policy.UpdatedAt)
		return policy, err
	}
	query := `INSERT OR REPLACE INTO agent_mcp_tool_policies (
policy_id, user_id, workspace_id, server_id, provider, tool_name, permission_policy,
requires_review, side_effect_level, allowed, metadata_json, created_at, updated_at
) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`
	_, err := s.db.ExecContext(ctx, query, policy.ID, policy.UserID, policy.WorkspaceID, policy.ServerID, policy.Provider, policy.ToolName, policy.PermissionPolicy, policy.RequiresReview, policy.SideEffectLevel, policy.Allowed, string(metadataJSON), policy.CreatedAt, policy.UpdatedAt)
	return policy, err
}

type mcpScanner interface {
	Scan(dest ...any) error
}

func scanMCPServerBinding(row mcpScanner) (MCPServerBinding, error) {
	var server MCPServerBinding
	var commandRaw, metadataRaw string
	var lastDiscoveredAt sql.NullTime
	err := row.Scan(&server.ID, &server.UserID, &server.WorkspaceID, &server.Provider, &server.DisplayName, &server.Transport, &server.URL, &commandRaw, &server.HeadersRef, &server.OAuthTokenRef, &server.Status, &lastDiscoveredAt, &server.Instructions, &metadataRaw, &server.CreatedAt, &server.UpdatedAt)
	if err != nil {
		return MCPServerBinding{}, err
	}
	_ = json.Unmarshal([]byte(commandRaw), &server.Command)
	_ = json.Unmarshal([]byte(metadataRaw), &server.Metadata)
	server.LastDiscoveredAt = nullTimePtr(lastDiscoveredAt)
	return normalizeMCPServerBinding(server, time.Now().UTC()), nil
}

func scanMCPToolPolicy(row mcpScanner) (MCPToolPolicy, error) {
	var policy MCPToolPolicy
	var metadataRaw string
	err := row.Scan(&policy.ID, &policy.UserID, &policy.WorkspaceID, &policy.ServerID, &policy.Provider, &policy.ToolName, &policy.PermissionPolicy, &policy.RequiresReview, &policy.SideEffectLevel, &policy.Allowed, &metadataRaw, &policy.CreatedAt, &policy.UpdatedAt)
	if err != nil {
		return MCPToolPolicy{}, err
	}
	_ = json.Unmarshal([]byte(metadataRaw), &policy.Metadata)
	return normalizeMCPToolPolicy(policy, time.Now().UTC()), nil
}

type MCPConnectorToolCall struct {
	UserID      string
	WorkspaceID string
	Provider    string
	ToolName    string
	Args        json.RawMessage
}

func (r *Runtime) SetMCPConnectorStore(store MCPConnectorStore) {
	if r == nil {
		return
	}
	if store == nil {
		store = NewMemoryMCPConnectorStore()
	}
	r.mcpConnectors = store
}

func (r *Runtime) SetMCPHost(host mcpcore.Host) {
	if r == nil {
		return
	}
	if host == nil {
		host = mcpcore.NewRuntimeHost(nil)
	}
	r.mcpHost = host
}

func (r *Runtime) mcpConnectorStore() MCPConnectorStore {
	if r == nil {
		return NewMemoryMCPConnectorStore()
	}
	if r.mcpConnectors == nil {
		r.mcpConnectors = NewMemoryMCPConnectorStore()
	}
	return r.mcpConnectors
}

func (r *Runtime) connectorMCPHost() mcpcore.Host {
	if r == nil {
		return mcpcore.NewRuntimeHost(nil)
	}
	if r.mcpHost == nil {
		r.mcpHost = mcpcore.NewRuntimeHost(nil)
	}
	return r.mcpHost
}

func (r *Runtime) ensureConnectorMCPBinding(ctx context.Context, connection ConnectorConnection) (MCPServerBinding, error) {
	connection = normalizeConnectorConnection(connection, time.Now().UTC())
	if connection.UserID == "" || connection.Provider == "" {
		return MCPServerBinding{}, fmt.Errorf("connector connection user and provider are required for MCP binding")
	}
	now := time.Now().UTC()
	provider, _ := connectorProviderByID(connection.Provider)
	connectionKind := mcpProviderConnectionKind(connection.Provider)
	url := strings.TrimSpace(mcpProviderDefaultURL(connection.Provider))
	transport := mcpProviderDefaultTransport(connection.Provider, url)
	if connectionKind == MCPConnectionKindBuiltinAdapter {
		transport = "inprocess"
		url = ""
	}
	needsRemoteURL := connectionKind == MCPConnectionKindRemote && url == ""
	if existing, err := r.mcpConnectorStore().GetServer(ctx, connection.UserID, connection.WorkspaceID, connection.Provider); err != nil {
		return MCPServerBinding{}, err
	} else if existing != nil && existing.Status != MCPServerStatusDisabled {
		existingKind := deepAgentWorkflowString(existing.Metadata, "connection_kind")
		tokenMatches := strings.TrimSpace(existing.OAuthTokenRef) == strings.TrimSpace(connection.TokenRef)
		if existing.Transport == transport && existing.URL == url && existingKind == connectionKind && tokenMatches {
			if needsRemoteURL && existing.Status == MCPServerStatusError {
				return *existing, nil
			}
			if existing.Status != MCPServerStatusError {
				if connectionKind == MCPConnectionKindBuiltinAdapter {
					needsToolRefresh, refreshErr := r.mcpConnectorBindingNeedsDefaultToolRefresh(ctx, *existing, connection.Provider)
					if refreshErr != nil {
						return MCPServerBinding{}, refreshErr
					}
					if needsToolRefresh {
						return r.refreshConnectorMCPDiscovery(ctx, *existing, connection, now)
					}
				}
				if connectionKind == MCPConnectionKindRemote && len(mcpToolNamesFromServerMetadata(*existing)) == 0 {
					return r.refreshConnectorMCPDiscovery(ctx, *existing, connection, now)
				}
				return *existing, nil
			}
		}
		existing.DisplayName = firstNonEmptyString(provider.Name, connection.Provider+" MCP")
		existing.Transport = transport
		existing.URL = url
		existing.OAuthTokenRef = connection.TokenRef
		existing.Status = MCPServerStatusConnected
		existing.LastDiscoveredAt = &now
		existing.UpdatedAt = now
		if existing.Metadata == nil {
			existing.Metadata = map[string]any{}
		}
		existing.Metadata["connection_kind"] = connectionKind
		existing.Metadata["connector_id"] = connection.ID
		delete(existing.Metadata, "last_discovery_error")
		if needsRemoteURL {
			existing.Status = MCPServerStatusError
			existing.LastDiscoveredAt = nil
			existing.Metadata["last_discovery_error"] = mcpProviderMissingURLMessage(connection.Provider)
			return r.mcpConnectorStore().UpsertServer(ctx, *existing)
		}
		updated, updateErr := r.mcpConnectorStore().UpsertServer(ctx, *existing)
		if updateErr != nil {
			return MCPServerBinding{}, updateErr
		}
		return r.refreshConnectorMCPDiscovery(ctx, updated, connection, now)
	}
	server := MCPServerBinding{
		ID:               "mcp-" + newSortableID(),
		UserID:           connection.UserID,
		WorkspaceID:      connection.WorkspaceID,
		Provider:         connection.Provider,
		DisplayName:      firstNonEmptyString(provider.Name, connection.Provider+" MCP"),
		Transport:        transport,
		URL:              url,
		OAuthTokenRef:    connection.TokenRef,
		Status:           MCPServerStatusConnected,
		LastDiscoveredAt: &now,
		CreatedAt:        now,
		UpdatedAt:        now,
		Metadata: map[string]any{
			"connection_kind": connectionKind,
			"connector_id":    connection.ID,
		},
	}
	server, err := r.mcpConnectorStore().UpsertServer(ctx, server)
	if err != nil {
		return MCPServerBinding{}, err
	}
	if needsRemoteURL {
		server.Status = MCPServerStatusError
		server.LastDiscoveredAt = nil
		server.Metadata["last_discovery_error"] = mcpProviderMissingURLMessage(connection.Provider)
		return r.mcpConnectorStore().UpsertServer(ctx, server)
	}
	return r.refreshConnectorMCPDiscovery(ctx, server, connection, now)
}

func (r *Runtime) mcpConnectorBindingNeedsDefaultToolRefresh(ctx context.Context, server MCPServerBinding, provider string) (bool, error) {
	defaultTools := defaultMCPToolsForProvider(provider)
	if len(defaultTools) == 0 {
		return false, nil
	}
	policies, err := r.mcpConnectorStore().ListToolPolicies(ctx, server.UserID, server.WorkspaceID, server.ID)
	if err != nil {
		return false, err
	}
	existing := make(map[string]bool, len(policies))
	for _, policy := range policies {
		if strings.TrimSpace(policy.ToolName) != "" {
			existing[strings.TrimSpace(policy.ToolName)] = true
		}
	}
	for _, toolName := range defaultTools {
		if !existing[strings.TrimSpace(toolName)] {
			return true, nil
		}
	}
	return false, nil
}

func connectorMCPArgsWithInternalContext(input json.RawMessage, userID, workspaceID string) (json.RawMessage, error) {
	if len(input) == 0 {
		input = json.RawMessage(`{}`)
	}
	var payload map[string]any
	if err := json.Unmarshal(input, &payload); err != nil {
		return nil, fmt.Errorf("mcp connector args must be a JSON object: %w", err)
	}
	if payload == nil {
		payload = map[string]any{}
	}
	payload["user_id"] = strings.TrimSpace(userID)
	if strings.TrimSpace(workspaceID) != "" {
		payload["workspace_id"] = strings.TrimSpace(workspaceID)
	} else {
		delete(payload, "workspace_id")
	}
	out, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(out), nil
}

func (r *Runtime) refreshConnectorMCPDiscovery(ctx context.Context, server MCPServerBinding, connection ConnectorConnection, now time.Time) (MCPServerBinding, error) {
	tools, discoveryErr := r.discoverConnectorMCPTools(ctx, server)
	if discoveryErr != nil {
		server.Status = MCPServerStatusError
		if server.Metadata == nil {
			server.Metadata = map[string]any{}
		}
		delete(server.Metadata, "tool_names")
		server.Metadata["last_discovery_error"] = discoveryErr.Error()
		server.UpdatedAt = time.Now().UTC()
		if updated, updateErr := r.mcpConnectorStore().UpsertServer(ctx, server); updateErr == nil {
			server = updated
		}
		tools = defaultMCPToolsForProvider(connection.Provider)
	} else {
		server.Status = MCPServerStatusConnected
		server.LastDiscoveredAt = &now
		server.UpdatedAt = now
		if server.Metadata == nil {
			server.Metadata = map[string]any{}
		}
		delete(server.Metadata, "last_discovery_error")
		server.Metadata["tool_names"] = append([]string(nil), tools...)
		if updated, updateErr := r.mcpConnectorStore().UpsertServer(ctx, server); updateErr == nil {
			server = updated
		}
	}
	for _, toolName := range tools {
		policy := MCPToolPolicy{
			ID:               "mcp-pol-" + newSortableID(),
			UserID:           connection.UserID,
			WorkspaceID:      connection.WorkspaceID,
			ServerID:         server.ID,
			Provider:         connection.Provider,
			ToolName:         toolName,
			PermissionPolicy: ConnectorPolicyReadOnly,
			RequiresReview:   false,
			SideEffectLevel:  MCPToolSideEffectRead,
			Allowed:          true,
			CreatedAt:        now,
			UpdatedAt:        now,
		}
		if _, err := r.mcpConnectorStore().UpsertToolPolicy(ctx, policy); err != nil {
			return MCPServerBinding{}, err
		}
	}
	return server, nil
}

func (r *Runtime) discoverConnectorMCPTools(ctx context.Context, server MCPServerBinding) ([]string, error) {
	if server.Transport == "inprocess" {
		return defaultMCPToolsForProvider(server.Provider), nil
	}
	cfg, err := r.mcpRuntimeConfigForServer(ctx, server)
	if err != nil {
		return nil, err
	}
	definitions, err := r.connectorMCPHost().DiscoverTools(ctx, cfg)
	if err != nil {
		return nil, err
	}
	tools := make([]string, 0, len(definitions))
	seen := map[string]bool{}
	for _, definition := range definitions {
		name := strings.TrimSpace(definition.Name)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		tools = append(tools, name)
	}
	sort.Strings(tools)
	return tools, nil
}

func (r *Runtime) CallConnectorMCPTool(ctx context.Context, call MCPConnectorToolCall) (mcpcore.ToolResult, MCPServerBinding, MCPToolPolicy, error) {
	call.UserID = strings.TrimSpace(call.UserID)
	call.WorkspaceID = strings.TrimSpace(call.WorkspaceID)
	call.Provider = normalizeConnectorProviderID(call.Provider)
	call.ToolName = strings.TrimSpace(call.ToolName)
	if call.UserID == "" || call.Provider == "" || call.ToolName == "" {
		return mcpcore.ToolResult{}, MCPServerBinding{}, MCPToolPolicy{}, fmt.Errorf("mcp connector call requires user, provider, and tool")
	}
	connection, err := r.connectorStore().GetConnection(ctx, call.UserID, call.WorkspaceID, call.Provider)
	if err != nil {
		return mcpcore.ToolResult{}, MCPServerBinding{}, MCPToolPolicy{}, err
	}
	if connection == nil || connection.Status != ConnectorStatusConnected || connection.PermissionPolicy == ConnectorPolicyDisabled {
		return mcpcore.ToolResult{}, MCPServerBinding{}, MCPToolPolicy{}, fmt.Errorf("%s connector is not connected", call.Provider)
	}
	server, err := r.ensureConnectorMCPBinding(ctx, *connection)
	if err != nil {
		return mcpcore.ToolResult{}, MCPServerBinding{}, MCPToolPolicy{}, err
	}
	if server.Status != MCPServerStatusConnected {
		reason := deepAgentWorkflowString(server.Metadata, "last_discovery_error")
		if reason == "" {
			reason = server.Status
		}
		return mcpcore.ToolResult{}, server, MCPToolPolicy{}, fmt.Errorf("%s MCP server is not connected: %s", call.Provider, reason)
	}
	policy, err := r.mcpConnectorStore().GetToolPolicy(ctx, call.UserID, call.WorkspaceID, server.ID, call.ToolName)
	if err != nil {
		return mcpcore.ToolResult{}, server, MCPToolPolicy{}, err
	}
	if policy == nil {
		defaultPolicy := defaultMCPToolPolicy(call.UserID, call.WorkspaceID, server, call.ToolName)
		policy = &defaultPolicy
	}
	if !policy.Allowed || policy.PermissionPolicy == ConnectorPolicyDisabled {
		return mcpcore.ToolResult{}, server, *policy, fmt.Errorf("mcp connector tool %s is disabled by policy", call.ToolName)
	}
	if policy.RequiresReview || policy.PermissionPolicy == ConnectorPolicyWriteWithReview || policy.SideEffectLevel == MCPToolSideEffectWrite || policy.SideEffectLevel == MCPToolSideEffectUnknown {
		return mcpcore.ToolResult{}, server, *policy, fmt.Errorf("%w: mcp connector tool %s requires review", ErrDeepAgentReviewRequired, call.ToolName)
	}
	cfg, err := r.mcpRuntimeConfigForServer(ctx, server)
	if err != nil {
		return mcpcore.ToolResult{}, server, *policy, err
	}
	if server.Transport == "inprocess" {
		call.Args, err = connectorMCPArgsWithInternalContext(call.Args, call.UserID, call.WorkspaceID)
		if err != nil {
			return mcpcore.ToolResult{}, server, *policy, err
		}
	}
	argsHash := connectorArgsHash(call.Args)
	scope := engine.ToolExecutionScopeFromContext(ctx)
	ledger := r.toolCallLedger
	idempotencyKey := firstNonEmptyString(scope.WorkflowRunID, scope.JobID, scope.SessionID, call.UserID) + ":mcp_connector:" + call.Provider + ":" + call.ToolName + ":" + argsHash
	entry := newMCPConnectorLedgerEntry(call, server, *policy, scope, argsHash, idempotencyKey)
	if ledger != nil {
		started, replayed, beginErr := ledger.BeginToolCall(ctx, entry)
		if beginErr != nil {
			return mcpcore.ToolResult{}, server, *policy, beginErr
		}
		entry = started
		if replayed && strings.TrimSpace(started.Output) != "" {
			return mcpcore.ToolResult{Output: started.Output}, server, *policy, nil
		}
	}
	emitJobEventFromContext(ctx, Event{Type: "mcp_connector_tool_call_started", Role: "tool", Content: call.ToolName, Data: deepAgentEventData(entry.Metadata)})
	result, err := r.connectorMCPHost().CallTool(ctx, cfg, call.ToolName, call.Args)
	if err != nil {
		if fallback, ok, fallbackErr := r.callGmailRESTMCPFallback(ctx, call, server, err); ok {
			if fallbackErr == nil {
				if ledger != nil {
					_ = ledger.CompleteToolCall(ctx, idempotencyKey, fallback.Output, map[string]any{"server_id": server.ID, "provider": call.Provider, "tool_name": call.ToolName, "fallback": "gmail_rest"})
				}
				emitJobEventFromContext(ctx, Event{Type: "mcp_connector_tool_call_succeeded", Role: "tool", Content: call.ToolName, Data: deepAgentEventData(map[string]any{"server_id": server.ID, "provider": call.Provider, "tool_name": call.ToolName, "fallback": "gmail_rest"})})
				return fallback, server, *policy, nil
			}
			err = fallbackErr
		}
		if ledger != nil {
			_ = ledger.FailToolCall(ctx, idempotencyKey, err.Error(), map[string]any{"server_id": server.ID, "provider": call.Provider, "tool_name": call.ToolName})
		}
		emitJobEventFromContext(ctx, Event{Type: "mcp_connector_tool_call_failed", Role: "tool", Content: call.ToolName, Error: err.Error(), Data: deepAgentEventData(entry.Metadata)})
		return mcpcore.ToolResult{}, server, *policy, err
	}
	if ledger != nil {
		_ = ledger.CompleteToolCall(ctx, idempotencyKey, result.Output, map[string]any{"server_id": server.ID, "provider": call.Provider, "tool_name": call.ToolName})
	}
	emitJobEventFromContext(ctx, Event{Type: "mcp_connector_tool_call_succeeded", Role: "tool", Content: call.ToolName, Data: deepAgentEventData(map[string]any{"server_id": server.ID, "provider": call.Provider, "tool_name": call.ToolName})})
	return result, server, *policy, nil
}

func (r *Runtime) mcpRuntimeConfigForServer(ctx context.Context, server MCPServerBinding) (mcpcore.HostConfig, error) {
	headers := map[string]string{}
	if strings.TrimSpace(server.OAuthTokenRef) != "" {
		token, err := r.connectorTokenForMCPServer(ctx, server)
		if err != nil {
			return mcpcore.HostConfig{}, err
		}
		if token != nil && strings.TrimSpace(token.AccessToken) != "" {
			headers["Authorization"] = connectorAuthorizationHeader(*token)
		}
	}
	cfg := mcpcore.HostConfig{
		Name:      firstNonEmptyString(server.ID, server.Provider),
		Provider:  server.Provider,
		Transport: server.Transport,
		URL:       server.URL,
		Command:   append([]string(nil), server.Command...),
		Headers:   headers,
	}
	if server.Transport == "inprocess" {
		inProcess, err := r.inProcessMCPServerForProvider(server.Provider)
		if err != nil {
			return mcpcore.HostConfig{}, err
		}
		cfg.InProcessServer = inProcess
	}
	return cfg, nil
}

func (r *Runtime) connectorTokenForMCPServer(ctx context.Context, server MCPServerBinding) (*ConnectorToken, error) {
	tokenRef := strings.TrimSpace(server.OAuthTokenRef)
	if tokenRef == "" {
		return nil, nil
	}
	token, err := r.connectorTokenVault().GetToken(ctx, tokenRef)
	if err != nil {
		return nil, err
	}
	if token == nil || !connectorTokenRefreshDue(*token, time.Now().UTC(), connectorTokenRefreshLookahead) {
		return token, nil
	}
	connection, err := r.connectorStore().GetConnection(ctx, server.UserID, server.WorkspaceID, server.Provider)
	if err != nil {
		return nil, err
	}
	if connection == nil {
		return token, nil
	}
	provider, ok := connectorProviderByID(connection.Provider)
	if !ok {
		return token, nil
	}
	if err := r.refreshConnectorToken(ctx, provider, *connection); err != nil {
		return nil, err
	}
	refreshedConnection, err := r.connectorStore().GetConnection(ctx, server.UserID, server.WorkspaceID, server.Provider)
	if err != nil {
		return nil, err
	}
	if refreshedConnection != nil {
		if refreshedConnection.Status == ConnectorStatusExpired {
			return nil, fmt.Errorf("%s connector token expired; reconnect %s", server.Provider, server.Provider)
		}
		if strings.TrimSpace(refreshedConnection.TokenRef) != "" {
			return r.connectorTokenVault().GetToken(ctx, refreshedConnection.TokenRef)
		}
	}
	return r.connectorTokenVault().GetToken(ctx, tokenRef)
}

func (r *Runtime) inProcessMCPServerForProvider(provider string) (*mcpcore.Server, error) {
	switch normalizeConnectorProviderID(provider) {
	case "github":
		return NewGitHubConnectorMCPServer(r), nil
	default:
		return nil, fmt.Errorf("no in-process MCP adapter for connector provider %s", provider)
	}
}

func defaultMCPToolPolicy(userID, workspaceID string, server MCPServerBinding, toolName string) MCPToolPolicy {
	return MCPToolPolicy{
		ID:               "mcp-pol-" + newSortableID(),
		UserID:           strings.TrimSpace(userID),
		WorkspaceID:      strings.TrimSpace(workspaceID),
		ServerID:         strings.TrimSpace(server.ID),
		Provider:         normalizeConnectorProviderID(server.Provider),
		ToolName:         strings.TrimSpace(toolName),
		PermissionPolicy: ConnectorPolicyReadOnly,
		RequiresReview:   false,
		SideEffectLevel:  MCPToolSideEffectRead,
		Allowed:          true,
		CreatedAt:        time.Now().UTC(),
		UpdatedAt:        time.Now().UTC(),
	}
}

func newMCPConnectorLedgerEntry(call MCPConnectorToolCall, server MCPServerBinding, policy MCPToolPolicy, scope engine.ToolExecutionScope, argsHash, idempotencyKey string) engine.ToolLedgerEntry {
	return engine.ToolLedgerEntry{
		UserID:            firstNonEmptyString(scope.UserID, call.UserID),
		SessionID:         scope.SessionID,
		JobID:             scope.JobID,
		WorkflowRunID:     scope.WorkflowRunID,
		WorkflowStepID:    scope.WorkflowStepID,
		WorkflowStepIndex: scope.WorkflowStepIndex,
		ToolName:          "mcp." + call.Provider + "." + call.ToolName,
		ArgsHash:          argsHash,
		IdempotencyKey:    idempotencyKey,
		Input:             call.Args,
		Metadata: map[string]any{
			"provider":           call.Provider,
			"server_id":          server.ID,
			"tool_name":          call.ToolName,
			"permission":         policy.PermissionPolicy,
			"requires_review":    policy.RequiresReview,
			"side_effect_level":  policy.SideEffectLevel,
			"external_call":      true,
			"connector_executor": "mcp",
		},
		StartedAt: time.Now().UTC(),
	}
}

func normalizeMCPServerBinding(server MCPServerBinding, now time.Time) MCPServerBinding {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	server.UserID = strings.TrimSpace(server.UserID)
	server.WorkspaceID = strings.TrimSpace(server.WorkspaceID)
	server.Provider = normalizeConnectorProviderID(server.Provider)
	server.DisplayName = strings.TrimSpace(server.DisplayName)
	server.Transport = strings.TrimSpace(server.Transport)
	if server.Transport == "" {
		if strings.TrimSpace(server.URL) != "" {
			server.Transport = "sse"
		} else {
			server.Transport = "inprocess"
		}
	}
	server.URL = strings.TrimSpace(server.URL)
	server.HeadersRef = strings.TrimSpace(server.HeadersRef)
	server.OAuthTokenRef = strings.TrimSpace(server.OAuthTokenRef)
	server.Status = normalizeMCPServerStatus(server.Status)
	if server.ID == "" {
		server.ID = "mcp-" + newSortableID()
	}
	if server.CreatedAt.IsZero() {
		server.CreatedAt = now
	}
	if server.UpdatedAt.IsZero() {
		server.UpdatedAt = now
	}
	if server.Metadata == nil {
		server.Metadata = map[string]any{}
	}
	return server
}

func normalizeMCPToolPolicy(policy MCPToolPolicy, now time.Time) MCPToolPolicy {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	policy.UserID = strings.TrimSpace(policy.UserID)
	policy.WorkspaceID = strings.TrimSpace(policy.WorkspaceID)
	policy.ServerID = strings.TrimSpace(policy.ServerID)
	policy.Provider = normalizeConnectorProviderID(policy.Provider)
	policy.ToolName = strings.TrimSpace(policy.ToolName)
	policy.PermissionPolicy = normalizeConnectorPolicy(policy.PermissionPolicy)
	policy.SideEffectLevel = normalizeMCPToolSideEffect(policy.SideEffectLevel)
	if policy.ID == "" {
		policy.ID = "mcp-pol-" + newSortableID()
	}
	if policy.CreatedAt.IsZero() {
		policy.CreatedAt = now
	}
	if policy.UpdatedAt.IsZero() {
		policy.UpdatedAt = now
	}
	if policy.Metadata == nil {
		policy.Metadata = map[string]any{}
	}
	return policy
}

func normalizeMCPServerStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case MCPServerStatusConnected:
		return MCPServerStatusConnected
	case MCPServerStatusDisabled:
		return MCPServerStatusDisabled
	case MCPServerStatusError:
		return MCPServerStatusError
	default:
		return MCPServerStatusDisconnected
	}
}

func normalizeMCPToolSideEffect(level string) string {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case MCPToolSideEffectRead:
		return MCPToolSideEffectRead
	case MCPToolSideEffectWrite:
		return MCPToolSideEffectWrite
	default:
		return MCPToolSideEffectUnknown
	}
}

func cloneMCPServerBinding(server MCPServerBinding) MCPServerBinding {
	server.Command = append([]string(nil), server.Command...)
	if server.LastDiscoveredAt != nil {
		at := *server.LastDiscoveredAt
		server.LastDiscoveredAt = &at
	}
	if server.Metadata != nil {
		metadata := make(map[string]any, len(server.Metadata))
		for key, value := range server.Metadata {
			metadata[key] = value
		}
		server.Metadata = metadata
	}
	return server
}

func cloneMCPToolPolicy(policy MCPToolPolicy) MCPToolPolicy {
	if policy.Metadata != nil {
		metadata := make(map[string]any, len(policy.Metadata))
		for key, value := range policy.Metadata {
			metadata[key] = value
		}
		policy.Metadata = metadata
	}
	return policy
}

func mcpServerKey(userID, workspaceID, provider string) string {
	return strings.TrimSpace(userID) + "\x00" + strings.TrimSpace(workspaceID) + "\x00" + normalizeConnectorProviderID(provider)
}

func mcpToolPolicyKey(userID, workspaceID, serverID, toolName string) string {
	return strings.TrimSpace(userID) + "\x00" + strings.TrimSpace(workspaceID) + "\x00" + strings.TrimSpace(serverID) + "\x00" + strings.TrimSpace(toolName)
}

func mcpProviderDefaultURL(provider string) string {
	for _, name := range mcpProviderServerURLEnvNames(provider) {
		if value := strings.TrimSpace(os.Getenv(name)); value != "" {
			return value
		}
	}
	switch normalizeConnectorProviderID(provider) {
	case "github":
		return "https://api.githubcopilot.com/mcp/"
	}
	return ""
}

func mcpProviderConnectionKind(provider string) string {
	return MCPConnectionKindRemote
}

func mcpProviderDefaultTransport(provider, url string) string {
	provider = normalizeConnectorProviderID(provider)
	if transport := strings.TrimSpace(os.Getenv(strings.ToUpper(strings.NewReplacer("-", "_", ".", "_").Replace(provider)) + "_MCP_SERVER_TRANSPORT")); transport != "" {
		return strings.ToLower(transport)
	}
	if (provider == "google_drive" || provider == "gmail") && strings.TrimSpace(os.Getenv("GOOGLE_MCP_SERVER_TRANSPORT")) != "" {
		return strings.ToLower(strings.TrimSpace(os.Getenv("GOOGLE_MCP_SERVER_TRANSPORT")))
	}
	url = strings.TrimSpace(url)
	if (provider == "google_drive" || provider == "gmail") && strings.Contains(url, "googleapis.com/mcp/") {
		return "http"
	}
	if provider == "notion" && strings.Contains(url, "mcp.notion.com/mcp") {
		return "http"
	}
	if provider == "linear" && strings.Contains(url, "mcp.linear.app/mcp") {
		return "http"
	}
	if provider == "slack" && strings.Contains(url, "mcp.slack.com/mcp") {
		return "http"
	}
	if provider == "github" && strings.Contains(url, "api.githubcopilot.com/mcp") {
		return "http"
	}
	return "sse"
}

func mcpProviderServerURLEnvNames(provider string) []string {
	provider = normalizeConnectorProviderID(provider)
	envKey := strings.ToUpper(strings.NewReplacer("-", "_", ".", "_").Replace(provider))
	names := []string{
		envKey + "_MCP_SERVER_URL",
		"AGENT_API_" + envKey + "_MCP_SERVER_URL",
	}
	switch provider {
	case "google_drive", "gmail":
		names = append(names, "GOOGLE_MCP_SERVER_URL", "AGENT_API_GOOGLE_MCP_SERVER_URL")
	}
	return names
}

func mcpProviderMissingURLMessage(provider string) string {
	return fmt.Sprintf("%s connector requires a remote MCP server URL (%s)", normalizeConnectorProviderID(provider), strings.Join(mcpProviderServerURLEnvNames(provider), " or "))
}

func defaultMCPToolsForProvider(provider string) []string {
	switch normalizeConnectorProviderID(provider) {
	default:
		return nil
	}
}
