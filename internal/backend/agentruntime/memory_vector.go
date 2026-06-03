package agentruntime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"claude-codex/internal/backend/httpclient"
	"claude-codex/internal/harness/state"
)

const (
	defaultMemoryVectorCollection = "agent_memories"
	defaultMemoryVectorLimit      = 12
	memoryRetrievalCacheNamespace = "memory_retrieval"
)

type MemoryVectorService struct {
	base    MemoryService
	items   MemoryItemService
	indexer *QdrantMemoryVectorIndex
	cache   *TypedCache[[]MemoryItem]
	logger  *slog.Logger
}

func NewMemoryVectorService(base MemoryService, config MemoryVectorConfig, logger any) MemoryService {
	if base == nil {
		return base
	}
	items, ok := base.(MemoryItemService)
	if !ok || !MemoryVectorEnabled(config) {
		return base
	}
	indexer := NewQdrantMemoryVectorIndex(config)
	if indexer == nil {
		return base
	}
	service := &MemoryVectorService{base: base, items: items, indexer: indexer, logger: componentLogger(structuredLogger(logger), "memory_vector")}
	if config.CacheStore != nil {
		service.cache = NewTypedCache[[]MemoryItem](config.CacheStore, CachePolicy{
			Namespace: memoryRetrievalCacheNamespace,
			TTL:       cacheTTLOrDefault(config.CacheDefaultTTL),
			FailOpen:  config.CacheFailOpen,
		}, config.CacheMetrics)
	}
	return service
}

func (s *MemoryVectorService) LoadContext(ctx context.Context, userID string, session *state.Session) (string, error) {
	if s == nil || s.base == nil {
		return "", nil
	}
	if session == nil || s.indexer == nil {
		return s.base.LoadContext(ctx, userID, session)
	}
	query := lastVisibleUserMessage(session)
	if strings.TrimSpace(query) == "" {
		return s.base.LoadContext(ctx, userID, session)
	}
	cacheKey := s.cacheKey(userID, session.ID, query)
	if s.cache != nil {
		if cached, ok, err := s.cache.Get(ctx, cacheKey); err != nil {
			return "", err
		} else if ok {
			selected, err := s.hydrateCachedItems(ctx, userID, cached)
			if err != nil {
				return "", err
			}
			if len(selected) > 0 {
				return s.recordAndFormatMemoryContext(ctx, userID, session.ID, query, selected)
			}
		}
	}
	vectorItems, vectorErr := s.indexer.SearchMemoryItems(ctx, userID, query, session.ID, defaultMemoryVectorLimit*3, s.items)
	allItems, listErr := s.items.ListMemoryItems(ctx, userID, MemoryItemFilter{Status: MemoryStatusActive})
	if listErr != nil {
		return "", listErr
	}
	keywordItems := selectMemoryItemsForSessionContext(allItems, query, session.ID, defaultMemoryVectorLimit*2)
	selected := mergeMemoryRetrievalResults(vectorItems, keywordItems, query, session.ID, defaultMemoryVectorLimit, s.indexer.config.RRFK)
	if len(selected) == 0 {
		if vectorErr != nil {
			s.logError(ctx, "memory vector retrieval failed", vectorErr, userID, session.ID, "")
		}
		return s.base.LoadContext(ctx, userID, session)
	}
	if s.cache != nil {
		_ = s.cache.Set(ctx, cacheKey, selected)
	}
	return s.recordAndFormatMemoryContext(ctx, userID, session.ID, query, selected)
}

func (s *MemoryVectorService) recordAndFormatMemoryContext(ctx context.Context, userID, sessionID, query string, selected []MemoryItem) (string, error) {
	now := time.Now().UTC()
	for i := range selected {
		selected[i] = recordMemoryInjection(selected[i], sessionID, query, now)
		updated, err := s.items.UpdateMemoryItem(ctx, userID, selected[i])
		if err != nil {
			return "", err
		}
		selected[i] = updated
	}
	return "# Memory\n\n" + formatMemoryItems(selected), nil
}

