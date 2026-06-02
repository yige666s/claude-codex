package agentruntime

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type EvaluationRunRequest struct {
	ID         string               `json:"id,omitempty"`
	Name       string               `json:"name,omitempty"`
	Trigger    string               `json:"trigger,omitempty"`
	Scope      EvaluationScope      `json:"scope"`
	Thresholds EvaluationThresholds `json:"thresholds,omitempty"`
}

type EvaluationRunReport struct {
	Run     EvaluationRun        `json:"run"`
	Results []EvaluationResult   `json:"results"`
	Reviews []EvaluationReview   `json:"reviews,omitempty"`
	Summary EvaluationRunSummary `json:"summary"`
}

type EvaluationEngine struct {
	Source EvaluationTraceSource
	Judge  GoldenJudge
	Now    func() time.Time
}

func NewEvaluationEngine(source EvaluationTraceSource) *EvaluationEngine {
	return &EvaluationEngine{Source: source}
}

func (e *EvaluationEngine) Evaluate(ctx context.Context, req EvaluationRunRequest) (EvaluationRunReport, error) {
	if e == nil || e.Source == nil {
		return EvaluationRunReport{}, fmt.Errorf("evaluation trace source is required")
	}
	now := e.now()
	scope := normalizeEvaluationScope(req.Scope)
	run := normalizeEvaluationRun(EvaluationRun{
		ID:        strings.TrimSpace(req.ID),
		Name:      firstNonEmptyString(strings.TrimSpace(req.Name), defaultEvaluationRunName(scope, now)),
		Status:    EvaluationRunStatusRunning,
		Trigger:   strings.TrimSpace(req.Trigger),
		Scope:     scope,
		StartedAt: now,
	})
	traces, err := e.Source.ListEvaluationTraces(ctx, scope)
	if err != nil {
		failedAt := e.now()
		run.Status = EvaluationRunStatusFailed
		run.CompletedAt = &failedAt
		run.Summary = err.Error()
		return EvaluationRunReport{Run: run}, err
	}
	results := make([]EvaluationResult, 0, len(traces))
	for _, trace := range traces {
		results = append(results, evaluateTraceResult(run.ID, trace))
	}
	aggregate := aggregateEvaluationMetrics(results)
	completedAt := e.now()
	run.Status = EvaluationRunStatusCompleted
	run.CompletedAt = &completedAt
	run.Total = aggregate.Total
	run.Passed = aggregate.Passed
	run.Failed = aggregate.Failed
	run.Warning = aggregate.Warning
	run.Metrics = evaluationAggregateMetricsMap(aggregate)
	run.Summary = evaluationSummaryText(aggregate)

	summary := summarizeEvaluationResults(run, results)
	return EvaluationRunReport{
		Run:     run,
		Results: results,
		Reviews: createEvaluationReviews(results),
		Summary: summary,
	}, nil
}

func createEvaluationReviews(results []EvaluationResult) []EvaluationReview {
	reviews := make([]EvaluationReview, 0)
	for _, result := range results {
		if !evaluationResultNeedsReview(result) {
			continue
		}
		review := normalizeEvaluationReview(EvaluationReview{
			ResultID: result.ID,
			Status:   EvaluationReviewStatusPending,
			Note:     evaluationReviewNote(result),
		})
		reviews = append(reviews, review)
	}
	return reviews
}

func evaluationResultNeedsReview(result EvaluationResult) bool {
	if result.Status == EvaluationResultStatusFailed {
		return true
	}
	for _, finding := range result.Findings {
		if finding.Severity == "error" || finding.Code == "high_risk_events" {
			return true
		}
	}
	return false
}

func evaluationReviewNote(result EvaluationResult) string {
	codes := make([]string, 0, len(result.Findings))
	for _, finding := range result.Findings {
		if strings.TrimSpace(finding.Code) == "" {
			continue
		}
		codes = append(codes, finding.Code)
	}
	if len(codes) == 0 {
		return "自动评估失败，需要人工复核。"
	}
	return "自动评估失败，需要人工复核：" + strings.Join(codes, ", ")
}

func evaluateTraceResult(runID string, trace EvaluationTrace) EvaluationResult {
	metrics := calculateTraceMetrics(trace)
	findings := evaluateTraceFindings(trace, metrics)
	status := evaluationStatusFromFindings(findings)
	return normalizeEvaluationResult(EvaluationResult{
		RunID:       runID,
		SubjectType: trace.SubjectType,
		SubjectID:   trace.SubjectID,
		UserID:      trace.UserID,
		SessionID:   trace.SessionID,
		JobID:       trace.JobID,
		SkillName:   trace.SkillName,
		Provider:    trace.Provider,
		Model:       trace.Model,
		Status:      status,
		Score:       evaluationScoreFromStatus(status),
		Input:       trace.Input,
		Output:      trace.Output,
		Metrics:     evaluationTraceMetricsMap(metrics),
		Findings:    findings,
		CreatedAt:   firstNonZeroTime(trace.CompletedAt, trace.CreatedAt, time.Now().UTC()),
	})
}

func (e *EvaluationEngine) now() time.Time {
	if e != nil && e.Now != nil {
		return e.Now().UTC()
	}
	return time.Now().UTC()
}

func defaultEvaluationRunName(scope EvaluationScope, now time.Time) string {
	subject := scope.SubjectType
	if subject == "" {
		subject = EvaluationSubjectJob
	}
	return subject + "_quality_" + now.UTC().Format("20060102T150405Z")
}

func evaluationSummaryText(metrics EvaluationAggregateMetrics) string {
	if metrics.Total == 0 {
		return "No matching real runtime records found for this evaluation scope."
	}
	return fmt.Sprintf(
		"Evaluated %d real runtime record(s): pass_rate=%.2f failed=%d warning=%d tool_errors=%d llm_failures=%d high_risk=%d p95_latency_ms=%d cost_usd=%.6f",
		metrics.Total,
		metrics.SuccessRate,
		metrics.Failed,
		metrics.Warning,
		metrics.ToolErrorCount,
		metrics.LLMFailures,
		metrics.HighRiskCount,
		metrics.P95LatencyMS,
		metrics.EstimatedCostUSD,
	)
}

func firstNonZeroTime(values ...any) time.Time {
	for _, value := range values {
		switch typed := value.(type) {
		case *time.Time:
			if typed != nil && !typed.IsZero() {
				return typed.UTC()
			}
		case time.Time:
			if !typed.IsZero() {
				return typed.UTC()
			}
		}
	}
	return time.Time{}
}
