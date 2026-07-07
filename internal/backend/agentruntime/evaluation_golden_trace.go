package agentruntime

import (
	"context"
	"fmt"
	"strings"

	"claude-codex/internal/harness/state"
)

type GoldenTraceCaptureRequest struct {
	SetID              string          `json:"set_id,omitempty"`
	SourceVersion      string          `json:"source_version,omitempty"`
	TargetVersion      string          `json:"target_version,omitempty"`
	Scope              EvaluationScope `json:"scope"`
	SubjectID          string          `json:"subject_id,omitempty"`
	EvaluationRunID    string          `json:"evaluation_run_id,omitempty"`
	EvaluationResultID string          `json:"evaluation_result_id,omitempty"`
	EvaluationStatus   string          `json:"evaluation_result_status,omitempty"`
	ExpectedAnswer     string          `json:"expected_answer,omitempty"`
	ExpectedFacts      []string        `json:"expected_facts,omitempty"`
	Tags               []string        `json:"tags,omitempty"`
	MaxCases           int             `json:"max_cases,omitempty"`
}

func BuildGoldenCasesFromRuntimeTraces(ctx context.Context, source EvaluationTraceSource, req GoldenTraceCaptureRequest) ([]GoldenCase, error) {
	if source == nil {
		return nil, fmt.Errorf("evaluation trace source is required")
	}
	scope := normalizeEvaluationScope(req.Scope)
	traces, err := source.ListEvaluationTraces(ctx, scope)
	if err != nil {
		return nil, err
	}
	subjectID := strings.TrimSpace(req.SubjectID)
	limit := req.MaxCases
	if limit <= 0 {
		limit = 1
	}
	if limit > 100 {
		limit = 100
	}
	out := make([]GoldenCase, 0, limit)
	for _, trace := range traces {
		if subjectID != "" && trace.SubjectID != subjectID && trace.JobID != subjectID && trace.SessionID != subjectID {
			continue
		}
		item, ok := goldenCaseFromTrace(trace, req)
		if !ok {
			continue
		}
		out = append(out, item)
		if len(out) >= limit {
			break
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no runtime trace matched golden case capture request")
	}
	return out, nil
}

func goldenCaseFromTrace(trace EvaluationTrace, req GoldenTraceCaptureRequest) (GoldenCase, bool) {
	query := strings.TrimSpace(trace.Input)
	answer := firstNonEmptyString(strings.TrimSpace(req.ExpectedAnswer), strings.TrimSpace(trace.Output))
	if query == "" || answer == "" {
		return GoldenCase{}, false
	}
	tags := normalizeNonEmptyStrings(append([]string{"runtime_trace"}, req.Tags...))
	facts := normalizeNonEmptyStrings(req.ExpectedFacts)
	if len(facts) == 0 {
		facts = []string{answer}
	}
	item := GoldenCase{
		ID:             goldenCaseTraceID(trace),
		Query:          query,
		ExpectedAnswer: answer,
		ExpectedFacts:  facts,
		GoldEvidence:   goldenEvidenceFromTrace(trace),
		Tags:           tags,
		Metadata: map[string]any{
			"source":       "runtime_trace",
			"subject_type": trace.SubjectType,
			"subject_id":   trace.SubjectID,
			"user_id":      trace.UserID,
			"session_id":   trace.SessionID,
			"job_id":       trace.JobID,
			"skill_name":   trace.SkillName,
			"provider":     trace.Provider,
			"model":        trace.Model,
		},
	}
	if trace.CompletedAt != nil {
		item.Metadata["completed_at"] = trace.CompletedAt.UTC().Format("2006-01-02T15:04:05Z07:00")
	}
	if !trace.CreatedAt.IsZero() {
		item.Metadata["created_at"] = trace.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00")
	}
	return normalizeGoldenCase(item), true
}

func goldenCaseTraceID(trace EvaluationTrace) string {
	raw := strings.Join([]string{"trace", trace.SubjectType, trace.SubjectID, trace.JobID, trace.SessionID}, "|")
	return stableEvaluationSubjectID(raw)
}

func goldenEvidenceFromTrace(trace EvaluationTrace) []GoldenEvidence {
	out := make([]GoldenEvidence, 0, 5)
	add := func(id, content, source string, metadata map[string]any) {
		content = strings.TrimSpace(content)
		if content == "" || len(out) >= 5 {
			return
		}
		out = append(out, normalizeGoldenEvidence(GoldenEvidence{
			ID:       id,
			Content:  truncateEvaluationString(content, 4096),
			Source:   source,
			Metadata: metadata,
		}))
	}
	for _, message := range trace.Messages {
		if message.Role != state.MessageRoleTool {
			continue
		}
		add(message.ID, message.ToolOutput, firstNonEmptyString(message.ToolName, "tool"), map[string]any{"tool_call_id": message.ToolCallID})
	}
	for _, event := range trace.JobEvents {
		if event == nil || strings.TrimSpace(event.Event.Content) == "" {
			continue
		}
		add(event.ID, event.Event.Content, "job_event:"+event.Type, map[string]any{"job_id": event.JobID})
	}
	return out
}

func BuildGoldenCasesFromEvaluationResults(ctx context.Context, store EvaluationStore, req GoldenTraceCaptureRequest) ([]GoldenCase, error) {
	if store == nil {
		return nil, fmt.Errorf("evaluation store is required")
	}
	filter := EvaluationResultFilter{
		RunID:         strings.TrimSpace(req.EvaluationRunID),
		Status:        strings.TrimSpace(req.EvaluationStatus),
		SubjectType:   strings.TrimSpace(req.Scope.SubjectType),
		UserID:        strings.TrimSpace(req.Scope.UserID),
		SessionID:     strings.TrimSpace(req.Scope.SessionID),
		JobID:         strings.TrimSpace(req.Scope.JobID),
		SkillName:     strings.TrimSpace(req.Scope.SkillName),
		Provider:      strings.TrimSpace(req.Scope.Provider),
		Model:         strings.TrimSpace(req.Scope.Model),
		PromptID:      strings.TrimSpace(req.Scope.PromptID),
		PromptVersion: strings.TrimSpace(req.Scope.PromptVersion),
		PromptHash:    strings.TrimSpace(req.Scope.PromptHash),
		ExperimentID:  strings.TrimSpace(req.Scope.ExperimentID),
		VariantID:     strings.TrimSpace(req.Scope.VariantID),
		Limit:         req.MaxCases,
	}
	if filter.Limit <= 0 {
		filter.Limit = 1
	}
	if filter.Limit > 100 {
		filter.Limit = 100
	}
	results, err := store.ListEvaluationResults(ctx, filter)
	if err != nil {
		return nil, err
	}
	resultID := strings.TrimSpace(req.EvaluationResultID)
	subjectID := strings.TrimSpace(req.SubjectID)
	out := make([]GoldenCase, 0, filter.Limit)
	for _, result := range results {
		if resultID != "" && result.ID != resultID {
			continue
		}
		if subjectID != "" && result.SubjectID != subjectID && result.JobID != subjectID && result.SessionID != subjectID {
			continue
		}
		if result.Status != EvaluationResultStatusFailed && result.Status != EvaluationResultStatusWarning {
			continue
		}
		item, ok := goldenCaseFromEvaluationResult(result, req)
		if !ok {
			continue
		}
		out = append(out, item)
		if len(out) >= filter.Limit {
			break
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no failed or warning evaluation result matched golden case capture request")
	}
	return out, nil
}

func goldenCaseFromEvaluationResult(result EvaluationResult, req GoldenTraceCaptureRequest) (GoldenCase, bool) {
	query := strings.TrimSpace(result.Input)
	answer := firstNonEmptyString(strings.TrimSpace(req.ExpectedAnswer), strings.TrimSpace(result.Output))
	if query == "" || answer == "" {
		return GoldenCase{}, false
	}
	facts := normalizeNonEmptyStrings(req.ExpectedFacts)
	if len(facts) == 0 {
		facts = []string{answer}
	}
	item := GoldenCase{
		ID:             stableEvaluationSubjectID(strings.Join([]string{"eval_result", result.ID}, "|")),
		Query:          query,
		ExpectedAnswer: answer,
		ExpectedFacts:  facts,
		GoldEvidence:   goldenEvidenceFromEvaluationResult(result),
		Tags:           normalizeNonEmptyStrings(append([]string{"eval_result", "badcase"}, req.Tags...)),
		Metadata: map[string]any{
			"source":         "evaluation_result",
			"result_id":      result.ID,
			"run_id":         result.RunID,
			"subject_type":   result.SubjectType,
			"subject_id":     result.SubjectID,
			"user_id":        result.UserID,
			"session_id":     result.SessionID,
			"job_id":         result.JobID,
			"skill_name":     result.SkillName,
			"provider":       result.Provider,
			"model":          result.Model,
			"prompt_id":      result.PromptID,
			"prompt_version": result.PromptVersion,
			"prompt_hash":    result.PromptHash,
			"status":         result.Status,
			"score":          result.Score,
		},
	}
	if !result.CreatedAt.IsZero() {
		item.Metadata["created_at"] = result.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00")
	}
	return normalizeGoldenCase(item), true
}

func goldenEvidenceFromEvaluationResult(result EvaluationResult) []GoldenEvidence {
	out := make([]GoldenEvidence, 0, len(result.Findings)+1)
	for idx, finding := range result.Findings {
		content := strings.TrimSpace(finding.Message)
		if content == "" {
			continue
		}
		out = append(out, normalizeGoldenEvidence(GoldenEvidence{
			ID:      fmt.Sprintf("%s-finding-%d", result.ID, idx+1),
			Content: truncateEvaluationString(content, 4096),
			Source:  "evaluation_finding:" + firstNonEmptyString(finding.Code, finding.Severity, "finding"),
			Metadata: map[string]any{
				"severity": finding.Severity,
				"code":     finding.Code,
			},
		}))
	}
	if len(out) == 0 && strings.TrimSpace(result.Output) != "" {
		out = append(out, normalizeGoldenEvidence(GoldenEvidence{
			ID:      result.ID + "-output",
			Content: truncateEvaluationString(result.Output, 4096),
			Source:  "evaluation_output",
		}))
	}
	return out
}

func upsertGoldenCases(existing, next []GoldenCase) []GoldenCase {
	byID := make(map[string]int, len(existing))
	out := make([]GoldenCase, 0, len(existing)+len(next))
	for _, item := range existing {
		item = normalizeGoldenCase(item)
		byID[item.ID] = len(out)
		out = append(out, item)
	}
	for _, item := range next {
		item = normalizeGoldenCase(item)
		if index, ok := byID[item.ID]; ok {
			out[index] = item
			continue
		}
		byID[item.ID] = len(out)
		out = append(out, item)
	}
	return out
}
