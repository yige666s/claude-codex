package agentruntime

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

const (
	DeepAgentTemplateEvalSetVersion = "phase11-v1"
	DeepAgentTemplateReplayTrigger  = "template_replay"

	defaultEvalRepairMaxTriggersPerRun = 3
)

type DeepAgentTemplateReplayRequest struct {
	TemplateIDs     []string             `json:"template_ids,omitempty"`
	PlannerVersion  string               `json:"planner_version,omitempty"`
	RouterVersion   string               `json:"router_version,omitempty"`
	ExecutorVersion string               `json:"executor_version,omitempty"`
	VerifierVersion string               `json:"verifier_version,omitempty"`
	ExperimentID    string               `json:"experiment_id,omitempty"`
	VariantID       string               `json:"variant_id,omitempty"`
	Persist         bool                 `json:"persist,omitempty"`
	RepairEnabled   bool                 `json:"repair_enabled,omitempty"`
	Metadata        map[string]any       `json:"metadata,omitempty"`
	Thresholds      EvaluationThresholds `json:"thresholds,omitempty"`
}

type DeepAgentTemplateReplayReport struct {
	Runs      []EvaluationRunReport          `json:"runs"`
	Dashboard DeepAgentTemplateEvalDashboard `json:"dashboard"`
	Corpus    []GoldenSet                    `json:"corpus,omitempty"`
	Versions  map[string]string              `json:"versions,omitempty"`
}

type DeepAgentTemplateEvalDashboard struct {
	Total       int                        `json:"total"`
	Templates   []DeepAgentTemplateEvalRow `json:"templates"`
	GeneratedAt time.Time                  `json:"generated_at"`
	Source      string                     `json:"source,omitempty"`
}

type DeepAgentTemplateEvalRow struct {
	TemplateID             string            `json:"template_id"`
	TemplateName           string            `json:"template_name,omitempty"`
	Total                  int               `json:"total"`
	Passed                 int               `json:"passed"`
	Failed                 int               `json:"failed"`
	Warning                int               `json:"warning"`
	SuccessRate            float64           `json:"success_rate"`
	BlockedRate            float64           `json:"blocked_rate"`
	AverageActionCount     float64           `json:"average_action_count"`
	AverageDurationMS      float64           `json:"average_duration_ms"`
	TokenEstimate          int               `json:"token_estimate"`
	EstimatedCostUSD       float64           `json:"estimated_cost_usd"`
	VerifierFailureReasons []map[string]any  `json:"verifier_failure_reasons,omitempty"`
	Versions               map[string]string `json:"versions,omitempty"`
}

type EvalRepairTriggerPolicy struct {
	Enabled             bool     `json:"enabled"`
	MaxTriggersPerRun   int      `json:"max_triggers_per_run,omitempty"`
	AllowedSubjectTypes []string `json:"allowed_subject_types,omitempty"`
}

func DefaultDeepAgentTemplateGoldenSets() []GoldenSet {
	out := []GoldenSet{}
	for _, tmpl := range DefaultDeepAgentTaskTemplates() {
		switch tmpl.ID {
		case DeepAgentTemplateResearchReport, DeepAgentTemplateCodeFix, DeepAgentTemplateDocGeneration:
			out = append(out, deepAgentTemplateGoldenSet(tmpl))
		}
	}
	return out
}

func deepAgentTemplateGoldenSet(tmpl DeepAgentTaskTemplate) GoldenSet {
	cases := []GoldenCase{
		deepAgentTemplateGoldenCase(tmpl, "happy", "succeeded", "", 4, 180000, 2200, 0.018),
		deepAgentTemplateGoldenCase(tmpl, "blocked", "blocked", deepAgentTemplateBlockedReason(tmpl), 2, 90000, 1200, 0.009),
		deepAgentTemplateGoldenCase(tmpl, "verifier_failure", "verifier_failed", deepAgentTemplateVerifierFailureReason(tmpl), 5, 240000, 2600, 0.022),
	}
	return normalizeGoldenSet(GoldenSet{
		ID:          "deep_agent_template_" + tmpl.ID,
		Name:        "DeepAgent template replay: " + tmpl.Name,
		Description: "Phase 11 replay corpus for " + tmpl.ID + " covering happy, blocked, and verifier failure paths.",
		Version:     DeepAgentTemplateEvalSetVersion,
		Metadata: map[string]any{
			"template_id":   tmpl.ID,
			"template_name": tmpl.Name,
			"phase":         "phase11",
			"replayable":    true,
		},
		Cases: cases,
	})
}

