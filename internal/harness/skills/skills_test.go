package skills

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSkillRegistry(t *testing.T) {
	registry := NewSkillRegistry()

	// Test registration
	skill := &SkillDefinition{
		Name:        "test-skill",
		Description: "Test skill",
		Aliases:     []string{"ts", "test"},
	}

	err := registry.Register(skill)
	if err != nil {
		t.Fatalf("failed to register skill: %v", err)
	}

	// Test retrieval by name
	retrieved, ok := registry.Get("test-skill")
	if !ok {
		t.Fatal("skill not found by name")
	}
	if retrieved.Name != "test-skill" {
		t.Errorf("expected name 'test-skill', got '%s'", retrieved.Name)
	}

	// Test retrieval by alias
	retrieved, ok = registry.Get("ts")
	if !ok {
		t.Fatal("skill not found by alias")
	}
	if retrieved.Name != "test-skill" {
		t.Errorf("expected name 'test-skill', got '%s'", retrieved.Name)
	}

	// Test duplicate registration
	err = registry.Register(skill)
	if err == nil {
		t.Error("expected error for duplicate registration")
	}

	// Test count
	if registry.Count() != 1 {
		t.Errorf("expected count 1, got %d", registry.Count())
	}

	// Test removal
	removed := registry.Remove("test-skill")
	if !removed {
		t.Error("failed to remove skill")
	}

	if registry.Count() != 0 {
		t.Errorf("expected count 0 after removal, got %d", registry.Count())
	}
}

func TestSecurityValidation(t *testing.T) {
	tests := []struct {
		name      string
		path      string
		shouldErr bool
	}{
		{"valid relative path", "file.txt", false},
		{"valid nested path", "dir/file.txt", false},
		{"absolute path", "/etc/passwd", true},
		{"parent traversal", "../file.txt", true},
		{"nested traversal", "dir/../../file.txt", true},
		{"double dot", "..", true},
	}

	baseDir := "/tmp/test"

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ValidateSkillPath(baseDir, tt.path)
			if tt.shouldErr && err == nil {
				t.Error("expected error but got none")
			}
			if !tt.shouldErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestIsPathSafe(t *testing.T) {
	tests := []struct {
		path string
		safe bool
	}{
		{"file.txt", true},
		{"dir/file.txt", true},
		{"/absolute/path", false},
		{"../parent", false},
		{"dir/../file", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			result := IsPathSafe(tt.path)
			if result != tt.safe {
				t.Errorf("expected %v, got %v", tt.safe, result)
			}
		})
	}
}

func TestSanitizeSkillName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"valid-name", "valid-name"},
		{"name/with/slashes", "name_with_slashes"},
		{"name:with:colons", "name_with_colons"},
		{"name*with*stars", "name_with_stars"},
		{"", "unnamed"},
		{"  spaces  ", "spaces"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := SanitizeSkillName(tt.input)
			if result != tt.expected {
				t.Errorf("expected '%s', got '%s'", tt.expected, result)
			}
		})
	}
}

func TestParseFrontmatter(t *testing.T) {
	content := `---
name: Test Skill
description: A test skill
when_to_use: When testing
allowed-tools: ["Read", "Write"]
user-invocable: true
---

This is the skill content.
`

	parsed, err := ParseSkillFile(content)
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	if parsed.Frontmatter.Name != "Test Skill" {
		t.Errorf("expected name 'Test Skill', got '%s'", parsed.Frontmatter.Name)
	}

	if parsed.Content != "This is the skill content." {
		t.Errorf("unexpected content: %s", parsed.Content)
	}
}

