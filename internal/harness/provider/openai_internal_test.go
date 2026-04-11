package provider

import (
	"context"
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
