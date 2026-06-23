package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestVertexProviderGenerateContent(t *testing.T) {
	t.Setenv("VERTEX_PROJECT_ID", "proj-1")
	t.Setenv("VERTEX_LOCATION", "us-central1")

	var gotPath, gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if _, ok := body["contents"].([]interface{}); !ok {
			t.Fatalf("expected contents in request, got %#v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"candidates":[{"content":{"role":"model","parts":[{"text":"hello from vertex"}]},"finishReason":"STOP"}],
			"usageMetadata":{"promptTokenCount":3,"candidatesTokenCount":4,"totalTokenCount":7}
		}`))
	}))
	defer server.Close()

	provider, err := NewVertexProvider(Config{
		Provider: "vertex",
		Token:    "tok",
		BaseURL:  server.URL + "/v1",
		Model:    "gemini-test",
		Timeout:  30,
	})
	if err != nil {
		t.Fatalf("NewVertexProvider: %v", err)
	}
	resp, err := provider.CreateMessage(context.Background(), MessageRequest{
		Model: "gemini-test",
		Messages: []Message{{
			Role:    "user",
			Content: "hello",
		}},
	})
	if err != nil {
		t.Fatalf("CreateMessage: %v", err)
	}
	if !strings.Contains(gotPath, "/v1/projects/proj-1/locations/us-central1/publishers/google/models/gemini-test:generateContent") {
		t.Fatalf("unexpected path: %s", gotPath)
	}
	if gotAuth != "Bearer tok" {
		t.Fatalf("auth = %q", gotAuth)
	}
	if len(resp.Content) != 1 || resp.Content[0].Text != "hello from vertex" {
		t.Fatalf("unexpected response: %#v", resp)
	}
}

func TestVertexProviderAddsGemini25ThinkingConfig(t *testing.T) {
	t.Setenv("VERTEX_PROJECT_ID", "proj-1")
	t.Setenv("VERTEX_LOCATION", "us-central1")

	var body map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"candidates":[{"content":{"role":"model","parts":[{"text":"thoughtful"}]},"finishReason":"STOP"}]
		}`))
	}))
	defer server.Close()

	provider, err := NewVertexProvider(Config{
		Provider: "vertex",
		Token:    "tok",
		BaseURL:  server.URL + "/v1",
		Model:    "gemini-2.5-flash",
		Timeout:  30,
	})
	if err != nil {
		t.Fatalf("NewVertexProvider: %v", err)
	}
	_, err = provider.CreateMessage(context.Background(), MessageRequest{
		Model:          "gemini-2.5-flash",
		Messages:       []Message{{Role: "user", Content: "think"}},
		ThinkingConfig: &ThinkingConfig{Enabled: true},
	})
	if err != nil {
		t.Fatalf("CreateMessage: %v", err)
	}
	generationConfig := body["generationConfig"].(map[string]interface{})
	thinkingConfig := generationConfig["thinkingConfig"].(map[string]interface{})
	if thinkingConfig["thinkingBudget"] != float64(-1) {
		t.Fatalf("thinkingConfig = %#v", thinkingConfig)
	}
}

