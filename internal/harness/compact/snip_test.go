package compact

import (
	"testing"

	"github.com/ding/claude-code/claude-go/internal/public/types"
)

func TestSnipMessages_NoLargeResults(t *testing.T) {
	messages := []types.Message{
		{
			Type: types.MessageTypeUser,
			Content: []types.ContentBlock{
				{
					Type:    "tool_result",
					Content: "small content",
				},
			},
		},
	}

	config := DefaultSnipConfig()
	result := SnipMessages(messages, config)

	if len(result) != 1 {
		t.Errorf("Expected 1 message, got %d", len(result))
	}

	if result[0].Content[0].Content != "small content" {
		t.Error("Content should not be modified")
	}
}

func TestSnipMessages_LargeResult(t *testing.T) {
	largeContent := make([]byte, 60000)
	for i := range largeContent {
		largeContent[i] = 'x'
	}

	messages := []types.Message{
		{
			Type: types.MessageTypeUser,
			Content: []types.ContentBlock{
				{
					Type:      "tool_result",
					ToolUseID: "test-1",
					Content:   string(largeContent),
				},
			},
		},
	}

	config := DefaultSnipConfig()
	result := SnipMessages(messages, config)

	if len(result) != 1 {
		t.Errorf("Expected 1 message, got %d", len(result))
	}

	snippedContent := result[0].Content[0].Content
	if len(snippedContent) >= len(largeContent) {
		t.Error("Content should be snipped")
	}

	if len(snippedContent) > config.MaxSize+1000 {
		t.Errorf("Snipped content too large: %d bytes", len(snippedContent))
	}
}

func TestSnipToolResult(t *testing.T) {
	config := DefaultSnipConfig()

	// Test small content (no snipping)
	smallBlock := types.ContentBlock{
		Type:    "tool_result",
		Content: "small",
	}

	result := snipToolResult(smallBlock, config)
	if result.Content != "small" {
		t.Error("Small content should not be snipped")
	}

	// Test large content (should snip)
	largeContent := make([]byte, 60000)
	for i := range largeContent {
		largeContent[i] = 'a'
	}

	largeBlock := types.ContentBlock{
		Type:    "tool_result",
		Content: string(largeContent),
	}

	result = snipToolResult(largeBlock, config)
	if len(result.Content) >= len(largeContent) {
		t.Error("Large content should be snipped")
	}

	// Should contain truncation message
	if !contains(result.Content, "truncated") {
		t.Error("Should contain truncation message")
	}
}

func TestSnipToolResultContent(t *testing.T) {
	content := make([]byte, 10000)
	for i := range content {
		content[i] = 'x'
	}

	result := SnipToolResultContent(string(content), 5000)

	if len(result) >= len(content) {
		t.Error("Content should be snipped")
	}

	if !contains(result, "truncated") {
		t.Error("Should contain truncation message")
	}
}

func TestEstimateTokensSaved(t *testing.T) {
	originalSize := 10000
	snippedSize := 5000

	tokens := EstimateTokensSaved(originalSize, snippedSize)

	// Should be approximately 1250 tokens (5000 bytes / 4)
	if tokens < 1000 || tokens > 1500 {
		t.Errorf("Expected ~1250 tokens, got %d", tokens)
	}
}

