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

func evaluateThresholdStatus(metrics EvaluationAggregateMetrics, thresholds EvaluationThresholds) string {
	if thresholds == (EvaluationThresholds{}) {
		return ""
	}
	var failed bool
	var warning bool
	if thresholds.MinSuccessRate > 0 && metrics.SuccessRate < thresholds.MinSuccessRate {
		failed = true
	}
	if thresholds.MaxToolErrorRate > 0 && metrics.ToolErrorRate > thresholds.MaxToolErrorRate {
		failed = true
	}
	if thresholds.MaxLLMErrorRate > 0 && metrics.LLMErrorRate > thresholds.MaxLLMErrorRate {
		warning = true
	}
	if thresholds.MaxHighRiskCount >= 0 && metrics.HighRiskCount > thresholds.MaxHighRiskCount {
		failed = true
	}
	if thresholds.MaxP95LatencyMS > 0 && metrics.P95LatencyMS > thresholds.MaxP95LatencyMS {
		warning = true
	}
	if thresholds.MaxCostUSD > 0 && metrics.EstimatedCostUSD > thresholds.MaxCostUSD {
		warning = true
	}
	if thresholds.MaxEmptyOutputRate > 0 && metrics.Total > 0 {
		emptyRate := float64(metrics.EmptyOutputCount) / float64(metrics.Total)
		if emptyRate > thresholds.MaxEmptyOutputRate {
			warning = true
		}
	}
	switch {
	case failed:
		return "failed"
	case warning:
		return "warning"
	default:
		return "passed"
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