func TestVertexProviderAddsGemini3ThinkingConfig(t *testing.T) {
	t.Setenv("VERTEX_PROJECT_ID", "proj-1")
	t.Setenv("VERTEX_LOCATION", "global")

	var body map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"candidates":[{"content":{"role":"model","parts":[{"text":"deep"}]},"finishReason":"STOP"}]
		}`))
	}))
	defer server.Close()

	provider, err := NewVertexProvider(Config{
		Provider: "vertex",
		Token:    "tok",
		BaseURL:  server.URL + "/v1",
		Model:    "gemini-3.1-flash-lite",
		Timeout:  30,
	})
	if err != nil {
		t.Fatalf("NewVertexProvider: %v", err)
	}
	_, err = provider.CreateMessage(context.Background(), MessageRequest{
		Model:          "gemini-3.1-flash-lite",
		Messages:       []Message{{Role: "user", Content: "think"}},
		ThinkingConfig: &ThinkingConfig{Enabled: true, Level: "high"},
	})
	if err != nil {
		t.Fatalf("CreateMessage: %v", err)
	}
	generationConfig := body["generationConfig"].(map[string]interface{})
	thinkingConfig := generationConfig["thinkingConfig"].(map[string]interface{})
	if thinkingConfig["thinkingLevel"] != "HIGH" {
		t.Fatalf("thinkingConfig = %#v", thinkingConfig)
	}
}

func TestVertexProviderStreamsGenerateContent(t *testing.T) {
	t.Setenv("VERTEX_PROJECT_ID", "proj-1")
	t.Setenv("VERTEX_LOCATION", "us-central1")

	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.String()
		if err := json.NewDecoder(r.Body).Decode(&map[string]interface{}{}); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"hel\"}]}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"lo\"}]},\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":3,\"candidatesTokenCount\":2,\"totalTokenCount\":5}}\n\n"))
	}))
	defer server.Close()

	provider, err := NewVertexProvider(Config{
		Provider: "vertex",
		Token:    "tok",
		BaseURL:  server.URL + "/v1",
		Model:    "gemini-test",
		Timeout:  30,
	})
	if err != nil {
		t.Fatalf("NewVertexProvider: %v", err)
	}
	var chunks []string
	resp, err := provider.StreamMessage(context.Background(), MessageRequest{
		Model:    "gemini-test",
		Messages: []Message{{Role: "user", Content: "hello"}},
	}, func(chunk string) {
		chunks = append(chunks, chunk)
	})
	if err != nil {
		t.Fatalf("StreamMessage: %v", err)
	}
	if !strings.Contains(gotPath, ":streamGenerateContent") || !strings.Contains(gotPath, "alt=sse") {
		t.Fatalf("unexpected path: %s", gotPath)
	}
	if strings.Join(chunks, "") != "hello" {
		t.Fatalf("chunks = %#v", chunks)
	}
	if len(resp.Content) != 1 || resp.Content[0].Text != "hello" {
		t.Fatalf("unexpected response: %#v", resp)
	}
	if resp.Usage.OutputTokens != 2 {
		t.Fatalf("usage = %#v", resp.Usage)
	}
}

func TestVertexProviderFallsBackToGenerateContentWhenStreamEOF(t *testing.T) {
	t.Setenv("VERTEX_PROJECT_ID", "proj-1")
	t.Setenv("VERTEX_LOCATION", "us-central1")

	provider, err := NewVertexProvider(Config{
		Provider: "vertex",
		Token:    "tok",
		BaseURL:  "https://vertex.test/v1",
		Model:    "gemini-test",
		Timeout:  30,
	})
	if err != nil {
		t.Fatalf("NewVertexProvider: %v", err)
	}
	var paths []string
	provider.httpClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		paths = append(paths, r.URL.String())
		if strings.Contains(r.URL.String(), ":streamGenerateContent") {
			return nil, io.EOF
		}
		if strings.Contains(r.URL.String(), ":generateContent") {
			return &http.Response{
				StatusCode: http.StatusOK,
				Status:     "200 OK",
				Header:     make(http.Header),
				Body: io.NopCloser(strings.NewReader(`{
					"candidates":[{"content":{"role":"model","parts":[{"text":"fallback text"}]},"finishReason":"STOP"}],
					"usageMetadata":{"promptTokenCount":3,"candidatesTokenCount":4,"totalTokenCount":7}
				}`)),
				Request: r,
			}, nil
		}
		t.Fatalf("unexpected request: %s", r.URL.String())
		return nil, nil
	})}

	var chunks []string
	resp, err := provider.StreamMessage(context.Background(), MessageRequest{
		Model:    "gemini-test",
		Messages: []Message{{Role: "user", Content: "hello"}},
	}, func(chunk string) {
		chunks = append(chunks, chunk)
	})
	if err != nil {
		t.Fatalf("StreamMessage: %v", err)
	}
	if len(paths) != 2 {
		t.Fatalf("paths = %#v", paths)
	}
	if !strings.Contains(paths[0], ":streamGenerateContent") || !strings.Contains(paths[1], ":generateContent") {
		t.Fatalf("unexpected paths: %#v", paths)
	}
	if strings.Join(chunks, "") != "fallback text" {
		t.Fatalf("chunks = %#v", chunks)
	}
	if len(resp.Content) != 1 || resp.Content[0].Text != "fallback text" {
		t.Fatalf("unexpected response: %#v", resp)
	}
}

func TestVertexProviderSanitizesGeminiToolSchemas(t *testing.T) {
	t.Setenv("VERTEX_PROJECT_ID", "proj-1")
	t.Setenv("VERTEX_LOCATION", "us-central1")

	var body map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"candidates":[{"content":{"role":"model","parts":[{"text":"schema ok"}]},"finishReason":"STOP"}]
		}`))
	}))
	defer server.Close()

	provider, err := NewVertexProvider(Config{
		Provider: "vertex",
		Token:    "tok",
		BaseURL:  server.URL + "/v1",
		Model:    "gemini-test",
		Timeout:  30,
	})
	if err != nil {
		t.Fatalf("NewVertexProvider: %v", err)
	}
	_, err = provider.CreateMessage(context.Background(), MessageRequest{
		Model:    "gemini-test",
		Messages: []Message{{Role: "user", Content: "use tool"}},
		Tools: []Tool{{
			Name:        "gmail_search_threads",
			Description: "Search Gmail",
			InputSchema: map[string]interface{}{
				"type": "object",
				"$defs": map[string]interface{}{
					"label": map[string]interface{}{
						"type":                       "string",
						"x-google-enum-descriptions": []interface{}{"inbox"},
					},
				},
				"properties": map[string]interface{}{
					"labels": map[string]interface{}{
						"type": "array",
						"items": map[string]interface{}{
							"$ref": "#/$defs/label",
						},
					},
					"kind": map[string]interface{}{
						"type":                       []interface{}{"string", "null"},
						"x-google-enum-descriptions": []interface{}{"primary"},
					},
				},
			},
		}},
	})
	if err != nil {
		t.Fatalf("CreateMessage: %v", err)
	}
	raw, _ := json.Marshal(body["tools"])
	text := string(raw)
	if strings.Contains(text, "$defs") || strings.Contains(text, "$ref") || strings.Contains(text, "x-google-enum-descriptions") {
		t.Fatalf("schema was not sanitized: %s", text)
	}
	if !strings.Contains(text, `"nullable":true`) {
		t.Fatalf("nullable type union was not normalized: %s", text)
	}
}

