package agentruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"

	"claude-codex/internal/harness/plannerapi"
	"claude-codex/internal/harness/state"
)

const (
	EvaluationSubjectGoldenCase = "golden_case"

	EvaluationMetricAnswerCorrectness = "answer_correctness"
	EvaluationMetricAnswerRelevancy   = "answer_relevancy"
	EvaluationMetricFaithfulness      = "faithfulness"
	EvaluationMetricContextPrecision  = "context_precision"
	EvaluationMetricContextRecall     = "context_recall"
)

type GoldenSet struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Version     string         `json:"version,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
	Cases       []GoldenCase   `json:"cases"`
	CreatedAt   time.Time      `json:"created_at,omitempty"`
	UpdatedAt   time.Time      `json:"updated_at,omitempty"`
}

type GoldenCase struct {
	ID             string           `json:"id"`
	Query          string           `json:"query"`
	ExpectedAnswer string           `json:"expected_answer,omitempty"`
	ExpectedFacts  []string         `json:"expected_facts,omitempty"`
	GoldEvidence   []GoldenEvidence `json:"gold_evidence,omitempty"`
	Tags           []string         `json:"tags,omitempty"`
	Metadata       map[string]any   `json:"metadata,omitempty"`
}

type GoldenEvidence struct {
	ID       string         `json:"id"`
	Content  string         `json:"content"`
	Source   string         `json:"source,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

type GoldenCandidate struct {
	CaseID            string           `json:"case_id"`
	Output            string           `json:"output"`
	RetrievedEvidence []GoldenEvidence `json:"retrieved_evidence,omitempty"`
	Metadata          map[string]any   `json:"metadata,omitempty"`
}

type GoldenEvaluationRequest struct {
	ID         string               `json:"id,omitempty"`
	Name       string               `json:"name,omitempty"`
	Trigger    string               `json:"trigger,omitempty"`
	Set        GoldenSet            `json:"set"`
	Candidates []GoldenCandidate    `json:"candidates"`
	Thresholds EvaluationThresholds `json:"thresholds,omitempty"`
}

type GoldenJudgeRequest struct {
	Set       GoldenSet       `json:"set"`
	Case      GoldenCase      `json:"case"`
	Candidate GoldenCandidate `json:"candidate"`
}

type GoldenJudgeResult struct {
	AnswerCorrectness float64             `json:"answer_correctness"`
	AnswerRelevancy   float64             `json:"answer_relevancy"`
	Faithfulness      float64             `json:"faithfulness"`
	ContextPrecision  float64             `json:"context_precision"`
	ContextRecall     float64             `json:"context_recall"`
	Findings          []EvaluationFinding `json:"findings,omitempty"`
	Metadata          map[string]any      `json:"metadata,omitempty"`
}

type GoldenJudge interface {
	JudgeGoldenCase(ctx context.Context, req GoldenJudgeRequest) (GoldenJudgeResult, error)
}

type HeuristicGoldenJudge struct{}

type PlannerGoldenJudge struct {
	Planner       plannerapi.Planner
	Model         string
	PromptVersion string
	SystemPrompt  string
}

