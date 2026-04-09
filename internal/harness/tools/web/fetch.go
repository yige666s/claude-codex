package web

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ding/claude-code/claude-go/internal/harness/permissions"
	toolkit "github.com/ding/claude-code/claude-go/internal/harness/tools"
)

type FetchTool struct {
	client *http.Client
}

type fetchInput struct {
	URL      string `json:"url"`
	MaxBytes int64  `json:"max_bytes,omitempty"`
}

func NewFetchTool(client *http.Client) *FetchTool {
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	return &FetchTool{client: client}
}

func (t *FetchTool) Name() string {
	return "web_fetch"
}

func (t *FetchTool) Description() string {
	return "Fetch a web page or JSON resource and return a text representation."
}

func (t *FetchTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"url":{"type":"string"},"max_bytes":{"type":"integer"}},"required":["url"]}`)
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

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, input.URL, nil)
	if err != nil {
		return toolkit.Result{}, err
	}
	request.Header.Set("user-agent", "claude-go-phase2/1.0")

	response, err := t.client.Do(request)
	if err != nil {
		return toolkit.Result{}, err
	}
	defer response.Body.Close()

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

	return toolkit.Result{
		Output: fmt.Sprintf("status: %s\ncontent_type: %s\nurl: %s\n\n%s", response.Status, contentType, input.URL, payload),
	}, nil
}
