package agentruntime

import (
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

func (s *Server) handleAdminOpsEval(w http.ResponseWriter, r *http.Request, actor User, parts []string) {
	if s.evaluation == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "evaluation store is not configured"})
		return
	}
	switch {
	case r.Method == http.MethodPost && len(parts) == 5 && parts[4] == "runs":
		s.handleAdminOpsCreateEvaluationRun(w, r, actor)
	case r.Method == http.MethodGet && len(parts) == 5 && parts[4] == "runs":
		s.handleAdminOpsListEvaluationRuns(w, r)
	case r.Method == http.MethodGet && len(parts) == 6 && parts[4] == "runs":
		s.handleAdminOpsGetEvaluationRun(w, r, parts[5])
	case r.Method == http.MethodGet && len(parts) == 5 && parts[4] == "results":
		s.handleAdminOpsListEvaluationResults(w, r)
	case r.Method == http.MethodGet && len(parts) == 5 && parts[4] == "reviews":
		s.handleAdminOpsListEvaluationReviews(w, r)
	case r.Method == http.MethodPatch && len(parts) == 6 && parts[4] == "reviews":
		s.handleAdminOpsUpdateEvaluationReview(w, r, actor, parts[5])
	case r.Method == http.MethodGet && len(parts) == 5 && parts[4] == "summary":
		s.handleAdminOpsEvaluationSummary(w, r)
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	}
}

func (s *Server) handleAdminOpsCreateEvaluationRun(w http.ResponseWriter, r *http.Request, actor User) {
	var req EvaluationRunRequest
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	req.Scope = normalizeEvaluationScope(req.Scope)
	if strings.TrimSpace(req.Scope.UserID) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "scope.user_id is required"})
		return
	}
	engine := NewEvaluationEngine(RuntimeEvaluationTraceSource{
		Runtime:  s.runtime,
		LLMUsage: s.llmUsage,
		Risk:     s.risk,
	})
	report, err := engine.Evaluate(r.Context(), req)
	if err != nil {
		if strings.Contains(err.Error(), " is required") {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	persisted, err := s.persistEvaluationRunReport(r, report)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.auditEvent(r, "admin_eval_run_create", actor, map[string]any{
		"eval_run_id":    persisted.Run.ID,
		"target_user_id": persisted.Run.Scope.UserID,
		"subject_type":   persisted.Run.Scope.SubjectType,
		"total":          persisted.Run.Total,
		"passed":         persisted.Run.Passed,
		"failed":         persisted.Run.Failed,
		"warning":        persisted.Run.Warning,
	})
	writeJSON(w, http.StatusCreated, map[string]any{
		"run":     persisted.Run,
		"results": persisted.Results,
		"reviews": persisted.Reviews,
		"summary": persisted.Summary,
	})
}

func (s *Server) persistEvaluationRunReport(r *http.Request, report EvaluationRunReport) (EvaluationRunReport, error) {
	ctx := context.Background()
	if r != nil {
		ctx = r.Context()
	}
	return s.persistEvaluationRunReportContext(ctx, report)
}

func (s *Server) persistEvaluationRunReportContext(ctx context.Context, report EvaluationRunReport) (EvaluationRunReport, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	run, err := s.evaluation.CreateEvaluationRun(ctx, report.Run)
	if err != nil {
		return EvaluationRunReport{}, err
	}
	results := make([]EvaluationResult, 0, len(report.Results))
	resultIDByOriginal := make(map[string]string, len(report.Results))
	for _, result := range report.Results {
		originalID := result.ID
		result.RunID = run.ID
		created, err := s.evaluation.CreateEvaluationResult(ctx, result)
		if err != nil {
			return EvaluationRunReport{}, err
		}
		results = append(results, created)
		resultIDByOriginal[originalID] = created.ID
		resultIDByOriginal[created.ID] = created.ID
	}
	reviews := make([]EvaluationReview, 0, len(report.Reviews))
	for _, review := range report.Reviews {
		if mapped := resultIDByOriginal[review.ResultID]; mapped != "" {
			review.ResultID = mapped
		}
		created, err := s.evaluation.CreateEvaluationReview(ctx, review)
		if err != nil {
			return EvaluationRunReport{}, err
		}
		reviews = append(reviews, created)
	}
	summary, err := s.evaluation.SummarizeEvaluationRun(ctx, run.ID)
	if err != nil {
		return EvaluationRunReport{}, err
	}
	return EvaluationRunReport{
		Run:     run,
		Results: results,
		Reviews: reviews,
		Summary: summary,
	}, nil
}

func (s *Server) handleAdminOpsListEvaluationRuns(w http.ResponseWriter, r *http.Request) {
	filter := EvaluationRunFilter{
		Status:  strings.TrimSpace(r.URL.Query().Get("status")),
		Trigger: strings.TrimSpace(r.URL.Query().Get("trigger")),
		Limit:   parseBoundedInt(r.URL.Query().Get("limit"), 100, 1, 500),
	}
	runs, err := s.evaluation.ListEvaluationRuns(r.Context(), filter)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"runs": runs})
}