func (s *MemoryVectorService) hydrateCachedItems(ctx context.Context, userID string, cached []MemoryItem) ([]MemoryItem, error) {
	if s == nil || s.items == nil || len(cached) == 0 {
		return []MemoryItem{}, nil
	}
	out := make([]MemoryItem, 0, len(cached))
	for _, item := range cached {
		itemID := strings.TrimSpace(item.ID)
		if itemID == "" {
			continue
		}
		current, err := s.items.GetMemoryItem(ctx, userID, itemID)
		if err != nil {
			continue
		}
		current = normalizeMemoryItem(current)
		if current.Status != MemoryStatusActive || strings.TrimSpace(current.Content) == "" {
			continue
		}
		out = append(out, current)
	}
	return out, nil
}

func (s *MemoryVectorService) LoadUserMemory(ctx context.Context, userID string) (string, error) {
	return s.base.LoadUserMemory(ctx, userID)
}

func (s *MemoryVectorService) LoadSessionMemory(ctx context.Context, userID, sessionID string) (string, error) {
	return s.base.LoadSessionMemory(ctx, userID, sessionID)
}

func (s *MemoryVectorService) AfterTurn(ctx context.Context, userID string, session *state.Session) error {
	before := map[string]MemoryItem{}
	if s.items != nil {
		if items, err := s.items.ListMemoryItems(ctx, userID, MemoryItemFilter{}); err == nil {
			for _, item := range items {
				before[item.ID] = item
			}
		}
	}
	if err := s.base.AfterTurn(ctx, userID, session); err != nil {
		return err
	}
	if s.items == nil {
		return nil
	}
	after, err := s.items.ListMemoryItems(ctx, userID, MemoryItemFilter{})
	if err != nil {
		return err
	}
	for _, item := range after {
		if old, ok := before[item.ID]; ok && old.UpdatedAt.Equal(item.UpdatedAt) && old.RawHash == item.RawHash {
			continue
		}
		_ = s.invalidateUserCache(ctx, userID)
		_, _ = s.syncMemoryVector(ctx, userID, item)
	}
	return nil
}

func (s *MemoryVectorService) DeleteSession(ctx context.Context, userID, sessionID string) error {
	if err := s.base.DeleteSession(ctx, userID, sessionID); err != nil {
		return err
	}
	_ = s.invalidateUserCache(ctx, userID)
	s.deleteSessionVectors(ctx, userID, sessionID)
	return nil
}

func (s *MemoryVectorService) DeleteUser(ctx context.Context, userID string) error {
	if err := s.base.DeleteUser(ctx, userID); err != nil {
		return err
	}
	_ = s.invalidateUserCache(ctx, userID)
	s.deleteUserVectors(ctx, userID)
	return nil
}

func (s *MemoryVectorService) DeleteSavedMemory(ctx context.Context, userID string) error {
	if service, ok := s.base.(SavedMemoryDeletionService); ok {
		if err := service.DeleteSavedMemory(ctx, userID); err != nil {
			return err
		}
		_ = s.invalidateUserCache(ctx, userID)
		s.deleteUserVectors(ctx, userID)
		return nil
	}
	return s.DeleteUser(ctx, userID)
}

func (s *MemoryVectorService) PruneBefore(ctx context.Context, cutoff time.Time) (int, error) {
	var before []MemoryItem
	if service, ok := s.base.(interface {
		ListAllMemoryItems(context.Context) ([]MemoryItem, error)
	}); ok {
		before, _ = service.ListAllMemoryItems(ctx)
	}
	n, err := s.base.PruneBefore(ctx, cutoff)
	if err != nil {
		return n, err
	}
	if len(before) == 0 {
		return n, nil
	}
	afterByID := map[string]MemoryItem{}
	for _, item := range before {
		if strings.TrimSpace(item.UserID) == "" {
			continue
		}
		current, err := s.items.GetMemoryItem(ctx, item.UserID, item.ID)
		if err == nil {
			afterByID[item.ID] = current
		}
	}
	for _, item := range before {
		current, ok := afterByID[item.ID]
		if !ok || current.Status != MemoryStatusActive || strings.TrimSpace(current.Content) == "" {
			_ = s.invalidateUserCache(ctx, item.UserID)
			s.deleteMemoryVector(ctx, item)
			continue
		}
		_ = s.invalidateUserCache(ctx, current.UserID)
		_, _ = s.syncMemoryVector(ctx, current.UserID, current)
	}
	return n, nil
}

