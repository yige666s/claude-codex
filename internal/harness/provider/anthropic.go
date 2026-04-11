package provider

import (
	"context"
	"time"

	api "claude-codex/internal/harness/anthropic"
)

// AnthropicProvider implements Provider for Anthropic Claude API
type AnthropicProvider struct {
	client *api.Client
	config Config
}

// NewAnthropicProvider creates a new Anthropic provider
func NewAnthropicProvider(cfg Config) (*AnthropicProvider, error) {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = "https://api.anthropic.com"
	}

	apiKey := cfg.APIKey
	if apiKey == "" && cfg.Token != "" {
		apiKey = cfg.Token
	}

	timeout := time.Duration(cfg.Timeout) * time.Second
	if timeout <= 0 {
		timeout = 600 * time.Second
	}

	client := api.NewClient(apiKey, baseURL, timeout)

	return &AnthropicProvider{
		client: client,
		config: cfg,
	}, nil
}

// CreateMessage sends a message request to Anthropic API
func (p *AnthropicProvider) CreateMessage(ctx context.Context, request MessageRequest) (*MessageResponse, error) {
	// Convert unified request to Anthropic format
	anthropicReq := api.MessageRequest{
		Model:     request.Model,
		MaxTokens: request.MaxTokens,
		System:    request.System,
		Messages:  make([]api.InputMessage, len(request.Messages)),
		Stream:    request.Stream,
	}

	// Convert messages
	for i, msg := range request.Messages {
		var contentBlocks []api.ContentBlock

		switch v := msg.Content.(type) {
		case string:
			contentBlocks = []api.ContentBlock{
				{
					Type: "text",
					Text: v,
				},
			}
		case []ContentBlock:
			contentBlocks = make([]api.ContentBlock, len(v))
			for j, block := range v {
				contentBlocks[j] = api.ContentBlock{
					Type: block.Type,
					Text: block.Text,
				}
			}
		}

		anthropicReq.Messages[i] = api.InputMessage{
			Role:    msg.Role,
			Content: contentBlocks,
		}
	}

	// Call Anthropic API
	resp, err := p.client.CreateMessage(ctx, anthropicReq)
	if err != nil {
		return nil, err
	}

	// Convert response to unified format
	content := make([]ContentBlock, len(resp.Content))
	for i, block := range resp.Content {
		content[i] = ContentBlock{
			Type: block.Type,
			Text: block.Text,
		}
	}

	return &MessageResponse{
		ID:         resp.ID,
		Model:      resp.Model,
		Role:       resp.Role,
		Content:    content,
		StopReason: resp.StopReason,
		Usage: Usage{
			InputTokens:  0, // Anthropic API doesn't return usage in current implementation
			OutputTokens: 0,
		},
	}, nil
}

// Name returns the provider name
func (p *AnthropicProvider) Name() string {
	return "anthropic"
}

// SupportedModels returns supported Claude models
func (p *AnthropicProvider) SupportedModels() []string {
	return []string{
		"claude-opus-4",
		"claude-sonnet-4-5",
		"claude-sonnet-3-5",
		"claude-haiku-3-5",
		"claude-3-opus-20240229",
		"claude-3-sonnet-20240229",
		"claude-3-haiku-20240307",
	}
}

// GetClient returns the underlying Anthropic API client
func (p *AnthropicProvider) GetClient() *api.Client {
	return p.client
}

