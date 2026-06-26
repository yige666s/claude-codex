package agentruntime

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

type RateLimitPolicy interface {
	Allow(key string) bool
}

type RateLimitFactory func(limit int) (RateLimitPolicy, error)

type NoopRateLimiter struct{}

func (NoopRateLimiter) Allow(string) bool { return true }

type ConfigurableRateLimiter struct {
	mu      sync.RWMutex
	current RateLimitPolicy
	factory RateLimitFactory
}

func NewConfigurableRateLimiter(initial RateLimitPolicy, factory RateLimitFactory) *ConfigurableRateLimiter {
	if initial == nil {
		initial = NoopRateLimiter{}
	}
	return &ConfigurableRateLimiter{current: initial, factory: factory}
}

func (l *ConfigurableRateLimiter) Allow(key string) bool {
	if l == nil {
		return true
	}
	l.mu.RLock()
	current := l.current
	l.mu.RUnlock()
	if current == nil {
		return true
	}
	return current.Allow(key)
}

func (l *ConfigurableRateLimiter) Current() RateLimitPolicy {
	if l == nil {
		return NoopRateLimiter{}
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	if l.current == nil {
		return NoopRateLimiter{}
	}
	return l.current
}

func (l *ConfigurableRateLimiter) SetLimit(limit int) error {
	if l == nil {
		return nil
	}
	if limit < 0 {
		return fmt.Errorf("rate limit must be 0 or greater")
	}
	factory := l.factory
	if factory == nil {
		factory = func(limit int) (RateLimitPolicy, error) {
			if limit == 0 {
				return NoopRateLimiter{}, nil
			}
			return NewRateLimiter(limit, time.Minute), nil
		}
	}
	next, err := factory(limit)
	if err != nil {
		return err
	}
	if next == nil {
		next = NoopRateLimiter{}
	}
	l.mu.Lock()
	l.current = next
	l.mu.Unlock()
	return nil
}

type RateLimiter struct {
	limit  int
	window time.Duration
	mu     sync.Mutex
	hits   map[string][]time.Time
}

func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	if limit <= 0 {
		limit = 60
	}
	if window <= 0 {
		window = time.Minute
	}
	return &RateLimiter{limit: limit, window: window, hits: make(map[string][]time.Time)}
}

func (l *RateLimiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	cutoff := now.Add(-l.window)
	recent := l.hits[key][:0]
	for _, hit := range l.hits[key] {
		if hit.After(cutoff) {
			recent = append(recent, hit)
		}
	}
	if len(recent) >= l.limit {
		l.hits[key] = recent
		return false
	}
	recent = append(recent, now)
	l.hits[key] = recent
	return true
}

type RedisRateLimiter struct {
	Address  string
	Password string
	DB       int
	Prefix   string
	Limit    int
	Window   time.Duration
	Timeout  time.Duration
	FailOpen bool
	client   redis.UniversalClient
}

func (l RedisRateLimiter) Allow(key string) bool {
	ok, err := l.allow(context.Background(), key)
	if err != nil {
		return l.FailOpen
	}
	return ok
}

func (l RedisRateLimiter) Ping(ctx context.Context) error {
	client, closeClient, err := l.redisClient()
	if err != nil {
		return err
	}
	defer closeClient()
	ctx, cancel := l.withTimeout(ctx)
	defer cancel()
	return client.Ping(ctx).Err()
}

func (l RedisRateLimiter) allow(ctx context.Context, key string) (bool, error) {
	if l.Limit <= 0 {
		return true, nil
	}
	window := l.Window
	if window <= 0 {
		window = time.Minute
	}
	prefix := l.Prefix
	if prefix == "" {
		prefix = "agentapi:rate"
	}
	client, closeClient, err := l.redisClient()
	if err != nil {
		return false, err
	}
	defer closeClient()
	ctx, cancel := l.withTimeout(ctx)
	defer cancel()
	bucket := time.Now().UnixNano() / int64(window)
	redisKey := strings.TrimRight(prefix, ":") + ":" + key + ":" + strconv.FormatInt(bucket, 10)
	count, err := client.Eval(ctx, redisFixedWindowScript, []string{redisKey}, redisTTLSeconds(window)).Int64()
	if err != nil {
		return false, err
	}
	return count <= int64(l.Limit), nil
}

func NewRedisRateLimiter(rawURL string, limit int, window time.Duration, failOpen bool) (*RedisRateLimiter, error) {
	cfg := &RedisRateLimiter{Limit: limit, Window: window, FailOpen: failOpen}
	if strings.TrimSpace(rawURL) == "" {
		cfg.Address = "127.0.0.1:6379"
		cfg.client = redis.NewClient(&redis.Options{Addr: cfg.Address})
		return cfg, nil
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	cfg.Address = parsed.Host
	if password, ok := parsed.User.Password(); ok {
		cfg.Password = password
	}
	if db := strings.Trim(parsed.Path, "/"); db != "" {
		n, err := strconv.Atoi(db)
		if err != nil {
			return nil, fmt.Errorf("invalid redis db: %w", err)
		}
		cfg.DB = n
	}
	if prefix := parsed.Query().Get("prefix"); prefix != "" {
		cfg.Prefix = prefix
	}
	cfg.client, err = NewRedisClientFromURL(rawURL)
	if err != nil {
		return nil, err
	}
	return cfg, nil
}

const redisFixedWindowScript = `
local current = redis.call("INCR", KEYS[1])
if current == 1 then
	redis.call("EXPIRE", KEYS[1], ARGV[1])
end
return current
`

func (l RedisRateLimiter) redisClient() (redis.UniversalClient, func(), error) {
	if l.client != nil {
		return l.client, func() {}, nil
	}
	options := &redis.Options{Addr: l.Address, Password: l.Password, DB: l.DB}
	if options.Addr == "" {
		options.Addr = "127.0.0.1:6379"
	}
	client := redis.NewClient(options)
	return client, func() { _ = client.Close() }, nil
}

func (l RedisRateLimiter) withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	timeout := l.Timeout
	if timeout <= 0 {
		timeout = 750 * time.Millisecond
	}
	if _, ok := ctx.Deadline(); ok {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, timeout)
}

func redisTTLSeconds(window time.Duration) int {
	ttl := int((window + time.Second - time.Nanosecond) / time.Second)
	if ttl < 1 {
		return 1
	}
	return ttl + 2
}