func (e *EvaluationEngine) EvaluateGolden(ctx context.Context, req GoldenEvaluationRequest) (EvaluationRunReport, error) {
	req.Set = normalizeGoldenSet(req.Set)
	if len(req.Set.Cases) == 0 {
		return EvaluationRunReport{}, fmt.Errorf("golden set requires at least one case")
	}
	now := e.now()
	runID := strings.TrimSpace(req.ID)
	if runID == "" {
		runID = newEvaluationID("evalrun")
	}
	run := normalizeEvaluationRun(EvaluationRun{
		ID:          runID,
		Name:        firstNonEmptyString(strings.TrimSpace(req.Name), defaultGoldenEvaluationRunName(req.Set, now)),
		Status:      EvaluationRunStatusRunning,
		Trigger:     strings.TrimSpace(req.Trigger),
		Scope:       EvaluationScope{SubjectType: EvaluationSubjectGoldenCase},
		StartedAt:   now,
		Metrics:     map[string]any{"golden_set_id": strings.TrimSpace(req.Set.ID), "golden_set_version": strings.TrimSpace(req.Set.Version)},
		Summary:     strings.TrimSpace(req.Set.Description),
		CompletedAt: nil,
	})

	candidates := goldenCandidatesByCaseID(req.Candidates)
	results := make([]EvaluationResult, 0, len(req.Set.Cases))
	judge := e.goldenJudge()
	for _, item := range req.Set.Cases {
		item = normalizeGoldenCase(item)
		candidate, ok := candidates[item.ID]
		if !ok {
			results = append(results, missingGoldenCandidateResult(run.ID, req.Set, item, now))
			continue
		}
		result, err := evaluateGoldenCaseResult(ctx, judge, run.ID, req.Set, item, normalizeGoldenCandidate(candidate), now)
		if err != nil {
			return EvaluationRunReport{}, err
		}
		results = append(results, result)
	}

	aggregate := aggregateEvaluationMetrics(results)
	goldenMetrics := aggregateGoldenMetrics(results)
	completedAt := e.now()
	run.Status = EvaluationRunStatusCompleted
	run.CompletedAt = &completedAt
	run.Total = aggregate.Total
	run.Passed = aggregate.Passed
	run.Failed = aggregate.Failed
	run.Warning = aggregate.Warning
	run.Metrics = mergeEvaluationMetricMaps(evaluationAggregateMetricsMap(aggregate), goldenMetrics)
	run.Summary = goldenEvaluationSummaryText(run, req.Set)

	summary := summarizeEvaluationResults(run, results)
	return EvaluationRunReport{
		Run:     run,
		Results: results,
		Reviews: createEvaluationReviews(results),
		Summary: summary,
	}, nil
}

func normalizeGoldenSet(set GoldenSet) GoldenSet {
	set.ID = strings.TrimSpace(set.ID)
	if set.ID == "" {
		set.ID = stableEvaluationSubjectID(set.Name + "\n" + set.Description + "\n" + set.Version)
	}
	set.Name = truncateEvaluationString(strings.TrimSpace(set.Name), 256)
	if set.Name == "" {
		set.Name = set.ID
	}
	set.Description = truncateEvaluationString(strings.TrimSpace(set.Description), 4096)
	set.Version = truncateEvaluationString(strings.TrimSpace(set.Version), 128)
	if set.Version == "" {
		set.Version = "v1"
	}
	if set.Metadata == nil {
		set.Metadata = map[string]any{}
	}
	for index := range set.Cases {
		set.Cases[index] = normalizeGoldenCase(set.Cases[index])
	}
	now := time.Now().UTC()
	if set.CreatedAt.IsZero() {
		set.CreatedAt = now
	} else {
		set.CreatedAt = set.CreatedAt.UTC()
	}
	if set.UpdatedAt.IsZero() {
		set.UpdatedAt = set.CreatedAt
	} else {
		set.UpdatedAt = set.UpdatedAt.UTC()
	}
	return set
}

func (e *EvaluationEngine) goldenJudge() GoldenJudge {
	if e != nil && e.Judge != nil {
		return e.Judge
	}
	return HeuristicGoldenJudge{}
}

func (j HeuristicGoldenJudge) JudgeGoldenCase(_ context.Context, req GoldenJudgeRequest) (GoldenJudgeResult, error) {
	expectedFacts := normalizedGoldenExpectedFacts(req.Case)
	answerCorrectness := factCoverageScore(req.Candidate.Output, expectedFacts)
	answerRelevancy := tokenOverlapScore(req.Case.Query, req.Candidate.Output)
	faithfulness := faithfulnessScore(req.Candidate.Output, req.Candidate.RetrievedEvidence, req.Case.GoldEvidence)
	contextPrecision := contextPrecisionScore(req.Candidate.RetrievedEvidence, req.Case.GoldEvidence, expectedFacts)
	contextRecall := contextRecallScore(req.Candidate.RetrievedEvidence, req.Case.GoldEvidence, expectedFacts)
	return GoldenJudgeResult{
		AnswerCorrectness: answerCorrectness,
		AnswerRelevancy:   answerRelevancy,
		Faithfulness:      faithfulness,
		ContextPrecision:  contextPrecision,
		ContextRecall:     contextRecall,
		Findings:          goldenMetricFindings(answerCorrectness, answerRelevancy, faithfulness, contextPrecision, contextRecall),
		Metadata:          map[string]any{"judge": "heuristic"},
	}, nil
}

