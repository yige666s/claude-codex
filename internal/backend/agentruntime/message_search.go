package agentruntime

import (
	"context"
	"errors"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
)

const (
	messageSearchBackendSQL           = "sql"
	messageSearchBackendFallback      = "fallback"
	messageSearchBackendElasticsearch = "elasticsearch"
	messageSearchBackendOpenSearch    = "opensearch"
	messageSearchBackendSemantic      = "semantic"
	messageSearchBackendHybrid        = "hybrid"

	defaultMessageSearchIndex                      = "agent_messages"
	defaultMessageSearchCollection                 = "agent_messages"
	defaultMessageSearchTimeout                    = 5 * time.Second
	defaultMessageSearchRRFK                       = 60
	defaultMessageSearchAnalyzer                   = "ik_max_word"
	defaultMessageSearchSearchAnalyzer             = "ik_smart"
	defaultMessageSearchIndexMaintenanceInterval   = 24 * time.Hour
	defaultMessageSearchIndexMaintenanceBatchLimit = 50
	defaultMessageSearchIndexEnsureInterval        = time.Minute
	defaultMessageSearchMinRecallWindow            = 50
	defaultMessageSearchMaxRecallWindow            = 120
	defaultMessageSearchRerankCandidateLimit       = 50
	defaultMessageSearchLowConfidenceScore         = 0.04

	messageEmbeddingProviderOpenAI = "openai"
	messageEmbeddingProviderVertex = "vertex"
)

type SemanticMessageSearcher interface {
	SearchSemanticMessages(ctx context.Context, userID, query string, limit int) ([]MessageSearchResult, error)
}

type MessageSearchResultHydrator interface {
	HydrateMessageSearchResults(ctx context.Context, userID string, results []MessageSearchResult) ([]MessageSearchResult, error)
}

type MessageSearchService struct {
	config   MessageSearchConfig
	fallback MessageSearchStore
	keyword  MessageSearchStore
	semantic SemanticMessageSearcher
	hydrator MessageSearchResultHydrator
	workflow *WorkflowEngine
}

func NewMessageSearchService(config MessageSearchConfig, fallback MessageSearchStore) *MessageSearchService {
	config = normalizeMessageSearchConfig(config)
	service := &MessageSearchService{
		config:   config,
		fallback: fallback,
	}
	if hydrator, ok := fallback.(MessageSearchResultHydrator); ok {
		service.hydrator = hydrator
	}
	switch config.Backend {
	case messageSearchBackendElasticsearch, messageSearchBackendOpenSearch, messageSearchBackendHybrid:
		if strings.TrimSpace(config.Endpoint) != "" {
			service.keyword = NewHTTPMessageFullTextSearcher(config)
		}
	}
	if config.Backend == messageSearchBackendSemantic || config.Backend == messageSearchBackendHybrid {
		if strings.TrimSpace(config.QdrantEndpoint) != "" && messageEmbeddingConfigured(config) {
			service.semantic = NewQdrantSemanticMessageSearcher(config)
		}
	}
	service.workflow = newMessageSearchWorkflowEngine(service)
	return service
}

func (s *MessageSearchService) SearchMessages(ctx context.Context, userID, query string, limit, offset int) ([]MessageSearchResult, error) {
	if s == nil {
		return []MessageSearchResult{}, nil
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return []MessageSearchResult{}, nil
	}
	limit, offset = normalizeSearchPage(limit, offset)

	switch s.config.Backend {
	case messageSearchBackendElasticsearch, messageSearchBackendOpenSearch:
		if s.keyword != nil {
			return s.keyword.SearchMessages(ctx, userID, query, limit, offset)
		}
		return nil, errMessageSearchNotConfigured("full-text backend")
	case messageSearchBackendSemantic:
		if s.semantic != nil {
			window := limit + offset
			if s.config.DynamicTopKEnabled {
				window = messageSearchRecallWindow(query, limit, offset, s.config)
			}
			results, err := s.semantic.SearchSemanticMessages(ctx, userID, query, window)
			if err != nil {
				return nil, err
			}
			results, err = s.hydrate(ctx, userID, results)
			if err != nil {
				return nil, err
			}
			if s.config.RerankEnabled {
				results = rerankMessageSearchResults(query, results, s.config)
			}
			return sliceSearchResults(results, limit, offset), nil
		}
		return s.searchFallback(ctx, userID, query, limit, offset)
	case messageSearchBackendHybrid:
		return s.searchHybrid(ctx, userID, query, limit, offset)
	default:
		return s.searchFallback(ctx, userID, query, limit, offset)
	}
}

