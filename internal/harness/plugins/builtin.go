package plugins

import (
	"sync"

	"claude-codex/internal/harness/skills"
)

// BuiltinMarketplaceName is the marketplace identifier for built-in plugins
const BuiltinMarketplaceName = "builtin"

// BuiltinPluginDefinition defines a built-in plugin
type BuiltinPluginDefinition struct {
	Name           string
	Description    string
	Version        string
	DefaultEnabled bool
	IsAvailable    func() bool // Optional availability check
	Skills         []*skills.SkillDefinition
	Hooks          map[string]interface{}
	MCPServers     map[string]interface{}
}

// LoadedPlugin represents a plugin that has been loaded
type LoadedPlugin struct {
	Name              string
	Manifest          PluginManifest
	Path              string // Filesystem path or "builtin" for built-in plugins
	Source            string // Plugin ID (name@marketplace)
	Repository        string // Repository URL or plugin ID
	Enabled           bool
	IsBuiltin         bool
	HooksConfig       map[string]interface{}
	MCPServers        map[string]interface{}
	LSPServers        map[string]interface{}
	Settings          map[string]interface{}
	CommandsPath      string
	CommandsPaths     []string
	CommandsMetadata  map[string]CommandMetadata
	AgentsPath        string
	AgentsPaths       []string
	SkillsPath        string
	SkillsPaths       []string
	OutputStylesPath  string
	OutputStylesPaths []string
}

// PluginManifest contains plugin metadata
type PluginManifest struct {
	Name         string
	Description  string
	Version      string
	Author       *PluginAuthor
	License      string
	Homepage     string
	Repository   string
	Keywords     []string
	Dependencies []string
}

// BuiltinPluginRegistry manages built-in plugins
type BuiltinPluginRegistry struct {
	mu      sync.RWMutex
	plugins map[string]*BuiltinPluginDefinition
}

var builtinRegistry = &BuiltinPluginRegistry{
	plugins: make(map[string]*BuiltinPluginDefinition),
}

// RegisterBuiltinPlugin registers a built-in plugin
func RegisterBuiltinPlugin(definition *BuiltinPluginDefinition) {
	builtinRegistry.mu.Lock()
	defer builtinRegistry.mu.Unlock()

	builtinRegistry.plugins[definition.Name] = definition
}

// IsBuiltinPluginID checks if a plugin ID represents a built-in plugin
func IsBuiltinPluginID(pluginID string) bool {
	suffix := "@" + BuiltinMarketplaceName
	return len(pluginID) > len(suffix) && pluginID[len(pluginID)-len(suffix):] == suffix
}

// GetBuiltinPluginDefinition returns a specific built-in plugin definition
func GetBuiltinPluginDefinition(name string) (*BuiltinPluginDefinition, bool) {
	builtinRegistry.mu.RLock()
	defer builtinRegistry.mu.RUnlock()

	def, ok := builtinRegistry.plugins[name]
	return def, ok
}

// GetBuiltinPlugins returns all built-in plugins split by enabled/disabled status
func GetBuiltinPlugins(enabledPlugins map[string]bool) (enabled, disabled []*LoadedPlugin) {
	builtinRegistry.mu.RLock()
	defer builtinRegistry.mu.RUnlock()

	for name, definition := range builtinRegistry.plugins {
		// Check availability
		if definition.IsAvailable != nil && !definition.IsAvailable() {
			continue
		}

		pluginID := name + "@" + BuiltinMarketplaceName

		// Determine enabled state: user preference > plugin default > true
		isEnabled := true
		if enabledPlugins != nil {
			if userSetting, ok := enabledPlugins[pluginID]; ok {
				isEnabled = userSetting
			} else {
				// Use plugin default if no user setting
				isEnabled = definition.DefaultEnabled
			}
		} else {
			// No user settings, use plugin default
			isEnabled = definition.DefaultEnabled
		}

		plugin := &LoadedPlugin{
			Name: name,
			Manifest: PluginManifest{
				Name:        name,
				Description: definition.Description,
				Version:     definition.Version,
			},
			Path:        BuiltinMarketplaceName,
			Source:      pluginID,
			Repository:  pluginID,
			Enabled:     isEnabled,
			IsBuiltin:   true,
			HooksConfig: definition.Hooks,
			MCPServers:  definition.MCPServers,
		}

		if isEnabled {
			enabled = append(enabled, plugin)
		} else {
			disabled = append(disabled, plugin)
		}
	}

	return enabled, disabled
}

// GetBuiltinPluginSkills returns skills from enabled built-in plugins
func GetBuiltinPluginSkills(enabledPlugins map[string]bool) []*skills.SkillDefinition {
	enabled, _ := GetBuiltinPlugins(enabledPlugins)

	var allSkills []*skills.SkillDefinition
	for _, plugin := range enabled {
		definition, ok := GetBuiltinPluginDefinition(plugin.Name)
		if !ok || definition.Skills == nil {
			continue
		}

		allSkills = append(allSkills, definition.Skills...)
	}

	return allSkills
}

// ClearBuiltinPlugins clears the built-in plugins registry (for testing)
func ClearBuiltinPlugins() {
	builtinRegistry.mu.Lock()
	defer builtinRegistry.mu.Unlock()

	builtinRegistry.plugins = make(map[string]*BuiltinPluginDefinition)
}

// InitBuiltinPlugins initializes built-in plugins
// This is called during CLI startup
func InitBuiltinPlugins() {
	// No built-in plugins registered yet
	// This is the scaffolding for migrating bundled skills
	// that should be user-toggleable
}