func deepAgentTemplateGoldenCase(tmpl DeepAgentTaskTemplate, scenario, expectedStatus, verifierReason string, actionCount int, durationMS int64, tokens int, cost float64) GoldenCase {
	expected := deepAgentTemplateExpectedAnswer(tmpl, scenario, expectedStatus, verifierReason)
	query := fmt.Sprintf("Replay expectation: %s", expected)
	facts := []string{tmpl.ID, scenario, expectedStatus}
	if verifierReason != "" {
		facts = append(facts, verifierReason)
	}
	return normalizeGoldenCase(GoldenCase{
		ID:             stableEvaluationSubjectID(strings.Join([]string{tmpl.ID, scenario, DeepAgentTemplateEvalSetVersion}, "|")),
		Query:          query,
		ExpectedAnswer: expected,
		ExpectedFacts:  facts,
		GoldEvidence: []GoldenEvidence{{
			ID:      tmpl.ID + "-" + scenario + "-trace",
			Source:  "deep_agent_template_replay",
			Content: expected,
			Metadata: map[string]any{
				"template_id":     tmpl.ID,
				"scenario":        scenario,
				"expected_status": expectedStatus,
			},
		}},
		Tags: []string{"deep_agent", "template_replay", tmpl.ID, scenario, expectedStatus},
		Metadata: map[string]any{
			"template_id":             tmpl.ID,
			"template_name":           tmpl.Name,
			"scenario":                scenario,
			"expected_status":         expectedStatus,
			"verifier_failure_reason": verifierReason,
			"action_count":            actionCount,
			"duration_ms":             durationMS,
			"token_estimate":          tokens,
			"estimated_cost_usd":      cost,
		},
	})
}

func deepAgentTemplateExpectedAnswer(tmpl DeepAgentTaskTemplate, scenario, status, reason string) string {
	switch scenario {
	case "blocked":
		return fmt.Sprintf("%s replay blocked with status %s because %s for template %s", tmpl.ID, status, reason, tmpl.ID)
	case "verifier_failure":
		return fmt.Sprintf("%s replay reached verifier failure status %s because %s for template %s", tmpl.ID, status, reason, tmpl.ID)
	default:
		return fmt.Sprintf("%s replay succeeded with status %s for template %s and deliverable %s", tmpl.ID, status, tmpl.ID, tmpl.Deliverable)
	}
}

func deepAgentTemplateBlockedReason(tmpl DeepAgentTaskTemplate) string {
	switch tmpl.ID {
	case DeepAgentTemplateResearchReport:
		return "missing_source_permission"
	case DeepAgentTemplateCodeFix:
		return "missing_reproduction_context"
	case DeepAgentTemplateDocGeneration:
		return "missing_required_source_document"
	default:
		return "blocked_by_required_context"
	}
}

func deepAgentTemplateVerifierFailureReason(tmpl DeepAgentTaskTemplate) string {
	switch tmpl.ID {
	case DeepAgentTemplateResearchReport:
		return "source_coverage_incomplete"
	case DeepAgentTemplateCodeFix:
		return "verification_test_failed"
	case DeepAgentTemplateDocGeneration:
		return "artifact_format_check_failed"
	default:
		return "verifier_check_failed"
	}
}

