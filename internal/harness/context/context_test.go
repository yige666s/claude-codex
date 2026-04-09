package context

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGetContextWindowForModel(t *testing.T) {
	tests := []struct {
		name     string
		model    string
		expected int
	}{
		{"Sonnet 4.6", "claude-sonnet-4-6", 1_000_000},
		{"Opus 4.6", "claude-opus-4-6", 1_000_000},
		{"Sonnet 4.6 with [1m]", "claude-sonnet-4-6[1m]", 1_000_000},
		{"Haiku 4.5", "claude-haiku-4-5", 200_000},
		{"Default", "unknown-model", 200_000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetContextWindowForModel(tt.model)
			if result != tt.expected {
				t.Errorf("expected %d, got %d", tt.expected, result)
			}
		})
	}
}

func TestGetModelMaxOutputTokens(t *testing.T) {
	tests := []struct {
		name               string
		model              string
		expectedDefault    int
		expectedUpperLimit int
	}{
		{"Opus 4.6", "claude-opus-4-6", 64_000, 128_000},
		{"Sonnet 4.6", "claude-sonnet-4-6", 32_000, 128_000},
		{"Sonnet 4", "claude-sonnet-4", 32_000, 64_000},
		{"Claude 3 Opus", "claude-3-opus", 4_096, 4_096},
		{"Claude 3 Sonnet", "claude-3-sonnet", 8_192, 8_192},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetModelMaxOutputTokens(tt.model)
			if result.Default != tt.expectedDefault {
				t.Errorf("expected default %d, got %d", tt.expectedDefault, result.Default)
			}
			if result.UpperLimit != tt.expectedUpperLimit {
				t.Errorf("expected upper limit %d, got %d", tt.expectedUpperLimit, result.UpperLimit)
			}
		})
	}
}

func TestCalculateContextPercentages(t *testing.T) {
	tests := []struct {
		name              string
		usage             *TokenUsage
		contextWindowSize int
		expectedUsed      int
		expectedRemaining int
	}{
		{
			name:              "Nil usage",
			usage:             nil,
			contextWindowSize: 200_000,
			expectedUsed:      0,
			expectedRemaining: 100,
		},
		{
			name: "50% usage",
			usage: &TokenUsage{
				InputTokens:              100_000,
				CacheCreationInputTokens: 0,
				CacheReadInputTokens:     0,
			},
			contextWindowSize: 200_000,
			expectedUsed:      50,
			expectedRemaining: 50,
		},
		{
			name: "With cache tokens",
			usage: &TokenUsage{
				InputTokens:              50_000,
				CacheCreationInputTokens: 25_000,
				CacheReadInputTokens:     25_000,
			},
			contextWindowSize: 200_000,
			expectedUsed:      50,
			expectedRemaining: 50,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			used, remaining := CalculateContextPercentages(tt.usage, tt.contextWindowSize)
			if used != tt.expectedUsed {
				t.Errorf("expected used %d%%, got %d%%", tt.expectedUsed, used)
			}
			if remaining != tt.expectedRemaining {
				t.Errorf("expected remaining %d%%, got %d%%", tt.expectedRemaining, remaining)
			}
		})
	}
}

func TestModelSupports1M(t *testing.T) {
	tests := []struct {
		name     string
		model    string
		expected bool
	}{
		{"Sonnet 4.6", "claude-sonnet-4-6", true},
		{"Opus 4.6", "claude-opus-4-6", true},
		{"Haiku 4.5", "claude-haiku-4-5", false},
		{"Claude 3", "claude-3-opus", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ModelSupports1M(tt.model)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestHas1MContext(t *testing.T) {
	tests := []struct {
		name     string
		model    string
		expected bool
	}{
		{"With [1m] suffix", "claude-sonnet-4[1m]", true},
		{"With [1M] suffix", "claude-sonnet-4[1M]", true},
		{"Without suffix", "claude-sonnet-4", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Has1MContext(tt.model)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestSystemPromptInjection(t *testing.T) {
	// Clear any existing injection
	SetSystemPromptInjection("")

	// Test getting empty injection
	if injection := GetSystemPromptInjection(); injection != "" {
		t.Errorf("expected empty injection, got %q", injection)
	}

	// Test setting injection
	SetSystemPromptInjection("test-injection")
	if injection := GetSystemPromptInjection(); injection != "test-injection" {
		t.Errorf("expected 'test-injection', got %q", injection)
	}

	// Clean up
	SetSystemPromptInjection("")
}

func TestGetSystemContext(t *testing.T) {
	// Create a temporary directory
	tmpDir := t.TempDir()

	// Clear caches
	ClearSystemContextCache()

	// Test without git status
	ctx, err := GetSystemContext(tmpDir, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(ctx) > 0 && ctx["gitStatus"] != "" {
		t.Error("expected no git status when includeGitStatus is false")
	}

	// Clean up
	ClearSystemContextCache()
}

func TestGetUserContext(t *testing.T) {
	// Create a temporary directory
	tmpDir := t.TempDir()

	// Clear caches
	ClearUserContextCache()

	// Test with disabled CLAUDE.md
	ctx, err := GetUserContext(tmpDir, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if ctx["claudeMd"] != "" {
		t.Error("expected no CLAUDE.md when disabled")
	}

	if ctx["currentDate"] == "" {
		t.Error("expected currentDate to be set")
	}

	// Clean up
	ClearUserContextCache()
}

func TestLoadClaudeMd(t *testing.T) {
	// Create a temporary directory
	tmpDir := t.TempDir()

	// Create a CLAUDE.md file
	claudeMdPath := filepath.Join(tmpDir, "CLAUDE.md")
	content := "# Test CLAUDE.md\n\nThis is a test."
	if err := os.WriteFile(claudeMdPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write CLAUDE.md: %v", err)
	}

	// Load CLAUDE.md
	result, err := loadClaudeMd(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(result, "Test CLAUDE.md") {
		t.Errorf("expected CLAUDE.md content to contain 'Test CLAUDE.md', got: %s", result)
	}
}
