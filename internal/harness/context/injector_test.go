package context

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSystemPromptBuilder(t *testing.T) {
	t.Run("AddPart", func(t *testing.T) {
		builder := NewSystemPromptBuilder()
		builder.AddPart("Part 1")
		builder.AddPart("Part 2")

		result := builder.Build()
		if !strings.Contains(result, "Part 1") {
			t.Error("expected result to contain 'Part 1'")
		}
		if !strings.Contains(result, "Part 2") {
			t.Error("expected result to contain 'Part 2'")
		}
	})

	t.Run("AddPart with empty string", func(t *testing.T) {
		builder := NewSystemPromptBuilder()
		builder.AddPart("Part 1")
		builder.AddPart("")
		builder.AddPart("Part 2")

		parts := builder.BuildArray()
		if len(parts) != 2 {
			t.Errorf("expected 2 parts, got %d", len(parts))
		}
	})

	t.Run("AddContext", func(t *testing.T) {
		builder := NewSystemPromptBuilder()
		ctx := map[string]string{
			"key1": "value1",
			"key2": "value2",
		}
		builder.AddContext(ctx)

		result := builder.Build()
		if !strings.Contains(result, "value1") {
			t.Error("expected result to contain 'value1'")
		}
		if !strings.Contains(result, "value2") {
			t.Error("expected result to contain 'value2'")
		}
	})

	t.Run("Build", func(t *testing.T) {
		builder := NewSystemPromptBuilder()
		builder.AddPart("Part 1")
		builder.AddPart("Part 2")

		result := builder.Build()
		expected := "Part 1\n\nPart 2"
		if result != expected {
			t.Errorf("expected %q, got %q", expected, result)
		}
	})

	t.Run("BuildArray", func(t *testing.T) {
		builder := NewSystemPromptBuilder()
		builder.AddPart("Part 1")
		builder.AddPart("Part 2")

		parts := builder.BuildArray()
		if len(parts) != 2 {
			t.Errorf("expected 2 parts, got %d", len(parts))
		}
		if parts[0] != "Part 1" {
			t.Errorf("expected 'Part 1', got %q", parts[0])
		}
		if parts[1] != "Part 2" {
			t.Errorf("expected 'Part 2', got %q", parts[1])
		}
	})
}

func TestInjectSystemContext(t *testing.T) {
	tmpDir := t.TempDir()

	// Clear caches
	ClearSystemContextCache()

	basePrompt := "Base prompt"
	result, err := InjectSystemContext(basePrompt, tmpDir, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(result, basePrompt) {
		t.Error("expected result to contain base prompt")
	}

	// Clean up
	ClearSystemContextCache()
}

func TestInjectUserContext(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a CLAUDE.md file
	claudeMdPath := filepath.Join(tmpDir, "CLAUDE.md")
	content := "# Test CLAUDE.md"
	if err := os.WriteFile(claudeMdPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write CLAUDE.md: %v", err)
	}

	// Clear caches
	ClearUserContextCache()

	basePrompt := "Base prompt"
	result, err := InjectUserContext(basePrompt, tmpDir, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(result, basePrompt) {
		t.Error("expected result to contain base prompt")
	}

	if !strings.Contains(result, "Test CLAUDE.md") {
		t.Error("expected result to contain CLAUDE.md content")
	}

	// Clean up
	ClearUserContextCache()
}

func TestInjectAllContext(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a CLAUDE.md file
	claudeMdPath := filepath.Join(tmpDir, "CLAUDE.md")
	content := "# Test CLAUDE.md"
	if err := os.WriteFile(claudeMdPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write CLAUDE.md: %v", err)
	}

	// Clear caches
	ClearSystemContextCache()
	ClearUserContextCache()

	basePrompt := "Base prompt"
	result, err := InjectAllContext(basePrompt, tmpDir, false, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(result, basePrompt) {
		t.Error("expected result to contain base prompt")
	}

	if !strings.Contains(result, "Test CLAUDE.md") {
		t.Error("expected result to contain CLAUDE.md content")
	}

	// Clean up
	ClearSystemContextCache()
	ClearUserContextCache()
}

func TestBuildSystemPromptParts(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a CLAUDE.md file
	claudeMdPath := filepath.Join(tmpDir, "CLAUDE.md")
	content := "# Test CLAUDE.md"
	if err := os.WriteFile(claudeMdPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write CLAUDE.md: %v", err)
	}

	// Clear caches
	ClearSystemContextCache()
	ClearUserContextCache()

	t.Run("Without custom prompt", func(t *testing.T) {
		parts, err := BuildSystemPromptParts(tmpDir, false, false, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if parts == nil {
			t.Fatal("expected parts to be non-nil")
		}

		if parts.UserContext == nil {
			t.Error("expected UserContext to be non-nil")
		}

		if parts.SystemContext == nil {
			t.Error("expected SystemContext to be non-nil")
		}
	})

	t.Run("With custom prompt", func(t *testing.T) {
		ClearSystemContextCache()
		ClearUserContextCache()

		parts, err := BuildSystemPromptParts(tmpDir, false, false, "Custom prompt")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(parts.DefaultSystemPrompt) != 0 {
			t.Error("expected DefaultSystemPrompt to be empty with custom prompt")
		}

		if len(parts.SystemContext) != 0 {
			t.Error("expected SystemContext to be empty with custom prompt")
		}

		if parts.UserContext == nil {
			t.Error("expected UserContext to be non-nil")
		}
	})

	// Clean up
	ClearSystemContextCache()
	ClearUserContextCache()
}

func TestAssembleSystemPrompt(t *testing.T) {
	t.Run("With default prompt", func(t *testing.T) {
		parts := &SystemPromptParts{
			DefaultSystemPrompt: []string{"Default part 1", "Default part 2"},
			UserContext: map[string]string{
				"currentDate": "2024-01-01",
			},
			SystemContext: map[string]string{
				"gitStatus": "clean",
			},
		}

		result := AssembleSystemPrompt(parts, "", "")

		if len(result) < 2 {
			t.Errorf("expected at least 2 parts, got %d", len(result))
		}

		combined := strings.Join(result, "\n")
		if !strings.Contains(combined, "Default part 1") {
			t.Error("expected result to contain 'Default part 1'")
		}
	})

	t.Run("With custom prompt", func(t *testing.T) {
		parts := &SystemPromptParts{
			DefaultSystemPrompt: []string{"Default part 1"},
			UserContext: map[string]string{
				"currentDate": "2024-01-01",
			},
			SystemContext: map[string]string{},
		}

		result := AssembleSystemPrompt(parts, "Custom prompt", "")

		combined := strings.Join(result, "\n")
		if !strings.Contains(combined, "Custom prompt") {
			t.Error("expected result to contain 'Custom prompt'")
		}
		if strings.Contains(combined, "Default part 1") {
			t.Error("expected result to not contain default prompt when custom is provided")
		}
	})

	t.Run("With append prompt", func(t *testing.T) {
		parts := &SystemPromptParts{
			DefaultSystemPrompt: []string{"Default part"},
			UserContext:         map[string]string{},
			SystemContext:       map[string]string{},
		}

		result := AssembleSystemPrompt(parts, "", "Append prompt")

		combined := strings.Join(result, "\n")
		if !strings.Contains(combined, "Append prompt") {
			t.Error("expected result to contain 'Append prompt'")
		}
	})
}