func TestShouldSnipToolResult(t *testing.T) {
	tests := []struct {
		name        string
		toolName    string
		contentSize int
		maxSize     int
		expected    bool
	}{
		{
			name:        "compactable tool, large content",
			toolName:    "Read",
			contentSize: 60000,
			maxSize:     50000,
			expected:    true,
		},
		{
			name:        "compactable tool, small content",
			toolName:    "Read",
			contentSize: 1000,
			maxSize:     50000,
			expected:    false,
		},
		{
			name:        "non-compactable tool, large content",
			toolName:    "UnknownTool",
			contentSize: 60000,
			maxSize:     50000,
			expected:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ShouldSnipToolResult(tt.toolName, tt.contentSize, tt.maxSize)
			if result != tt.expected {
				t.Errorf("Expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestGetToolResultSize(t *testing.T) {
	block := types.ContentBlock{
		Type:    "tool_result",
		Content: "test content",
	}

	size := GetToolResultSize(block)
	if size != 12 {
		t.Errorf("Expected size 12, got %d", size)
	}

	// Non-tool-result block
	textBlock := types.ContentBlock{
		Type: "text",
		Text: "test",
	}

	size = GetToolResultSize(textBlock)
	if size != 0 {
		t.Errorf("Expected size 0 for non-tool-result, got %d", size)
	}
}

func TestCountLargeToolResults(t *testing.T) {
	largeContent := make([]byte, 60000)
	for i := range largeContent {
		largeContent[i] = 'x'
	}

	messages := []types.Message{
		{
			Type: types.MessageTypeUser,
			Content: []types.ContentBlock{
				{Type: "tool_result", Content: string(largeContent)},
				{Type: "tool_result", Content: "small"},
			},
		},
		{
			Type: types.MessageTypeUser,
			Content: []types.ContentBlock{
				{Type: "tool_result", Content: string(largeContent)},
			},
		},
	}

	count := CountLargeToolResults(messages, 50000)
	if count != 2 {
		t.Errorf("Expected 2 large results, got %d", count)
	}
}

func TestSnipMessagesWithStats(t *testing.T) {
	largeContent := make([]byte, 60000)
	for i := range largeContent {
		largeContent[i] = 'x'
	}

	messages := []types.Message{
		{
			Type: types.MessageTypeUser,
			Content: []types.ContentBlock{
				{Type: "tool_result", Content: string(largeContent)},
			},
		},
		{
			Type: types.MessageTypeUser,
			Content: []types.ContentBlock{
				{Type: "tool_result", Content: "small"},
			},
		},
	}

	config := DefaultSnipConfig()
	result, stats := SnipMessagesWithStats(messages, config)

	if len(result) != 2 {
		t.Errorf("Expected 2 messages, got %d", len(result))
	}

	if stats.TotalMessages != 2 {
		t.Errorf("Expected 2 total messages, got %d", stats.TotalMessages)
	}

	if stats.MessagesWithSnips != 1 {
		t.Errorf("Expected 1 message with snips, got %d", stats.MessagesWithSnips)
	}

	if stats.ToolResultsSnipped != 1 {
		t.Errorf("Expected 1 tool result snipped, got %d", stats.ToolResultsSnipped)
	}

	if stats.BytesRemoved <= 0 {
		t.Error("Expected bytes removed > 0")
	}

	if stats.EstimatedTokensSaved <= 0 {
		t.Error("Expected tokens saved > 0")
	}
}

func TestFormatSnipStats(t *testing.T) {
	stats := &SnipStats{
		TotalMessages:        10,
		MessagesWithSnips:    2,
		ToolResultsSnipped:   3,
		BytesRemoved:         10000,
		EstimatedTokensSaved: 2500,
	}

	result := FormatSnipStats(stats)

	if !contains(result, "3 tool results") {
		t.Error("Should mention tool results count")
	}

	if !contains(result, "2 messages") {
		t.Error("Should mention messages count")
	}

	if !contains(result, "10000 bytes") {
		t.Error("Should mention bytes removed")
	}

	// Test zero snips
	zeroStats := &SnipStats{}
	result = FormatSnipStats(zeroStats)

	if !contains(result, "No tool results") {
		t.Error("Should indicate no snips")
	}
}

func TestIsCompactable(t *testing.T) {
	tests := []struct {
		toolName string
		expected bool
	}{
		{"Read", true},
		{"Bash", true},
		{"Grep", true},
		{"Write", true},
		{"UnknownTool", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.toolName, func(t *testing.T) {
			result := IsCompactable(tt.toolName)
			if result != tt.expected {
				t.Errorf("IsCompactable(%q) = %v, want %v", tt.toolName, result, tt.expected)
			}
		})
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