func TestVertexProviderPreservesFunctionCallThoughtSignature(t *testing.T) {
	t.Setenv("VERTEX_PROJECT_ID", "proj-1")
	t.Setenv("VERTEX_LOCATION", "us-central1")

	var body map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"candidates":[{
				"content":{"role":"model","parts":[{
					"functionCall":{"name":"default_api:skill","args":{"name":"fireworks-tech-graph"}},
					"thoughtSignature":"signed-thought"
				}]},
				"finishReason":"STOP"
			}]
		}`))
	}))
	defer server.Close()

	provider, err := NewVertexProvider(Config{
		Provider: "vertex",
		Token:    "tok",
		BaseURL:  server.URL + "/v1",
		Model:    "gemini-test",
		Timeout:  30,
	})
	if err != nil {
		t.Fatalf("NewVertexProvider: %v", err)
	}
	resp, err := provider.CreateMessage(context.Background(), MessageRequest{
		Model:    "gemini-test",
		Messages: []Message{{Role: "user", Content: "draw architecture"}},
	})
	if err != nil {
		t.Fatalf("CreateMessage: %v", err)
	}
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].ThoughtSignature != "signed-thought" {
		t.Fatalf("thought signature was not preserved: %#v", resp.ToolCalls)
	}

	_, err = provider.CreateMessage(context.Background(), MessageRequest{
		Model: "gemini-test",
		Messages: []Message{{
			Role: "assistant",
			ToolCalls: []ToolCall{{
				ID:               "call-1",
				Name:             "default_api:skill",
				Input:            json.RawMessage(`{"name":"fireworks-tech-graph"}`),
				ThoughtSignature: resp.ToolCalls[0].ThoughtSignature,
			}},
		}},
	})
	if err != nil {
		t.Fatalf("CreateMessage with signed tool history: %v", err)
	}
	contents := body["contents"].([]interface{})
	parts := contents[0].(map[string]interface{})["parts"].([]interface{})
	part := parts[0].(map[string]interface{})
	if part["thoughtSignature"] != "signed-thought" {
		t.Fatalf("request functionCall missing thoughtSignature: %#v", part)
	}
}

func TestVertexProviderRefreshesTokenAfterUnauthorized(t *testing.T) {
	t.Setenv("VERTEX_PROJECT_ID", "proj-1")
	t.Setenv("VERTEX_LOCATION", "us-central1")

	var authHeaders []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeaders = append(authHeaders, r.Header.Get("Authorization"))
		if len(authHeaders) == 1 {
			http.Error(w, `{"error":"expired"}`, http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"candidates":[{"content":{"role":"model","parts":[{"text":"ok"}]},"finishReason":"STOP"}]
		}`))
	}))
	defer server.Close()

	provider, err := NewVertexProvider(Config{
		Provider: "vertex",
		Token:    "old-token",
		BaseURL:  server.URL + "/v1",
		Model:    "gemini-test",
		Timeout:  30,
	})
	if err != nil {
		t.Fatalf("NewVertexProvider: %v", err)
	}
	provider.tokenRefresher = func(context.Context) (string, error) {
		return "fresh-token", nil
	}

	resp, err := provider.CreateMessage(context.Background(), MessageRequest{
		Model:    "gemini-test",
		Messages: []Message{{Role: "user", Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("CreateMessage: %v", err)
	}
	if len(authHeaders) != 2 {
		t.Fatalf("request count = %d, want 2", len(authHeaders))
	}
	if authHeaders[0] != "Bearer old-token" || authHeaders[1] != "Bearer fresh-token" {
		t.Fatalf("auth headers = %#v", authHeaders)
	}
	if len(resp.Content) != 1 || resp.Content[0].Text != "ok" {
		t.Fatalf("unexpected response: %#v", resp)
	}
}

func TestVertexProviderMapsToolResultsToFunctionResponses(t *testing.T) {
	t.Setenv("VERTEX_PROJECT_ID", "proj-1")
	t.Setenv("VERTEX_LOCATION", "us-central1")

	var body map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"candidates":[{"content":{"role":"model","parts":[{"text":"done"}]},"finishReason":"STOP"}]
		}`))
	}))
	defer server.Close()

	provider, err := NewVertexProvider(Config{
		Provider: "vertex",
		Token:    "tok",
		BaseURL:  server.URL + "/v1",
		Model:    "gemini-test",
		Timeout:  30,
	})
	if err != nil {
		t.Fatalf("NewVertexProvider: %v", err)
	}
	_, err = provider.CreateMessage(context.Background(), MessageRequest{
		Model: "gemini-test",
		Messages: []Message{
			{Role: "assistant", ToolCalls: []ToolCall{{ID: "call-1", Name: "Artifact", Input: json.RawMessage(`{"filename":"x.png"}`)}}},
			{Role: "tool", ToolCallID: "call-1", ToolName: "Artifact", Content: `{"id":"artifact-1"}`},
		},
	})
	if err != nil {
		t.Fatalf("CreateMessage: %v", err)
	}
	contents, ok := body["contents"].([]interface{})
	if !ok || len(contents) != 2 {
		t.Fatalf("unexpected contents: %#v", body["contents"])
	}
	toolResult, ok := contents[1].(map[string]interface{})
	if !ok {
		t.Fatalf("unexpected tool result content: %#v", contents[1])
	}
	if toolResult["role"] != "user" {
		t.Fatalf("tool result role = %q, want user", toolResult["role"])
	}
	parts, ok := toolResult["parts"].([]interface{})
	if !ok || len(parts) != 1 {
		t.Fatalf("unexpected tool result parts: %#v", toolResult["parts"])
	}
	part, ok := parts[0].(map[string]interface{})
	if !ok {
		t.Fatalf("unexpected function response part: %#v", parts[0])
	}
	response, ok := part["functionResponse"].(map[string]interface{})
	if !ok {
		t.Fatalf("missing functionResponse: %#v", part)
	}
	if response["name"] != "Artifact" {
		t.Fatalf("functionResponse name = %q, want Artifact", response["name"])
	}
}

func TestVertexProviderMapsAttachmentsToGeminiParts(t *testing.T) {
	t.Setenv("VERTEX_PROJECT_ID", "proj-1")
	t.Setenv("VERTEX_LOCATION", "us-central1")

	var body map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"candidates":[{"content":{"role":"model","parts":[{"text":"done"}]},"finishReason":"STOP"}]
		}`))
	}))
	defer server.Close()

	provider, err := NewVertexProvider(Config{
		Provider: "vertex",
		Token:    "tok",
		BaseURL:  server.URL + "/v1",
		Model:    "gemini-test",
		Timeout:  30,
	})
	if err != nil {
		t.Fatalf("NewVertexProvider: %v", err)
	}
	_, err = provider.CreateMessage(context.Background(), MessageRequest{
		Model: "gemini-test",
		Messages: []Message{{
			Role: "user",
			Content: []ContentBlock{
				{Type: "text", Text: "analyze these"},
				{Type: "image", Source: map[string]interface{}{
					"media_type": "image/png",
					"data":       "aW1n",
				}},
				{Type: "file", Source: map[string]interface{}{
					"media_type": "application/pdf",
					"file_uri":   "gs://bucket/report.pdf",
				}},
			},
		}},
	})
	if err != nil {
		t.Fatalf("CreateMessage: %v", err)
	}
	contents := body["contents"].([]interface{})
	parts := contents[0].(map[string]interface{})["parts"].([]interface{})
	if len(parts) != 3 {
		t.Fatalf("parts len = %d, want 3: %#v", len(parts), parts)
	}
	if _, ok := parts[1].(map[string]interface{})["inlineData"]; !ok {
		t.Fatalf("missing inlineData: %#v", parts[1])
	}
	if _, ok := parts[2].(map[string]interface{})["fileData"]; !ok {
		t.Fatalf("missing fileData: %#v", parts[2])
	}
}

