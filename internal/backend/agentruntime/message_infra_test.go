package agentruntime

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"claude-codex/internal/harness/state"
)

func TestNewRedisClientFromURLStripsCachePrefixQuery(t *testing.T) {
	client, err := NewRedisClientFromURL("redis://:secret@localhost:6379/3?prefix=agentapi:message:ctx")
	if err != nil {
		t.Fatalf("new redis client: %v", err)
	}
	defer client.Close()

	redisClient, ok := client.(*redis.Client)
	if !ok {
		t.Fatalf("expected *redis.Client, got %T", client)
	}
	options := redisClient.Options()
	if options.Addr != "localhost:6379" || options.DB != 3 || options.Password != "secret" {
		t.Fatalf("unexpected redis options: addr=%q db=%d password=%q", options.Addr, options.DB, options.Password)
	}
	if prefix := RedisPrefixFromURL("redis://localhost:6379/3?prefix=agentapi:message:ctx"); prefix != "agentapi:message:ctx" {
		t.Fatalf("unexpected prefix %q", prefix)
	}
}

func TestRedisSessionContextCacheUsesConfiguredPrefix(t *testing.T) {
	cache := NewRedisSessionContextCacheWithPrefix(nil, time.Hour, "agentapi:message:ctx:")
	key := cache.key("user@example.com", "session-1")
	want := "agentapi:message:ctx:" + userPathID("user@example.com") + ":session-1"
	if key != want {
		t.Fatalf("unexpected key %q", key)
	}
}

func TestRedisSessionContextCacheRoundTrip(t *testing.T) {
	rawURL := os.Getenv("AGENT_RUNTIME_TEST_REDIS_URL")
	if rawURL == "" {
		t.Skip("set AGENT_RUNTIME_TEST_REDIS_URL to run redis integration test")
	}
	client, err := NewRedisClientFromURL(rawURL)
	if err != nil {
		t.Fatalf("new redis client: %v", err)
	}
	defer client.Close()
	cache := NewRedisSessionContextCacheWithPrefix(client, time.Minute, RedisPrefixFromURL(rawURL))
	cache.maxMessages = 2
	ctx := context.Background()
	userID := "message-ctx-" + time.Now().UTC().Format("20060102T150405.000000000")
	sessionID := "session-1"
	defer func() { _ = cache.InvalidateContext(context.Background(), userID, sessionID) }()

	for i, id := range []string{"m1", "m2", "m3"} {
		if err := cache.AppendContextMessage(ctx, userID, sessionID, state.Message{
			ID:            id,
			UserID:        userID,
			SessionID:     sessionID,
			SeqNo:         int64(i + 1),
			Role:          state.MessageRoleUser,
			Content:       id,
			Status:        state.MessageStatusNormal,
			IsContextUsed: true,
		}); err != nil {
			t.Fatalf("append context message: %v", err)
		}
	}
	messages, ok, err := cache.GetContext(ctx, userID, sessionID, SessionLoadOptions{MaxMessages: 2, MaxTokens: 100})
	if err != nil {
		t.Fatalf("get context: %v", err)
	}
	if !ok || len(messages) != 2 || messages[0].ID != "m2" || messages[1].ID != "m3" {
		t.Fatalf("unexpected list context ok=%t messages=%#v", ok, messages)
	}
	if _, ok, err := cache.GetContext(ctx, userID, sessionID, SessionLoadOptions{MaxMessages: 3, MaxTokens: 100}); err != nil || ok {
		t.Fatalf("oversized request should miss, ok=%t err=%v", ok, err)
	}
	if err := cache.SetContext(ctx, userID, sessionID, DefaultSessionLoadOptions(), []state.Message{{
		ID:            "refill",
		UserID:        userID,
		SessionID:     sessionID,
		SeqNo:         4,
		Role:          state.MessageRoleAssistant,
		Content:       "refill",
		Status:        state.MessageStatusNormal,
		IsContextUsed: true,
	}}); err != nil {
		t.Fatalf("set context: %v", err)
	}
	messages, ok, err = cache.GetContext(ctx, userID, sessionID, SessionLoadOptions{MaxMessages: 1, MaxTokens: 100})
	if err != nil {
		t.Fatalf("get refilled context: %v", err)
	}
	if !ok || len(messages) != 1 || messages[0].ID != "refill" {
		t.Fatalf("unexpected refilled context ok=%t messages=%#v", ok, messages)
	}
}

func TestRedisMessageSequenceAllocatorUsesConfiguredPrefix(t *testing.T) {
	allocator := NewRedisMessageSequenceAllocatorWithPrefix(nil, "agentapi:message:seq:")
	key := allocator.key("user@example.com", "session-1")
	want := "agentapi:message:seq:" + userPathID("user@example.com") + ":session-1"
	if key != want {
		t.Fatalf("unexpected key %q", key)
	}
}

