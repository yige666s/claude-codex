package auth

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"claude-codex/internal/app/config"
	"claude-codex/internal/app/securestorage"
	oauthsvc "claude-codex/internal/backend/services/oauth"
)

type fakeStore struct {
	data securestorage.Data
}

func (s *fakeStore) Name() string { return "fake" }
func (s *fakeStore) Read() (securestorage.Data, error) {
	if s.data == nil {
		return securestorage.Data{}, nil
	}
	out := securestorage.Data{}
	for k, v := range s.data {
		out[k] = v
	}
	return out, nil
}
func (s *fakeStore) Write(data securestorage.Data) (securestorage.WriteResult, error) {
	s.data = data
	return securestorage.WriteResult{}, nil
}
func (s *fakeStore) Delete() error {
	s.data = securestorage.Data{}
	return nil
}

func TestOAuthConfigFromAppConfigUsesTSDefaults(t *testing.T) {
	cfg := config.Default()
	oauthCfg := OAuthConfigFromAppConfig(cfg)
	if oauthCfg.ClientID != defaultClientID {
		t.Fatalf("unexpected client id: %q", oauthCfg.ClientID)
	}
	if oauthCfg.TokenURL != defaultTokenURL {
		t.Fatalf("unexpected token url: %q", oauthCfg.TokenURL)
	}
	if oauthCfg.ClaudeAIAuthorizeURL != defaultClaudeAIAuthorizeURL {
		t.Fatalf("unexpected authorize url: %q", oauthCfg.ClaudeAIAuthorizeURL)
	}
}

func TestManagerSaveLoadAndLogout(t *testing.T) {
	store := &fakeStore{}
	manager, err := NewManager(config.Default(), store)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	tokens := &oauthsvc.OAuthTokens{
		AccessToken:  "access",
		RefreshToken: "refresh",
		ExpiresAt:    time.Now().Add(time.Hour).Unix(),
		Scopes:       []string{"user:profile"},
	}
	if err := manager.SaveOAuthTokens(tokens); err != nil {
		t.Fatalf("SaveOAuthTokens: %v", err)
	}
	if err := manager.SaveTrustedDeviceToken("trusted"); err != nil {
		t.Fatalf("SaveTrustedDeviceToken: %v", err)
	}

	loaded, err := manager.LoadOAuthTokens()
	if err != nil {
		t.Fatalf("LoadOAuthTokens: %v", err)
	}
	if loaded.AccessToken != "access" {
		t.Fatalf("unexpected access token: %#v", loaded)
	}

	status, err := manager.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !status.Authenticated || !status.HasTrustedDevice {
		t.Fatalf("unexpected status: %+v", status)
	}

	if err := manager.Logout(); err != nil {
		t.Fatalf("Logout: %v", err)
	}
	status, err = manager.Status(context.Background())
	if err != nil {
		t.Fatalf("Status after logout: %v", err)
	}
	if status.Authenticated || status.HasTrustedDevice {
		t.Fatalf("expected logged out status, got %+v", status)
	}
}

func TestManagerGetOAuthTokensRefreshesExpiredToken(t *testing.T) {
	store := &fakeStore{data: securestorage.Data{
		securestorage.KeyClaudeAIOAuth: map[string]any{
			"access_token":  "stale",
			"refresh_token": "refresh",
			"expires_at":    time.Now().Add(-time.Minute).Unix(),
			"scopes":        []string{"user:profile"},
		},
	}}
	manager, err := NewManager(config.Default(), store)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	manager.refreshToken = func(_ context.Context, refreshToken string, scopes []string) (*oauthsvc.OAuthTokens, error) {
		if refreshToken != "refresh" {
			t.Fatalf("unexpected refresh token: %s", refreshToken)
		}
		if len(scopes) != 1 || scopes[0] != "user:profile" {
			t.Fatalf("unexpected scopes: %#v", scopes)
		}
		return &oauthsvc.OAuthTokens{
			AccessToken:  "fresh",
			RefreshToken: "refresh-2",
			ExpiresAt:    time.Now().Add(time.Hour).Unix(),
			Scopes:       []string{"user:profile"},
		}, nil
	}

	token, err := manager.GetAccessToken(context.Background())
	if err != nil {
		t.Fatalf("GetAccessToken: %v", err)
	}
	if token != "fresh" {
		t.Fatalf("unexpected access token: %s", token)
	}
	loaded, err := manager.LoadOAuthTokens()
	if err != nil {
		t.Fatalf("LoadOAuthTokens after refresh: %v", err)
	}
	if loaded.RefreshToken != "refresh-2" {
		t.Fatalf("expected refreshed tokens to persist, got %#v", loaded)
	}
}

func TestEnrollTrustedDevice(t *testing.T) {
	client := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if !strings.HasSuffix(r.URL.Path, "/api/auth/trusted_devices") {
				t.Fatalf("unexpected path: %s", r.URL.Path)
			}
			body, _ := json.Marshal(map[string]string{"device_token": "trusted-token"})
			return &http.Response{
				StatusCode: http.StatusCreated,
				Body:       io.NopCloser(strings.NewReader(string(body))),
				Header:     make(http.Header),
			}, nil
		}),
	}
	token, err := EnrollTrustedDevice(context.Background(), client, defaultBaseAPIURL, "access-token", "darwin", "host")
	if err != nil {
		t.Fatalf("EnrollTrustedDevice: %v", err)
	}
	if token != "trusted-token" {
		t.Fatalf("unexpected token: %s", token)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return fn(r)
}
