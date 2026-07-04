package agentruntime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"claude-codex/internal/harness/engine"
)

type GitHubRepositoryInfo struct {
	ID              int64     `json:"id,omitempty"`
	FullName        string    `json:"full_name,omitempty"`
	Description     string    `json:"description,omitempty"`
	HTMLURL         string    `json:"html_url,omitempty"`
	DefaultBranch   string    `json:"default_branch,omitempty"`
	Private         bool      `json:"private,omitempty"`
	OpenIssuesCount int       `json:"open_issues_count,omitempty"`
	StargazersCount int       `json:"stargazers_count,omitempty"`
	ForksCount      int       `json:"forks_count,omitempty"`
	UpdatedAt       time.Time `json:"updated_at,omitempty"`
}

type GitHubIssueInfo struct {
	ID        int64     `json:"id,omitempty"`
	Number    int       `json:"number,omitempty"`
	Title     string    `json:"title,omitempty"`
	State     string    `json:"state,omitempty"`
	HTMLURL   string    `json:"html_url,omitempty"`
	Body      string    `json:"body,omitempty"`
	Author    string    `json:"author,omitempty"`
	Labels    []string  `json:"labels,omitempty"`
	CreatedAt time.Time `json:"created_at,omitempty"`
	UpdatedAt time.Time `json:"updated_at,omitempty"`
}

func (r *Runtime) ReadGitHubRepository(ctx context.Context, userID, workspaceID, owner, repo string) (GitHubRepositoryInfo, error) {
	owner, repo = normalizeGitHubOwnerRepo(owner, repo)
	if owner == "" || repo == "" {
		return GitHubRepositoryInfo{}, fmt.Errorf("github repo reader requires owner and repo")
	}
	var out GitHubRepositoryInfo
	err := r.doGitHubConnectorRequest(ctx, userID, workspaceID, "github_repo_reader", map[string]any{"owner": owner, "repo": repo}, "/repos/"+url.PathEscape(owner)+"/"+url.PathEscape(repo), &out)
	return out, err
}

