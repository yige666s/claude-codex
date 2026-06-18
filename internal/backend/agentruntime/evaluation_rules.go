package agentruntime

import (
	"fmt"
	"strings"
)

type EvaluationThresholds struct {
	MinSuccessRate     float64 `json:"min_success_rate,omitempty"`
	MaxToolErrorRate   float64 `json:"max_tool_error_rate,omitempty"`
	MaxLLMErrorRate    float64 `json:"max_llm_error_rate,omitempty"`
	MaxHighRiskCount   int     `json:"max_high_risk_count,omitempty"`
	MaxP95LatencyMS    int64   `json:"max_p95_latency_ms,omitempty"`
	MaxCostUSD         float64 `json:"max_cost_usd,omitempty"`
	MaxEmptyOutputRate float64 `json:"max_empty_output_rate,omitempty"`
}

func evaluateTraceFindings(trace EvaluationTrace, metrics EvaluationTraceMetrics) []EvaluationFinding {
	findings := make([]EvaluationFinding, 0)
	if trace.Job != nil {
		switch trace.Job.Status {
		case JobStatusFailed:
			findings = append(findings, EvaluationFinding{
				Severity: "error",
				Code:     "job_failed",
				Message:  firstNonEmptyString(trace.Job.Error, "job failed"),
			})
		case JobStatusCancelled:
			findings = append(findings, EvaluationFinding{
				Severity: "warning",
				Code:     "job_cancelled",
				Message:  firstNonEmptyString(trace.Job.Error, "job was cancelled"),
			})
		case JobStatusQueued, JobStatusRunning:
			findings = append(findings, EvaluationFinding{
				Severity: "warning",
				Code:     "job_incomplete",
				Message:  "job is not terminal yet",
			})
		}
	}
	if metrics.EmptyOutput {
		findings = append(findings, EvaluationFinding{
			Severity: "warning",
			Code:     "empty_output",
			Message:  "no visible assistant output was found",
		})
	}
	if trace.SubjectType == EvaluationSubjectDeepAgent {
		findings = append(findings, evaluateDeepAgentTraceFindings(trace, metrics)...)
	}
	if metrics.ToolErrorCount > 0 {
		findings = append(findings, EvaluationFinding{
			Severity: "error",
			Code:     "tool_errors",
			Message:  fmt.Sprintf("%d tool error signal(s) found", metrics.ToolErrorCount),
			Metadata: map[string]any{"count": metrics.ToolErrorCount},
		})
	}
	if metrics.SkillFailureCount > 0 {
		findings = append(findings, EvaluationFinding{
			Severity: "error",
			Code:     "skill_failures",
			Message:  fmt.Sprintf("%d skill execution failure(s) found", metrics.SkillFailureCount),
			Metadata: map[string]any{"count": metrics.SkillFailureCount},
		})
	}
	if metrics.StructuredOutputErrorCount > 0 {
		findings = append(findings, EvaluationFinding{
			Severity: "warning",
			Code:     "structured_output_errors",
			Message:  fmt.Sprintf("%d structured output validation error(s) found", metrics.StructuredOutputErrorCount),
			Metadata: map[string]any{
				"count":           metrics.StructuredOutputErrorCount,
				"repair_attempts": metrics.StructuredOutputRepairAttemptCount,
				"repair_success":  metrics.StructuredOutputRepairSuccessCount,
			},
		})
	}
	if metrics.StructuredOutputFallbackCount > 0 {
		findings = append(findings, EvaluationFinding{
			Severity: "warning",
			Code:     "structured_output_fallbacks",
			Message:  fmt.Sprintf("%d structured output fallback(s) used", metrics.StructuredOutputFallbackCount),
			Metadata: map[string]any{
				"count":  metrics.StructuredOutputFallbackCount,
				"levels": metrics.StructuredOutputFallbackLevels,
			},
		})
	}
	if metrics.LLMFailures > 0 {
		findings = append(findings, EvaluationFinding{
			Severity: "warning",
			Code:     "llm_failures",
			Message:  fmt.Sprintf("%d LLM failure(s) found", metrics.LLMFailures),
			Metadata: map[string]any{"count": metrics.LLMFailures},
		})
	}
	if metrics.RiskHighCount > 0 {
		findings = append(findings, EvaluationFinding{
			Severity: "error",
			Code:     "high_risk_events",
			Message:  fmt.Sprintf("%d high risk event(s) found", metrics.RiskHighCount),
			Metadata: map[string]any{"count": metrics.RiskHighCount},
		})
	}
	if metrics.RiskMediumCount > 0 {
		findings = append(findings, EvaluationFinding{
			Severity: "warning",
			Code:     "medium_risk_events",
			Message:  fmt.Sprintf("%d medium risk event(s) found", metrics.RiskMediumCount),
			Metadata: map[string]any{"count": metrics.RiskMediumCount},
		})
	}
	if looksLikeAssistantError(trace.Output) {
		findings = append(findings, EvaluationFinding{
			Severity: "warning",
			Code:     "assistant_error_output",
			Message:  "assistant output appears to contain an error message",
		})
	}
	return normalizeEvaluationFindings(findings)
}

