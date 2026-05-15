package agentruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/segmentio/kafka-go"

	"claude-codex/internal/harness/state"
)

const (
	defaultRedisSessionContextTTL    = 24 * time.Hour
	defaultRedisSessionContextPrefix = "agent:session:ctx"
	defaultRedisSessionContextWindow = 200
	defaultRedisSessionListTTL       = 10 * time.Minute
	defaultRedisSessionListPrefix    = "agent:session:list"
	defaultRedisMessageSeqPrefix     = "agent:session:seq"
	defaultRedisMessageSeqLockTTL    = 30 * time.Second
)

const redisMessageSeqFloorScript = `
local current = redis.call("GET", KEYS[1])
local floor = tonumber(ARGV[1]) or 0
if current == false or tonumber(current) < floor then
	redis.call("SET", KEYS[1], floor)
end
return redis.call("GET", KEYS[1])
`

const redisMessageSeqReleaseLockScript = `
if redis.call("GET", KEYS[1]) == ARGV[1] then
	return redis.call("DEL", KEYS[1])
end
return 0
`

type MessageSequenceAllocator interface {
	NextMessageSeq(ctx context.Context, userID, sessionID string) (int64, error)
	SetMessageSeqFloor(ctx context.Context, userID, sessionID string, floor int64) error
}

type MessageSequenceLocker interface {
	AcquireMessageSeqLock(ctx context.Context, userID, sessionID string) (func(context.Context) error, error)
}

type MessageSequenceReconciler interface {
	ReconcileMessageSeq(ctx context.Context, userID, sessionID string, sqlMaxSeq int64) error
}

type RedisMessageSequenceAllocator struct {
	client  redis.UniversalClient
	prefix  string
	lockTTL time.Duration
}

func NewRedisMessageSequenceAllocator(client redis.UniversalClient) *RedisMessageSequenceAllocator {
	return NewRedisMessageSequenceAllocatorWithPrefix(client, "")
}

func NewRedisMessageSequenceAllocatorWithPrefix(client redis.UniversalClient, prefix string) *RedisMessageSequenceAllocator {
	prefix = strings.TrimRight(strings.TrimSpace(prefix), ":")
	if prefix == "" {
		prefix = defaultRedisMessageSeqPrefix
	}
	return &RedisMessageSequenceAllocator{client: client, prefix: prefix, lockTTL: defaultRedisMessageSeqLockTTL}
}

func (a *RedisMessageSequenceAllocator) NextMessageSeq(ctx context.Context, userID, sessionID string) (int64, error) {
	if a == nil || a.client == nil {
		return 0, fmt.Errorf("redis message sequence allocator is not configured")
	}
	key := a.key(userID, sessionID)
	if key == "" {
		return 0, fmt.Errorf("user ID and session ID are required")
	}
	return a.client.Incr(ctx, key).Result()
}

func (a *RedisMessageSequenceAllocator) SetMessageSeqFloor(ctx context.Context, userID, sessionID string, floor int64) error {
	if a == nil || a.client == nil {
		return nil
	}
	if floor < 0 {
		floor = 0
	}
	key := a.key(userID, sessionID)
	if key == "" {
		return fmt.Errorf("user ID and session ID are required")
	}
	return a.client.Eval(ctx, redisMessageSeqFloorScript, []string{key}, floor).Err()
}

func (a *RedisMessageSequenceAllocator) ReconcileMessageSeq(ctx context.Context, userID, sessionID string, sqlMaxSeq int64) error {
	if a == nil || a.client == nil {
		return nil
	}
	if sqlMaxSeq < 0 {
		sqlMaxSeq = 0
	}
	key := a.key(userID, sessionID)
	if key == "" {
		return fmt.Errorf("user ID and session ID are required")
	}
	return a.client.Set(ctx, key, sqlMaxSeq, 0).Err()
}

func (a *RedisMessageSequenceAllocator) AcquireMessageSeqLock(ctx context.Context, userID, sessionID string) (func(context.Context) error, error) {
	if a == nil || a.client == nil {
		return func(context.Context) error { return nil }, nil
	}
	key := a.lockKey(userID, sessionID)
	if key == "" {
		return nil, fmt.Errorf("user ID and session ID are required")
	}
	token := uuid.NewString()
	ttl := a.lockTTL
	if ttl <= 0 {
		ttl = defaultRedisMessageSeqLockTTL
	}
	backoff := 10 * time.Millisecond
	for {
		acquired, err := a.client.SetNX(ctx, key, token, ttl).Result()
		if err != nil {
			return nil, err
		}
		if acquired {
			return func(ctx context.Context) error {
				return a.client.Eval(ctx, redisMessageSeqReleaseLockScript, []string{key}, token).Err()
			}, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(backoff):
		}
		if backoff < 100*time.Millisecond {
			backoff *= 2
		}
	}
}

