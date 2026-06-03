package agentruntime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	defaultCacheTTL         = 10 * time.Minute
	defaultRedisCachePrefix = "agent:cache"
)

type CacheStore interface {
	Get(ctx context.Context, key string) ([]byte, bool, error)
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
	Delete(ctx context.Context, key string) error
	DeletePrefix(ctx context.Context, prefix string) error
}

type CachePolicy struct {
	Namespace string
	TTL       time.Duration
	FailOpen  bool
}

func (p CachePolicy) normalized() CachePolicy {
	p.Namespace = strings.Trim(strings.TrimSpace(p.Namespace), ":")
	if p.Namespace == "" {
		p.Namespace = "default"
	}
	if p.TTL <= 0 {
		p.TTL = defaultCacheTTL
	}
	return p
}

type CacheMetrics struct {
	mu      sync.Mutex
	entries map[string]CacheMetricSnapshot
}

type CacheMetricSnapshot struct {
	Hits         int64         `json:"hits"`
	Misses       int64         `json:"misses"`
	Writes       int64         `json:"writes"`
	Deletes      int64         `json:"deletes"`
	Errors       int64         `json:"errors"`
	TotalLatency time.Duration `json:"total_latency"`
}

func NewCacheMetrics() *CacheMetrics {
	return &CacheMetrics{entries: make(map[string]CacheMetricSnapshot)}
}

func (m *CacheMetrics) Snapshot() map[string]CacheMetricSnapshot {
	if m == nil {
		return map[string]CacheMetricSnapshot{}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[string]CacheMetricSnapshot, len(m.entries))
	for namespace, snapshot := range m.entries {
		out[namespace] = snapshot
	}
	return out
}