func TestRedisMessageSequenceAllocatorRoundTrip(t *testing.T) {
	rawURL := os.Getenv("AGENT_RUNTIME_TEST_REDIS_URL")
	if rawURL == "" {
		t.Skip("set AGENT_RUNTIME_TEST_REDIS_URL to run redis integration test")
	}
	client, err := NewRedisClientFromURL(rawURL)
	if err != nil {
		t.Fatalf("new redis client: %v", err)
	}
	defer client.Close()
	allocator := NewRedisMessageSequenceAllocatorWithPrefix(client, RedisPrefixFromURL(rawURL))
	ctx := context.Background()
	userID := "message-seq-" + time.Now().UTC().Format("20060102T150405.000000000")
	sessionID := "session-1"
	defer client.Del(context.Background(), allocator.key(userID, sessionID))

	seq, err := allocator.NextMessageSeq(ctx, userID, sessionID)
	if err != nil {
		t.Fatalf("next seq: %v", err)
	}
	if seq != 1 {
		t.Fatalf("expected first seq 1, got %d", seq)
	}
	if err := allocator.SetMessageSeqFloor(ctx, userID, sessionID, 10); err != nil {
		t.Fatalf("set floor: %v", err)
	}
	seq, err = allocator.NextMessageSeq(ctx, userID, sessionID)
	if err != nil {
		t.Fatalf("next seq after floor: %v", err)
	}
	if seq != 11 {
		t.Fatalf("expected seq 11 after floor, got %d", seq)
	}
	if err := allocator.SetMessageSeqFloor(ctx, userID, sessionID, 5); err != nil {
		t.Fatalf("set lower floor: %v", err)
	}
	seq, err = allocator.NextMessageSeq(ctx, userID, sessionID)
	if err != nil {
		t.Fatalf("next seq after lower floor: %v", err)
	}
	if seq != 12 {
		t.Fatalf("expected lower floor to preserve current seq, got %d", seq)
	}
	if err := allocator.ReconcileMessageSeq(ctx, userID, sessionID, 3); err != nil {
		t.Fatalf("reconcile seq: %v", err)
	}
	seq, err = allocator.NextMessageSeq(ctx, userID, sessionID)
	if err != nil {
		t.Fatalf("next seq after reconcile: %v", err)
	}
	if seq != 4 {
		t.Fatalf("expected reconcile to recover counter to SQL max, got %d", seq)
	}
	release, err := allocator.AcquireMessageSeqLock(ctx, userID, sessionID)
	if err != nil {
		t.Fatalf("acquire seq lock: %v", err)
	}
	if release == nil {
		t.Fatal("expected release function")
	}
	if err := release(ctx); err != nil {
		t.Fatalf("release seq lock: %v", err)
	}
}

func TestRedisSessionListCacheKeysAndPayload(t *testing.T) {
	cache := NewRedisSessionListCacheWithPrefix(nil, time.Hour, "agentapi:session:list:")
	if got, want := cache.zsetKey("user@example.com"), "agentapi:session:list:"+userPathID("user@example.com")+":z"; got != want {
		t.Fatalf("unexpected zset key %q", got)
	}
	session := &state.Session{
		ID:        "session-1",
		UserID:    "user@example.com",
		Title:     "Cached",
		Status:    state.SessionStatusActive,
		UpdatedAt: time.Unix(100, 0).UTC(),
		Messages:  []state.Message{{ID: "message-1", Content: "not for list cache"}},
	}
	id, score, raw, ok, err := redisSessionListCacheItem(session)
	if err != nil {
		t.Fatalf("cache item: %v", err)
	}
	if !ok || id != "session-1" || score != float64(session.UpdatedAt.UnixMilli()) {
		t.Fatalf("unexpected cache item id=%q score=%f ok=%t", id, score, ok)
	}
	var cached state.Session
	if err := json.Unmarshal([]byte(raw), &cached); err != nil {
		t.Fatalf("decode cache payload: %v", err)
	}
	if len(cached.Messages) != 0 {
		t.Fatalf("session list cache should not store transcript messages: %#v", cached.Messages)
	}
}

func TestRedisSessionListCacheRoundTrip(t *testing.T) {
	rawURL := os.Getenv("AGENT_RUNTIME_TEST_REDIS_URL")
	if rawURL == "" {
		t.Skip("set AGENT_RUNTIME_TEST_REDIS_URL to run redis integration test")
	}
	client, err := NewRedisClientFromURL(rawURL)
	if err != nil {
		t.Fatalf("new redis client: %v", err)
	}
	defer client.Close()
	cache := NewRedisSessionListCacheWithPrefix(client, time.Minute, RedisPrefixFromURL(rawURL))
	ctx := context.Background()
	userID := "session-list-cache-" + time.Now().UTC().Format("20060102T150405.000000000")
	defer func() { _ = cache.InvalidateUser(context.Background(), userID) }()
	sessions := []*state.Session{
		{ID: "old", UserID: userID, Status: state.SessionStatusActive, UpdatedAt: time.Unix(100, 0).UTC()},
		{ID: "new", UserID: userID, Status: state.SessionStatusActive, UpdatedAt: time.Unix(200, 0).UTC()},
	}
	if err := cache.SetSessions(ctx, userID, sessions); err != nil {
		t.Fatalf("set sessions: %v", err)
	}
	page, ok, err := cache.GetSessions(ctx, userID, 0, 1)
	if err != nil {
		t.Fatalf("get page: %v", err)
	}
	if !ok || len(page) != 1 || page[0].ID != "new" {
		t.Fatalf("unexpected page ok=%t sessions=%#v", ok, page)
	}
	sessions[0].UpdatedAt = time.Unix(300, 0).UTC()
	if err := cache.UpsertSession(ctx, userID, sessions[0]); err != nil {
		t.Fatalf("upsert session: %v", err)
	}
	page, ok, err = cache.GetSessions(ctx, userID, 0, 2)
	if err != nil {
		t.Fatalf("get after upsert: %v", err)
	}
	if !ok || len(page) != 2 || page[0].ID != "old" || page[1].ID != "new" {
		t.Fatalf("unexpected ordered sessions ok=%t sessions=%#v", ok, page)
	}
	if err := cache.RemoveSession(ctx, userID, "old"); err != nil {
		t.Fatalf("remove session: %v", err)
	}
	page, ok, err = cache.GetSessions(ctx, userID, 0, 10)
	if err != nil {
		t.Fatalf("get after remove: %v", err)
	}
	if !ok || len(page) != 1 || page[0].ID != "new" {
		t.Fatalf("unexpected sessions after remove ok=%t sessions=%#v", ok, page)
	}
}
