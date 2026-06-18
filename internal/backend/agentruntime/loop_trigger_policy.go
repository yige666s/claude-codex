package agentruntime

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

var ErrLoopTriggerPolicyBlocked = errors.New("loop trigger blocked by policy")

const defaultLoopReleaseGateTemplateReplayPasses = 3

type LoopReleaseGateReport struct {
	CriticalTestsPassed        bool       `json:"critical_tests_passed"`
	TemplateReplayPassCount    int        `json:"template_replay_pass_count"`
	GovernanceKillSwitchPassed bool       `json:"governance_kill_switch_passed"`
	QuotaGuardPassed           bool       `json:"quota_guard_passed"`
	CheckedAt                  *time.Time `json:"checked_at,omitempty"`
}

func (g LoopReleaseGateReport) Passed() bool {
	return g.CriticalTestsPassed &&
		g.TemplateReplayPassCount >= defaultLoopReleaseGateTemplateReplayPasses &&
		g.GovernanceKillSwitchPassed &&
		g.QuotaGuardPassed
}

func (g LoopReleaseGateReport) MissingChecks() []string {
	missing := []string{}
	if !g.CriticalTestsPassed {
		missing = append(missing, "critical tests")
	}
	if g.TemplateReplayPassCount < defaultLoopReleaseGateTemplateReplayPasses {
		missing = append(missing, fmt.Sprintf("at least %d template replay passes", defaultLoopReleaseGateTemplateReplayPasses))
	}
	if !g.GovernanceKillSwitchPassed {
		missing = append(missing, "governance kill switch test")
	}
	if !g.QuotaGuardPassed {
		missing = append(missing, "quota guard test")
	}
	return missing
}

type LoopTriggerPolicy struct {
	ScheduleEnabled           bool                  `json:"schedule_enabled"`
	WebhookEnabled            bool                  `json:"webhook_enabled"`
	EvalRepairEnabled         bool                  `json:"eval_repair_enabled"`
	MonitorEnabled            bool                  `json:"monitor_enabled"`
	WebhookAllowedSources     []string              `json:"webhook_allowed_sources,omitempty"`
	ScheduleAllowedTemplates  []string              `json:"schedule_allowed_templates,omitempty"`
	MonitorAllowedTemplates   []string              `json:"monitor_allowed_templates,omitempty"`
	HighRiskReviewPending     bool                  `json:"high_risk_review_pending"`
	ReleaseGate               LoopReleaseGateReport `json:"release_gate"`
	AllowManualWithoutRelease bool                  `json:"allow_manual_without_release"`
}

func DefaultLoopTriggerPolicy() LoopTriggerPolicy {
	return LoopTriggerPolicy{
		ScheduleEnabled:           false,
		WebhookEnabled:            false,
		EvalRepairEnabled:         false,
		MonitorEnabled:            false,
		ScheduleAllowedTemplates:  []string{LoopTemplateResearchReport, LoopTemplateDocGeneration, LoopTemplateWebMonitor, LoopTemplateMemoryRefinement},
		MonitorAllowedTemplates:   []string{LoopTemplateWebMonitor},
		HighRiskReviewPending:     true,
		AllowManualWithoutRelease: true,
	}
}

func normalizeLoopTriggerPolicy(policy LoopTriggerPolicy) LoopTriggerPolicy {
	defaults := DefaultLoopTriggerPolicy()
	if len(policy.ScheduleAllowedTemplates) == 0 {
		policy.ScheduleAllowedTemplates = defaults.ScheduleAllowedTemplates
	}
	if len(policy.MonitorAllowedTemplates) == 0 {
		policy.MonitorAllowedTemplates = defaults.MonitorAllowedTemplates
	}
	policy.WebhookAllowedSources = normalizeLoopPolicyStrings(policy.WebhookAllowedSources)
	policy.ScheduleAllowedTemplates = normalizeLoopPolicyStrings(policy.ScheduleAllowedTemplates)
	policy.MonitorAllowedTemplates = normalizeLoopPolicyStrings(policy.MonitorAllowedTemplates)
	if !policy.HighRiskReviewPending {
		policy.HighRiskReviewPending = defaults.HighRiskReviewPending
	}
	if !policy.AllowManualWithoutRelease {
		policy.AllowManualWithoutRelease = defaults.AllowManualWithoutRelease
	}
	return policy
}

