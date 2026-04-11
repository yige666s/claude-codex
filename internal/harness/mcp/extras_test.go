package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
)

type relayClient struct {
	kind         string
	name         string
	experimental map[string]any
}

func (c relayClient) ConnectionType() string                   { return c.kind }
func (c relayClient) Name() string                             { return c.name }
func (c relayClient) ExperimentalCapabilities() map[string]any { return c.experimental }

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return fn(r)
}

func TestChannelAllowlistAndPermissions(t *testing.T) {
	t.Setenv("CLAUDE_GO_MCP_CHANNEL_ALLOWLIST", `[{"marketplace":"anthropic","plugin":"telegram"}]`)
	t.Setenv("CLAUDE_GO_MCP_CHANNELS", "true")
	t.Setenv("CLAUDE_GO_MCP_CHANNEL_PERMISSIONS", "true")

	if !IsChannelsEnabled() || !IsChannelPermissionRelayEnabled() {
		t.Fatal("expected channel features enabled")
	}
	if !IsChannelAllowlisted("telegram@anthropic") || IsChannelAllowlisted("slack@anthropic") {
		t.Fatal("unexpected channel allowlist result")
	}
	if preview := TruncateForPreview(map[string]any{"x": strings.Repeat("a", 300)}); len(preview) <= 200 {
		t.Fatalf("expected preview truncation, got %q", preview)
	}
	callbacks := CreateChannelPermissionCallbacks()
	var resolved ChannelPermissionResponse
	unsub := callbacks.OnResponse("abcde", func(response ChannelPermissionResponse) { resolved = response })
	if !callbacks.Resolve("ABCDE", "allow", "telegram") {
		t.Fatal("expected resolve to succeed")
	}
	if resolved.Behavior != "allow" || resolved.FromServer != "telegram" {
		t.Fatalf("unexpected resolved response: %+v", resolved)
	}
	unsub()

	filtered := FilterPermissionRelayClients([]relayClient{
		{kind: "connected", name: "telegram", experimental: map[string]any{"claude/channel": true, "claude/channel/permission": true}},
		{kind: "connected", name: "other", experimental: map[string]any{"claude/channel": true}},
	}, func(name string) bool { return name == "telegram" })
	if len(filtered) != 1 || filtered[0].name != "telegram" {
		t.Fatalf("unexpected filtered clients: %#v", filtered)
	}
}

func TestElicitationRegistry(t *testing.T) {
	registry := NewElicitationRegistry()
	ch := registry.Enqueue("server", "req-1", map[string]any{"mode": "url"}, &ElicitationWaitingState{ActionLabel: "Retry"})
	if !registry.MarkCompleted("server", "req-1") {
		t.Fatal("expected mark completed")
	}
	if !registry.Resolve("req-1", ElicitationResult{Action: "accept"}) {
		t.Fatal("expected resolve")
	}
	result := <-ch
	if result.Action != "accept" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestOAuthPortHelpers(t *testing.T) {
	if got := BuildRedirectURI(3118); got != "http://localhost:3118/callback" {
		t.Fatalf("unexpected redirect uri: %s", got)
	}
	port, err := FindAvailablePort()
	if err != nil {
		t.Skipf("sandbox does not allow port probing: %v", err)
	}
	if port <= 0 {
		t.Fatalf("unexpected port: %d", port)
	}
}

func TestOfficialRegistry(t *testing.T) {
	ResetOfficialMCPURLsForTesting()
	client := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			body, _ := json.Marshal(map[string]any{
				"servers": []any{
					map[string]any{"server": map[string]any{"remotes": []any{map[string]any{"url": "https://example.com/path?x=1"}}}},
				},
			})
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(string(body))),
				Header:     make(http.Header),
			}, nil
		}),
	}
	if err := PrefetchOfficialMCPURLs(client); err != nil {
		t.Fatalf("PrefetchOfficialMCPURLs: %v", err)
	}
	if !IsOfficialMCPURL("https://example.com/path") {
		t.Fatal("expected official url to be cached")
	}
}

func TestMCPResourcesToolsWithActiveClient(t *testing.T) {
	server := NewServer(nil)
	server.ResourceProvider = testResources{}
	client := NewInProcessClient(server)
	RegisterActiveClient("resource-server", client)
	defer ClearActiveClients()

	resources, err := client.ListResources(context.Background())
	if err != nil || len(resources) != 1 {
		t.Fatalf("unexpected resources: %#v err=%v", resources, err)
	}
}

func TestSDKControlClientTransport(t *testing.T) {
	transport := NewSDKControlClientTransport("sdk-server", func(serverName string, message JSONRPCMessage) (JSONRPCMessage, error) {
		if serverName != "sdk-server" {
			t.Fatalf("unexpected server name: %s", serverName)
		}
		return JSONRPCMessage{ID: message.ID, Result: []byte(`{"ok":true}`)}, nil
	})
	got := make(chan JSONRPCMessage, 1)
	transport.OnMessage = func(message JSONRPCMessage) { got <- message }
	if err := transport.Send(JSONRPCMessage{ID: 1}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if (<-got).ID != 1 {
		t.Fatal("expected response to round-trip")
	}
}

func TestChannelEnvReset(t *testing.T) {
	os.Unsetenv("CLAUDE_GO_MCP_CHANNELS")
	os.Unsetenv("CLAUDE_GO_MCP_CHANNEL_PERMISSIONS")
}
