package permissions

import "strings"

var permissionRuleSources = []RuleSource{
	SourceUserSettings,
	SourceProjectSettings,
	SourceLocalSettings,
	SourcePolicySettings,
	SourceFlagSettings,
	SourceCLIArg,
	SourceCommand,
	SourceSession,
}

// GetAllowRules returns parsed allow rules in stable source order.
func GetAllowRules(ctx *ToolContext) []Rule {
	if ctx == nil {
		return nil
	}
	return parseRules(ctx.AlwaysAllowRules, BehaviorAllow)
}

// GetDenyRules returns parsed deny rules in stable source order.
func GetDenyRules(ctx *ToolContext) []Rule {
	if ctx == nil {
		return nil
	}
	return parseRules(ctx.AlwaysDenyRules, BehaviorDeny)
}

// GetAskRules returns parsed ask rules in stable source order.
func GetAskRules(ctx *ToolContext) []Rule {
	if ctx == nil {
		return nil
	}
	return parseRules(ctx.AlwaysAskRules, BehaviorAsk)
}

// ToolAlwaysAllowedRule returns a tool-wide allow rule for a tool name.
func ToolAlwaysAllowedRule(ctx *ToolContext, toolName string) (Rule, bool) {
	return findMatchingToolRule(GetAllowRules(ctx), toolName)
}

// GetDenyRuleForTool returns a tool-wide deny rule for a tool name.
func GetDenyRuleForTool(ctx *ToolContext, toolName string) (Rule, bool) {
	return findMatchingToolRule(GetDenyRules(ctx), toolName)
}

// GetAskRuleForTool returns a tool-wide ask rule for a tool name.
func GetAskRuleForTool(ctx *ToolContext, toolName string) (Rule, bool) {
	return findMatchingToolRule(GetAskRules(ctx), toolName)
}

// GetDenyRuleForAgent returns a deny rule matching Agent(agentType)-style syntax.
func GetDenyRuleForAgent(ctx *ToolContext, agentToolName, agentType string) (Rule, bool) {
	for _, rule := range GetDenyRules(ctx) {
		if rule.Value.ToolName == agentToolName && rule.Value.RuleContent == agentType {
			return rule, true
		}
	}
	return Rule{}, false
}

// AgentDescriptor is the minimal shape needed to filter denied agents.
type AgentDescriptor struct {
	AgentType string
}

// FilterDeniedAgents removes agents denied by Agent(agentType)-style rules.
func FilterDeniedAgents(agents []AgentDescriptor, ctx *ToolContext, agentToolName string) []AgentDescriptor {
	denied := make(map[string]bool)
	for _, rule := range GetDenyRules(ctx) {
		if rule.Value.ToolName == agentToolName && rule.Value.RuleContent != "" {
			denied[rule.Value.RuleContent] = true
		}
	}
	filtered := make([]AgentDescriptor, 0, len(agents))
	for _, agent := range agents {
		if !denied[agent.AgentType] {
			filtered = append(filtered, agent)
		}
	}
	return filtered
}

// GetRuleByContentsForToolName maps rule content to the rule for a tool and behavior.
func GetRuleByContentsForToolName(ctx *ToolContext, toolName string, behavior Behavior) map[string]Rule {
	rulesByContent := make(map[string]Rule)
	var rules []Rule
	switch behavior {
	case BehaviorAllow:
		rules = GetAllowRules(ctx)
	case BehaviorDeny:
		rules = GetDenyRules(ctx)
	case BehaviorAsk:
		rules = GetAskRules(ctx)
	default:
		return rulesByContent
	}
	for _, rule := range rules {
		if rule.Behavior == behavior && rule.Value.ToolName == toolName && rule.Value.RuleContent != "" {
			rulesByContent[rule.Value.RuleContent] = rule
		}
	}
	return rulesByContent
}

func findMatchingToolRule(rules []Rule, toolName string) (Rule, bool) {
	for _, rule := range rules {
		if toolMatchesRuleName(toolName, rule) {
			return rule, true
		}
	}
	return Rule{}, false
}

func toolMatchesRuleName(toolName string, rule Rule) bool {
	if rule.Value.RuleContent != "" {
		return false
	}
	if rule.Value.ToolName == toolName {
		return true
	}

	ruleServer, ruleTool, ruleOK := splitMCPToolName(rule.Value.ToolName)
	toolServer, _, toolOK := splitMCPToolName(toolName)
	if !ruleOK || !toolOK {
		return false
	}
	return ruleServer == toolServer && (ruleTool == "" || ruleTool == "*")
}

func splitMCPToolName(name string) (serverName string, toolName string, ok bool) {
	if !strings.HasPrefix(name, "mcp__") {
		return "", "", false
	}
	parts := strings.Split(name, "__")
	if len(parts) < 2 || parts[0] != "mcp" || parts[1] == "" {
		return "", "", false
	}
	if len(parts) == 2 {
		return parts[1], "", true
	}
	return parts[1], strings.Join(parts[2:], "__"), true
}