func (j PlannerGoldenJudge) JudgeGoldenCase(ctx context.Context, req GoldenJudgeRequest) (GoldenJudgeResult, error) {
	if j.Planner == nil {
		return GoldenJudgeResult{}, fmt.Errorf("planner golden judge requires planner")
	}
	session := &state.Session{
		ID:        "golden-judge",
		UserID:    "evaluation",
		StartedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
		Messages: []state.Message{
			{Role: state.MessageRoleSystem, Content: j.systemPrompt(), Hidden: true, CreatedAt: time.Now().UTC()},
			{Role: state.MessageRoleUser, Content: goldenJudgeUserPrompt(req), CreatedAt: time.Now().UTC()},
		},
	}
	plan, err := j.Planner.Next(ctx, session, nil)
	if err != nil {
		return GoldenJudgeResult{}, err
	}
	result, err := parseGoldenJudgeJSON(plan.AssistantText)
	if err != nil {
		return GoldenJudgeResult{}, err
	}
	if result.Metadata == nil {
		result.Metadata = map[string]any{}
	}
	result.Metadata["judge"] = "llm-as-judge"
	if model := strings.TrimSpace(j.Model); model != "" {
		result.Metadata["model"] = model
	}
	result.Metadata["prompt_version"] = j.promptVersion()
	return normalizeGoldenJudgeResult(result), nil
}

func evaluateGoldenCaseResult(ctx context.Context, judge GoldenJudge, runID string, set GoldenSet, item GoldenCase, candidate GoldenCandidate, now time.Time) (EvaluationResult, error) {
	judgement, err := judge.JudgeGoldenCase(ctx, GoldenJudgeRequest{Set: set, Case: item, Candidate: candidate})
	if err != nil {
		return EvaluationResult{}, fmt.Errorf("judge golden case %s: %w", item.ID, err)
	}
	judgement = normalizeGoldenJudgeResult(judgement)
	findings := normalizeEvaluationFindings(judgement.Findings)
	status := evaluationStatusFromFindings(findings)
	score := goldenCompositeScore(judgement)
	if status == EvaluationResultStatusPassed && score < 0.70 {
		status = EvaluationResultStatusWarning
	}
	if score < 0.45 {
		status = EvaluationResultStatusFailed
	}
	return normalizeEvaluationResult(EvaluationResult{
		RunID:       runID,
		SubjectType: EvaluationSubjectGoldenCase,
		SubjectID:   item.ID,
		Status:      status,
		Score:       score,
		Input:       item.Query,
		Output:      candidate.Output,
		Metrics:     goldenJudgeMetricsMap(judgement, set, item, candidate),
		Findings:    findings,
		CreatedAt:   now,
	}), nil
}

func missingGoldenCandidateResult(runID string, set GoldenSet, item GoldenCase, now time.Time) EvaluationResult {
	return normalizeEvaluationResult(EvaluationResult{
		RunID:       runID,
		SubjectType: EvaluationSubjectGoldenCase,
		SubjectID:   item.ID,
		Status:      EvaluationResultStatusFailed,
		Score:       0,
		Input:       item.Query,
		Metrics: map[string]any{
			"golden_set_id":      strings.TrimSpace(set.ID),
			"golden_set_version": strings.TrimSpace(set.Version),
			"case_tags":          item.Tags,
		},
		Findings: []EvaluationFinding{{
			Severity: "error",
			Code:     "golden_candidate_missing",
			Message:  "golden case has no candidate output",
		}},
		CreatedAt: now,
	})
}

