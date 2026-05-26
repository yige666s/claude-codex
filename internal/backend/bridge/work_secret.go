package bridge

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"claude-codex/internal/backend/httpclient"
)

type WorkSecretSource struct {
	Type    string             `json:"type"`
	GitInfo *WorkSecretGitInfo `json:"git_info,omitempty"`
}

type WorkSecretGitInfo struct {
	Type  string `json:"type"`
	Repo  string `json:"repo"`
	Ref   string `json:"ref,omitempty"`
	Token string `json:"token,omitempty"`
}

type WorkSecretAuth struct {
	Type  string `json:"type"`
	Token string `json:"token"`
}

type WorkSecret struct {
	Version              int                `json:"version"`
	SessionIngressToken  string             `json:"session_ingress_token"`
	APIBaseURL           string             `json:"api_base_url"`
	Sources              []WorkSecretSource `json:"sources,omitempty"`
	Auth                 []WorkSecretAuth   `json:"auth,omitempty"`
	ClaudeCodeArgs       map[string]string  `json:"claude_code_args,omitempty"`
	MCPConfig            any                `json:"mcp_config,omitempty"`
	EnvironmentVariables map[string]string  `json:"environment_variables,omitempty"`
	UseCodeSessions      bool               `json:"use_code_sessions,omitempty"`
}

func DecodeWorkSecret(secret string) (*WorkSecret, error) {
	payload, err := base64.RawURLEncoding.DecodeString(secret)
	if err != nil {
		return nil, err
	}

	var decoded WorkSecret
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return nil, err
	}
	if decoded.Version != 1 {
		return nil, fmt.Errorf("unsupported work secret version: %d", decoded.Version)
	}
	if strings.TrimSpace(decoded.SessionIngressToken) == "" {
		return nil, fmt.Errorf("invalid work secret: missing session_ingress_token")
	}
	if strings.TrimSpace(decoded.APIBaseURL) == "" {
		return nil, fmt.Errorf("invalid work secret: missing api_base_url")
	}
	return &decoded, nil
}

func BuildSDKURL(apiBaseURL, sessionID string) string {
	base := strings.TrimRight(strings.TrimSpace(apiBaseURL), "/")
	isLocalhost := strings.Contains(base, "localhost") || strings.Contains(base, "127.0.0.1")
	protocol := "wss"
	version := "v1"
	if isLocalhost {
		protocol = "ws"
		version = "v2"
	}
	host := strings.TrimPrefix(strings.TrimPrefix(base, "https://"), "http://")
	return fmt.Sprintf("%s://%s/%s/session_ingress/ws/%s", protocol, host, version, sessionID)
}

func BuildCCRV2SDKURL(apiBaseURL, sessionID string) string {
	return fmt.Sprintf("%s/v1/code/sessions/%s", strings.TrimRight(strings.TrimSpace(apiBaseURL), "/"), sessionID)
}

func SameSessionID(a, b string) bool {
	if a == b {
		return true
	}
	aBody := a[strings.LastIndex(a, "_")+1:]
	bBody := b[strings.LastIndex(b, "_")+1:]
	return len(aBody) >= 4 && aBody == bBody
}

func RegisterWorker(ctx context.Context, client *http.Client, sessionURL, accessToken string) (int64, error) {
	if client == nil {
		client = http.DefaultClient
	}
	var body struct {
		WorkerEpoch any `json:"worker_epoch"`
	}
	err := httpclient.New(
		httpclient.WithHTTPClient(client),
		httpclient.WithComponent("bridge_worker"),
	).JSON(ctx, http.MethodPost, strings.TrimRight(sessionURL, "/")+"/worker/register", map[string]any{}, &body,
		httpclient.WithBearer(accessToken),
		httpclient.WithHeader("anthropic-version", "2023-06-01"),
	)
	if err != nil {
		var statusErr *httpclient.StatusError
		if errors.As(err, &statusErr) {
			return 0, fmt.Errorf("registerWorker: unexpected status %d", statusErr.StatusCode)
		}
		return 0, err
	}
	switch value := body.WorkerEpoch.(type) {
	case float64:
		return int64(value), nil
	case string:
		var epoch int64
		if _, err := fmt.Sscan(value, &epoch); err != nil {
			return 0, fmt.Errorf("registerWorker: invalid worker_epoch %q", value)
		}
		return epoch, nil
	default:
		return 0, fmt.Errorf("registerWorker: invalid worker_epoch in response")
	}
}
