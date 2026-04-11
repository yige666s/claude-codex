package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"claude-codex/internal/app/config"
	"claude-codex/internal/harness/permissions"
	toolkit "claude-codex/internal/harness/tools"
)

type testTool struct{}
type testResources struct{}

func (testTool) Name() string                  { return "echo" }
func (testTool) Description() string           { return "echo input" }
func (testTool) InputSchema() json.RawMessage  { return json.RawMessage(`{"type":"object"}`) }
func (testTool) Permission() permissions.Level { return permissions.LevelRead }
func (testTool) IsConcurrencySafe() bool       { return true }
func (testTool) Execute(_ context.Context, input json.RawMessage) (toolkit.Result, error) {
	return toolkit.Result{Output: string(input)}, nil
}

func (testResources) ListResources(_ context.Context) ([]ResourceDefinition, error) {
	return []ResourceDefinition{{URI: "file://resource.txt", Name: "resource.txt", MimeType: "text/plain"}}, nil
}

func (testResources) ReadResource(_ context.Context, uri string) ([]ResourceContent, error) {
	return []ResourceContent{{URI: uri, MimeType: "text/plain", Text: "resource-body"}}, nil
}

func TestServerHTTPToolsAndCall(t *testing.T) {
	registry := toolkit.NewRegistry(testTool{})
	server := NewServer(registry)
	server.Name = "local-mcp"
	server.Version = "1.2.3"
	server.Instructions = "Use echo for smoke tests."
	server.ResourceProvider = testResources{}
	httpServer, ok := newHTTPTestServer(t, http.HandlerFunc(server.ServeHTTP))
	if !ok {
		return
	}
	defer httpServer.Close()

	client, err := NewClientFromConfig(config.MCPServerConfig{Name: "local", URL: httpServer.URL, Transport: "sse"}, httpServer.Client())
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	if _, err := client.Initialize(context.Background()); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if client.Instructions != server.Instructions {
		t.Fatalf("initialize instructions = %q, want %q", client.Instructions, server.Instructions)
	}
	tools, err := client.ListTools(context.Background())
	if err != nil || len(tools) != 1 {
		t.Fatalf("unexpected tools %#v err=%v", tools, err)
	}
	output, err := client.CallTool(context.Background(), "echo", json.RawMessage(`{"hello":"world"}`))
	if err != nil || !bytes.Contains([]byte(output), []byte("world")) {
		t.Fatalf("unexpected call output %q err=%v", output, err)
	}
	resources, err := client.ListResources(context.Background())
	if err != nil || len(resources) != 1 || resources[0].URI != "file://resource.txt" {
		t.Fatalf("unexpected resources %#v err=%v", resources, err)
	}
	contents, err := client.ReadResource(context.Background(), "file://resource.txt")
	if err != nil || len(contents) != 1 || contents[0].Text != "resource-body" {
		t.Fatalf("unexpected resource contents %#v err=%v", contents, err)
	}
}

func TestInProcessClientInitializeListAndCall(t *testing.T) {
	registry := toolkit.NewRegistry(testTool{})
	server := NewServer(registry)
	server.Name = "in-process"
	server.Version = "9.9.9"
	server.Instructions = "in-process instructions"
	server.ResourceProvider = testResources{}

	client := NewInProcessClient(server)

	if _, err := client.Initialize(context.Background()); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if client.Name() != server.Name {
		t.Fatalf("client name = %q, want %q", client.Name(), server.Name)
	}
	if client.Instructions != server.Instructions {
		t.Fatalf("initialize instructions = %q, want %q", client.Instructions, server.Instructions)
	}

	tools, err := client.ListTools(context.Background())
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "echo" {
		t.Fatalf("unexpected tools: %#v", tools)
	}

	output, err := client.CallTool(context.Background(), "echo", json.RawMessage(`{"ping":"pong"}`))
	if err != nil {
		t.Fatalf("call tool: %v", err)
	}
	if !bytes.Contains([]byte(output), []byte("pong")) {
		t.Fatalf("unexpected call output %q", output)
	}
	resources, err := client.ListResources(context.Background())
	if err != nil || len(resources) != 1 {
		t.Fatalf("list resources: %#v err=%v", resources, err)
	}
}