func goldenJudgeMetricsMap(result GoldenJudgeResult, set GoldenSet, item GoldenCase, candidate GoldenCandidate) map[string]any {
	values := map[string]any{
		EvaluationMetricAnswerCorrectness: result.AnswerCorrectness,
		EvaluationMetricAnswerRelevancy:   result.AnswerRelevancy,
		EvaluationMetricFaithfulness:      result.Faithfulness,
		EvaluationMetricContextPrecision:  result.ContextPrecision,
		EvaluationMetricContextRecall:     result.ContextRecall,
		"golden_set_id":                   strings.TrimSpace(set.ID),
		"golden_set_version":              strings.TrimSpace(set.Version),
		"case_tags":                       item.Tags,
		"retrieved_evidence_count":        len(candidate.RetrievedEvidence),
	}
	for key, value := range result.Metadata {
		if strings.TrimSpace(key) == "" {
			continue
		}
		values["judge_"+key] = value
	}
	return values
}

func goldenJudgeSystemPrompt() string {
	return goldenJudgeSystemPromptForVersion(DefaultGoldenJudgePromptVersion)
}

const DefaultGoldenJudgePromptVersion = "ragas-json-v1"

func goldenJudgeSystemPromptForVersion(version string) string {
	return `You are an impartial RAG evaluation judge.
Score the candidate answer from 0 to 1 on:
- answer_correctness: does it cover the expected answer/facts?
- answer_relevancy: does it answer the query?
- faithfulness: is it supported by retrieved evidence?
- context_precision: is retrieved evidence relevant?
- context_recall: did retrieval cover gold evidence/facts?

Return only JSON with numeric fields answer_correctness, answer_relevancy, faithfulness, context_precision, context_recall, and optional findings.`
}

func (j PlannerGoldenJudge) systemPrompt() string {
	if strings.TrimSpace(j.SystemPrompt) != "" {
		return strings.TrimSpace(j.SystemPrompt)
	}
	return goldenJudgeSystemPromptForVersion(j.promptVersion())
}

func (j PlannerGoldenJudge) promptVersion() string {
	if strings.TrimSpace(j.PromptVersion) != "" {
		return strings.TrimSpace(j.PromptVersion)
	}
	return DefaultGoldenJudgePromptVersion
}

func goldenJudgeUserPrompt(req GoldenJudgeRequest) string {
	payload := map[string]any{
		"query":              req.Case.Query,
		"expected_answer":    req.Case.ExpectedAnswer,
		"expected_facts":     req.Case.ExpectedFacts,
		"gold_evidence":      req.Case.GoldEvidence,
		"candidate_answer":   req.Candidate.Output,
		"retrieved_evidence": req.Candidate.RetrievedEvidence,
	}
	raw, _ := json.MarshalIndent(payload, "", "  ")
	return string(raw)
}

func parseGoldenJudgeJSON(text string) (GoldenJudgeResult, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return GoldenJudgeResult{}, fmt.Errorf("empty judge response")
	}
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start < 0 || end < start {
		return GoldenJudgeResult{}, fmt.Errorf("judge response did not contain JSON")
	}
	var result GoldenJudgeResult
	if err := json.Unmarshal([]byte(text[start:end+1]), &result); err != nil {
		return GoldenJudgeResult{}, fmt.Errorf("parse judge response: %w", err)
	}
	return result, nil
}

func goldenMetricFindings(answerCorrectness, answerRelevancy, faithfulness, contextPrecision, contextRecall float64) []EvaluationFinding {
	type check struct {
		key       string
		value     float64
		threshold float64
		severity  string
	}
	checks := []check{
		{EvaluationMetricAnswerCorrectness, answerCorrectness, 0.75, "error"},
		{EvaluationMetricFaithfulness, faithfulness, 0.70, "error"},
		{EvaluationMetricContextRecall, contextRecall, 0.70, "warning"},
		{EvaluationMetricContextPrecision, contextPrecision, 0.50, "warning"},
		{EvaluationMetricAnswerRelevancy, answerRelevancy, 0.20, "warning"},
	}
	findings := make([]EvaluationFinding, 0)
	for _, item := range checks {
		if item.value >= item.threshold {
			continue
		}
		findings = append(findings, EvaluationFinding{
			Severity: item.severity,
			Code:     item.key + "_low",
			Message:  fmt.Sprintf("%s score %.2f below threshold %.2f", item.key, item.value, item.threshold),
			Metadata: map[string]any{"score": item.value, "threshold": item.threshold},
		})
	}
	return findings
}

