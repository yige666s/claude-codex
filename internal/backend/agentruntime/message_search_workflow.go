package agentruntime

import (
	"context"
	"encoding/json"
	"fmt"
)

const (
	ragSearchWorkflowName    = "rag_search"
	ragSearchWorkflowVersion = "v1"
)

func ragSearchWorkflowDefinition() WorkflowDefinition {
	return WorkflowDefinition{
		Name:    ragSearchWorkflowName,
		Version: ragSearchWorkflowVersion,
		Steps: []WorkflowStepDefinition{
			{Name: "normalize_query"},
			{Name: "query_rewrite"},
			{Name: "hybrid_retrieve"},
			{Name: "rerank"},
			{Name: "select_results"},
		},
	}
}

func newMessageSearchWorkflowEngine(service *MessageSearchService) *WorkflowEngine {
	engine := NewWorkflowEngine(NewMemoryWorkflowStore(), ContextWorkflowEventSink{})
	engine.RegisterStepHandler("normalize_query", service.workflowNormalizeQuery)
	engine.RegisterStepHandler("query_rewrite", service.workflowQueryRewrite)
	engine.RegisterStepHandler("hybrid_retrieve", service.workflowHybridRetrieve)
	engine.RegisterStepHandler("rerank", service.workflowRerank)
	engine.RegisterStepHandler("select_results", service.workflowSelectResults)
	return engine
}

func (s *MessageSearchService) workflowNormalizeQuery(_ context.Context, _ *WorkflowRun, input map[string]any) (map[string]any, error) {
	query := workflowString(input, "query")
	limit, offset := normalizeSearchPage(workflowInt(input, "limit"), workflowInt(input, "offset"))
	window := messageSearchRecallWindow(query, limit, offset, s.config)
	return map[string]any{
		"normalized_query": query,
		"limit":            limit,
		"offset":           offset,
		"window":           window,
	}, nil
}

func (s *MessageSearchService) workflowQueryRewrite(_ context.Context, _ *WorkflowRun, input map[string]any) (map[string]any, error) {
	query := workflowString(input, "normalized_query")
	variants := []string{query}
	if s.config.QueryRewriteEnabled {
		variants = appendUniqueSearchVariants(variants, rewriteMessageSearchQuery(query)...)
	}
	return map[string]any{
		"variants":      variants,
		"variant_count": len(variants),
	}, nil
}

func (s *MessageSearchService) workflowHybridRetrieve(ctx context.Context, _ *WorkflowRun, input map[string]any) (map[string]any, error) {
	userID := workflowString(input, "user_id")
	query := workflowString(input, "normalized_query")
	limit := workflowInt(input, "limit")
	window := workflowInt(input, "window")
	variants := workflowStringSlice(input, "variants")
	if len(variants) == 0 {
		variants = []string{query}
	}
	plan := messageSearchPlan{Query: query, Variants: variants, Window: window}
	candidates, metrics, err := s.searchHybridCandidates(ctx, userID, plan, limit)
	if err != nil {
		return nil, err
	}
	out := map[string]any{
		"candidates":        candidates,
		"candidate_count":   len(candidates),
		"keyword_count":     metrics.KeywordCount,
		"semantic_count":    metrics.SemanticCount,
		"expanded":          metrics.Expanded,
		"variant_count":     len(variants),
		"window":            window,
		"low_confidence":    metrics.LowConfidence,
		"initial_count":     metrics.InitialCount,
		"retrieval_backend": messageSearchBackendHybrid,
	}
	return out, nil
}

func (s *MessageSearchService) workflowRerank(_ context.Context, _ *WorkflowRun, input map[string]any) (map[string]any, error) {
	query := workflowString(input, "normalized_query")
	candidates := messageResultsFromWorkflowState(input, "candidates")
	if s.config.RerankEnabled {
		candidates = rerankMessageSearchResults(query, candidates, s.config)
	}
	return map[string]any{
		"candidates":      candidates,
		"candidate_count": len(candidates),
		"rerank_enabled":  s.config.RerankEnabled,
	}, nil
}

