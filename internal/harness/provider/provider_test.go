package provider_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"claude-codex/internal/harness/provider"
)

func TestFactory(t *testing.T) {
	factory := provider.NewFactory()

	tests := []struct {
		name         string
		providerName string
		wantErr      bool
	}{
		{"anthropic", "anthropic", false},
		{"claude", "claude", false},
		{"openai", "openai", false},
		{"gpt", "gpt", false},
		{"deepseek", "deepseek", false},
		{"qwen", "qwen", false},
		{"dashscope", "dashscope", false},
		{"gemini", "gemini", false},
		{"google", "google", false},
		{"shortapi", "shortapi", false},
		{"short", "short", false},
		{"invalid", "invalid", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := provider.Config{
				Provider: tt.providerName,
				APIKey:   "test-key",
				Model:    "test-model",
				Timeout:  600,
			}

			_, err := factory.CreateProvider(cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("CreateProvider() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestProviderInfo(t *testing.T) {
	factory := provider.NewFactory()

	tests := []struct {
		name         string
		providerName string
		wantName     string
		wantModels   int
		wantErr      bool
	}{
		{"anthropic", "anthropic", "anthropic", 7, false},
		{"openai", "openai", "openai", 9, false},
		{"deepseek", "deepseek", "deepseek", 2, false},
		{"qwen", "qwen", "qwen", 7, false},
		{"gemini", "gemini", "gemini", 5, false},
		{"shortapi", "shortapi", "shortapi", 9, false},
		{"invalid", "invalid", "", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name, models, err := factory.GetProviderInfo(tt.providerName)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetProviderInfo() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if name != tt.wantName {
					t.Errorf("GetProviderInfo() name = %v, want %v", name, tt.wantName)
				}
				if len(models) != tt.wantModels {
					t.Errorf("GetProviderInfo() models count = %v, want %v", len(models), tt.wantModels)
				}
			}
		})
	}
}

func TestValidateConfig(t *testing.T) {
	factory := provider.NewFactory()

	tests := []struct {
		name    string
		cfg     provider.Config
		wantErr bool
	}{
		{
			name: "valid anthropic",
			cfg: provider.Config{
				Provider: "anthropic",
				APIKey:   "test-key",
				Model:    "claude-sonnet-4-5",
			},
			wantErr: false,
		},
		{
			name: "valid openai",
			cfg: provider.Config{
				Provider: "openai",
				APIKey:   "test-key",
				Model:    "gpt-4",
			},
			wantErr: false,
		},
		{
			name: "valid deepseek",
			cfg: provider.Config{
				Provider: "deepseek",
				APIKey:   "test-key",
				Model:    "deepseek-chat",
			},
			wantErr: false,
		},
		{
			name: "valid qwen",
			cfg: provider.Config{
				Provider: "qwen",
				APIKey:   "test-key",
				Model:    "qwen-plus",
			},
			wantErr: false,
		},
		{
			name: "valid shortapi",
			cfg: provider.Config{
				Provider: "shortapi",
				APIKey:   "test-key",
				Model:    "google/gemini-3.1-pro-preview",
			},
			wantErr: false,
		},
		{
			name: "missing provider",
			cfg: provider.Config{
				APIKey: "test-key",
				Model:  "test-model",
			},
			wantErr: true,
		},
		{
			name: "missing api key",
			cfg: provider.Config{
				Provider: "anthropic",
				Model:    "test-model",
			},
			wantErr: true,
		},
		{
			name: "missing model",
			cfg: provider.Config{
				Provider: "anthropic",
				APIKey:   "test-key",
			},
			wantErr: true,
		},
		{
			name: "invalid provider",
			cfg: provider.Config{
				Provider: "invalid",
				APIKey:   "test-key",
				Model:    "test-model",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := factory.ValidateConfig(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestDefaultConfig(t *testing.T) {
	factory := provider.NewFactory()

	tests := []struct {
		name         string
		providerName string
		wantProvider string
		wantModel    string
		wantErr      bool
	}{
		{"anthropic", "anthropic", "anthropic", "claude-sonnet-4-5", false},
		{"openai", "openai", "openai", "gpt-4o", false},
		{"deepseek", "deepseek", "deepseek", "deepseek-chat", false},
		{"qwen", "qwen", "qwen", "qwen-plus", false},
		{"gemini", "gemini", "gemini", "gemini-1.5-pro", false},
		{"shortapi", "shortapi", "shortapi", "google/gemini-3.1-pro-preview", false},
		{"invalid", "invalid", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := factory.DefaultConfig(tt.providerName)
			if (err != nil) != tt.wantErr {
				t.Errorf("DefaultConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if cfg.Provider != tt.wantProvider {
					t.Errorf("DefaultConfig() provider = %v, want %v", cfg.Provider, tt.wantProvider)
				}
				if cfg.Model != tt.wantModel {
					t.Errorf("DefaultConfig() model = %v, want %v", cfg.Model, tt.wantModel)
				}
			}
		})
	}
}

func TestDeepSeekProviderUsesOpenAICompatibleEndpoint(t *testing.T) {
	var gotPath string
	var gotAuth string
	var gotModel string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		gotModel, _ = body["model"].(string)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl-test","object":"chat.completion","created":1,"model":"deepseek-chat","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}}`))
	}))
	defer server.Close()

	p, err := provider.NewDeepSeekProvider(provider.Config{
		APIKey:  "test-key",
		BaseURL: server.URL,
		Timeout: 1,
	})
	if err != nil {
		t.Fatalf("NewDeepSeekProvider: %v", err)
	}
	resp, err := p.CreateMessage(context.Background(), provider.MessageRequest{
		Messages: []provider.Message{{Role: "user", Content: "ping"}},
	})
	if err != nil {
		t.Fatalf("CreateMessage: %v", err)
	}
	if gotPath != "/chat/completions" {
		t.Fatalf("path = %q, want /chat/completions", gotPath)
	}
	if gotAuth != "Bearer test-key" {
		t.Fatalf("authorization = %q, want bearer test-key", gotAuth)
	}
	if gotModel != "deepseek-chat" {
		t.Fatalf("model = %q, want deepseek-chat", gotModel)
	}
	if len(resp.Content) != 1 || resp.Content[0].Text != "ok" {
		t.Fatalf("unexpected response: %#v", resp)
	}
}

func TestDeepSeekProviderSanitizesToolSchemas(t *testing.T) {
	var gotTool map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		tools, _ := body["tools"].([]any)
		if len(tools) != 1 {
			t.Fatalf("tools = %#v, want one tool", body["tools"])
		}
		gotTool, _ = tools[0].(map[string]any)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl-test","object":"chat.completion","created":1,"model":"deepseek-chat","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}}`))
	}))
	defer server.Close()

	p, err := provider.NewDeepSeekProvider(provider.Config{
		APIKey:  "test-key",
		BaseURL: server.URL,
		Timeout: 1,
	})
	if err != nil {
		t.Fatalf("NewDeepSeekProvider: %v", err)
	}
	_, err = p.CreateMessage(context.Background(), provider.MessageRequest{
		Messages: []provider.Message{{Role: "user", Content: "ping"}},
		Tools: []provider.Tool{{
			Name: "search",
			InputSchema: map[string]any{
				"type": "object",
				"$defs": map[string]any{
					"item": map[string]any{"type": "object", "properties": map[string]any{"value": map[string]any{"type": "string"}}},
				},
				"properties": map[string]any{
					"items": map[string]any{"type": "array", "items": map[string]any{"$ref": "#/$defs/item"}},
					"mode":  map[string]any{"type": "string", "enum": []any{true, "safe"}},
					"x":     map[string]any{"type": "string", "x-google-enum-descriptions": []any{"bad"}},
				},
			},
		}},
	})
	if err != nil {
		t.Fatalf("CreateMessage: %v", err)
	}
	function, _ := gotTool["function"].(map[string]any)
	params, _ := function["parameters"].(map[string]any)
	if _, ok := params["$defs"]; ok {
		t.Fatalf("schema still contains $defs: %#v", params)
	}
	props, _ := params["properties"].(map[string]any)
	items, _ := props["items"].(map[string]any)
	itemSchema, _ := items["items"].(map[string]any)
	if _, ok := itemSchema["$ref"]; ok {
		t.Fatalf("schema still contains $ref: %#v", itemSchema)
	}
	mode, _ := props["mode"].(map[string]any)
	enum, _ := mode["enum"].([]any)
	if len(enum) != 1 || enum[0] != "safe" {
		t.Fatalf("enum = %#v, want only safe", enum)
	}
	x, _ := props["x"].(map[string]any)
	if _, ok := x["x-google-enum-descriptions"]; ok {
		t.Fatalf("schema still contains x-google-enum-descriptions: %#v", x)
	}
}

func TestMessageRequestConversion(t *testing.T) {
	ctx := context.Background()

	// Test basic message structure
	req := provider.MessageRequest{
		Model: "test-model",
		Messages: []provider.Message{
			{
				Role:    "user",
				Content: "Hello, world!",
			},
		},
		MaxTokens:   1000,
		Temperature: 0.7,
		System:      "You are a helpful assistant",
	}

	// Verify structure
	if len(req.Messages) != 1 {
		t.Errorf("Expected 1 message, got %d", len(req.Messages))
	}

	if req.Messages[0].Role != "user" {
		t.Errorf("Expected role 'user', got '%s'", req.Messages[0].Role)
	}

	// Test with content blocks
	req2 := provider.MessageRequest{
		Model: "test-model",
		Messages: []provider.Message{
			{
				Role: "user",
				Content: []provider.ContentBlock{
					{Type: "text", Text: "Hello"},
					{Type: "text", Text: "World"},
				},
			},
		},
	}

	if len(req2.Messages) != 1 {
		t.Errorf("Expected 1 message, got %d", len(req2.Messages))
	}

	_ = ctx // Suppress unused warning
}
