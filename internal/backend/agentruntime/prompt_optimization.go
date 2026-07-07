package agentruntime

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
)

const (
	promptOptimizationWorkflowName    = "prompt_optimization"
	promptOptimizationWorkflowVersion = "v1"
)

func promptOptimizationWorkflowDefinition() WorkflowDefinition {
	return WorkflowDefinition{
		Name:    promptOptimizationWorkflowName,
		Version: promptOptimizationWorkflowVersion,
		Steps: []WorkflowStepDefinition{
			{Name: "collect_badcases"},
			{Name: "cluster_failures"},
			{Name: "generate_candidate_prompt"},
			{Name: "offline_replay"},
			{Name: "create_review"},
		},
	}
}

func (s *Server) handleAdminOpsPromptOptimize(w http.ResponseWriter, r *http.Request, actor User, promptID string) {
	if s.promptStore == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "prompt store is not configured"})
		return
	}
	if s.runtime == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "runtime is not configured"})
		return
	}
	var body struct {
		BaselineVersion string               `json:"baseline_version"`
		SetID           string               `json:"set_id"`
		SetVersion      string               `json:"set_version"`
		Judge           string               `json:"judge"`
		MaxBadcases     int                  `json:"max_badcases"`
		Thresholds      EvaluationThresholds `json:"thresholds"`
	}
	if err := readOptionalJSON(r, &body); err != nil {
		writeJSONError(w, err)
		return
	}
	engine := s.newPromptOptimizationWorkflowEngine()
	run, err := engine.Execute(r.Context(), WorkflowRequest{
		Definition: promptOptimizationWorkflowDefinition(),
		UserID:     actor.ID,
		State: map[string]any{
			"prompt_id":        strings.TrimSpace(promptID),
			"baseline_version": strings.TrimSpace(body.BaselineVersion),
			"set_id":           strings.TrimSpace(body.SetID),
			"set_version":      strings.TrimSpace(body.SetVersion),
			"judge":            strings.TrimSpace(body.Judge),
			"max_badcases":     body.MaxBadcases,
			"actor":            actor.ID,
			"thresholds":       body.Thresholds,
		},
	})
	if err != nil {
		writeJSONError(w, err)
		return
	}
	steps, _ := engine.Store().ListWorkflowStepRuns(r.Context(), run.ID)
	s.auditEvent(r, "prompt_optimize", actor, map[string]any{"prompt_id": promptID, "workflow_run_id": run.ID, "candidate_version": run.State["candidate_version"]})
	writeJSON(w, http.StatusCreated, map[string]any{"workflow": run, "steps": steps})
}

func (s *Server) handleAdminOpsListPromptOptimizationRuns(w http.ResponseWriter, r *http.Request) {
	if s.runtime == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "runtime is not configured"})
		return
	}
	runs, err := s.runtime.ListWorkflowRuns(r.Context(), WorkflowRunFilter{
		Name:   promptOptimizationWorkflowName,
		Status: strings.TrimSpace(r.URL.Query().Get("status")),
		Limit:  parseBoundedInt(r.URL.Query().Get("limit"), 100, 1, 300),
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"workflows": runs})
}

func (s *Server) handleAdminOpsGetPromptOptimizationRun(w http.ResponseWriter, r *http.Request, runID string) {
	if s.runtime == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "runtime is not configured"})
		return
	}
	run, err := s.runtime.GetWorkflowRun(r.Context(), runID)
	if err != nil || run == nil || run.Name != promptOptimizationWorkflowName {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "prompt optimization run not found"})
		return
	}
	steps, err := s.runtime.ListWorkflowSteps(r.Context(), runID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"workflow": run, "steps": steps})
}

func (s *Server) newPromptOptimizationWorkflowEngine() *WorkflowEngine {
	store := s.runtime.workflowStore
	if store == nil {
		store = NewMemoryWorkflowStore()
		s.runtime.SetWorkflowStore(store)
	}
	engine := NewWorkflowEngine(store, ContextWorkflowEventSink{})
	engine.RegisterStepHandler("collect_badcases", s.promptOptimizationCollectBadcases)
	engine.RegisterStepHandler("cluster_failures", s.promptOptimizationClusterFailures)
	engine.RegisterStepHandler("generate_candidate_prompt", s.promptOptimizationGenerateCandidate)
	engine.RegisterStepHandler("offline_replay", s.promptOptimizationOfflineReplay)
	engine.RegisterStepHandler("create_review", s.promptOptimizationCreateReview)
	return engine
}