func deepAgentTemplateReplayCandidates(set GoldenSet, req DeepAgentTemplateReplayRequest) []GoldenCandidate {
	candidates := make([]GoldenCandidate, 0, len(set.Cases))
	for _, item := range set.Cases {
		metadata := cloneEvaluationMap(item.Metadata)
		if metadata == nil {
			metadata = map[string]any{}
		}
		for key, value := range req.Metadata {
			metadata[key] = value
		}
		metadata["planner_version"] = firstNonEmptyString(req.PlannerVersion, "planner-current")
		metadata["router_version"] = firstNonEmptyString(req.RouterVersion, "router-current")
		metadata["executor_version"] = firstNonEmptyString(req.ExecutorVersion, "executor-current")
		metadata["verifier_version"] = firstNonEmptyString(req.VerifierVersion, "verifier-current")
		metadata["experiment_id"] = req.ExperimentID
		metadata["variant_id"] = req.VariantID
		candidates = append(candidates, normalizeGoldenCandidate(GoldenCandidate{
			CaseID:            item.ID,
			Output:            item.ExpectedAnswer,
			RetrievedEvidence: append([]GoldenEvidence(nil), item.GoldEvidence...),
			Metadata:          metadata,
		}))
	}
	return candidates
}

func (s *Server) RunDeepAgentTemplateReplay(ctx context.Context, req DeepAgentTemplateReplayRequest) (DeepAgentTemplateReplayReport, error) {
	if s == nil || s.evaluation == nil {
		return DeepAgentTemplateReplayReport{}, fmt.Errorf("template replay requires evaluation store")
	}
	req.TemplateIDs = normalizeTemplateReplayIDs(req.TemplateIDs)
	sets := filterTemplateGoldenSets(DefaultDeepAgentTemplateGoldenSets(), req.TemplateIDs)
	if len(sets) == 0 {
		return DeepAgentTemplateReplayReport{}, fmt.Errorf("no template replay corpus matched request")
	}
	engine := NewEvaluationEngine(nil)
	runReports := make([]EvaluationRunReport, 0, len(sets))
	for _, set := range sets {
		if _, err := s.evaluation.UpsertGoldenSet(ctx, set); err != nil {
			return DeepAgentTemplateReplayReport{}, err
		}
		report, err := engine.EvaluateGolden(ctx, GoldenEvaluationRequest{
			Name:       set.ID + "_replay",
			Trigger:    DeepAgentTemplateReplayTrigger,
			Set:        set,
			Candidates: deepAgentTemplateReplayCandidates(set, req),
			Thresholds: req.Thresholds,
		})
		if err != nil {
			return DeepAgentTemplateReplayReport{}, err
		}
		report.Run.Scope.TemplateID = fmt.Sprint(set.Metadata["template_id"])
		report.Run.Metrics = mergeEvaluationMetricMaps(report.Run.Metrics, map[string]any{
			"template_id":          set.Metadata["template_id"],
			"planner_version":      firstNonEmptyString(req.PlannerVersion, "planner-current"),
			"router_version":       firstNonEmptyString(req.RouterVersion, "router-current"),
			"executor_version":     firstNonEmptyString(req.ExecutorVersion, "executor-current"),
			"verifier_version":     firstNonEmptyString(req.VerifierVersion, "verifier-current"),
			"eval_repair_disabled": !req.RepairEnabled,
		})
		if req.Persist {
			persisted, err := s.persistEvaluationRunReportContext(ctx, report)
			if err != nil {
				return DeepAgentTemplateReplayReport{}, err
			}
			report = persisted
		}
		runReports = append(runReports, report)
	}
	return DeepAgentTemplateReplayReport{
		Runs:      runReports,
		Dashboard: DeepAgentTemplateEvalDashboardFromReports(runReports),
		Corpus:    sets,
		Versions: map[string]string{
			"planner":  firstNonEmptyString(req.PlannerVersion, "planner-current"),
			"router":   firstNonEmptyString(req.RouterVersion, "router-current"),
			"executor": firstNonEmptyString(req.ExecutorVersion, "executor-current"),
			"verifier": firstNonEmptyString(req.VerifierVersion, "verifier-current"),
		},
	}, nil
}