func (s *MessageSearchService) searchFallback(ctx context.Context, userID, query string, limit, offset int) ([]MessageSearchResult, error) {
	if s == nil || s.fallback == nil {
		return []MessageSearchResult{}, nil
	}
	results, err := s.fallback.SearchMessages(ctx, userID, query, limit, offset)
	if err != nil {
		return nil, err
	}
	for i := range results {
		if results[i].Source == "" {
			results[i].Source = messageSearchBackendSQL
		}
	}
	return results, nil
}

func (s *MessageSearchService) searchHybrid(ctx context.Context, userID, query string, limit, offset int) ([]MessageSearchResult, error) {
	if s != nil && s.workflow != nil {
		run, err := s.workflow.Execute(ctx, WorkflowRequest{
			Definition: ragSearchWorkflowDefinition(),
			UserID:     userID,
			JobID:      jobIDFromContext(ctx),
			State: map[string]any{
				"user_id": userID,
				"query":   query,
				"limit":   limit,
				"offset":  offset,
				"backend": s.config.Backend,
			},
		})
		if err != nil {
			return nil, err
		}
		return messageResultsFromWorkflowState(run.State, "results"), nil
	}
	return s.searchHybridDirect(ctx, userID, query, limit, offset)
}

func (s *MessageSearchService) searchHybridDirect(ctx context.Context, userID, query string, limit, offset int) ([]MessageSearchResult, error) {
	plan := buildMessageSearchPlan(query, limit, offset, s.config)

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
		return nil, keywordErr
	}
	if semanticErr != nil {
		return nil, semanticErr
	}
	lists := [][]MessageSearchResult{keywordResults, semanticResults}
	merged := rrfMergeMessageSearchResultsWithK(s.config.RRFK, lists...)
	if s.config.MultiTurnEnabled && shouldExpandMessageSearch(merged, limit, s.config) {
		for _, variant := range plan.Variants {
			if variant == plan.Query {
				continue
			}
			variantResults := s.searchHybridVariant(ctx, userID, variant, plan.Window)
			if len(variantResults) > 0 {
				lists = append(lists, variantResults)
			}
		}
		merged = rrfMergeMessageSearchResultsWithK(s.config.RRFK, lists...)
	}
	if s.config.RerankEnabled {
		merged = rerankMessageSearchResults(query, merged, s.config)
	}
	return sliceSearchResults(merged, limit, offset), nil
}

func (s *MessageSearchService) searchHybridVariant(ctx context.Context, userID, query string, window int) []MessageSearchResult {
	results := make([]MessageSearchResult, 0)
	if s == nil || strings.TrimSpace(query) == "" {
		return results
	}
	if s.keyword != nil {
		if hits, err := s.keyword.SearchMessages(ctx, userID, query, window, 0); err == nil {
			results = append(results, hits...)
		}
	}
	if s.semantic != nil {
		hits, err := s.semantic.SearchSemanticMessages(ctx, userID, query, window)
		if err == nil {
			hits, err = s.hydrate(ctx, userID, hits)
		}
		if err == nil {
			results = append(results, hits...)
		}
	}
	return results
}

func (s *MessageSearchService) hydrate(ctx context.Context, userID string, results []MessageSearchResult) ([]MessageSearchResult, error) {
	if s == nil || s.hydrator == nil || len(results) == 0 {
		return results, nil
	}
	return s.hydrator.HydrateMessageSearchResults(ctx, userID, results)
}

