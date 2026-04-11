package remotemanagedsettings

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	appauth "claude-codex/internal/app/auth"
	"claude-codex/internal/app/config"
	"claude-codex/internal/app/securestorage"
	appsettings "claude-codex/internal/app/settings"
	oauthsvc "claude-codex/internal/backend/services/oauth"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return fn(r)
}

type fakeStore struct {
	data securestorage.Data
}

func (s *fakeStore) Name() string                      { return "fake" }
func (s *fakeStore) Read() (securestorage.Data, error) { return s.data, nil }
func (s *fakeStore) Write(data securestorage.Data) (securestorage.WriteResult, error) {
	s.data = data
	return securestorage.WriteResult{}, nil
}
func (s *fakeStore) Delete() error {
	s.data = securestorage.Data{}
	return nil
}

func TestComputeChecksumStable(t *testing.T) {
	a := appsettings.Document{"b": "2", "a": appsettings.Document{"y": 2, "x": 1}}
	b := appsettings.Document{"a": appsettings.Document{"x": 1, "y": 2}, "b": "2"}
	if ComputeChecksum(a) != ComputeChecksum(b) {
		t.Fatal("checksum should be stable across key order")
	}
}

func TestFetchCachesRemoteManagedSettings(t *testing.T) {
	store := &fakeStore{}
	manager, err := appauth.NewManager(config.Default(), store)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	if err := manager.SaveOAuthTokens(&oauthsvc.OAuthTokens{
		AccessToken:  "oauth-token",
		RefreshToken: "refresh-token",
		ExpiresAt:    time.Now().Add(time.Hour).Unix(),
		Scopes:       []string{oauthsvc.ProfileScope},
	}); err != nil {
		t.Fatalf("SaveOAuthTokens: %v", err)
	}
	service := NewWithManager(manager)
	service.httpClient = &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if got := r.Header.Get("Authorization"); got != "Bearer oauth-token" {
				t.Fatalf("unexpected auth header: %q", got)
			}
			body, _ := json.Marshal(map[string]any{
				"uuid":     "settings-1",
				"checksum": "sha256:test",
				"settings": map[string]any{"verbose": true},
			})
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(string(body))),
				Header:     make(http.Header),
			}, nil
		}),
	}

	result := service.Fetch(context.Background(), "")
	if !result.Success {
		t.Fatalf("expected success, got %+v", result)
	}
	if cached := appsettings.GetRemoteManagedSettingsCache(); cached["verbose"] != true {
		t.Fatalf("expected remote settings cache to be updated, got %#v", cached)
	}
}

func TestFetch304NotModified(t *testing.T) {
	store := &fakeStore{}
	manager, err := appauth.NewManager(config.Default(), store)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	if err := manager.SaveOAuthTokens(&oauthsvc.OAuthTokens{
		AccessToken:  "oauth-token",
		RefreshToken: "refresh-token",
		ExpiresAt:    time.Now().Add(time.Hour).Unix(),
		Scopes:       []string{oauthsvc.ProfileScope},
	}); err != nil {
		t.Fatalf("SaveOAuthTokens: %v", err)
	}
	service := NewWithManager(manager)
	service.httpClient = &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if got := r.Header.Get("If-None-Match"); got != `"sha256:cached"` {
				t.Fatalf("unexpected etag header: %q", got)
			}
			return &http.Response{
				StatusCode: http.StatusNotModified,
				Body:       io.NopCloser(strings.NewReader("")),
				Header:     make(http.Header),
			}, nil
		}),
	}

	result := service.Fetch(context.Background(), "sha256:cached")
	if !result.Success || !result.NotModified {
		t.Fatalf("expected not modified result, got %+v", result)
	}
}
