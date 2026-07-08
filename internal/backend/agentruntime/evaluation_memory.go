package agentruntime

import (
	"context"
	"fmt"
	"strings"

	"claude-codex/internal/harness/state"
)

const defaultMemoryEvalSetID = "memory-golden"

type MemoryEvaluationRunRequest struct {
	ID         string               `json:"id"`
	Name       string               `json:"name"`
	Trigger    string               `json:"trigger"`
	SetID      string               `json:"set_id"`
	SetVersion string               `json:"set_version"`
	UserID     string               `json:"user_id"`
	Judge      string               `json:"judge"`
	Cleanup    *bool                `json:"cleanup,omitempty"`
	Thresholds EvaluationThresholds `json:"thresholds"`
}

type memoryEvaluationInput struct {
	Set        GoldenSet
	Candidates []GoldenCandidate
	UserID     string
	Cleanup    bool
}

func (s *Server) buildMemoryEvaluationInput(ctx context.Context, req MemoryEvaluationRunRequest) (memoryEvaluationInput, error) {
	if s == nil || s.runtime == nil || s.runtime.memory == nil {
		return memoryEvaluationInput{}, fmt.Errorf("memory service is not configured")
	}
	setID := strings.TrimSpace(req.SetID)
	if setID == "" {
		setID = defaultMemoryEvalSetID
	}
	set, err := s.getGoldenSetVersion(ctx, setID, req.SetVersion)
	if err != nil {
		return memoryEvaluationInput{}, err
	}
	set = normalizeGoldenSet(set)
	userID := strings.TrimSpace(req.UserID)
	cleanup := false
	if userID == "" {
		userID = "eval-memory-" + newEvaluationID("run")
		cleanup = true
	}
	if req.Cleanup != nil {
		cleanup = *req.Cleanup
	}
	candidates := make([]GoldenCandidate, 0, len(set.Cases))
	for _, item := range set.Cases {
		candidate, err := s.evaluateMemoryGoldenCase(ctx, userID, normalizeGoldenCase(item))
		if err != nil {
			return memoryEvaluationInput{}, err
		}
		candidates = append(candidates, candidate)
	}
	return memoryEvaluationInput{Set: set, Candidates: candidates, UserID: userID, Cleanup: cleanup}, nil
}

func (s *Server) evaluateMemoryGoldenCase(ctx context.Context, userID string, item GoldenCase) (GoldenCandidate, error) {
	sessionID := firstNonEmptyString(memoryCaseMetadataString(item, "session_id"), "memory-eval-"+item.ID)
	setup := memoryCaseSetupMemories(item)
	for index, content := range setup {
		mem := newConversationMemoryItem(userID, sessionID, content)
		mem.ID = stableEvaluationSubjectID(strings.Join([]string{"memory", item.ID, fmt.Sprint(index), content}, "\n"))
		mem.Status = MemoryStatusActive
		mem.Source = MemorySourceSystem
		mem.Metadata = map[string]any{
			"eval_case_id": item.ID,
			"eval_seed":    true,
		}
		if service, ok := s.runtime.memory.(MemoryItemService); ok {
			if _, err := service.UpdateMemoryItem(ctx, userID, mem); err != nil {
				return GoldenCandidate{}, err
			}
		}
	}

	captured, err := s.captureMemoryEvalTurn(ctx, userID, sessionID, item)
	if err != nil {
		return GoldenCandidate{}, err
	}

	query := firstNonEmptyString(memoryCaseMetadataString(item, "user_message"), item.Query)
	session := state.NewSession("")
	session.ID = sessionID + "-query"
	session.UserID = userID
	session.AddUserMessage(query)
	if err := s.runtime.injectMemory(ctx, userID, session); err != nil {
		return GoldenCandidate{}, err
	}
	contextText := memoryInjectedContextText(session)
	retrieved := s.memoryEvidenceForSession(ctx, userID, session.ID, contextText)
	expectedFacts := memoryCaseExpectedFacts(item)
	forbidden := memoryCaseForbiddenFacts(item)
	metrics := memoryEvaluationMetrics(contextText, captured, retrieved, expectedFacts, forbidden)
	output := formatMemoryEvaluationOutput(captured, contextText, metrics)
	return GoldenCandidate{
		CaseID:            item.ID,
		Output:            output,
		RetrievedEvidence: retrieved,
		Metadata: map[string]any{
			"source":                   "admin_memory_eval",
			"memory_eval":              true,
			"memory_user_id":           userID,
			"memory_session_id":        session.ID,
			"memory_seed_count":        len(setup),
			"memory_captured_count":    len(captured),
			"memory_context_recall":    metrics.ContextRecall,
			"memory_capture_recall":    metrics.CaptureRecall,
			"memory_forbidden_leak":    metrics.ForbiddenLeak,
			"namespace_isolation_pass": metrics.NamespaceIsolationPass,
		},
	}, nil
}

