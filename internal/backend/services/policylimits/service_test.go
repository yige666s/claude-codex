package policylimits

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"claude-codex/internal/app/auth"
	"claude-codex/internal/app/config"
	"claude-codex/internal/app/securestorage"
	oauthsvc "claude-codex/internal/backend/services/oauth"
)

type fakeStore struct{ data securestorage.Data }

func (s *fakeStore) Name() string                      { return "fake" }
func (s *fakeStore) Read() (securestorage.Data, error) { return s.data, nil }
func (s *fakeStore) Write(data securestorage.Data) (securestorage.WriteResult, error) {
	s.data = data
	return securestorage.WriteResult{}, nil
}
func (s *fakeStore) Delete() error { s.data = securestorage.Data{}; return nil }

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return fn(r) }

func TestPolicyLimitsFetch(t *testing.T) {
	store := &fakeStore{}
	manager, err := auth.NewManager(config.Default(), store)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	_ = manager.SaveOAuthTokens(&oauthsvc.OAuthTokens{
		AccessToken:      "oauth",
		RefreshToken:     "refresh",
		ExpiresAt:        time.Now().Add(time.Hour).Unix(),
		Scopes:           []string{oauthsvc.ProfileScope},
		SubscriptionType: "enterprise",
	})
	svc := &Service{
		auth:       manager,
		httpClient: &http.Client{},
	}
	svc.httpClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		body, _ := json.Marshal(Response{Restrictions: Restrictions{"allow_remote_sessions": true}})
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(string(body))), Header: make(http.Header)}, nil
	})
	result := svc.Fetch(context.Background())
	if !result.Success || result.Restrictions["allow_remote_sessions"] != true {
		t.Fatalf("unexpected fetch result: %+v", result)
	}
}