func (s *Server) promptOptimizationCollectBadcases(ctx context.Context, _ *WorkflowRun, input map[string]any) (map[string]any, error) {
	promptID := workflowString(input, "prompt_id")
	baselineVersion := workflowString(input, "baseline_version")
	if baselineVersion == "" {
		published, err := s.promptStore.GetPublishedPromptVersion(ctx, promptID)
		if err != nil {
			return nil, err
		}
		baselineVersion = published.Version
	}
	maxBadcases := promptWorkflowInt(input, "max_badcases", 25)
	if maxBadcases <= 0 || maxBadcases > 100 {
		maxBadcases = 25
	}
	badcases := []map[string]any{}
	if s.evaluation != nil {
		results, err := s.evaluation.ListEvaluationResults(ctx, EvaluationResultFilter{PromptID: promptID, PromptVersion: baselineVersion, Limit: maxBadcases * 3})
		if err != nil {
			return nil, err
		}
		for _, result := range results {
			if result.Status != EvaluationResultStatusFailed && result.Status != EvaluationResultStatusWarning {
				continue
			}
			badcases = append(badcases, map[string]any{
				"result_id":    result.ID,
				"subject_type": result.SubjectType,
				"subject_id":   result.SubjectID,
				"status":       result.Status,
				"score":        result.Score,
				"findings":     result.Findings,
			})
			if len(badcases) >= maxBadcases {
				break
			}
		}
	}
	return map[string]any{"baseline_version": baselineVersion, "badcases": badcases, "badcase_count": len(badcases)}, nil
}

func (s *Server) promptOptimizationClusterFailures(_ context.Context, _ *WorkflowRun, input map[string]any) (map[string]any, error) {
	badcases := workflowMapSlice(input, "badcases")
	counts := map[string]int{}
	for _, item := range badcases {
		findings := workflowMapSlice(item, "findings")
		if len(findings) == 0 {
			counts[fmt.Sprint(item["status"])]++
			continue
		}
		for _, finding := range findings {
			key := firstNonEmptyString(workflowString(finding, "code"), workflowString(finding, "severity"), "unknown")
			counts[key]++
		}
	}
	clusters := make([]map[string]any, 0, len(counts))
	for key, count := range counts {
		clusters = append(clusters, map[string]any{"cluster": key, "count": count})
	}
	sort.Slice(clusters, func(i, j int) bool {
		return fmt.Sprint(clusters[i]["cluster"]) < fmt.Sprint(clusters[j]["cluster"])
	})
	return map[string]any{"failure_clusters": clusters, "cluster_summary": promptClusterSummary(clusters)}, nil
}

func (s *Server) promptOptimizationGenerateCandidate(ctx context.Context, _ *WorkflowRun, input map[string]any) (map[string]any, error) {
	promptID := workflowString(input, "prompt_id")
	baselineVersion := workflowString(input, "baseline_version")
	baseline, err := s.promptStore.GetPromptVersion(ctx, promptID, baselineVersion)
	if err != nil {
		return nil, err
	}
	version := "opt-" + time.Now().UTC().Format("20060102150405")
	summary := workflowString(input, "cluster_summary")
	content := promptOptimizedContent(baseline.Content, summary)
	candidate := normalizePromptVersion(PromptVersion{
		PromptID:        promptID,
		Version:         version,
		Status:          PromptStatusReviewPending,
		Content:         content,
		VariablesSchema: baseline.VariablesSchema,
		RenderConfig:    baseline.RenderConfig,
		BaseVersion:     baseline.Version,
		Changelog:       "Generated by prompt_optimization workflow from failed/warning evaluation cases. Requires human review before publish.",
		CreatedBy:       workflowString(input, "actor"),
	})
	return map[string]any{
		"candidate_version":   candidate.Version,
		"candidate_content":   candidate.Content,
		"candidate_hash":      candidate.ContentHash,
		"candidate_changelog": candidate.Changelog,
	}, nil
}

func (s *Server) promptOptimizationOfflineReplay(ctx context.Context, _ *WorkflowRun, input map[string]any) (map[string]any, error) {
	if s.evaluation == nil || workflowString(input, "set_id") == "" {
		return map[string]any{"offline_replay": "skipped"}, nil
	}
	promptID := workflowString(input, "prompt_id")
	baselineVersionID := workflowString(input, "baseline_version")
	candidateVersionID := workflowString(input, "candidate_version")
	baseline, err := s.promptStore.GetPromptVersion(ctx, promptID, baselineVersionID)
	if err != nil {
		return nil, err
	}
	candidate := normalizePromptVersion(PromptVersion{
		PromptID:        promptID,
		Version:         candidateVersionID,
		Status:          PromptStatusReviewPending,
		Content:         workflowString(input, "candidate_content"),
		VariablesSchema: baseline.VariablesSchema,
		RenderConfig:    baseline.RenderConfig,
		BaseVersion:     baseline.Version,
	})
	set, err := s.getGoldenSetVersion(ctx, workflowString(input, "set_id"), workflowString(input, "set_version"))
	if err != nil {
		return nil, err
	}
	candidates := make([]GoldenCandidate, 0, len(set.Cases))
	for _, item := range set.Cases {
		candidates = append(candidates, GoldenCandidate{
			CaseID:            item.ID,
			Output:            firstNonEmptyString(item.ExpectedAnswer, strings.Join(item.ExpectedFacts, "\n"), item.Query),
			RetrievedEvidence: item.GoldEvidence,
			Metadata:          map[string]any{"source": "prompt_optimization"},
		})
	}
	engine := s.goldenEvaluationEngineForRequest(ctx, workflowString(input, "judge"))
	if engine == nil {
		return nil, fmt.Errorf("evaluation judge is not configured")
	}
	baselineReport, err := engine.EvaluateGolden(ctx, GoldenEvaluationRequest{
		Name:       promptID + "_" + baseline.Version + "_optimization_baseline",
		Trigger:    "prompt_optimization",
		Set:        set,
		Candidates: attachPromptMetadataToGoldenCandidates(candidates, baseline, "baseline"),
	})
	if err != nil {
		return nil, err
	}
	candidateReport, err := engine.EvaluateGolden(ctx, GoldenEvaluationRequest{
		Name:       promptID + "_" + candidate.Version + "_optimization_candidate",
		Trigger:    "prompt_optimization",
		Set:        set,
		Candidates: attachPromptMetadataToGoldenCandidates(candidates, candidate, "candidate"),
	})
	if err != nil {
		return nil, err
	}
	persistedBaseline, err := s.persistEvaluationRunReportContext(ctx, baselineReport)
	if err != nil {
		return nil, err
	}
	persistedCandidate, err := s.persistEvaluationRunReportContext(ctx, candidateReport)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"offline_replay":        "completed",
		"baseline_eval_run_id":  persistedBaseline.Run.ID,
		"candidate_eval_run_id": persistedCandidate.Run.ID,
		"baseline_pass_rate":    persistedBaseline.Summary.PassRate,
		"candidate_pass_rate":   persistedCandidate.Summary.PassRate,
	}, nil
}

