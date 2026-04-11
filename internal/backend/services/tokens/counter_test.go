package tokens

import (
	"testing"

	api "claude-codex/internal/harness/anthropic"
)

func TestRoughTokenCountEstimation(t *testing.T) {
	tests := []struct {
		name           string
		content        string
		bytesPerToken  int
		expectedTokens int
	}{
		{
			name:           "Empty string",
			content:        "",
			bytesPerToken:  4,
			expectedTokens: 0,
		},
		{
			name:           "Short text",
			content:        "Hello, world!",
			bytesPerToken:  4,
			expectedTokens: 3, // 13 bytes / 4 = 3
		},
		{
			name:           "Longer text",
			content:        "This is a longer piece of text that should result in more tokens.",
			bytesPerToken:  4,
			expectedTokens: 16, // 66 bytes / 4 = 16
		},
		{
			name:           "Dense format (3 bytes per token)",
			content:        `{"key": "value"}`,
			bytesPerToken:  3,
			expectedTokens: 5, // 16 bytes / 3 = 5
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := RoughTokenCountEstimation(tt.content, tt.bytesPerToken)
			if result != tt.expectedTokens {
				t.Errorf("expected %d tokens, got %d", tt.expectedTokens, result)
			}
		})
	}
}

func TestBytesPerTokenForFileType(t *testing.T) {
	tests := []struct {
		name          string
		fileExtension string
		expected      int
	}{
		{"JSON file", ".json", 3},
		{"JavaScript file", ".js", 3},
		{"TypeScript file", ".ts", 3},
		{"Python file", ".py", 3},
		{"Go file", ".go", 3},
		{"Markdown file", ".md", 4},
		{"Text file", ".txt", 4},
		{"Unknown extension", ".xyz", 4},
		{"No dot prefix", "json", 3},
		{"Uppercase", ".JSON", 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := BytesPerTokenForFileType(tt.fileExtension)
			if result != tt.expected {
				t.Errorf("expected %d, got %d", tt.expected, result)
			}
		})
	}
}

func TestRoughTokenCountEstimationForFileType(t *testing.T) {
	content := "function hello() { return 'world'; }"

	tests := []struct {
		name          string
		fileExtension string
		expectedMin   int
		expectedMax   int
	}{
		{"JavaScript", ".js", 10, 15},
		{"Markdown", ".md", 8, 12},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := RoughTokenCountEstimationForFileType(content, tt.fileExtension)
			if result < tt.expectedMin || result > tt.expectedMax {
				t.Errorf("expected between %d and %d, got %d", tt.expectedMin, tt.expectedMax, result)
			}
		})
	}
}

func TestHasThinkingBlocks(t *testing.T) {
	tests := []struct {
		name     string
		messages []api.InputMessage
		expected bool
	}{
		{
			name:     "Empty messages",
			messages: []api.InputMessage{},
			expected: false,
		},
		{
			name: "User message only",
			messages: []api.InputMessage{
				{
					Role: "user",
					Content: []api.ContentBlock{
						{Type: "text", Text: "Hello"},
					},
				},
			},
			expected: false,
		},
		{
			name: "Assistant message without thinking",
			messages: []api.InputMessage{
				{
					Role: "assistant",
					Content: []api.ContentBlock{
						{Type: "text", Text: "Hello back"},
					},
				},
			},
			expected: false,
		},
		{
			name: "Assistant message with thinking block",
			messages: []api.InputMessage{
				{
					Role: "assistant",
					Content: []api.ContentBlock{
						{Type: "thinking", Text: "Let me think..."},
						{Type: "text", Text: "Here's my response"},
					},
				},
			},
			expected: true,
		},
		{
			name: "Assistant message with redacted_thinking block",
			messages: []api.InputMessage{
				{
					Role: "assistant",
					Content: []api.ContentBlock{
						{Type: "redacted_thinking"},
						{Type: "text", Text: "Response"},
					},
				},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := HasThinkingBlocks(tt.messages)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestStripToolSearchFieldsFromMessages(t *testing.T) {
	tests := []struct {
		name     string
		messages []api.InputMessage
		validate func([]api.InputMessage) bool
	}{
		{
			name: "Pass through tool_use",
			messages: []api.InputMessage{
				{
					Role: "assistant",
					Content: []api.ContentBlock{
						{
							Type:  "tool_use",
							ID:    "tool_123",
							Name:  "test_tool",
							Input: []byte(`{"key": "value"}`),
						},
					},
				},
			},
			validate: func(normalized []api.InputMessage) bool {
				block := normalized[0].Content[0]
				return block.Type == "tool_use" && block.ID == "tool_123"
			},
		},
		{
			name: "Pass through text blocks",
			messages: []api.InputMessage{
				{
					Role: "user",
					Content: []api.ContentBlock{
						{Type: "text", Text: "Hello"},
					},
				},
			},
			validate: func(normalized []api.InputMessage) bool {
				return len(normalized[0].Content) == 1
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := stripToolSearchFieldsFromMessages(tt.messages)
			if !tt.validate(result) {
				t.Errorf("validation failed for %s", tt.name)
			}
		})
	}
}

func TestRoughTokenCountEstimationForMessages(t *testing.T) {
	messages := []api.InputMessage{
		{
			Role: "user",
			Content: []api.ContentBlock{
				{Type: "text", Text: "Hello, how are you?"},
			},
		},
		{
			Role: "assistant",
			Content: []api.ContentBlock{
				{Type: "text", Text: "I'm doing well, thank you!"},
			},
		},
	}

	result := RoughTokenCountEstimationForMessages(messages)

	// Should be roughly 20-30 tokens (includes JSON serialization overhead)
	if result < 15 || result > 35 {
		t.Errorf("expected between 15 and 35 tokens, got %d", result)
	}
}
