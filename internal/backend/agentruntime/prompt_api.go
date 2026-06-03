package agentruntime

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

func (s *Server) SetPromptStore(store PromptStore) {
	if s == nil {
		return
	}
	s.promptStore = store
	if s.runtime != nil {
		s.runtime.SetPromptStore(store)
	}
}

func (s *Server) promptStoreRequiredMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.promptStore == nil {
			writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "prompt store is not configured"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleAdminOpsUpsertPrompt(w http.ResponseWriter, r *http.Request, actor User) {
	var body struct {
		Prompt  PromptTemplate `json:"prompt"`
		Version *PromptVersion `json:"version,omitempty"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSONError(w, err)
		return
	}
	prompt, err := s.promptStore.UpsertPrompt(r.Context(), body.Prompt)
	if err != nil {
		writeJSONError(w, err)
		return
	}
	payload := map[string]any{"prompt": prompt}
	if body.Version != nil {
		version := *body.Version
		version.PromptID = firstNonEmptyString(version.PromptID, prompt.ID)
		version.CreatedBy = firstNonEmptyString(version.CreatedBy, actor.ID)
		created, err := s.promptStore.CreatePromptVersion(r.Context(), version)
		if err != nil {
			writeJSONError(w, err)
			return
		}
		payload["version"] = created
	}
	s.auditEvent(r, "prompt_upsert", actor, map[string]any{"prompt_id": prompt.ID})
	writeJSON(w, http.StatusCreated, payload)
}

func (s *Server) handleAdminOpsListPrompts(w http.ResponseWriter, r *http.Request) {
	prompts, err := s.promptStore.ListPrompts(r.Context(), PromptListFilter{
		Scope:  strings.TrimSpace(r.URL.Query().Get("scope")),
		Status: strings.TrimSpace(r.URL.Query().Get("status")),
		Query:  strings.TrimSpace(r.URL.Query().Get("q")),
		Limit:  parseBoundedInt(r.URL.Query().Get("limit"), 100, 1, 500),
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"prompts": prompts})
}

func (s *Server) handleAdminOpsGetPrompt(w http.ResponseWriter, r *http.Request, promptID string) {
	prompt, err := s.promptStore.GetPrompt(r.Context(), promptID)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
		}
		writeJSON(w, status, map[string]string{"error": "prompt not found"})
		return
	}
	versions, err := s.promptStore.ListPromptVersions(r.Context(), prompt.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	payload := map[string]any{"prompt": prompt, "versions": versions}
	if published, err := s.promptStore.GetPublishedPromptVersion(r.Context(), prompt.ID); err == nil {
		payload["published_version"] = published
	}
	writeJSON(w, http.StatusOK, payload)
}

func (s *Server) handleAdminOpsCreatePromptVersion(w http.ResponseWriter, r *http.Request, actor User, promptID string) {
	var version PromptVersion
	if err := readJSON(r, &version); err != nil {
		writeJSONError(w, err)
		return
	}
	version.PromptID = firstNonEmptyString(version.PromptID, promptID)
	version.CreatedBy = firstNonEmptyString(version.CreatedBy, actor.ID)
	created, err := s.promptStore.CreatePromptVersion(r.Context(), version)
	if err != nil {
		writeJSONError(w, err)
		return
	}
	s.auditEvent(r, "prompt_version_create", actor, map[string]any{"prompt_id": created.PromptID, "version": created.Version, "status": created.Status})
	writeJSON(w, http.StatusCreated, map[string]any{"version": created})
}

func (s *Server) handleAdminOpsListPromptVersions(w http.ResponseWriter, r *http.Request, promptID string) {
	versions, err := s.promptStore.ListPromptVersions(r.Context(), promptID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"versions": versions})
}

func (s *Server) handleAdminOpsPublishPrompt(w http.ResponseWriter, r *http.Request, actor User, promptID string) {
	var body struct {
		Version   string `json:"version"`
		Changelog string `json:"changelog"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSONError(w, err)
		return
	}
	version, err := s.promptStore.PublishPromptVersion(r.Context(), promptID, body.Version, actor.ID, body.Changelog)
	if err != nil {
		writePromptStoreError(w, err, "prompt version not found")
		return
	}
	s.auditEvent(r, "prompt_publish", actor, map[string]any{"prompt_id": version.PromptID, "version": version.Version})
	writeJSON(w, http.StatusOK, map[string]any{"version": version})
}

func (s *Server) handleAdminOpsRollbackPrompt(w http.ResponseWriter, r *http.Request, actor User, promptID string) {
	var body struct {
		Version   string `json:"version"`
		Changelog string `json:"changelog"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSONError(w, err)
		return
	}
	version, err := s.promptStore.RollbackPromptVersion(r.Context(), promptID, body.Version, actor.ID, body.Changelog)
	if err != nil {
		writePromptStoreError(w, err, "prompt version not found")
		return
	}
	s.auditEvent(r, "prompt_rollback", actor, map[string]any{"prompt_id": version.PromptID, "version": version.Version})
	writeJSON(w, http.StatusOK, map[string]any{"version": version})
}

func (s *Server) handleAdminOpsPromptVersionDiff(w http.ResponseWriter, r *http.Request, promptID string) {
	fromVersion := strings.TrimSpace(r.URL.Query().Get("from_version"))
	toVersion := strings.TrimSpace(r.URL.Query().Get("to_version"))
	if toVersion == "" || strings.EqualFold(toVersion, "current") || strings.EqualFold(toVersion, "published") {
		published, err := s.promptStore.GetPublishedPromptVersion(r.Context(), promptID)
		if err != nil {
			writePromptStoreError(w, err, "published prompt version not found")
			return
		}
		toVersion = published.Version
	}
	from, err := s.promptStore.GetPromptVersion(r.Context(), promptID, fromVersion)
	if err != nil {
		writePromptStoreError(w, err, "from prompt version not found")
		return
	}
	to, err := s.promptStore.GetPromptVersion(r.Context(), promptID, toVersion)
	if err != nil {
		writePromptStoreError(w, err, "to prompt version not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"diff": diffPromptVersions(from, to), "from": from, "to": to})
}

func (s *Server) handleAdminOpsPromptRenderPreview(w http.ResponseWriter, r *http.Request, promptID, version string) {
	var body struct {
		Variables map[string]any `json:"variables"`
	}
	if err := readOptionalJSON(r, &body); err != nil {
		writeJSONError(w, err)
		return
	}
	resolver := NewPromptResolver(s.promptStore, nil)
	resolution, err := resolver.Resolve(r.Context(), PromptResolveRequest{PromptID: promptID, ForcedVersion: version})
	if err != nil {
		writePromptStoreError(w, err, "prompt version not found")
		return
	}
	rendered, err := RenderPrompt(resolution, body.Variables)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error(), "render": rendered})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"render": rendered})
}

func (s *Server) handleAdminOpsPromptVersionEval(w http.ResponseWriter, r *http.Request, actor User, promptID, version string) {
	if s.evaluation == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "evaluation store is not configured"})
		return
	}
	var req struct {
		ID         string               `json:"id"`
		Name       string               `json:"name"`
		Trigger    string               `json:"trigger"`
		SetID      string               `json:"set_id"`
		SetVersion string               `json:"set_version"`
		Judge      string               `json:"judge"`
		Candidates []GoldenCandidate    `json:"candidates"`
		Thresholds EvaluationThresholds `json:"thresholds"`
	}
	if err := readJSON(r, &req); err != nil {
		writeJSONError(w, err)
		return
	}
	promptVersion, err := s.promptStore.GetPromptVersion(r.Context(), promptID, version)
	if err != nil {
		writePromptStoreError(w, err, "prompt version not found")
		return
	}
	set, err := s.evaluation.GetGoldenSetVersion(r.Context(), req.SetID, req.SetVersion)
	if err != nil {
		writePromptStoreError(w, err, "golden set not found")
		return
	}
	candidates := attachPromptMetadataToGoldenCandidates(req.Candidates, promptVersion, "")
	engine := s.goldenEvaluationEngineForRequest(r.Context(), req.Judge)
	if engine == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "evaluation judge is not configured"})
		return
	}
	report, err := engine.EvaluateGolden(r.Context(), GoldenEvaluationRequest{
		ID:         req.ID,
		Name:       firstNonEmptyString(req.Name, fmt.Sprintf("%s %s prompt eval", promptID, version)),
		Trigger:    firstNonEmptyString(req.Trigger, "manual_prompt_golden"),
		Set:        set,
		Candidates: candidates,
		Thresholds: req.Thresholds,
	})
	if err != nil {
		writeJSONError(w, err)
		return
	}
	report.Run.Metrics = mergeEvaluationMetricMaps(report.Run.Metrics, map[string]any{
		"prompt_id":      promptVersion.PromptID,
		"prompt_version": promptVersion.Version,
		"prompt_hash":    promptVersion.ContentHash,
	})
	persisted, err := s.persistEvaluationRunReport(r, report)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.auditEvent(r, "prompt_version_eval", actor, map[string]any{"prompt_id": promptID, "version": version, "eval_run_id": persisted.Run.ID})
	writeJSON(w, http.StatusCreated, map[string]any{"run": persisted.Run, "results": persisted.Results, "reviews": persisted.Reviews, "summary": persisted.Summary})
}

func (s *Server) handleAdminOpsUpsertPromptExperiment(w http.ResponseWriter, r *http.Request, actor User) {
	var body struct {
		Experiment PromptExperiment          `json:"experiment"`
		Variants   []PromptExperimentVariant `json:"variants"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSONError(w, err)
		return
	}
	body.Experiment.CreatedBy = firstNonEmptyString(body.Experiment.CreatedBy, actor.ID)
	body.Experiment.UpdatedBy = firstNonEmptyString(body.Experiment.UpdatedBy, actor.ID)
	experiment, err := s.promptStore.UpsertPromptExperiment(r.Context(), body.Experiment, body.Variants)
	if err != nil {
		writeJSONError(w, err)
		return
	}
	_, variants, _ := s.promptStore.GetPromptExperiment(r.Context(), experiment.ID)
	s.auditEvent(r, "prompt_experiment_upsert", actor, map[string]any{"experiment_id": experiment.ID, "prompt_id": experiment.PromptID, "status": experiment.Status})
	writeJSON(w, http.StatusCreated, map[string]any{"experiment": experiment, "variants": variants})
}

func (s *Server) handleAdminOpsListPromptExperiments(w http.ResponseWriter, r *http.Request) {
	experiments, err := s.promptStore.ListPromptExperiments(r.Context(), PromptExperimentFilter{
		PromptID: strings.TrimSpace(r.URL.Query().Get("prompt_id")),
		Status:   strings.TrimSpace(r.URL.Query().Get("status")),
		Query:    strings.TrimSpace(r.URL.Query().Get("q")),
		Limit:    parseBoundedInt(r.URL.Query().Get("limit"), 100, 1, 500),
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"experiments": experiments})
}

func (s *Server) handleAdminOpsGetPromptExperiment(w http.ResponseWriter, r *http.Request, experimentID string) {
	experiment, variants, err := s.promptStore.GetPromptExperiment(r.Context(), experimentID)
	if err != nil {
		writePromptStoreError(w, err, "prompt experiment not found")
		return
	}
	payload := map[string]any{"experiment": experiment, "variants": variants}
	if s.llmUsage != nil {
		payload["usage_by_variant"] = s.promptExperimentVariantUsage(r.Context(), experiment, variants)
	}
	writeJSON(w, http.StatusOK, payload)
}

func (s *Server) handleAdminOpsPromptExperimentAction(w http.ResponseWriter, r *http.Request, actor User, experimentID, action string) {
	var body struct {
		WinnerVariantID string `json:"winner_variant_id"`
	}
	if err := readOptionalJSON(r, &body); err != nil {
		writeJSONError(w, err)
		return
	}
	status := ""
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "start":
		status = PromptExperimentStatusRunning
	case "pause":
		status = PromptExperimentStatusPaused
	case "complete":
		status = PromptExperimentStatusCompleted
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported prompt experiment action"})
		return
	}
	experiment, err := s.promptStore.UpdatePromptExperimentStatus(r.Context(), experimentID, status, body.WinnerVariantID, actor.ID)
	if err != nil {
		writePromptStoreError(w, err, "prompt experiment not found")
		return
	}
	_, variants, _ := s.promptStore.GetPromptExperiment(r.Context(), experiment.ID)
	s.auditEvent(r, "prompt_experiment_"+status, actor, map[string]any{"experiment_id": experiment.ID, "prompt_id": experiment.PromptID, "winner_variant_id": experiment.WinnerVariantID})
	writeJSON(w, http.StatusOK, map[string]any{"experiment": experiment, "variants": variants})
}