func (m *CacheMetrics) record(namespace string, latency time.Duration, mutate func(*CacheMetricSnapshot)) {
	if m == nil || mutate == nil {
		return
	}
	namespace = strings.TrimSpace(namespace)
	if namespace == "" {
		namespace = "default"
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	entry := m.entries[namespace]
	entry.TotalLatency += latency
	mutate(&entry)
	m.entries[namespace] = entry
}

type TypedCache[T any] struct {
	store   CacheStore
	policy  CachePolicy
	metrics *CacheMetrics
}

func NewTypedCache[T any](store CacheStore, policy CachePolicy, metrics *CacheMetrics) *TypedCache[T] {
	return &TypedCache[T]{store: store, policy: policy.normalized(), metrics: metrics}
}

func (c *TypedCache[T]) Get(ctx context.Context, key string) (T, bool, error) {
	var zero T
	if c == nil || c.store == nil {
		return zero, false, nil
	}
	started := time.Now()
	data, ok, err := c.store.Get(ctx, cacheNamespacedKey(c.policy.Namespace, key))
	if err != nil {
		c.metrics.record(c.policy.Namespace, time.Since(started), func(snapshot *CacheMetricSnapshot) {
			snapshot.Errors++
		})
		if c.policy.FailOpen {
			return zero, false, nil
		}
		return zero, false, err
	}
	if !ok {
		c.metrics.record(c.policy.Namespace, time.Since(started), func(snapshot *CacheMetricSnapshot) {
			snapshot.Misses++
		})
		return zero, false, nil
	}
	var value T
	if err := json.Unmarshal(data, &value); err != nil {
		c.metrics.record(c.policy.Namespace, time.Since(started), func(snapshot *CacheMetricSnapshot) {
			snapshot.Errors++
		})
		if c.policy.FailOpen {
			return zero, false, nil
		}
		return zero, false, err
	}
	c.metrics.record(c.policy.Namespace, time.Since(started), func(snapshot *CacheMetricSnapshot) {
		snapshot.Hits++
	})
	return value, true, nil
}

func (c *TypedCache[T]) Set(ctx context.Context, key string, value T) error {
	if c == nil || c.store == nil {
		return nil
	}
	started := time.Now()
	data, err := json.Marshal(value)
	if err != nil {
		c.metrics.record(c.policy.Namespace, time.Since(started), func(snapshot *CacheMetricSnapshot) {
			snapshot.Errors++
		})
		if c.policy.FailOpen {
			return nil
		}
		return err
	}
	err = c.store.Set(ctx, cacheNamespacedKey(c.policy.Namespace, key), data, c.policy.TTL)
	if err != nil {
		c.metrics.record(c.policy.Namespace, time.Since(started), func(snapshot *CacheMetricSnapshot) {
			snapshot.Errors++
		})
		if c.policy.FailOpen {
			return nil
		}
		return err
	}
	c.metrics.record(c.policy.Namespace, time.Since(started), func(snapshot *CacheMetricSnapshot) {
		snapshot.Writes++
	})
	return nil
}

func (c *TypedCache[T]) Delete(ctx context.Context, key string) error {
	if c == nil || c.store == nil {
		return nil
	}
	started := time.Now()
	err := c.store.Delete(ctx, cacheNamespacedKey(c.policy.Namespace, key))
	if err != nil {
		c.metrics.record(c.policy.Namespace, time.Since(started), func(snapshot *CacheMetricSnapshot) {
			snapshot.Errors++
		})
		if c.policy.FailOpen {
			return nil
		}
		return err
	}
	c.metrics.record(c.policy.Namespace, time.Since(started), func(snapshot *CacheMetricSnapshot) {
		snapshot.Deletes++
	})
	return nil
}

func (c *TypedCache[T]) DeleteNamespace(ctx context.Context) error {
	if c == nil || c.store == nil {
		return nil
	}
	started := time.Now()
	err := c.store.DeletePrefix(ctx, strings.TrimRight(c.policy.Namespace, ":")+":")
	if err != nil {
		c.metrics.record(c.policy.Namespace, time.Since(started), func(snapshot *CacheMetricSnapshot) {
			snapshot.Errors++
		})
		if c.policy.FailOpen {
			return nil
		}
		return err
	}
	c.metrics.record(c.policy.Namespace, time.Since(started), func(snapshot *CacheMetricSnapshot) {
		snapshot.Deletes++
	})
	return nil
}

type CacheKeyOptions struct {
	Namespace string
	UserID    string
	SessionID string
	Version   string
	Parts     []string
}

func BuildCacheKey(opts CacheKeyOptions) string {
	items := make([]string, 0, len(opts.Parts)+3)
	if namespace := strings.Trim(strings.TrimSpace(opts.Namespace), ":"); namespace != "" {
		items = append(items, "n="+namespace)
	}
	if userID := strings.TrimSpace(opts.UserID); userID != "" {
		items = append(items, "u="+userPathID(userID))
	}
	if sessionID := strings.TrimSpace(opts.SessionID); sessionID != "" {
		items = append(items, "s="+sessionID)
	}
	if version := strings.TrimSpace(opts.Version); version != "" {
		items = append(items, "v="+version)
	}
	items = append(items, opts.Parts...)
	return cacheHashKey(items...)
}

func cacheHashKey(parts ...string) string {
	hash := sha256.New()
	for _, part := range parts {
		part = strings.TrimSpace(part)
		_, _ = fmt.Fprintf(hash, "%d:%s|", len(part), part)
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func cacheNamespacedKey(namespace, key string) string {
	namespace = strings.Trim(strings.TrimSpace(namespace), ":")
	key = strings.Trim(strings.TrimSpace(key), ":")
	if namespace == "" {
		return key
	}
	if key == "" {
		return namespace
	}
	return namespace + ":" + key
}

type NoopCacheStore struct{}

func (NoopCacheStore) Get(context.Context, string) ([]byte, bool, error) {
	return nil, false, nil
}

func (NoopCacheStore) Set(context.Context, string, []byte, time.Duration) error {
	return nil
}

func (NoopCacheStore) Delete(context.Context, string) error {
	return nil
}

func (NoopCacheStore) DeletePrefix(context.Context, string) error {
	return nil
}

type MemoryCacheStore struct {
	mu         sync.Mutex
	defaultTTL time.Duration
	entries    map[string]memoryCacheEntry
}

type memoryCacheEntry struct {
	value     []byte
	expiresAt time.Time
}

func NewMemoryCacheStore(defaultTTL time.Duration) *MemoryCacheStore {
	if defaultTTL <= 0 {
		defaultTTL = defaultCacheTTL
	}
	return &MemoryCacheStore{defaultTTL: defaultTTL, entries: make(map[string]memoryCacheEntry)}
}

func (s *MemoryCacheStore) Get(_ context.Context, key string) ([]byte, bool, error) {
	if s == nil {
		return nil, false, nil
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, false, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.entries[key]
	if !ok {
		return nil, false, nil
	}
	if !entry.expiresAt.IsZero() && time.Now().After(entry.expiresAt) {
		delete(s.entries, key)
		return nil, false, nil
	}
	return append([]byte(nil), entry.value...), true, nil
}

func (s *MemoryCacheStore) Set(_ context.Context, key string, value []byte, ttl time.Duration) error {
	if s == nil {
		return nil
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return nil
	}
	if ttl <= 0 {
		ttl = s.defaultTTL
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries[key] = memoryCacheEntry{value: append([]byte(nil), value...), expiresAt: time.Now().Add(ttl)}
	return nil
}

func (s *MemoryCacheStore) Delete(_ context.Context, key string) error {
	if s == nil {
		return nil
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.entries, key)
	return nil
}

func (s *MemoryCacheStore) DeletePrefix(_ context.Context, prefix string) error {
	if s == nil {
		return nil
	}
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for key := range s.entries {
		if strings.HasPrefix(key, prefix) {
			delete(s.entries, key)
		}
	}
	return nil
}

type RedisCacheStore struct {
	client     redis.UniversalClient
	prefix     string
	defaultTTL time.Duration
}

func NewRedisCacheStore(client redis.UniversalClient, defaultTTL time.Duration) *RedisCacheStore {
	return NewRedisCacheStoreWithPrefix(client, defaultTTL, "")
}

func NewRedisCacheStoreWithPrefix(client redis.UniversalClient, defaultTTL time.Duration, prefix string) *RedisCacheStore {
	if defaultTTL <= 0 {
		defaultTTL = defaultCacheTTL
	}
	prefix = strings.TrimRight(strings.TrimSpace(prefix), ":")
	if prefix == "" {
		prefix = defaultRedisCachePrefix
	}
	return &RedisCacheStore{client: client, prefix: prefix, defaultTTL: defaultTTL}
}

func (s *RedisCacheStore) Get(ctx context.Context, key string) ([]byte, bool, error) {
	if s == nil || s.client == nil {
		return nil, false, nil
	}
	value, err := s.client.Get(ctx, s.key(key)).Bytes()
	if err == redis.Nil {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return value, true, nil
}

func (s *RedisCacheStore) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if s == nil || s.client == nil {
		return nil
	}
	if ttl <= 0 {
		ttl = s.defaultTTL
	}
	return s.client.Set(ctx, s.key(key), value, ttl).Err()
}

func (s *RedisCacheStore) Delete(ctx context.Context, key string) error {
	if s == nil || s.client == nil {
		return nil
	}
	return s.client.Del(ctx, s.key(key)).Err()
}

func (s *RedisCacheStore) DeletePrefix(ctx context.Context, prefix string) error {
	if s == nil || s.client == nil {
		return nil
	}
	pattern := s.key(strings.TrimRight(strings.TrimSpace(prefix), ":") + ":*")
	iter := s.client.Scan(ctx, 0, pattern, 100).Iterator()
	keys := make([]string, 0, 100)
	for iter.Next(ctx) {
		keys = append(keys, iter.Val())
		if len(keys) >= 100 {
			if err := s.client.Del(ctx, keys...).Err(); err != nil {
				return err
			}
			keys = keys[:0]
		}
	}
	if err := iter.Err(); err != nil {
		return err
	}
	if len(keys) > 0 {
		return s.client.Del(ctx, keys...).Err()
	}
	return nil
}

func (s *RedisCacheStore) key(key string) string {
	key = strings.Trim(strings.TrimSpace(key), ":")
	prefix := strings.TrimRight(strings.TrimSpace(s.prefix), ":")
	if prefix == "" {
		prefix = defaultRedisCachePrefix
	}
	if key == "" {
		return prefix
	}
	return prefix + ":" + key
}
