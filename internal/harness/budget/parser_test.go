package budget

import "testing"

func TestParseTokenBudget(t *testing.T) {
	tests := []struct {
		input    string
		expected int
	}{
		// Shorthand at start
		{"+500k", 500_000},
		{"+2m", 2_000_000},
		{"+1.5m", 1_500_000},
		{"+3b", 3_000_000_000},
		{"  +100k", 100_000},

		// Shorthand at end
		{"do this +500k", 500_000},
		{"complete task +2m.", 2_000_000},
		{"finish work +1.5m!", 1_500_000},

		// Verbose pattern
		{"use 500k tokens", 500_000},
		{"spend 2m tokens", 2_000_000},
		{"use 1.5M token", 1_500_000},
		{"spend 100K tokens", 100_000},

		// No budget
		{"no budget here", 0},
		{"just regular text", 0},
		{"", 0},

		// Case insensitive
		{"+500K", 500_000},
		{"+2M", 2_000_000},
		{"use 500K tokens", 500_000},
	}

	for _, tt := range tests {
		result := ParseTokenBudget(tt.input)
		if result != tt.expected {
			t.Errorf("ParseTokenBudget(%q) = %d, expected %d", tt.input, result, tt.expected)
		}
	}
}

func TestFindTokenBudgetPositions(t *testing.T) {
	tests := []struct {
		input    string
		expected []BudgetPosition
	}{
		{
			"+500k",
			[]BudgetPosition{{Start: 0, End: 5}},
		},
		{
			"do this +500k",
			[]BudgetPosition{{Start: 8, End: 13}},
		},
		{
			"use 500k tokens",
			[]BudgetPosition{{Start: 0, End: 15}},
		},
		{
			"+500k and use 2m tokens",
			[]BudgetPosition{
				{Start: 0, End: 5},
				{Start: 10, End: 23},
			},
		},
		{
			"no budget",
			[]BudgetPosition{},
		},
	}

	for _, tt := range tests {
		result := FindTokenBudgetPositions(tt.input)
		if len(result) != len(tt.expected) {
			t.Errorf("FindTokenBudgetPositions(%q) returned %d positions, expected %d",
				tt.input, len(result), len(tt.expected))
			continue
		}

		for i, pos := range result {
			if pos.Start != tt.expected[i].Start || pos.End != tt.expected[i].End {
				t.Errorf("FindTokenBudgetPositions(%q)[%d] = {%d, %d}, expected {%d, %d}",
					tt.input, i, pos.Start, pos.End, tt.expected[i].Start, tt.expected[i].End)
			}
		}
	}
}

func TestParseBudgetMatch(t *testing.T) {
	tests := []struct {
		value    string
		suffix   string
		expected int
	}{
		{"500", "k", 500_000},
		{"2", "m", 2_000_000},
		{"1.5", "m", 1_500_000},
		{"3", "b", 3_000_000_000},
		{"0.5", "k", 500},
		{"100", "K", 100_000},
		{"2", "M", 2_000_000},
	}

	for _, tt := range tests {
		result := parseBudgetMatch(tt.value, tt.suffix)
		if result != tt.expected {
			t.Errorf("parseBudgetMatch(%q, %q) = %d, expected %d",
				tt.value, tt.suffix, result, tt.expected)
		}
	}
}

func TestParseTokenBudget_EdgeCases(t *testing.T) {
	tests := []struct {
		input    string
		expected int
	}{
		// Should not match partial patterns
		{"k500", 0},
		{"500", 0},
		{"tokens", 0},

		// Should match with various whitespace
		{"+500k  ", 500_000},
		{"  +500k", 500_000},
		{"use  500k  tokens", 500_000},

		// Should handle punctuation
		{"task +500k.", 500_000},
		{"task +500k!", 500_000},
		{"task +500k?", 500_000},
	}

	for _, tt := range tests {
		result := ParseTokenBudget(tt.input)
		if result != tt.expected {
			t.Errorf("ParseTokenBudget(%q) = %d, expected %d", tt.input, result, tt.expected)
		}
	}
}