func (s *Server) captureMemoryEvalTurn(ctx context.Context, userID, sessionID string, item GoldenCase) ([]MemoryItem, error) {
	userMessage := memoryCaseMetadataString(item, "capture_user_message")
	assistantMessage := memoryCaseMetadataString(item, "capture_assistant_message")
	if strings.TrimSpace(userMessage) == "" && strings.TrimSpace(assistantMessage) == "" {
		return nil, nil
	}
	before := map[string]bool{}
	if service, ok := s.runtime.memory.(MemoryItemService); ok {
		items, err := service.ListMemoryItems(ctx, userID, MemoryItemFilter{Status: ""})
		if err != nil {
			return nil, err
		}
		for _, existing := range items {
			before[existing.ID] = true
		}
	}
	session := state.NewSession("")
	session.ID = sessionID + "-capture"
	session.UserID = userID
	session.AddUserMessage(userMessage)
	session.AddAssistantMessage(firstNonEmptyString(assistantMessage, "noted"))
	if err := s.runtime.memory.AfterTurn(ctx, userID, session); err != nil {
		return nil, err
	}
	service, ok := s.runtime.memory.(MemoryItemService)
	if !ok {
		return nil, nil
	}
	items, err := service.ListMemoryItems(ctx, userID, MemoryItemFilter{Status: MemoryStatusActive})
	if err != nil {
		return nil, err
	}
	captured := make([]MemoryItem, 0)
	for _, item := range items {
		if before[item.ID] {
			continue
		}
		captured = append(captured, item)
	}
	return captured, nil
}

func (s *Server) memoryEvidenceForSession(ctx context.Context, userID, sessionID, contextText string) []GoldenEvidence {
	service, ok := s.runtime.memory.(MemoryItemService)
	if !ok {
		if strings.TrimSpace(contextText) == "" {
			return nil
		}
		return []GoldenEvidence{{ID: stableEvaluationSubjectID(contextText), Content: contextText, Source: "memory_context"}}
	}
	items, err := service.ListMemoryItems(ctx, userID, MemoryItemFilter{Status: MemoryStatusActive})
	if err != nil {
		return nil
	}
	out := make([]GoldenEvidence, 0)
	for _, item := range items {
		if fmt.Sprint(item.Metadata["last_injected_session_id"]) != sessionID {
			continue
		}
		out = append(out, GoldenEvidence{
			ID:      item.ID,
			Content: item.Content,
			Source:  "memory",
			Metadata: map[string]any{
				"memory_category": item.Category,
				"memory_source":   item.Source,
				"memory_weight":   item.Weight,
			},
		})
	}
	if len(out) == 0 && strings.TrimSpace(contextText) != "" {
		out = append(out, GoldenEvidence{ID: stableEvaluationSubjectID(contextText), Content: contextText, Source: "memory_context"})
	}
	return out
}

type memoryEvalMetrics struct {
	ContextRecall          float64
	CaptureRecall          float64
	ForbiddenLeak          float64
	NamespaceIsolationPass float64
}

func memoryEvaluationMetrics(contextText string, captured []MemoryItem, retrieved []GoldenEvidence, expectedFacts, forbidden []string) memoryEvalMetrics {
	contextCorpus := contextText + "\n" + goldenEvidenceText(retrieved)
	capturedParts := make([]string, 0, len(captured))
	for _, item := range captured {
		capturedParts = append(capturedParts, item.Content)
	}
	capturedText := strings.Join(capturedParts, "\n")
	leak := 0.0
	for _, fact := range forbidden {
		if textContainsMeaning(contextCorpus+"\n"+capturedText, fact) {
			leak = 1
			break
		}
	}
	return memoryEvalMetrics{
		ContextRecall:          factCoverageScore(contextCorpus, expectedFacts),
		CaptureRecall:          factCoverageScore(capturedText, expectedFacts),
		ForbiddenLeak:          leak,
		NamespaceIsolationPass: 1 - leak,
	}
}

func formatMemoryEvaluationOutput(captured []MemoryItem, contextText string, metrics memoryEvalMetrics) string {
	parts := []string{}
	if len(captured) > 0 {
		lines := make([]string, 0, len(captured))
		for _, item := range captured {
			lines = append(lines, "- "+item.Content)
		}
		parts = append(parts, "Captured memory:\n"+strings.Join(lines, "\n"))
	}
	if strings.TrimSpace(contextText) != "" {
		parts = append(parts, "Injected memory context:\n"+strings.TrimSpace(contextText))
	}
	parts = append(parts, fmt.Sprintf("Memory metrics: context_recall=%.3f capture_recall=%.3f forbidden_leak=%.3f", metrics.ContextRecall, metrics.CaptureRecall, metrics.ForbiddenLeak))
	return strings.Join(parts, "\n\n")
}

