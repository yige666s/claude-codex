package provider

import (
	"context"
)

// Provider defines the interface for LLM providers
type Provider interface {
	// CreateMessage sends a message request and returns the response
	CreateMessage(ctx context.Context, request MessageRequest) (*MessageResponse, error)

	// Name returns the provider name
	Name() string

	// SupportedModels returns a list of supported model names
	SupportedModels() []string
}

// MessageRequest represents a unified message request across providers
type MessageRequest struct {
	Model       string          `json:"model"`
	Messages    []Message       `json:"messages"`
	MaxTokens   int             `json:"max_tokens,omitempty"`
	Temperature float64         `json:"temperature,omitempty"`
	TopP        float64         `json:"top_p,omitempty"`
	Stream      bool            `json:"stream,omitempty"`
	Tools       []Tool          `json:"tools,omitempty"`
	System      string          `json:"system,omitempty"`
}

// Message represents a single message in the conversation
type Message struct {
	Role    string        `json:"role"`
	Content interface{}   `json:"content"` // Can be string or []ContentBlock
}

// ContentBlock represents a content block (text, image, etc.)
type ContentBlock struct {
	Type   string                 `json:"type"`
	Text   string                 `json:"text,omitempty"`
	Source map[string]interface{} `json:"source,omitempty"`
}

// Tool represents a function/tool definition
type Tool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	InputSchema map[string]interface{} `json:"input_schema,omitempty"`
}

// MessageResponse represents a unified response across providers
type MessageResponse struct {
	ID           string        `json:"id"`
	Model        string        `json:"model"`
	Role         string        `json:"role"`
	Content      []ContentBlock `json:"content"`
	StopReason   string        `json:"stop_reason,omitempty"`
	Usage        Usage         `json:"usage"`
}

// Usage represents token usage information
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// Config represents provider configuration
type Config struct {
	Provider string `json:"provider"` // anthropic, openai, gemini
	APIKey   string `json:"api_key,omitempty"`
	BaseURL  string `json:"base_url,omitempty"`
	Token    string `json:"token,omitempty"` // Alternative to APIKey for some providers
	Model    string `json:"model"`
	Timeout  int    `json:"timeout_seconds"`
}