func TestVertexProviderBatchesConsecutiveToolResults(t *testing.T) {
	t.Setenv("VERTEX_PROJECT_ID", "proj-1")
	t.Setenv("VERTEX_LOCATION", "us-central1")

	var body map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"candidates":[{"content":{"role":"model","parts":[{"text":"done"}]},"finishReason":"STOP"}]
		}`))
	}))
	defer server.Close()

	provider, err := NewVertexProvider(Config{
		Provider: "vertex",
		Token:    "tok",
		BaseURL:  server.URL + "/v1",
		Model:    "gemini-test",
		Timeout:  30,
	})
	if err != nil {
		t.Fatalf("NewVertexProvider: %v", err)
	}
	_, err = provider.CreateMessage(context.Background(), MessageRequest{
		Model: "gemini-test",
		Messages: []Message{
			{Role: "assistant", ToolCalls: []ToolCall{
				{ID: "call-1", Name: "WebFetch", Input: json.RawMessage(`{"url":"https://example.com/a"}`)},
				{ID: "call-2", Name: "WebFetch", Input: json.RawMessage(`{"url":"https://example.com/b"}`)},
			}},
			{Role: "tool", ToolCallID: "call-1", ToolName: "WebFetch", Content: "result a"},
			{Role: "tool", ToolCallID: "call-2", ToolName: "WebFetch", Content: "result b"},
		},
	})
	if err != nil {
		t.Fatalf("CreateMessage: %v", err)
	}
	contents, ok := body["contents"].([]interface{})
	if !ok || len(contents) != 2 {
		t.Fatalf("unexpected contents: %#v", body["contents"])
	}
	assistant, ok := contents[0].(map[string]interface{})
	if !ok {
		t.Fatalf("unexpected assistant content: %#v", contents[0])
	}
	assistantParts, ok := assistant["parts"].([]interface{})
	if !ok || len(assistantParts) != 2 {
		t.Fatalf("expected two function calls in one model turn, got %#v", assistant["parts"])
	}
	toolResult, ok := contents[1].(map[string]interface{})
	if !ok {
		t.Fatalf("unexpected tool result content: %#v", contents[1])
	}
	if toolResult["role"] != "user" {
		t.Fatalf("tool result role = %q, want user", toolResult["role"])
	}
	parts, ok := toolResult["parts"].([]interface{})
	if !ok || len(parts) != 2 {
		t.Fatalf("expected two function responses in one user turn, got %#v", toolResult["parts"])
	}
}

func TestVertexProviderCallsClaudeRawPredict(t *testing.T) {
	t.Setenv("VERTEX_PROJECT_ID", "proj-1")
	t.Setenv("VERTEX_LOCATION", "us-central1")
	t.Setenv("VERTEX_ANTHROPIC_LOCATION", "global")

	var gotPath string
	var body map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"msg-1",
			"type":"message",
			"role":"assistant",
			"model":"claude-sonnet-4-5",
			"content":[
				{"type":"text","text":"I can help."},
				{"type":"tool_use","id":"toolu_1","name":"Artifact","input":{"filename":"x.svg"}}
			],
			"stop_reason":"tool_use",
			"usage":{"input_tokens":11,"output_tokens":7}
		}`))
	}))
	defer server.Close()

	provider, err := NewVertexProvider(Config{
		Provider: "vertex",
		Token:    "tok",
		BaseURL:  server.URL + "/v1",
		Model:    "claude-sonnet-4-5@20250929",
		Timeout:  30,
	})
	if err != nil {
		t.Fatalf("NewVertexProvider: %v", err)
	}
	resp, err := provider.CreateMessage(context.Background(), MessageRequest{
		Model:     "claude-sonnet-4-5@20250929",
		System:    "You are concise.",
		MaxTokens: 128,
		Tools: []Tool{{
			Name:        "Artifact",
			Description: "Register an artifact",
			InputSchema: map[string]interface{}{"type": "object"},
		}},
		Messages: []Message{{
			Role: "user",
			Content: []ContentBlock{
				{Type: "text", Text: "make a diagram"},
				{Type: "image", Source: map[string]interface{}{
					"media_type": "image/png",
					"data":       "aW1n",
				}},
			},
		}},
	})
	if err != nil {
		t.Fatalf("CreateMessage: %v", err)
	}
	wantPath := "/v1/projects/proj-1/locations/global/publishers/anthropic/models/claude-sonnet-4-5@20250929:rawPredict"
	if gotPath != wantPath {
		t.Fatalf("path = %s, want %s", gotPath, wantPath)
	}
	if body["anthropic_version"] != vertexAnthropicVersion {
		t.Fatalf("anthropic_version = %#v", body["anthropic_version"])
	}
	if body["system"] != "You are concise." || body["max_tokens"].(float64) != 128 {
		t.Fatalf("unexpected system/max_tokens: %#v", body)
	}
	tools := body["tools"].([]interface{})
	if tools[0].(map[string]interface{})["name"] != "Artifact" {
		t.Fatalf("unexpected tools: %#v", tools)
	}
	messages := body["messages"].([]interface{})
	content := messages[0].(map[string]interface{})["content"].([]interface{})
	if content[1].(map[string]interface{})["type"] != "image" {
		t.Fatalf("missing image block: %#v", content)
	}
	source := content[1].(map[string]interface{})["source"].(map[string]interface{})
	if source["type"] != "base64" || source["media_type"] != "image/png" {
		t.Fatalf("unexpected image source: %#v", source)
	}
	if len(resp.Content) != 1 || resp.Content[0].Text != "I can help." {
		t.Fatalf("unexpected content: %#v", resp.Content)
	}
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].Name != "Artifact" {
		t.Fatalf("unexpected tool calls: %#v", resp.ToolCalls)
	}
	if resp.Usage.InputTokens != 11 || resp.Usage.OutputTokens != 7 {
		t.Fatalf("usage = %#v", resp.Usage)
	}
}

