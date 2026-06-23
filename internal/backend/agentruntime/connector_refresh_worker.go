package agentruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

type ConnectorRefreshWorker struct {
	runtime   *Runtime
	interval  time.Duration
	lookahead time.Duration
	limit     int
}

const connectorTokenRefreshLookahead = 10 * time.Minute

func NewConnectorRefreshWorker(runtime *Runtime, interval time.Duration) *ConnectorRefreshWorker {
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	return &ConnectorRefreshWorker{runtime: runtime, interval: interval, lookahead: connectorTokenRefreshLookahead, limit: 100}
}

func (w *ConnectorRefreshWorker) Run(ctx context.Context) error {
	if w == nil || w.runtime == nil {
		return fmt.Errorf("connector refresh worker is not configured")
	}
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		_, _ = w.runtime.RefreshDueConnectorTokens(ctx, w.lookahead, w.limit)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (r *Runtime) RefreshDueConnectorTokens(ctx context.Context, lookahead time.Duration, limit int) (int, error) {
	if r == nil {
		return 0, fmt.Errorf("runtime is not configured")
	}
	if lookahead <= 0 {
		lookahead = connectorTokenRefreshLookahead
	}
	if limit <= 0 {
		limit = 100
	}
	before := time.Now().UTC().Add(lookahead)
	connections, err := r.connectorStore().ListRefreshableConnections(ctx, before, limit)
	if err != nil {
		return 0, err
	}
	refreshed := 0
	for _, connection := range connections {
		provider, ok := connectorProviderByID(connection.Provider)
		if !ok {
			continue
		}
		if err := r.refreshConnectorToken(ctx, provider, connection); err == nil {
			refreshed++
		}
	}
	return refreshed, nil
}

func (r *Runtime) refreshConnectorToken(ctx context.Context, provider ConnectorProvider, connection ConnectorConnection) error {
	isNotionMCP := provider.ID == "notion" && deepAgentWorkflowString(connection.Metadata, "oauth_mode") == "mcp"
	if !isNotionMCP && !connectorProviderConfigured(provider) {
		return nil
	}
	lock := r.connectorRefreshLock(connectorRefreshLockKey(connection))
	lock.Lock()
	defer lock.Unlock()

	if current, err := r.connectorStore().GetConnection(ctx, connection.UserID, connection.WorkspaceID, connection.Provider); err != nil {
		return err
	} else if current != nil {
		connection = *current
	}
	token, err := r.connectorTokenVault().GetToken(ctx, connection.TokenRef)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	if token == nil || strings.TrimSpace(token.AccessToken) == "" {
		return r.markConnectorRefreshExpired(ctx, connection, "missing access token")
	}
	if !connectorTokenRefreshDue(*token, now, 0) {
		return nil
	}
	if strings.TrimSpace(token.RefreshToken) == "" {
		if connection.ExpiresAt != nil && connection.ExpiresAt.Before(now) {
			return r.markConnectorRefreshExpired(ctx, connection, "missing refresh token")
		}
		return nil
	}
	req, err := connectorRefreshRequest(ctx, provider, connection, *token)
	if err != nil {
		if isNotionMCP && strings.Contains(err.Error(), "missing dynamic client_id") {
			_ = r.markConnectorRefreshExpired(ctx, connection, err.Error())
		}
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if readErr != nil {
		return readErr
	}
	var parsed connectorOAuthTokenResponse
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_ = json.Unmarshal(body, &parsed)
		if connectorRefreshErrorExpiresConnection(parsed.Error) {
			_ = r.markConnectorRefreshExpired(ctx, connection, firstNonEmptyString(parsed.Error, "refresh failed")+": "+parsed.ErrorDescription)
		}
		return fmt.Errorf("%s OAuth refresh failed: status %d: %s", provider.ID, resp.StatusCode, truncateDeepAgentDiagnosticText(string(body), 600))
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return err
	}
	if parsed.Error != "" {
		if connectorRefreshErrorExpiresConnection(parsed.Error) {
			_ = r.markConnectorRefreshExpired(ctx, connection, parsed.Error+": "+parsed.ErrorDescription)
		}
		return fmt.Errorf("%s OAuth refresh failed: %s %s", provider.ID, parsed.Error, parsed.ErrorDescription)
	}
	if strings.TrimSpace(parsed.AccessToken) == "" {
		return fmt.Errorf("%s OAuth refresh returned no access token", provider.ID)
	}
	newToken := *token
	newToken.Ref = connectorTokenRef(provider.ID, parsed.AccessToken)
	newToken.AccessToken = parsed.AccessToken
	newToken.RefreshToken = firstNonEmptyString(parsed.RefreshToken, token.RefreshToken)
	newToken.TokenType = firstNonEmptyString(parsed.TokenType, token.TokenType, "bearer")
	if scopes := connectorOAuthResponseScopes(parsed.Scope, nil); len(scopes) > 0 {
		newToken.Scopes = scopes
	}
	if parsed.ExpiresIn > 0 {
		expires := now.Add(time.Duration(parsed.ExpiresIn) * time.Second)
		newToken.ExpiresAt = &expires
	}
	if parsed.RefreshTokenExpiresIn > 0 {
		expires := now.Add(time.Duration(parsed.RefreshTokenExpiresIn) * time.Second)
		newToken.RefreshExpiresAt = &expires
	}
	newToken.UpdatedAt = now
	if err := r.connectorTokenVault().PutToken(ctx, newToken); err != nil {
		return err
	}
	if newToken.Ref != connection.TokenRef {
		_ = r.connectorTokenVault().DeleteToken(ctx, connection.TokenRef)
	}
	connection.TokenRef = newToken.Ref
	connection.Scopes = firstNonEmptyConnectorStringSlice(newToken.Scopes, connection.Scopes)
	connection.ExpiresAt = newToken.ExpiresAt
	connection.Status = ConnectorStatusConnected
	connection.UpdatedAt = now
	connection.LastSyncAt = &now
	if connection.Metadata == nil {
		connection.Metadata = map[string]any{}
	}
	connection.Metadata["last_refresh_at"] = now.Format(time.RFC3339)
	delete(connection.Metadata, "last_refresh_error")
	delete(connection.Metadata, "last_refresh_error_at")
	_, err = r.connectorStore().UpsertConnection(ctx, connection)
	return err
}

func connectorRefreshRequest(ctx context.Context, provider ConnectorProvider, connection ConnectorConnection, token ConnectorToken) (*http.Request, error) {
	values := url.Values{}
	values.Set("grant_type", "refresh_token")
	values.Set("refresh_token", token.RefreshToken)
	endpoint := connectorTokenURL(provider)
	if provider.ID == "notion" && deepAgentWorkflowString(connection.Metadata, "oauth_mode") == "mcp" {
		clientID := firstNonEmptyString(deepAgentWorkflowString(connection.Metadata, "oauth_client_id"), deepAgentWorkflowString(connection.Metadata, "client_id"))
		if strings.TrimSpace(clientID) == "" {
			return nil, fmt.Errorf("notion MCP OAuth refresh is missing dynamic client_id; reconnect Notion")
		}
		values.Set("client_id", clientID)
		if resource := deepAgentWorkflowString(connection.Metadata, "resource"); resource != "" {
			values.Set("resource", resource)
		}
		endpoint = firstNonEmptyString(deepAgentWorkflowString(connection.Metadata, "token_endpoint"), "https://mcp.notion.com/token")
	} else {
		values.Set("client_id", strings.TrimSpace(os.Getenv(provider.ClientIDEnv)))
		if secret := strings.TrimSpace(os.Getenv(provider.ClientSecretEnv)); secret != "" {
			values.Set("client_secret", secret)
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(values.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req, nil
}

func connectorTokenRefreshDue(token ConnectorToken, now time.Time, lookahead time.Duration) bool {
	if lookahead <= 0 {
		lookahead = connectorTokenRefreshLookahead
	}
	if token.ExpiresAt == nil || token.ExpiresAt.IsZero() {
		return false
	}
	return !token.ExpiresAt.After(now.Add(lookahead))
}

func connectorRefreshLockKey(connection ConnectorConnection) string {
	return strings.Join([]string{
		strings.TrimSpace(connection.UserID),
		strings.TrimSpace(connection.WorkspaceID),
		normalizeConnectorProviderID(connection.Provider),
		strings.TrimSpace(connection.TokenRef),
	}, "\x00")
}

func connectorRefreshErrorExpiresConnection(code string) bool {
	switch strings.ToLower(strings.TrimSpace(code)) {
	case "invalid_grant", "invalid_token", "unauthorized_client":
		return true
	default:
		return false
	}
}

func (r *Runtime) markConnectorRefreshExpired(ctx context.Context, connection ConnectorConnection, reason string) error {
	now := time.Now().UTC()
	connection.Status = ConnectorStatusExpired
	connection.UpdatedAt = now
	if connection.Metadata == nil {
		connection.Metadata = map[string]any{}
	}
	connection.Metadata["last_refresh_error"] = truncateDeepAgentDiagnosticText(strings.TrimSpace(reason), 600)
	connection.Metadata["last_refresh_error_at"] = now.Format(time.RFC3339)
	_, err := r.connectorStore().UpsertConnection(ctx, connection)
	return err
}
