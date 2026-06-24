package provider

import (
	"context"
	"strings"
)

const (
	defaultNVIDIABaseURL = "https://integrate.api.nvidia.com/v1"
	defaultNVIDIAModel   = "nvidia/nemotron-3-ultra-550b-a55b"
)

// NVIDIAProvider uses NVIDIA NIM's OpenAI-compatible chat completions endpoint.
type NVIDIAProvider struct {
	*OpenAIProvider
}

func NewNVIDIAProvider(cfg Config) (*NVIDIAProvider, error) {
	if cfg.BaseURL == "" {
		cfg.BaseURL = defaultNVIDIABaseURL
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	if cfg.BaseURL == "https://integrate.api.nvidia.com" {
		cfg.BaseURL = defaultNVIDIABaseURL
	}
	if cfg.Model == "" {
		cfg.Model = defaultNVIDIAModel
	}
	cfg.Provider = "nvidia"
	openai, err := NewOpenAIProvider(cfg)
	if err != nil {
		return nil, err
	}
	return &NVIDIAProvider{OpenAIProvider: openai}, nil
}

func (p *NVIDIAProvider) CreateMessage(ctx context.Context, request MessageRequest) (*MessageResponse, error) {
	if strings.TrimSpace(request.Model) == "" {
		request.Model = defaultNVIDIAModel
	}
	return p.OpenAIProvider.CreateMessage(ctx, request)
}

func (p *NVIDIAProvider) Name() string {
	return "nvidia"
}

func (p *NVIDIAProvider) SupportedModels() []string {
	return []string{
		"nvidia/nemotron-3-ultra-550b-a55b",
		"nvidia/nemotron-3-super-120b-a12b",
		"nvidia/nemotron-3-nano-30b-a3b",
		"deepseek-ai/deepseek-v4-flash",
		"qwen/qwen3-coder-480b-a35b-instruct",
		"qwen/qwen3-next-80b-a3b-instruct",
		"openai/gpt-oss-120b",
	}
}