func (s *Server) handleAdminOpsGetEvaluationRun(w http.ResponseWriter, r *http.Request, runID string) {
	run, err := s.evaluation.GetEvaluationRun(r.Context(), runID)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
		}
		writeJSON(w, status, map[string]string{"error": "evaluation run not found"})
		return
	}
	results, err := s.evaluation.ListEvaluationResults(r.Context(), EvaluationResultFilter{
		RunID: strings.TrimSpace(run.ID),
		Limit: parseBoundedInt(r.URL.Query().Get("limit"), 500, 1, 2000),
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	reviews, err := evaluationReviewsForResults(r.Context(), s.evaluation, results)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	summary, err := s.evaluation.SummarizeEvaluationRun(r.Context(), run.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"run":     run,
		"results": results,
		"reviews": reviews,
		"summary": summary,
	})
}

func (s *Server) handleAdminOpsListEvaluationResults(w http.ResponseWriter, r *http.Request) {
	filter := evaluationResultFilterFromRequest(r)
	results, err := s.evaluation.ListEvaluationResults(r.Context(), filter)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	format := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("format")))
	if format == "csv" {
		writeEvaluationResultsCSV(w, results)
		return
	}
	if format != "" && format != "json" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported evaluation results format"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": results})
}

func (s *Server) handleAdminOpsListEvaluationReviews(w http.ResponseWriter, r *http.Request) {
	filter := EvaluationReviewFilter{
		ResultID: strings.TrimSpace(r.URL.Query().Get("result_id")),
		Status:   strings.TrimSpace(r.URL.Query().Get("status")),
		Limit:    parseBoundedInt(r.URL.Query().Get("limit"), 100, 1, 500),
	}
	reviews, err := s.evaluation.ListEvaluationReviews(r.Context(), filter)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"reviews": reviews})
}

