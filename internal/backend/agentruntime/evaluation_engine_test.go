package agentruntime

import (
	"context"
	"testing"
	"time"

	"claude-codex/internal/harness/state"
)

type staticEvaluationTraceSource struct {
	traces []EvaluationTrace
	err    error
}

func (s staticEvaluationTraceSource) ListEvaluationTraces(context.Context, EvaluationScope) ([]EvaluationTrace, error) {
	if s.err != nil {
		return nil, s.err
	}
	out := make([]EvaluationTrace, len(s.traces))
	copy(out, s.traces)
	return out, nil
}

func TestEvaluationEngineEvaluatesRealTraceMetrics(t *testing.T) {
	start := time.Date(2026, 5, 16, 9, 0, 0, 0, time.UTC)
	finish := start.Add(2 * time.Second)
	now := finish.Add(time.Minute)

	engine := NewEvaluationEngine(staticEvaluationTraceSource{traces: []EvaluationTrace{
		{
			SubjectType: EvaluationSubjectJob,
			SubjectID:   "job-1",
			UserID:      "user-1",
			SessionID:   "session-1",
			JobID:       "job-1",
			Provider:    "openai",
			Model:       "gpt-test",
			Input:       "生成日报",
			Output:      "日报已生成",
			Job: &Job{
				ID:         "job-1",
				UserID:     "user-1",
				SessionID:  "session-1",
				Status:     JobStatusSucceeded,
				Content:    "生成日报",
				CreatedAt:  start,
				UpdatedAt:  finish,
				StartedAt:  &start,
				FinishedAt: &finish,
			},
			Messages: []state.Message{
				{Role: state.MessageRoleUser, Content: "生成日报", CreatedAt: start},
				{Role: state.MessageRoleAssistant, Content: "日报已生成", CreatedAt: finish},
			},
			SkillExecutions: []SkillExecutionRecord{
				{ID: "skill-1", SkillName: "docx", Status: SkillExecutionStatusSucceeded, StartedAt: start, CompletedAt: finish},
			},
			LLMUsage: []LLMUsageRecord{
				{ID: "usage-1", UserID: "user-1", SessionID: "session-1", Provider: "openai", Model: "gpt-test", Status: "success", InputTokens: 10, OutputTokens: 15, TotalTokens: 25, EstimatedCostUSD: 0.000123, CreatedAt: finish},
			},
			Artifacts: []*Artifact{
				{ID: "artifact-1", Kind: AssetKindArtifact, UserID: "user-1", SessionID: "session-1", JobID: "job-1", Filename: "report.docx", CreatedAt: finish},
			},
			CreatedAt:   start,
			CompletedAt: &finish,
		},
	}})
	engine.Now = func() time.Time { return now }

	report, err := engine.Evaluate(context.Background(), EvaluationRunRequest{
		Name:    "daily-report-real-data",
		Trigger: "manual",
		Scope: EvaluationScope{
			SubjectType: EvaluationSubjectJob,
			UserID:      "user-1",
			SessionID:   "session-1",
		},
	})
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	if report.Run.Status != EvaluationRunStatusCompleted {
		t.Fatalf("run status = %q, want completed", report.Run.Status)
	}
	if report.Run.Total != 1 || report.Run.Passed != 1 || report.Run.Failed != 0 || report.Run.Warning != 0 {
		t.Fatalf("unexpected run counters: total=%d passed=%d failed=%d warning=%d", report.Run.Total, report.Run.Passed, report.Run.Failed, report.Run.Warning)
	}
	if report.Run.ThresholdStatus != "" {
		t.Fatalf("threshold status = %q, want empty", report.Run.ThresholdStatus)
	}
	if len(report.Results) != 1 {
		t.Fatalf("result count = %d, want 1", len(report.Results))
	}
	if len(report.Reviews) != 0 {
		t.Fatalf("review count = %d, want 0", len(report.Reviews))
	}
	result := report.Results[0]
	if result.Status != EvaluationResultStatusPassed {
		t.Fatalf("result status = %q, want passed; findings=%v", result.Status, result.Findings)
	}
	if got := result.Metrics["duration_ms"]; got != int64(2000) {
		t.Fatalf("duration_ms = %#v, want int64(2000)", got)
	}
	if got := result.Metrics["llm_requests"]; got != 1 {
		t.Fatalf("llm_requests = %#v, want 1", got)
	}
	if got := result.Metrics["artifact_count"]; got != 1 {
		t.Fatalf("artifact_count = %#v, want 1", got)
	}
	if report.Summary.PassRate != 1 {
		t.Fatalf("summary pass rate = %v, want 1", report.Summary.PassRate)
	}
}