func TestParseStringArray(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		expected []string
	}{
		{"string with commas", "a, b, c", []string{"a", "b", "c"}},
		{"array", []interface{}{"a", "b", "c"}, []string{"a", "b", "c"}},
		{"nil", nil, nil},
		{"empty string", "", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseStringArray(tt.input)
			if len(result) != len(tt.expected) {
				t.Errorf("expected length %d, got %d", len(tt.expected), len(result))
				return
			}
			for i, v := range result {
				if v != tt.expected[i] {
					t.Errorf("at index %d: expected '%s', got '%s'", i, tt.expected[i], v)
				}
			}
		})
	}
}

func TestSkillLoader(t *testing.T) {
	// Create temporary directory
	tmpDir := t.TempDir()

	// Create a test skill file
	skillContent := `---
name: Test Skill
description: A test skill
---

Test content
`

	skillPath := filepath.Join(tmpDir, "test-skill.md")
	err := os.WriteFile(skillPath, []byte(skillContent), 0644)
	if err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	// Load skill
	loader := NewSkillLoader()
	skill, err := loader.LoadSkillFromFile(skillPath, SourceFile)
	if err != nil {
		t.Fatalf("failed to load skill: %v", err)
	}

	if skill.Name != "test-skill" {
		t.Errorf("expected name 'test-skill', got '%s'", skill.Name)
	}

	if skill.DisplayName != "Test Skill" {
		t.Errorf("expected display name 'Test Skill', got '%s'", skill.DisplayName)
	}

	// Test cache
	cached := loader.cache.Get(skill.FileIdentity)
	if cached == nil {
		t.Error("skill not cached")
	}
}

func TestSkillManager(t *testing.T) {
	manager := NewSkillManager()

	// Register a bundled skill
	skill := NewSimpleSkill("test", "Test skill", "Test content")
	err := RegisterBundledSkill(skill)
	if err != nil {
		t.Fatalf("failed to register bundled skill: %v", err)
	}

	// Load bundled skills
	err = manager.LoadBundledSkills()
	if err != nil {
		t.Fatalf("failed to load bundled skills: %v", err)
	}

	// Retrieve skill
	retrieved, ok := manager.GetSkill("test")
	if !ok {
		t.Fatal("skill not found")
	}

	if retrieved.Name != "test" {
		t.Errorf("expected name 'test', got '%s'", retrieved.Name)
	}

	// Test stats
	stats := manager.GetStats()
	if stats.TotalSkills == 0 {
		t.Error("expected at least one skill")
	}

	// Cleanup
	ClearBundledSkills()
}

func TestConditionalActivation(t *testing.T) {
	manager := NewSkillManager()

	// Create a conditional skill
	skill := &SkillDefinition{
		Name:        "conditional-skill",
		Description: "Conditional skill",
		Paths:       []string{"src/**"},
		Source:      SourceFile,
	}

	manager.mu.Lock()
	manager.conditionalSkills[skill.Name] = skill
	manager.mu.Unlock()

	// Activate for matching path
	activated := manager.ActivateConditionalSkillsForPaths([]string{"src/main.go"})
	if activated != 1 {
		t.Errorf("expected 1 activation, got %d", activated)
	}

	// Check if skill is now active
	_, ok := manager.GetSkill("conditional-skill")
	if !ok {
		t.Error("skill should be active")
	}

	// Check conditional count
	count := manager.GetConditionalSkillCount()
	if count != 0 {
		t.Errorf("expected 0 conditional skills, got %d", count)
	}
}

func TestMCPSkills(t *testing.T) {
	// Register default builder
	RegisterMCPSkillBuilder(DefaultMCPSkillBuilder)

	// Build MCP skill
	metadata := map[string]interface{}{
		"name":        "test-tool",
		"description": "Test MCP tool",
	}

	skill, err := BuildMCPSkill("test-tool", metadata)
	if err != nil {
		t.Fatalf("failed to build MCP skill: %v", err)
	}

	if skill.Name != "test-tool" {
		t.Errorf("expected name 'test-tool', got '%s'", skill.Name)
	}

	if skill.Source != SourceMCP {
		t.Errorf("expected source MCP, got %s", skill.Source)
	}
}
