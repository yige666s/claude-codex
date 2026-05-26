package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	appauth "claude-codex/internal/app/auth"
	"claude-codex/internal/app/config"
	"claude-codex/internal/app/securestorage"
	"claude-codex/internal/backend/httpclient"
)

type AccessTokenSource func(context.Context) (string, error)
type TrustedDeviceTokenSource func() (string, error)

type BridgeAPIClient struct {
	baseURL            string
	httpClient         *http.Client
	accessTokenSource  AccessTokenSource
	trustedDeviceToken TrustedDeviceTokenSource
	runnerVersion      string
}

type BridgeEnvironmentConfig struct {
	MachineName        string         `json:"machine_name"`
	Directory          string         `json:"directory"`
	Branch             string         `json:"branch,omitempty"`
	GitRepoURL         string         `json:"git_repo_url,omitempty"`
	MaxSessions        int            `json:"max_sessions,omitempty"`
	Metadata           map[string]any `json:"metadata,omitempty"`
	ReuseEnvironmentID string         `json:"environment_id,omitempty"`
}

type BridgeEnvironmentRegistration struct {
	EnvironmentID     string `json:"environment_id"`
	EnvironmentSecret string `json:"environment_secret"`
}

type WorkData struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

type WorkResponse struct {
	ID            string   `json:"id"`
	Type          string   `json:"type"`
	EnvironmentID string   `json:"environment_id"`
	State         string   `json:"state"`
	Data          WorkData `json:"data"`
	Secret        string   `json:"secret"`
	CreatedAt     string   `json:"created_at"`
}

type PermissionResponseEvent struct {
	Type     string                         `json:"type"`
	Response PermissionResponseEventPayload `json:"response"`
}

type PermissionResponseEventPayload struct {
	Subtype   string         `json:"subtype"`
	RequestID string         `json:"request_id"`
	Response  map[string]any `json:"response"`
}

type HeartbeatResponse struct {
	LeaseExtended bool   `json:"lease_extended"`
	State         string `json:"state"`
}

func NewBridgeAPIClient(baseURL string, accessTokenSource AccessTokenSource, trustedDeviceTokenSource TrustedDeviceTokenSource) *BridgeAPIClient {
	return &BridgeAPIClient{
		baseURL:            strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		httpClient:         &http.Client{},
		accessTokenSource:  accessTokenSource,
		trustedDeviceToken: trustedDeviceTokenSource,
		runnerVersion:      "claude-codex",
	}
}

func BridgeRetryPolicy() httpclient.RetryPolicy {
	return httpclient.RetryPolicy{
		MaxAttempts: 3,
		BaseDelay:   250 * time.Millisecond,
		MaxDelay:    2 * time.Second,
		Jitter:      0.2,
		Methods: map[string]bool{
			http.MethodGet:    true,
			http.MethodPost:   true,
			http.MethodDelete: true,
		},
		Statuses: map[int]bool{
			http.StatusRequestTimeout:      true,
			http.StatusTooManyRequests:     true,
			http.StatusInternalServerError: true,
			http.StatusBadGateway:          true,
			http.StatusServiceUnavailable:  true,
			http.StatusGatewayTimeout:      true,
		},
	}
}

func NewAuthenticatedBridgeAPIClient(cfg config.Config, store securestorage.Store) (*BridgeAPIClient, error) {
	manager, err := appauth.NewManager(cfg, store)
	if err != nil {
		return nil, err
	}
	return NewBridgeAPIClient(
		manager.BaseAPIURL(),
		manager.GetAccessToken,
		manager.GetTrustedDeviceToken,
	), nil
}

