package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// OpenAIProvider implements Provider for OpenAI API (GPT models)
type OpenAIProvider struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
	config     Config
}

// NewOpenAIProvider creates a new OpenAI provider
func NewOpenAIProvider(cfg Config) (*OpenAIProvider, error) {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}

	apiKey := cfg.APIKey
	if apiKey == "" && cfg.Token != "" {
		apiKey = cfg.Token
	}

	timeout := time.Duration(cfg.Timeout) * time.Second
	if timeout <= 0 {
		timeout = 600 * time.Second
	}

	return &OpenAIProvider{
		apiKey:  apiKey,
		baseURL: strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{
			Timeout: timeout,
		},
		config: cfg,
	}, nil
}

// openAIRequest represents OpenAI API request format
type openAIRequest struct {
	Model       string              `json:"model"`
	Messages    []openAIMessage     `json:"messages"`
	MaxTokens   int                 `json:"max_tokens,omitempty"`
	Temperature float64             `json:"temperature,omitempty"`
	TopP        float64             `json:"top_p,omitempty"`
	Stream      bool                `json:"stream,omitempty"`
	Tools       []openAITool        `json:"tools,omitempty"`
}

type openAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAITool struct {
	Type     string                 `json:"type"`
	Function openAIFunction         `json:"function"`
}

type openAIFunction struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	Parameters  map[string]interface{} `json:"parameters,omitempty"`
}

// openAIResponse represents OpenAI API response format
type openAIResponse struct {
	ID      string          `json:"id"`
	Object  string          `json:"object"`
	Created int64           `json:"created"`
	Model   string          `json:"model"`
	Choices []openAIChoice  `json:"choices"`
	Usage   openAIUsage     `json:"usage"`
}

type openAIChoice struct {
	Index        int           `json:"index"`
	Message      openAIMessage `json:"message"`
	FinishReason string        `json:"finish_reason"`
}

type openAIUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// CreateMessage sends a message request to OpenAI API
func (p *OpenAIProvider) CreateMessage(ctx context.Context, request MessageRequest) (*MessageResponse, error) {
	// Convert unified request to OpenAI format
	openAIReq := openAIRequest{
		Model:       request.Model,
		MaxTokens:   request.MaxTokens,
		Temperature: request.Temperature,
		TopP:        request.TopP,
		Stream:      request.Stream,
		Messages:    make([]openAIMessage, 0, len(request.Messages)+1),
	}

	// Add system message if present
	if request.System != "" {
		openAIReq.Messages = append(openAIReq.Messages, openAIMessage{
			Role:    "system",
			Content: request.System,
		})
	}

	// Convert messages
	for _, msg := range request.Messages {
		content := ""
		switch v := msg.Content.(type) {
		case string:
			content = v
		case []ContentBlock:
			// Concatenate text blocks
			var texts []string
			for _, block := range v {
				if block.Type == "text" {
					texts = append(texts, block.Text)
				}
			}
			content = strings.Join(texts, "\n")
		}

		openAIReq.Messages = append(openAIReq.Messages, openAIMessage{
			Role:    msg.Role,
			Content: content,
		})
	}

	// Convert tools if present
	if len(request.Tools) > 0 {
		openAIReq.Tools = make([]openAITool, len(request.Tools))
		for i, tool := range request.Tools {
			openAIReq.Tools[i] = openAITool{
				Type: "function",
				Function: openAIFunction{
					Name:        tool.Name,
					Description: tool.Description,
					Parameters:  tool.InputSchema,
				},
			}
		}
	}

	// Marshal request
	body, err := json.Marshal(openAIReq)
	if err != nil {
		return nil, err
	}

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)

	// Send request
	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusBadRequest {
		data, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("openai request failed: %s: %s", resp.Status, string(data))
	}

	// Parse response
	var openAIResp openAIResponse
	if err := json.NewDecoder(resp.Body).Decode(&openAIResp); err != nil {
		return nil, err
	}

	if len(openAIResp.Choices) == 0 {
		return nil, fmt.Errorf("no choices in response")
	}

	// Convert to unified format
	choice := openAIResp.Choices[0]
	return &MessageResponse{
		ID:    openAIResp.ID,
		Model: openAIResp.Model,
		Role:  choice.Message.Role,
		Content: []ContentBlock{
			{
				Type: "text",
				Text: choice.Message.Content,
			},
		},
		StopReason: choice.FinishReason,
		Usage: Usage{
			InputTokens:  openAIResp.Usage.PromptTokens,
			OutputTokens: openAIResp.Usage.CompletionTokens,
		},
	}, nil
}

// Name returns the provider name
func (p *OpenAIProvider) Name() string {
	return "openai"
}

// SupportedModels returns supported GPT models
func (p *OpenAIProvider) SupportedModels() []string {
	return []string{
		"gpt-4",
		"gpt-4-turbo",
		"gpt-4-turbo-preview",
		"gpt-4-0125-preview",
		"gpt-4-1106-preview",
		"gpt-4o",
		"gpt-4o-mini",
		"gpt-3.5-turbo",
		"gpt-3.5-turbo-16k",
	}
}