func (s *MemoryVectorService) GetMemoryItem(ctx context.Context, userID, itemID string) (MemoryItem, error) {
	return s.items.GetMemoryItem(ctx, userID, itemID)
}

func (s *MemoryVectorService) ListMemoryItems(ctx context.Context, userID string, filter MemoryItemFilter) ([]MemoryItem, error) {
	return s.items.ListMemoryItems(ctx, userID, filter)
}

func (s *MemoryVectorService) UpdateMemoryItem(ctx context.Context, userID string, item MemoryItem) (MemoryItem, error) {
	saved, err := s.items.UpdateMemoryItem(ctx, userID, item)
	if err != nil {
		return MemoryItem{}, err
	}
	_ = s.invalidateUserCache(ctx, userID)
	return s.syncMemoryVector(ctx, userID, saved)
}

func (s *MemoryVectorService) DeleteMemoryItem(ctx context.Context, userID, itemID string) error {
	item, getErr := s.items.GetMemoryItem(ctx, userID, itemID)
	if err := s.items.DeleteMemoryItem(ctx, userID, itemID); err != nil {
		return err
	}
	if getErr == nil {
		_ = s.invalidateUserCache(ctx, userID)
		s.deleteMemoryVector(ctx, item)
	}
	return nil
}

func (s *MemoryVectorService) GetMemorySettings(ctx context.Context, userID string) (MemorySettings, error) {
	if service, ok := s.base.(MemorySettingsService); ok {
		return service.GetMemorySettings(ctx, userID)
	}
	return defaultMemorySettings(), nil
}

func (s *MemoryVectorService) UpdateMemorySettings(ctx context.Context, userID string, settings MemorySettings) (MemorySettings, error) {
	if service, ok := s.base.(MemorySettingsService); ok {
		updated, err := service.UpdateMemorySettings(ctx, userID, settings)
		if err == nil {
			_ = s.invalidateUserCache(ctx, userID)
		}
		return updated, err
	}
	return normalizeMemorySettings(settings), nil
}

func (s *MemoryVectorService) GetPersonalizationSettings(ctx context.Context, userID string) (PersonalizationSettings, error) {
	if service, ok := s.base.(PersonalizationSettingsService); ok {
		return service.GetPersonalizationSettings(ctx, userID)
	}
	return defaultPersonalizationSettings(), nil
}

func (s *MemoryVectorService) UpdatePersonalizationSettings(ctx context.Context, userID string, settings PersonalizationSettings) (PersonalizationSettings, error) {
	if service, ok := s.base.(PersonalizationSettingsService); ok {
		updated, err := service.UpdatePersonalizationSettings(ctx, userID, settings)
		if err == nil {
			_ = s.invalidateUserCache(ctx, userID)
		}
		return updated, err
	}
	return normalizePersonalizationSettings(settings), nil
}

func (s *MemoryVectorService) DeletePersonalizationSettings(ctx context.Context, userID string) error {
	if service, ok := s.base.(PersonalizationSettingsService); ok {
		err := service.DeletePersonalizationSettings(ctx, userID)
		if err == nil {
			_ = s.invalidateUserCache(ctx, userID)
		}
		return err
	}
	return nil
}

func (s *MemoryVectorService) cacheKey(userID, sessionID, query string) string {
	if s == nil {
		return ""
	}
	userPrefix := userPathID(userID)
	if userPrefix == "" {
		userPrefix = "anonymous"
	}
	hash := BuildCacheKey(CacheKeyOptions{
		Namespace: memoryRetrievalCacheNamespace,
		UserID:    userID,
		SessionID: sessionID,
		Version:   s.indexer.modelVersion,
		Parts: []string{
			"query=" + strings.TrimSpace(query),
			"collection=" + strings.TrimSpace(s.indexer.config.QdrantCollection),
			"rrf=" + fmt.Sprintf("%d", s.indexer.config.RRFK),
			"limit=" + fmt.Sprintf("%d", defaultMemoryVectorLimit),
		},
	})
	return userPrefix + ":" + hash
}

func (s *MemoryVectorService) invalidateUserCache(ctx context.Context, userID string) error {
	if s == nil || s.indexer == nil || s.indexer.config.CacheStore == nil {
		return nil
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return nil
	}
	return s.indexer.config.CacheStore.DeletePrefix(ctx, memoryRetrievalCacheNamespace+":"+userPathID(userID)+":")
}

