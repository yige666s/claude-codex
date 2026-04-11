package policylimits

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	appauth "claude-codex/internal/app/auth"
	"claude-codex/internal/app/config"
)

type Restrictions map[string]any

type Response struct {
	Restrictions Restrictions `json:"restrictions"`
	Checksum     string       `json:"checksum,omitempty"`
}

type FetchResult struct {
	Success      bool
	Restrictions Restrictions
	Checksum     string
	NotModified  bool
	Error        string
	SkipRetry    bool
}

type Service struct {
	auth       *appauth.Manager
	httpClient *http.Client

	mu           sync.RWMutex
	restrictions Restrictions
	checksum     string
}

func New(cfg config.Config) (*Service, error) {
	auth, err := appauth.NewManager(cfg, nil)
	if err != nil {
		return nil, err
	}
	return &Service{
		auth:         auth,
		httpClient:   &http.Client{Timeout: 10 * time.Second},
		restrictions: Restrictions{},
	}, nil
}

func (s *Service) IsEligible(ctx context.Context) bool {
	status, err := s.auth.Status(ctx)
	return err == nil && status.Authenticated &&
		(status.SubscriptionType == "enterprise" || status.SubscriptionType == "team")
}

func (s *Service) Current() Restrictions {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneRestrictions(s.restrictions)
}

func (s *Service) Fetch(ctx context.Context) FetchResult {
	if !s.IsEligible(ctx) {
		return FetchResult{Success: false, SkipRetry: true, Error: "not eligible for policy limits"}
	}
	token, err := s.auth.GetAccessToken(ctx)
	if err != nil {
		return FetchResult{Success: false, SkipRetry: true, Error: err.Error()}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(s.auth.BaseAPIURL(), "/")+"/api/claude_code/policy_limits", nil)
	if err != nil {
		return FetchResult{Success: false, Error: err.Error()}
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("anthropic-beta", "oauth-2025-04-20")
	s.mu.RLock()
	if s.checksum != "" {
		req.Header.Set("If-None-Match", `"`+s.checksum+`"`)
	}
	s.mu.RUnlock()

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
		return FetchResult{Success: true, NotModified: true}
	case http.StatusNoContent, http.StatusNotFound:
		s.setRestrictions(Restrictions{}, "")
		return FetchResult{Success: true, Restrictions: Restrictions{}}
	case http.StatusOK:
	default:
		return FetchResult{Success: false, Error: fmt.Sprintf("policy limits fetch failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(body)))}
	}

	var parsed Response
	if err := json.Unmarshal(body, &parsed); err != nil {
		return FetchResult{Success: false, Error: "invalid policy limits response"}
	}
	checksum := parsed.Checksum
	if checksum == "" {
		checksum = ComputeChecksum(parsed.Restrictions)
	}
	s.setRestrictions(parsed.Restrictions, checksum)
	return FetchResult{Success: true, Restrictions: cloneRestrictions(parsed.Restrictions), Checksum: checksum}
}

func ComputeChecksum(restrictions Restrictions) string {
	data, _ := json.Marshal(restrictions)
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func (s *Service) setRestrictions(restrictions Restrictions, checksum string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.restrictions = cloneRestrictions(restrictions)
	s.checksum = checksum
}

func cloneRestrictions(in Restrictions) Restrictions {
	if in == nil {
		return Restrictions{}
	}
	out := Restrictions{}
	for k, v := range in {
		out[k] = v
	}
	return out
}