func (s *Server) handleAdminOpsUpdateEvaluationReview(w http.ResponseWriter, r *http.Request, actor User, reviewID string) {
	var body struct {
		Status   string `json:"status"`
		Reviewer string `json:"reviewer"`
		Note     string `json:"note"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	reviewer := firstNonEmptyString(body.Reviewer, actor.ID)
	review, err := s.evaluation.UpdateEvaluationReview(r.Context(), EvaluationReview{
		ID:       reviewID,
		Status:   body.Status,
		Reviewer: reviewer,
		Note:     body.Note,
	})
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
		}
		writeJSON(w, status, map[string]string{"error": "evaluation review not found"})
		return
	}
	s.auditEvent(r, "admin_eval_review_update", actor, map[string]any{
		"eval_review_id": review.ID,
		"eval_result_id": review.ResultID,
		"status":         review.Status,
		"reviewer":       review.Reviewer,
	})
	writeJSON(w, http.StatusOK, map[string]any{"review": review})
}

func (s *Server) handleAdminOpsEvaluationSummary(w http.ResponseWriter, r *http.Request) {
	runs, err := s.evaluation.ListEvaluationRuns(r.Context(), EvaluationRunFilter{
		Status:  strings.TrimSpace(r.URL.Query().Get("status")),
		Trigger: strings.TrimSpace(r.URL.Query().Get("trigger")),
		Limit:   parseBoundedInt(r.URL.Query().Get("limit"), 500, 1, 2000),
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	from, ok := optionalEvaluationTimeQuery(w, r, "from")
	if !ok {
		return
	}
	to, ok := optionalEvaluationTimeQuery(w, r, "to")
	if !ok {
		return
	}
	summary := EvaluationRunSummary{Metrics: map[string]any{}}
	filtered := make([]EvaluationRun, 0, len(runs))
	for _, run := range runs {
		if !evaluationRunInTimeRange(run, from, to) {
			continue
		}
		filtered = append(filtered, run)
		summary.Total += run.Total
		summary.Passed += run.Passed
		summary.Failed += run.Failed
		summary.Warning += run.Warning
	}
	if summary.Total > 0 {
		total := float64(summary.Total)
		summary.PassRate = float64(summary.Passed) / total
		summary.FailureRate = float64(summary.Failed) / total
		summary.WarningRate = float64(summary.Warning) / total
	}
	summary.Metrics = evaluationSummaryMetricsForRuns(filtered)
	format := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("format")))
	if format == "markdown" || format == "md" {
		writeEvaluationSummaryMarkdown(w, summary, filtered, from, to)
		return
	}
	if format != "" && format != "json" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported evaluation summary format"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"summary": summary, "runs": filtered})
}

func evaluationResultFilterFromRequest(r *http.Request) EvaluationResultFilter {
	query := r.URL.Query()
	return EvaluationResultFilter{
		RunID:       strings.TrimSpace(query.Get("run_id")),
		Status:      strings.TrimSpace(query.Get("status")),
		SubjectType: strings.TrimSpace(query.Get("subject_type")),
		UserID:      strings.TrimSpace(query.Get("user_id")),
		SessionID:   strings.TrimSpace(query.Get("session_id")),
		JobID:       strings.TrimSpace(query.Get("job_id")),
		SkillName:   strings.TrimSpace(query.Get("skill_name")),
		Provider:    strings.TrimSpace(query.Get("provider")),
		Model:       strings.TrimSpace(query.Get("model")),
		Limit:       parseBoundedInt(query.Get("limit"), 200, 1, 1000),
	}
}

func evaluationReviewsForResults(ctx context.Context, store EvaluationStore, results []EvaluationResult) ([]EvaluationReview, error) {
	reviews := make([]EvaluationReview, 0)
	for _, result := range results {
		items, err := store.ListEvaluationReviews(ctx, EvaluationReviewFilter{ResultID: result.ID, Limit: 100})
		if err != nil {
			return nil, err
		}
		reviews = append(reviews, items...)
	}
	return reviews, nil
}

func optionalEvaluationTimeQuery(w http.ResponseWriter, r *http.Request, name string) (*time.Time, bool) {
	value := strings.TrimSpace(r.URL.Query().Get(name))
	if value == "" {
		return nil, true
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid " + name})
		return nil, false
	}
	parsed = parsed.UTC()
	return &parsed, true
}

func evaluationRunInTimeRange(run EvaluationRun, from, to *time.Time) bool {
	at := run.StartedAt
	if run.CompletedAt != nil && !run.CompletedAt.IsZero() {
		at = *run.CompletedAt
	}
	if from != nil && at.Before(*from) {
		return false
	}
	if to != nil && !at.Before(*to) {
		return false
	}
	return true
}

func evaluationSummaryMetricsForRuns(runs []EvaluationRun) map[string]any {
	out := map[string]any{
		"run_count": len(runs),
	}
	var averageLatencyTotal float64
	var averageLatencyWeight float64
	for _, run := range runs {
		for key, value := range run.Metrics {
			switch key {
			case "tool_call_count", "tool_error_count", "skill_count", "skill_failure_count", "llm_requests", "llm_failures", "input_tokens", "output_tokens", "total_tokens", "high_risk_count", "medium_risk_count", "low_risk_count", "artifact_count", "empty_output_count":
				out[key] = mapFloat(out, key) + mapFloat(map[string]any{key: value}, key)
			case "p95_latency_ms":
				if next := mapFloat(map[string]any{key: value}, key); next > mapFloat(out, key) {
					out[key] = next
				}
			case "estimated_cost_usd":
				out[key] = roundEvaluationCost(mapFloat(out, key) + mapFloat(map[string]any{key: value}, key))
			case "average_latency_ms":
				latency := mapFloat(map[string]any{key: value}, key)
				if latency > 0 && run.Total > 0 {
					averageLatencyTotal += latency * float64(run.Total)
					averageLatencyWeight += float64(run.Total)
				}
			}
		}
	}
	if averageLatencyWeight > 0 {
		out["average_latency_ms"] = averageLatencyTotal / averageLatencyWeight
	}
	if total := mapFloat(out, "tool_call_count"); total > 0 {
		out["tool_error_rate"] = mapFloat(out, "tool_error_count") / total
	}
	if total := mapFloat(out, "llm_requests"); total > 0 {
		out["llm_error_rate"] = mapFloat(out, "llm_failures") / total
	}
	totalResults := 0.0
	for _, run := range runs {
		totalResults += float64(run.Total)
	}
	if totalResults > 0 {
		out["empty_output_rate"] = mapFloat(out, "empty_output_count") / totalResults
	}
	return out
}

func writeEvaluationResultsCSV(w http.ResponseWriter, results []EvaluationResult) {
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="evaluation-results.csv"`)
	writer := csv.NewWriter(w)
	_ = writer.Write([]string{
		"id", "run_id", "subject_type", "subject_id", "user_id", "session_id", "job_id",
		"skill_name", "provider", "model", "status", "score", "findings", "metrics", "created_at",
	})
	for _, result := range results {
		_ = writer.Write([]string{
			result.ID,
			result.RunID,
			result.SubjectType,
			result.SubjectID,
			result.UserID,
			result.SessionID,
			result.JobID,
			result.SkillName,
			result.Provider,
			result.Model,
			result.Status,
			fmt.Sprintf("%.3f", result.Score),
			evaluationJSONColumn(result.Findings),
			evaluationJSONColumn(result.Metrics),
			result.CreatedAt.UTC().Format(time.RFC3339),
		})
	}
	writer.Flush()
}