func (s *MemoryVectorService) syncMemoryVector(ctx context.Context, userID string, item MemoryItem) (MemoryItem, error) {
	if s == nil || s.indexer == nil {
		return item, nil
	}
	if !memoryVectorIndexable(item) {
		s.deleteMemoryVector(ctx, item)
		return item, nil
	}
	indexedAt := time.Now().UTC()
	if err := s.indexer.IndexMemory(ctx, item); err != nil {
		s.logError(ctx, "memory vector index failed", err, userID, item.SessionID, item.ID)
		item = annotateMemoryEmbedding(item, "error", s.indexer.modelVersion, memoryVectorID(item.UserID, item.ID), indexedAt, err)
		updated, updateErr := s.items.UpdateMemoryItem(ctx, userID, item)
		if updateErr == nil {
			item = updated
		}
		return item, nil
	}
	item = annotateMemoryEmbedding(item, "indexed", s.indexer.modelVersion, memoryVectorID(item.UserID, item.ID), indexedAt, nil)
	updated, err := s.items.UpdateMemoryItem(ctx, userID, item)
	if err != nil {
		return item, err
	}
	return updated, nil
}

func (s *MemoryVectorService) deleteMemoryVector(ctx context.Context, item MemoryItem) {
	if s == nil || s.indexer == nil || strings.TrimSpace(item.UserID) == "" || strings.TrimSpace(item.ID) == "" {
		return
	}
	if err := s.indexer.DeleteMemory(ctx, item.UserID, item.ID); err != nil {
		s.logError(ctx, "memory vector delete failed", err, item.UserID, item.SessionID, item.ID)
	}
}

func (s *MemoryVectorService) deleteSessionVectors(ctx context.Context, userID, sessionID string) {
	if s == nil || s.indexer == nil {
		return
	}
	if err := s.indexer.DeleteSession(ctx, userID, sessionID); err != nil {
		s.logError(ctx, "memory vector session delete failed", err, userID, sessionID, "")
	}
}

func (s *MemoryVectorService) deleteUserVectors(ctx context.Context, userID string) {
	if s == nil || s.indexer == nil {
		return
	}
	if err := s.indexer.DeleteUser(ctx, userID); err != nil {
		s.logError(ctx, "memory vector user delete failed", err, userID, "", "")
	}
}

func (s *MemoryVectorService) logError(ctx context.Context, message string, err error, userID, sessionID, memoryID string) {
	logger := (*slog.Logger)(nil)
	if s != nil {
		logger = s.logger
	}
	attrs := contextLogAttrs(ctx, userID, sessionID, "")
	if memoryID = strings.TrimSpace(memoryID); memoryID != "" {
		attrs = append(attrs, slog.String("memory_id", memoryID))
	}
	logError(ctx, logger, message, err, attrs...)
}

type QdrantMemoryVectorIndex struct {
	config          MemoryVectorConfig
	endpoint        string
	collection      string
	apiKey          string
	scoreThreshold  float64
	client          *http.Client
	embedder        QueryEmbedder
	indexEmbedder   QueryEmbedder
	modelVersion    string
	collectionMu    sync.Mutex
	collectionReady bool
}

func NewQdrantMemoryVectorIndex(config MemoryVectorConfig) *QdrantMemoryVectorIndex {
	config = normalizeMemoryVectorConfig(config)
	if !MemoryVectorEnabled(config) {
		return nil
	}
	queryConfig := memoryVectorMessageSearchConfig(config, false)
	indexConfig := memoryVectorMessageSearchConfig(config, true)
	return &QdrantMemoryVectorIndex{
		config:         config,
		endpoint:       strings.TrimRight(strings.TrimSpace(config.QdrantEndpoint), "/"),
		collection:     strings.TrimSpace(config.QdrantCollection),
		apiKey:         strings.TrimSpace(config.QdrantAPIKey),
		scoreThreshold: config.QdrantScoreThreshold,
		client:         &http.Client{Timeout: config.Timeout},
		embedder:       NewMessageQueryEmbedder(queryConfig),
		indexEmbedder:  NewMessageQueryEmbedder(indexConfig),
		modelVersion:   messageEmbeddingModelVersion(indexConfig),
	}
}

