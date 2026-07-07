package agentruntime

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	ConnectorStatusDisconnected = "disconnected"
	ConnectorStatusPending      = "pending"
	ConnectorStatusConnected    = "connected"
	ConnectorStatusExpired      = "expired"
	ConnectorStatusError        = "error"
	ConnectorStatusDisabled     = "disabled"

	ConnectorPolicyReadOnly        = "read_only"
	ConnectorPolicyDraftWrite      = "draft_write"
	ConnectorPolicyWriteWithReview = "write_with_review"
	ConnectorPolicyDisabled        = "disabled"
)

type ConnectorProvider struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Description     string   `json:"description"`
	Category        string   `json:"category"`
	AuthURL         string   `json:"auth_url,omitempty"`
	TokenURL        string   `json:"token_url,omitempty"`
	ClientIDEnv     string   `json:"client_id_env,omitempty"`
	ClientSecretEnv string   `json:"client_secret_env,omitempty"`
	Scopes          []string `json:"scopes"`
	Capabilities    []string `json:"capabilities"`
	DefaultPolicy   string   `json:"default_policy"`
	Configured      bool     `json:"configured"`
	ReviewByDefault bool     `json:"review_by_default"`
	ConnectionKind  string   `json:"connection_kind"`
	DefaultMCPURL   string   `json:"default_mcp_server_url,omitempty"`
	OfficialMCP     bool     `json:"official_mcp_server"`
	SyncedIndex     bool     `json:"supports_synced_index"`
}

type ConnectorConnection struct {
	ID                   string         `json:"id"`
	UserID               string         `json:"user_id"`
	WorkspaceID          string         `json:"workspace_id,omitempty"`
	Provider             string         `json:"provider"`
	Status               string         `json:"status"`
	PermissionPolicy     string         `json:"permission_policy"`
	Scopes               []string       `json:"scopes"`
	TokenRef             string         `json:"token_ref,omitempty"`
	ExternalAccountID    string         `json:"external_account_id,omitempty"`
	ExternalAccountLabel string         `json:"external_account_label,omitempty"`
	Metadata             map[string]any `json:"metadata,omitempty"`
	ConnectedAt          *time.Time     `json:"connected_at,omitempty"`
	LastSyncAt           *time.Time     `json:"last_sync_at,omitempty"`
	ExpiresAt            *time.Time     `json:"expires_at,omitempty"`
	CreatedAt            time.Time      `json:"created_at"`
	UpdatedAt            time.Time      `json:"updated_at"`
	DisconnectedAt       *time.Time     `json:"disconnected_at,omitempty"`
}

type ConnectorOAuthState struct {
	State       string   `json:"state"`
	UserID      string   `json:"user_id"`
	Provider    string   `json:"provider"`
	Scopes      []string `json:"scopes"`
	RedirectURI string   `json:"redirect_uri,omitempty"`
	Metadata    map[string]any
	CreatedAt   time.Time  `json:"created_at"`
	ExpiresAt   time.Time  `json:"expires_at"`
	UsedAt      *time.Time `json:"used_at,omitempty"`
}

type ConnectorStatus struct {
	Provider   ConnectorProvider    `json:"provider"`
	Connection *ConnectorConnection `json:"connection,omitempty"`
	Context    ConnectorContextHint `json:"context"`
	MCPServer  *MCPServerBinding    `json:"mcp_server,omitempty"`
	MCPTools   []MCPToolPolicy      `json:"mcp_tools,omitempty"`
}

type ConnectorContextHint struct {
	Enabled    bool     `json:"enabled"`
	TaskTypes  []string `json:"task_types"`
	Evidence   []string `json:"evidence"`
	PolicyHint string   `json:"policy_hint"`
}

type ConnectorAuthStart struct {
	Provider    string    `json:"provider"`
	State       string    `json:"state"`
	AuthURL     string    `json:"auth_url"`
	Scopes      []string  `json:"scopes"`
	Configured  bool      `json:"configured"`
	ExpiresAt   time.Time `json:"expires_at"`
	RedirectURI string    `json:"redirect_uri,omitempty"`
}

