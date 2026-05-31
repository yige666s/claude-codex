package provider

import (
	"context"
	"encoding/json"
	"strings"
)

type thinkingConfigContextKey struct{}
type googleSearchGroundingContextKey struct{}

const (
	GoogleSearchGroundingOff    = "off"
	GoogleSearchGroundingAuto   = "auto"
	GoogleSearchGroundingAlways = "always"
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

// StreamingProvider is implemented by providers that can emit text chunks
// before the final message is available.
type StreamingProvider interface {
	Provider
	StreamMessage(ctx context.Context, request MessageRequest, onChunk func(string)) (*MessageResponse, error)
}

// MessageRequest represents a unified message request across providers
type MessageRequest struct {
	Model          string          `json:"model"`
	Messages       []Message       `json:"messages"`
	MaxTokens      int             `json:"max_tokens,omitempty"`
	Temperature    float64         `json:"temperature,omitempty"`
	TopP           float64         `json:"top_p,omitempty"`
	Stream         bool            `json:"stream,omitempty"`
	Tools          []Tool          `json:"tools,omitempty"`
	System         string          `json:"system,omitempty"`
	ThinkingConfig *ThinkingConfig `json:"thinking_config,omitempty"`
	// GoogleSearchGrounding controls provider-native Google Search grounding for
	// providers/models that support it. "auto" and "always" attach the
	// googleSearch tool and let Gemini decide whether it needs a search.
	GoogleSearchGrounding string `json:"google_search_grounding,omitempty"`
}

// ThinkingConfig requests provider-native thinking/reasoning controls when a model supports them.
type ThinkingConfig struct {
	Enabled      bool   `json:"enabled"`
	BudgetTokens int    `json:"budget_tokens,omitempty"`
	Level        string `json:"level,omitempty"`
}

func WithThinkingConfig(ctx context.Context, config *ThinkingConfig) context.Context {
	if config == nil || !config.Enabled {
		return ctx
	}
	return context.WithValue(ctx, thinkingConfigContextKey{}, config)
}

func ThinkingConfigFromContext(ctx context.Context) *ThinkingConfig {
	config, _ := ctx.Value(thinkingConfigContextKey{}).(*ThinkingConfig)
	return config
}

func WithGoogleSearchGrounding(ctx context.Context, mode string) context.Context {
	mode = NormalizeGoogleSearchGroundingMode(mode)
	if mode == "" {
		return ctx
	}
	return context.WithValue(ctx, googleSearchGroundingContextKey{}, mode)
}

func GoogleSearchGroundingFromContext(ctx context.Context) string {
	mode, _ := ctx.Value(googleSearchGroundingContextKey{}).(string)
	return NormalizeGoogleSearchGroundingMode(mode)
}

func NormalizeGoogleSearchGroundingMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "":
		return ""
	case GoogleSearchGroundingOff:
		return GoogleSearchGroundingOff
	case GoogleSearchGroundingAlways, "on", "true", "enabled":
		return GoogleSearchGroundingAlways
	case GoogleSearchGroundingAuto:
		return GoogleSearchGroundingAuto
	default:
		return ""
	}
}

// Message represents a single message in the conversation
type Message struct {
	Role       string      `json:"role"`
	Content    interface{} `json:"content"` // Can be string or []ContentBlock
	ToolCallID string      `json:"tool_call_id,omitempty"`
	ToolName   string      `json:"tool_name,omitempty"`
	ToolCalls  []ToolCall  `json:"tool_calls,omitempty"`
}

// ContentBlock represents a content block (text, image, etc.)
type ContentBlock struct {
	Type   string                 `json:"type"`
	Text   string                 `json:"text,omitempty"`
	Source map[string]interface{} `json:"source,omitempty"`
}

type ToolCall struct {
	ID               string          `json:"id"`
	Name             string          `json:"name"`
	Input            json.RawMessage `json:"input,omitempty"`
	ThoughtSignature string          `json:"thought_signature,omitempty"`
}

// Tool represents a function/tool definition
type Tool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	InputSchema map[string]interface{} `json:"input_schema,omitempty"`
}

// MessageResponse represents a unified response across providers
type MessageResponse struct {
	ID                string          `json:"id"`
	Model             string          `json:"model"`
	Role              string          `json:"role"`
	Content           []ContentBlock  `json:"content"`
	ToolCalls         []ToolCall      `json:"tool_calls,omitempty"`
	StopReason        string          `json:"stop_reason,omitempty"`
	Usage             Usage           `json:"usage"`
	GroundingMetadata json.RawMessage `json:"grounding_metadata,omitempty"`
}

// Usage represents token usage information
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// Config represents provider configuration
type Config struct {
	Provider                string `json:"provider"` // anthropic, openai, qwen, gemini, vertex, shortapi, custom
	APIKey                  string `json:"api_key,omitempty"`
	BaseURL                 string `json:"base_url,omitempty"`
	Token                   string `json:"token,omitempty"` // Alternative to APIKey for some providers
	Model                   string `json:"model"`
	Timeout                 int    `json:"timeout_seconds"`
	VertexLocation          string `json:"vertex_location,omitempty"`
	VertexAnthropicLocation string `json:"vertex_anthropic_location,omitempty"`
	GoogleSearchGrounding   string `json:"google_search_grounding,omitempty"`
}