func (s *Server) promptOptimizationCreateReview(ctx context.Context, _ *WorkflowRun, input map[string]any) (map[string]any, error) {
	promptID := workflowString(input, "prompt_id")
	changelog := workflowString(input, "candidate_changelog")
	recommendation := "ready_for_review"
	if workflowString(input, "offline_replay") == "completed" && workflowFloat(input, "candidate_pass_rate") < workflowFloat(input, "baseline_pass_rate") {
		recommendation = "blocked_by_eval_regression"
		changelog = strings.TrimSpace(changelog + "\n\nReview gate: candidate pass rate is below baseline; keep in review until fixed or explicitly overridden.")
	}
	version := PromptVersion{
		PromptID:    promptID,
		Version:     workflowString(input, "candidate_version"),
		Status:      PromptStatusReviewPending,
		Content:     workflowString(input, "candidate_content"),
		BaseVersion: workflowString(input, "baseline_version"),
		Changelog:   changelog,
		CreatedBy:   workflowString(input, "actor"),
	}
	baseline, err := s.promptStore.GetPromptVersion(ctx, promptID, version.BaseVersion)
	if err != nil {
		return nil, err
	}
	version.VariablesSchema = baseline.VariablesSchema
	version.RenderConfig = baseline.RenderConfig
	created, err := s.promptStore.CreatePromptVersion(ctx, version)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"review_prompt_id":      created.PromptID,
		"review_prompt_version": created.Version,
		"review_status":         created.Status,
		"review_prompt_hash":    created.ContentHash,
		"review_recommendation": recommendation,
	}, nil
}

func promptClusterSummary(clusters []map[string]any) string {
	if len(clusters) == 0 {
		return "No failed or warning evaluation cases were found; preserve current behavior and add stricter non-regression constraints."
	}
	parts := make([]string, 0, len(clusters))
	for _, cluster := range clusters {
		parts = append(parts, fmt.Sprintf("%s=%v", cluster["cluster"], cluster["count"]))
	}
	return strings.Join(parts, "; ")
}

func promptOptimizedContent(content, clusterSummary string) string {
	content = strings.TrimSpace(content)
	addendum := strings.TrimSpace(`Prompt optimization review notes:
- Address badcase clusters: ` + clusterSummary + `
- Preserve the current variable contract and output format.
- Prefer explicit refusal, clarification, or no-op behavior over hallucinating, cross-session leakage, or acting on stale context.
- Do not weaken tool, evidence, privacy, or human-review constraints.`)
	if strings.Contains(content, "Prompt optimization review notes:") {
		return content
	}
	return strings.TrimSpace(content + "\n\n" + addendum)
}

func promptWorkflowInt(input map[string]any, key string, fallback int) int {
	switch value := input[key].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	case float32:
		return int(value)
	default:
		return fallback
	}
}

func workflowFloat(input map[string]any, key string) float64 {
	switch value := input[key].(type) {
	case float64:
		return value
	case float32:
		return float64(value)
	case int:
		return float64(value)
	case int64:
		return float64(value)
	default:
		return 0
	}
}

func workflowMapSlice(input map[string]any, key string) []map[string]any {
	raw, ok := input[key]
	if !ok || raw == nil {
		return nil
	}
	switch values := raw.(type) {
	case []map[string]any:
		return values
	case []any:
		out := make([]map[string]any, 0, len(values))
		for _, value := range values {
			if item, ok := value.(map[string]any); ok {
				out = append(out, item)
			}
		}
		return out
	default:
		return nil
	}
}