func normalizeTemplateReplayIDs(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if id := normalizeDeepAgentTemplateID(value); id != "" {
			out = appendUniqueStrings(out, []string{id})
		}
	}
	return out
}

func filterTemplateGoldenSets(sets []GoldenSet, templateIDs []string) []GoldenSet {
	if len(templateIDs) == 0 {
		return sets
	}
	allowed := map[string]bool{}
	for _, id := range templateIDs {
		allowed[id] = true
	}
	out := make([]GoldenSet, 0, len(sets))
	for _, set := range sets {
		if allowed[fmt.Sprint(set.Metadata["template_id"])] {
			out = append(out, set)
		}
	}
	return out
}

func DeepAgentTemplateEvalDashboardFromReports(reports []EvaluationRunReport) DeepAgentTemplateEvalDashboard {
	results := []EvaluationResult{}
	for _, report := range reports {
		results = append(results, report.Results...)
	}
	return deepAgentTemplateEvalDashboardFromResults(results, "replay")
}

func BuildDeepAgentTemplateEvalDashboard(ctx context.Context, store EvaluationStore) (DeepAgentTemplateEvalDashboard, error) {
	if store == nil {
		return DeepAgentTemplateEvalDashboard{}, fmt.Errorf("evaluation store is required")
	}
	results, err := store.ListEvaluationResults(ctx, EvaluationResultFilter{
		SubjectType: EvaluationSubjectGoldenCase,
		Limit:       2000,
	})
	if err != nil {
		return DeepAgentTemplateEvalDashboard{}, err
	}
	return deepAgentTemplateEvalDashboardFromResults(results, "store"), nil
}

func deepAgentTemplateEvalDashboardFromResults(results []EvaluationResult, source string) DeepAgentTemplateEvalDashboard {
	type accum struct {
		row           DeepAgentTemplateEvalRow
		blocked       int
		actionTotal   float64
		actionCount   int
		durationTotal float64
		durationCount int
		reasonCounts  map[string]int
	}
	byTemplate := map[string]*accum{}
	for _, result := range results {
		templateID := firstNonEmptyString(metricString(result.Metrics, "template_id"), "unknown")
		if templateID == "unknown" {
			continue
		}
		item := byTemplate[templateID]
		if item == nil {
			item = &accum{reasonCounts: map[string]int{}}
			item.row.TemplateID = templateID
			item.row.TemplateName = metricString(result.Metrics, "template_name")
			item.row.Versions = map[string]string{}
			byTemplate[templateID] = item
		}
		item.row.Total++
		switch result.Status {
		case EvaluationResultStatusPassed:
			item.row.Passed++
		case EvaluationResultStatusFailed:
			item.row.Failed++
		case EvaluationResultStatusWarning:
			item.row.Warning++
		}
		if strings.EqualFold(metricString(result.Metrics, "expected_status"), "blocked") || strings.EqualFold(metricString(result.Metrics, "scenario"), "blocked") {
			item.blocked++
		}
		if value := mapFloat(result.Metrics, "action_count"); value > 0 {
			item.actionTotal += value
			item.actionCount++
		}
		if value := mapFloat(result.Metrics, "duration_ms"); value > 0 {
			item.durationTotal += value
			item.durationCount++
		}
		item.row.TokenEstimate += mapInt(result.Metrics, "token_estimate")
		item.row.EstimatedCostUSD = roundEvaluationCost(item.row.EstimatedCostUSD + mapFloat(result.Metrics, "estimated_cost_usd"))
		for _, key := range []string{"planner_version", "router_version", "executor_version", "verifier_version"} {
			if value := metricString(result.Metrics, key); value != "" {
				item.row.Versions[strings.TrimSuffix(key, "_version")] = value
			}
		}
		if reason := metricString(result.Metrics, "verifier_failure_reason"); reason != "" {
			item.reasonCounts[reason]++
		}
		for _, finding := range result.Findings {
			if finding.Code == "deep_agent_verifier_failed" || strings.Contains(finding.Code, "verifier") {
				item.reasonCounts[firstNonEmptyString(finding.Message, finding.Code)]++
			}
		}
	}
	rows := make([]DeepAgentTemplateEvalRow, 0, len(byTemplate))
	total := 0
	for _, item := range byTemplate {
		if item.row.Total > 0 {
			item.row.SuccessRate = float64(item.row.Passed) / float64(item.row.Total)
			item.row.BlockedRate = float64(item.blocked) / float64(item.row.Total)
		}
		if item.actionCount > 0 {
			item.row.AverageActionCount = item.actionTotal / float64(item.actionCount)
		}
		if item.durationCount > 0 {
			item.row.AverageDurationMS = item.durationTotal / float64(item.durationCount)
		}
		item.row.VerifierFailureReasons = sortedReasonRows(item.reasonCounts)
		rows = append(rows, item.row)
		total += item.row.Total
	}
	sort.SliceStable(rows, func(i, j int) bool {
		return rows[i].TemplateID < rows[j].TemplateID
	})
	return DeepAgentTemplateEvalDashboard{
		Total:       total,
		Templates:   rows,
		GeneratedAt: time.Now().UTC(),
		Source:      source,
	}
}

