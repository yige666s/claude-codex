package remotemanagedsettings

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	appauth "claude-codex/internal/app/auth"
	"claude-codex/internal/app/config"
	appsettings "claude-codex/internal/app/settings"
)

const endpointPath = "/api/claude_code/settings"

type FetchResult struct {
	Success     bool
	Settings    appsettings.Document
	Checksum    string
	NotModified bool
	Error       string
	SkipRetry   bool
}

type Service struct {
	auth       *appauth.Manager
	httpClient *http.Client
}

func New(cfg config.Config) (*Service, error) {
	manager, err := appauth.NewManager(cfg, nil)
	if err != nil {
		return nil, err
	}
	return NewWithManager(manager), nil
}

func NewWithManager(manager *appauth.Manager) *Service {
	return &Service{
		auth:       manager,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

func IsEligible(ctx context.Context, cfg config.Config) bool {
	manager, err := appauth.NewManager(cfg, nil)
	if err != nil {
		return false
	}
	status, err := manager.Status(ctx)
	return err == nil && status.Authenticated
}

func ComputeChecksum(settings appsettings.Document) string {
	sorted := sortDeep(settings)
	data, _ := json.Marshal(sorted)
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func (s *Service) Fetch(ctx context.Context, cachedChecksum string) FetchResult {
	accessToken, err := s.auth.GetAccessToken(ctx)
	if err != nil {
		return FetchResult{Success: false, Error: "authentication required", SkipRetry: true}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(s.auth.BaseAPIURL(), "/")+endpointPath, nil)
	if err != nil {
		return FetchResult{Success: false, Error: err.Error()}
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("anthropic-beta", "oauth-2025-04-20")
	req.Header.Set("User-Agent", "claude-codex")
	if cachedChecksum != "" {
		req.Header.Set("If-None-Match", `"`+cachedChecksum+`"`)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return FetchResult{Success: false, Error: err.Error()}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return FetchResult{Success: false, Error: err.Error()}
	}
	switch resp.StatusCode {
	case http.StatusNotModified:
		return FetchResult{Success: true, NotModified: true, Checksum: cachedChecksum}
	case http.StatusNoContent, http.StatusNotFound:
		appsettings.ClearRemoteManagedSettingsCache()
		return FetchResult{Success: true, Settings: appsettings.Document{}}
	case http.StatusOK:
	default:
		return FetchResult{Success: false, Error: fmt.Sprintf("remote managed settings fetch failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(body)))}
	}

	var payload struct {
		UUID     string               `json:"uuid"`
		Checksum string               `json:"checksum"`
		Settings appsettings.Document `json:"settings"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return FetchResult{Success: false, Error: "invalid remote managed settings format"}
	}
	if validation := appsettings.ValidateSettingsFileContent(string(mustJSON(payload.Settings))); !validation.IsValid {
		return FetchResult{Success: false, Error: validation.Error}
	}

	checksum := payload.Checksum
	if checksum == "" {
		checksum = ComputeChecksum(payload.Settings)
	}
	appsettings.SetRemoteManagedSettingsCache(payload.Settings)
	return FetchResult{
		Success:  true,
		Settings: payload.Settings,
		Checksum: checksum,
	}
}

func sortDeep(value any) any {
	switch v := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		out := make(map[string]any, len(v))
		for _, k := range keys {
			out[k] = sortDeep(v[k])
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i := range v {
			out[i] = sortDeep(v[i])
		}
		return out
	default:
		return v
	}
}

func mustJSON(v any) []byte {
	data, _ := json.Marshal(v)
	return data
}