func (a *RedisMessageSequenceAllocator) key(userID, sessionID string) string {
	userID = strings.TrimSpace(userID)
	sessionID = strings.TrimSpace(sessionID)
	if userID == "" || sessionID == "" {
		return ""
	}
	prefix := strings.TrimRight(strings.TrimSpace(a.prefix), ":")
	if prefix == "" {
		prefix = defaultRedisMessageSeqPrefix
	}
	return fmt.Sprintf("%s:%s:%s", prefix, userPathID(userID), sessionID)
}

func (a *RedisMessageSequenceAllocator) lockKey(userID, sessionID string) string {
	key := a.key(userID, sessionID)
	if key == "" {
		return ""
	}
	return key + ":lock"
}

func NewRedisClientFromURL(rawURL string) (redis.UniversalClient, error) {
	if strings.TrimSpace(rawURL) == "" {
		return redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379"}), nil
	}
	clientURL, _, err := parseRedisURLWithPrefix(rawURL)
	if err != nil {
		return nil, err
	}
	options, err := redis.ParseURL(clientURL)
	if err != nil {
		return nil, err
	}
	return redis.NewClient(options), nil
}

func RedisPrefixFromURL(rawURL string) string {
	_, prefix, err := parseRedisURLWithPrefix(rawURL)
	if err != nil {
		return ""
	}
	return prefix
}

func parseRedisURLWithPrefix(rawURL string) (string, string, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return "", "", nil
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", "", err
	}
	query := parsed.Query()
	prefix := strings.TrimSpace(query.Get("prefix"))
	query.Del("prefix")
	parsed.RawQuery = query.Encode()
	return parsed.String(), prefix, nil
}

type RedisSessionContextCache struct {
	client      redis.UniversalClient
	ttl         time.Duration
	prefix      string
	maxMessages int
}

func NewRedisSessionContextCache(client redis.UniversalClient, ttl time.Duration) *RedisSessionContextCache {
	return NewRedisSessionContextCacheWithPrefix(client, ttl, "")
}

func NewRedisSessionContextCacheWithPrefix(client redis.UniversalClient, ttl time.Duration, prefix string) *RedisSessionContextCache {
	if ttl <= 0 {
		ttl = defaultRedisSessionContextTTL
	}
	prefix = strings.TrimRight(strings.TrimSpace(prefix), ":")
	if prefix == "" {
		prefix = defaultRedisSessionContextPrefix
	}
	return &RedisSessionContextCache{client: client, ttl: ttl, prefix: prefix, maxMessages: defaultRedisSessionContextWindow}
}

func (c *RedisSessionContextCache) ContextWindowSize() int {
	if c == nil || c.maxMessages <= 0 {
		return defaultRedisSessionContextWindow
	}
	return c.maxMessages
}

func (c *RedisSessionContextCache) GetContext(ctx context.Context, userID, sessionID string, opts SessionLoadOptions) ([]state.Message, bool, error) {
	if c == nil || c.client == nil {
		return nil, false, nil
	}
	opts = normalizeSessionLoadOptions(opts)
	if opts.MaxMessages > c.ContextWindowSize() {
		return nil, false, nil
	}
	items, err := c.client.LRange(ctx, c.key(userID, sessionID), 0, -1).Result()
	if err != nil {
		return nil, false, err
	}
	if len(items) == 0 {
		return nil, false, nil
	}
	messages := make([]state.Message, 0, len(items))
	for _, item := range items {
		var message state.Message
		if err := json.Unmarshal([]byte(item), &message); err != nil {
			return nil, false, err
		}
		messages = append(messages, message)
	}
	messages = applySessionTokenBudget(applySessionMaxMessages(messages, opts), opts)
	return messages, true, nil
}

func (c *RedisSessionContextCache) SetContext(ctx context.Context, userID, sessionID string, opts SessionLoadOptions, messages []state.Message) error {
	if c == nil || c.client == nil {
		return nil
	}
	key := c.key(userID, sessionID)
	pipe := c.client.TxPipeline()
	pipe.Del(ctx, key)
	values, err := c.contextMessageValues(messages)
	if err != nil {
		return err
	}
	if len(values) > 0 {
		pipe.RPush(ctx, key, values...)
		pipe.LTrim(ctx, key, int64(-c.ContextWindowSize()), -1)
		pipe.Expire(ctx, key, c.ttl)
	}
	_, err = pipe.Exec(ctx)
	return err
}