func sortedReasonRows(counts map[string]int) []map[string]any {
	type pair struct {
		reason string
		count  int
	}
	pairs := make([]pair, 0, len(counts))
	for reason, count := range counts {
		if strings.TrimSpace(reason) == "" {
			continue
		}
		pairs = append(pairs, pair{reason: reason, count: count})
	}
	sort.SliceStable(pairs, func(i, j int) bool {
		if pairs[i].count == pairs[j].count {
			return pairs[i].reason < pairs[j].reason
		}
		return pairs[i].count > pairs[j].count
	})
	out := make([]map[string]any, 0, len(pairs))
	for _, item := range pairs {
		out = append(out, map[string]any{"reason": item.reason, "count": item.count})
	}
	return out
}

func metricString(values map[string]any, key string) string {
	if values == nil {
		return ""
	}
	value, ok := values[key]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func DefaultEvalRepairTriggerPolicy(run EvaluationRun) EvalRepairTriggerPolicy {
	if mapBool(run.Metrics, "eval_repair_disabled") {
		return EvalRepairTriggerPolicy{Enabled: false}
	}
	return EvalRepairTriggerPolicy{
		Enabled:           !strings.Contains(strings.ToLower(strings.TrimSpace(run.Trigger)), "repair"),
		MaxTriggersPerRun: defaultEvalRepairMaxTriggersPerRun,
		AllowedSubjectTypes: []string{
			EvaluationSubjectDeepAgent,
			EvaluationSubjectGoldenCase,
			EvaluationSubjectJob,
			EvaluationSubjectSession,
			EvaluationSubjectSkillExecution,
		},
	}
}

func evalRepairResultAllowed(policy EvalRepairTriggerPolicy, result EvaluationResult, triggered int) bool {
	if !policy.Enabled {
		return false
	}
	maxTriggers := policy.MaxTriggersPerRun
	if maxTriggers <= 0 {
		maxTriggers = defaultEvalRepairMaxTriggersPerRun
	}
	if triggered >= maxTriggers {
		return false
	}
	if len(policy.AllowedSubjectTypes) == 0 {
		return true
	}
	for _, subject := range policy.AllowedSubjectTypes {
		if strings.EqualFold(strings.TrimSpace(subject), result.SubjectType) {
			return true
		}
	}
	return false
}

func mapBool(values map[string]any, key string) bool {
	switch typed := values[key].(type) {
	case bool:
		return typed
	case string:
		switch strings.ToLower(strings.TrimSpace(typed)) {
		case "1", "true", "yes", "y":
			return true
		}
	}
	return false
}