type ConnectorToken struct {
	Ref              string     `json:"ref"`
	Provider         string     `json:"provider"`
	AccessToken      string     `json:"-"`
	RefreshToken     string     `json:"-"`
	TokenType        string     `json:"token_type,omitempty"`
	Scopes           []string   `json:"scopes,omitempty"`
	ExpiresAt        *time.Time `json:"expires_at,omitempty"`
	RefreshExpiresAt *time.Time `json:"refresh_expires_at,omitempty"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

type ConnectorTokenVault interface {
	PutToken(ctx context.Context, token ConnectorToken) error
	GetToken(ctx context.Context, ref string) (*ConnectorToken, error)
	DeleteToken(ctx context.Context, ref string) error
}

type MemoryConnectorTokenVault struct {
	mu     sync.Mutex
	tokens map[string]ConnectorToken
}

func NewMemoryConnectorTokenVault() *MemoryConnectorTokenVault {
	return &MemoryConnectorTokenVault{tokens: map[string]ConnectorToken{}}
}

func (v *MemoryConnectorTokenVault) PutToken(_ context.Context, token ConnectorToken) error {
	if v == nil {
		return fmt.Errorf("connector token vault is not configured")
	}
	token.Ref = strings.TrimSpace(token.Ref)
	if token.Ref == "" {
		return fmt.Errorf("connector token ref is required")
	}
	token.Provider = normalizeConnectorProviderID(token.Provider)
	token.Scopes = normalizeConnectorScopes(token.Scopes)
	if token.TokenType == "" {
		token.TokenType = "bearer"
	}
	if token.UpdatedAt.IsZero() {
		token.UpdatedAt = time.Now().UTC()
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	v.tokens[token.Ref] = cloneConnectorToken(token)
	return nil
}

func connectorAuthorizationHeader(token ConnectorToken) string {
	scheme := strings.TrimSpace(token.TokenType)
	if scheme == "" || strings.EqualFold(scheme, "bearer") || strings.EqualFold(scheme, "bot") {
		scheme = "Bearer"
	}
	return scheme + " " + token.AccessToken
}

func (v *MemoryConnectorTokenVault) GetToken(_ context.Context, ref string) (*ConnectorToken, error) {
	if v == nil {
		return nil, nil
	}
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, nil
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	token, ok := v.tokens[ref]
	if !ok {
		return nil, nil
	}
	cloned := cloneConnectorToken(token)
	return &cloned, nil
}

func (v *MemoryConnectorTokenVault) DeleteToken(_ context.Context, ref string) error {
	if v == nil {
		return nil
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	delete(v.tokens, strings.TrimSpace(ref))
	return nil
}

type ConnectorStore interface {
	Init(context.Context) error
	ListConnections(ctx context.Context, userID, workspaceID string) ([]ConnectorConnection, error)
	GetConnection(ctx context.Context, userID, workspaceID, provider string) (*ConnectorConnection, error)
	UpsertConnection(ctx context.Context, connection ConnectorConnection) (ConnectorConnection, error)
	DisconnectConnection(ctx context.Context, userID, workspaceID, provider string, at time.Time) error
	ListRefreshableConnections(ctx context.Context, before time.Time, limit int) ([]ConnectorConnection, error)
	CreateOAuthState(ctx context.Context, state ConnectorOAuthState) error
	ConsumeOAuthState(ctx context.Context, userID, provider, state string, at time.Time) (*ConnectorOAuthState, error)
}

type MemoryConnectorStore struct {
	mu          sync.Mutex
	connections map[string]ConnectorConnection
	states      map[string]ConnectorOAuthState
}

func NewMemoryConnectorStore() *MemoryConnectorStore {
	return &MemoryConnectorStore{
		connections: make(map[string]ConnectorConnection),
		states:      make(map[string]ConnectorOAuthState),
	}
}

func (s *MemoryConnectorStore) Init(context.Context) error {
	if s == nil {
		return fmt.Errorf("connector store is not configured")
	}
	return nil
}

func (s *MemoryConnectorStore) ListConnections(_ context.Context, userID, workspaceID string) ([]ConnectorConnection, error) {
	if s == nil {
		return []ConnectorConnection{}, nil
	}
	userID = strings.TrimSpace(userID)
	workspaceID = strings.TrimSpace(workspaceID)
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]ConnectorConnection, 0)
	for _, connection := range s.connections {
		if connection.UserID != userID || connection.WorkspaceID != workspaceID {
			continue
		}
		out = append(out, cloneConnectorConnection(connection))
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	return out, nil
}

func (s *MemoryConnectorStore) GetConnection(_ context.Context, userID, workspaceID, provider string) (*ConnectorConnection, error) {
	if s == nil {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	connection, ok := s.connections[connectorConnectionKey(userID, workspaceID, provider)]
	if !ok {
		return nil, nil
	}
	cloned := cloneConnectorConnection(connection)
	return &cloned, nil
}

func (s *MemoryConnectorStore) UpsertConnection(_ context.Context, connection ConnectorConnection) (ConnectorConnection, error) {
	if s == nil {
		return connection, fmt.Errorf("connector store is not configured")
	}
	connection = normalizeConnectorConnection(connection, time.Now().UTC())
	s.mu.Lock()
	defer s.mu.Unlock()
	key := connectorConnectionKey(connection.UserID, connection.WorkspaceID, connection.Provider)
	if existing, ok := s.connections[key]; ok && connection.CreatedAt.IsZero() {
		connection.CreatedAt = existing.CreatedAt
	}
	s.connections[key] = cloneConnectorConnection(connection)
	return cloneConnectorConnection(connection), nil
}

func (s *MemoryConnectorStore) DisconnectConnection(_ context.Context, userID, workspaceID, provider string, at time.Time) error {
	if s == nil {
		return fmt.Errorf("connector store is not configured")
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := connectorConnectionKey(userID, workspaceID, provider)
	connection, ok := s.connections[key]
	if !ok {
		return nil
	}
	connection.Status = ConnectorStatusDisconnected
	connection.TokenRef = ""
	connection.UpdatedAt = at
	connection.DisconnectedAt = &at
	s.connections[key] = cloneConnectorConnection(connection)
	return nil
}

func (s *MemoryConnectorStore) ListRefreshableConnections(_ context.Context, before time.Time, limit int) ([]ConnectorConnection, error) {
	if s == nil {
		return []ConnectorConnection{}, nil
	}
	if before.IsZero() {
		before = time.Now().UTC()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]ConnectorConnection, 0)
	for _, connection := range s.connections {
		if connection.Status != ConnectorStatusConnected || strings.TrimSpace(connection.TokenRef) == "" || connection.ExpiresAt == nil {
			continue
		}
		if connection.ExpiresAt.After(before) {
			continue
		}
		out = append(out, cloneConnectorConnection(connection))
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].ExpiresAt == nil {
			return false
		}
		if out[j].ExpiresAt == nil {
			return true
		}
		return out[i].ExpiresAt.Before(*out[j].ExpiresAt)
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (s *MemoryConnectorStore) CreateOAuthState(_ context.Context, state ConnectorOAuthState) error {
	if s == nil {
		return fmt.Errorf("connector store is not configured")
	}
	state = normalizeConnectorOAuthState(state, time.Now().UTC())
	s.mu.Lock()
	defer s.mu.Unlock()
	s.states[state.State] = cloneConnectorOAuthState(state)
	return nil
}

func (s *MemoryConnectorStore) ConsumeOAuthState(_ context.Context, userID, provider, state string, at time.Time) (*ConnectorOAuthState, error) {
	if s == nil {
		return nil, fmt.Errorf("connector store is not configured")
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.states[strings.TrimSpace(state)]
	if !ok {
		return nil, fmt.Errorf("connector OAuth state not found")
	}
	if record.UserID != strings.TrimSpace(userID) || record.Provider != normalizeConnectorProviderID(provider) {
		return nil, fmt.Errorf("connector OAuth state does not match this user or provider")
	}
	if record.UsedAt != nil {
		return nil, fmt.Errorf("connector OAuth state was already used")
	}
	if !record.ExpiresAt.IsZero() && at.After(record.ExpiresAt) {
		return nil, fmt.Errorf("connector OAuth state expired")
	}
	record.UsedAt = &at
	s.states[record.State] = cloneConnectorOAuthState(record)
	cloned := cloneConnectorOAuthState(record)
	return &cloned, nil
}

type SQLConnectorStore struct {
	db      *sql.DB
	dialect SQLDialect
}

func NewSQLConnectorStoreWithDialect(db *sql.DB, dialect SQLDialect) *SQLConnectorStore {
	if dialect == "" {
		dialect = SQLDialectQuestion
	}
	return &SQLConnectorStore{db: db, dialect: dialect}
}

func (s *SQLConnectorStore) Init(ctx context.Context) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("connector store is not configured")
	}
	if err := requireSQLColumns(ctx, s.db, "agent_connector_connections",
		"connection_id", "user_id", "workspace_id", "provider", "status", "permission_policy",
		"scopes_json", "token_ref", "external_account_id", "external_account_label", "metadata_json",
		"connected_at", "last_sync_at", "expires_at", "created_at", "updated_at", "disconnected_at",
	); err != nil {
		return err
	}
	return requireSQLColumns(ctx, s.db, "agent_connector_oauth_states",
		"state", "user_id", "provider", "scopes_json", "redirect_uri", "created_at", "expires_at", "used_at",
	)
}

func (s *SQLConnectorStore) ListConnections(ctx context.Context, userID, workspaceID string) ([]ConnectorConnection, error) {
	query := s.dialect.Bind(`SELECT connection_id, user_id, workspace_id, provider, status, permission_policy, scopes_json, token_ref, external_account_id, external_account_label, metadata_json, connected_at, last_sync_at, expires_at, created_at, updated_at, disconnected_at
FROM agent_connector_connections
WHERE user_id = ? AND workspace_id = ?
ORDER BY updated_at DESC`)
	rows, err := s.db.QueryContext(ctx, query, strings.TrimSpace(userID), strings.TrimSpace(workspaceID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ConnectorConnection
	for rows.Next() {
		connection, err := scanConnectorConnection(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, connection)
	}
	return out, rows.Err()
}

func (s *SQLConnectorStore) GetConnection(ctx context.Context, userID, workspaceID, provider string) (*ConnectorConnection, error) {
	query := s.dialect.Bind(`SELECT connection_id, user_id, workspace_id, provider, status, permission_policy, scopes_json, token_ref, external_account_id, external_account_label, metadata_json, connected_at, last_sync_at, expires_at, created_at, updated_at, disconnected_at
FROM agent_connector_connections
WHERE user_id = ? AND workspace_id = ? AND provider = ?
LIMIT 1`)
	row := s.db.QueryRowContext(ctx, query, strings.TrimSpace(userID), strings.TrimSpace(workspaceID), normalizeConnectorProviderID(provider))
	connection, err := scanConnectorConnection(row)
	if err != nil {
		if errorsIsSQLNoRows(err) {
			return nil, nil
		}
		return nil, err
	}
	return &connection, nil
}

func (s *SQLConnectorStore) UpsertConnection(ctx context.Context, connection ConnectorConnection) (ConnectorConnection, error) {
	now := time.Now().UTC()
	connection = normalizeConnectorConnection(connection, now)
	scopesJSON, _ := json.Marshal(connection.Scopes)
	metadataJSON, _ := json.Marshal(connection.Metadata)
	if s.dialect == SQLDialectPostgres {
		query := `INSERT INTO agent_connector_connections (
connection_id, user_id, workspace_id, provider, status, permission_policy, scopes_json, token_ref,
external_account_id, external_account_label, metadata_json, connected_at, last_sync_at, expires_at, created_at, updated_at, disconnected_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)
ON CONFLICT (user_id, workspace_id, provider) DO UPDATE SET
status = EXCLUDED.status,
permission_policy = EXCLUDED.permission_policy,
scopes_json = EXCLUDED.scopes_json,
token_ref = EXCLUDED.token_ref,
external_account_id = EXCLUDED.external_account_id,
external_account_label = EXCLUDED.external_account_label,
metadata_json = EXCLUDED.metadata_json,
connected_at = EXCLUDED.connected_at,
last_sync_at = EXCLUDED.last_sync_at,
expires_at = EXCLUDED.expires_at,
updated_at = EXCLUDED.updated_at,
disconnected_at = EXCLUDED.disconnected_at`
		_, err := s.db.ExecContext(ctx, query,
			connection.ID, connection.UserID, connection.WorkspaceID, connection.Provider, connection.Status, connection.PermissionPolicy,
			string(scopesJSON), connection.TokenRef, connection.ExternalAccountID, connection.ExternalAccountLabel, string(metadataJSON),
			connection.ConnectedAt, connection.LastSyncAt, connection.ExpiresAt, connection.CreatedAt, connection.UpdatedAt, connection.DisconnectedAt,
		)
		if err != nil {
			return connection, err
		}
		return connection, nil
	}
	query := `INSERT OR REPLACE INTO agent_connector_connections (
connection_id, user_id, workspace_id, provider, status, permission_policy, scopes_json, token_ref,
external_account_id, external_account_label, metadata_json, connected_at, last_sync_at, expires_at, created_at, updated_at, disconnected_at
) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`
	_, err := s.db.ExecContext(ctx, query,
		connection.ID, connection.UserID, connection.WorkspaceID, connection.Provider, connection.Status, connection.PermissionPolicy,
		string(scopesJSON), connection.TokenRef, connection.ExternalAccountID, connection.ExternalAccountLabel, string(metadataJSON),
		connection.ConnectedAt, connection.LastSyncAt, connection.ExpiresAt, connection.CreatedAt, connection.UpdatedAt, connection.DisconnectedAt,
	)
	return connection, err
}

func (s *SQLConnectorStore) DisconnectConnection(ctx context.Context, userID, workspaceID, provider string, at time.Time) error {
	if at.IsZero() {
		at = time.Now().UTC()
	}
	query := s.dialect.Bind(`UPDATE agent_connector_connections
SET status = ?, token_ref = '', updated_at = ?, disconnected_at = ?
WHERE user_id = ? AND workspace_id = ? AND provider = ?`)
	_, err := s.db.ExecContext(ctx, query, ConnectorStatusDisconnected, at, at, strings.TrimSpace(userID), strings.TrimSpace(workspaceID), normalizeConnectorProviderID(provider))
	return err
}

func (s *SQLConnectorStore) ListRefreshableConnections(ctx context.Context, before time.Time, limit int) ([]ConnectorConnection, error) {
	if before.IsZero() {
		before = time.Now().UTC()
	}
	if limit <= 0 {
		limit = 100
	}
	query := s.dialect.Bind(`SELECT connection_id, user_id, workspace_id, provider, status, permission_policy, scopes_json, token_ref, external_account_id, external_account_label, metadata_json, connected_at, last_sync_at, expires_at, created_at, updated_at, disconnected_at
FROM agent_connector_connections
WHERE status = ? AND token_ref <> '' AND expires_at IS NOT NULL AND expires_at <= ?
ORDER BY expires_at ASC
LIMIT ?`)
	rows, err := s.db.QueryContext(ctx, query, ConnectorStatusConnected, before, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ConnectorConnection{}
	for rows.Next() {
		connection, err := scanConnectorConnection(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, connection)
	}
	return out, rows.Err()
}

func (s *SQLConnectorStore) CreateOAuthState(ctx context.Context, state ConnectorOAuthState) error {
	state = normalizeConnectorOAuthState(state, time.Now().UTC())
	scopesJSON, _ := json.Marshal(state.Scopes)
	metadataJSON, _ := json.Marshal(state.Metadata)
	query := s.dialect.Bind(`INSERT INTO agent_connector_oauth_states (state, user_id, provider, scopes_json, redirect_uri, metadata_json, created_at, expires_at, used_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	_, err := s.db.ExecContext(ctx, query, state.State, state.UserID, state.Provider, string(scopesJSON), state.RedirectURI, string(metadataJSON), state.CreatedAt, state.ExpiresAt, state.UsedAt)
	return err
}

func (s *SQLConnectorStore) ConsumeOAuthState(ctx context.Context, userID, provider, state string, at time.Time) (*ConnectorOAuthState, error) {
	if at.IsZero() {
		at = time.Now().UTC()
	}
	query := s.dialect.Bind(`SELECT state, user_id, provider, scopes_json, redirect_uri, metadata_json, created_at, expires_at, used_at
FROM agent_connector_oauth_states
WHERE state = ? AND user_id = ? AND provider = ?
LIMIT 1`)
	row := s.db.QueryRowContext(ctx, query, strings.TrimSpace(state), strings.TrimSpace(userID), normalizeConnectorProviderID(provider))
	record, err := scanConnectorOAuthState(row)
	if err != nil {
		if errorsIsSQLNoRows(err) {
			return nil, fmt.Errorf("connector OAuth state not found")
		}
		return nil, err
	}
	if record.UsedAt != nil {
		return nil, fmt.Errorf("connector OAuth state was already used")
	}
	if !record.ExpiresAt.IsZero() && at.After(record.ExpiresAt) {
		return nil, fmt.Errorf("connector OAuth state expired")
	}
	update := s.dialect.Bind(`UPDATE agent_connector_oauth_states SET used_at = ? WHERE state = ?`)
	if _, err := s.db.ExecContext(ctx, update, at, record.State); err != nil {
		return nil, err
	}
	record.UsedAt = &at
	return &record, nil
}

type connectorScanner interface {
	Scan(dest ...any) error
}

func scanConnectorConnection(row connectorScanner) (ConnectorConnection, error) {
	var connection ConnectorConnection
	var scopesRaw, metadataRaw string
	var connectedAt, lastSyncAt, expiresAt, disconnectedAt sql.NullTime
	err := row.Scan(
		&connection.ID, &connection.UserID, &connection.WorkspaceID, &connection.Provider, &connection.Status, &connection.PermissionPolicy,
		&scopesRaw, &connection.TokenRef, &connection.ExternalAccountID, &connection.ExternalAccountLabel, &metadataRaw,
		&connectedAt, &lastSyncAt, &expiresAt, &connection.CreatedAt, &connection.UpdatedAt, &disconnectedAt,
	)
	if err != nil {
		return ConnectorConnection{}, err
	}
	_ = json.Unmarshal([]byte(scopesRaw), &connection.Scopes)
	_ = json.Unmarshal([]byte(metadataRaw), &connection.Metadata)
	connection.ConnectedAt = nullTimePtr(connectedAt)
	connection.LastSyncAt = nullTimePtr(lastSyncAt)
	connection.ExpiresAt = nullTimePtr(expiresAt)
	connection.DisconnectedAt = nullTimePtr(disconnectedAt)
	return normalizeConnectorConnection(connection, time.Now().UTC()), nil
}

func scanConnectorOAuthState(row connectorScanner) (ConnectorOAuthState, error) {
	var state ConnectorOAuthState
	var scopesRaw, metadataRaw string
	var usedAt sql.NullTime
	err := row.Scan(&state.State, &state.UserID, &state.Provider, &scopesRaw, &state.RedirectURI, &metadataRaw, &state.CreatedAt, &state.ExpiresAt, &usedAt)
	if err != nil {
		return ConnectorOAuthState{}, err
	}
	_ = json.Unmarshal([]byte(scopesRaw), &state.Scopes)
	_ = json.Unmarshal([]byte(metadataRaw), &state.Metadata)
	state.UsedAt = nullTimePtr(usedAt)
	return normalizeConnectorOAuthState(state, time.Now().UTC()), nil
}

func DefaultConnectorProviders() []ConnectorProvider {
	providers := []ConnectorProvider{
		{
			ID:              "github",
			Name:            "GitHub",
			Description:     "Read repositories, issues, pull requests, and prepare reviewed code changes.",
			Category:        "code",
			AuthURL:         "https://github.com/login/oauth/authorize",
			TokenURL:        "https://github.com/login/oauth/access_token",
			ClientIDEnv:     "GITHUB_OAUTH_CLIENT_ID",
			ClientSecretEnv: "GITHUB_OAUTH_CLIENT_SECRET",
			Scopes:          []string{"repo", "read:user", "user:email"},
			Capabilities:    []string{"issue_context", "pull_request_context", "repository_read", "draft_write"},
			DefaultPolicy:   ConnectorPolicyWriteWithReview,
			ReviewByDefault: true,
			ConnectionKind:  MCPConnectionKindRemote,
		},
		{
			ID:              "google_drive",
			Name:            "Google Drive",
			Description:     "Bring files and folders into DeepAgent research context.",
			Category:        "documents",
			AuthURL:         "https://accounts.google.com/o/oauth2/v2/auth",
			TokenURL:        "https://oauth2.googleapis.com/token",
			ClientIDEnv:     "GOOGLE_OAUTH_CLIENT_ID",
			ClientSecretEnv: "GOOGLE_OAUTH_CLIENT_SECRET",
			Scopes:          []string{"openid", "email", "profile", "https://www.googleapis.com/auth/drive.readonly"},
			Capabilities:    []string{"file_context", "document_read"},
			DefaultPolicy:   ConnectorPolicyReadOnly,
			ReviewByDefault: true,
			ConnectionKind:  MCPConnectionKindRemote,
		},
		{
			ID:              "gmail",
			Name:            "Gmail",
			Description:     "Use email threads as private task evidence.",
			Category:        "communication",
			AuthURL:         "https://accounts.google.com/o/oauth2/v2/auth",
			TokenURL:        "https://oauth2.googleapis.com/token",
			ClientIDEnv:     "GOOGLE_OAUTH_CLIENT_ID",
			ClientSecretEnv: "GOOGLE_OAUTH_CLIENT_SECRET",
			Scopes:          []string{"openid", "email", "profile", "https://www.googleapis.com/auth/gmail.readonly", "https://www.googleapis.com/auth/gmail.compose"},
			Capabilities:    []string{"mail_context", "thread_read", "draft_write"},
			DefaultPolicy:   ConnectorPolicyReadOnly,
			ReviewByDefault: true,
			ConnectionKind:  MCPConnectionKindRemote,
		},
		{
			ID:              "notion",
			Name:            "Notion",
			Description:     "Use workspace pages and docs as planning context.",
			Category:        "documents",
			AuthURL:         "https://api.notion.com/v1/oauth/authorize",
			TokenURL:        "https://api.notion.com/v1/oauth/token",
			ClientIDEnv:     "NOTION_OAUTH_CLIENT_ID",
			ClientSecretEnv: "NOTION_OAUTH_CLIENT_SECRET",
			Scopes:          []string{"read_content"},
			Capabilities:    []string{"page_context", "database_context"},
			DefaultPolicy:   ConnectorPolicyReadOnly,
			ReviewByDefault: true,
			ConnectionKind:  MCPConnectionKindRemote,
		},
		{
			ID:              "slack",
			Name:            "Slack",
			Description:     "Use channel and thread context for team tasks.",
			Category:        "communication",
			AuthURL:         "https://slack.com/oauth/v2_user/authorize",
			TokenURL:        "https://slack.com/api/oauth.v2.user.access",
			ClientIDEnv:     "SLACK_CLIENT_ID",
			ClientSecretEnv: "SLACK_CLIENT_SECRET",
			Scopes: []string{
				"search:read.public", "search:read.private", "search:read.mpim", "search:read.im", "search:read.files", "search:read.users",
				"files:read", "emoji:read", "channels:history", "groups:history", "mpim:history", "im:history",
				"users:read", "users:read.email", "channels:read", "groups:read", "mpim:read",
			},
			Capabilities:    []string{"thread_context", "team_context"},
			DefaultPolicy:   ConnectorPolicyReadOnly,
			ReviewByDefault: true,
			ConnectionKind:  MCPConnectionKindRemote,
		},
		{
			ID:              "linear",
			Name:            "Linear",
			Description:     "Read issues and draft status updates with review.",
			Category:        "project",
			AuthURL:         "https://linear.app/oauth/authorize",
			TokenURL:        "https://api.linear.app/oauth/token",
			ClientIDEnv:     "LINEAR_CLIENT_ID",
			ClientSecretEnv: "LINEAR_CLIENT_SECRET",
			Scopes:          []string{"read", "write"},
			Capabilities:    []string{"issue_context", "draft_status_update"},
			DefaultPolicy:   ConnectorPolicyWriteWithReview,
			ReviewByDefault: true,
			ConnectionKind:  MCPConnectionKindRemote,
		},
	}
	for i := range providers {
		providers[i] = normalizeConnectorProvider(providers[i])
	}
	return providers
}

func (r *Runtime) SetConnectorStore(store ConnectorStore) {
	if r == nil {
		return
	}
	if store == nil {
		store = NewMemoryConnectorStore()
	}
	r.connectors = store
}

func (r *Runtime) SetConnectorTokenVault(vault ConnectorTokenVault) {
	if r == nil {
		return
	}
	if vault == nil {
		vault = NewMemoryConnectorTokenVault()
	}
	r.connectorTokens = vault
}

func (r *Runtime) ListConnectorStatus(ctx context.Context, userID, workspaceID string) ([]ConnectorStatus, error) {
	if r == nil {
		return []ConnectorStatus{}, nil
	}
	store := r.connectorStore()
	connections, err := store.ListConnections(ctx, userID, strings.TrimSpace(workspaceID))
	if err != nil {
		return nil, err
	}
	byProvider := make(map[string]ConnectorConnection, len(connections))
	for _, connection := range connections {
		byProvider[connection.Provider] = connection
	}
	providers := DefaultConnectorProviders()
	out := make([]ConnectorStatus, 0, len(providers))
	for _, provider := range providers {
		provider.Configured = connectorProviderConfigured(provider)
		var connectionPtr *ConnectorConnection
		var mcpServer *MCPServerBinding
		var mcpTools []MCPToolPolicy
		if connection, ok := byProvider[provider.ID]; ok {
			cloned := cloneConnectorConnection(connection)
			connectionPtr = &cloned
			server, serverErr := r.mcpConnectorStore().GetServer(ctx, userID, strings.TrimSpace(workspaceID), provider.ID)
			if serverErr != nil {
				return nil, serverErr
			}
			if connection.Status == ConnectorStatusConnected && connection.PermissionPolicy != ConnectorPolicyDisabled {
				created, ensureErr := r.ensureConnectorMCPBinding(ctx, connection)
				if ensureErr != nil {
					return nil, ensureErr
				}
				server = &created
			}
			if server != nil {
				serverClone := cloneMCPServerBinding(*server)
				mcpServer = &serverClone
				policies, policyErr := r.mcpConnectorStore().ListToolPolicies(ctx, userID, strings.TrimSpace(workspaceID), server.ID)
				if policyErr != nil {
					return nil, policyErr
				}
				mcpTools = make([]MCPToolPolicy, 0, len(policies))
				discoveredTools := mcpToolNamesFromServerMetadata(serverClone)
				for _, policy := range policies {
					if len(discoveredTools) > 0 && !discoveredTools[policy.ToolName] {
						continue
					}
					mcpTools = append(mcpTools, cloneMCPToolPolicy(policy))
				}
			}
		}
		out = append(out, ConnectorStatus{
			Provider:   provider,
			Connection: connectionPtr,
			Context:    connectorContextHint(provider, connectionPtr),
			MCPServer:  mcpServer,
			MCPTools:   mcpTools,
		})
	}
	return out, nil
}

func mcpToolNamesFromServerMetadata(server MCPServerBinding) map[string]bool {
	raw, ok := server.Metadata["tool_names"]
	if !ok {
		return nil
	}
	out := map[string]bool{}
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value != "" {
			out[value] = true
		}
	}
	switch typed := raw.(type) {
	case []string:
		for _, value := range typed {
			add(value)
		}
	case []any:
		for _, item := range typed {
			if value, ok := item.(string); ok {
				add(value)
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (r *Runtime) StartConnectorAuth(ctx context.Context, userID, workspaceID, providerID, redirectURI string, requestedScopes []string) (ConnectorAuthStart, error) {
	provider, ok := connectorProviderByID(providerID)
	if !ok {
		return ConnectorAuthStart{}, fmt.Errorf("connector provider not found: %s", providerID)
	}
	now := time.Now().UTC()
	scopes := normalizeConnectorScopes(firstNonEmptyConnectorStringSlice(requestedScopes, provider.Scopes))
	state := ConnectorOAuthState{
		State:       "coauth-" + newSortableID(),
		UserID:      strings.TrimSpace(userID),
		Provider:    provider.ID,
		Scopes:      scopes,
		RedirectURI: strings.TrimSpace(redirectURI),
		CreatedAt:   now,
		ExpiresAt:   now.Add(15 * time.Minute),
	}
	authURL := ""
	if provider.ID == "notion" && notionUsesOfficialMCPAuth() {
		prepared, err := r.prepareNotionMCPOAuth(ctx, state)
		if err != nil {
			return ConnectorAuthStart{}, err
		}
		state = prepared.State
		authURL = prepared.AuthURL
	}
	if err := r.connectorStore().CreateOAuthState(ctx, state); err != nil {
		return ConnectorAuthStart{}, err
	}
	if authURL == "" {
		authURL = connectorAuthURL(provider, state)
	}
	return ConnectorAuthStart{
		Provider:    provider.ID,
		State:       state.State,
		AuthURL:     authURL,
		Scopes:      scopes,
		Configured:  connectorProviderConfigured(provider),
		ExpiresAt:   state.ExpiresAt,
		RedirectURI: state.RedirectURI,
	}, nil
}

func (r *Runtime) CompleteConnectorAuth(ctx context.Context, userID, workspaceID, providerID, state, code, externalAccountID, externalAccountLabel string, scopes []string) (ConnectorConnection, error) {
	provider, ok := connectorProviderByID(providerID)
	if !ok {
		return ConnectorConnection{}, fmt.Errorf("connector provider not found: %s", providerID)
	}
	now := time.Now().UTC()
	oauthState, err := r.connectorStore().ConsumeOAuthState(ctx, userID, provider.ID, state, now)
	if err != nil {
		return ConnectorConnection{}, err
	}
	if strings.TrimSpace(code) == "" {
		return ConnectorConnection{}, fmt.Errorf("connector OAuth code is required")
	}
	selectedScopes := normalizeConnectorScopes(firstNonEmptyConnectorStringSlice(scopes, oauthState.Scopes))
	token, account, exchangeErr := r.exchangeConnectorOAuthCode(ctx, provider, *oauthState, code, selectedScopes)
	if exchangeErr != nil {
		return ConnectorConnection{}, exchangeErr
	}
	if len(token.Scopes) > 0 {
		selectedScopes = token.Scopes
	}
	connection := ConnectorConnection{
		ID:                   "conn-" + newSortableID(),
		UserID:               strings.TrimSpace(userID),
		WorkspaceID:          strings.TrimSpace(workspaceID),
		Provider:             provider.ID,
		Status:               ConnectorStatusConnected,
		PermissionPolicy:     normalizeConnectorPolicy(provider.DefaultPolicy),
		Scopes:               selectedScopes,
		TokenRef:             token.Ref,
		ExternalAccountID:    firstNonEmptyString(externalAccountID, account.ID),
		ExternalAccountLabel: firstNonEmptyString(externalAccountLabel, account.Label),
		ConnectedAt:          &now,
		LastSyncAt:           &now,
		ExpiresAt:            token.ExpiresAt,
		CreatedAt:            now,
		UpdatedAt:            now,
		Metadata: map[string]any{
			"token_storage":      "vault_ref",
			"write_review_mode":  provider.ReviewByDefault,
			"oauth_state_issued": oauthState.CreatedAt.Format(time.RFC3339),
			"oauth_exchange":     token.AccessToken != "",
		},
	}
	if connection.ExternalAccountLabel == "" {
		connection.ExternalAccountLabel = provider.Name + " account"
	}
	if provider.ID == "notion" && oauthState.Metadata != nil && deepAgentWorkflowString(oauthState.Metadata, "oauth_mode") == "mcp" {
		connection.Metadata["oauth_mode"] = "mcp"
		connection.Metadata["oauth_client_id"] = deepAgentWorkflowString(oauthState.Metadata, "client_id")
		connection.Metadata["token_endpoint"] = deepAgentWorkflowString(oauthState.Metadata, "token_endpoint")
		connection.Metadata["authorization_endpoint"] = deepAgentWorkflowString(oauthState.Metadata, "authorization_endpoint")
		connection.Metadata["resource"] = deepAgentWorkflowString(oauthState.Metadata, "resource")
	}
	connection, err = r.connectorStore().UpsertConnection(ctx, connection)
	if err != nil {
		return ConnectorConnection{}, err
	}
	if _, err := r.ensureConnectorMCPBinding(ctx, connection); err != nil {
		return ConnectorConnection{}, err
	}
	return connection, nil
}

func (r *Runtime) UpdateConnectorPolicy(ctx context.Context, userID, workspaceID, providerID, policy string) (ConnectorConnection, error) {
	providerID = normalizeConnectorProviderID(providerID)
	policy = normalizeConnectorPolicy(policy)
	connection, err := r.connectorStore().GetConnection(ctx, userID, strings.TrimSpace(workspaceID), providerID)
	if err != nil {
		return ConnectorConnection{}, err
	}
	if connection == nil {
		return ConnectorConnection{}, fmt.Errorf("connector connection not found")
	}
	connection.PermissionPolicy = policy
	connection.UpdatedAt = time.Now().UTC()
	if policy == ConnectorPolicyDisabled {
		connection.Status = ConnectorStatusDisabled
	}
	return r.connectorStore().UpsertConnection(ctx, *connection)
}

func (r *Runtime) DisconnectConnector(ctx context.Context, userID, workspaceID, providerID string) error {
	connection, _ := r.connectorStore().GetConnection(ctx, userID, strings.TrimSpace(workspaceID), normalizeConnectorProviderID(providerID))
	if connection != nil && strings.TrimSpace(connection.TokenRef) != "" {
		_ = r.connectorTokenVault().DeleteToken(ctx, connection.TokenRef)
	}
	at := time.Now().UTC()
	_ = r.mcpConnectorStore().DisableServer(ctx, userID, strings.TrimSpace(workspaceID), normalizeConnectorProviderID(providerID), at)
	return r.connectorStore().DisconnectConnection(ctx, userID, strings.TrimSpace(workspaceID), normalizeConnectorProviderID(providerID), at)
}

type connectorExternalAccount struct {
	ID    string
	Label string
}

type connectorOAuthTokenResponse struct {
	AccessToken           string `json:"access_token"`
	TokenType             string `json:"token_type"`
	Scope                 any    `json:"scope"`
	RefreshToken          string `json:"refresh_token"`
	ExpiresIn             int64  `json:"expires_in"`
	RefreshTokenExpiresIn int64  `json:"refresh_token_expires_in"`
	Error                 string `json:"error"`
	ErrorDescription      string `json:"error_description"`
}

func (r *Runtime) exchangeConnectorOAuthCode(ctx context.Context, provider ConnectorProvider, state ConnectorOAuthState, code string, scopes []string) (ConnectorToken, connectorExternalAccount, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return ConnectorToken{}, connectorExternalAccount{}, fmt.Errorf("connector OAuth code is required")
	}
	if strings.HasPrefix(code, "local-dev-") || !connectorProviderConfigured(provider) {
		return r.exchangeLocalConnectorCode(ctx, provider, code, scopes)
	}
	switch provider.ID {
	case "github":
		return r.exchangeGitHubOAuthCode(ctx, provider, state, code)
	case "google_drive", "gmail":
		token, account, err := r.exchangeFormConnectorOAuthCode(ctx, provider, state, code, scopes)
		if err != nil {
			return ConnectorToken{}, connectorExternalAccount{}, err
		}
		if fetched := r.fetchGoogleAccount(ctx, token); fetched.ID != "" || fetched.Label != "" {
			account = fetched
		}
		return token, account, nil
	case "linear":
		token, account, err := r.exchangeFormConnectorOAuthCode(ctx, provider, state, code, scopes)
		if err != nil {
			return ConnectorToken{}, connectorExternalAccount{}, err
		}
		if fetched := r.fetchLinearAccount(ctx, token); fetched.ID != "" || fetched.Label != "" {
			account = fetched
		}
		return token, account, nil
	case "slack":
		return r.exchangeSlackOAuthCode(ctx, provider, state, code, scopes)
	case "notion":
		if state.Metadata != nil && deepAgentWorkflowString(state.Metadata, "oauth_mode") == "mcp" {
			return r.exchangeNotionMCPOAuthCode(ctx, provider, state, code, scopes)
		}
		return r.exchangeNotionOAuthCode(ctx, provider, state, code, scopes)
	default:
		return r.exchangeFormConnectorOAuthCode(ctx, provider, state, code, scopes)
	}
}

func (r *Runtime) exchangeLocalConnectorCode(ctx context.Context, provider ConnectorProvider, code string, scopes []string) (ConnectorToken, connectorExternalAccount, error) {
	now := time.Now().UTC()
	token := ConnectorToken{
		Ref:         connectorTokenRef(provider.ID, code),
		Provider:    provider.ID,
		AccessToken: code,
		TokenType:   "bearer",
		Scopes:      normalizeConnectorScopes(scopes),
		UpdatedAt:   now,
	}
	if err := r.connectorTokenVault().PutToken(ctx, token); err != nil {
		return ConnectorToken{}, connectorExternalAccount{}, err
	}
	return token, connectorExternalAccount{Label: provider.Name + " account"}, nil
}

func (r *Runtime) exchangeGitHubOAuthCode(ctx context.Context, provider ConnectorProvider, state ConnectorOAuthState, code string) (ConnectorToken, connectorExternalAccount, error) {
	values := url.Values{}
	values.Set("client_id", strings.TrimSpace(os.Getenv(provider.ClientIDEnv)))
	values.Set("client_secret", strings.TrimSpace(os.Getenv(provider.ClientSecretEnv)))
	values.Set("code", strings.TrimSpace(code))
	if redirectURI := strings.TrimSpace(state.RedirectURI); redirectURI != "" {
		values.Set("redirect_uri", redirectURI)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, connectorTokenURL(provider), strings.NewReader(values.Encode()))
	if err != nil {
		return ConnectorToken{}, connectorExternalAccount{}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ConnectorToken{}, connectorExternalAccount{}, err
	}
	defer resp.Body.Close()
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if readErr != nil {
		return ConnectorToken{}, connectorExternalAccount{}, readErr
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ConnectorToken{}, connectorExternalAccount{}, fmt.Errorf("github OAuth token exchange failed: status %d: %s", resp.StatusCode, truncateDeepAgentDiagnosticText(string(body), 600))
	}
	var parsed connectorOAuthTokenResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return ConnectorToken{}, connectorExternalAccount{}, err
	}
	if parsed.Error != "" {
		return ConnectorToken{}, connectorExternalAccount{}, fmt.Errorf("github OAuth token exchange failed: %s %s", parsed.Error, parsed.ErrorDescription)
	}
	if strings.TrimSpace(parsed.AccessToken) == "" {
		return ConnectorToken{}, connectorExternalAccount{}, fmt.Errorf("github OAuth token exchange returned no access token")
	}
	now := time.Now().UTC()
	token := ConnectorToken{
		Ref:          connectorTokenRef(provider.ID, parsed.AccessToken),
		Provider:     provider.ID,
		AccessToken:  parsed.AccessToken,
		RefreshToken: parsed.RefreshToken,
		TokenType:    firstNonEmptyString(parsed.TokenType, "bearer"),
		Scopes:       connectorOAuthResponseScopes(parsed.Scope, nil),
		UpdatedAt:    now,
	}
	if parsed.ExpiresIn > 0 {
		expires := now.Add(time.Duration(parsed.ExpiresIn) * time.Second)
		token.ExpiresAt = &expires
	}
	if parsed.RefreshTokenExpiresIn > 0 {
		expires := now.Add(time.Duration(parsed.RefreshTokenExpiresIn) * time.Second)
		token.RefreshExpiresAt = &expires
	}
	if len(token.Scopes) == 0 {
		token.Scopes = normalizeConnectorScopes(state.Scopes)
	}
	if err := r.connectorTokenVault().PutToken(ctx, token); err != nil {
		return ConnectorToken{}, connectorExternalAccount{}, err
	}
	account := r.fetchGitHubAccount(ctx, token)
	return token, account, nil
}

func (r *Runtime) exchangeFormConnectorOAuthCode(ctx context.Context, provider ConnectorProvider, state ConnectorOAuthState, code string, scopes []string) (ConnectorToken, connectorExternalAccount, error) {
	values := url.Values{}
	values.Set("client_id", strings.TrimSpace(os.Getenv(provider.ClientIDEnv)))
	values.Set("client_secret", strings.TrimSpace(os.Getenv(provider.ClientSecretEnv)))
	values.Set("grant_type", "authorization_code")
	values.Set("code", strings.TrimSpace(code))
	if redirectURI := strings.TrimSpace(state.RedirectURI); redirectURI != "" {
		values.Set("redirect_uri", redirectURI)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, connectorTokenURL(provider), strings.NewReader(values.Encode()))
	if err != nil {
		return ConnectorToken{}, connectorExternalAccount{}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	token, err := r.doConnectorTokenRequest(ctx, provider, state, req, scopes)
	if err != nil {
		return ConnectorToken{}, connectorExternalAccount{}, err
	}
	return token, connectorExternalAccount{Label: provider.Name + " account"}, nil
}

func (r *Runtime) exchangeSlackOAuthCode(ctx context.Context, provider ConnectorProvider, state ConnectorOAuthState, code string, scopes []string) (ConnectorToken, connectorExternalAccount, error) {
	values := url.Values{}
	values.Set("client_id", strings.TrimSpace(os.Getenv(provider.ClientIDEnv)))
	values.Set("client_secret", strings.TrimSpace(os.Getenv(provider.ClientSecretEnv)))
	values.Set("code", strings.TrimSpace(code))
	if redirectURI := strings.TrimSpace(state.RedirectURI); redirectURI != "" {
		values.Set("redirect_uri", redirectURI)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, connectorTokenURL(provider), strings.NewReader(values.Encode()))
	if err != nil {
		return ConnectorToken{}, connectorExternalAccount{}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ConnectorToken{}, connectorExternalAccount{}, err
	}
	defer resp.Body.Close()
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if readErr != nil {
		return ConnectorToken{}, connectorExternalAccount{}, readErr
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ConnectorToken{}, connectorExternalAccount{}, fmt.Errorf("slack OAuth token exchange failed: status %d: %s", resp.StatusCode, truncateDeepAgentDiagnosticText(string(body), 600))
	}
	var parsed struct {
		OK          bool   `json:"ok"`
		Error       string `json:"error"`
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		Scope       string `json:"scope"`
		AuthedUser  struct {
			ID          string `json:"id"`
			AccessToken string `json:"access_token"`
			TokenType   string `json:"token_type"`
			Scope       string `json:"scope"`
		} `json:"authed_user"`
		Team struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"team"`
		Enterprise struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"enterprise"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return ConnectorToken{}, connectorExternalAccount{}, err
	}
	if !parsed.OK || parsed.Error != "" {
		return ConnectorToken{}, connectorExternalAccount{}, fmt.Errorf("slack OAuth token exchange failed: %s", firstNonEmptyString(parsed.Error, "not_ok"))
	}
	response := connectorOAuthTokenResponse{
		AccessToken: firstNonEmptyString(parsed.AuthedUser.AccessToken, parsed.AccessToken),
		TokenType:   firstNonEmptyString(parsed.AuthedUser.TokenType, parsed.TokenType, "Bearer"),
		Scope:       firstNonEmptyString(parsed.AuthedUser.Scope, parsed.Scope),
	}
	token, err := r.storeConnectorOAuthToken(ctx, provider, state, response, scopes)
	if err != nil {
		return ConnectorToken{}, connectorExternalAccount{}, err
	}
	account := connectorExternalAccount{
		ID:    firstNonEmptyString(parsed.AuthedUser.ID, parsed.Team.ID, parsed.Enterprise.ID),
		Label: firstNonEmptyString(parsed.Team.Name, parsed.Enterprise.Name, provider.Name+" workspace"),
	}
	return token, account, nil
}

func (r *Runtime) exchangeNotionOAuthCode(ctx context.Context, provider ConnectorProvider, state ConnectorOAuthState, code string, scopes []string) (ConnectorToken, connectorExternalAccount, error) {
	payload := map[string]string{
		"grant_type": "authorization_code",
		"code":       strings.TrimSpace(code),
	}
	if redirectURI := strings.TrimSpace(state.RedirectURI); redirectURI != "" {
		payload["redirect_uri"] = redirectURI
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return ConnectorToken{}, connectorExternalAccount{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, connectorTokenURL(provider), strings.NewReader(string(body)))
	if err != nil {
		return ConnectorToken{}, connectorExternalAccount{}, err
	}
	clientID := strings.TrimSpace(os.Getenv(provider.ClientIDEnv))
	clientSecret := strings.TrimSpace(os.Getenv(provider.ClientSecretEnv))
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(clientID+":"+clientSecret)))
	req.Header.Set("Notion-Version", firstNonEmptyString(strings.TrimSpace(os.Getenv("NOTION_API_VERSION")), "2022-06-28"))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ConnectorToken{}, connectorExternalAccount{}, err
	}
	defer resp.Body.Close()
	respBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if readErr != nil {
		return ConnectorToken{}, connectorExternalAccount{}, readErr
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ConnectorToken{}, connectorExternalAccount{}, fmt.Errorf("notion OAuth token exchange failed: status %d: %s", resp.StatusCode, truncateDeepAgentDiagnosticText(string(respBody), 600))
	}
	var parsed struct {
		AccessToken   string `json:"access_token"`
		TokenType     string `json:"token_type"`
		BotID         string `json:"bot_id"`
		WorkspaceID   string `json:"workspace_id"`
		WorkspaceName string `json:"workspace_name"`
		Error         string `json:"error"`
		Code          string `json:"code"`
		Message       string `json:"message"`
		Owner         struct {
			User struct {
				ID     string `json:"id"`
				Name   string `json:"name"`
				Person struct {
					Email string `json:"email"`
				} `json:"person"`
			} `json:"user"`
		} `json:"owner"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return ConnectorToken{}, connectorExternalAccount{}, err
	}
	if parsed.Error != "" || parsed.Code != "" {
		return ConnectorToken{}, connectorExternalAccount{}, fmt.Errorf("notion OAuth token exchange failed: %s %s", firstNonEmptyString(parsed.Error, parsed.Code), parsed.Message)
	}
	response := connectorOAuthTokenResponse{
		AccessToken: parsed.AccessToken,
		TokenType:   parsed.TokenType,
	}
	token, err := r.storeConnectorOAuthToken(ctx, provider, state, response, scopes)
	if err != nil {
		return ConnectorToken{}, connectorExternalAccount{}, err
	}
	account := connectorExternalAccount{
		ID:    firstNonEmptyString(parsed.WorkspaceID, parsed.BotID, parsed.Owner.User.ID),
		Label: firstNonEmptyString(parsed.WorkspaceName, parsed.Owner.User.Person.Email, parsed.Owner.User.Name, provider.Name+" workspace"),
	}
	return token, account, nil
}

type notionMCPOAuthStart struct {
	State   ConnectorOAuthState
	AuthURL string
}

func (r *Runtime) prepareNotionMCPOAuth(ctx context.Context, state ConnectorOAuthState) (notionMCPOAuthStart, error) {
	redirectURI := strings.TrimSpace(state.RedirectURI)
	if redirectURI == "" {
		return notionMCPOAuthStart{}, fmt.Errorf("notion MCP OAuth requires redirect_uri")
	}
	metadata, err := r.notionMCPAuthorizationServerMetadata(ctx)
	if err != nil {
		return notionMCPOAuthStart{}, err
	}
	codeVerifier, codeChallenge, err := newConnectorPKCEPair()
	if err != nil {
		return notionMCPOAuthStart{}, err
	}
	clientID, err := r.registerNotionMCPClient(ctx, metadata.RegistrationEndpoint, redirectURI)
	if err != nil {
		return notionMCPOAuthStart{}, err
	}
	resource := firstNonEmptyString(strings.TrimSpace(mcpProviderDefaultURL("notion")), "https://mcp.notion.com/mcp")
	values := url.Values{}
	values.Set("response_type", "code")
	values.Set("client_id", clientID)
	values.Set("redirect_uri", redirectURI)
	values.Set("state", state.State)
	values.Set("code_challenge", codeChallenge)
	values.Set("code_challenge_method", "S256")
	values.Set("resource", resource)
	authURL := metadata.AuthorizationEndpoint
	if strings.Contains(authURL, "?") {
		authURL += "&" + values.Encode()
	} else {
		authURL += "?" + values.Encode()
	}
	state.Scopes = nil
	state.Metadata = map[string]any{
		"oauth_mode":             "mcp",
		"client_id":              clientID,
		"code_verifier":          codeVerifier,
		"token_endpoint":         metadata.TokenEndpoint,
		"authorization_endpoint": metadata.AuthorizationEndpoint,
		"resource":               resource,
	}
	return notionMCPOAuthStart{State: state, AuthURL: authURL}, nil
}

type notionMCPOAuthMetadata struct {
	AuthorizationEndpoint string
	TokenEndpoint         string
	RegistrationEndpoint  string
}

func (r *Runtime) notionMCPAuthorizationServerMetadata(ctx context.Context) (notionMCPOAuthMetadata, error) {
	if authURL := strings.TrimSpace(os.Getenv("NOTION_MCP_AUTHORIZATION_URL")); authURL != "" {
		return notionMCPOAuthMetadata{
			AuthorizationEndpoint: authURL,
			TokenEndpoint:         firstNonEmptyString(strings.TrimSpace(os.Getenv("NOTION_MCP_TOKEN_URL")), "https://mcp.notion.com/token"),
			RegistrationEndpoint:  firstNonEmptyString(strings.TrimSpace(os.Getenv("NOTION_MCP_REGISTRATION_URL")), "https://mcp.notion.com/register"),
		}, nil
	}
	metadataURL := firstNonEmptyString(strings.TrimSpace(os.Getenv("NOTION_MCP_AUTHORIZATION_SERVER_METADATA_URL")), "https://mcp.notion.com/.well-known/oauth-authorization-server")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, metadataURL, nil)
	if err != nil {
		return notionMCPOAuthMetadata{}, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return notionMCPOAuthMetadata{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 128*1024))
	if err != nil {
		return notionMCPOAuthMetadata{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return notionMCPOAuthMetadata{}, fmt.Errorf("notion MCP OAuth metadata failed: status %d: %s", resp.StatusCode, truncateDeepAgentDiagnosticText(string(body), 600))
	}
	var parsed struct {
		AuthorizationEndpoint string `json:"authorization_endpoint"`
		TokenEndpoint         string `json:"token_endpoint"`
		RegistrationEndpoint  string `json:"registration_endpoint"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return notionMCPOAuthMetadata{}, err
	}
	if strings.TrimSpace(parsed.AuthorizationEndpoint) == "" || strings.TrimSpace(parsed.TokenEndpoint) == "" || strings.TrimSpace(parsed.RegistrationEndpoint) == "" {
		return notionMCPOAuthMetadata{}, fmt.Errorf("notion MCP OAuth metadata is missing required endpoints")
	}
	return notionMCPOAuthMetadata{
		AuthorizationEndpoint: strings.TrimSpace(parsed.AuthorizationEndpoint),
		TokenEndpoint:         strings.TrimSpace(parsed.TokenEndpoint),
		RegistrationEndpoint:  strings.TrimSpace(parsed.RegistrationEndpoint),
	}, nil
}

func (r *Runtime) registerNotionMCPClient(ctx context.Context, endpoint, redirectURI string) (string, error) {
	payload := map[string]any{
		"client_name":                "claude-codex",
		"redirect_uris":              []string{redirectURI},
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
		"token_endpoint_auth_method": "none",
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(body)))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 128*1024))
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("notion MCP dynamic client registration failed: status %d: %s", resp.StatusCode, truncateDeepAgentDiagnosticText(string(respBody), 600))
	}
	var parsed struct {
		ClientID string `json:"client_id"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", err
	}
	if strings.TrimSpace(parsed.ClientID) == "" {
		return "", fmt.Errorf("notion MCP dynamic client registration returned no client_id")
	}
	return strings.TrimSpace(parsed.ClientID), nil
}

func (r *Runtime) exchangeNotionMCPOAuthCode(ctx context.Context, provider ConnectorProvider, state ConnectorOAuthState, code string, scopes []string) (ConnectorToken, connectorExternalAccount, error) {
	values := url.Values{}
	values.Set("grant_type", "authorization_code")
	values.Set("code", code)
	values.Set("redirect_uri", state.RedirectURI)
	values.Set("client_id", deepAgentWorkflowString(state.Metadata, "client_id"))
	values.Set("code_verifier", deepAgentWorkflowString(state.Metadata, "code_verifier"))
	if resource := deepAgentWorkflowString(state.Metadata, "resource"); resource != "" {
		values.Set("resource", resource)
	}
	tokenEndpoint := firstNonEmptyString(deepAgentWorkflowString(state.Metadata, "token_endpoint"), "https://mcp.notion.com/token")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenEndpoint, strings.NewReader(values.Encode()))
	if err != nil {
		return ConnectorToken{}, connectorExternalAccount{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	token, err := r.doConnectorTokenRequest(ctx, provider, state, req, scopes)
	if err != nil {
		return ConnectorToken{}, connectorExternalAccount{}, err
	}
	return token, connectorExternalAccount{ID: "notion-mcp", Label: "Notion MCP"}, nil
}

func (r *Runtime) doConnectorTokenRequest(_ context.Context, provider ConnectorProvider, state ConnectorOAuthState, req *http.Request, scopes []string) (ConnectorToken, error) {
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ConnectorToken{}, err
	}
	defer resp.Body.Close()
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if readErr != nil {
		return ConnectorToken{}, readErr
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ConnectorToken{}, fmt.Errorf("%s OAuth token exchange failed: status %d: %s", provider.ID, resp.StatusCode, truncateDeepAgentDiagnosticText(string(body), 600))
	}
	var parsed connectorOAuthTokenResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return ConnectorToken{}, err
	}
	if parsed.Error != "" {
		return ConnectorToken{}, fmt.Errorf("%s OAuth token exchange failed: %s %s", provider.ID, parsed.Error, parsed.ErrorDescription)
	}
	return r.storeConnectorOAuthToken(req.Context(), provider, state, parsed, scopes)
}

func (r *Runtime) storeConnectorOAuthToken(ctx context.Context, provider ConnectorProvider, state ConnectorOAuthState, parsed connectorOAuthTokenResponse, scopes []string) (ConnectorToken, error) {
	if strings.TrimSpace(parsed.AccessToken) == "" {
		return ConnectorToken{}, fmt.Errorf("%s OAuth token exchange returned no access token", provider.ID)
	}
	now := time.Now().UTC()
	token := ConnectorToken{
		Ref:          connectorTokenRef(provider.ID, parsed.AccessToken),
		Provider:     provider.ID,
		AccessToken:  parsed.AccessToken,
		RefreshToken: parsed.RefreshToken,
		TokenType:    firstNonEmptyString(parsed.TokenType, "bearer"),
		Scopes:       connectorOAuthResponseScopes(parsed.Scope, scopes),
		UpdatedAt:    now,
	}
	if parsed.ExpiresIn > 0 {
		expires := now.Add(time.Duration(parsed.ExpiresIn) * time.Second)
		token.ExpiresAt = &expires
	}
	if parsed.RefreshTokenExpiresIn > 0 {
		expires := now.Add(time.Duration(parsed.RefreshTokenExpiresIn) * time.Second)
		token.RefreshExpiresAt = &expires
	}
	if len(token.Scopes) == 0 {
		token.Scopes = normalizeConnectorScopes(state.Scopes)
	}
	if err := r.connectorTokenVault().PutToken(ctx, token); err != nil {
		return ConnectorToken{}, err
	}
	return token, nil
}

func (r *Runtime) fetchGitHubAccount(ctx context.Context, token ConnectorToken) connectorExternalAccount {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(connectorGitHubAPIBaseURL(), "/")+"/user", nil)
	if err != nil {
		return connectorExternalAccount{}
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", connectorAuthorizationHeader(token))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return connectorExternalAccount{}
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return connectorExternalAccount{}
	}
	var payload struct {
		ID    int64  `json:"id"`
		Login string `json:"login"`
		Name  string `json:"name"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64*1024)).Decode(&payload); err != nil {
		return connectorExternalAccount{}
	}
	return connectorExternalAccount{
		ID:    fmt.Sprint(payload.ID),
		Label: firstNonEmptyString(payload.Login, payload.Name),
	}
}

func (r *Runtime) fetchGoogleAccount(ctx context.Context, token ConnectorToken) connectorExternalAccount {
	userInfoURL := strings.TrimSpace(firstNonEmptyString(os.Getenv("GOOGLE_OAUTH_USERINFO_URL"), "https://openidconnect.googleapis.com/v1/userinfo"))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, userInfoURL, nil)
	if err != nil {
		return connectorExternalAccount{}
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", connectorAuthorizationHeader(token))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return connectorExternalAccount{}
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return connectorExternalAccount{}
	}
	var payload struct {
		Sub   string `json:"sub"`
		Email string `json:"email"`
		Name  string `json:"name"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64*1024)).Decode(&payload); err != nil {
		return connectorExternalAccount{}
	}
	return connectorExternalAccount{
		ID:    payload.Sub,
		Label: firstNonEmptyString(payload.Email, payload.Name),
	}
}

