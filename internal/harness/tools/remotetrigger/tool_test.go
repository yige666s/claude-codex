package remotetrigger

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	oauthsvc "claude-codex/internal/backend/services/oauth"
	toolkit "claude-codex/internal/harness/tools"
)

type stubAuth struct {
	accessToken string
	baseAPIURL  string
	tokens      *oauthsvc.OAuthTokens
	err         error
}

func (s stubAuth) GetAccessToken(context.Context) (string, error) {
	return s.accessToken, s.err
}

func (s stubAuth) LoadOAuthTokens() (*oauthsvc.OAuthTokens, error) {
	return s.tokens, nil
}

func (s stubAuth) BaseAPIURL() string {
	return s.baseAPIURL
}

func executeRemoteTrigger(t *testing.T, tool toolkit.Tool, payload string) Output {
	t.Helper()
	result, err := tool.Execute(context.Background(), json.RawMessage(payload))
	if err != nil {
		t.Fatalf("Execute(%s) error = %v", payload, err)
	}
	var out Output
	if err := json.Unmarshal([]byte(result.Output), &out); err != nil {
		t.Fatalf("unmarshal output %q: %v", result.Output, err)
	}
	return out
}

func TestRemoteTriggerListBuildsAuthenticatedRequest(t *testing.T) {
	var gotMethod, gotPath, gotAuth, gotOrg, gotBeta string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotOrg = r.Header.Get("x-organization-uuid")
		gotBeta = r.Header.Get("anthropic-beta")
		_, _ = w.Write([]byte(`{"triggers":[{"id":"daily"}]}`))
	}))
	defer server.Close()

	tool := NewToolWithHTTPClient(stubAuth{
		accessToken: "access-token",
		baseAPIURL:  server.URL,
		tokens: &oauthsvc.OAuthTokens{
			TokenAccount: &oauthsvc.TokenAccount{OrganizationUUID: "org-123"},
		},
	}, server.Client())

	out := executeRemoteTrigger(t, tool, `{"action":"list"}`)
	if out.Status != http.StatusOK || out.JSON != `{"triggers":[{"id":"daily"}]}` {
		t.Fatalf("unexpected output: %#v", out)
	}
	if gotMethod != http.MethodGet || gotPath != "/v1/code/triggers" {
		t.Fatalf("unexpected request %s %s", gotMethod, gotPath)
	}
	if gotAuth != "Bearer access-token" || gotOrg != "org-123" || gotBeta != triggersBeta {
		t.Fatalf("missing auth headers: auth=%q org=%q beta=%q", gotAuth, gotOrg, gotBeta)
	}
}

func TestRemoteTriggerCreatePostsBody(t *testing.T) {
	var gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/code/triggers" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		data, _ := io.ReadAll(r.Body)
		gotBody = string(data)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"daily"}`))
	}))
	defer server.Close()

	tool := NewToolWithHTTPClient(stubAuth{
		accessToken: "token",
		baseAPIURL:  server.URL,
		tokens: &oauthsvc.OAuthTokens{
			Profile: &oauthsvc.ProfileResponse{
				Organization: oauthsvc.OrganizationInfo{UUID: "org-from-profile"},
			},
		},
	}, server.Client())

	out := executeRemoteTrigger(t, tool, `{"action":"create","body":{"name":"daily","enabled":true}}`)
	if out.Status != http.StatusCreated || out.JSON != `{"id":"daily"}` {
		t.Fatalf("unexpected output: %#v", out)
	}
	if !strings.Contains(gotBody, `"name":"daily"`) || !strings.Contains(gotBody, `"enabled":true`) {
		t.Fatalf("unexpected body: %s", gotBody)
	}
}

func TestRemoteTriggerValidatesActionRequirements(t *testing.T) {
	tool := NewToolWithHTTPClient(stubAuth{
		accessToken: "token",
		baseAPIURL:  "https://example.test",
		tokens: &oauthsvc.OAuthTokens{
			TokenAccount: &oauthsvc.TokenAccount{OrganizationUUID: "org"},
		},
	}, http.DefaultClient)

	for _, tc := range []struct {
		payload string
		want    string
	}{
		{`{"action":"get"}`, "get requires trigger_id"},
		{`{"action":"update","trigger_id":"daily"}`, "update requires body"},
		{`{"action":"run","trigger_id":"bad/slash"}`, "trigger_id must match"},
		{`{"action":"delete"}`, "action must be one of"},
	} {
		_, err := tool.Execute(context.Background(), json.RawMessage(tc.payload))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("Execute(%s) error = %v, want %q", tc.payload, err, tc.want)
		}
	}
}

func TestRemoteTriggerRequiresAuthAndOrganization(t *testing.T) {
	tool := NewToolWithHTTPClient(stubAuth{
		baseAPIURL: "https://example.test",
		tokens:     &oauthsvc.OAuthTokens{},
	}, http.DefaultClient)
	_, err := tool.Execute(context.Background(), json.RawMessage(`{"action":"list"}`))
	if err == nil || !strings.Contains(err.Error(), "not authenticated") {
		t.Fatalf("expected auth error, got %v", err)
	}

	tool = NewToolWithHTTPClient(stubAuth{
		accessToken: "token",
		baseAPIURL:  "https://example.test",
		tokens:      &oauthsvc.OAuthTokens{},
	}, http.DefaultClient)
	_, err = tool.Execute(context.Background(), json.RawMessage(`{"action":"list"}`))
	if err == nil || !strings.Contains(err.Error(), "organization UUID") {
		t.Fatalf("expected organization error, got %v", err)
	}
}
