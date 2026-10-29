package bridge

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"claude-codex/internal/app/config"
	"claude-codex/internal/app/securestorage"
)

func TestBridgeAPIClientUsesAuthAndTrustedDeviceHeaders(t *testing.T) {
	requests := make([]*http.Request, 0, 2)
	client := NewBridgeAPIClient(
		"https://bridge.test",
		func(context.Context) (string, error) { return "oauth-token", nil },
		func() (string, error) { return "trusted-token", nil },
	)
	client.httpClient = &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			requests = append(requests, r.Clone(r.Context()))
			body, _ := json.Marshal(BridgeEnvironmentRegistration{
				EnvironmentID:     "env-1",
				EnvironmentSecret: "secret-1",
			})
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(string(body))),
				Header:     make(http.Header),
			}, nil
		}),
	}

	result, err := client.RegisterBridgeEnvironment(context.Background(), BridgeEnvironmentConfig{
		MachineName: "host",
		Directory:   "/tmp/project",
	})
	if err != nil {
		t.Fatalf("RegisterBridgeEnvironment: %v", err)
	}
	if result.EnvironmentID != "env-1" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if got := requests[0].Header.Get("Authorization"); got != "Bearer oauth-token" {
		t.Fatalf("unexpected auth header: %q", got)
	}
	if got := requests[0].Header.Get("X-Trusted-Device-Token"); got != "trusted-token" {
		t.Fatalf("unexpected trusted device header: %q", got)
	}
}

func TestBridgeAPIClientPollForWorkEmptyBodyReturnsNil(t *testing.T) {
	client := NewBridgeAPIClient(
		"https://bridge.test",
		func(context.Context) (string, error) { return "oauth-token", nil },
		nil,
	)
	client.httpClient = &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("")),
				Header:     make(http.Header),
			}, nil
		}),
	}

	work, err := client.PollForWork(context.Background(), "env-1", "env-secret", 0)
	if err != nil {
		t.Fatalf("PollForWork: %v", err)
	}
	if work != nil {
		t.Fatalf("expected nil work, got %+v", work)
	}
}

func TestBridgeAPIClientRetriesBridgePost(t *testing.T) {
	attempts := 0
	client := NewBridgeAPIClient(
		"https://bridge.test",
		func(context.Context) (string, error) { return "oauth-token", nil },
		nil,
	)
	client.httpClient = &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			attempts++
			if attempts == 1 {
				return &http.Response{
					StatusCode: http.StatusServiceUnavailable,
					Body:       io.NopCloser(strings.NewReader("try again")),
					Header:     make(http.Header),
				}, nil
			}
			body, _ := json.Marshal(BridgeEnvironmentRegistration{
				EnvironmentID:     "env-1",
				EnvironmentSecret: "secret-1",
			})
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(string(body))),
				Header:     make(http.Header),
			}, nil
		}),
	}

	result, err := client.RegisterBridgeEnvironment(context.Background(), BridgeEnvironmentConfig{
		MachineName: "host",
		Directory:   "/tmp/project",
	})
	if err != nil {
		t.Fatalf("RegisterBridgeEnvironment: %v", err)
	}
	if result.EnvironmentID != "env-1" || attempts != 2 {
		t.Fatalf("unexpected result/attempts: %+v attempts=%d", result, attempts)
	}
}

func TestBridgeAPIClientRefreshesBearerOnUnauthorized(t *testing.T) {
	tokens := []string{"old-token", "fresh-token"}
	sourceCalls := 0
	requests := make([]string, 0, 2)
	client := NewBridgeAPIClient(
		"https://bridge.test",
		func(context.Context) (string, error) {
			token := tokens[sourceCalls]
			sourceCalls++
			return token, nil
		},
		nil,
	)
	client.httpClient = &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			requests = append(requests, r.Header.Get("Authorization"))
			if len(requests) == 1 {
				return &http.Response{
					StatusCode: http.StatusUnauthorized,
					Body:       io.NopCloser(strings.NewReader("expired")),
					Header:     make(http.Header),
				}, nil
			}
			body, _ := json.Marshal(BridgeEnvironmentRegistration{
				EnvironmentID:     "env-1",
				EnvironmentSecret: "secret-1",
			})
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(string(body))),
				Header:     make(http.Header),
			}, nil
		}),
	}

	if _, err := client.RegisterBridgeEnvironment(context.Background(), BridgeEnvironmentConfig{MachineName: "host"}); err != nil {
		t.Fatalf("RegisterBridgeEnvironment: %v", err)
	}
	if sourceCalls != 2 {
		t.Fatalf("expected access token source to be called twice, got %d", sourceCalls)
	}
	if strings.Join(requests, ",") != "Bearer old-token,Bearer fresh-token" {
		t.Fatalf("unexpected authorization headers: %v", requests)
	}
}

func TestNewAuthenticatedBridgeAPIClient(t *testing.T) {
	store := &fakeAuthStore{data: securestorage.Data{
		"claudeAiOauth":      map[string]any{"access_token": "oauth-token", "refresh_token": "refresh", "expires_at": 4102444800, "scopes": []string{"user:profile"}},
		"trustedDeviceToken": "trusted-token",
	}}
	client, err := NewAuthenticatedBridgeAPIClient(config.Default(), store)
	if err != nil {
		t.Fatalf("NewAuthenticatedBridgeAPIClient: %v", err)
	}
	if client.baseURL == "" {
		t.Fatal("expected base url")
	}
	token, err := client.accessTokenSource(context.Background())
	if err != nil || token != "oauth-token" {
		t.Fatalf("unexpected access token: %q %v", token, err)
	}
	device, err := client.trustedDeviceToken()
	if err != nil || device != "trusted-token" {
		t.Fatalf("unexpected trusted device token: %q %v", device, err)
	}
}

type fakeAuthStore struct {
	data securestorage.Data
}

func (s *fakeAuthStore) Name() string                      { return "fake" }
func (s *fakeAuthStore) Read() (securestorage.Data, error) { return s.data, nil }
func (s *fakeAuthStore) Write(data securestorage.Data) (securestorage.WriteResult, error) {
	s.data = data
	return securestorage.WriteResult{}, nil
}
func (s *fakeAuthStore) Delete() error {
	s.data = securestorage.Data{}
	return nil
}