func (c *BridgeAPIClient) RegisterBridgeEnvironment(ctx context.Context, cfg BridgeEnvironmentConfig) (*BridgeEnvironmentRegistration, error) {
	var result BridgeEnvironmentRegistration
	if err := c.doJSON(ctx, http.MethodPost, "/v1/environments/bridge", cfg, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *BridgeAPIClient) PollForWork(ctx context.Context, environmentID, environmentSecret string, reclaimOlderThanMS int64) (*WorkResponse, error) {
	path := fmt.Sprintf("/v1/environments/%s/work/poll", validatePathID(environmentID))
	if reclaimOlderThanMS > 0 {
		path += "?reclaim_older_than_ms=" + url.QueryEscape(fmt.Sprintf("%d", reclaimOlderThanMS))
	}
	var result WorkResponse
	err := c.doJSONWithBearer(ctx, http.MethodGet, path, environmentSecret, nil, &result)
	if err == errEmptyBody {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if result.ID == "" {
		return nil, nil
	}
	return &result, nil
}

func (c *BridgeAPIClient) AcknowledgeWork(ctx context.Context, environmentID, workID, sessionToken string) error {
	return c.doJSONWithBearer(ctx, http.MethodPost, fmt.Sprintf("/v1/environments/%s/work/%s/ack", validatePathID(environmentID), validatePathID(workID)), sessionToken, map[string]any{}, nil)
}

func (c *BridgeAPIClient) StopWork(ctx context.Context, environmentID, workID, sessionToken string, force bool) error {
	return c.doJSONWithBearer(ctx, http.MethodPost, fmt.Sprintf("/v1/environments/%s/work/%s/stop", validatePathID(environmentID), validatePathID(workID)), sessionToken, map[string]any{"force": force}, nil)
}

func (c *BridgeAPIClient) DeregisterEnvironment(ctx context.Context, environmentID string) error {
	return c.doJSON(ctx, http.MethodDelete, fmt.Sprintf("/v1/environments/%s", validatePathID(environmentID)), nil, nil)
}

func (c *BridgeAPIClient) SendPermissionResponseEvent(ctx context.Context, sessionID string, event PermissionResponseEvent, sessionToken string) error {
	return c.doJSONWithBearer(ctx, http.MethodPost, fmt.Sprintf("/v1/sessions/%s/events", validatePathID(sessionID)), sessionToken, event, nil)
}

func (c *BridgeAPIClient) ArchiveSession(ctx context.Context, sessionID string) error {
	return c.doJSON(ctx, http.MethodPost, fmt.Sprintf("/v1/sessions/%s/archive", validatePathID(sessionID)), map[string]any{}, nil)
}

func (c *BridgeAPIClient) ReconnectSession(ctx context.Context, environmentID, sessionID string) error {
	return c.doJSON(ctx, http.MethodPost, fmt.Sprintf("/v1/environments/%s/sessions/%s/reconnect", validatePathID(environmentID), validatePathID(sessionID)), map[string]any{}, nil)
}

func (c *BridgeAPIClient) HeartbeatWork(ctx context.Context, environmentID, workID, sessionToken string) (*HeartbeatResponse, error) {
	var result HeartbeatResponse
	if err := c.doJSONWithBearer(ctx, http.MethodPost, fmt.Sprintf("/v1/environments/%s/work/%s/heartbeat", validatePathID(environmentID), validatePathID(workID)), sessionToken, map[string]any{}, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

var errEmptyBody = fmt.Errorf("empty body")

func (c *BridgeAPIClient) doJSON(ctx context.Context, method, path string, body any, target any) error {
	accessToken, err := c.accessTokenSource(ctx)
	if err != nil {
		return err
	}
	status, payload, err := c.doJSONBytes(ctx, method, path, accessToken, body)
	if err != nil {
		return err
	}
	if status == http.StatusUnauthorized {
		accessToken, err = c.accessTokenSource(ctx)
		if err != nil {
			return err
		}
		status, payload, err = c.doJSONBytes(ctx, method, path, accessToken, body)
		if err != nil {
			return err
		}
	}
	return decodeBridgeAPIResponse(method, path, status, payload, target)
}

func (c *BridgeAPIClient) doJSONWithBearer(ctx context.Context, method, path, bearer string, body any, target any) error {
	status, payload, err := c.doJSONBytes(ctx, method, path, bearer, body)
	if err != nil {
		return err
	}
	if status == http.StatusUnauthorized {
		status, payload, err = c.doJSONBytes(ctx, method, path, bearer, body)
		if err != nil {
			return err
		}
	}
	return decodeBridgeAPIResponse(method, path, status, payload, target)
}

func decodeBridgeAPIResponse(method, path string, status int, payload []byte, target any) error {
	if status < 200 || status >= 300 {
		return fmt.Errorf("bridge api %s %s failed (%d): %s", method, path, status, strings.TrimSpace(string(payload)))
	}
	if len(strings.TrimSpace(string(payload))) == 0 || target == nil {
		if len(strings.TrimSpace(string(payload))) == 0 && target != nil {
			return errEmptyBody
		}
		return nil
	}
	return json.Unmarshal(payload, target)
}

func (c *BridgeAPIClient) doJSONBytes(ctx context.Context, method, path, bearer string, body any) (int, []byte, error) {
	headers := make(http.Header)
	headers.Set("Authorization", "Bearer "+bearer)
	headers.Set("anthropic-version", "2023-06-01")
	headers.Set("anthropic-beta", "environments-2025-11-01")
	headers.Set("x-environment-runner-version", c.runnerVersion)
	if c.trustedDeviceToken != nil {
		if token, err := c.trustedDeviceToken(); err == nil && strings.TrimSpace(token) != "" {
			headers.Set("X-Trusted-Device-Token", token)
		}
	}
	status, payload, _, err := httpclient.New(
		httpclient.WithHTTPClient(c.httpClient),
		httpclient.WithComponent("bridge_api"),
		httpclient.WithRetry(BridgeRetryPolicy()),
	).Bytes(ctx, method, c.baseURL+path, body,
		httpclient.WithHeaders(headers),
	)
	if err != nil {
		var statusErr *httpclient.StatusError
		if errors.As(err, &statusErr) {
			return statusErr.StatusCode, []byte(statusErr.Body), nil
		}
		return 0, nil, err
	}
	return status, payload, nil
}

func validatePathID(id string) string {
	return url.PathEscape(strings.TrimSpace(id))
}
