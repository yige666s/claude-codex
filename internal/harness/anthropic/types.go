package anthropic

import "encoding/json"

type MessageRequest struct {
	Model     string         `json:"model"`
	MaxTokens int            `json:"max_tokens"`
	System    string         `json:"system,omitempty"`
	Messages  []InputMessage `json:"messages"`
	Tools     []Tool         `json:"tools,omitempty"`
	Stream    bool           `json:"stream,omitempty"`
}

type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type InputMessage struct {
	Role    string         `json:"role"`
	Content []ContentBlock `json:"content"`
}

type ContentBlock struct {
	// common
	Type string `json:"type"`
	// text
	Text string `json:"text,omitempty"`
	// tool_use (assistant)
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
	// tool_result (user)
	ToolUseID string `json:"tool_use_id,omitempty"`
	Content   string `json:"content,omitempty"`
}

type MessageResponse struct {
	ID         string         `json:"id"`
	Model      string         `json:"model"`
	Role       string         `json:"role"`
	Content    []ContentBlock `json:"content"`
	StopReason string         `json:"stop_reason"`
}

type StreamEvent struct {
	Event string          `json:"event"`
	Data  json.RawMessage `json:"data"`
}

// CountTokensRequest represents a token counting request
type CountTokensRequest struct {
	Model      string         `json:"model"`
	Messages   []InputMessage `json:"messages"`
	Tools      []Tool         `json:"tools,omitempty"`
	MaxTokens  int            `json:"max_tokens,omitempty"`
	System     string         `json:"system,omitempty"`
	Thinking   *Thinking      `json:"thinking,omitempty"`
}

// Thinking configuration for extended thinking
type Thinking struct {
	Type         string `json:"type"`
	BudgetTokens int    `json:"budget_tokens,omitempty"`
}

// CountTokensResponse represents the token counting response
type CountTokensResponse struct {
	InputTokens int `json:"input_tokens"`
}
