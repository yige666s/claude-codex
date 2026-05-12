package remotetrigger

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	oauthsvc "claude-codex/internal/backend/services/oauth"
	"claude-codex/internal/harness/permissions"
	toolkit "claude-codex/internal/harness/tools"
)

const (
	ToolName     = "RemoteTrigger"
	triggersBeta = "ccr-triggers-2026-01-30"
)

type AuthProvider interface {
	GetAccessToken(ctx context.Context) (string, error)
	LoadOAuthTokens() (*oauthsvc.OAuthTokens, error)
	BaseAPIURL() string
}

type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

type Tool struct {
	auth       AuthProvider
	httpClient HTTPDoer
}

type Input struct {
	Action    string         `json:"action"`
	TriggerID string         `json:"trigger_id,omitempty"`
	Body      map[string]any `json:"body,omitempty"`
}

type Output struct {
	Status int    `json:"status"`
	JSON   string `json:"json"`
}

var triggerIDPattern = regexp.MustCompile(`^[\w-]+$`)

func NewTool(auth AuthProvider) toolkit.Tool {
	return NewToolWithHTTPClient(auth, &http.Client{Timeout: 20 * time.Second})
}

func NewToolWithHTTPClient(auth AuthProvider, httpClient HTTPDoer) toolkit.Tool {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 20 * time.Second}
	}
	return &Tool{
		auth:       auth,
		httpClient: httpClient,
	}
}

func (t *Tool) Name() string {
	return ToolName
}

func (t *Tool) Description() string {
	return "Manage scheduled remote Claude Code agents (triggers) via the claude.ai CCR API."
}

func (t *Tool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "action": {
      "type": "string",
      "enum": ["list", "get", "create", "update", "run"],
      "description": "Remote trigger action to perform."
    },
    "trigger_id": {
      "type": "string",
      "pattern": "^[\\w-]+$",
      "description": "Required for get, update, and run."
    },
    "body": {
      "type": "object",
      "description": "JSON body for create and update."
    }
  },
  "required": ["action"]
}`)
}

func (t *Tool) Permission() permissions.Level {
	return permissions.LevelWrite
}

func (t *Tool) IsConcurrencySafe() bool {
	return true
}

func (t *Tool) Execute(ctx context.Context, raw json.RawMessage) (toolkit.Result, error) {
	var input Input
	if err := json.Unmarshal(raw, &input); err != nil {
		return toolkit.Result{}, fmt.Errorf("%s: invalid input: %w", ToolName, err)
	}

	method, path, body, err := requestSpec(input)
	if err != nil {
		return toolkit.Result{}, err
	}
	if t.auth == nil {
		return toolkit.Result{}, fmt.Errorf("%s: auth provider is required", ToolName)
	}
	accessToken, err := t.auth.GetAccessToken(ctx)
	if err != nil {
		return toolkit.Result{}, fmt.Errorf("not authenticated with a claude.ai account. Run /login and try again: %w", err)
	}
	if strings.TrimSpace(accessToken) == "" {
		return toolkit.Result{}, fmt.Errorf("not authenticated with a claude.ai account. Run /login and try again")
	}
	orgUUID, err := organizationUUID(t.auth)
	if err != nil {
		return toolkit.Result{}, err
	}

	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(t.auth.BaseAPIURL(), "/")+path, body)
	if err != nil {
		return toolkit.Result{}, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("anthropic-beta", triggersBeta)
	req.Header.Set("x-organization-uuid", orgUUID)

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return toolkit.Result{}, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return toolkit.Result{}, err
	}
	out := Output{
		Status: resp.StatusCode,
		JSON:   compactJSON(respBody),
	}
	data, err := json.Marshal(out)
	if err != nil {
		return toolkit.Result{}, err
	}
	return toolkit.Result{Output: string(data)}, nil
}

func requestSpec(input Input) (method string, path string, body io.Reader, err error) {
	action := strings.TrimSpace(input.Action)
	triggerID := strings.TrimSpace(input.TriggerID)
	if triggerID != "" && !triggerIDPattern.MatchString(triggerID) {
		return "", "", nil, fmt.Errorf("%s: trigger_id must match ^[\\w-]+$", ToolName)
	}

	switch action {
	case "list":
		return http.MethodGet, "/v1/code/triggers", nil, nil
	case "get":
		if triggerID == "" {
			return "", "", nil, fmt.Errorf("%s: get requires trigger_id", ToolName)
		}
		return http.MethodGet, "/v1/code/triggers/" + triggerID, nil, nil
	case "create":
		if len(input.Body) == 0 {
			return "", "", nil, fmt.Errorf("%s: create requires body", ToolName)
		}
		body, err := json.Marshal(input.Body)
		if err != nil {
			return "", "", nil, err
		}
		return http.MethodPost, "/v1/code/triggers", bytes.NewReader(body), nil
	case "update":
		if triggerID == "" {
			return "", "", nil, fmt.Errorf("%s: update requires trigger_id", ToolName)
		}
		if len(input.Body) == 0 {
			return "", "", nil, fmt.Errorf("%s: update requires body", ToolName)
		}
		body, err := json.Marshal(input.Body)
		if err != nil {
			return "", "", nil, err
		}
		return http.MethodPost, "/v1/code/triggers/" + triggerID, bytes.NewReader(body), nil
	case "run":
		if triggerID == "" {
			return "", "", nil, fmt.Errorf("%s: run requires trigger_id", ToolName)
		}
		return http.MethodPost, "/v1/code/triggers/" + triggerID + "/run", bytes.NewReader([]byte("{}")), nil
	default:
		return "", "", nil, fmt.Errorf("%s: action must be one of list, get, create, update, run", ToolName)
	}
}

func organizationUUID(auth AuthProvider) (string, error) {
	tokens, err := auth.LoadOAuthTokens()
	if err != nil {
		return "", err
	}
	if tokens == nil {
		return "", fmt.Errorf("unable to resolve organization UUID")
	}
	if tokens.TokenAccount != nil && strings.TrimSpace(tokens.TokenAccount.OrganizationUUID) != "" {
		return tokens.TokenAccount.OrganizationUUID, nil
	}
	if tokens.Profile != nil && strings.TrimSpace(tokens.Profile.Organization.UUID) != "" {
		return tokens.Profile.Organization.UUID, nil
	}
	return "", fmt.Errorf("unable to resolve organization UUID")
}

func compactJSON(data []byte) string {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return "null"
	}
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return trimmed
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return trimmed
	}
	return string(encoded)
}
