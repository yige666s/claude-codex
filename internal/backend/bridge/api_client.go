package bridge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	appauth "claude-codex/internal/app/auth"
	"claude-codex/internal/app/config"
	"claude-codex/internal/app/securestorage"
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
	return c.doJSONWithBearer(ctx, method, path, accessToken, body, target)
}

func (c *BridgeAPIClient) doJSONWithBearer(ctx context.Context, method, path, bearer string, body any, target any) error {
	req, err := c.newRequest(ctx, method, path, bearer, body)
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		req, err = c.newRequest(ctx, method, path, bearer, body)
		if err != nil {
			return err
		}
		resp, err = c.httpClient.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		payload, err = io.ReadAll(resp.Body)
		if err != nil {
			return err
		}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("bridge api %s %s failed (%d): %s", method, path, resp.StatusCode, strings.TrimSpace(string(payload)))
	}
	if len(strings.TrimSpace(string(payload))) == 0 || target == nil {
		if len(strings.TrimSpace(string(payload))) == 0 && target != nil {
			return errEmptyBody
		}
		return nil
	}
	return json.Unmarshal(payload, target)
}

func (c *BridgeAPIClient) newRequest(ctx context.Context, method, path, bearer string, body any) (*http.Request, error) {
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+bearer)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("anthropic-beta", "environments-2025-11-01")
	req.Header.Set("x-environment-runner-version", c.runnerVersion)
	if c.trustedDeviceToken != nil {
		if token, err := c.trustedDeviceToken(); err == nil && strings.TrimSpace(token) != "" {
			req.Header.Set("X-Trusted-Device-Token", token)
		}
	}
	return req, nil
}

func validatePathID(id string) string {
	return url.PathEscape(strings.TrimSpace(id))
}
