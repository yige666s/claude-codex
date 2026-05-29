package provider

import (
	"context"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

func TestShortAPIProviderUsesOpenAICompatibleEndpoint(t *testing.T) {
	provider, err := NewShortAPIProvider(Config{
		Provider: "shortapi",
		APIKey:   "test-key",
		BaseURL:  "https://api.shortapi.ai",
		Model:    "google/gemini-3.1-pro-preview",
	})
	if err != nil {
		t.Fatalf("NewShortAPIProvider: %v", err)
	}
	provider.httpClient = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.String() != "https://api.shortapi.ai/v1/chat/completions" {
				t.Fatalf("unexpected URL %s", req.URL.String())
			}
			if got := req.Header.Get("Authorization"); got != "Bearer test-key" {
				t.Fatalf("unexpected auth header %q", got)
			}
			body, err := io.ReadAll(req.Body)
			if err != nil {
				t.Fatalf("read request body: %v", err)
			}
			text := string(body)
			if !strings.Contains(text, `"model":"google/gemini-3.1-pro-preview"`) || !strings.Contains(text, `"content":"hello"`) {
				t.Fatalf("unexpected request body %s", text)
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Status:     "200 OK",
				Body: io.NopCloser(strings.NewReader(`{
					"id":"chatcmpl-shortapi",
					"model":"google/gemini-3.1-pro-preview",
					"choices":[{"message":{"role":"assistant","content":"hi from shortapi"},"finish_reason":"stop"}],
					"usage":{"prompt_tokens":7,"completion_tokens":3,"total_tokens":10}
				}`)),
				Header: make(http.Header),
			}, nil
		}),
	}
	resp, err := provider.CreateMessage(context.Background(), MessageRequest{
		Model: "google/gemini-3.1-pro-preview",
		Messages: []Message{
			{Role: "user", Content: "hello"},
		},
		MaxTokens: 64,
	})
	if err != nil {
		t.Fatalf("CreateMessage: %v", err)
	}
	if resp.ID != "chatcmpl-shortapi" || len(resp.Content) != 1 || resp.Content[0].Text != "hi from shortapi" {
		t.Fatalf("unexpected response %#v", resp)
	}
	if resp.Usage.InputTokens != 7 || resp.Usage.OutputTokens != 3 {
		t.Fatalf("unexpected usage %#v", resp.Usage)
	}
}

func TestShortAPIProviderNormalizesBareModelAliases(t *testing.T) {
	provider, err := NewShortAPIProvider(Config{
		Provider: "shortapi",
		APIKey:   "test-key",
		BaseURL:  "https://api.shortapi.ai/v1",
		Model:    "gemini-3.1-pro-preview",
	})
	if err != nil {
		t.Fatalf("NewShortAPIProvider: %v", err)
	}
	if provider.config.Model != "google/gemini-3.1-pro-preview" {
		t.Fatalf("provider model = %q", provider.config.Model)
	}
	provider.httpClient = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			body, err := io.ReadAll(req.Body)
			if err != nil {
				t.Fatalf("read request body: %v", err)
			}
			text := string(body)
			if !strings.Contains(text, `"model":"google/gemini-2.5-flash"`) {
				t.Fatalf("bare request model was not normalized: %s", text)
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Status:     "200 OK",
				Body: io.NopCloser(strings.NewReader(`{
					"id":"chatcmpl-shortapi",
					"model":"google/gemini-2.5-flash",
					"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],
					"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}
				}`)),
				Header: make(http.Header),
			}, nil
		}),
	}
	if _, err := provider.CreateMessage(context.Background(), MessageRequest{
		Model: "gemini-2.5-flash",
		Messages: []Message{
			{Role: "user", Content: "hello"},
		},
		MaxTokens: 64,
	}); err != nil {
		t.Fatalf("CreateMessage: %v", err)
	}
}

func TestShortAPIProviderRealRequest(t *testing.T) {
	if os.Getenv("SHORTAPI_REAL_TEST") != "1" {
		t.Skip("set SHORTAPI_REAL_TEST=1 and SHORTAPI_KEY to run the ShortAPI integration test")
	}
	apiKey := strings.TrimSpace(os.Getenv("SHORTAPI_KEY"))
	if apiKey == "" {
		t.Skip("SHORTAPI_KEY is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	provider, err := NewShortAPIProvider(Config{
		Provider: "shortapi",
		APIKey:   apiKey,
		Model:    defaultShortAPIModel,
		Timeout:  240,
	})
	if err != nil {
		t.Fatalf("NewShortAPIProvider: %v", err)
	}
	resp, err := provider.CreateMessage(ctx, MessageRequest{
		Model: defaultShortAPIModel,
		Messages: []Message{
			{Role: "user", Content: "用一句话回答：1+1等于几？"},
		},
		MaxTokens:   64,
		Temperature: 0.1,
	})
	if err != nil {
		t.Fatalf("CreateMessage: %v", err)
	}
	if resp == nil || len(resp.Content) == 0 || strings.TrimSpace(resp.Content[0].Text) == "" {
		t.Fatalf("expected non-empty response, got %#v", resp)
	}
	t.Logf("shortapi model=%s response=%q", resp.Model, resp.Content[0].Text)
}