func evaluateDeepAgentTraceFindings(trace EvaluationTrace, metrics EvaluationTraceMetrics) []EvaluationFinding {
	findings := make([]EvaluationFinding, 0)
	switch metrics.DeepAgentFinalStatus {
	case DeepAgentRunStatusSucceeded:
	case DeepAgentRunStatusBlocked, DeepAgentRunStatusFailed, DeepAgentRunStatusBudgetExceeded, DeepAgentRunStatusReviewPending:
		findings = append(findings, EvaluationFinding{
			Severity: "error",
			Code:     "deep_agent_not_succeeded",
			Message:  firstNonEmptyString(metrics.DeepAgentBlockedReason, "DeepAgent run did not succeed"),
			Metadata: map[string]any{
				"final_status": metrics.DeepAgentFinalStatus,
				"task_type":    metrics.DeepAgentTaskType,
			},
		})
	}
	if metrics.DeepAgentVerifierFailed > 0 {
		findings = append(findings, EvaluationFinding{
			Severity: "error",
			Code:     "deep_agent_verifier_failed",
			Message:  fmt.Sprintf("%d DeepAgent verifier check(s) failed", metrics.DeepAgentVerifierFailed),
			Metadata: map[string]any{"count": metrics.DeepAgentVerifierFailed},
		})
	}
	if metrics.DeepAgentNoProgressCount > 0 {
		findings = append(findings, EvaluationFinding{
			Severity: "warning",
			Code:     "deep_agent_no_progress",
			Message:  fmt.Sprintf("DeepAgent no-progress count reached %d", metrics.DeepAgentNoProgressCount),
			Metadata: map[string]any{"count": metrics.DeepAgentNoProgressCount},
		})
	}
	if trace.DeepAgent != nil && trace.DeepAgent.Governance.PolicyBlocked {
		findings = append(findings, EvaluationFinding{
			Severity: "error",
			Code:     "deep_agent_policy_blocked",
			Message:  firstNonEmptyString(trace.DeepAgent.Governance.PolicyBlockReason, "DeepAgent governance policy blocked the action"),
		})
	}
	return findings
}

func evaluationStatusFromFindings(findings []EvaluationFinding) string {
	status := EvaluationResultStatusPassed
	for _, finding := range findings {
		switch finding.Severity {
		case "error":
			return EvaluationResultStatusFailed
		case "warning":
			status = EvaluationResultStatusWarning
		}
	}
	return status
}

func evaluationScoreFromStatus(status string) float64 {
	switch status {
	case EvaluationResultStatusPassed:
		return 1
	case EvaluationResultStatusWarning:
		return 0.5
	default:
		return 0
	}
}

func looksLikeAssistantError(output string) bool {
	text := strings.ToLower(strings.TrimSpace(output))
	if text == "" {
		return false
	}
	for _, marker := range []string{
		"request failed:",
		"error:",
		"permission denied",
		"daily llm",
		"timed out",
		"rate limit",
	} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}