func writeEvaluationSummaryMarkdown(w http.ResponseWriter, summary EvaluationRunSummary, runs []EvaluationRun, from, to *time.Time) {
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="evaluation-summary.md"`)
	var b strings.Builder
	b.WriteString("# Agent Evaluation Summary\n\n")
	b.WriteString("- Window: ")
	b.WriteString(evaluationReportTimeWindow(from, to))
	b.WriteString("\n")
	b.WriteString(fmt.Sprintf("- Runs: %d\n", len(runs)))
	b.WriteString(fmt.Sprintf("- Results: %d total, %d passed, %d failed, %d warning\n\n", summary.Total, summary.Passed, summary.Failed, summary.Warning))
	b.WriteString("| Metric | Value |\n| --- | --- |\n")
	evaluationMarkdownRow(&b, "Pass rate", fmt.Sprintf("%.2f%%", summary.PassRate*100))
	evaluationMarkdownRow(&b, "Failure rate", fmt.Sprintf("%.2f%%", summary.FailureRate*100))
	evaluationMarkdownRow(&b, "Warning rate", fmt.Sprintf("%.2f%%", summary.WarningRate*100))
	evaluationMarkdownRow(&b, "Tool errors", fmt.Sprintf("%.0f", mapFloat(summary.Metrics, "tool_error_count")))
	evaluationMarkdownRow(&b, "Tool failure rate", fmt.Sprintf("%.2f%%", mapFloat(summary.Metrics, "tool_error_rate")*100))
	evaluationMarkdownRow(&b, "LLM failures", fmt.Sprintf("%.0f", mapFloat(summary.Metrics, "llm_failures")))
	evaluationMarkdownRow(&b, "LLM failure rate", fmt.Sprintf("%.2f%%", mapFloat(summary.Metrics, "llm_error_rate")*100))
	evaluationMarkdownRow(&b, "Average latency ms", fmt.Sprintf("%.0f", mapFloat(summary.Metrics, "average_latency_ms")))
	evaluationMarkdownRow(&b, "P95 latency ms", fmt.Sprintf("%.0f", mapFloat(summary.Metrics, "p95_latency_ms")))
	evaluationMarkdownRow(&b, "Total tokens", fmt.Sprintf("%.0f", mapFloat(summary.Metrics, "total_tokens")))
	evaluationMarkdownRow(&b, "Estimated cost USD", fmt.Sprintf("%.6f", mapFloat(summary.Metrics, "estimated_cost_usd")))
	b.WriteString("\n## Runs\n\n")
	b.WriteString("| Run | Status | Pass Rate | Failed | Warning | Completed |\n| --- | --- | --- | --- | --- | --- |\n")
	for _, run := range runs {
		passRate := 0.0
		if run.Total > 0 {
			passRate = float64(run.Passed) / float64(run.Total)
		}
		completedAt := ""
		if run.CompletedAt != nil && !run.CompletedAt.IsZero() {
			completedAt = run.CompletedAt.UTC().Format(time.RFC3339)
		}
		evaluationMarkdownRow(&b,
			run.ID,
			run.Status,
			fmt.Sprintf("%.2f%%", passRate*100),
			fmt.Sprintf("%d", run.Failed),
			fmt.Sprintf("%d", run.Warning),
			completedAt,
		)
	}
	_, _ = w.Write([]byte(b.String()))
}

func evaluationJSONColumn(value any) string {
	if value == nil {
		return ""
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	if string(raw) == "null" {
		return ""
	}
	return string(raw)
}

func evaluationReportTimeWindow(from, to *time.Time) string {
	left := "unbounded"
	right := "now"
	if from != nil {
		left = from.UTC().Format(time.RFC3339)
	}
	if to != nil {
		right = to.UTC().Format(time.RFC3339)
	}
	return left + " to " + right
}

func evaluationMarkdownRow(b *strings.Builder, cells ...string) {
	b.WriteString("| ")
	for index, cell := range cells {
		if index > 0 {
			b.WriteString(" | ")
		}
		b.WriteString(strings.ReplaceAll(strings.ReplaceAll(cell, "\n", " "), "|", "\\|"))
	}
	b.WriteString(" |\n")
}