func TestVertexProviderMapsClaudeToolResults(t *testing.T) {
	t.Setenv("VERTEX_PROJECT_ID", "proj-1")
	t.Setenv("VERTEX_ANTHROPIC_LOCATION", "us-east5")

	var body map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"msg-2",
			"role":"assistant",
			"content":[{"type":"text","text":"done"}],
			"stop_reason":"end_turn"
		}`))
	}))
	defer server.Close()

	provider, err := NewVertexProvider(Config{
		Provider: "vertex",
		Token:    "tok",
		BaseURL:  server.URL + "/v1",
		Model:    "claude-sonnet-4-5",
		Timeout:  30,
	})
	if err != nil {
		t.Fatalf("NewVertexProvider: %v", err)
	}
	_, err = provider.CreateMessage(context.Background(), MessageRequest{
		Model: "claude-sonnet-4-5",
		Messages: []Message{
			{Role: "assistant", ToolCalls: []ToolCall{{ID: "call-1", Name: "Artifact", Input: json.RawMessage(`{"filename":"x.png"}`)}}},
			{Role: "tool", ToolCallID: "call-1", ToolName: "Artifact", Content: `{"id":"artifact-1"}`},
		},
	})
	if err != nil {
		t.Fatalf("CreateMessage: %v", err)
	}
	messages := body["messages"].([]interface{})
	if len(messages) != 2 {
		t.Fatalf("messages len = %d, want 2: %#v", len(messages), messages)
	}
	assistantBlocks := messages[0].(map[string]interface{})["content"].([]interface{})
	if assistantBlocks[0].(map[string]interface{})["type"] != "tool_use" {
		t.Fatalf("missing tool_use block: %#v", assistantBlocks)
	}
	userBlocks := messages[1].(map[string]interface{})["content"].([]interface{})
	toolResult := userBlocks[0].(map[string]interface{})
	if toolResult["type"] != "tool_result" || toolResult["tool_use_id"] != "call-1" {
		t.Fatalf("unexpected tool_result block: %#v", toolResult)
	}
}

func TestVertexEndpointBaseURLForGlobalAndMultiRegion(t *testing.T) {
	cases := map[string]string{
		"global":   "https://aiplatform.googleapis.com/v1",
		"us":       "https://aiplatform.us.rep.googleapis.com/v1",
		"eu":       "https://aiplatform.eu.rep.googleapis.com/v1",
		"us-east5": "https://us-east5-aiplatform.googleapis.com/v1",
	}
	for location, want := range cases {
		if got := vertexEndpointBaseURL(location); got != want {
			t.Fatalf("vertexEndpointBaseURL(%q) = %q, want %q", location, got, want)
		}
	}
}

func TestVertexProviderUsesConfigLocationOverride(t *testing.T) {
	t.Setenv("VERTEX_PROJECT_ID", "proj-1")
	t.Setenv("VERTEX_LOCATION", "us-central1")

	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"candidates":[{"content":{"role":"model","parts":[{"text":"ok"}]},"finishReason":"STOP"}]
		}`))
	}))
	defer server.Close()

	provider, err := NewVertexProvider(Config{
		Provider:       "vertex",
		Token:          "tok",
		BaseURL:        server.URL + "/v1",
		Model:          "gemini-3.1-flash-lite",
		Timeout:        30,
		VertexLocation: "global",
	})
	if err != nil {
		t.Fatalf("NewVertexProvider: %v", err)
	}
	if _, err := provider.CreateMessage(context.Background(), MessageRequest{
		Model:    "gemini-3.1-flash-lite",
		Messages: []Message{{Role: "user", Content: "hello"}},
	}); err != nil {
		t.Fatalf("CreateMessage: %v", err)
	}
	want := "/v1/projects/proj-1/locations/global/publishers/google/models/gemini-3.1-flash-lite:generateContent"
	if gotPath != want {
		t.Fatalf("path = %s, want %s", gotPath, want)
	}
}
