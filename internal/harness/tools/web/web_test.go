package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFetchToolReturnsTextContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("content-type", "text/plain")
		_, _ = w.Write([]byte("hello from web fetch"))
	}))
	defer server.Close()

	tool := NewFetchTool(server.Client())
	input, _ := json.Marshal(map[string]any{"url": server.URL})
	result, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("fetch execute: %v", err)
	}
	if !strings.Contains(result.Output, "hello from web fetch") {
		t.Fatalf("unexpected fetch output: %q", result.Output)
	}
}

func TestSearchToolParsesAnchorResults(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("content-type", "text/html")
		_, _ = w.Write([]byte(`<html><body><a href="https://example.com">Example Result</a></body></html>`))
	}))
	defer server.Close()

	tool := NewSearchTool(server.Client())
	input, _ := json.Marshal(map[string]any{"query": "example", "endpoint": server.URL})
	result, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("search execute: %v", err)
	}
	if !strings.Contains(result.Output, "Example Result - https://example.com") {
		t.Fatalf("unexpected search output: %q", result.Output)
	}
}
