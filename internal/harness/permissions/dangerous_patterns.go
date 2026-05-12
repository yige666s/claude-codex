package permissions

// DangerousBashPatterns lists bash command prefixes that can execute arbitrary code.
// These are used to strip broad allow-rules when entering auto mode.
var DangerousBashPatterns = []string{
	// Interpreters
	"python", "python3", "python2",
	"node", "deno", "tsx",
	"ruby", "perl", "php", "lua",
	// Package runners
	"npx", "bunx", "npm run", "yarn run", "pnpm run", "bun run",
	// Shells
	"bash", "sh", "zsh", "fish",
	// Dangerous utilities
	"eval", "exec", "env", "xargs", "sudo",
}

// CrossPlatformCodeExecPatterns is the subset safe for non-Ant builds.
var CrossPlatformCodeExecPatterns = []string{
	"python", "python3", "python2",
	"node", "deno", "tsx",
	"ruby", "perl", "php", "lua",
	"npx", "bunx", "npm run", "yarn run", "pnpm run", "bun run",
	"bash", "sh",
	"ssh",
}

// SafeYoloAllowlistedTools lists tools that skip the classifier in auto mode.
var SafeYoloAllowlistedTools = map[string]bool{
	"FileRead":             true,
	"Grep":                 true,
	"Glob":                 true,
	"LSP":                  true,
	"ToolSearch":           true,
	"ListMcpResourcesTool": true,
	"ReadMcpResourceTool":  true,
	"TodoWrite":            true,
	"TaskCreate":           true,
	"TaskGet":              true,
	"TaskUpdate":           true,
	"TaskList":             true,
	"TaskStop":             true,
	"TaskOutput":           true,
	"AskUserQuestion":      true,
	"EnterPlanMode":        true,
	"ExitPlanMode":         true,
	"TeamCreate":           true,
	"TeamDelete":           true,
	"SendMessage":          true,
	"Sleep":                true,
	"classify_result":      true,
}

// IsAutoModeAllowlistedTool returns true if the tool skips the classifier in auto mode.
func IsAutoModeAllowlistedTool(toolName string) bool {
	return SafeYoloAllowlistedTools[toolName]
}

// IsDangerousBashPermission checks if a rule content string represents a dangerous
// broad allow-rule that should be stripped when entering auto mode.
func IsDangerousBashPermission(ruleContent string) bool {
	for _, pattern := range DangerousBashPatterns {
		// Matches "pattern:*" or "pattern " prefix
		if ruleContent == pattern+":*" || ruleContent == pattern {
			return true
		}
	}
	return false
}
