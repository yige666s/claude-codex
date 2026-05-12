package web

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"claude-codex/internal/harness/permissions"
	toolkit "claude-codex/internal/harness/tools"
)

type FetchTool struct {
	client         *http.Client
	allowedDomains []string
}

type fetchInput struct {
	URL      string `json:"url"`
	Prompt   string `json:"prompt,omitempty"`
	MaxBytes int64  `json:"max_bytes,omitempty"`
}

func NewFetchTool(client *http.Client) *FetchTool {
	return NewFetchToolWithAllowlist(client, nil)
}

func NewFetchToolWithAllowlist(client *http.Client, allowedDomains []string) *FetchTool {
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	return &FetchTool{client: client, allowedDomains: append([]string(nil), allowedDomains...)}
}

func (t *FetchTool) Name() string {
	return "WebFetch"
}

func (t *FetchTool) Description() string {
	return "Fetch a web page or JSON resource and return a text representation."
}

func (t *FetchTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"url":{"type":"string","description":"The URL to fetch content from"},"prompt":{"type":"string","description":"The prompt describing what information to extract from the fetched content"},"max_bytes":{"type":"integer","description":"Maximum bytes to read before processing"}},"required":["url","prompt"]}`)
}

func (t *FetchTool) Permission() permissions.Level {
	return permissions.LevelRead
}

func (t *FetchTool) IsConcurrencySafe() bool {
	return true // web fetch is read-only and safe to run concurrently
}

func (t *FetchTool) Execute(ctx context.Context, raw json.RawMessage) (toolkit.Result, error) {
	var input fetchInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return toolkit.Result{}, err
	}
	if strings.TrimSpace(input.URL) == "" {
		return toolkit.Result{}, fmt.Errorf("url is required")
	}

	requestURL := strings.TrimSpace(input.URL)
	if strings.HasPrefix(requestURL, "http://") && !strings.HasPrefix(requestURL, "http://localhost") && !strings.HasPrefix(requestURL, "http://127.0.0.1") {
		requestURL = "https://" + strings.TrimPrefix(requestURL, "http://")
	}
	if err := validateURLAllowed(requestURL, t.allowedDomains); err != nil {
		return toolkit.Result{}, err
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return toolkit.Result{}, err
	}
	request.Header.Set("user-agent", "claude-codex-phase2/1.0")

	response, err := t.client.Do(request)
	if err != nil {
		return toolkit.Result{}, err
	}
	defer response.Body.Close()
	if response.Request != nil && response.Request.URL != nil {
		if err := validateURLAllowed(response.Request.URL.String(), t.allowedDomains); err != nil {
			return toolkit.Result{}, err
		}
	}

	maxBytes := input.MaxBytes
	if maxBytes <= 0 {
		maxBytes = 32 * 1024
	}

	body, err := io.ReadAll(io.LimitReader(response.Body, maxBytes))
	if err != nil {
		return toolkit.Result{}, err
	}

	contentType := response.Header.Get("content-type")
	payload := strings.TrimSpace(string(body))
	if strings.Contains(contentType, "text/html") {
		payload = stripHTML(payload)
	}

	finalURL := response.Request.URL.String()
	var builder strings.Builder
	fmt.Fprintf(&builder, "status: %s\ncontent_type: %s\nurl: %s\n", response.Status, contentType, finalURL)
	if finalURL != requestURL {
		fmt.Fprintf(&builder, "redirected_from: %s\n", requestURL)
	}
	if strings.TrimSpace(input.Prompt) != "" {
		fmt.Fprintf(&builder, "prompt: %s\n", strings.TrimSpace(input.Prompt))
	}
	fmt.Fprintf(&builder, "\n%s", payload)
	return toolkit.Result{Output: builder.String()}, nil
}
