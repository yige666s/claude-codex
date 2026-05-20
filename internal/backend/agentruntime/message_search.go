package agentruntime

import (
	"context"
	"errors"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
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
			results, err := s.semantic.SearchSemanticMessages(ctx, userID, query, limit+offset)
			if err != nil {
				return nil, err
			}
			results, err = s.hydrate(ctx, userID, results)
			if err != nil {
				return nil, err
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
	window := limit + offset + 50
	if window < limit+offset {
		window = limit + offset
	}

	var keywordResults []MessageSearchResult
	var semanticResults []MessageSearchResult
	var keywordErr error
	var semanticErr error

	if s.keyword != nil {
		keywordResults, keywordErr = s.keyword.SearchMessages(ctx, userID, query, window, 0)
	}
	if s.semantic != nil {
		semanticResults, semanticErr = s.semantic.SearchSemanticMessages(ctx, userID, query, window)
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
	return sliceSearchResults(rrfMergeMessageSearchResults(keywordResults, semanticResults, s.config.RRFK), limit, offset), nil
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
