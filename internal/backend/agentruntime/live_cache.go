package agentruntime

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	defaultLiveSetupPromptCacheTTL    = time.Minute
	defaultRedisLiveSetupPromptPrefix = "agent:live:setup"
)

type LiveSetupPromptCache interface {
	GetLiveSetupPrompt(ctx context.Context, key string) (string, bool, error)
	SetLiveSetupPrompt(ctx context.Context, key, value string) error
}

type NoopLiveSetupPromptCache struct{}

func (NoopLiveSetupPromptCache) GetLiveSetupPrompt(context.Context, string) (string, bool, error) {
	return "", false, nil
}

func (NoopLiveSetupPromptCache) SetLiveSetupPrompt(context.Context, string, string) error {
	return nil
}

type MemoryLiveSetupPromptCache struct {
	mu      sync.Mutex
	ttl     time.Duration
	entries map[string]memoryLiveSetupPromptCacheEntry
}

type memoryLiveSetupPromptCacheEntry struct {
	value     string
	expiresAt time.Time
}

func NewMemoryLiveSetupPromptCache(ttl time.Duration) *MemoryLiveSetupPromptCache {
	if ttl <= 0 {
		ttl = defaultLiveSetupPromptCacheTTL
	}
	return &MemoryLiveSetupPromptCache{
		ttl:     ttl,
		entries: make(map[string]memoryLiveSetupPromptCacheEntry),
	}
}

func (c *MemoryLiveSetupPromptCache) GetLiveSetupPrompt(_ context.Context, key string) (string, bool, error) {
	if c == nil {
		return "", false, nil
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return "", false, nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok {
		return "", false, nil
	}
	if !entry.expiresAt.IsZero() && time.Now().After(entry.expiresAt) {
		delete(c.entries, key)
		return "", false, nil
	}
	return entry.value, true, nil
}

func (c *MemoryLiveSetupPromptCache) SetLiveSetupPrompt(_ context.Context, key, value string) error {
	if c == nil {
		return nil
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = memoryLiveSetupPromptCacheEntry{
		value:     value,
		expiresAt: time.Now().Add(c.ttl),
	}
	return nil
}

type RedisLiveSetupPromptCache struct {
	client redis.UniversalClient
	ttl    time.Duration
	prefix string
}

func NewRedisLiveSetupPromptCache(client redis.UniversalClient, ttl time.Duration) *RedisLiveSetupPromptCache {
	return NewRedisLiveSetupPromptCacheWithPrefix(client, ttl, "")
}

func NewRedisLiveSetupPromptCacheWithPrefix(client redis.UniversalClient, ttl time.Duration, prefix string) *RedisLiveSetupPromptCache {
	if ttl <= 0 {
		ttl = defaultLiveSetupPromptCacheTTL
	}
	prefix = strings.TrimRight(strings.TrimSpace(prefix), ":")
	if prefix == "" {
		prefix = defaultRedisLiveSetupPromptPrefix
	}
	return &RedisLiveSetupPromptCache{client: client, ttl: ttl, prefix: prefix}
}

func (c *RedisLiveSetupPromptCache) GetLiveSetupPrompt(ctx context.Context, key string) (string, bool, error) {
	if c == nil || c.client == nil {
		return "", false, nil
	}
	value, err := c.client.Get(ctx, c.key(key)).Result()
	if err == redis.Nil {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return value, true, nil
}

func (c *RedisLiveSetupPromptCache) SetLiveSetupPrompt(ctx context.Context, key, value string) error {
	if c == nil || c.client == nil {
		return nil
	}
	return c.client.Set(ctx, c.key(key), value, c.ttl).Err()
}

func (c *RedisLiveSetupPromptCache) key(key string) string {
	key = strings.Trim(strings.TrimSpace(key), ":")
	prefix := strings.TrimRight(strings.TrimSpace(c.prefix), ":")
	if prefix == "" {
		prefix = defaultRedisLiveSetupPromptPrefix
	}
	if key == "" {
		return prefix
	}
	return prefix + ":" + key
}
