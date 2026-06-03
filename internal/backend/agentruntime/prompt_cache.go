package agentruntime

import (
	"context"
	"strings"
	"time"
)

const promptResolverCacheNamespace = "prompt_resolver"

func NewCachedPromptResolver(store PromptStore, fallbacks map[string]PromptVersion, cacheStore CacheStore, ttl time.Duration, failOpen bool, metrics *CacheMetrics) PromptResolver {
	resolver := NewPromptResolver(store, fallbacks)
	if cacheStore == nil {
		return resolver
	}
	resolver.Cache = NewTypedCache[PromptResolution](cacheStore, CachePolicy{
		Namespace: promptResolverCacheNamespace,
		TTL:       ttl,
		FailOpen:  failOpen,
	}, metrics)
	return resolver
}

type CacheInvalidatingPromptStore struct {
	Store      PromptStore
	CacheStore CacheStore
}

func NewCacheInvalidatingPromptStore(store PromptStore, cacheStore CacheStore) PromptStore {
	if store == nil || cacheStore == nil {
		return store
	}
	return &CacheInvalidatingPromptStore{Store: store, CacheStore: cacheStore}
}

func (s *CacheInvalidatingPromptStore) Init(ctx context.Context) error {
	return s.Store.Init(ctx)
}

func (s *CacheInvalidatingPromptStore) UpsertPrompt(ctx context.Context, prompt PromptTemplate) (PromptTemplate, error) {
	out, err := s.Store.UpsertPrompt(ctx, prompt)
	if err == nil {
		_ = s.invalidate(ctx)
	}
	return out, err
}

func (s *CacheInvalidatingPromptStore) GetPrompt(ctx context.Context, id string) (PromptTemplate, error) {
	return s.Store.GetPrompt(ctx, id)
}

func (s *CacheInvalidatingPromptStore) ListPrompts(ctx context.Context, filter PromptListFilter) ([]PromptTemplate, error) {
	return s.Store.ListPrompts(ctx, filter)
}

func (s *CacheInvalidatingPromptStore) CreatePromptVersion(ctx context.Context, version PromptVersion) (PromptVersion, error) {
	out, err := s.Store.CreatePromptVersion(ctx, version)
	if err == nil && out.Status == PromptStatusPublished {
		_ = s.invalidate(ctx)
	}
	return out, err
}

func (s *CacheInvalidatingPromptStore) GetPromptVersion(ctx context.Context, promptID, version string) (PromptVersion, error) {
	return s.Store.GetPromptVersion(ctx, promptID, version)
}

func (s *CacheInvalidatingPromptStore) GetPublishedPromptVersion(ctx context.Context, promptID string) (PromptVersion, error) {
	return s.Store.GetPublishedPromptVersion(ctx, promptID)
}

func (s *CacheInvalidatingPromptStore) ListPromptVersions(ctx context.Context, promptID string) ([]PromptVersion, error) {
	return s.Store.ListPromptVersions(ctx, promptID)
}

func (s *CacheInvalidatingPromptStore) PublishPromptVersion(ctx context.Context, promptID, version, actor, changelog string) (PromptVersion, error) {
	out, err := s.Store.PublishPromptVersion(ctx, promptID, version, actor, changelog)
	if err == nil {
		_ = s.invalidate(ctx)
	}
	return out, err
}

func (s *CacheInvalidatingPromptStore) RollbackPromptVersion(ctx context.Context, promptID, version, actor, changelog string) (PromptVersion, error) {
	out, err := s.Store.RollbackPromptVersion(ctx, promptID, version, actor, changelog)
	if err == nil {
		_ = s.invalidate(ctx)
	}
	return out, err
}

func (s *CacheInvalidatingPromptStore) UpsertPromptExperiment(ctx context.Context, experiment PromptExperiment, variants []PromptExperimentVariant) (PromptExperiment, error) {
	out, err := s.Store.UpsertPromptExperiment(ctx, experiment, variants)
	if err == nil {
		_ = s.invalidate(ctx)
	}
	return out, err
}

func (s *CacheInvalidatingPromptStore) GetPromptExperiment(ctx context.Context, id string) (PromptExperiment, []PromptExperimentVariant, error) {
	return s.Store.GetPromptExperiment(ctx, id)
}

func (s *CacheInvalidatingPromptStore) ListPromptExperiments(ctx context.Context, filter PromptExperimentFilter) ([]PromptExperiment, error) {
	return s.Store.ListPromptExperiments(ctx, filter)
}

func (s *CacheInvalidatingPromptStore) UpdatePromptExperimentStatus(ctx context.Context, id, status, winnerVariantID, actor string) (PromptExperiment, error) {
	out, err := s.Store.UpdatePromptExperimentStatus(ctx, id, status, winnerVariantID, actor)
	if err == nil {
		_ = s.invalidate(ctx)
	}
	return out, err
}

func (s *CacheInvalidatingPromptStore) invalidate(ctx context.Context) error {
	if s == nil || s.CacheStore == nil {
		return nil
	}
	return s.CacheStore.DeletePrefix(ctx, strings.TrimRight(promptResolverCacheNamespace, ":")+":")
}