func TestEvaluationEngineFlagsFailuresAndAggregates(t *testing.T) {
	start := time.Date(2026, 5, 16, 10, 0, 0, 0, time.UTC)
	finish := start.Add(5 * time.Second)

	engine := NewEvaluationEngine(staticEvaluationTraceSource{traces: []EvaluationTrace{
		{
			SubjectType: EvaluationSubjectJob,
			SubjectID:   "job-failed",
			UserID:      "user-1",
			SessionID:   "session-1",
			JobID:       "job-failed",
			Input:       "执行任务",
			Output:      "error: tool failed",
			Job: &Job{
				ID:         "job-failed",
				UserID:     "user-1",
				SessionID:  "session-1",
				Status:     JobStatusFailed,
				Error:      "tool failed",
				CreatedAt:  start,
				UpdatedAt:  finish,
				StartedAt:  &start,
				FinishedAt: &finish,
			},
			Messages: []state.Message{
				{Role: state.MessageRoleAssistant, Content: "error: tool failed", CreatedAt: finish},
			},
			JobEvents: []*JobEvent{
				{ID: "event-1", JobID: "job-failed", UserID: "user-1", SessionID: "session-1", Type: "error", Event: Event{Type: "error", Error: "tool failed"}, CreatedAt: finish},
			},
			SkillExecutions: []SkillExecutionRecord{
				{ID: "skill-failed", SkillName: "docx", Status: SkillExecutionStatusFailed, Error: "missing artifact", StartedAt: start, CompletedAt: finish},
			},
			LLMUsage: []LLMUsageRecord{
				{ID: "usage-failed", UserID: "user-1", SessionID: "session-1", Provider: "openai", Model: "gpt-test", Status: "failed", CreatedAt: finish},
			},
			RiskEvents: []RiskEvent{
				{ID: "risk-1", UserID: "user-1", SessionID: "session-1", JobID: "job-failed", Operation: RiskOperationJobCreate, Reason: "blocked", RiskLevel: RiskLevelHigh, CreatedAt: finish},
			},
			CreatedAt:   start,
			CompletedAt: &finish,
		},
		{
			SubjectType: EvaluationSubjectJob,
			SubjectID:   "job-running",
			UserID:      "user-1",
			SessionID:   "session-1",
			JobID:       "job-running",
			Job: &Job{
				ID:        "job-running",
				UserID:    "user-1",
				SessionID: "session-1",
				Status:    JobStatusRunning,
				CreatedAt: finish,
				UpdatedAt: finish,
			},
			CreatedAt: finish,
		},
	}})

	report, err := engine.Evaluate(context.Background(), EvaluationRunRequest{
		Scope: EvaluationScope{SubjectType: EvaluationSubjectJob, UserID: "user-1"},
	})
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	if report.Run.Total != 2 || report.Run.Failed != 1 || report.Run.Warning != 1 || report.Run.Passed != 0 {
		t.Fatalf("unexpected run counters: total=%d passed=%d failed=%d warning=%d", report.Run.Total, report.Run.Passed, report.Run.Failed, report.Run.Warning)
	}
	if report.Run.ThresholdStatus != "" {
		t.Fatalf("threshold status = %q, want empty", report.Run.ThresholdStatus)
	}
	if len(report.Results) != 2 {
		t.Fatalf("result count = %d, want 2", len(report.Results))
	}
	if len(report.Reviews) != 1 {
		t.Fatalf("review count = %d, want 1", len(report.Reviews))
	}
	if report.Reviews[0].ResultID != report.Results[0].ID || report.Reviews[0].Status != EvaluationReviewStatusPending {
		t.Fatalf("unexpected review: %+v", report.Reviews[0])
	}
	if report.Results[0].Status != EvaluationResultStatusFailed {
		t.Fatalf("first result status = %q, want failed; findings=%v", report.Results[0].Status, report.Results[0].Findings)
	}
	if report.Results[1].Status != EvaluationResultStatusWarning {
		t.Fatalf("second result status = %q, want warning; findings=%v", report.Results[1].Status, report.Results[1].Findings)
	}
	if got := report.Run.Metrics["high_risk_count"]; got != 1 {
		t.Fatalf("high_risk_count = %#v, want 1", got)
	}
	if got := report.Run.Metrics["empty_output_count"]; got != 1 {
		t.Fatalf("empty_output_count = %#v, want 1", got)
	}
	if report.Summary.PassRate != 0 || report.Summary.FailureRate != 0.5 || report.Summary.WarningRate != 0.5 {
		t.Fatalf("unexpected rates: pass=%v failure=%v warning=%v", report.Summary.PassRate, report.Summary.FailureRate, report.Summary.WarningRate)
	}
}
