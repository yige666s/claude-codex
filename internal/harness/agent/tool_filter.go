package agent

import "sort"

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

	// Wildcard: agent wants all tools.
	if len(def.Tools) == 1 && def.Tools[0] == "*" {
		filtered := filterToolsForAgent(availableTools, isBuiltIn, isAsync, def.Permission)
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
	for _, t := range def.Tools {
		if available[t] {
			valid = append(valid, t)
		} else {
			invalid = append(invalid, t)
		}
	}

	filtered := filterToolsForAgent(valid, isBuiltIn, isAsync, def.Permission)
	filtered = applyDisallowed(filtered, def.DisallowedTools)
	sort.Strings(filtered)
	sort.Strings(invalid)

	return ResolvedAgentTools{
		HasWildcard:   false,
		ValidTools:    filtered,
		InvalidTools:  invalid,
		ResolvedTools: filtered,
	}
}

// applyDisallowed removes any tool names that appear in the disallowed list.
func applyDisallowed(tools []string, disallowed []string) []string {
	if len(disallowed) == 0 {
		return tools
	}
	deny := make(map[string]bool, len(disallowed))
	for _, d := range disallowed {
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
