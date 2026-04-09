package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// agentDefinitionsCache caches the result of getAgentDefinitionsWithOverrides
// per working directory. Invalidated by ClearAgentDefinitionsCache().
var agentDefinitionsCache struct {
	mu    sync.RWMutex
	cache map[string][]*AgentDefinition
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
//  2. User settings (~/.claude/agents/)
//  3. Project settings (<cwd>/.claude/agents/)
//  4. Built-in agents
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
	userAgents, warn, err := loadFromDir(userAgentsDir, SourceUser)
	if err != nil {
		warnings = append(warnings, fmt.Sprintf("user agents dir: %v", err))
	}
	warnings = append(warnings, warn...)

	// Merge in project-level agents.
	projectAgentsDir := filepath.Join(cwd, ".claude", "agents")
	projectAgents, warn, err := loadFromDir(projectAgentsDir, SourceUser)
	if err != nil {
		warnings = append(warnings, fmt.Sprintf("project agents dir: %v", err))
	}
	warnings = append(warnings, warn...)

	// Merge: higher-priority sources win on AgentType collision.
	merged := mergeAgentDefinitions(builtins, userAgents, projectAgents)

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

// agentFrontmatter holds YAML-like frontmatter fields parsed from .md agent files.
type agentFrontmatter struct {
	AgentType       string   `json:"agent_type"`
	WhenToUse       string   `json:"when_to_use"`
	Tools           []string `json:"tools"`
	DisallowedTools []string `json:"disallowed_tools"`
	MaxTurns        int      `json:"max_turns"`
	Model           string   `json:"model"`
	Permission      string   `json:"permission_mode"`
	Background      bool     `json:"background"`
	OmitClaudeMd    bool     `json:"omit_claude_md"`
	Color           string   `json:"color"`
	MCPServers      []string `json:"mcp_servers"`
	Skills          []string `json:"skills"`
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

	// Fall back to filename as agent type.
	agentType := fm.AgentType
	if agentType == "" {
		base := filepath.Base(path)
		agentType = strings.TrimSuffix(base, filepath.Ext(base))
	}

	def := &AgentDefinition{
		AgentType:       AgentType(agentType),
		WhenToUse:       fm.WhenToUse,
		Tools:           fm.Tools,
		DisallowedTools: fm.DisallowedTools,
		MaxTurns:        fm.MaxTurns,
		Model:           ModelOption(fm.Model),
		Permission:      PermissionMode(fm.Permission),
		Source:          source,
		BaseDir:         filepath.Dir(path),
		Background:      fm.Background,
		OmitClaudeMd:    fm.OmitClaudeMd,
		Color:           fm.Color,
		MCPServers:      fm.MCPServers,
		Skills:          fm.Skills,
		SystemPrompt:    systemPrompt,
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
	AgentType       string   `json:"agent_type"`
	WhenToUse       string   `json:"when_to_use"`
	Tools           []string `json:"tools"`
	DisallowedTools []string `json:"disallowed_tools"`
	MaxTurns        int      `json:"max_turns"`
	Model           string   `json:"model"`
	PermissionMode  string   `json:"permission_mode"`
	SystemPrompt    string   `json:"system_prompt"`
	Background      bool     `json:"background"`
	OmitClaudeMd    bool     `json:"omit_claude_md"`
	Color           string   `json:"color"`
	MCPServers      []string `json:"mcp_servers"`
	Skills          []string `json:"skills"`
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

	agentType := raw.AgentType
	if agentType == "" {
		base := filepath.Base(path)
		agentType = strings.TrimSuffix(base, filepath.Ext(base))
	}

	def := &AgentDefinition{
		AgentType:       AgentType(agentType),
		WhenToUse:       raw.WhenToUse,
		Tools:           raw.Tools,
		DisallowedTools: raw.DisallowedTools,
		MaxTurns:        raw.MaxTurns,
		Model:           ModelOption(raw.Model),
		Permission:      PermissionMode(raw.PermissionMode),
		Source:          source,
		BaseDir:         filepath.Dir(path),
		Background:      raw.Background,
		OmitClaudeMd:    raw.OmitClaudeMd,
		Color:           raw.Color,
		MCPServers:      raw.MCPServers,
		Skills:          raw.Skills,
		SystemPrompt:    raw.SystemPrompt,
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
			listItems = append(listItems, strings.TrimPrefix(trimmed, "- "))
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