func aggregateGoldenMetrics(results []EvaluationResult) map[string]any {
	keys := []string{
		EvaluationMetricAnswerCorrectness,
		EvaluationMetricAnswerRelevancy,
		EvaluationMetricFaithfulness,
		EvaluationMetricContextPrecision,
		EvaluationMetricContextRecall,
	}
	out := map[string]any{}
	for _, key := range keys {
		var total float64
		var count int
		for _, result := range results {
			if result.Metrics == nil {
				continue
			}
			value := mapFloat(result.Metrics, key)
			if value == 0 && result.Metrics[key] == nil {
				continue
			}
			total += value
			count++
		}
		if count > 0 {
			out[key+"_avg"] = roundEvaluationScore(total / float64(count))
		}
	}
	return out
}

func goldenCompositeScore(result GoldenJudgeResult) float64 {
	score := result.AnswerCorrectness*0.35 +
		result.Faithfulness*0.25 +
		result.ContextRecall*0.15 +
		result.ContextPrecision*0.15 +
		result.AnswerRelevancy*0.10
	return roundEvaluationScore(score)
}

func normalizeGoldenJudgeResult(result GoldenJudgeResult) GoldenJudgeResult {
	result.AnswerCorrectness = clampScore(result.AnswerCorrectness)
	result.AnswerRelevancy = clampScore(result.AnswerRelevancy)
	result.Faithfulness = clampScore(result.Faithfulness)
	result.ContextPrecision = clampScore(result.ContextPrecision)
	result.ContextRecall = clampScore(result.ContextRecall)
	result.Findings = normalizeEvaluationFindings(result.Findings)
	if result.Metadata == nil {
		result.Metadata = map[string]any{}
	}
	return result
}

func goldenEvaluationSummaryText(run EvaluationRun, set GoldenSet) string {
	return fmt.Sprintf(
		"Evaluated %d golden case(s) from %s%s: pass_rate=%.2f answer_correctness=%.2f faithfulness=%.2f context_precision=%.2f context_recall=%.2f",
		run.Total,
		firstNonEmptyString(strings.TrimSpace(set.Name), strings.TrimSpace(set.ID), "golden set"),
		goldenSetVersionSuffix(set.Version),
		mapFloat(run.Metrics, "success_rate"),
		mapFloat(run.Metrics, EvaluationMetricAnswerCorrectness+"_avg"),
		mapFloat(run.Metrics, EvaluationMetricFaithfulness+"_avg"),
		mapFloat(run.Metrics, EvaluationMetricContextPrecision+"_avg"),
		mapFloat(run.Metrics, EvaluationMetricContextRecall+"_avg"),
	)
}

func defaultGoldenEvaluationRunName(set GoldenSet, now time.Time) string {
	name := strings.TrimSpace(set.Name)
	if name == "" {
		name = "golden"
	}
	return sanitizeEvaluationName(name) + "_golden_" + now.UTC().Format("20060102T150405Z")
}

func goldenSetVersionSuffix(version string) string {
	version = strings.TrimSpace(version)
	if version == "" {
		return ""
	}
	return "@" + version
}

func goldenCandidatesByCaseID(candidates []GoldenCandidate) map[string]GoldenCandidate {
	out := make(map[string]GoldenCandidate, len(candidates))
	for _, candidate := range candidates {
		candidate = normalizeGoldenCandidate(candidate)
		if candidate.CaseID == "" {
			continue
		}
		out[candidate.CaseID] = candidate
	}
	return out
}

func normalizeGoldenCase(item GoldenCase) GoldenCase {
	item.ID = strings.TrimSpace(item.ID)
	if item.ID == "" {
		item.ID = stableEvaluationSubjectID(item.Query)
	}
	item.Query = strings.TrimSpace(item.Query)
	item.ExpectedAnswer = strings.TrimSpace(item.ExpectedAnswer)
	item.ExpectedFacts = normalizeNonEmptyStrings(item.ExpectedFacts)
	for index := range item.GoldEvidence {
		item.GoldEvidence[index] = normalizeGoldenEvidence(item.GoldEvidence[index])
	}
	item.Tags = normalizeNonEmptyStrings(item.Tags)
	if item.Metadata == nil {
		item.Metadata = map[string]any{}
	}
	return item
}

