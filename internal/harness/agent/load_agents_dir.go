package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// agentDefinitionsCache caches the result of getAgentDefinitionsWithOverrides
// per working directory. Invalidated by ClearAgentDefinitionsCache().
var agentDefinitionsCache struct {
	mu    sync.RWMutex
	cache map[string][]*AgentDefinition
}

var pluginDefinitions struct {
	mu          sync.RWMutex
	definitions []*AgentDefinition
}

func init() {
	agentDefinitionsCache.cache = make(map[string][]*AgentDefinition)
}

// ClearAgentDefinitionsCache invalidates the memoized agent definitions cache.
// Call this after /reload-plugins or plugin installation.
func ClearAgentDefinitionsCache() {
	agentDefinitionsCache.mu.Lock()
	agentDefinitionsCache.cache = make(map[string][]*AgentDefinition)
	agentDefinitionsCache.mu.Unlock()
}

func RegisterPluginDefinitions(definitions []*AgentDefinition) {
	pluginDefinitions.mu.Lock()
	defer pluginDefinitions.mu.Unlock()
	pluginDefinitions.definitions = append([]*AgentDefinition(nil), definitions...)
	ClearAgentDefinitionsCache()
}

func ClearPluginDefinitions() {
	pluginDefinitions.mu.Lock()
	pluginDefinitions.definitions = nil
	pluginDefinitions.mu.Unlock()
	ClearAgentDefinitionsCache()
}

func GetPluginDefinitions() []*AgentDefinition {
	pluginDefinitions.mu.RLock()
	defer pluginDefinitions.mu.RUnlock()
	out := make([]*AgentDefinition, len(pluginDefinitions.definitions))
	copy(out, pluginDefinitions.definitions)
	return out
}

// AgentDefinitionsResult holds the merged list of agent definitions and any
// warnings encountered during loading.
type AgentDefinitionsResult struct {
	Agents   []*AgentDefinition
	Warnings []string
}

// GetAgentDefinitionsWithOverrides loads all agent definitions for the given
// working directory. Results are memoized until ClearAgentDefinitionsCache().
//
// Priority (highest first):
//  1. Policy settings (managed agents — override everything)
//  2. Flag settings / CLI args
//  3. Project settings (<cwd>/.claude/agents/)
//  4. User settings (~/.claude/agents/)
//  5. Plugin agents
//  6. Built-in agents
//
// Mirrors getAgentDefinitionsWithOverrides in loadAgentsDir.ts.
func GetAgentDefinitionsWithOverrides(cwd string, isCoordinatorMode bool) (*AgentDefinitionsResult, error) {
	cacheKey := fmt.Sprintf("%s|%v", cwd, isCoordinatorMode)

	agentDefinitionsCache.mu.RLock()
	if cached, ok := agentDefinitionsCache.cache[cacheKey]; ok {
		agentDefinitionsCache.mu.RUnlock()
		return &AgentDefinitionsResult{Agents: cached}, nil
	}
	agentDefinitionsCache.mu.RUnlock()

	result, err := loadAgentDefinitions(cwd, isCoordinatorMode)
	if err != nil {
		return nil, err
	}

	agentDefinitionsCache.mu.Lock()
	agentDefinitionsCache.cache[cacheKey] = result.Agents
	agentDefinitionsCache.mu.Unlock()

	return result, nil
}

// loadAgentDefinitions performs the actual load (no caching).
func loadAgentDefinitions(cwd string, isCoordinatorMode bool) (*AgentDefinitionsResult, error) {
	var warnings []string

	// Start with built-ins.
	builtins := GetBuiltInAgents(isCoordinatorMode)
	builtins = append(builtins, ForkAgent)

	// Merge in user-level agents.
	home, _ := os.UserHomeDir()
	userAgentsDir := filepath.Join(home, ".claude", "agents")
	userAgents, warn, err := loadFromDir(userAgentsDir, SourceUserSettings)
	if err != nil {
		warnings = append(warnings, fmt.Sprintf("user agents dir: %v", err))
	}
	warnings = append(warnings, warn...)

	// Merge in project-level agents.
	projectAgentsDir := filepath.Join(cwd, ".claude", "agents")
	projectAgents, warn, err := loadFromDir(projectAgentsDir, SourceProjectSettings)
	if err != nil {
		warnings = append(warnings, fmt.Sprintf("project agents dir: %v", err))
	}
	warnings = append(warnings, warn...)

	pluginAgents := GetPluginDefinitions()

	// Merge: higher-priority sources win on AgentType collision.
	merged := mergeAgentDefinitions(builtins, pluginAgents, userAgents, projectAgents)

	return &AgentDefinitionsResult{Agents: merged, Warnings: warnings}, nil
}