func (i *QdrantMemoryVectorIndex) IndexMemory(ctx context.Context, item MemoryItem) error {
	if i == nil || i.endpoint == "" || i.collection == "" || i.indexEmbedder == nil {
		return errMessageSearchNotConfigured("memory qdrant vector indexer")
	}
	if !memoryVectorIndexable(item) {
		return nil
	}
	text := memoryVectorIndexText(item)
	vector, err := i.indexEmbedder.EmbedQuery(ctx, text)
	if err != nil {
		return err
	}
	if len(vector) == 0 {
		return fmt.Errorf("memory vector indexer received empty embedding")
	}
	if err := i.ensureCollection(ctx, len(vector)); err != nil {
		return err
	}
	return i.upsertPoint(ctx, memoryVectorID(item.UserID, item.ID), vector, memoryVectorPayload(item, text, i.modelVersion))
}

func (i *QdrantMemoryVectorIndex) SearchMemoryItems(ctx context.Context, userID, query, sessionID string, limit int, store MemoryItemService) ([]MemoryItem, error) {
	if i == nil || i.endpoint == "" || i.collection == "" || i.embedder == nil {
		return nil, errMessageSearchNotConfigured("memory qdrant backend")
	}
	if store == nil {
		return nil, fmt.Errorf("memory vector search requires memory item store")
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return []MemoryItem{}, nil
	}
	if limit <= 0 || limit > 200 {
		limit = defaultMemoryVectorLimit
	}
	vector, err := i.embedder.EmbedQuery(ctx, query)
	if err != nil {
		return nil, err
	}
	body := map[string]any{
		"vector":       vector,
		"limit":        limit,
		"with_payload": true,
		"filter": map[string]any{
			"must": []map[string]any{
				{"key": "user_id", "match": map[string]any{"value": strings.TrimSpace(userID)}},
				{"key": "status", "match": map[string]any{"value": MemoryStatusActive}},
			},
		},
	}
	if i.scoreThreshold > 0 {
		body["score_threshold"] = i.scoreThreshold
	}
	var response qdrantSearchResponse
	if err := i.postJSONOut(ctx, joinEndpointPath(i.endpoint, "collections", i.collection, "points", "search"), body, &response); err != nil {
		return nil, err
	}
	out := make([]MemoryItem, 0, len(response.Result))
	for _, hit := range response.Result {
		itemID := searchPayloadString(hit.Payload, "memory_id")
		if itemID == "" {
			continue
		}
		item, err := store.GetMemoryItem(ctx, userID, itemID)
		if err != nil {
			continue
		}
		item = normalizeMemoryItem(item)
		if item.Status != MemoryStatusActive || strings.TrimSpace(item.Content) == "" || isManagedPersonalizationMemory(item) || !memoryVisibleInSession(item, sessionID) {
			continue
		}
		item.Weight = clamp01(0.70*hit.Score + 0.30*memoryContextScore(item, query))
		out = append(out, item)
	}
	sortMemoryItems(out)
	return limitMemoryItems(out, defaultMemoryVectorLimit), nil
}

func (i *QdrantMemoryVectorIndex) DeleteMemory(ctx context.Context, userID, itemID string) error {
	if i == nil || i.endpoint == "" || i.collection == "" {
		return errMessageSearchNotConfigured("memory qdrant vector indexer")
	}
	body := map[string]any{"points": []string{memoryVectorID(userID, itemID)}}
	return i.postJSON(ctx, joinEndpointPath(i.endpoint, "collections", i.collection, "points", "delete")+"?wait=true", body)
}

func (i *QdrantMemoryVectorIndex) DeleteSession(ctx context.Context, userID, sessionID string) error {
	return i.deleteByFilter(ctx, []map[string]any{
		{"key": "user_id", "match": map[string]any{"value": strings.TrimSpace(userID)}},
		{"key": "session_id", "match": map[string]any{"value": strings.TrimSpace(sessionID)}},
	})
}

func (i *QdrantMemoryVectorIndex) DeleteUser(ctx context.Context, userID string) error {
	return i.deleteByFilter(ctx, []map[string]any{
		{"key": "user_id", "match": map[string]any{"value": strings.TrimSpace(userID)}},
	})
}