func (s *MessageSearchService) workflowSelectResults(_ context.Context, _ *WorkflowRun, input map[string]any) (map[string]any, error) {
	candidates := messageResultsFromWorkflowState(input, "candidates")
	limit := workflowInt(input, "limit")
	offset := workflowInt(input, "offset")
	results := sliceSearchResults(candidates, limit, offset)
	return map[string]any{
		"results":      results,
		"result_count": len(results),
	}, nil
}

type messageSearchCandidateMetrics struct {
	KeywordCount  int
	SemanticCount int
	InitialCount  int
	Expanded      bool
	LowConfidence bool
}

func (s *MessageSearchService) searchHybridCandidates(ctx context.Context, userID string, plan messageSearchPlan, limit int) ([]MessageSearchResult, messageSearchCandidateMetrics, error) {
	var keywordResults []MessageSearchResult
	var semanticResults []MessageSearchResult
	var keywordErr error
	var semanticErr error

	if s.keyword != nil {
		keywordResults, keywordErr = s.keyword.SearchMessages(ctx, userID, plan.Query, plan.Window, 0)
	}
	if s.semantic != nil {
		semanticResults, semanticErr = s.semantic.SearchSemanticMessages(ctx, userID, plan.Query, plan.Window)
		if semanticErr == nil {
			semanticResults, semanticErr = s.hydrate(ctx, userID, semanticResults)
		}
	}
	if keywordErr != nil {
		return nil, messageSearchCandidateMetrics{}, keywordErr
	}
	if semanticErr != nil {
		return nil, messageSearchCandidateMetrics{}, semanticErr
	}
	lists := [][]MessageSearchResult{keywordResults, semanticResults}
	merged := rrfMergeMessageSearchResultsWithK(s.config.RRFK, lists...)
	metrics := messageSearchCandidateMetrics{
		KeywordCount:  len(keywordResults),
		SemanticCount: len(semanticResults),
		InitialCount:  len(merged),
		LowConfidence: shouldExpandMessageSearch(merged, limit, s.config),
	}
	if s.config.MultiTurnEnabled && metrics.LowConfidence {
		for _, variant := range plan.Variants {
			if variant == plan.Query {
				continue
			}
			variantResults := s.searchHybridVariant(ctx, userID, variant, plan.Window)
			if len(variantResults) > 0 {
				lists = append(lists, variantResults)
				metrics.Expanded = true
			}
		}
		merged = rrfMergeMessageSearchResultsWithK(s.config.RRFK, lists...)
	}
	return merged, metrics, nil
}

func messageResultsFromWorkflowState(state map[string]any, key string) []MessageSearchResult {
	value, ok := state[key]
	if !ok || value == nil {
		return []MessageSearchResult{}
	}
	if typed, ok := value.([]MessageSearchResult); ok {
		return append([]MessageSearchResult(nil), typed...)
	}
	data, err := json.Marshal(value)
	if err != nil {
		return []MessageSearchResult{}
	}
	var out []MessageSearchResult
	if err := json.Unmarshal(data, &out); err != nil {
		return []MessageSearchResult{}
	}
	return out
}

func workflowString(state map[string]any, key string) string {
	value, _ := state[key].(string)
	return value
}

func workflowInt(state map[string]any, key string) int {
	switch value := state[key].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	case json.Number:
		n, _ := value.Int64()
		return int(n)
	default:
		return 0
	}
}

func workflowStringSlice(state map[string]any, key string) []string {
	value, ok := state[key]
	if !ok || value == nil {
		return nil
	}
	if typed, ok := value.([]string); ok {
		return append([]string(nil), typed...)
	}
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		text, ok := item.(string)
		if !ok {
			continue
		}
		out = append(out, text)
	}
	return out
}

func describeRAGWorkflowStep(step *WorkflowStepRun) string {
	if step == nil {
		return ""
	}
	return fmt.Sprintf("%s:%s", step.StepName, step.Status)
}
