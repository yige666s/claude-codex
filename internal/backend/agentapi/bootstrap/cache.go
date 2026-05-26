package bootstrap

import (
	"context"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"claude-codex/internal/backend/agentruntime"
)

type RedisHealthCloser interface {
	Ping(context.Context) *redis.StatusCmd
	Close() error
}

func BuildRateLimiter(backend, redisURL string, limit int, window time.Duration, redisFailOpen bool) agentruntime.RateLimitPolicy {
	switch strings.ToLower(strings.TrimSpace(backend)) {
	case "redis":
		limiter, err := agentruntime.NewRedisRateLimiter(redisURL, limit, window, redisFailOpen)
		if err != nil {
			logFatalf("init redis rate limiter: %v", err)
		}
		return limiter
	case "gateway", "none", "off", "disabled":
		return agentruntime.NoopRateLimiter{}
	default:
		return agentruntime.NewRateLimiter(limit, window)
	}
}

func BuildMessageContextCache(backend, redisURL string, ttl time.Duration) (agentruntime.SessionContextCache, RedisHealthCloser) {
	switch strings.ToLower(strings.TrimSpace(backend)) {
	case "redis":
		client, err := agentruntime.NewRedisClientFromURL(redisURL)
		if err != nil {
			logFatalf("init redis message context cache: %v", err)
		}
		return agentruntime.NewRedisSessionContextCacheWithPrefix(client, ttl, agentruntime.RedisPrefixFromURL(redisURL)), client
	case "none", "off", "disabled":
		return agentruntime.NoopSessionContextCache{}, nil
	default:
		return agentruntime.NewMemorySessionContextCache(), nil
	}
}

func BuildSessionListCache(backend, redisURL string, ttl time.Duration) (agentruntime.SessionListCache, RedisHealthCloser) {
	switch strings.ToLower(strings.TrimSpace(backend)) {
	case "redis":
		client, err := agentruntime.NewRedisClientFromURL(redisURL)
		if err != nil {
			logFatalf("init redis session list cache: %v", err)
		}
		return agentruntime.NewRedisSessionListCacheWithPrefix(client, ttl, agentruntime.RedisPrefixFromURL(redisURL)), client
	default:
		return nil, nil
	}
}

func BuildMessageSequenceAllocator(backend, redisURL string) (agentruntime.MessageSequenceAllocator, RedisHealthCloser) {
	switch strings.ToLower(strings.TrimSpace(backend)) {
	case "redis":
		client, err := agentruntime.NewRedisClientFromURL(redisURL)
		if err != nil {
			logFatalf("init redis message sequence allocator: %v", err)
		}
		return agentruntime.NewRedisMessageSequenceAllocatorWithPrefix(client, agentruntime.RedisPrefixFromURL(redisURL)), client
	default:
		return nil, nil
	}
}

func BuildRedisJobQueue(redisURL string, config agentruntime.RedisJobQueueConfig) (*agentruntime.RedisJobQueue, redis.UniversalClient) {
	client, err := agentruntime.NewRedisClientFromURL(redisURL)
	if err != nil {
		logFatalf("init redis job queue: %v", err)
	}
	queue := agentruntime.NewRedisJobQueue(client, config)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := queue.Ensure(ctx); err != nil {
		logFatalf("init redis job queue stream: %v", err)
	}
	return queue, client
}

func MessageEventsBackendMode(backend string) (publishKafka bool, localVectorIndexing bool) {
	switch strings.ToLower(strings.TrimSpace(backend)) {
	case "kafka":
		return true, false
	case "dual", "both", "local+kafka", "kafka+local":
		return true, true
	case "none", "off", "disabled":
		return false, false
	default:
		return false, true
	}
}

func BuildKafkaMessageEventPublisher(config agentruntime.KafkaMessageEventConfig) (agentruntime.MessageEventPublisher, interface{ Close() error }) {
	writer, err := agentruntime.NewKafkaMessageEventWriter(config)
	if err != nil {
		logFatalf("init kafka message event publisher: %v", err)
	}
	return agentruntime.NewKafkaMessageEventPublisher(writer, config.Topic), writer
}
