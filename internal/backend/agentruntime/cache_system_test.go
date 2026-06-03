package agentruntime

import (
	"context"
	"testing"
	"time"
)

func TestTypedCacheMemoryRoundTripAndMetrics(t *testing.T) {
	store := NewMemoryCacheStore(time.Hour)
	metrics := NewCacheMetrics()
	cache := NewTypedCache[map[string]string](store, CachePolicy{Namespace: "prompt", TTL: time.Hour}, metrics)
	ctx := context.Background()
	key := BuildCacheKey(CacheKeyOptions{
		UserID:    "alice@example.com",
		SessionID: "session-1",
		Version:   "v1",
		Parts:     []string{"live_setup", "model=a"},
	})

	if _, ok, err := cache.Get(ctx, key); err != nil || ok {
		t.Fatalf("empty cache Get() ok=%t err=%v", ok, err)
	}
	if err := cache.Set(ctx, key, map[string]string{"content": "hello"}); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	got, ok, err := cache.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if !ok || got["content"] != "hello" {
		t.Fatalf("Get() = %#v ok=%t", got, ok)
	}
	snapshot := metrics.Snapshot()["prompt"]
	if snapshot.Misses != 1 || snapshot.Writes != 1 || snapshot.Hits != 1 {
		t.Fatalf("unexpected metrics: %#v", snapshot)
	}
}

func TestTypedCacheDeleteNamespace(t *testing.T) {
	store := NewMemoryCacheStore(time.Hour)
	cache := NewTypedCache[string](store, CachePolicy{Namespace: "memory", TTL: time.Hour}, nil)
	ctx := context.Background()

	if err := cache.Set(ctx, "a", "one"); err != nil {
		t.Fatalf("set a: %v", err)
	}
	if err := cache.Set(ctx, "b", "two"); err != nil {
		t.Fatalf("set b: %v", err)
	}
	if err := cache.DeleteNamespace(ctx); err != nil {
		t.Fatalf("delete namespace: %v", err)
	}
	if _, ok, err := cache.Get(ctx, "a"); err != nil || ok {
		t.Fatalf("expected namespace delete miss, ok=%t err=%v", ok, err)
	}
}

func TestMemoryCacheStoreExpires(t *testing.T) {
	store := NewMemoryCacheStore(time.Millisecond)
	ctx := context.Background()
	if err := store.Set(ctx, "short", []byte("value"), time.Millisecond); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	time.Sleep(5 * time.Millisecond)
	if _, ok, err := store.Get(ctx, "short"); err != nil || ok {
		t.Fatalf("expired Get() ok=%t err=%v", ok, err)
	}
}