func normalizeMessageSearchConfig(config MessageSearchConfig) MessageSearchConfig {
	config.Backend = strings.ToLower(strings.TrimSpace(config.Backend))
	if config.Backend == "" {
		config.Backend = messageSearchBackendSQL
	}
	if config.Backend == "elastic" {
		config.Backend = messageSearchBackendElasticsearch
	}
	if config.Backend == "fulltext" || config.Backend == "full-text" {
		config.Backend = messageSearchBackendElasticsearch
	}
	switch config.Backend {
	case messageSearchBackendSQL, messageSearchBackendFallback, messageSearchBackendElasticsearch, messageSearchBackendOpenSearch, messageSearchBackendSemantic, messageSearchBackendHybrid:
	default:
		config.Backend = messageSearchBackendSQL
	}
	if strings.TrimSpace(config.Index) == "" {
		config.Index = defaultMessageSearchIndex
	}
	if strings.TrimSpace(config.IndexWriteAlias) == "" {
		config.IndexWriteAlias = strings.TrimSpace(config.Index)
	}
	if strings.TrimSpace(config.IndexLifecyclePolicy) == "" {
		config.IndexLifecyclePolicy = config.IndexWriteAlias + "_ilm"
	}
	if strings.TrimSpace(config.IndexTemplateName) == "" {
		config.IndexTemplateName = config.IndexWriteAlias + "_template"
	}
	if strings.TrimSpace(config.IndexAnalyzer) == "" {
		config.IndexAnalyzer = defaultMessageSearchAnalyzer
	}
	if strings.TrimSpace(config.IndexSearchAnalyzer) == "" {
		config.IndexSearchAnalyzer = defaultMessageSearchSearchAnalyzer
	}
	if config.IndexMaintenanceInterval <= 0 {
		config.IndexMaintenanceInterval = defaultMessageSearchIndexMaintenanceInterval
	}
	if config.IndexMaintenanceBatchLimit <= 0 || config.IndexMaintenanceBatchLimit > 1000 {
		config.IndexMaintenanceBatchLimit = defaultMessageSearchIndexMaintenanceBatchLimit
	}
	if strings.TrimSpace(config.QdrantCollection) == "" {
		config.QdrantCollection = defaultMessageSearchCollection
	}
	config.EmbeddingProvider = strings.ToLower(strings.TrimSpace(config.EmbeddingProvider))
	if config.EmbeddingProvider == "" {
		if strings.TrimSpace(config.EmbeddingProjectID) != "" || strings.HasPrefix(strings.ToLower(strings.TrimSpace(config.EmbeddingModel)), "gemini-embedding") {
			config.EmbeddingProvider = messageEmbeddingProviderVertex
		} else {
			config.EmbeddingProvider = messageEmbeddingProviderOpenAI
		}
	}
	switch config.EmbeddingProvider {
	case "google", "gemini", "vertexai", "vertex-ai":
		config.EmbeddingProvider = messageEmbeddingProviderVertex
	case messageEmbeddingProviderOpenAI, messageEmbeddingProviderVertex:
	default:
		config.EmbeddingProvider = messageEmbeddingProviderOpenAI
	}
	if config.EmbeddingProvider == messageEmbeddingProviderVertex {
		if strings.TrimSpace(config.EmbeddingModel) == "" {
			config.EmbeddingModel = "gemini-embedding-2"
		}
		if strings.TrimSpace(config.EmbeddingLocation) == "" {
			config.EmbeddingLocation = "global"
		}
		if strings.TrimSpace(config.EmbeddingTaskType) == "" {
			config.EmbeddingTaskType = "RETRIEVAL_QUERY"
		}
		if strings.TrimSpace(config.EmbeddingIndexTaskType) == "" {
			config.EmbeddingIndexTaskType = "RETRIEVAL_DOCUMENT"
		}
	}
	if config.Timeout <= 0 {
		config.Timeout = defaultMessageSearchTimeout
	}
	if config.EmbeddingTimeout <= 0 {
		config.EmbeddingTimeout = config.Timeout
	}
	if config.RRFK <= 0 {
		config.RRFK = defaultMessageSearchRRFK
	}
	if config.MinRecallWindow <= 0 {
		config.MinRecallWindow = defaultMessageSearchMinRecallWindow
	}
	if config.MaxRecallWindow <= 0 {
		config.MaxRecallWindow = defaultMessageSearchMaxRecallWindow
	}
	if config.MaxRecallWindow < config.MinRecallWindow {
		config.MaxRecallWindow = config.MinRecallWindow
	}
	if config.RerankCandidateLimit <= 0 {
		config.RerankCandidateLimit = defaultMessageSearchRerankCandidateLimit
	}
	if config.LowConfidenceScore <= 0 {
		config.LowConfidenceScore = defaultMessageSearchLowConfidenceScore
	}
	return config
}

