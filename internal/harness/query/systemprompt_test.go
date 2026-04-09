package query

import (
	"context"
	"testing"

	"github.com/ding/claude-code/claude-go/internal/harness/prompt"
	"github.com/ding/claude-code/claude-go/internal/public/types"
)

func TestNewSystemPromptBuilder(t *testing.T) {
	builder := NewSystemPromptBuilder()
	if builder == nil {
		t.Fatal("NewSystemPromptBuilder returned nil")
	}
	if builder.promptBuilder == nil {
		t.Fatal("promptBuilder is nil")
	}
}

func TestBuildSystemPrompt(t *testing.T) {
	builder := NewSystemPromptBuilder()
	ctx := context.Background()

	userContext := map[string]string{
		"claudeMd":    "# Test Project",
		"currentDate": "2026-04-08",
	}

	systemContext := map[string]string{
		"platform":  "darwin",
		"shell":     "zsh",
		"gitStatus": "clean",
	}

	// Test with custom prompt
	customPrompt := "You are a helpful assistant."
	sp, err := builder.BuildSystemPrompt(ctx, userContext, systemContext, customPrompt, "", "claude-sonnet-4-6")
	if err != nil {
		t.Fatalf("BuildSystemPrompt failed: %v", err)
	}

	if sp.Content == "" && len(sp.Parts) == 0 {
		t.Error("SystemPrompt is empty")
	}

	// Verify custom prompt is included
	if sp.Content != customPrompt {
		t.Errorf("Expected content '%s', got '%s'", customPrompt, sp.Content)
	}
}

func TestBuildSystemPromptWithSections(t *testing.T) {
	builder := NewSystemPromptBuilder()
	ctx := context.Background()

	sections := []*prompt.SystemPromptSection{
		prompt.NewSection("test1", func() (string, error) {
			return "Section 1 content", nil
		}),
		prompt.NewSection("test2", func() (string, error) {
			return "Section 2 content", nil
		}),
	}

	sp, err := builder.BuildSystemPromptWithSections(ctx, sections)
	if err != nil {
		t.Fatalf("BuildSystemPromptWithSections failed: %v", err)
	}

	if len(sp.Parts) != 2 {
		t.Errorf("Expected 2 parts, got %d", len(sp.Parts))
	}

	if sp.Content == "" {
		t.Error("SystemPrompt content is empty")
	}
}

func TestCollectContext(t *testing.T) {
	builder := NewSystemPromptBuilder()
	ctx := context.Background()

	userCtx, sysCtx, err := builder.CollectContext(ctx, ".", true)
	if err != nil {
		t.Fatalf("CollectContext failed: %v", err)
	}

	if userCtx == nil {
		t.Error("userContext is nil")
	}

	if sysCtx == nil {
		t.Error("systemContext is nil")
	}

	// Check that we got some context
	if len(sysCtx) == 0 {
		t.Error("systemContext is empty")
	}
}

func TestConvertToTypesSystemPrompt(t *testing.T) {
	// Test with nil
	sp := convertToTypesSystemPrompt(nil)
	if sp.Content != "" || len(sp.Parts) != 0 {
		t.Error("Expected empty SystemPrompt for nil input")
	}

	// Test with empty prompt
	emptyPrompt := prompt.NewSystemPrompt([]string{})
	sp = convertToTypesSystemPrompt(emptyPrompt)
	if sp.Content != "" || len(sp.Parts) != 0 {
		t.Error("Expected empty SystemPrompt for empty input")
	}

	// Test with sections
	sections := []string{"Section 1", "Section 2", "Section 3"}
	fullPrompt := prompt.NewSystemPrompt(sections)
	sp = convertToTypesSystemPrompt(fullPrompt)

	if len(sp.Parts) != 3 {
		t.Errorf("Expected 3 parts, got %d", len(sp.Parts))
	}

	if sp.Content == "" {
		t.Error("Content should not be empty")
	}

	// Verify parts structure
	for i, part := range sp.Parts {
		if part.Type != "text" {
			t.Errorf("Part %d: expected type 'text', got '%s'", i, part.Type)
		}
		if part.Content != sections[i] {
			t.Errorf("Part %d: expected content '%s', got '%s'", i, sections[i], part.Content)
		}
		if !part.Cache {
			t.Errorf("Part %d: expected Cache=true", i)
		}
	}
}

func TestIsEmptySystemPrompt(t *testing.T) {
	tests := []struct {
		name     string
		sp       types.SystemPrompt
		expected bool
	}{
		{
			name:     "empty prompt",
			sp:       types.SystemPrompt{},
			expected: true,
		},
		{
			name: "prompt with content",
			sp: types.SystemPrompt{
				Content: "test content",
			},
			expected: false,
		},
		{
			name: "prompt with parts",
			sp: types.SystemPrompt{
				Parts: []types.SystemPromptPart{
					{Type: "text", Content: "test"},
				},
			},
			expected: false,
		},
		{
			name: "prompt with both",
			sp: types.SystemPrompt{
				Content: "test",
				Parts: []types.SystemPromptPart{
					{Type: "text", Content: "test"},
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isEmptySystemPrompt(tt.sp)
			if result != tt.expected {
				t.Errorf("Expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestClearCache(t *testing.T) {
	builder := NewSystemPromptBuilder()
	ctx := context.Background()

	// Build something to populate cache
	sections := []*prompt.SystemPromptSection{
		prompt.NewSection("cached", func() (string, error) {
			return "cached content", nil
		}),
	}

	_, err := builder.BuildSystemPromptWithSections(ctx, sections)
	if err != nil {
		t.Fatalf("BuildSystemPromptWithSections failed: %v", err)
	}

	// Clear cache
	builder.ClearCache()

	// Get stats
	stats := builder.GetCacheStats()
	if stats.Size != 0 {
		t.Errorf("Expected cache size 0 after clear, got %d", stats.Size)
	}
}

func TestGetCacheStats(t *testing.T) {
	builder := NewSystemPromptBuilder()
	ctx := context.Background()

	// Initially empty
	stats := builder.GetCacheStats()
	if stats.Size != 0 {
		t.Errorf("Expected initial cache size 0, got %d", stats.Size)
	}

	// Build something
	sections := []*prompt.SystemPromptSection{
		prompt.NewSection("test", func() (string, error) {
			return "test content", nil
		}),
	}

	_, err := builder.BuildSystemPromptWithSections(ctx, sections)
	if err != nil {
		t.Fatalf("BuildSystemPromptWithSections failed: %v", err)
	}

	// Check stats
	stats = builder.GetCacheStats()
	if stats.Size == 0 {
		t.Error("Expected cache size > 0 after building")
	}
}