func (s *Server) promptExperimentVariantUsage(ctx context.Context, experiment PromptExperiment, variants []PromptExperimentVariant) []map[string]any {
	out := make([]map[string]any, 0, len(variants))
	for _, variant := range variants {
		summary, err := s.llmUsage.SummarizeLLMUsage(ctx, LLMUsageAdminFilter{
			Since:        time.Now().UTC().Add(-7 * 24 * time.Hour),
			ExperimentID: experiment.ID,
			VariantID:    variant.VariantID,
			Limit:        50,
		})
		if err != nil {
			continue
		}
		out = append(out, map[string]any{
			"variant_id":          variant.VariantID,
			"prompt_version":      variant.PromptVersion,
			"requests":            summary.Requests,
			"successes":           summary.Successes,
			"failures":            summary.Failures,
			"estimated_cost_usd":  summary.EstimatedCostUSD,
			"average_latency_ms":  summary.AverageLatencyMs,
			"total_tokens":        summary.TotalTokens,
			"recent_sample_count": len(summary.Recent),
		})
	}
	return out
}

func (s *Server) goldenEvaluationEngineForRequest(ctx context.Context, judge string) *EvaluationEngine {
	engine := NewEvaluationEngine(RuntimeEvaluationTraceSource{Runtime: s.runtime, LLMUsage: s.llmUsage, Risk: s.risk})
	judgeMode := strings.ToLower(strings.TrimSpace(judge))
	if judgeMode == "" {
		judgeMode = "heuristic"
	}
	switch judgeMode {
	case "heuristic":
		return engine
	case "llm", "llm-as-judge":
		if s.evaluationJudge == nil {
			return nil
		}
		engine.Judge = s.promptAwareGoldenJudge(ctx, s.evaluationJudge)
		return engine
	default:
		return nil
	}
}

