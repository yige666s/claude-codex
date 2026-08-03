package provider

import (
	"context"
	"errors"
)

var ErrBedrockNotImplemented = errors.New("AWS Bedrock provider is not implemented")

// BedrockProvider implements Provider for AWS Bedrock
type BedrockProvider struct {
	config Config
}

// NewBedrockProvider creates a new Bedrock provider
func NewBedrockProvider(cfg Config) (*BedrockProvider, error) {
	return nil, ErrBedrockNotImplemented
}

func (p *BedrockProvider) Name() string {
	return "bedrock"
}

func (p *BedrockProvider) SupportedModels() []string {
	return nil
}

func (p *BedrockProvider) CreateMessage(ctx context.Context, request MessageRequest) (*MessageResponse, error) {
	return nil, ErrBedrockNotImplemented
}
