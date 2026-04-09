package tui

import (
	"context"
	"testing"
)

// mockRegistry implements CommandRegistry for testing
type mockRegistry struct {
	commands []Command
}

func (m *mockRegistry) List() []Command {
	return m.commands
}

func (m *mockRegistry) Execute(ctx context.Context, name string, args []string) (string, error) {
	// Mock implementation - just return empty string for tests
	return "", nil
}

func TestGenerateCommandSuggestions(t *testing.T) {
	registry := &mockRegistry{
		commands: []Command{
			{Name: "/help", Aliases: []string{"/h", "/?"}, Description: "Show help"},
			{Name: "/history", Aliases: []string{"/hist"}, Description: "Show history"},
			{Name: "/config", Aliases: []string{"/cfg"}, Description: "Show config"},
			{Name: "/theme", Description: "Change theme"},
		},
	}

	tests := []struct {
		name          string
		input         string
		maxResults    int
		expectedCount int
		firstMatch    string
	}{
		{
			name:          "empty input returns all",
			input:         "/",
			maxResults:    10,
			expectedCount: 4,
			firstMatch:    "/help",
		},
		{
			name:          "exact match",
			input:         "/help",
			maxResults:    5,
			expectedCount: 1,
			firstMatch:    "/help",
		},
		{
			name:          "prefix match",
			input:         "/h",
			maxResults:    5,
			expectedCount: 4, // matches /help, /history (both start with h)
			firstMatch:    "", // don't check order since both have same score
		},
		{
			name:          "fuzzy match",
			input:         "/cfg",
			maxResults:    5,
			expectedCount: 1,
			firstMatch:    "/config",
		},
		{
			name:          "no match",
			input:         "/xyz",
			maxResults:    5,
			expectedCount: 0,
			firstMatch:    "",
		},
		{
			name:          "max results limit",
			input:         "/",
			maxResults:    2,
			expectedCount: 2,
			firstMatch:    "/help",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			suggestions := GenerateCommandSuggestions(tt.input, registry, tt.maxResults)

			if len(suggestions) != tt.expectedCount {
				t.Errorf("expected %d suggestions, got %d", tt.expectedCount, len(suggestions))
			}

			if tt.expectedCount > 0 && tt.firstMatch != "" && suggestions[0].Command.Name != tt.firstMatch {
				t.Errorf("expected first match %q, got %q", tt.firstMatch, suggestions[0].Command.Name)
			}
		})
	}
}

func TestScoreCommand(t *testing.T) {
	cmd := Command{
		Name:        "/help",
		Aliases:     []string{"/h", "/?"},
		Description: "Show help message",
	}

	tests := []struct {
		name          string
		query         string
		expectedScore int
		comparison    string
	}{
		{
			name:          "exact match",
			query:         "help",
			expectedScore: 1000,
			comparison:    "==",
		},
		{
			name:          "prefix match",
			query:         "hel",
			expectedScore: 500,
			comparison:    ">=",
		},
		{
			name:          "alias exact match",
			query:         "h",
			expectedScore: 900,
			comparison:    "==",
		},
		{
			name:          "no match",
			query:         "xyz",
			expectedScore: 0,
			comparison:    "==",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := scoreCommand(cmd, tt.query)

			switch tt.comparison {
			case "==":
				if score != tt.expectedScore {
					t.Errorf("expected score %d, got %d", tt.expectedScore, score)
				}
			case ">=":
				if score < tt.expectedScore {
					t.Errorf("expected score >= %d, got %d", tt.expectedScore, score)
				}
			}
		})
	}
}

func TestFuzzyMatch(t *testing.T) {
	tests := []struct {
		name     string
		target   string
		query    string
		expected bool
	}{
		{
			name:     "exact match",
			target:   "help",
			query:    "help",
			expected: true,
		},
		{
			name:     "fuzzy match",
			target:   "history",
			query:    "hst",
			expected: true,
		},
		{
			name:     "no match",
			target:   "help",
			query:    "xyz",
			expected: false,
		},
		{
			name:     "empty query",
			target:   "help",
			query:    "",
			expected: true,
		},
		{
			name:     "empty target",
			target:   "",
			query:    "h",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := fuzzyMatch(tt.target, tt.query)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}