func (r *Runtime) ListGitHubRepositories(ctx context.Context, userID, workspaceID, visibility string, limit int) ([]GitHubRepositoryInfo, error) {
	visibility = strings.ToLower(strings.TrimSpace(visibility))
	if visibility == "" {
		visibility = "public"
	}
	switch visibility {
	case "all", "public", "private":
	default:
		return nil, fmt.Errorf("github repository lister visibility must be one of all, public, or private")
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	query := url.Values{}
	query.Set("visibility", visibility)
	query.Set("affiliation", "owner,collaborator,organization_member")
	query.Set("sort", "updated")
	query.Set("per_page", strconv.Itoa(limit))
	var out []GitHubRepositoryInfo
	err := r.doGitHubConnectorRequest(ctx, userID, workspaceID, "github_repository_lister", map[string]any{"visibility": visibility, "limit": limit}, "/user/repos?"+query.Encode(), &out)
	return out, err
}

func (r *Runtime) ReadGitHubIssue(ctx context.Context, userID, workspaceID, owner, repo string, issueNumber int) (GitHubIssueInfo, error) {
	owner, repo = normalizeGitHubOwnerRepo(owner, repo)
	if owner == "" || repo == "" || issueNumber <= 0 {
		return GitHubIssueInfo{}, fmt.Errorf("github issue reader requires owner, repo, and positive issue_number")
	}
	var raw struct {
		ID        int64     `json:"id"`
		Number    int       `json:"number"`
		Title     string    `json:"title"`
		State     string    `json:"state"`
		HTMLURL   string    `json:"html_url"`
		Body      string    `json:"body"`
		CreatedAt time.Time `json:"created_at"`
		UpdatedAt time.Time `json:"updated_at"`
		User      struct {
			Login string `json:"login"`
		} `json:"user"`
		Labels []struct {
			Name string `json:"name"`
		} `json:"labels"`
	}
	err := r.doGitHubConnectorRequest(ctx, userID, workspaceID, "github_issue_reader", map[string]any{"owner": owner, "repo": repo, "issue_number": issueNumber}, "/repos/"+url.PathEscape(owner)+"/"+url.PathEscape(repo)+"/issues/"+strconv.Itoa(issueNumber), &raw)
	if err != nil {
		return GitHubIssueInfo{}, err
	}
	labels := make([]string, 0, len(raw.Labels))
	for _, label := range raw.Labels {
		if strings.TrimSpace(label.Name) != "" {
			labels = append(labels, label.Name)
		}
	}
	return GitHubIssueInfo{
		ID:        raw.ID,
		Number:    raw.Number,
		Title:     raw.Title,
		State:     raw.State,
		HTMLURL:   raw.HTMLURL,
		Body:      raw.Body,
		Author:    raw.User.Login,
		Labels:    labels,
		CreatedAt: raw.CreatedAt,
		UpdatedAt: raw.UpdatedAt,
	}, nil
}

func (r *Runtime) doGitHubConnectorRequest(ctx context.Context, userID, workspaceID, toolName string, input map[string]any, path string, out any) error {
	if r == nil {
		return fmt.Errorf("runtime is not configured")
	}
	connection, token, err := r.githubConnectorToken(ctx, userID, workspaceID)
	if err != nil {
		return err
	}
	inputJSON, _ := json.Marshal(input)
	argsHash := connectorArgsHash(inputJSON)
	scope := engine.ToolExecutionScopeFromContext(ctx)
	ledger := r.toolCallLedger
	idempotencyKey := firstNonEmptyString(scope.WorkflowRunID, scope.JobID, scope.SessionID, userID) + ":connector:" + toolName + ":" + argsHash
	entry := engine.ToolLedgerEntry{
		UserID:            firstNonEmptyString(scope.UserID, userID),
		SessionID:         scope.SessionID,
		JobID:             scope.JobID,
		WorkflowRunID:     scope.WorkflowRunID,
		WorkflowStepID:    scope.WorkflowStepID,
		WorkflowStepIndex: scope.WorkflowStepIndex,
		ToolName:          toolName,
		ArgsHash:          argsHash,
		IdempotencyKey:    idempotencyKey,
		Input:             inputJSON,
		Metadata: map[string]any{
			"provider":      "github",
			"connector_id":  connection.ID,
			"connector_ref": connection.TokenRef,
			"external_call": true,
			"permission":    connection.PermissionPolicy,
			"side_effect":   "read",
		},
		StartedAt: time.Now().UTC(),
	}
	if ledger != nil {
		started, replayed, beginErr := ledger.BeginToolCall(ctx, entry)
		if beginErr != nil {
			return beginErr
		}
		entry = started
		if replayed && strings.TrimSpace(started.Output) != "" {
			return json.Unmarshal([]byte(started.Output), out)
		}
	}
	emitJobEventFromContext(ctx, toolCallStartEvent("", toolName, idempotencyKey, json.RawMessage(inputJSON), entry.Metadata))
	endpoint := connectorGitHubAPIBaseURL() + path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		if ledger != nil {
			_ = ledger.FailToolCall(ctx, idempotencyKey, err.Error(), nil)
		}
		emitJobEventFromContext(ctx, toolCallResultEvent("", toolName, idempotencyKey, json.RawMessage(inputJSON), "", err, entry.Metadata))
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", connectorAuthorizationHeader(*token))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		if ledger != nil {
			_ = ledger.FailToolCall(ctx, idempotencyKey, err.Error(), map[string]any{"endpoint": endpoint})
		}
		emitJobEventFromContext(ctx, toolCallResultEvent("", toolName, idempotencyKey, json.RawMessage(inputJSON), "", err, entry.Metadata))
		return err
	}
	defer resp.Body.Close()
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	if readErr != nil {
		if ledger != nil {
			_ = ledger.FailToolCall(ctx, idempotencyKey, readErr.Error(), map[string]any{"endpoint": endpoint, "status_code": resp.StatusCode})
		}
		return readErr
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		err := fmt.Errorf("github connector call failed: status %d: %s", resp.StatusCode, truncateDeepAgentDiagnosticText(string(body), 600))
		if ledger != nil {
			_ = ledger.FailToolCall(ctx, idempotencyKey, err.Error(), map[string]any{"endpoint": endpoint, "status_code": resp.StatusCode})
		}
		emitJobEventFromContext(ctx, toolCallResultEvent("", toolName, idempotencyKey, json.RawMessage(inputJSON), "", err, entry.Metadata))
		return err
	}
	if err := json.Unmarshal(body, out); err != nil {
		if ledger != nil {
			_ = ledger.FailToolCall(ctx, idempotencyKey, err.Error(), map[string]any{"endpoint": endpoint, "status_code": resp.StatusCode})
		}
		return err
	}
	if ledger != nil {
		_ = ledger.CompleteToolCall(ctx, idempotencyKey, string(body), map[string]any{"endpoint": endpoint, "status_code": resp.StatusCode})
	}
	emitJobEventFromContext(ctx, toolCallResultEvent("", toolName, idempotencyKey, json.RawMessage(inputJSON), "GitHub connector call completed.", nil, map[string]any{"provider": "github", "tool_name": toolName, "status_code": resp.StatusCode}))
	return nil
}

func (r *Runtime) githubConnectorToken(ctx context.Context, userID, workspaceID string) (*ConnectorConnection, *ConnectorToken, error) {
	connection, err := r.connectorStore().GetConnection(ctx, userID, strings.TrimSpace(workspaceID), "github")
	if err != nil {
		return nil, nil, err
	}
	if connection == nil || connection.Status != ConnectorStatusConnected || connection.PermissionPolicy == ConnectorPolicyDisabled {
		return nil, nil, fmt.Errorf("github connector is not connected")
	}
	token, err := r.connectorTokenVault().GetToken(ctx, connection.TokenRef)
	if err != nil {
		return nil, nil, err
	}
	if token == nil || strings.TrimSpace(token.AccessToken) == "" {
		return nil, nil, fmt.Errorf("github connector token is not available; reconnect GitHub")
	}
	return connection, token, nil
}

func normalizeGitHubOwnerRepo(owner, repo string) (string, string) {
	owner = strings.Trim(strings.TrimSpace(owner), "/")
	repo = strings.Trim(strings.TrimSpace(repo), "/")
	if strings.Contains(owner, "/") && repo == "" {
		parts := strings.Split(owner, "/")
		if len(parts) >= 2 {
			owner = parts[len(parts)-2]
			repo = strings.TrimSuffix(parts[len(parts)-1], ".git")
		}
	}
	return owner, strings.TrimSuffix(repo, ".git")
}

func connectorArgsHash(input []byte) string {
	sum := sha256.Sum256(input)
	return hex.EncodeToString(sum[:])
}
