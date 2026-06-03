package agentruntime

import (
	"context"
	"strings"
	"time"
)

const embeddingCacheNamespace = "embedding"

type CachedQueryEmbedder struct {
	base  QueryEmbedder
	cache *TypedCache[[]float32]
	key   func(string) string
}

func NewCachedQueryEmbedder(base QueryEmbedder, config MessageSearchConfig) QueryEmbedder {
	if base == nil || config.CacheStore == nil {
		return base
	}
	ttl := config.CacheDefaultTTL
	if ttl <= 0 {
		ttl = defaultCacheTTL
	}
	return &CachedQueryEmbedder{
		base: base,
		cache: NewTypedCache[[]float32](config.CacheStore, CachePolicy{
			Namespace: embeddingCacheNamespace,
			TTL:       ttl,
			FailOpen:  config.CacheFailOpen,
		}, config.CacheMetrics),
		key: func(query string) string {
			return embeddingCacheKey(config, query)
		},
	}
}

func (e *CachedQueryEmbedder) EmbedQuery(ctx context.Context, query string) ([]float32, error) {
	if e == nil || e.base == nil {
		return nil, errMessageSearchNotConfigured("embedding backend")
	}
	if e.cache != nil {
		if vector, ok, err := e.cache.Get(ctx, e.key(query)); err != nil {
			return nil, err
		} else if ok {
			return append([]float32(nil), vector...), nil
		}
	}
	vector, err := e.base.EmbedQuery(ctx, query)
	if err != nil {
		return nil, err
	}
	if e.cache != nil && len(vector) > 0 {
		_ = e.cache.Set(ctx, e.key(query), vector)
	}
	return vector, nil
}

func embeddingCacheKey(config MessageSearchConfig, query string) string {
	config = normalizeMessageSearchConfig(config)
	return BuildCacheKey(CacheKeyOptions{
		Namespace: embeddingCacheNamespace,
		Version:   messageEmbeddingModelVersion(config),
		Parts: []string{
			"provider=" + strings.TrimSpace(config.EmbeddingProvider),
			"endpoint=" + strings.TrimSpace(config.EmbeddingEndpoint),
			"project=" + strings.TrimSpace(config.EmbeddingProjectID),
			"location=" + strings.TrimSpace(config.EmbeddingLocation),
			"task=" + strings.TrimSpace(config.EmbeddingTaskType),
			"auto_truncate=" + boolCachePart(config.EmbeddingAutoTruncate),
			"query=" + strings.TrimSpace(query),
		},
	})
}

func cacheTTLOrDefault(ttl time.Duration) time.Duration {
	if ttl <= 0 {
		return defaultCacheTTL
	}
	return ttl
}

func boolCachePart(value bool) string {
	if value {
		return "true"
	}
	return "false"
}