func memoryInjectedContextText(session *state.Session) string {
	if session == nil {
		return ""
	}
	for i := len(session.Messages) - 1; i >= 0; i-- {
		msg := session.Messages[i]
		if !msg.Hidden || !strings.Contains(msg.Content, "<memory>") {
			continue
		}
		return strings.TrimSpace(msg.Content)
	}
	return ""
}

func memoryCaseExpectedFacts(item GoldenCase) []string {
	facts := normalizeNonEmptyStrings(append([]string{}, item.ExpectedFacts...))
	if value := memoryCaseMetadataString(item, "expected_memory"); value != "" {
		facts = append(facts, splitEvaluationSentences(value)...)
	}
	if len(facts) == 0 && strings.TrimSpace(item.ExpectedAnswer) != "" {
		facts = append(facts, item.ExpectedAnswer)
	}
	for _, evidence := range item.GoldEvidence {
		if strings.TrimSpace(evidence.Content) != "" {
			facts = append(facts, evidence.Content)
		}
	}
	return normalizeNonEmptyStrings(facts)
}

func memoryCaseForbiddenFacts(item GoldenCase) []string {
	out := memoryCaseMetadataStrings(item, "forbidden_memory")
	out = append(out, memoryCaseMetadataStrings(item, "forbidden_memories")...)
	return normalizeNonEmptyStrings(out)
}

func memoryCaseSetupMemories(item GoldenCase) []string {
	out := memoryCaseMetadataStrings(item, "setup_memory")
	out = append(out, memoryCaseMetadataStrings(item, "setup_memories")...)
	if len(out) == 0 {
		for _, evidence := range item.GoldEvidence {
			if strings.TrimSpace(evidence.Content) != "" {
				out = append(out, evidence.Content)
			}
		}
	}
	return normalizeNonEmptyStrings(out)
}

func memoryCaseMetadataString(item GoldenCase, key string) string {
	if item.Metadata == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(item.Metadata[key]))
}

func memoryCaseMetadataStrings(item GoldenCase, key string) []string {
	if item.Metadata == nil {
		return nil
	}
	value, ok := item.Metadata[key]
	if !ok {
		return nil
	}
	switch typed := value.(type) {
	case string:
		return splitMemoryEvalLines(typed)
	case []string:
		return typed
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			text := strings.TrimSpace(fmt.Sprint(item))
			if text != "" {
				out = append(out, text)
			}
		}
		return out
	default:
		text := strings.TrimSpace(fmt.Sprint(value))
		if text == "" {
			return nil
		}
		return []string{text}
	}
}

func splitMemoryEvalLines(value string) []string {
	parts := strings.FieldsFunc(value, func(r rune) bool {
		return r == '\n' || r == ','
	})
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func attachMemoryMetricsToReport(report EvaluationRunReport, candidates []GoldenCandidate) EvaluationRunReport {
	byCaseID := goldenCandidatesByCaseID(candidates)
	for index := range report.Results {
		result := &report.Results[index]
		candidate, ok := byCaseID[result.SubjectID]
		if !ok {
			continue
		}
		if result.Metrics == nil {
			result.Metrics = map[string]any{}
		}
		for _, key := range []string{"memory_context_recall", "memory_capture_recall", "memory_forbidden_leak", "namespace_isolation_pass", "memory_seed_count", "memory_captured_count"} {
			if value, ok := candidate.Metadata[key]; ok {
				result.Metrics[key] = value
			}
		}
		if mapFloat(result.Metrics, "memory_forbidden_leak") > 0 {
			result.Status = EvaluationResultStatusFailed
			result.Findings = append(result.Findings, EvaluationFinding{
				Severity: "error",
				Code:     "memory_forbidden_leak",
				Message:  "memory evaluation context contained forbidden memory",
			})
		}
	}
	memoryMetrics := aggregateMemoryMetrics(report.Results)
	report.Run.Metrics = mergeEvaluationMetricMaps(report.Run.Metrics, memoryMetrics)
	report.Summary = summarizeEvaluationResults(report.Run, report.Results)
	report.Reviews = createEvaluationReviews(report.Results)
	return report
}

func aggregateMemoryMetrics(results []EvaluationResult) map[string]any {
	keys := []string{"memory_context_recall", "memory_capture_recall", "memory_forbidden_leak", "namespace_isolation_pass"}
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