// mergeAgentDefinitions merges agent lists. Later lists have higher priority
// and override earlier entries with the same AgentType.
func mergeAgentDefinitions(lists ...[]*AgentDefinition) []*AgentDefinition {
	seen := make(map[AgentType]*AgentDefinition)
	// Process lowest priority first.
	for _, list := range lists {
		for _, def := range list {
			seen[def.AgentType] = def
		}
	}
	result := make([]*AgentDefinition, 0, len(seen))
	for _, def := range seen {
		result = append(result, def)
	}
	return result
}

// loadFromDir scans a directory for agent definition files (.md and .json).
func loadFromDir(dir string, source AgentSource) ([]*AgentDefinition, []string, error) {
	var defs []*AgentDefinition
	var warnings []string

	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil, nil // empty is fine
	}
	if err != nil {
		return nil, nil, err
	}

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		path := filepath.Join(dir, name)

		var def *AgentDefinition
		var warn string
		var parseErr error

		switch strings.ToLower(filepath.Ext(name)) {
		case ".md":
			def, warn, parseErr = parseAgentFromMarkdown(path, source)
		case ".json":
			def, warn, parseErr = parseAgentFromJSON(path, source)
		default:
			continue
		}

		if parseErr != nil {
			warnings = append(warnings, fmt.Sprintf("skipping %s: %v", path, parseErr))
			continue
		}
		if warn != "" {
			warnings = append(warnings, warn)
		}
		if def != nil {
			defs = append(defs, def)
		}
	}
	return defs, warnings, nil
}

func LoadDefinitionsFromDirectory(dir string, source AgentSource) ([]*AgentDefinition, []string, error) {
	return loadFromDir(dir, source)
}

func LoadDefinitionFromFile(path string, source AgentSource) (*AgentDefinition, string, error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md":
		return parseAgentFromMarkdown(path, source)
	case ".json":
		return parseAgentFromJSON(path, source)
	default:
		return nil, "", fmt.Errorf("unsupported agent definition extension %q", filepath.Ext(path))
	}
}

// agentFrontmatter holds YAML-like frontmatter fields parsed from .md agent files.
type agentFrontmatter struct {
	Name                 string   `json:"name"`
	Description          string   `json:"description"`
	AgentType            string   `json:"agent_type"`
	WhenToUse            string   `json:"when_to_use"`
	Tools                []string `json:"tools"`
	DisallowedTools      []string `json:"disallowed_tools"`
	DisallowedToolsCamel []string `json:"disallowedTools"`
	MaxTurns             int      `json:"max_turns"`
	MaxTurnsCamel        int      `json:"maxTurns"`
	Model                string   `json:"model"`
	Permission           string   `json:"permission_mode"`
	PermissionMode       string   `json:"permissionMode"`
	Background           bool     `json:"background"`
	OmitClaudeMd         bool     `json:"omit_claude_md"`
	OmitClaudeMdCamel    bool     `json:"omitClaudeMd"`
	Color                string   `json:"color"`
	MCPServers           []string `json:"mcp_servers"`
	MCPServersCamel      []string `json:"mcpServers"`
	RequiredMCPServers   []string `json:"requiredMcpServers"`
	Skills               []string `json:"skills"`
	InitialPrompt        string   `json:"initialPrompt"`
	Memory               string   `json:"memory"`
	Isolation            string   `json:"isolation"`
	Effort               string   `json:"effort"`
}

