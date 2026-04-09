package provider

import (
	"context"

	api "github.com/ding/claude-code/claude-go/internal/harness/anthropic"
)

// BedrockProvider implements Provider for AWS Bedrock
type BedrockProvider struct {
	config Config
}

// NewBedrockProvider creates a new Bedrock provider
func NewBedrockProvider(cfg Config) (*BedrockProvider, error) {
	// TODO: Implement AWS Bedrock integration
	// This would use the AWS SDK to call Bedrock's Claude models
	// Example: github.com/aws/aws-sdk-go-v2/service/bedrockruntime
	return &BedrockProvider{config: cfg}, nil
}

func (p *BedrockProvider) Name() string {
	return "bedrock"
}

func (p *BedrockProvider) SupportedModels() []string {
	return []string{
		"anthropic.claude-3-5-sonnet-20241022-v2:0",
		"anthropic.claude-3-5-sonnet-20240620-v1:0",
		"anthropic.claude-3-opus-20240229-v1:0",
		"anthropic.claude-3-sonnet-20240229-v1:0",
		"anthropic.claude-3-haiku-20240307-v1:0",
	}
}

func (p *BedrockProvider) CreateMessage(ctx context.Context, request MessageRequest) (*MessageResponse, error) {
	// TODO: Implement Bedrock API call
	// 1. Initialize AWS SDK client with credentials from config
	// 2. Convert MessageRequest to Bedrock format (similar to Anthropic)
	// 3. Call bedrock-runtime:InvokeModel
	// 4. Parse response and convert to MessageResponse
	return nil, &api.HTTPError{
		StatusCode: 501,
		Status:     "Not Implemented",
		Body:       "Bedrock provider not yet implemented",
	}
}