func normalizeGoldenCandidate(candidate GoldenCandidate) GoldenCandidate {
	candidate.CaseID = strings.TrimSpace(candidate.CaseID)
	candidate.Output = strings.TrimSpace(candidate.Output)
	for index := range candidate.RetrievedEvidence {
		candidate.RetrievedEvidence[index] = normalizeGoldenEvidence(candidate.RetrievedEvidence[index])
	}
	if candidate.Metadata == nil {
		candidate.Metadata = map[string]any{}
	}
	return candidate
}

func normalizeGoldenEvidence(evidence GoldenEvidence) GoldenEvidence {
	evidence.ID = strings.TrimSpace(evidence.ID)
	evidence.Content = strings.TrimSpace(evidence.Content)
	evidence.Source = strings.TrimSpace(evidence.Source)
	if evidence.ID == "" {
		evidence.ID = stableEvaluationSubjectID(evidence.Source + "\n" + evidence.Content)
	}
	if evidence.Metadata == nil {
		evidence.Metadata = map[string]any{}
	}
	return evidence
}

func normalizedGoldenExpectedFacts(item GoldenCase) []string {
	facts := normalizeNonEmptyStrings(item.ExpectedFacts)
	if len(facts) == 0 && strings.TrimSpace(item.ExpectedAnswer) != "" {
		facts = append(facts, item.ExpectedAnswer)
	}
	return facts
}

func factCoverageScore(output string, facts []string) float64 {
	if len(facts) == 0 {
		if strings.TrimSpace(output) == "" {
			return 0
		}
		return 1
	}
	var matched int
	for _, fact := range facts {
		if textContainsMeaning(output, fact) {
			matched++
		}
	}
	return roundEvaluationScore(float64(matched) / float64(len(facts)))
}

func tokenOverlapScore(left, right string) float64 {
	leftTokens := meaningfulTokens(left)
	rightTokens := meaningfulTokens(right)
	if len(leftTokens) == 0 || len(rightTokens) == 0 {
		return 0
	}
	rightSet := map[string]bool{}
	for _, token := range rightTokens {
		rightSet[token] = true
	}
	var matched int
	for _, token := range leftTokens {
		if rightSet[token] {
			matched++
		}
	}
	return roundEvaluationScore(float64(matched) / float64(len(leftTokens)))
}

func faithfulnessScore(output string, retrieved, gold []GoldenEvidence) float64 {
	output = strings.TrimSpace(output)
	if output == "" {
		return 0
	}
	evidenceText := goldenEvidenceText(append(append([]GoldenEvidence{}, retrieved...), gold...))
	if strings.TrimSpace(evidenceText) == "" {
		return 0
	}
	sentences := splitEvaluationSentences(output)
	if len(sentences) == 0 {
		sentences = []string{output}
	}
	var supported int
	for _, sentence := range sentences {
		if tokenOverlapScore(sentence, evidenceText) >= 0.35 || textContainsMeaning(evidenceText, sentence) {
			supported++
		}
	}
	return roundEvaluationScore(float64(supported) / float64(len(sentences)))
}

func contextPrecisionScore(retrieved, gold []GoldenEvidence, facts []string) float64 {
	if len(retrieved) == 0 {
		return 0
	}
	var relevant int
	for _, item := range retrieved {
		if goldenEvidenceRelevant(item, gold, facts) {
			relevant++
		}
	}
	return roundEvaluationScore(float64(relevant) / float64(len(retrieved)))
}

func contextRecallScore(retrieved, gold []GoldenEvidence, facts []string) float64 {
	if len(gold) > 0 {
		var matched int
		for _, item := range gold {
			if goldenEvidenceCovered(item, retrieved) {
				matched++
			}
		}
		return roundEvaluationScore(float64(matched) / float64(len(gold)))
	}
	if len(facts) == 0 {
		if len(retrieved) > 0 {
			return 1
		}
		return 0
	}
	return factCoverageScore(goldenEvidenceText(retrieved), facts)
}

