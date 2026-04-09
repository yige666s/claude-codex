package provider

import (
	"context"

	api "github.com/ding/claude-code/claude-go/internal/harness/anthropic"
)

// VertexProvider implements Provider for GCP Vertex AI
type VertexProvider struct {
	config Config
}

// NewVertexProvider creates a new Vertex AI provider
func NewVertexProvider(cfg Config) (*VertexProvider, error) {
	// TODO: Implement GCP Vertex AI integration
	// This would use the GCP SDK to call Vertex AI's Claude models
	// Example: cloud.google.com/go/aiplatform/apiv1
	return &VertexProvider{config: cfg}, nil
}

func (p *VertexProvider) Name() string {
	return "vertex"
}

func (p *VertexProvider) SupportedModels() []string {
	return []string{
		"claude-3-5-sonnet@20241022",
		"claude-3-5-sonnet@20240620",
		"claude-3-opus@20240229",
		"claude-3-sonnet@20240229",
		"claude-3-haiku@20240307",
	}
}

func (p *VertexProvider) CreateMessage(ctx context.Context, request MessageRequest) (*MessageResponse, error) {
	// TODO: Implement Vertex AI API call
	// 1. Initialize GCP client with credentials from config
	// 2. Convert MessageRequest to Vertex AI format
	// 3. Call Vertex AI Prediction API
	// 4. Parse response and convert to MessageResponse
	return nil, &api.HTTPError{
		StatusCode: 501,
		Status:     "Not Implemented",
		Body:       "Vertex AI provider not yet implemented",
	}
}