func (s *Server) promptAwareGoldenJudge(ctx context.Context, judge GoldenJudge) GoldenJudge {
	if s == nil || s.promptStore == nil || judge == nil {
		return judge
	}
	resolution, err := NewPromptResolver(s.promptStore, nil).Resolve(ctx, PromptResolveRequest{PromptID: PromptIDEvalJudge})
	if err != nil {
		return judge
	}
	rendered, err := RenderPrompt(resolution, nil)
	if err != nil {
		return judge
	}
	switch typed := judge.(type) {
	case PlannerGoldenJudge:
		typed.SystemPrompt = rendered.Content
		typed.PromptID = rendered.PromptID
		typed.PromptVersion = rendered.PromptVersion
		typed.PromptHash = rendered.PromptHash
		return typed
	case *PlannerGoldenJudge:
		copy := *typed
		copy.SystemPrompt = rendered.Content
		copy.PromptID = rendered.PromptID
		copy.PromptVersion = rendered.PromptVersion
		copy.PromptHash = rendered.PromptHash
		return copy
	default:
		return judge
	}
}

func attachPromptMetadataToGoldenCandidates(candidates []GoldenCandidate, version PromptVersion, variantID string) []GoldenCandidate {
	out := make([]GoldenCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.Metadata == nil {
			candidate.Metadata = map[string]any{}
		}
		candidate.Metadata["prompt_id"] = version.PromptID
		candidate.Metadata["prompt_version"] = version.Version
		candidate.Metadata["prompt_hash"] = version.ContentHash
		if strings.TrimSpace(variantID) != "" {
			candidate.Metadata["variant_id"] = strings.TrimSpace(variantID)
		}
		out = append(out, candidate)
	}
	return out
}

func writePromptStoreError(w http.ResponseWriter, err error, notFoundMessage string) {
	status := http.StatusInternalServerError
	message := err.Error()
	if errors.Is(err, sql.ErrNoRows) {
		status = http.StatusNotFound
		message = notFoundMessage
	}
	writeJSON(w, status, map[string]string{"error": message})
}