func (i *QdrantMemoryVectorIndex) deleteByFilter(ctx context.Context, must []map[string]any) error {
	if i == nil || i.endpoint == "" || i.collection == "" {
		return errMessageSearchNotConfigured("memory qdrant vector indexer")
	}
	body := map[string]any{"filter": map[string]any{"must": must}}
	return i.postJSON(ctx, joinEndpointPath(i.endpoint, "collections", i.collection, "points", "delete")+"?wait=true", body)
}

func (i *QdrantMemoryVectorIndex) ensureCollection(ctx context.Context, vectorSize int) error {
	if vectorSize <= 0 {
		return fmt.Errorf("qdrant memory collection vector size is required")
	}
	i.collectionMu.Lock()
	defer i.collectionMu.Unlock()
	if i.collectionReady {
		return nil
	}
	headers := make(http.Header)
	if i.apiKey != "" {
		headers.Set("api-key", i.apiKey)
	}
	status, bodyBytes, _, err := httpclient.New(
		httpclient.WithHTTPClient(i.client),
		httpclient.WithComponent("qdrant_memory_vector"),
	).Bytes(ctx, http.MethodGet, joinEndpointPath(i.endpoint, "collections", i.collection), nil,
		httpclient.WithHeaders(headers),
		httpclient.WithOKStatuses(http.StatusOK, http.StatusNotFound),
	)
	if err != nil {
		return err
	}
	if status >= 200 && status < 300 {
		i.collectionReady = true
		return nil
	}
	if status != http.StatusNotFound {
		return fmt.Errorf("qdrant memory collection check failed: status %d: %s", status, strings.TrimSpace(string(bodyBytes)))
	}
	createBody := map[string]any{
		"vectors": map[string]any{
			"size":     vectorSize,
			"distance": "Cosine",
		},
	}
	if err := i.putJSON(ctx, joinEndpointPath(i.endpoint, "collections", i.collection), createBody); err != nil {
		return err
	}
	i.collectionReady = true
	return nil
}

func (i *QdrantMemoryVectorIndex) upsertPoint(ctx context.Context, vectorID string, vector []float32, payload map[string]any) error {
	body := map[string]any{
		"points": []map[string]any{
			{"id": vectorID, "vector": vector, "payload": payload},
		},
	}
	return i.putJSON(ctx, joinEndpointPath(i.endpoint, "collections", i.collection, "points")+"?wait=true", body)
}

func (i *QdrantMemoryVectorIndex) putJSON(ctx context.Context, url string, payload any) error {
	return i.writeJSON(ctx, http.MethodPut, url, payload, nil)
}

func (i *QdrantMemoryVectorIndex) postJSON(ctx context.Context, url string, payload any) error {
	return i.writeJSON(ctx, http.MethodPost, url, payload, nil)
}

func (i *QdrantMemoryVectorIndex) postJSONOut(ctx context.Context, url string, payload any, out any) error {
	return i.writeJSON(ctx, http.MethodPost, url, payload, out)
}

func (i *QdrantMemoryVectorIndex) writeJSON(ctx context.Context, method, url string, payload any, out any) error {
	headers := make(http.Header)
	if i.apiKey != "" {
		headers.Set("api-key", i.apiKey)
	}
	err := httpclient.New(
		httpclient.WithHTTPClient(i.client),
		httpclient.WithComponent("qdrant_memory_vector"),
	).JSON(ctx, method, url, payload, out, httpclient.WithHeaders(headers))
	if err != nil {
		var statusErr *httpclient.StatusError
		if errors.As(err, &statusErr) {
			return fmt.Errorf("qdrant memory vector request failed: %s: %s", statusErr.Status, strings.TrimSpace(statusErr.Body))
		}
		return err
	}
	return nil
}