func messageEmbeddingConfigured(config MessageSearchConfig) bool {
	config = normalizeMessageSearchConfig(config)
	switch config.EmbeddingProvider {
	case messageEmbeddingProviderVertex:
		return strings.TrimSpace(config.EmbeddingProjectID) != "" || strings.Contains(strings.TrimSpace(config.EmbeddingModel), "/")
	default:
		return strings.TrimSpace(config.EmbeddingEndpoint) != ""
	}
}

func MessageVectorIndexingEnabled(config MessageSearchConfig) bool {
	config = normalizeMessageSearchConfig(config)
	if config.Backend != messageSearchBackendSemantic && config.Backend != messageSearchBackendHybrid {
		return false
	}
	return strings.TrimSpace(config.QdrantEndpoint) != "" &&
		strings.TrimSpace(config.QdrantCollection) != "" &&
		messageEmbeddingConfigured(config)
}

func MessageFullTextIndexingEnabled(config MessageSearchConfig) bool {
	config = normalizeMessageSearchConfig(config)
	switch config.Backend {
	case messageSearchBackendElasticsearch, messageSearchBackendOpenSearch, messageSearchBackendHybrid:
		return strings.TrimSpace(config.Endpoint) != "" && strings.TrimSpace(config.IndexWriteAlias) != ""
	default:
		return false
	}
}

func messageVectorIndexingEnabled(config MessageSearchConfig) bool {
	return MessageVectorIndexingEnabled(config)
}

func normalizeSearchPage(limit, offset int) (int, int) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

func rrfMergeMessageSearchResultsWithK(k int, lists ...[]MessageSearchResult) []MessageSearchResult {
	if k <= 0 {
		k = defaultMessageSearchRRFK
	}
	type scored struct {
		result MessageSearchResult
		score  float64
		best   int
	}
	merged := make(map[string]*scored)
	for _, results := range lists {
		for rank, result := range results {
			key := messageSearchResultKey(result)
			if key == "" {
				key = "rank:" + strconv.Itoa(rank) + ":" + result.SessionID + ":" + result.Snippet
			}
			value, ok := merged[key]
			if !ok {
				result.Score = 0
				value = &scored{result: result, best: math.MaxInt}
				merged[key] = value
			}
			value.score += 1 / float64(k+rank+1)
			if rank < value.best {
				value.best = rank
				if result.Snippet != "" {
					value.result.Snippet = result.Snippet
				}
				if result.Content != "" {
					value.result.Content = result.Content
				}
				if result.Source != "" {
					value.result.Source = result.Source
				}
			}
		}
	}
	out := make([]MessageSearchResult, 0, len(merged))
	for _, value := range merged {
		value.result.Score = value.score
		if value.result.Source == "" {
			value.result.Source = messageSearchBackendHybrid
		}
		out = append(out, value.result)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score == out[j].Score {
			return out[i].CreatedAt.After(out[j].CreatedAt)
		}
		return out[i].Score > out[j].Score
	})
	return out
}