func normalizeLoopPolicyStrings(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func (r *Runtime) SetLoopTriggerPolicy(policy LoopTriggerPolicy) {
	if r == nil {
		return
	}
	r.loopTriggerPolicy = normalizeLoopTriggerPolicy(policy)
}

func (r *Runtime) currentLoopTriggerPolicy() LoopTriggerPolicy {
	if r == nil {
		return DefaultLoopTriggerPolicy()
	}
	return normalizeLoopTriggerPolicy(r.loopTriggerPolicy)
}

func (r *Runtime) checkLoopTriggerPolicy(req LoopTriggerRequest) error {
	policy := r.currentLoopTriggerPolicy()
	triggerType := normalizeLoopTriggerType(req.TriggerType)
	if triggerType == LoopTriggerTypeManual && policy.AllowManualWithoutRelease {
		return nil
	}
	if !policy.ReleaseGate.Passed() {
		return loopTriggerPolicyBlocked("release gate is not passed: " + strings.Join(policy.ReleaseGate.MissingChecks(), ", "))
	}
	switch triggerType {
	case LoopTriggerTypeSchedule:
		if !policy.ScheduleEnabled {
			return loopTriggerPolicyBlocked("schedule trigger is disabled")
		}
		if !stringInSlice(loopTriggerTemplateID(req), policy.ScheduleAllowedTemplates) {
			return loopTriggerPolicyBlocked("schedule trigger template is not allowed")
		}
	case LoopTriggerTypeWebhook:
		if !policy.WebhookEnabled {
			return loopTriggerPolicyBlocked("webhook trigger is disabled")
		}
		if !stringInSlice(strings.ToLower(strings.TrimSpace(req.Source)), policy.WebhookAllowedSources) {
			return loopTriggerPolicyBlocked("webhook source requires configured signing secret")
		}
	case LoopTriggerTypeEval:
		if !policy.EvalRepairEnabled {
			return loopTriggerPolicyBlocked("eval repair trigger is disabled")
		}
		if !loopTriggerEvalRepairScoped(req) {
			return loopTriggerPolicyBlocked("eval repair trigger requires evaluation result scope")
		}
	case LoopTriggerTypeMonitor:
		if !policy.MonitorEnabled {
			return loopTriggerPolicyBlocked("monitor trigger is disabled")
		}
		if !stringInSlice(loopTriggerTemplateID(req), policy.MonitorAllowedTemplates) {
			return loopTriggerPolicyBlocked("monitor trigger must use a read-only monitor template")
		}
		if loopTriggerPayloadBool(req.Payload, "allow_write") || loopTriggerPayloadBool(req.Payload, "allow_side_effects") {
			return loopTriggerPolicyBlocked("monitor trigger must remain read-only")
		}
	}
	return nil
}

func loopTriggerTemplateID(req LoopTriggerRequest) string {
	return normalizeLoopTemplateID(firstNonEmptyString(req.TemplateID, deepAgentWorkflowString(req.Payload, "template_id")))
}

func loopTriggerEvalRepairScoped(req LoopTriggerRequest) bool {
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(req.Source)), "evaluation:") {
		return false
	}
	return strings.TrimSpace(deepAgentWorkflowString(req.Payload, "evaluation_run_id")) != "" &&
		strings.TrimSpace(deepAgentWorkflowString(req.Payload, "evaluation_result_id")) != "" &&
		strings.TrimSpace(deepAgentWorkflowString(req.Payload, "subject_type")) != ""
}

func loopTriggerPayloadBool(payload map[string]any, key string) bool {
	switch strings.ToLower(strings.TrimSpace(deepAgentWorkflowString(payload, key))) {
	case "1", "true", "yes", "y":
		return true
	default:
		return false
	}
}

func loopTriggerPolicyBlocked(reason string) error {
	return fmt.Errorf("%w: %s", ErrLoopTriggerPolicyBlocked, reason)
}