func TestClientSubscribeEventsOverHTTP(t *testing.T) {
	registry := toolkit.NewRegistry(testTool{})
	server := NewServer(registry)
	httpServer, ok := newHTTPTestServer(t, http.HandlerFunc(server.ServeHTTP))
	if !ok {
		return
	}
	defer httpServer.Close()

	client, err := NewClientFromConfig(
		config.MCPServerConfig{Name: "local", URL: httpServer.URL, Transport: "sse"},
		httpServer.Client(),
	)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events := make(chan Event, 4)
	done := make(chan error, 1)
	go func() {
		done <- client.SubscribeEvents(ctx, func(event Event) error {
			events <- event
			if event.Name == "message" {
				cancel()
			}
			return nil
		})
	}()

	select {
	case event := <-events:
		if event.Name != "ready" {
			t.Fatalf("first event = %q, want ready", event.Name)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for ready event")
	}

	if _, err := client.CallTool(context.Background(), "echo", json.RawMessage(`{"hello":"stream"}`)); err != nil {
		t.Fatalf("call tool: %v", err)
	}

	select {
	case event := <-events:
		if event.Name != "message" {
			t.Fatalf("second event = %q, want message", event.Name)
		}
		var payload struct {
			Event   string `json:"event"`
			Payload struct {
				Name string `json:"name"`
			} `json:"payload"`
		}
		if err := json.Unmarshal(event.Data, &payload); err != nil {
			t.Fatalf("unmarshal event payload: %v", err)
		}
		if payload.Event != "tool_call" || payload.Payload.Name != "echo" {
			t.Fatalf("unexpected payload: %+v", payload)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for tool_call event")
	}

	if err := <-done; err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("subscribe events: %v", err)
	}
}

func newHTTPTestServer(t *testing.T, handler http.Handler) (*httptest.Server, bool) {
	t.Helper()
	var server *httptest.Server
	ok := true
	func() {
		defer func() {
			if recover() != nil {
				ok = false
			}
		}()
		server = httptest.NewServer(handler)
	}()
	if !ok {
		t.Skip("sandbox does not allow binding local test servers")
	}
	return server, ok
}

func TestSDKControlTransportRoundTrip(t *testing.T) {
	RegisterSDKControlHandler("sdk-server", func(_ string, msg JSONRPCMessage) (JSONRPCMessage, error) {
		return JSONRPCMessage{
			ID:      msg.ID,
			JSONRPC: "2.0",
			Result:  mustJSON(ListToolsResult{Tools: []ToolDefinition{{Name: "sdkTool"}}}),
		}, nil
	})
	defer UnregisterSDKControlHandler("sdk-server")

	client, err := NewClientFromConfig(config.MCPServerConfig{Name: "sdk-server", Transport: "sdk"}, nil)
	if err != nil {
		t.Fatalf("new sdk client: %v", err)
	}
	tools, err := client.ListTools(context.Background())
	if err != nil {
		t.Fatalf("sdk list tools: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "sdkTool" {
		t.Fatalf("unexpected sdk tools: %#v", tools)
	}
}

func TestInProcessTransportPair(t *testing.T) {
	a, b := CreateLinkedTransportPair()
	done := make(chan JSONRPCMessage, 1)
	b.OnMessage = func(message JSONRPCMessage) {
		done <- message
	}
	if err := a.Send(JSONRPCMessage{Method: "ping"}); err != nil {
		t.Fatalf("send: %v", err)
	}
	select {
	case msg := <-done:
		if msg.Method != "ping" {
			t.Fatalf("unexpected message: %+v", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for linked transport")
	}
}