func rrfMergeMessageSearchResults(keywordResults, semanticResults []MessageSearchResult, k int) []MessageSearchResult {
	return rrfMergeMessageSearchResultsWithK(k, keywordResults, semanticResults)
}

func messageSearchResultKey(result MessageSearchResult) string {
	if result.MessageID != "" {
		return "message:" + result.MessageID
	}
	if result.SessionID == "" {
		return ""
	}
	return result.SessionID + ":" + strconv.Itoa(result.MessageIndex)
}

func sliceSearchResults(results []MessageSearchResult, limit, offset int) []MessageSearchResult {
	limit, offset = normalizeSearchPage(limit, offset)
	if offset >= len(results) {
		return []MessageSearchResult{}
	}
	end := offset + limit
	if end > len(results) {
		end = len(results)
	}
	return results[offset:end]
}

func errMessageSearchNotConfigured(component string) error {
	return errors.New("message search " + component + " is not configured")
}

type messageSearchPlan struct {
	Query    string
	Variants []string
	Window   int
}

func buildMessageSearchPlan(query string, limit, offset int, config MessageSearchConfig) messageSearchPlan {
	query = strings.Join(strings.Fields(strings.TrimSpace(query)), " ")
	plan := messageSearchPlan{Query: query, Variants: []string{query}, Window: messageSearchRecallWindow(query, limit, offset, config)}
	if config.QueryRewriteEnabled {
		plan.Variants = appendUniqueSearchVariants(plan.Variants, rewriteMessageSearchQuery(query)...)
	}
	return plan
}

func messageSearchRecallWindow(query string, limit, offset int, config MessageSearchConfig) int {
	limit, offset = normalizeSearchPage(limit, offset)
	base := limit + offset + 50
	if !config.DynamicTopKEnabled {
		return base
	}
	window := limit + offset
	if window < config.MinRecallWindow {
		window = config.MinRecallWindow
	}
	if messageSearchNeedsWideRecall(query) {
		window += 40
	}
	if len(messageSearchTerms(query)) <= 2 {
		window += 20
	}
	if window > config.MaxRecallWindow {
		window = config.MaxRecallWindow
	}
	return window
}

func messageSearchNeedsWideRecall(query string) bool {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return false
	}
	for _, marker := range []string{"之前", "上次", "那个", "刚才", "以前", "previous", "last time", "earlier"} {
		if strings.Contains(query, marker) {
			return true
		}
	}
	return false
}

func rewriteMessageSearchQuery(query string) []string {
	out := make([]string, 0, 3)
	trimmed := strings.TrimSpace(query)
	if trimmed == "" {
		return out
	}
	rewritten := trimmed
	for _, phrase := range []string{"请问", "麻烦", "帮我", "帮忙", "查一下", "搜一下", "找一下", "我想知道", "你能不能", "can you", "please", "could you"} {
		rewritten = strings.ReplaceAll(rewritten, phrase, " ")
	}
	rewritten = strings.Join(strings.Fields(strings.TrimSpace(rewritten)), " ")
	if rewritten != "" && rewritten != trimmed {
		out = append(out, rewritten)
	}
	terms := messageSearchTerms(trimmed)
	if len(terms) >= 2 {
		out = append(out, strings.Join(terms, " "))
	}
	return out
}

func appendUniqueSearchVariants(values []string, candidates ...string) []string {
	seen := map[string]bool{}
	for _, value := range values {
		seen[strings.ToLower(strings.TrimSpace(value))] = true
	}
	for _, candidate := range candidates {
		candidate = strings.Join(strings.Fields(strings.TrimSpace(candidate)), " ")
		key := strings.ToLower(candidate)
		if candidate == "" || seen[key] {
			continue
		}
		seen[key] = true
		values = append(values, candidate)
		if len(values) >= 4 {
			break
		}
	}
	return values
}