func (c *RedisSessionContextCache) AppendContextMessage(ctx context.Context, userID, sessionID string, message state.Message) error {
	if c == nil || c.client == nil {
		return nil
	}
	if message.Status != 0 && message.Status != state.MessageStatusNormal {
		return c.InvalidateContext(ctx, userID, sessionID)
	}
	if !message.IsContextUsed && message.ID != "" {
		return c.InvalidateContext(ctx, userID, sessionID)
	}
	data, err := json.Marshal(message)
	if err != nil {
		return err
	}
	key := c.key(userID, sessionID)
	pipe := c.client.TxPipeline()
	pipe.RPush(ctx, key, data)
	pipe.LTrim(ctx, key, int64(-c.ContextWindowSize()), -1)
	pipe.Expire(ctx, key, c.ttl)
	_, err = pipe.Exec(ctx)
	return err
}

func (c *RedisSessionContextCache) InvalidateContext(ctx context.Context, userID, sessionID string) error {
	if c == nil || c.client == nil {
		return nil
	}
	return c.client.Del(ctx, c.key(userID, sessionID)).Err()
}

func (c *RedisSessionContextCache) contextMessageValues(messages []state.Message) ([]any, error) {
	if len(messages) == 0 {
		return nil, nil
	}
	if len(messages) > c.ContextWindowSize() {
		messages = messages[len(messages)-c.ContextWindowSize():]
	}
	values := make([]any, 0, len(messages))
	for _, message := range messages {
		if message.Status != 0 && message.Status != state.MessageStatusNormal {
			continue
		}
		if !message.IsContextUsed && message.ID != "" {
			continue
		}
		data, err := json.Marshal(message)
		if err != nil {
			return nil, err
		}
		values = append(values, data)
	}
	return values, nil
}

func (c *RedisSessionContextCache) key(userID, sessionID string) string {
	prefix := strings.TrimRight(strings.TrimSpace(c.prefix), ":")
	if prefix == "" {
		prefix = defaultRedisSessionContextPrefix
	}
	return fmt.Sprintf("%s:%s:%s", prefix, userPathID(userID), sessionID)
}

type SessionListCache interface {
	GetSessions(ctx context.Context, userID string, offset, limit int) ([]*state.Session, bool, error)
	SetSessions(ctx context.Context, userID string, sessions []*state.Session) error
	UpsertSession(ctx context.Context, userID string, session *state.Session) error
	RemoveSession(ctx context.Context, userID, sessionID string) error
	InvalidateUser(ctx context.Context, userID string) error
}

type RedisSessionListCache struct {
	client redis.UniversalClient
	ttl    time.Duration
	prefix string
}

func NewRedisSessionListCache(client redis.UniversalClient, ttl time.Duration) *RedisSessionListCache {
	return NewRedisSessionListCacheWithPrefix(client, ttl, "")
}

func NewRedisSessionListCacheWithPrefix(client redis.UniversalClient, ttl time.Duration, prefix string) *RedisSessionListCache {
	if ttl <= 0 {
		ttl = defaultRedisSessionListTTL
	}
	prefix = strings.TrimRight(strings.TrimSpace(prefix), ":")
	if prefix == "" {
		prefix = defaultRedisSessionListPrefix
	}
	return &RedisSessionListCache{client: client, ttl: ttl, prefix: prefix}
}

func (c *RedisSessionListCache) GetSessions(ctx context.Context, userID string, offset, limit int) ([]*state.Session, bool, error) {
	if c == nil || c.client == nil {
		return nil, false, nil
	}
	if offset < 0 {
		offset = 0
	}
	if exists, err := c.client.Exists(ctx, c.readyKey(userID)).Result(); err != nil {
		return nil, false, err
	} else if exists == 0 {
		return nil, false, nil
	}
	start := int64(offset)
	stop := int64(-1)
	if limit > 0 {
		stop = int64(offset + limit - 1)
	}
	ids, err := c.client.ZRevRange(ctx, c.zsetKey(userID), start, stop).Result()
	if err != nil {
		return nil, false, err
	}
	if len(ids) == 0 {
		return []*state.Session{}, true, nil
	}
	values, err := c.client.HMGet(ctx, c.hashKey(userID), ids...).Result()
	if err != nil {
		return nil, false, err
	}
	out := make([]*state.Session, 0, len(values))
	for _, value := range values {
		if value == nil {
			return nil, false, nil
		}
		raw, ok := value.(string)
		if !ok {
			return nil, false, fmt.Errorf("unexpected redis session list value %T", value)
		}
		var session state.Session
		if err := json.Unmarshal([]byte(raw), &session); err != nil {
			return nil, false, err
		}
		out = append(out, &session)
	}
	return out, true, nil
}

