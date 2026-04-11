package api

import (
	"context"
	"fmt"
	"os"
	"time"

	anthropic "claude-codex/internal/harness/anthropic"
)

// Client wraps the Anthropic API client with additional functionality
type Client struct {
	*anthropic.Client
	Provider APIProvider
	Model    string
}

// NewClient creates a new API client with the specified configuration
func NewClient(ctx context.Context, config ClientConfig) (*Client, error) {
	// Determine provider
	provider := config.Provider
	if provider == "" {
		provider = getAPIProvider()
	}

	// Get API key if not provided
	apiKey := config.APIKey
	if apiKey == "" && provider == ProviderFirstParty {
		apiKey = getAnthropicAPIKey()
	}

	// Determine base URL based on provider
	baseURL := "https://api.anthropic.com"

	switch provider {
	case ProviderAWS:
		// AWS Bedrock - would need AWS SDK integration
		region := getAWSRegion()
		baseURL = fmt.Sprintf("https://bedrock-runtime.%s.amazonaws.com", region)

	case ProviderVertex:
		// Vertex AI - would need GCP SDK integration
		projectID := os.Getenv("ANTHROPIC_VERTEX_PROJECT_ID")
		region := getVertexRegion(config.Model)
		baseURL = fmt.Sprintf("https://%s-aiplatform.googleapis.com/v1/projects/%s/locations/%s/publishers/anthropic/models",
			region, projectID, region)

	case ProviderFoundry:
		// Azure Foundry configuration
		resource := os.Getenv("ANTHROPIC_FOUNDRY_RESOURCE")
		if envURL := os.Getenv("ANTHROPIC_FOUNDRY_BASE_URL"); envURL != "" {
			baseURL = envURL
		} else if resource != "" {
			baseURL = fmt.Sprintf("https://%s.services.ai.azure.com", resource)
		}
	}

	// Create timeout
	timeout := 30 * time.Second
	if config.MaxRetries > 0 {
		// Increase timeout for retries
		timeout = time.Duration(config.MaxRetries) * 30 * time.Second
	}

	// Create Anthropic client
	baseClient := anthropic.NewClient(apiKey, baseURL, timeout)

	return &Client{
		Client:   baseClient,
		Provider: provider,
		Model:    config.Model,
	}, nil
}

// CreateMessageWithRetry creates a message with retry logic
func (c *Client) CreateMessageWithRetry(
	ctx context.Context,
	request MessageRequest,
	opts RetryOptions,
) (*MessageResponse, error) {
	var response *MessageResponse

	// Set model from client if not in request
	if request.Model == "" {
		request.Model = c.Model
	}

	// Set retry options model
	if opts.Model == "" {
		opts.Model = request.Model
	}

	err := WithRetry(ctx, opts, func(ctx context.Context) error {
		resp, err := c.Client.CreateMessage(ctx, request)
		if err != nil {
			return err
		}
		response = resp
		return nil
	})

	if err != nil {
		return nil, err
	}

	return response, nil
}

// CreateMessageWithFallback creates a message with retry and fallback model
func (c *Client) CreateMessageWithFallback(
	ctx context.Context,
	request MessageRequest,
	opts RetryOptions,
) (*MessageResponse, error) {
	var response *MessageResponse

	// Set model from client if not in request
	if request.Model == "" {
		request.Model = c.Model
	}

	// Set retry options model
	if opts.Model == "" {
		opts.Model = request.Model
	}

	err := RetryWithFallback(ctx, opts, func(ctx context.Context, model string) error {
		// Update request model
		req := request
		req.Model = model

		resp, err := c.Client.CreateMessage(ctx, req)
		if err != nil {
			return err
		}
		response = resp
		return nil
	})

	if err != nil {
		// Check if fallback was triggered
		var fallbackErr *FallbackTriggeredError
		if fe, ok := err.(*FallbackTriggeredError); ok {
			fallbackErr = fe
			// Fallback succeeded, return response
			if response != nil {
				// Log fallback event
				fmt.Fprintf(os.Stderr, "Fallback triggered: %s -> %s\n",
					fallbackErr.OriginalModel, fallbackErr.FallbackModel)
				return response, nil
			}
		}
		return nil, err
	}

	return response, nil
}

// getAPIProvider determines the API provider from environment
func getAPIProvider() APIProvider {
	// Check for AWS Bedrock
	if os.Getenv("AWS_REGION") != "" || os.Getenv("AWS_DEFAULT_REGION") != "" {
		return ProviderAWS
	}

	// Check for Vertex AI
	if os.Getenv("ANTHROPIC_VERTEX_PROJECT_ID") != "" {
		return ProviderVertex
	}

	// Check for Foundry
	if os.Getenv("ANTHROPIC_FOUNDRY_RESOURCE") != "" || os.Getenv("ANTHROPIC_FOUNDRY_BASE_URL") != "" {
		return ProviderFoundry
	}

	// Default to first party
	return ProviderFirstParty
}

// getAnthropicAPIKey gets the Anthropic API key from environment
func getAnthropicAPIKey() string {
	// Try standard environment variable
	if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" {
		return key
	}

	// Try Claude Code specific variable
	if key := os.Getenv("CLAUDE_CODE_API_KEY"); key != "" {
		return key
	}

	return ""
}

// getAWSRegion gets the AWS region from environment
func getAWSRegion() string {
	if region := os.Getenv("AWS_REGION"); region != "" {
		return region
	}
	if region := os.Getenv("AWS_DEFAULT_REGION"); region != "" {
		return region
	}
	return "us-east-1" // Default region
}

// getVertexRegion gets the Vertex AI region for a model
func getVertexRegion(model string) string {
	// Check model-specific region variables
	modelRegionVars := map[string]string{
		"claude-3-5-haiku":  "VERTEX_REGION_CLAUDE_3_5_HAIKU",
		"claude-haiku-4-5":  "VERTEX_REGION_CLAUDE_HAIKU_4_5",
		"claude-3-5-sonnet": "VERTEX_REGION_CLAUDE_3_5_SONNET",
		"claude-3-7-sonnet": "VERTEX_REGION_CLAUDE_3_7_SONNET",
	}

	if envVar, ok := modelRegionVars[model]; ok {
		if region := os.Getenv(envVar); region != "" {
			return region
		}
	}

	// Check global region variable
	if region := os.Getenv("CLOUD_ML_REGION"); region != "" {
		return region
	}

	// Default region
	return "us-east5"
}

// IsFirstPartyProvider checks if using first-party Anthropic API
func (c *Client) IsFirstPartyProvider() bool {
	return c.Provider == ProviderFirstParty
}

// GetProvider returns the API provider
func (c *Client) GetProvider() APIProvider {
	return c.Provider
}