func (r *Runtime) fetchLinearAccount(ctx context.Context, token ConnectorToken) connectorExternalAccount {
	graphQLURL := strings.TrimSpace(firstNonEmptyString(os.Getenv("LINEAR_GRAPHQL_URL"), os.Getenv("LINEAR_API_URL"), "https://api.linear.app/graphql"))
	body := strings.NewReader(`{"query":"query ConnectorViewer { viewer { id name email } }"}`)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, graphQLURL, body)
	if err != nil {
		return connectorExternalAccount{}
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", connectorAuthorizationHeader(token))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return connectorExternalAccount{}
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return connectorExternalAccount{}
	}
	var payload struct {
		Data struct {
			Viewer struct {
				ID    string `json:"id"`
				Name  string `json:"name"`
				Email string `json:"email"`
			} `json:"viewer"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64*1024)).Decode(&payload); err != nil {
		return connectorExternalAccount{}
	}
	return connectorExternalAccount{
		ID:    payload.Data.Viewer.ID,
		Label: firstNonEmptyString(payload.Data.Viewer.Email, payload.Data.Viewer.Name),
	}
}

func (r *Runtime) connectorContextPrompt(ctx context.Context, req ChatRequest) string {
	lines := r.connectorContextLines(ctx, req)
	if strings.TrimSpace(lines) == "" {
		return ""
	}
	return PromptConnectorContextHeader + "\n" + lines + "\n" + PromptConnectorContextSuffix
}

func (r *Runtime) connectorContextLines(ctx context.Context, req ChatRequest) string {
	selected := normalizeConnectorScopes(req.ConnectorContext)
	if len(selected) == 0 || r == nil {
		return ""
	}
	want := make(map[string]bool, len(selected))
	for _, provider := range selected {
		want[normalizeConnectorProviderID(provider)] = true
	}
	statuses, err := r.ListConnectorStatus(ctx, req.UserID, "")
	if err != nil {
		return ""
	}
	var lines []string
	for _, status := range statuses {
		if !want[status.Provider.ID] || status.Connection == nil || status.Connection.Status != ConnectorStatusConnected || status.Connection.PermissionPolicy == ConnectorPolicyDisabled {
			continue
		}
		label := strings.TrimSpace(status.Connection.ExternalAccountLabel)
		if label == "" {
			label = status.Provider.Name
		}
		mcpHint := "mcp_server=unavailable"
		if status.MCPServer != nil {
			if status.MCPServer.Status == MCPServerStatusConnected && len(status.MCPTools) > 0 {
				toolNames := make([]string, 0, len(status.MCPTools))
				for _, tool := range status.MCPTools {
					if strings.TrimSpace(tool.ToolName) != "" && tool.Allowed && tool.PermissionPolicy != ConnectorPolicyDisabled {
						toolNames = append(toolNames, connectorRuntimeToolName(status.Provider.ID, tool.ToolName))
					}
				}
				if len(toolNames) > 0 {
					mcpHint = fmt.Sprintf("mcp_server=%s; mcp_tools=%s", status.MCPServer.Transport, strings.Join(toolNames, ", "))
				}
			} else if reason := deepAgentWorkflowString(status.MCPServer.Metadata, "last_discovery_error"); reason != "" {
				mcpHint = "mcp_server=unavailable; reason=" + reason
			} else {
				mcpHint = "mcp_server=unavailable; status=" + status.MCPServer.Status
			}
		}
		lines = append(lines, fmt.Sprintf("- %s: %s; policy=%s; evidence=%s; %s", status.Provider.Name, label, status.Connection.PermissionPolicy, strings.Join(status.Context.Evidence, ", "), mcpHint))
	}
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n")
}

func (r *Runtime) resolveConnectorContext(ctx context.Context, req ChatRequest) []string {
	explicit := normalizeConnectorScopes(req.ConnectorContext)
	if len(explicit) > 0 {
		return explicit
	}
	return r.inferConnectorContext(ctx, req.UserID, req.Content)
}

func (r *Runtime) inferConnectorContext(ctx context.Context, userID, content string) []string {
	if r == nil || strings.TrimSpace(userID) == "" || strings.TrimSpace(content) == "" {
		return nil
	}
	statuses, err := r.ListConnectorStatus(ctx, userID, "")
	if err != nil {
		return nil
	}
	text := strings.ToLower(content)
	tokens := connectorIntentTokens(text)
	type candidate struct {
		provider string
		score    int
	}
	var candidates []candidate
	for _, status := range statuses {
		if status.Connection == nil || status.Connection.Status != ConnectorStatusConnected || status.Connection.PermissionPolicy == ConnectorPolicyDisabled {
			continue
		}
		providerID := normalizeConnectorProviderID(status.Provider.ID)
		score := connectorIntentScore(providerID, strings.ToLower(status.Provider.Name), text, tokens)
		if score > 0 {
			candidates = append(candidates, candidate{provider: providerID, score: score})
		}
	}
	if len(candidates) == 0 {
		return nil
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].score == candidates[j].score {
			return candidates[i].provider < candidates[j].provider
		}
		return candidates[i].score > candidates[j].score
	})
	maxScore := candidates[0].score
	var out []string
	for _, item := range candidates {
		if len(out) >= 3 {
			break
		}
		if maxScore >= 8 {
			if item.score < 8 {
				break
			}
		} else if item.score < maxScore {
			break
		}
		out = append(out, item.provider)
	}
	return out
}

func connectorIntentScore(providerID, providerName, text string, tokens map[string]bool) int {
	score := 0
	if providerID != "" && connectorIntentContains(text, tokens, providerID) {
		score += 10
	}
	if providerName != "" && providerName != providerID && connectorIntentContains(text, tokens, providerName) {
		score += 10
	}
	switch providerID {
	case "github":
		score += connectorIntentTermScore(text, tokens, 6, "github")
		score += connectorIntentTermScore(text, tokens, 4, "repo", "repository", "仓库", "代码库", "public repo", "公有repo", "公开仓库", "pull request", "pr", "commit", "branch", "分支")
		score += connectorIntentTermScore(text, tokens, 2, "issue", "issues")
	case "gmail":
		score += connectorIntentTermScore(text, tokens, 6, "gmail")
		score += connectorIntentTermScore(text, tokens, 4, "email", "mail", "inbox", "邮件", "邮箱", "收件箱", "新邮件", "未读邮件")
	case "google_drive":
		score += connectorIntentTermScore(text, tokens, 6, "google drive", "drive")
		score += connectorIntentTermScore(text, tokens, 4, "文件", "文件夹", "云盘", "网盘", "存储", "docs", "document")
		score += connectorIntentTermScore(text, tokens, 2, "文档")
	case "notion":
		score += connectorIntentTermScore(text, tokens, 6, "notion")
		score += connectorIntentTermScore(text, tokens, 4, "page", "database", "wiki", "页面", "知识库")
		score += connectorIntentTermScore(text, tokens, 2, "文档")
	case "slack":
		score += connectorIntentTermScore(text, tokens, 6, "slack")
		score += connectorIntentTermScore(text, tokens, 4, "channel", "thread", "workspace", "dm", "频道", "线程", "工作区", "群聊", "消息")
	case "linear":
		score += connectorIntentTermScore(text, tokens, 6, "linear")
		score += connectorIntentTermScore(text, tokens, 4, "ticket", "roadmap", "工单", "任务", "看板", "状态更新")
		score += connectorIntentTermScore(text, tokens, 2, "issue", "issues", "项目")
	}
	return score
}

func connectorIntentTermScore(text string, tokens map[string]bool, points int, terms ...string) int {
	for _, term := range terms {
		if connectorIntentContains(text, tokens, term) {
			return points
		}
	}
	return 0
}

func connectorIntentContains(text string, tokens map[string]bool, term string) bool {
	term = strings.ToLower(strings.TrimSpace(term))
	if term == "" {
		return false
	}
	if isASCIIWord(term) && !strings.Contains(term, " ") {
		return tokens[term]
	}
	return strings.Contains(text, term)
}

func connectorIntentTokens(text string) map[string]bool {
	tokens := map[string]bool{}
	var b strings.Builder
	flush := func() {
		if b.Len() == 0 {
			return
		}
		tokens[b.String()] = true
		b.Reset()
	}
	for _, r := range text {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			b.WriteRune(r)
			continue
		}
		flush()
	}
	flush()
	return tokens
}

func isASCIIWord(value string) bool {
	for _, r := range value {
		if r == ' ' {
			return false
		}
		if !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_') {
			return false
		}
	}
	return true
}

func (s *Server) handleListConnectors(w http.ResponseWriter, r *http.Request, user User) {
	workspaceID := strings.TrimSpace(r.URL.Query().Get("workspace_id"))
	statuses, err := s.runtime.ListConnectorStatus(r.Context(), user.ID, workspaceID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"connectors": statuses})
}

func (s *Server) handleStartConnectorAuth(w http.ResponseWriter, r *http.Request, user User, provider string) {
	var body struct {
		WorkspaceID string   `json:"workspace_id,omitempty"`
		RedirectURI string   `json:"redirect_uri,omitempty"`
		Scopes      []string `json:"scopes,omitempty"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSONError(w, err)
		return
	}
	start, err := s.runtime.StartConnectorAuth(r.Context(), user.ID, body.WorkspaceID, provider, body.RedirectURI, body.Scopes)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.auditEvent(r, "connector_start_auth", user, map[string]any{"provider": start.Provider, "configured": start.Configured})
	s.recordGovernanceEvent("connector_start_auth")
	writeJSON(w, http.StatusOK, map[string]any{"auth": start})
}

