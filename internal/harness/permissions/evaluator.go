package permissions

import (
	"context"
	"fmt"
	"strings"
)

// Decision is the typed result of an interactive permission approval flow.
type Decision struct {
	Behavior Behavior
	Updates  []PermissionUpdate
	Remember bool
	Reason   string
}

type DecisionHandler func(ctx context.Context, request Request) (Decision, error)

// DecisionResolver can resolve a permission request before the interactive
// approval handler is used. It returns ok=false to let the next resolver run.
type DecisionResolver interface {
	ResolvePermission(ctx context.Context, request Request) (decision Decision, ok bool, err error)
}

// DecisionResolverFunc adapts a function into a DecisionResolver.
type DecisionResolverFunc func(context.Context, Request) (Decision, bool, error)

func (f DecisionResolverFunc) ResolvePermission(ctx context.Context, request Request) (Decision, bool, error) {
	return f(ctx, request)
}

func evaluateRuleDecision(toolCtx *ToolContext, request Request) (PermissionResult, bool) {
	if toolCtx == nil {
		return PermissionResult{}, false
	}
	if rule, ok := findRequestRule(GetDenyRules(toolCtx), request); ok {
		return PermissionResult{
			Behavior: BehaviorDeny,
			Message:  fmt.Sprintf("Tool %s denied by permission rule.", request.ToolName),
			DecisionReason: &DecisionReason{
				Type: ReasonRule,
				Rule: &rule,
			},
		}, true
	}
	if rule, ok := findRequestRule(GetAskRules(toolCtx), request); ok {
		return PermissionResult{
			Behavior:    BehaviorAsk,
			Message:     createPermissionRequestMessage(request.ToolName, &rule),
			Suggestions: approvalSuggestionsForRule(request, rule),
			DecisionReason: &DecisionReason{
				Type: ReasonRule,
				Rule: &rule,
			},
		}, true
	}
	if rule, ok := findRequestRule(GetAllowRules(toolCtx), request); ok {
		return PermissionResult{
			Behavior: BehaviorAllow,
			DecisionReason: &DecisionReason{
				Type: ReasonRule,
				Rule: &rule,
			},
		}, true
	}
	return PermissionResult{}, false
}

func findRequestRule(rules []Rule, request Request) (Rule, bool) {
	for _, rule := range rules {
		if requestMatchesRule(request, rule) {
			return rule, true
		}
	}
	return Rule{}, false
}

func requestMatchesRule(request Request, rule Rule) bool {
	if !strings.EqualFold(rule.Value.ToolName, request.ToolName) && !toolMatchesRuleName(request.ToolName, rule) {
		return false
	}
	if rule.Value.RuleContent == "" {
		return true
	}

	switch {
	case strings.EqualFold(request.ToolName, "bash"):
		command := request.Metadata["command"]
		if command == "" {
			command = request.Summary
		}
		if command == "" {
			return false
		}
		return MatchesRuleForBehavior(ParseShellPermissionRule(rule.Value.RuleContent), command, rule.Behavior, request.Metadata["compound"] != "")
	case request.Metadata["subagent_type"] != "":
		return rule.Value.RuleContent == request.Metadata["subagent_type"]
	case request.Metadata["path"] != "":
		return rule.Value.RuleContent == request.Metadata["path"]
	case request.Metadata["url"] != "":
		return rule.Value.RuleContent == request.Metadata["url"]
	case request.Metadata["uri"] != "":
		return rule.Value.RuleContent == request.Metadata["uri"]
	default:
		return rule.Value.RuleContent == request.Summary
	}
}

func approvalSuggestionsForRequest(request Request) []PermissionUpdate {
	if !strings.EqualFold(request.ToolName, "bash") {
		return nil
	}
	command := strings.TrimSpace(request.Metadata["command"])
	if command == "" {
		command = strings.TrimSpace(request.Summary)
	}
	if command == "" {
		return nil
	}

	return []PermissionUpdate{SuggestionForShellCommand("Bash", command)}
}

func approvalSuggestionsForRule(request Request, rule Rule) []PermissionUpdate {
	if rule.Value.RuleContent == "" {
		return approvalSuggestionsForRequest(request)
	}
	return []PermissionUpdate{{
		Type:        UpdateAddRules,
		Destination: SourceLocalSettings,
		Behavior:    BehaviorAllow,
		Rules:       []RuleValue{rule.Value},
	}}
}

func createPermissionRequestMessage(toolName string, rule *Rule) string {
	if rule == nil {
		return fmt.Sprintf("Claude requested permission to use %s.", toolName)
	}
	return fmt.Sprintf("Permission rule %q requires approval for %s.", RuleValueToString(rule.Value), toolName)
}
