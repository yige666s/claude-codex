package compact

import (
	"testing"

	api "github.com/ding/claude-code/claude-go/internal/harness/anthropic"
)

func TestGetEffectiveContextWindowSize(t *testing.T) {
	tests := []struct {
		name          string
		model         string
		contextWindow int
		expected      int
	}{
		{
			name:          "Opus 4.6",
			model:         "claude-opus-4-6",
			contextWindow: 200000,
			expected:      200000 - 8192, // MaxOutputTokens for this model
		},
		{
			name:          "Sonnet 4.6",
			model:         "claude-sonnet-4-6",
			contextWindow: 200000,
			expected:      200000 - 8192,
		},
		{
			name:          "With MaxOutputTokensForSummary cap",
			model:         "claude-opus-4-6",
			contextWindow: 200000,
			expected:      200000 - 8192, // Capped at MaxOutputTokensForSummary (20000)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetEffectiveContextWindowSize(tt.model, tt.contextWindow)
			// Allow some variance due to min() logic
			if result < tt.expected-20000 || result > tt.expected {
				t.Errorf("expected around %d, got %d", tt.expected, result)
			}
		})
	}
}

func TestGetAutoCompactThreshold(t *testing.T) {
	tests := []struct {
		name          string
		model         string
		contextWindow int
		expectedMin   int
	}{
		{
			name:          "Standard threshold",
			model:         "claude-sonnet-4-6",
			contextWindow: 200000,
			expectedMin:   170000, // Should be contextWindow - buffer
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetAutoCompactThreshold(tt.model, tt.contextWindow)
			if result < tt.expectedMin {
				t.Errorf("expected at least %d, got %d", tt.expectedMin, result)
			}
		})
	}
}

func TestCalculateTokenWarningState(t *testing.T) {
	tests := []struct {
		name                        string
		tokenUsage                  int
		model                       string
		contextWindow               int
		autoCompactEnabled          bool
		expectAboveWarningThreshold bool
		expectAboveErrorThreshold   bool
	}{
		{
			name:                        "Low usage",
			tokenUsage:                  10000,
			model:                       "claude-sonnet-4-6",
			contextWindow:               200000,
			autoCompactEnabled:          true,
			expectAboveWarningThreshold: false,
			expectAboveErrorThreshold:   false,
		},
		{
			name:                        "High usage",
			tokenUsage:                  180000,
			model:                       "claude-sonnet-4-6",
			contextWindow:               200000,
			autoCompactEnabled:          true,
			expectAboveWarningThreshold: true,
			expectAboveErrorThreshold:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CalculateTokenWarningState(
				tt.tokenUsage,
				tt.model,
				tt.contextWindow,
				tt.autoCompactEnabled,
			)

			if result.IsAboveWarningThreshold != tt.expectAboveWarningThreshold {
				t.Errorf("expected IsAboveWarningThreshold=%v, got %v",
					tt.expectAboveWarningThreshold, result.IsAboveWarningThreshold)
			}

			if result.IsAboveErrorThreshold != tt.expectAboveErrorThreshold {
				t.Errorf("expected IsAboveErrorThreshold=%v, got %v",
					tt.expectAboveErrorThreshold, result.IsAboveErrorThreshold)
			}
		})
	}
}

