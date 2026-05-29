package provider

import "strings"

const (
	defaultShortAPIBaseURL = "https://api.shortapi.ai/v1"
	defaultShortAPIModel   = "google/gemini-3.1-pro-preview"
)

// ShortAPIProvider uses ShortAPI's OpenAI-compatible chat completions endpoint.
type ShortAPIProvider struct {
	*OpenAIProvider
}

func NewShortAPIProvider(cfg Config) (*ShortAPIProvider, error) {
	if cfg.BaseURL == "" {
		cfg.BaseURL = defaultShortAPIBaseURL
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	if cfg.BaseURL == "https://api.shortapi.ai" {
		cfg.BaseURL = defaultShortAPIBaseURL
	}
	if cfg.Model == "" {
		cfg.Model = defaultShortAPIModel
	}
	cfg.Provider = "shortapi"
	openai, err := NewOpenAIProvider(cfg)
	if err != nil {
		return nil, err
	}
	return &ShortAPIProvider{OpenAIProvider: openai}, nil
}

func (p *ShortAPIProvider) Name() string {
	return "shortapi"
}

func (p *ShortAPIProvider) SupportedModels() []string {
	return []string{
		"google/gemini-3.1-pro-preview",
		"openai/gpt-5.4",
		"openai/gpt-5.4-pro",
		"openai/gpt-5.4-mini",
		"openai/gpt-5.4-nano",
		"anthropic/claude-sonnet-4.6",
		"anthropic/claude-opus-4.6",
		"deepseek/deepseek-v3.2",
		"qwen/qwen-3.6-plus",
	}
}