func shouldExpandMessageSearch(results []MessageSearchResult, limit int, config MessageSearchConfig) bool {
	if len(results) < limit {
		return true
	}
	if len(results) == 0 {
		return true
	}
	return results[0].Score > 0 && results[0].Score < config.LowConfidenceScore
}

func rerankMessageSearchResults(query string, results []MessageSearchResult, config MessageSearchConfig) []MessageSearchResult {
	if len(results) <= 1 {
		return results
	}
	candidateLimit := config.RerankCandidateLimit
	if candidateLimit <= 0 || candidateLimit > len(results) {
		candidateLimit = len(results)
	}
	head := append([]MessageSearchResult(nil), results[:candidateLimit]...)
	tail := append([]MessageSearchResult(nil), results[candidateLimit:]...)
	maxScore := 0.0
	for _, result := range head {
		if result.Score > maxScore {
			maxScore = result.Score
		}
	}
	type scored struct {
		result MessageSearchResult
		score  float64
		base   float64
	}
	scoredResults := make([]scored, 0, len(head))
	for _, result := range head {
		base := 0.0
		if maxScore > 0 {
			base = result.Score / maxScore
		}
		relevance := messageSearchRelevanceScore(query, result)
		recency := messageSearchRecencyScore(result.CreatedAt)
		score := 0.55*relevance + 0.35*base + 0.10*recency
		result.Score = score
		scoredResults = append(scoredResults, scored{result: result, score: score, base: base})
	}
	sort.SliceStable(scoredResults, func(i, j int) bool {
		if scoredResults[i].score == scoredResults[j].score {
			return scoredResults[i].base > scoredResults[j].base
		}
		return scoredResults[i].score > scoredResults[j].score
	})
	out := make([]MessageSearchResult, 0, len(results))
	for _, value := range scoredResults {
		out = append(out, value.result)
	}
	out = append(out, tail...)
	return out
}

func messageSearchRelevanceScore(query string, result MessageSearchResult) float64 {
	terms := messageSearchTerms(query)
	if len(terms) == 0 {
		return 0
	}
	text := strings.ToLower(strings.Join([]string{result.Content, result.Snippet, result.SessionTitle, result.Role}, " "))
	matches := 0
	for _, term := range terms {
		if strings.Contains(text, strings.ToLower(term)) {
			matches++
		}
	}
	score := float64(matches) / float64(len(terms))
	if strings.Contains(text, strings.ToLower(strings.TrimSpace(query))) {
		score += 0.25
	}
	if score > 1 {
		return 1
	}
	return score
}

func messageSearchRecencyScore(createdAt time.Time) float64 {
	if createdAt.IsZero() {
		return 0
	}
	age := time.Since(createdAt)
	if age <= 24*time.Hour {
		return 1
	}
	if age <= 7*24*time.Hour {
		return 0.6
	}
	if age <= 30*24*time.Hour {
		return 0.3
	}
	return 0
}

func messageSearchTerms(query string) []string {
	query = strings.ToLower(strings.TrimSpace(query))
	terms := make([]string, 0)
	var current []rune
	flush := func() {
		if len(current) == 0 {
			return
		}
		term := string(current)
		current = current[:0]
		if len([]rune(term)) < 2 || messageSearchStopWord(term) {
			return
		}
		terms = append(terms, term)
	}
	for _, r := range query {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-' || r == '.' {
			current = append(current, r)
			continue
		}
		flush()
	}
	flush()
	return compactSearchTerms(terms)
}

func compactSearchTerms(terms []string) []string {
	out := make([]string, 0, len(terms))
	seen := map[string]bool{}
	for _, term := range terms {
		if seen[term] {
			continue
		}
		seen[term] = true
		out = append(out, term)
	}
	return out
}

func messageSearchStopWord(term string) bool {
	switch term {
	case "the", "and", "or", "for", "with", "this", "that", "what", "when", "where", "怎么", "什么", "一下", "一个":
		return true
	default:
		return false
	}
}