func normalizeMemoryVectorConfig(config MemoryVectorConfig) MemoryVectorConfig {
	if strings.TrimSpace(config.QdrantCollection) == "" {
		config.QdrantCollection = defaultMemoryVectorCollection
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
	msgConfig := normalizeMessageSearchConfig(memoryVectorMessageSearchConfig(config, false))
	config.EmbeddingProvider = msgConfig.EmbeddingProvider
	config.EmbeddingModel = msgConfig.EmbeddingModel
	config.EmbeddingLocation = msgConfig.EmbeddingLocation
	config.EmbeddingTaskType = msgConfig.EmbeddingTaskType
	config.EmbeddingIndexTaskType = msgConfig.EmbeddingIndexTaskType
	config.EmbeddingAutoTruncate = msgConfig.EmbeddingAutoTruncate
	return config
}

func MemoryVectorEnabled(config MemoryVectorConfig) bool {
	config = normalizeMemoryVectorConfig(config)
	if !config.Enabled {
		return false
	}
	return strings.TrimSpace(config.QdrantEndpoint) != "" &&
		strings.TrimSpace(config.QdrantCollection) != "" &&
		messageEmbeddingConfigured(memoryVectorMessageSearchConfig(config, false))
}

func MemoryVectorConfigFromMessageSearch(config MessageSearchConfig) MemoryVectorConfig {
	config = normalizeMessageSearchConfig(config)
	return MemoryVectorConfig{
		Enabled:                strings.TrimSpace(config.QdrantEndpoint) != "" && messageEmbeddingConfigured(config),
		QdrantEndpoint:         config.QdrantEndpoint,
		QdrantCollection:       defaultMemoryVectorCollection,
		QdrantAPIKey:           config.QdrantAPIKey,
		QdrantScoreThreshold:   config.QdrantScoreThreshold,
		EmbeddingProvider:      config.EmbeddingProvider,
		EmbeddingEndpoint:      config.EmbeddingEndpoint,
		EmbeddingAPIKey:        config.EmbeddingAPIKey,
		EmbeddingAccessToken:   config.EmbeddingAccessToken,
		EmbeddingModel:         config.EmbeddingModel,
		EmbeddingDimensions:    config.EmbeddingDimensions,
		EmbeddingTimeout:       config.EmbeddingTimeout,
		EmbeddingProjectID:     config.EmbeddingProjectID,
		EmbeddingLocation:      config.EmbeddingLocation,
		EmbeddingTaskType:      config.EmbeddingTaskType,
		EmbeddingIndexTaskType: config.EmbeddingIndexTaskType,
		EmbeddingAutoTruncate:  config.EmbeddingAutoTruncate,
		Timeout:                config.Timeout,
		RRFK:                   config.RRFK,
		CacheStore:             config.CacheStore,
		CacheMetrics:           config.CacheMetrics,
		CacheDefaultTTL:        config.CacheDefaultTTL,
		CacheFailOpen:          config.CacheFailOpen,
	}
}

func memoryVectorMessageSearchConfig(config MemoryVectorConfig, index bool) MessageSearchConfig {
	taskType := config.EmbeddingTaskType
	if index && strings.TrimSpace(config.EmbeddingIndexTaskType) != "" {
		taskType = config.EmbeddingIndexTaskType
	}
	return MessageSearchConfig{
		Backend:                messageSearchBackendSemantic,
		QdrantEndpoint:         config.QdrantEndpoint,
		QdrantCollection:       config.QdrantCollection,
		QdrantAPIKey:           config.QdrantAPIKey,
		QdrantScoreThreshold:   config.QdrantScoreThreshold,
		EmbeddingProvider:      config.EmbeddingProvider,
		EmbeddingEndpoint:      config.EmbeddingEndpoint,
		EmbeddingAPIKey:        config.EmbeddingAPIKey,
		EmbeddingAccessToken:   config.EmbeddingAccessToken,
		EmbeddingModel:         config.EmbeddingModel,
		EmbeddingDimensions:    config.EmbeddingDimensions,
		EmbeddingTimeout:       config.EmbeddingTimeout,
		EmbeddingProjectID:     config.EmbeddingProjectID,
		EmbeddingLocation:      config.EmbeddingLocation,
		EmbeddingTaskType:      taskType,
		EmbeddingIndexTaskType: config.EmbeddingIndexTaskType,
		EmbeddingAutoTruncate:  config.EmbeddingAutoTruncate,
		Timeout:                config.Timeout,
		RRFK:                   config.RRFK,
		CacheStore:             config.CacheStore,
		CacheMetrics:           config.CacheMetrics,
		CacheDefaultTTL:        config.CacheDefaultTTL,
		CacheFailOpen:          config.CacheFailOpen,
	}
}

func memoryVectorIndexable(item MemoryItem) bool {
	item = normalizeMemoryItem(item)
	if strings.TrimSpace(item.UserID) == "" || strings.TrimSpace(item.ID) == "" || strings.TrimSpace(item.Content) == "" {
		return false
	}
	if item.Status != MemoryStatusActive {
		return false
	}
	return !isManagedPersonalizationMemory(item)
}

func memoryVectorIndexText(item MemoryItem) string {
	item = normalizeMemoryItem(item)
	parts := []string{strings.TrimSpace(item.Content)}
	if item.Category != "" {
		parts = append(parts, "Category: "+item.Category)
	}
	if item.Namespace != "" {
		parts = append(parts, "Namespace: "+item.Namespace)
	}
	if len(item.Tags) > 0 {
		parts = append(parts, "Tags: "+strings.Join(item.Tags, ", "))
	}
	return strings.Join(compactStrings(parts), "\n")
}

func memoryVectorPayload(item MemoryItem, text, modelVersion string) map[string]any {
	item = normalizeMemoryItem(item)
	return map[string]any{
		"memory_id":     item.ID,
		"user_id":       item.UserID,
		"session_id":    item.SessionID,
		"namespace":     item.Namespace,
		"kind":          item.Kind,
		"level":         item.Level,
		"category":      item.Category,
		"source":        item.Source,
		"visibility":    item.Visibility,
		"status":        item.Status,
		"content":       text,
		"model_version": modelVersion,
		"updated_at":    item.UpdatedAt.UTC().Format(time.RFC3339Nano),
		"created_at":    item.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func memoryVectorID(userID, itemID string) string {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte(strings.Join([]string{strings.TrimSpace(userID), strings.TrimSpace(itemID)}, ":"))).String()
}

func annotateMemoryEmbedding(item MemoryItem, status, modelVersion, vectorID string, at time.Time, indexErr error) MemoryItem {
	if item.Metadata == nil {
		item.Metadata = map[string]any{}
	}
	item.Metadata["embedding_status"] = status
	item.Metadata["embedding_model_version"] = modelVersion
	item.Metadata["embedding_vector_id"] = vectorID
	item.Metadata["embedding_updated_at"] = at.UTC().Format(time.RFC3339Nano)
	if indexErr != nil {
		item.Metadata["embedding_error"] = truncateMemoryVectorError(indexErr.Error())
	} else {
		delete(item.Metadata, "embedding_error")
	}
	return item
}

func truncateMemoryVectorError(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 240 {
		return value[:240]
	}
	return value
}

func mergeMemoryRetrievalResults(vectorItems, keywordItems []MemoryItem, query, sessionID string, limit, k int) []MemoryItem {
	if k <= 0 {
		k = defaultMessageSearchRRFK
	}
	type scored struct {
		item  MemoryItem
		score float64
		best  int
	}
	merged := map[string]*scored{}
	add := func(items []MemoryItem, vectorWeight float64) {
		for rank, item := range items {
			item = normalizeMemoryItem(item)
			if item.Status != MemoryStatusActive || strings.TrimSpace(item.Content) == "" || isManagedPersonalizationMemory(item) || !memoryVisibleInSession(item, sessionID) {
				continue
			}
			item.Weight = clamp01((1-vectorWeight)*memoryContextScore(item, query) + vectorWeight*item.Weight)
			value, ok := merged[item.ID]
			if !ok {
				value = &scored{item: item, best: rank}
				merged[item.ID] = value
			}
			value.score += 1 / float64(k+rank+1)
			if rank < value.best {
				value.best = rank
				value.item = item
			}
		}
	}
	add(vectorItems, 0.45)
	add(keywordItems, 0)
	out := make([]scored, 0, len(merged))
	for _, value := range merged {
		value.item.Weight = clamp01(value.item.Weight + value.score)
		out = append(out, *value)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].score == out[j].score {
			return out[i].item.Weight > out[j].item.Weight
		}
		return out[i].score > out[j].score
	})
	items := make([]MemoryItem, 0, len(out))
	for _, value := range out {
		items = append(items, value.item)
	}
	return limitMemoryItems(items, limit)
}

func compactStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}