func (s *Server) handleCompleteConnectorAuth(w http.ResponseWriter, r *http.Request, user User, provider string) {
	var body struct {
		WorkspaceID          string   `json:"workspace_id,omitempty"`
		State                string   `json:"state"`
		Code                 string   `json:"code"`
		ExternalAccountID    string   `json:"external_account_id,omitempty"`
		ExternalAccountLabel string   `json:"external_account_label,omitempty"`
		Scopes               []string `json:"scopes,omitempty"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSONError(w, err)
		return
	}
	connection, err := s.runtime.CompleteConnectorAuth(r.Context(), user.ID, body.WorkspaceID, provider, body.State, body.Code, body.ExternalAccountID, body.ExternalAccountLabel, body.Scopes)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.auditEvent(r, "connector_complete_auth", user, map[string]any{"provider": connection.Provider, "status": connection.Status})
	s.recordGovernanceEvent("connector_complete_auth")
	writeJSON(w, http.StatusOK, map[string]any{"connection": connection})
}

func (s *Server) handleUpdateConnectorPolicy(w http.ResponseWriter, r *http.Request, user User, provider string) {
	var body struct {
		WorkspaceID string `json:"workspace_id,omitempty"`
		Policy      string `json:"policy"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSONError(w, err)
		return
	}
	connection, err := s.runtime.UpdateConnectorPolicy(r.Context(), user.ID, body.WorkspaceID, provider, body.Policy)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.auditEvent(r, "connector_update_policy", user, map[string]any{"provider": connection.Provider, "policy": connection.PermissionPolicy})
	s.recordGovernanceEvent("connector_update_policy")
	writeJSON(w, http.StatusOK, map[string]any{"connection": connection})
}