func goldenEvidenceRelevant(item GoldenEvidence, gold []GoldenEvidence, facts []string) bool {
	if goldenEvidenceCovered(item, gold) {
		return true
	}
	for _, fact := range facts {
		if textContainsMeaning(item.Content, fact) {
			return true
		}
	}
	return false
}

func goldenEvidenceCovered(target GoldenEvidence, candidates []GoldenEvidence) bool {
	target = normalizeGoldenEvidence(target)
	if target.ID != "" {
		for _, candidate := range candidates {
			candidate = normalizeGoldenEvidence(candidate)
			if candidate.ID == target.ID {
				return true
			}
		}
	}
	for _, candidate := range candidates {
		if tokenOverlapScore(target.Content, candidate.Content) >= 0.60 || textContainsMeaning(candidate.Content, target.Content) || textContainsMeaning(target.Content, candidate.Content) {
			return true
		}
	}
	return false
}

func goldenEvidenceText(items []GoldenEvidence) string {
	parts := make([]string, 0, len(items))
	for _, item := range items {
		if text := strings.TrimSpace(item.Content); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n")
}

func textContainsMeaning(haystack, needle string) bool {
	haystack = normalizeEvaluationText(haystack)
	needle = normalizeEvaluationText(needle)
	if needle == "" || haystack == "" {
		return false
	}
	if strings.Contains(haystack, needle) {
		return true
	}
	return tokenOverlapScore(haystack, needle) >= 0.75
}

func splitEvaluationSentences(text string) []string {
	parts := regexp.MustCompile(`[。！？!?；;\n]+`).Split(text, -1)
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func meaningfulTokens(text string) []string {
	text = strings.ToLower(strings.TrimSpace(text))
	if text == "" {
		return nil
	}
	raw := regexp.MustCompile(`[^\p{Han}\p{L}\p{N}]+`).Split(text, -1)
	seen := map[string]bool{}
	out := make([]string, 0, len(raw))
	for _, token := range raw {
		token = strings.TrimSpace(token)
		if token == "" || len(token) <= 1 && !containsHan(token) {
			continue
		}
		if containsHan(token) {
			for _, r := range token {
				if r < 0x3400 || r > 0x9fff {
					continue
				}
				part := string(r)
				if !seen[part] {
					seen[part] = true
					out = append(out, part)
				}
			}
			continue
		}
		if !seen[token] {
			seen[token] = true
			out = append(out, token)
		}
	}
	sort.Strings(out)
	return out
}

func normalizeEvaluationText(text string) string {
	text = strings.ToLower(strings.TrimSpace(text))
	text = regexp.MustCompile(`[\s\p{P}\p{S}]+`).ReplaceAllString(text, "")
	return text
}

func containsHan(text string) bool {
	for _, r := range text {
		if r >= 0x3400 && r <= 0x9fff {
			return true
		}
	}
	return false
}

func normalizeNonEmptyStrings(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func mergeEvaluationMetricMaps(left, right map[string]any) map[string]any {
	out := map[string]any{}
	for key, value := range left {
		out[key] = value
	}
	for key, value := range right {
		out[key] = value
	}
	return out
}

func clampScore(value float64) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0
	}
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return roundEvaluationScore(value)
}

func roundEvaluationScore(value float64) float64 {
	return math.Round(value*1000) / 1000
}

func sanitizeEvaluationName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = regexp.MustCompile(`[^a-z0-9一-龥]+`).ReplaceAllString(value, "_")
	value = strings.Trim(value, "_")
	if value == "" {
		return "evaluation"
	}
	return value
}

func stableEvaluationSubjectID(value string) string {
	value = normalizeEvaluationText(value)
	if value == "" {
		return newEvaluationID("golden")
	}
	runes := []rune(value)
	if len(runes) > 48 {
		value = string(runes[:48])
	}
	return "golden-" + value
}
