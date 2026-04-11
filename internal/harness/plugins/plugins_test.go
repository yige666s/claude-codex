package plugins

import (
	"testing"

	"claude-codex/internal/harness/skills"
)

func TestRegisterBuiltinPlugin(t *testing.T) {
	// Clear registry
	ClearBuiltinPlugins()

	// Register a plugin
	plugin := &BuiltinPluginDefinition{
		Name:           "test-plugin",
		Description:    "Test plugin",
		Version:        "1.0.0",
		DefaultEnabled: true,
	}

	RegisterBuiltinPlugin(plugin)

	// Retrieve plugin
	retrieved, ok := GetBuiltinPluginDefinition("test-plugin")
	if !ok {
		t.Fatal("plugin not found")
	}

	if retrieved.Name != "test-plugin" {
		t.Errorf("expected name 'test-plugin', got '%s'", retrieved.Name)
	}

	// Cleanup
	ClearBuiltinPlugins()
}

func TestIsBuiltinPluginID(t *testing.T) {
	tests := []struct {
		pluginID string
		expected bool
	}{
		{"test@builtin", true},
		{"plugin@builtin", true},
		{"test@marketplace", false},
		{"test", false},
		{"@builtin", false},
	}

	for _, tt := range tests {
		t.Run(tt.pluginID, func(t *testing.T) {
			result := IsBuiltinPluginID(tt.pluginID)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestGetBuiltinPlugins(t *testing.T) {
	ClearBuiltinPlugins()

	// Register plugins
	plugin1 := &BuiltinPluginDefinition{
		Name:           "plugin1",
		Description:    "Plugin 1",
		Version:        "1.0.0",
		DefaultEnabled: true,
	}

	plugin2 := &BuiltinPluginDefinition{
		Name:           "plugin2",
		Description:    "Plugin 2",
		Version:        "1.0.0",
		DefaultEnabled: false,
	}

	RegisterBuiltinPlugin(plugin1)
	RegisterBuiltinPlugin(plugin2)

	// Get plugins with no user settings
	enabled, disabled := GetBuiltinPlugins(nil)

	if len(enabled) != 1 {
		t.Errorf("expected 1 enabled plugin, got %d", len(enabled))
	}

	if len(disabled) != 1 {
		t.Errorf("expected 1 disabled plugin, got %d", len(disabled))
	}

	// Get plugins with user settings
	userSettings := map[string]bool{
		"plugin1@builtin": false, // Override default
		"plugin2@builtin": true,  // Override default
	}

	enabled, disabled = GetBuiltinPlugins(userSettings)

	if len(enabled) != 1 {
		t.Errorf("expected 1 enabled plugin, got %d", len(enabled))
	}

	if enabled[0].Name != "plugin2" {
		t.Errorf("expected plugin2 to be enabled, got %s", enabled[0].Name)
	}

	if len(disabled) != 1 {
		t.Errorf("expected 1 disabled plugin, got %d", len(disabled))
	}

	if disabled[0].Name != "plugin1" {
		t.Errorf("expected plugin1 to be disabled, got %s", disabled[0].Name)
	}

	ClearBuiltinPlugins()
}

func TestGetBuiltinPluginSkills(t *testing.T) {
	ClearBuiltinPlugins()

	// Create skills
	skill1 := &skills.SkillDefinition{
		Name:        "skill1",
		Description: "Skill 1",
	}

	skill2 := &skills.SkillDefinition{
		Name:        "skill2",
		Description: "Skill 2",
	}

	// Register plugin with skills
	plugin := &BuiltinPluginDefinition{
		Name:           "test-plugin",
		Description:    "Test plugin",
		Version:        "1.0.0",
		DefaultEnabled: true,
		Skills:         []*skills.SkillDefinition{skill1, skill2},
	}

	RegisterBuiltinPlugin(plugin)

	// Get skills
	userSettings := map[string]bool{
		"test-plugin@builtin": true,
	}

	skillsList := GetBuiltinPluginSkills(userSettings)

	if len(skillsList) != 2 {
		t.Errorf("expected 2 skills, got %d", len(skillsList))
	}

	// Test with disabled plugin
	userSettings["test-plugin@builtin"] = false
	skillsList = GetBuiltinPluginSkills(userSettings)

	if len(skillsList) != 0 {
		t.Errorf("expected 0 skills from disabled plugin, got %d", len(skillsList))
	}

	ClearBuiltinPlugins()
}

func TestPluginAvailability(t *testing.T) {
	ClearBuiltinPlugins()

	// Register plugin with availability check
	available := true
	plugin := &BuiltinPluginDefinition{
		Name:           "conditional-plugin",
		Description:    "Conditional plugin",
		Version:        "1.0.0",
		DefaultEnabled: true,
		IsAvailable: func() bool {
			return available
		},
	}

	RegisterBuiltinPlugin(plugin)

	// Plugin should be available
	enabled, disabled := GetBuiltinPlugins(nil)
	if len(enabled) != 1 {
		t.Errorf("expected 1 enabled plugin, got %d", len(enabled))
	}

	// Make plugin unavailable
	available = false
	enabled, disabled = GetBuiltinPlugins(nil)
	if len(enabled) != 0 {
		t.Errorf("expected 0 enabled plugins when unavailable, got %d", len(enabled))
	}
	if len(disabled) != 0 {
		t.Errorf("expected 0 disabled plugins when unavailable, got %d", len(disabled))
	}

	ClearBuiltinPlugins()
}

func TestClearBuiltinPlugins(t *testing.T) {
	// Register a plugin
	plugin := &BuiltinPluginDefinition{
		Name:        "test",
		Description: "Test",
		Version:     "1.0.0",
	}

	RegisterBuiltinPlugin(plugin)

	// Verify it exists
	_, ok := GetBuiltinPluginDefinition("test")
	if !ok {
		t.Fatal("plugin should exist")
	}

	// Clear registry
	ClearBuiltinPlugins()

	// Verify it's gone
	_, ok = GetBuiltinPluginDefinition("test")
	if ok {
		t.Error("plugin should not exist after clear")
	}
}
