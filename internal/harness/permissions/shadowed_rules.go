package permissions

import "strings"

type ShadowType string

const (
	ShadowTypeAsk  ShadowType = "ask"
	ShadowTypeDeny ShadowType = "deny"
)

type UnreachableRule struct {
	Rule       Rule
	Reason     string
	ShadowedBy Rule
	ShadowType ShadowType
	Fix        string
}

type DetectUnreachableRulesOptions struct {
	SandboxAutoAllowEnabled bool
}

func IsSharedSettingSource(source RuleSource) bool {
	switch source {
	case SourceProjectSettings, SourcePolicySettings, SourceCommand:
		return true
	default:
		return false
	}
}

func DetectUnreachableRules(ctx *ToolContext, options DetectUnreachableRulesOptions) []UnreachableRule {
	allowRules := parseRules(ctx.AlwaysAllowRules, BehaviorAllow)
	askRules := parseRules(ctx.AlwaysAskRules, BehaviorAsk)
	denyRules := parseRules(ctx.AlwaysDenyRules, BehaviorDeny)

	var unreachable []UnreachableRule
	for _, allowRule := range allowRules {
		if allowRule.Value.RuleContent == "" {
			continue
		}

		if denyRule, ok := findToolWideRule(denyRules, allowRule.Value.ToolName); ok {
			unreachable = append(unreachable, buildUnreachableRule(allowRule, denyRule, ShadowTypeDeny))
			continue
		}

		if askRule, ok := findToolWideRule(askRules, allowRule.Value.ToolName); ok {
			if options.SandboxAutoAllowEnabled &&
				strings.EqualFold(allowRule.Value.ToolName, "bash") &&
				!IsSharedSettingSource(askRule.Source) {
				continue
			}
			unreachable = append(unreachable, buildUnreachableRule(allowRule, askRule, ShadowTypeAsk))
		}
	}

	return unreachable
}

func parseRules(rules RulesBySource, behavior Behavior) []Rule {
	var out []Rule
	for _, source := range permissionRuleSources {
		values := rules[source]
		for _, value := range values {
			out = append(out, Rule{
				Source:   source,
				Behavior: behavior,
				Value:    RuleValueFromString(value),
			})
		}
	}
	return out
}

func findToolWideRule(rules []Rule, toolName string) (Rule, bool) {
	for _, rule := range rules {
		if rule.Value.RuleContent != "" {
			continue
		}
		if strings.EqualFold(rule.Value.ToolName, toolName) {
			return rule, true
		}
	}
	return Rule{}, false
}

func buildUnreachableRule(allowRule, shadowingRule Rule, shadowType ShadowType) UnreachableRule {
	var reason string
	switch shadowType {
	case ShadowTypeDeny:
		reason = `Blocked by "` + shadowingRule.Value.ToolName + `" deny rule`
	default:
		reason = `Shadowed by "` + shadowingRule.Value.ToolName + `" ask rule`
	}

	return UnreachableRule{
		Rule:       allowRule,
		Reason:     reason + " (from " + string(shadowingRule.Source) + ")",
		ShadowedBy: shadowingRule,
		ShadowType: shadowType,
		Fix:        buildFixSuggestion(shadowType, shadowingRule, allowRule),
	}
}

func buildFixSuggestion(shadowType ShadowType, shadowingRule, shadowedRule Rule) string {
	verb := "ask"
	if shadowType == ShadowTypeDeny {
		verb = "deny"
	}
	return `Remove the "` + shadowingRule.Value.ToolName + `" ` + verb + ` rule from ` +
		string(shadowingRule.Source) + `, or remove the specific allow rule from ` + string(shadowedRule.Source)
}
