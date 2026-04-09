package api

import "time"

// Message represents a conversation message
type Message struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"` // Can be string or []ContentBlock
}

// ContentBlock represents a content block in a message
type ContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
	// Add other fields as needed (tool_use, tool_result, etc.)
}

// Tool represents a tool definition
type Tool struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema interface{} `json:"input_schema"`
}

// CreateMessageRequest represents the API request
type CreateMessageRequest struct {
	Model         string    `json:"model"`
	Messages      []Message `json:"messages"`
	MaxTokens     int       `json:"max_tokens"`
	System        string    `json:"system,omitempty"`
	Temperature   float64   `json:"temperature,omitempty"`
	Tools         []Tool    `json:"tools,omitempty"`
	Stream        bool      `json:"stream,omitempty"`
	Metadata      *Metadata `json:"metadata,omitempty"`
	StopSequences []string  `json:"stop_sequences,omitempty"`
}

// Metadata represents request metadata
type Metadata struct {
	UserID string `json:"user_id,omitempty"`
}

// StreamEvent represents a streaming event
type StreamEvent struct {
	Type  string      `json:"type"`
	Index int         `json:"index,omitempty"`
	Delta *Delta      `json:"delta,omitempty"`
	Usage *Usage      `json:"usage,omitempty"`
	Error *ErrorBlock `json:"error,omitempty"`
}

// Delta represents incremental content
type Delta struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// Usage represents token usage statistics
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// ErrorBlock represents an API error
type ErrorBlock struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// Response represents a complete API response
type Response struct {
	ID           string         `json:"id"`
	Type         string         `json:"type"`
	Role         string         `json:"role"`
	Content      []ContentBlock `json:"content"`
	Model        string         `json:"model"`
	StopReason   string         `json:"stop_reason,omitempty"`
	Usage        Usage          `json:"usage"`
	RequestID    string         `json:"-"` // From response headers
	ResponseTime time.Duration  `json:"-"` // Calculated
}

// ClientOptions represents client configuration
type ClientOptions struct {
	APIKey     string
	BaseURL    string
	MaxRetries int
	Timeout    time.Duration
	UserAgent  string
}

// RetryConfig represents retry configuration
type RetryConfig struct {
	MaxRetries     int
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
	BackoffFactor  float64
}
