package provider

import (
	"context"
	"fmt"
	"strings"
)

// Factory creates providers based on configuration
type Factory struct{}

// NewFactory creates a new provider factory
func NewFactory() *Factory {
	return &Factory{}
}

// CreateProvider creates a provider based on the configuration
func (f *Factory) CreateProvider(cfg Config) (Provider, error) {
	provider := strings.ToLower(cfg.Provider)

	switch provider {
	case "anthropic", "claude":
		return NewAnthropicProvider(cfg)
	case "openai", "gpt":
		return NewOpenAIProvider(cfg)
	case "gemini", "google":
		return NewGeminiProvider(cfg)
	case "bedrock", "aws":
		return NewBedrockProvider(cfg)
	case "vertex", "gcp":
		return NewVertexProvider(cfg)
	default:
		return nil, fmt.Errorf("unsupported provider: %s", cfg.Provider)
	}
}

// ListProviders returns a list of supported providers
func (f *Factory) ListProviders() []string {
	return []string{"anthropic", "openai", "gemini", "bedrock", "vertex"}
}

// GetProviderInfo returns information about a specific provider
func (f *Factory) GetProviderInfo(providerName string) (string, []string, error) {
	provider := strings.ToLower(providerName)

	switch provider {
	case "anthropic", "claude":
		p := &AnthropicProvider{}
		return p.Name(), p.SupportedModels(), nil
	case "openai", "gpt":
		p := &OpenAIProvider{}
		return p.Name(), p.SupportedModels(), nil
	case "gemini", "google":
		p := &GeminiProvider{}
		return p.Name(), p.SupportedModels(), nil
	case "bedrock", "aws":
		p := &BedrockProvider{}
		return p.Name(), p.SupportedModels(), nil
	case "vertex", "gcp":
		p := &VertexProvider{}
		return p.Name(), p.SupportedModels(), nil
	default:
		return "", nil, fmt.Errorf("unsupported provider: %s", providerName)
	}
}

// ValidateConfig validates provider configuration
func (f *Factory) ValidateConfig(cfg Config) error {
	if cfg.Provider == "" {
		return fmt.Errorf("provider is required")
	}

	provider := strings.ToLower(cfg.Provider)
	if provider != "anthropic" && provider != "claude" &&
	   provider != "openai" && provider != "gpt" &&
	   provider != "gemini" && provider != "google" &&
	   provider != "bedrock" && provider != "aws" &&
	   provider != "vertex" && provider != "gcp" {
		return fmt.Errorf("unsupported provider: %s", cfg.Provider)
	}

	if cfg.APIKey == "" && cfg.Token == "" {
		return fmt.Errorf("api_key or token is required")
	}

	if cfg.Model == "" {
		return fmt.Errorf("model is required")
	}

	return nil
}

// DefaultConfig returns default configuration for a provider
func (f *Factory) DefaultConfig(providerName string) (Config, error) {
	provider := strings.ToLower(providerName)

	switch provider {
	case "anthropic", "claude":
		return Config{
			Provider: "anthropic",
			BaseURL:  "https://api.anthropic.com",
			Model:    "claude-sonnet-4-5",
			Timeout:  600,
		}, nil
	case "openai", "gpt":
		return Config{
			Provider: "openai",
			BaseURL:  "https://api.openai.com/v1",
			Model:    "gpt-4o",
			Timeout:  600,
		}, nil
	case "gemini", "google":
		return Config{
			Provider: "gemini",
			BaseURL:  "https://generativelanguage.googleapis.com/v1beta",
			Model:    "gemini-1.5-pro",
			Timeout:  600,
		}, nil
	default:
		return Config{}, fmt.Errorf("unsupported provider: %s", providerName)
	}
}

// CreateProviderFromEnv creates a provider from environment variables
func CreateProviderFromEnv(ctx context.Context, providerName string) (Provider, error) {
	factory := NewFactory()

	cfg, err := factory.DefaultConfig(providerName)
	if err != nil {
		return nil, err
	}

	// Try to get API key from environment
	// This is a placeholder - actual implementation would check env vars

	return factory.CreateProvider(cfg)
}
