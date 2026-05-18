package agent

import (
	"sort"
	"strings"

	"claude-codex/internal/harness/permissions"
)

// ResolvedAgentTools is the result of resolving an agent's tool configuration.
// Mirrors ResolvedAgentTools in TypeScript.
type ResolvedAgentTools struct {
	// HasWildcard is true when the agent declared tools: ["*"]
	HasWildcard bool
	// ValidTools is the subset of requested tools that exist in availableTools
	ValidTools []string
	// InvalidTools is the subset of requested tools that do NOT exist
	InvalidTools []string
	// ResolvedTools is the final filtered list to hand to the agent
	ResolvedTools []string
	// AllowedAgentTypes are agent sub-types the agent may spawn (when HasWildcard)
	AllowedAgentTypes []string
}

// filterToolsForAgent filters an available-tools list down to what an agent is
// allowed to use, applying AllAgentDisallowedTools, AsyncAgentAllowedTools, and
// CustomAgentDisallowedTools depending on the agent's properties.
//
// Mirrors filterToolsForAgent in agentToolUtils.ts.
func filterToolsForAgent(
	tools []string,
	isBuiltIn bool,
	isAsync bool,
	permissionMode PermissionMode,
) []string {
	var result []string
	for _, name := range tools {
		if strings.HasPrefix(name, "mcp__") {
			result = append(result, name)
			continue
		}
		if name == ToolExitPlanMode && permissionMode == PermissionMode("plan") {
			result = append(result, name)
			continue
		}
		// Universal deny-list
		if AllAgentDisallowedTools[name] {
			continue
		}
		// Non-built-in agents have an additional deny-list
		if !isBuiltIn && CustomAgentDisallowedTools[name] {
			continue
		}
		// Async agents can only use the async-allowed set
		if isAsync && !AsyncAgentAllowedTools[name] {
			if InProcessTeammateAllowedTools[name] {
				result = append(result, name)
				continue
			}
			continue
		}
		result = append(result, name)
	}
	return result
}

// resolveAgentTools resolves which tools are available to an agent given its
// definition and the host session's available tools.
//
// Mirrors resolveAgentTools in agentToolUtils.ts.
func resolveAgentTools(
	def *AgentDefinition,
	availableTools []string,
	isAsync bool,
	isMainThread bool,
) ResolvedAgentTools {
	isBuiltIn := def.Source == SourceBuiltIn

	// Build a set of available tool names for O(1) lookup.
	available := make(map[string]bool, len(availableTools))
	for _, t := range availableTools {
		available[t] = true
	}

	agentTools := def.Tools
	if len(agentTools) == 0 {
		agentTools = []string{"*"}
	}

	// Wildcard: agent wants all tools.
	if len(agentTools) == 1 && agentTools[0] == "*" {
		filtered := availableTools
		if !isMainThread {
			filtered = filterToolsForAgent(availableTools, isBuiltIn, isAsync, def.Permission)
		}
		// Apply agent-level disallowed list.
		filtered = applyDisallowed(filtered, def.DisallowedTools)
		sort.Strings(filtered)
		return ResolvedAgentTools{
			HasWildcard:   true,
			ValidTools:    filtered,
			ResolvedTools: filtered,
		}
	}

	// Explicit tool list.
	var valid, invalid []string
	var allowedAgentTypes []string
	for _, toolSpec := range agentTools {
		rule := permissions.RuleValueFromString(toolSpec)
		toolName := rule.ToolName
		if toolName == "" {
			toolName = toolSpec
		}

		if toolName == ToolAgent {
			if rule.RuleContent != "" {
				for _, value := range strings.Split(rule.RuleContent, ",") {
					value = strings.TrimSpace(value)
					if value != "" {
						allowedAgentTypes = append(allowedAgentTypes, value)
					}
				}
			}
			if !isMainThread {
				valid = append(valid, toolSpec)
				continue
			}
		}

		if available[toolName] {
			valid = append(valid, toolSpec)
		} else {
			invalid = append(invalid, toolSpec)
		}
	}

	validToolNames := make([]string, 0, len(valid))
	for _, toolSpec := range valid {
		rule := permissions.RuleValueFromString(toolSpec)
		if rule.ToolName != "" {
			validToolNames = append(validToolNames, rule.ToolName)
			continue
		}
		validToolNames = append(validToolNames, toolSpec)
	}

	filtered := validToolNames
	if !isMainThread {
		filtered = filterToolsForAgent(validToolNames, isBuiltIn, isAsync, def.Permission)
	}
	filtered = applyDisallowed(filtered, def.DisallowedTools)
	sort.Strings(filtered)
	sort.Strings(valid)
	sort.Strings(invalid)

	return ResolvedAgentTools{
		HasWildcard:       false,
		ValidTools:        valid,
		InvalidTools:      invalid,
		ResolvedTools:     filtered,
		AllowedAgentTypes: allowedAgentTypes,
	}
}

// ResolveAgentTools exposes the agent tool resolution contract for callers
// outside this package while keeping the implementation shared with tests.
func ResolveAgentTools(def *AgentDefinition, availableTools []string, isAsync bool, isMainThread bool) ResolvedAgentTools {
	if def == nil {
		return ResolvedAgentTools{}
	}
	return resolveAgentTools(def, availableTools, isAsync, isMainThread)
}

// HasRequiredMCPServers returns true when every required MCP server pattern on
// the agent is satisfied by at least one available server name. Matching is
// case-insensitive and substring-based to mirror Claude Code's
// hasRequiredMcpServers behavior.
func HasRequiredMCPServers(def *AgentDefinition, availableServers []string) bool {
	if def == nil || len(def.RequiredMCPServers) == 0 {
		return true
	}
	for _, rawPattern := range def.RequiredMCPServers {
		pattern := strings.ToLower(strings.TrimSpace(rawPattern))
		if pattern == "" {
			continue
		}
		matched := false
		for _, server := range availableServers {
			if strings.Contains(strings.ToLower(server), pattern) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

// FilterAgentsByMCPRequirements returns only agents whose required MCP server
// patterns are available.
func FilterAgentsByMCPRequirements(definitions []*AgentDefinition, availableServers []string) []*AgentDefinition {
	filtered := make([]*AgentDefinition, 0, len(definitions))
	for _, def := range definitions {
		if HasRequiredMCPServers(def, availableServers) {
			filtered = append(filtered, def)
		}
	}
	return filtered
}

// applyDisallowed removes any tool names that appear in the disallowed list.
func applyDisallowed(tools []string, disallowed []string) []string {
	if len(disallowed) == 0 {
		return tools
	}
	deny := make(map[string]bool, len(disallowed))
	for _, d := range disallowed {
		rule := permissions.RuleValueFromString(d)
		if rule.ToolName != "" {
			deny[rule.ToolName] = true
			continue
		}
		deny[d] = true
	}
	result := tools[:0:0]
	for _, t := range tools {
		if !deny[t] {
			result = append(result, t)
		}
	}
	return result
}