// parseAgentFromMarkdown parses a .md file with optional YAML frontmatter.
// The file format is:
//
//	---
//	agent_type: my-agent
//	when_to_use: ...
//	tools: [Read, Write]
//	---
//	System prompt content here.
func parseAgentFromMarkdown(path string, source AgentSource) (*AgentDefinition, string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}

	content := string(data)
	fm := agentFrontmatter{}
	systemPrompt := content

	// Extract YAML frontmatter between --- delimiters.
	if strings.HasPrefix(content, "---") {
		parts := strings.SplitN(content[3:], "---", 2)
		if len(parts) == 2 {
			fmText := strings.TrimSpace(parts[0])
			systemPrompt = strings.TrimSpace(parts[1])
			if parseErr := parseSimpleYAML(fmText, &fm); parseErr != nil {
				return nil, fmt.Sprintf("frontmatter parse warning in %s: %v", path, parseErr), nil
			}
		}
	}

	normalizeFrontmatter(&fm)

	// Fall back to filename as agent type for legacy Go definitions. TS-style
	// agents should provide "name" in frontmatter; keeping the fallback
	// preserves existing Go behavior.
	agentType := fm.AgentType
	if agentType == "" {
		base := filepath.Base(path)
		agentType = strings.TrimSuffix(base, filepath.Ext(base))
	}

	def := &AgentDefinition{
		AgentType:          AgentType(agentType),
		WhenToUse:          fm.WhenToUse,
		Tools:              fm.Tools,
		DisallowedTools:    fm.DisallowedTools,
		MaxTurns:           fm.MaxTurns,
		Model:              ModelOption(fm.Model),
		Effort:             fm.Effort,
		Permission:         PermissionMode(fm.Permission),
		Source:             source,
		BaseDir:            filepath.Dir(path),
		Filename:           strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)),
		Background:         fm.Background,
		Isolation:          fm.Isolation,
		Memory:             fm.Memory,
		OmitClaudeMd:       fm.OmitClaudeMd,
		Color:              fm.Color,
		MCPServers:         fm.MCPServers,
		RequiredMCPServers: fm.RequiredMCPServers,
		Skills:             fm.Skills,
		InitialPrompt:      fm.InitialPrompt,
		SystemPrompt:       systemPrompt,
	}

	// Apply defaults.
	if len(def.Tools) == 0 {
		def.Tools = []string{"*"}
	}
	if def.MaxTurns == 0 {
		def.MaxTurns = 200
	}
	if def.Model == "" {
		def.Model = ModelInherit
	}
	if def.Permission == "" {
		def.Permission = PermissionDefault
	}

	return def, "", nil
}

// agentJSONDef is the JSON shape for .json agent definition files.
type agentJSONDef struct {
	Description          string   `json:"description"`
	Prompt               string   `json:"prompt"`
	AgentType            string   `json:"agent_type"`
	WhenToUse            string   `json:"when_to_use"`
	Tools                []string `json:"tools"`
	DisallowedTools      []string `json:"disallowed_tools"`
	DisallowedToolsCamel []string `json:"disallowedTools"`
	MaxTurns             int      `json:"max_turns"`
	MaxTurnsCamel        int      `json:"maxTurns"`
	Model                string   `json:"model"`
	Effort               string   `json:"effort"`
	PermissionMode       string   `json:"permission_mode"`
	PermissionModeCamel  string   `json:"permissionMode"`
	SystemPrompt         string   `json:"system_prompt"`
	Background           bool     `json:"background"`
	OmitClaudeMd         bool     `json:"omit_claude_md"`
	OmitClaudeMdCamel    bool     `json:"omitClaudeMd"`
	Color                string   `json:"color"`
	MCPServers           []string `json:"mcp_servers"`
	MCPServersCamel      []string `json:"mcpServers"`
	RequiredMCPServers   []string `json:"requiredMcpServers"`
	Skills               []string `json:"skills"`
	InitialPrompt        string   `json:"initialPrompt"`
	Memory               string   `json:"memory"`
	Isolation            string   `json:"isolation"`
}

// parseAgentFromJSON parses a .json agent definition file.
func parseAgentFromJSON(path string, source AgentSource) (*AgentDefinition, string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}

	var raw agentJSONDef
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, "", fmt.Errorf("JSON parse error: %w", err)
	}
	normalizeJSONAgent(&raw)

	agentType := raw.AgentType
	if agentType == "" {
		base := filepath.Base(path)
		agentType = strings.TrimSuffix(base, filepath.Ext(base))
	}

	def := &AgentDefinition{
		AgentType:          AgentType(agentType),
		WhenToUse:          raw.WhenToUse,
		Tools:              raw.Tools,
		DisallowedTools:    raw.DisallowedTools,
		MaxTurns:           raw.MaxTurns,
		Model:              ModelOption(raw.Model),
		Effort:             raw.Effort,
		Permission:         PermissionMode(raw.PermissionMode),
		Source:             source,
		BaseDir:            filepath.Dir(path),
		Filename:           strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)),
		Background:         raw.Background,
		Isolation:          raw.Isolation,
		Memory:             raw.Memory,
		OmitClaudeMd:       raw.OmitClaudeMd,
		Color:              raw.Color,
		MCPServers:         raw.MCPServers,
		RequiredMCPServers: raw.RequiredMCPServers,
		Skills:             raw.Skills,
		InitialPrompt:      raw.InitialPrompt,
		SystemPrompt:       raw.SystemPrompt,
	}

	if len(def.Tools) == 0 {
		def.Tools = []string{"*"}
	}
	if def.MaxTurns == 0 {
		def.MaxTurns = 200
	}
	if def.Model == "" {
		def.Model = ModelInherit
	}
	if def.Permission == "" {
		def.Permission = PermissionDefault
	}

	return def, "", nil
}