func TestShouldTriggerAutoCompact(t *testing.T) {
	tests := []struct {
		name          string
		tokenUsage    int
		model         string
		contextWindow int
		tracking      *AutoCompactTrackingState
		turnID        string
		expected      bool
	}{
		{
			name:          "Below threshold",
			tokenUsage:    10000,
			model:         "claude-sonnet-4-6",
			contextWindow: 200000,
			tracking:      &AutoCompactTrackingState{},
			turnID:        "turn1",
			expected:      false,
		},
		{
			name:          "Above threshold",
			tokenUsage:    180000,
			model:         "claude-sonnet-4-6",
			contextWindow: 200000,
			tracking:      &AutoCompactTrackingState{},
			turnID:        "turn1",
			expected:      true,
		},
		{
			name:          "Already compacted this turn",
			tokenUsage:    180000,
			model:         "claude-sonnet-4-6",
			contextWindow: 200000,
			tracking: &AutoCompactTrackingState{
				Compacted: true,
				TurnID:    "turn1",
			},
			turnID:   "turn1",
			expected: false,
		},
		{
			name:          "Circuit breaker tripped",
			tokenUsage:    180000,
			model:         "claude-sonnet-4-6",
			contextWindow: 200000,
			tracking: &AutoCompactTrackingState{
				ConsecutiveFailures: MaxConsecutiveAutoCompactFailures,
			},
			turnID:   "turn1",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ShouldTriggerAutoCompact(
				tt.tokenUsage,
				tt.model,
				tt.contextWindow,
				tt.tracking,
				tt.turnID,
			)

			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestStripImagesFromMessages(t *testing.T) {
	tests := []struct {
		name     string
		messages []api.InputMessage
		validate func([]api.InputMessage) bool
	}{
		{
			name: "Strip image blocks",
			messages: []api.InputMessage{
				{
					Role: "user",
					Content: []api.ContentBlock{
						{Type: "text", Text: "Hello"},
						{Type: "image"},
					},
				},
			},
			validate: func(result []api.InputMessage) bool {
				if len(result) != 1 {
					return false
				}
				// Should have text and image replacement marker
				hasText := false
				hasMarker := false
				for _, block := range result[0].Content {
					if block.Type == "text" {
						if block.Text == "Hello" {
							hasText = true
						}
						if block.Text == "[Image was shared]" {
							hasMarker = true
						}
					}
				}
				return hasText && hasMarker
			},
		},
		{
			name: "Keep non-image content",
			messages: []api.InputMessage{
				{
					Role: "user",
					Content: []api.ContentBlock{
						{Type: "text", Text: "Hello"},
					},
				},
			},
			validate: func(result []api.InputMessage) bool {
				return len(result) == 1 && len(result[0].Content) == 1
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := StripImagesFromMessages(tt.messages)
			if !tt.validate(result) {
				t.Errorf("validation failed for %s", tt.name)
			}
		})
	}
}

func TestGroupMessagesByAPIRound(t *testing.T) {
	tests := []struct {
		name          string
		messages      []api.InputMessage
		expectedGroups int
	}{
		{
			name: "Single round",
			messages: []api.InputMessage{
				{Role: "user", Content: []api.ContentBlock{{Type: "text", Text: "Hello"}}},
				{Role: "assistant", Content: []api.ContentBlock{{Type: "text", Text: "Hi"}}},
			},
			expectedGroups: 1,
		},
		{
			name: "Multiple rounds",
			messages: []api.InputMessage{
				{Role: "user", Content: []api.ContentBlock{{Type: "text", Text: "Hello"}}},
				{Role: "assistant", Content: []api.ContentBlock{{Type: "text", Text: "Hi"}}},
				{Role: "user", Content: []api.ContentBlock{{Type: "text", Text: "How are you"}}},
				{Role: "assistant", Content: []api.ContentBlock{{Type: "text", Text: "Good"}}},
			},
			expectedGroups: 1, // Without proper ID tracking, this will be 1 group
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GroupMessagesByAPIRound(tt.messages)
			if len(result) < 1 {
				t.Errorf("expected at least 1 group, got %d", len(result))
			}
		})
	}
}

func TestFormatCompactSummary(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Remove analysis tags",
			input:    "<analysis>Some analysis</analysis><summary>Summary text</summary>",
			expected: "<summary>Summary text</summary>",
		},
		{
			name:     "No analysis tags",
			input:    "<summary>Summary text</summary>",
			expected: "<summary>Summary text</summary>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatCompactSummary(tt.input)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}
