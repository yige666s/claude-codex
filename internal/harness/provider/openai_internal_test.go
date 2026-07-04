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

func TestOpenAIProviderStreamsChatCompletionChunks(t *testing.T) {
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
			body, err := io.ReadAll(req.Body)
			if err != nil {
				t.Fatalf("read request body: %v", err)
			}
			if !strings.Contains(string(body), `"stream":true`) {
				t.Fatalf("expected stream request body, got %s", string(body))
			}
			stream := strings.Join([]string{
				`data: {"id":"chatcmpl-1","model":"gpt-4o","choices":[{"index":0,"delta":{"role":"assistant","content":"hel"},"finish_reason":null}]}`,
				`data: {"id":"chatcmpl-1","model":"gpt-4o","choices":[{"index":0,"delta":{"content":"lo"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`,
				`data: [DONE]`,
				``,
			}, "\n\n")
			return &http.Response{
				StatusCode: 200,
				Status:     "200 OK",
				Body:       io.NopCloser(strings.NewReader(stream)),
				Header:     make(http.Header),
			}, nil
		}),
	}

	var chunks []string
	resp, err := provider.StreamMessage(context.Background(), MessageRequest{
		Model: "gpt-4o",
		Messages: []Message{
			{Role: "user", Content: "hello"},
		},
		MaxTokens: 100,
	}, func(chunk string) {
		chunks = append(chunks, chunk)
	})
	if err != nil {
		t.Fatalf("StreamMessage() error = %v", err)
	}
	if got := strings.Join(chunks, "|"); got != "hel|lo" {
		t.Fatalf("chunks = %q", got)
	}
	if len(resp.Content) != 1 || resp.Content[0].Text != "hello" {
		t.Fatalf("unexpected response content %#v", resp.Content)
	}
	if resp.StopReason != "stop" {
		t.Fatalf("stop reason = %q", resp.StopReason)
	}
	if resp.Usage.InputTokens != 3 || resp.Usage.OutputTokens != 2 {
		t.Fatalf("usage = %#v", resp.Usage)
	}
}

func TestOpenAIProviderStreamsToolCallDeltas(t *testing.T) {
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
			stream := strings.Join([]string{
				`data: {"id":"chatcmpl-1","model":"gpt-4o","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_123","type":"function","function":{"name":"ba","arguments":"{\"command\""}}]},"finish_reason":null}]}`,
				`data: {"id":"chatcmpl-1","model":"gpt-4o","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"name":"sh","arguments":":\"pwd\"}"}}]},"finish_reason":"tool_calls"}]}`,
				`data: [DONE]`,
				``,
			}, "\n\n")
			return &http.Response{
				StatusCode: 200,
				Status:     "200 OK",
				Body:       io.NopCloser(strings.NewReader(stream)),
				Header:     make(http.Header),
			}, nil
		}),
	}

	resp, err := provider.StreamMessage(context.Background(), MessageRequest{
		Model: "gpt-4o",
		Messages: []Message{
			{Role: "user", Content: "run pwd"},
		},
		MaxTokens: 100,
	}, nil)
	if err != nil {
		t.Fatalf("StreamMessage() error = %v", err)
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