func (s *Server) handleDisconnectConnector(w http.ResponseWriter, r *http.Request, user User, provider string) {
	var body struct {
		WorkspaceID string `json:"workspace_id,omitempty"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSONError(w, err)
		return
	}
	if err := s.runtime.DisconnectConnector(r.Context(), user.ID, body.WorkspaceID, provider); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.auditEvent(r, "connector_disconnect", user, map[string]any{"provider": normalizeConnectorProviderID(provider)})
	s.recordGovernanceEvent("connector_disconnect")
	writeJSON(w, http.StatusOK, map[string]string{"status": "disconnected"})
}

func (r *Runtime) connectorStore() ConnectorStore {
	if r == nil {
		return NewMemoryConnectorStore()
	}
	if r.connectors == nil {
		r.connectors = NewMemoryConnectorStore()
	}
	return r.connectors
}

func (r *Runtime) connectorTokenVault() ConnectorTokenVault {
	if r == nil {
		return NewMemoryConnectorTokenVault()
	}
	if r.connectorTokens == nil {
		r.connectorTokens = NewMemoryConnectorTokenVault()
	}
	return r.connectorTokens
}

func connectorProviderByID(id string) (ConnectorProvider, bool) {
	id = normalizeConnectorProviderID(id)
	for _, provider := range DefaultConnectorProviders() {
		if provider.ID == id {
			return provider, true
		}
	}
	return ConnectorProvider{}, false
}

func normalizeConnectorProvider(provider ConnectorProvider) ConnectorProvider {
	provider.ID = normalizeConnectorProviderID(provider.ID)
	provider.DefaultPolicy = normalizeConnectorPolicy(provider.DefaultPolicy)
	provider.Scopes = normalizeConnectorScopes(provider.Scopes)
	provider.Capabilities = normalizeConnectorScopes(provider.Capabilities)
	provider.AuthURL = strings.TrimSpace(provider.AuthURL)
	provider.TokenURL = strings.TrimSpace(provider.TokenURL)
	provider.ClientIDEnv = strings.TrimSpace(provider.ClientIDEnv)
	provider.ClientSecretEnv = strings.TrimSpace(provider.ClientSecretEnv)
	provider.Configured = connectorProviderConfigured(provider)
	if strings.TrimSpace(provider.ConnectionKind) == "" {
		provider.ConnectionKind = mcpProviderConnectionKind(provider.ID)
	}
	provider.DefaultMCPURL = strings.TrimSpace(provider.DefaultMCPURL)
	return provider
}

func normalizeConnectorProviderID(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func normalizeConnectorPolicy(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case ConnectorPolicyDraftWrite:
		return ConnectorPolicyDraftWrite
	case ConnectorPolicyWriteWithReview:
		return ConnectorPolicyWriteWithReview
	case ConnectorPolicyDisabled:
		return ConnectorPolicyDisabled
	default:
		return ConnectorPolicyReadOnly
	}
}

func normalizeConnectorStatus(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case ConnectorStatusPending:
		return ConnectorStatusPending
	case ConnectorStatusConnected:
		return ConnectorStatusConnected
	case ConnectorStatusExpired:
		return ConnectorStatusExpired
	case ConnectorStatusError:
		return ConnectorStatusError
	case ConnectorStatusDisabled:
		return ConnectorStatusDisabled
	default:
		return ConnectorStatusDisconnected
	}
}

func normalizeConnectorScopes(scopes []string) []string {
	seen := make(map[string]bool, len(scopes))
	out := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		scope = strings.TrimSpace(scope)
		if scope == "" || seen[scope] {
			continue
		}
		seen[scope] = true
		out = append(out, scope)
	}
	return out
}

func normalizeConnectorConnection(connection ConnectorConnection, now time.Time) ConnectorConnection {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	connection.UserID = strings.TrimSpace(connection.UserID)
	connection.WorkspaceID = strings.TrimSpace(connection.WorkspaceID)
	connection.Provider = normalizeConnectorProviderID(connection.Provider)
	connection.Status = normalizeConnectorStatus(connection.Status)
	connection.PermissionPolicy = normalizeConnectorPolicy(connection.PermissionPolicy)
	connection.Scopes = normalizeConnectorScopes(connection.Scopes)
	if connection.ID == "" {
		connection.ID = "conn-" + newSortableID()
	}
	if connection.CreatedAt.IsZero() {
		connection.CreatedAt = now
	}
	if connection.UpdatedAt.IsZero() {
		connection.UpdatedAt = now
	}
	if connection.Metadata == nil {
		connection.Metadata = map[string]any{}
	}
	return connection
}

func normalizeConnectorOAuthState(state ConnectorOAuthState, now time.Time) ConnectorOAuthState {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	state.State = strings.TrimSpace(state.State)
	state.UserID = strings.TrimSpace(state.UserID)
	state.Provider = normalizeConnectorProviderID(state.Provider)
	state.Scopes = normalizeConnectorScopes(state.Scopes)
	state.RedirectURI = strings.TrimSpace(state.RedirectURI)
	if state.Metadata == nil {
		state.Metadata = map[string]any{}
	}
	if state.CreatedAt.IsZero() {
		state.CreatedAt = now
	}
	if state.ExpiresAt.IsZero() {
		state.ExpiresAt = now.Add(15 * time.Minute)
	}
	return state
}

func connectorConnectionKey(userID, workspaceID, provider string) string {
	return strings.TrimSpace(userID) + "\x00" + strings.TrimSpace(workspaceID) + "\x00" + normalizeConnectorProviderID(provider)
}

func cloneConnectorConnection(connection ConnectorConnection) ConnectorConnection {
	connection.Scopes = append([]string(nil), connection.Scopes...)
	if connection.Metadata != nil {
		metadata := make(map[string]any, len(connection.Metadata))
		for key, value := range connection.Metadata {
			metadata[key] = value
		}
		connection.Metadata = metadata
	}
	return connection
}

func cloneConnectorOAuthState(state ConnectorOAuthState) ConnectorOAuthState {
	state.Scopes = append([]string(nil), state.Scopes...)
	if state.Metadata != nil {
		metadata := make(map[string]any, len(state.Metadata))
		for key, value := range state.Metadata {
			metadata[key] = value
		}
		state.Metadata = metadata
	}
	return state
}

func cloneConnectorToken(token ConnectorToken) ConnectorToken {
	token.Scopes = append([]string(nil), token.Scopes...)
	if token.ExpiresAt != nil {
		expires := *token.ExpiresAt
		token.ExpiresAt = &expires
	}
	if token.RefreshExpiresAt != nil {
		expires := *token.RefreshExpiresAt
		token.RefreshExpiresAt = &expires
	}
	return token
}

func connectorProviderConfigured(provider ConnectorProvider) bool {
	env := strings.TrimSpace(provider.ClientIDEnv)
	if env == "" || strings.TrimSpace(os.Getenv(env)) == "" {
		return false
	}
	secretEnv := strings.TrimSpace(provider.ClientSecretEnv)
	return secretEnv == "" || strings.TrimSpace(os.Getenv(secretEnv)) != ""
}

func connectorAuthURL(provider ConnectorProvider, state ConnectorOAuthState) string {
	clientID := strings.TrimSpace(os.Getenv(provider.ClientIDEnv))
	values := url.Values{}
	values.Set("client_id", clientID)
	values.Set("state", state.State)
	if len(state.Scopes) > 0 {
		values.Set("scope", connectorScopeParam(provider.ID, state.Scopes))
	}
	if state.RedirectURI != "" {
		values.Set("redirect_uri", state.RedirectURI)
	}
	if provider.ID == "google_drive" || provider.ID == "gmail" {
		values.Set("response_type", "code")
		values.Set("access_type", "offline")
		values.Set("prompt", "consent")
	}
	if provider.ID == "notion" || provider.ID == "linear" {
		values.Set("response_type", "code")
	}
	base := strings.TrimSpace(provider.AuthURL)
	if base == "" || clientID == "" {
		return "agentapi://connectors/" + url.PathEscape(provider.ID) + "/callback?" + values.Encode()
	}
	return base + "?" + values.Encode()
}

func notionUsesOfficialMCPAuth() bool {
	return strings.Contains(strings.TrimSpace(mcpProviderDefaultURL("notion")), "mcp.notion.com/mcp")
}

func newConnectorPKCEPair() (string, string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", err
	}
	verifier := base64.RawURLEncoding.EncodeToString(raw)
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	return verifier, challenge, nil
}

func connectorTokenURL(provider ConnectorProvider) string {
	overrideEnv := strings.ToUpper(strings.ReplaceAll(provider.ID, "-", "_")) + "_OAUTH_TOKEN_URL"
	if override := strings.TrimSpace(os.Getenv(overrideEnv)); override != "" {
		return override
	}
	if provider.ID == "google_drive" || provider.ID == "gmail" {
		if override := strings.TrimSpace(os.Getenv("GOOGLE_OAUTH_TOKEN_URL")); override != "" {
			return override
		}
	}
	return firstNonEmptyString(provider.TokenURL, "https://github.com/login/oauth/access_token")
}

func connectorGitHubAPIBaseURL() string {
	return strings.TrimRight(firstNonEmptyString(strings.TrimSpace(os.Getenv("GITHUB_API_BASE_URL")), "https://api.github.com"), "/")
}

func connectorTokenRef(providerID, code string) string {
	sum := sha256.Sum256([]byte(normalizeConnectorProviderID(providerID) + "\x00" + strings.TrimSpace(code)))
	return "oauth:" + normalizeConnectorProviderID(providerID) + ":" + hex.EncodeToString(sum[:])[:24]
}

func connectorScopeParam(providerID string, scopes []string) string {
	scopes = normalizeConnectorScopes(scopes)
	switch normalizeConnectorProviderID(providerID) {
	case "slack", "linear":
		return strings.Join(scopes, ",")
	default:
		return strings.Join(scopes, " ")
	}
}

func connectorOAuthResponseScopes(scope any, fallback []string) []string {
	var scopes []string
	switch value := scope.(type) {
	case string:
		scopes = strings.Fields(strings.ReplaceAll(value, ",", " "))
	case []string:
		scopes = value
	case []any:
		for _, item := range value {
			if text := strings.TrimSpace(fmt.Sprint(item)); text != "" {
				scopes = append(scopes, text)
			}
		}
	}
	if len(scopes) == 0 {
		scopes = fallback
	}
	return normalizeConnectorScopes(scopes)
}

func connectorContextHint(provider ConnectorProvider, connection *ConnectorConnection) ConnectorContextHint {
	connected := connection != nil && connection.Status == ConnectorStatusConnected && connection.PermissionPolicy != ConnectorPolicyDisabled
	hint := ConnectorContextHint{
		Enabled:    connected,
		TaskTypes:  connectorTaskTypes(provider),
		Evidence:   append([]string(nil), provider.Capabilities...),
		PolicyHint: "read-only by default",
	}
	if connection != nil && connection.PermissionPolicy == ConnectorPolicyWriteWithReview {
		hint.PolicyHint = "write operations require review"
	}
	if connection != nil && connection.PermissionPolicy == ConnectorPolicyDisabled {
		hint.PolicyHint = "connector disabled"
	}
	return hint
}

func connectorTaskTypes(provider ConnectorProvider) []string {
	switch provider.ID {
	case "github":
		return []string{"code_review", "bug_fix", "issue_triage", "implementation_plan"}
	case "google_drive", "notion":
		return []string{"research", "summarization", "planning"}
	case "gmail", "slack":
		return []string{"communication_summary", "follow_up", "planning"}
	case "linear":
		return []string{"issue_triage", "status_update", "implementation_plan"}
	default:
		return []string{"research"}
	}
}

func firstNonEmptyConnectorStringSlice(primary, fallback []string) []string {
	if len(normalizeConnectorScopes(primary)) > 0 {
		return primary
	}
	return fallback
}

func nullTimePtr(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	t := value.Time
	return &t
}