func (c *RedisSessionListCache) SetSessions(ctx context.Context, userID string, sessions []*state.Session) error {
	if c == nil || c.client == nil {
		return nil
	}
	zsetKey := c.zsetKey(userID)
	hashKey := c.hashKey(userID)
	readyKey := c.readyKey(userID)
	pipe := c.client.TxPipeline()
	pipe.Del(ctx, zsetKey, hashKey)
	for _, session := range sessions {
		item, score, data, ok, err := redisSessionListCacheItem(session)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		pipe.HSet(ctx, hashKey, item, data)
		pipe.ZAdd(ctx, zsetKey, redis.Z{Score: score, Member: item})
	}
	pipe.Set(ctx, readyKey, "1", c.ttl)
	pipe.Expire(ctx, zsetKey, c.ttl)
	pipe.Expire(ctx, hashKey, c.ttl)
	_, err := pipe.Exec(ctx)
	return err
}

func (c *RedisSessionListCache) UpsertSession(ctx context.Context, userID string, session *state.Session) error {
	if c == nil || c.client == nil || session == nil {
		return nil
	}
	if session.Status == state.SessionStatusDeleted {
		return c.RemoveSession(ctx, userID, session.ID)
	}
	exists, err := c.client.Exists(ctx, c.readyKey(userID)).Result()
	if err != nil || exists == 0 {
		return err
	}
	item, score, data, ok, err := redisSessionListCacheItem(session)
	if err != nil || !ok {
		return err
	}
	pipe := c.client.TxPipeline()
	pipe.HSet(ctx, c.hashKey(userID), item, data)
	pipe.ZAdd(ctx, c.zsetKey(userID), redis.Z{Score: score, Member: item})
	pipe.Expire(ctx, c.readyKey(userID), c.ttl)
	pipe.Expire(ctx, c.zsetKey(userID), c.ttl)
	pipe.Expire(ctx, c.hashKey(userID), c.ttl)
	_, err = pipe.Exec(ctx)
	return err
}

func (c *RedisSessionListCache) RemoveSession(ctx context.Context, userID, sessionID string) error {
	if c == nil || c.client == nil {
		return nil
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil
	}
	pipe := c.client.TxPipeline()
	pipe.ZRem(ctx, c.zsetKey(userID), sessionID)
	pipe.HDel(ctx, c.hashKey(userID), sessionID)
	pipe.Expire(ctx, c.readyKey(userID), c.ttl)
	pipe.Expire(ctx, c.zsetKey(userID), c.ttl)
	pipe.Expire(ctx, c.hashKey(userID), c.ttl)
	_, err := pipe.Exec(ctx)
	return err
}

func (c *RedisSessionListCache) InvalidateUser(ctx context.Context, userID string) error {
	if c == nil || c.client == nil {
		return nil
	}
	return c.client.Del(ctx, c.readyKey(userID), c.zsetKey(userID), c.hashKey(userID)).Err()
}

func (c *RedisSessionListCache) keyPrefix(userID string) string {
	prefix := strings.TrimRight(strings.TrimSpace(c.prefix), ":")
	if prefix == "" {
		prefix = defaultRedisSessionListPrefix
	}
	return fmt.Sprintf("%s:%s", prefix, userPathID(userID))
}

func (c *RedisSessionListCache) readyKey(userID string) string {
	return c.keyPrefix(userID) + ":ready"
}

func (c *RedisSessionListCache) zsetKey(userID string) string {
	return c.keyPrefix(userID) + ":z"
}

func (c *RedisSessionListCache) hashKey(userID string) string {
	return c.keyPrefix(userID) + ":h"
}

func redisSessionListCacheItem(session *state.Session) (string, float64, string, bool, error) {
	if session == nil || strings.TrimSpace(session.ID) == "" || session.Status == state.SessionStatusDeleted {
		return "", 0, "", false, nil
	}
	clone := *session
	clone.Messages = nil
	data, err := json.Marshal(&clone)
	if err != nil {
		return "", 0, "", false, err
	}
	return clone.ID, sessionListScore(&clone), string(data), true, nil
}

func sessionListScore(session *state.Session) float64 {
	if session == nil {
		return 0
	}
	t := session.UpdatedAt
	if t.IsZero() {
		t = session.LastMessageAt
	}
	if t.IsZero() {
		t = session.StartedAt
	}
	return float64(t.UTC().UnixMilli())
}

type KafkaMessageEventPublisher struct {
	writer *kafka.Writer
	topic  string
}

func NewKafkaMessageEventPublisher(writer *kafka.Writer, topic string) *KafkaMessageEventPublisher {
	return &KafkaMessageEventPublisher{writer: writer, topic: topic}
}

func (p *KafkaMessageEventPublisher) PublishMessageEvent(ctx context.Context, event MessageEvent) error {
	if p == nil || p.writer == nil {
		return nil
	}
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	message := kafka.Message{
		Key:   []byte(event.SessionID),
		Value: data,
		Time:  event.CreatedAt,
	}
	if p.topic != "" && strings.TrimSpace(p.writer.Topic) == "" {
		message.Topic = p.topic
	}
	return p.writer.WriteMessages(ctx, message)
}
