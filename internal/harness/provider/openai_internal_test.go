package provider

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func TestOpenAIProviderParsesToolCalls(t *testing.T) {
	provider, err := NewOpenAIProvider(Config{
		Provider: "openai",
		APIKey:   "test-key",
		Model:    "gpt-4o",
	})
	if err != nil {
		t.Fatalf("NewOpenAIProvider() error = %v", err)
	}
	provider.httpClient = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			body := `{
				"id":"chatcmpl-1",
				"model":"gpt-4o",
				"choices":[
					{
						"index":0,
						"finish_reason":"tool_calls",
						"message":{
							"role":"assistant",
							"tool_calls":[
								{
									"id":"call_123",
									"type":"function",
									"function":{"name":"bash","arguments":"{\"command\":\"pwd\"}"}
								}
							]
						}
					}
				],
				"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}
			}`
			return &http.Response{
				StatusCode: 200,
				Status:     "200 OK",
				Body:       io.NopCloser(strings.NewReader(body)),
				Header:     make(http.Header),
			}, nil
		}),
	}

	resp, err := provider.CreateMessage(context.Background(), MessageRequest{
		Model: "gpt-4o",
		Messages: []Message{
			{Role: "user", Content: "run pwd"},
		},
		MaxTokens: 100,
	})
	if err != nil {
		t.Fatalf("CreateMessage() error = %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected one tool call, got %#v", resp)
	}
	if resp.ToolCalls[0].Name != "bash" || string(resp.ToolCalls[0].Input) != `{"command":"pwd"}` {
		t.Fatalf("unexpected tool call %#v", resp.ToolCalls[0])
	}
	if resp.StopReason != "tool_use" {
		t.Fatalf("expected stop reason tool_use, got %q", resp.StopReason)
	}
}

func TestOpenAIProviderReturnsClearErrorOnEmptyBody(t *testing.T) {
	provider, err := NewOpenAIProvider(Config{
		Provider: "openai",
		APIKey:   "test-key",
		Model:    "gpt-4o",
	})
	if err != nil {
		t.Fatalf("NewOpenAIProvider() error = %v", err)
	}
	provider.httpClient = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: 200,
				Status:     "200 OK",
				Body:       io.NopCloser(strings.NewReader("")),
				Header:     make(http.Header),
			}, nil
		}),
	}

	_, err = provider.CreateMessage(context.Background(), MessageRequest{
		Model: "gpt-4o",
		Messages: []Message{
			{Role: "user", Content: "hello"},
		},
		MaxTokens: 100,
	})
	if err == nil || !strings.Contains(err.Error(), "empty response body") {
		t.Fatalf("expected clear empty body error, got %v", err)
	}
}

func TestOpenAIProviderIncludesSafeRequestSummaryOnTransportError(t *testing.T) {
	provider, err := NewOpenAIProvider(Config{
		Provider: "openai",
		APIKey:   "test-key",
		BaseURL:  "https://api.example.test/v1",
		Model:    "gpt-4o",
	})
	if err != nil {
		t.Fatalf("NewOpenAIProvider() error = %v", err)
	}
	provider.httpClient = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return nil, errors.New("connection reset")
		}),
	}

	_, err = provider.CreateMessage(context.Background(), MessageRequest{
		Model:  "gpt-4o",
		System: "system prompt",
		Messages: []Message{
			{Role: "user", Content: "hello"},
		},
		Tools: []Tool{{Name: "search", InputSchema: map[string]interface{}{"type": "object"}}},
	})
	if err == nil {
		t.Fatalf("expected transport error")
	}
	for _, want := range []string{
		"connection reset",
		"url=https://api.example.test/v1/chat/completions",
		"model=gpt-4o",
		"messages=2",
		"tools=1",
		"system_chars=13",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not contain %q", err.Error(), want)
		}
	}
	if strings.Contains(err.Error(), "test-key") {
		t.Fatalf("error leaked api key: %q", err.Error())
	}
}

func TestOpenAIProviderRetriesRetryableTransportErrors(t *testing.T) {
	provider, err := NewOpenAIProvider(Config{
		Provider: "openai",
		APIKey:   "test-key",
		BaseURL:  "https://api.example.test/v1",
		Model:    "gpt-4o",
	})
	if err != nil {
		t.Fatalf("NewOpenAIProvider() error = %v", err)
	}
	calls := 0
	provider.httpClient = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			calls++
			if calls == 1 {
				return nil, io.ErrUnexpectedEOF
			}
			return &http.Response{
				StatusCode: 200,
				Status:     "200 OK",
				Body: io.NopCloser(strings.NewReader(`{
					"id":"chatcmpl-1",
					"model":"gpt-4o",
					"choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"ok"}}],
					"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}
				}`)),
				Header: make(http.Header),
			}, nil
		}),
	}

	resp, err := provider.CreateMessage(context.Background(), MessageRequest{
		Model: "gpt-4o",
		Messages: []Message{
			{Role: "user", Content: "hello"},
		},
		MaxTokens: 100,
	})
	if err != nil {
		t.Fatalf("CreateMessage() error = %v", err)
	}
	if calls != 2 {
		t.Fatalf("expected two calls, got %d", calls)
	}
	if len(resp.Content) != 1 || resp.Content[0].Text != "ok" {
		t.Fatalf("unexpected response content %#v", resp.Content)
	}
}

func TestDeepSeekProviderUsesDefaultHostFailoverTransport(t *testing.T) {
	deepseek, err := NewDeepSeekProvider(Config{
		APIKey:  "test-key",
		BaseURL: "https://api.deepseek.com",
	})
	if err != nil {
		t.Fatalf("NewDeepSeekProvider() error = %v", err)
	}
	if deepseek.httpClient.Transport == nil {
		t.Fatalf("expected DeepSeek default host to install failover transport")
	}

	custom, err := NewDeepSeekProvider(Config{
		APIKey:  "test-key",
		BaseURL: "https://deepseek.example.test",
	})
	if err != nil {
		t.Fatalf("NewDeepSeekProvider() error = %v", err)
	}
	if custom.httpClient.Transport != nil {
		t.Fatalf("did not expect custom DeepSeek host to install failover transport")
	}
}