func normalizeFrontmatter(fm *agentFrontmatter) {
	if fm.AgentType == "" {
		fm.AgentType = fm.Name
	}
	if fm.WhenToUse == "" {
		fm.WhenToUse = strings.ReplaceAll(fm.Description, `\n`, "\n")
	}
	if len(fm.DisallowedTools) == 0 {
		fm.DisallowedTools = fm.DisallowedToolsCamel
	}
	if fm.MaxTurns == 0 {
		fm.MaxTurns = fm.MaxTurnsCamel
	}
	if fm.Permission == "" {
		fm.Permission = fm.PermissionMode
	}
	if !fm.OmitClaudeMd {
		fm.OmitClaudeMd = fm.OmitClaudeMdCamel
	}
	if len(fm.MCPServers) == 0 {
		fm.MCPServers = fm.MCPServersCamel
	}
}

func normalizeJSONAgent(raw *agentJSONDef) {
	if raw.WhenToUse == "" {
		raw.WhenToUse = strings.ReplaceAll(raw.Description, `\n`, "\n")
	}
	if raw.SystemPrompt == "" {
		raw.SystemPrompt = raw.Prompt
	}
	if len(raw.DisallowedTools) == 0 {
		raw.DisallowedTools = raw.DisallowedToolsCamel
	}
	if raw.MaxTurns == 0 {
		raw.MaxTurns = raw.MaxTurnsCamel
	}
	if raw.PermissionMode == "" {
		raw.PermissionMode = raw.PermissionModeCamel
	}
	if !raw.OmitClaudeMd {
		raw.OmitClaudeMd = raw.OmitClaudeMdCamel
	}
	if len(raw.MCPServers) == 0 {
		raw.MCPServers = raw.MCPServersCamel
	}
}

// parseSimpleYAML is a minimal YAML parser for the agent frontmatter subset.
// It handles string, bool, int, and string-array values. For production use,
// a proper YAML library should be used.
func parseSimpleYAML(text string, out *agentFrontmatter) error {
	// Build a JSON-compatible map by parsing key: value lines.
	m := make(map[string]interface{})
	var currentKey string
	var inList bool
	var listItems []string

	flush := func() {
		if currentKey != "" && inList {
			m[currentKey] = listItems
			listItems = nil
			inList = false
		}
	}

	for _, line := range strings.Split(text, "\n") {
		// List item continuation.
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "- ") && currentKey != "" {
			inList = true
			item := strings.TrimSpace(strings.TrimPrefix(trimmed, "- "))
			item = strings.Trim(item, `"'`)
			listItems = append(listItems, item)
			continue
		}

		// Key: value line.
		if idx := strings.Index(trimmed, ":"); idx > 0 {
			flush()
			key := strings.TrimSpace(trimmed[:idx])
			val := strings.TrimSpace(trimmed[idx+1:])
			currentKey = key
			inList = false

			if val == "" {
				// Value may be a list on subsequent lines.
				continue
			}

			// Inline list: [a, b, c]
			if strings.HasPrefix(val, "[") && strings.HasSuffix(val, "]") {
				inner := strings.Trim(val, "[]")
				var items []string
				for _, item := range strings.Split(inner, ",") {
					item = strings.TrimSpace(item)
					item = strings.Trim(item, `"'`)
					if item != "" {
						items = append(items, item)
					}
				}
				m[key] = items
				currentKey = ""
				continue
			}

			// Boolean.
			if val == "true" || val == "yes" {
				m[key] = true
				continue
			}
			if val == "false" || val == "no" {
				m[key] = false
				continue
			}
			if n, err := strconv.Atoi(val); err == nil {
				m[key] = n
				continue
			}

			// Strip optional quotes.
			if (strings.HasPrefix(val, `"`) && strings.HasSuffix(val, `"`)) ||
				(strings.HasPrefix(val, `'`) && strings.HasSuffix(val, `'`)) {
				val = val[1 : len(val)-1]
			}
			m[key] = val
		}
	}
	flush()

	// Round-trip via JSON to populate the struct.
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}
