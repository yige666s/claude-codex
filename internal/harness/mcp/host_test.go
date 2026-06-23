package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	toolkit "claude-codex/internal/harness/tools"
)

func TestRuntimeHostInProcessDiscoveryAndCall(t *testing.T) {
	server := NewServer(toolkit.NewRegistry(testTool{}))
	server.Name = "host-inprocess"
	host := NewRuntimeHost(nil)

	tools, err := host.DiscoverTools(context.Background(), HostConfig{Name: "host-inprocess", InProcessServer: server})
	if err != nil {
		t.Fatalf("discover tools: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "echo" {
		t.Fatalf("unexpected tools: %#v", tools)
	}
	result, err := host.CallTool(context.Background(), HostConfig{Name: "host-inprocess", InProcessServer: server}, "echo", json.RawMessage(`{"hello":"host"}`))
	if err != nil {
		t.Fatalf("call tool: %v", err)
	}
	if !bytes.Contains([]byte(result.Output), []byte("host")) {
		t.Fatalf("unexpected output: %q", result.Output)
	}
}

func TestRuntimeHostHTTPHeaders(t *testing.T) {
	server := NewServer(toolkit.NewRegistry(testTool{}))
	httpServer, ok := newHTTPTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer host-token" {
			t.Fatalf("authorization header = %q", r.Header.Get("Authorization"))
		}
		server.ServeHTTP(w, r)
	}))
	if !ok {
		return
	}
	defer httpServer.Close()

	host := NewRuntimeHost(httpServer.Client())
	tools, err := host.DiscoverTools(context.Background(), HostConfig{
		Name:      "host-http",
		Transport: "sse",
		URL:       httpServer.URL,
		Headers:   map[string]string{"Authorization": "Bearer host-token"},
	})
	if err != nil {
		t.Fatalf("discover tools: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "echo" {
		t.Fatalf("unexpected tools: %#v", tools)
	}
}

func TestRuntimeHostStreamableHTTPDiscoveryAndCall(t *testing.T) {
	var sawMethods []string
	httpServer, ok := newHTTPTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/mcp/v1" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer host-token" {
			t.Fatalf("authorization header = %q", r.Header.Get("Authorization"))
		}
		var req Request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		sawMethods = append(sawMethods, req.Method)
		w.Header().Set("Content-Type", "application/json")
		switch req.Method {
		case "initialize":
			var params map[string]any
			if err := json.Unmarshal(req.Params, &params); err != nil {
				t.Fatalf("decode initialize params: %v", err)
			}
			if _, ok := params["capabilities"].(map[string]any); !ok {
				t.Fatalf("initialize capabilities missing: %#v", params)
			}
			w.Header().Set("mcp-session-id", "session-1")
			_ = json.NewEncoder(w).Encode(Response{JSONRPC: "2.0", ID: req.ID, Result: json.RawMessage(`{"protocolVersion":"2024-11-05"}`)})
		case "tools/list":
			if r.Header.Get("Mcp-Session-Id") != "session-1" {
				t.Fatalf("session header = %q", r.Header.Get("Mcp-Session-Id"))
			}
			_ = json.NewEncoder(w).Encode(Response{JSONRPC: "2.0", ID: req.ID, Result: json.RawMessage(`{"tools":[{"name":"drive_search","description":"Search Drive","inputSchema":{"type":"object"}}]}`)})
		case "tools/call":
			if r.Header.Get("Mcp-Session-Id") != "session-1" {
				t.Fatalf("session header = %q", r.Header.Get("Mcp-Session-Id"))
			}
			_ = json.NewEncoder(w).Encode(Response{JSONRPC: "2.0", ID: req.ID, Result: json.RawMessage(`{"content":[{"type":"text","text":"drive result"}]}`)})
		default:
			_ = json.NewEncoder(w).Encode(Response{JSONRPC: "2.0", ID: req.ID, Error: &RPCError{Code: -32601, Message: "not found"}})
		}
	}))
	if !ok {
		return
	}
	defer httpServer.Close()

	host := NewRuntimeHost(httpServer.Client())
	cfg := HostConfig{
		Name:      "host-streamable-http",
		Transport: "http",
		URL:       httpServer.URL + "/mcp/v1",
		Headers:   map[string]string{"Authorization": "Bearer host-token"},
	}
	tools, err := host.DiscoverTools(context.Background(), cfg)
	if err != nil {
		t.Fatalf("discover tools: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "drive_search" || !bytes.Contains(tools[0].InputSchema, []byte("object")) {
		t.Fatalf("unexpected tools: %#v", tools)
	}
	result, err := host.CallTool(context.Background(), cfg, "drive_search", json.RawMessage(`{"q":"today"}`))
	if err != nil {
		t.Fatalf("call tool: %v", err)
	}
	if result.Output != "drive result" {
		t.Fatalf("unexpected output: %q", result.Output)
	}
	if strings.Join(sawMethods, ",") != "initialize,tools/list,initialize,tools/call" {
		t.Fatalf("methods = %#v", sawMethods)
	}
}

func TestRuntimeHostStreamableHTTPToolIsError(t *testing.T) {
	httpServer, ok := newHTTPTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req Request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		switch req.Method {
		case "initialize":
			_ = json.NewEncoder(w).Encode(Response{JSONRPC: "2.0", ID: req.ID, Result: json.RawMessage(`{"protocolVersion":"2024-11-05"}`)})
		case "tools/call":
			_ = json.NewEncoder(w).Encode(Response{JSONRPC: "2.0", ID: req.ID, Result: json.RawMessage(`{"content":[{"type":"text","text":"The caller does not have permission"}],"isError":true}`)})
		default:
			_ = json.NewEncoder(w).Encode(Response{JSONRPC: "2.0", ID: req.ID, Result: json.RawMessage(`{}`)})
		}
	}))
	if !ok {
		return
	}
	defer httpServer.Close()

	host := NewRuntimeHost(httpServer.Client())
	_, err := host.CallTool(context.Background(), HostConfig{Name: "host-streamable-http", Transport: "http", URL: httpServer.URL}, "gmail_search", json.RawMessage(`{"q":"today"}`))
	if err == nil || !strings.Contains(err.Error(), "The caller does not have permission") {
		t.Fatalf("expected MCP tool error, got %v", err)
	}
}
