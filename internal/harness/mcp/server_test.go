package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ding/claude-code/claude-go/internal/app/config"
	"github.com/ding/claude-code/claude-go/internal/harness/permissions"
	toolkit "github.com/ding/claude-code/claude-go/internal/harness/tools"
)

type testTool struct{}

func (testTool) Name() string                  { return "echo" }
func (testTool) Description() string           { return "echo input" }
func (testTool) InputSchema() json.RawMessage  { return json.RawMessage(`{"type":"object"}`) }
func (testTool) Permission() permissions.Level { return permissions.LevelRead }
func (testTool) IsConcurrencySafe() bool       { return true }
func (testTool) Execute(_ context.Context, input json.RawMessage) (toolkit.Result, error) {
	return toolkit.Result{Output: string(input)}, nil
}

func TestServerHTTPToolsAndCall(t *testing.T) {
	registry := toolkit.NewRegistry(testTool{})
	server := NewServer(registry)
	httpServer := httptest.NewServer(http.HandlerFunc(server.ServeHTTP))
	defer httpServer.Close()

	client, err := NewClientFromConfig(config.MCPServerConfig{Name: "local", URL: httpServer.URL, Transport: "sse"}, httpServer.Client())
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	tools, err := client.ListTools(context.Background())
	if err != nil || len(tools) != 1 {
		t.Fatalf("unexpected tools %#v err=%v", tools, err)
	}
	output, err := client.CallTool(context.Background(), "echo", json.RawMessage(`{"hello":"world"}`))
	if err != nil || !bytes.Contains([]byte(output), []byte("world")) {
		t.Fatalf("unexpected call output %q err=%v", output, err)
	}
}
