package permissions

import (
	"context"
	"strings"

	"claude-codex/internal/harness/hooks"
)

// HookDecisionResolver lets PermissionRequest hooks resolve permission prompts
// before the interactive handler is used.
type HookDecisionResolver struct {
	Executor   *hooks.Executor
	WorkingDir string
	SessionID  string
}

func (r HookDecisionResolver) ResolvePermission(ctx context.Context, request Request) (Decision, bool, error) {
	if r.Executor == nil {
		return Decision{}, false, nil
	}
	result, err := r.Executor.Execute(ctx, hooks.EventPermissionRequest, &hooks.HookInput{
		Event:      hooks.EventPermissionRequest,
		SessionID:  r.SessionID,
		WorkingDir: r.WorkingDir,
		Permission: &hooks.PermissionInfo{
			ToolName:    request.ToolName,
			Description: request.Summary,
			Input:       metadataAsHookInput(request.Metadata),
			Reason:      "permission request",
		},
		Metadata: map[string]any{
			"level": request.Level,
		},
	})
	if err != nil {
		return Decision{}, false, err
	}
	if len(result.BlockingErrors) > 0 {
		return Decision{
			Behavior: BehaviorDeny,
			Reason:   strings.Join(result.BlockingErrors, "; "),
		}, true, nil
	}
	if !result.Continue {
		reason := strings.TrimSpace(result.StopReason)
		if reason == "" {
			reason = "permission request stopped by hook"
		}
		return Decision{Behavior: BehaviorDeny, Reason: reason}, true, nil
	}

	behavior := Behavior(strings.ToLower(strings.TrimSpace(result.PermissionBehavior)))
	switch behavior {
	case BehaviorAllow, BehaviorDeny:
		return Decision{
			Behavior: behavior,
			Reason:   result.PermissionDecisionReason,
			Updates:  hookPermissionUpdates(result.PermissionUpdates),
		}, true, nil
	case BehaviorAsk, BehaviorPassthrough, "":
		return Decision{}, false, nil
	default:
		return Decision{
			Behavior: BehaviorDeny,
			Reason:   "permission hook returned invalid behavior " + string(behavior),
		}, true, nil
	}
}

func metadataAsHookInput(metadata map[string]string) map[string]any {
	input := make(map[string]any, len(metadata))
	for key, value := range metadata {
		input[key] = value
	}
	return input
}

func hookPermissionUpdates(updates []hooks.PermissionUpdate) []PermissionUpdate {
	if len(updates) == 0 {
		return nil
	}
	out := make([]PermissionUpdate, 0, len(updates))
	for _, update := range updates {
		tool := strings.TrimSpace(update.Tool)
		if tool == "" {
			continue
		}
		behavior := Behavior(strings.ToLower(strings.TrimSpace(update.Behavior)))
		switch behavior {
		case BehaviorAllow, BehaviorDeny, BehaviorAsk:
		default:
			behavior = BehaviorAllow
		}
		out = append(out, PermissionUpdate{
			Type:        UpdateAddRules,
			Destination: SourceSession,
			Behavior:    behavior,
			Rules:       []RuleValue{RuleValueFromString(tool)},
		})
	}
	return out
}
