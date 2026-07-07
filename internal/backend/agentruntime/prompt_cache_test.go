package agentruntime

import (
	"context"
	"testing"
	"time"
)

func TestCachedPromptResolverInvalidatesOnPublish(t *testing.T) {
	ctx := context.Background()
	cache := NewMemoryCacheStore(time.Hour)
	store := NewCacheInvalidatingPromptStore(NewMemoryPromptStore(), cache)
	if _, err := store.UpsertPrompt(ctx, PromptTemplate{ID: "live_setup", Name: "Live setup"}); err != nil {
		t.Fatalf("upsert prompt: %v", err)
	}
	if _, err := store.CreatePromptVersion(ctx, PromptVersion{
		PromptID: "live_setup",
		Version:  "v1",
		Status:   PromptStatusPublished,
		Content:  "first",
	}); err != nil {
		t.Fatalf("create v1: %v", err)
	}

	resolver := NewCachedPromptResolver(store, nil, cache, time.Hour, false, nil)
	first, err := resolver.Resolve(ctx, PromptResolveRequest{PromptID: "live_setup", UserID: "alice", SessionID: "s1"})
	if err != nil {
		t.Fatalf("resolve first: %v", err)
	}
	if first.Version.Version != "v1" {
		t.Fatalf("first version = %s, want v1", first.Version.Version)
	}

	if _, err := store.CreatePromptVersion(ctx, PromptVersion{
		PromptID: "live_setup",
		Version:  "v2",
		Status:   PromptStatusDraft,
		Content:  "second",
	}); err != nil {
		t.Fatalf("create v2: %v", err)
	}
	if _, err := store.PublishPromptVersion(ctx, "live_setup", "v2", "admin", "ship v2"); err != nil {
		t.Fatalf("publish v2: %v", err)
	}

	second, err := resolver.Resolve(ctx, PromptResolveRequest{PromptID: "live_setup", UserID: "alice", SessionID: "s1"})
	if err != nil {
		t.Fatalf("resolve second: %v", err)
	}
	if second.Version.Version != "v2" {
		t.Fatalf("second version = %s, want v2", second.Version.Version)
	}
}

func TestCachedPromptResolverInvalidatesOnEnvironmentPin(t *testing.T) {
	ctx := context.Background()
	cache := NewMemoryCacheStore(time.Hour)
	store := NewCacheInvalidatingPromptStore(NewMemoryPromptStore(), cache)
	if _, err := store.UpsertPrompt(ctx, PromptTemplate{ID: "live_setup", Name: "Live setup"}); err != nil {
		t.Fatalf("upsert prompt: %v", err)
	}
	for _, version := range []PromptVersion{
		{PromptID: "live_setup", Version: "v1", Status: PromptStatusPublished, Content: "first"},
		{PromptID: "live_setup", Version: "v2", Status: PromptStatusReviewPending, Content: "second"},
	} {
		if _, err := store.CreatePromptVersion(ctx, version); err != nil {
			t.Fatalf("create %s: %v", version.Version, err)
		}
	}
	if _, err := store.SetPromptEnvironmentPin(ctx, PromptEnvironmentPin{PromptID: "live_setup", Environment: PromptEnvironmentProduction, Version: "v1", PinnedBy: "admin"}); err != nil {
		t.Fatalf("pin v1: %v", err)
	}
	resolver := NewCachedPromptResolver(store, nil, cache, time.Hour, false, nil)
	first, err := resolver.Resolve(ctx, PromptResolveRequest{PromptID: "live_setup", Environment: PromptEnvironmentProduction, UserID: "alice", SessionID: "s1"})
	if err != nil {
		t.Fatalf("resolve first: %v", err)
	}
	if first.Version.Version != "v1" {
		t.Fatalf("first version = %s, want v1", first.Version.Version)
	}
	if _, err := store.SetPromptEnvironmentPin(ctx, PromptEnvironmentPin{PromptID: "live_setup", Environment: PromptEnvironmentProduction, Version: "v2", PinnedBy: "admin"}); err != nil {
		t.Fatalf("pin v2: %v", err)
	}
	second, err := resolver.Resolve(ctx, PromptResolveRequest{PromptID: "live_setup", Environment: PromptEnvironmentProduction, UserID: "alice", SessionID: "s1"})
	if err != nil {
		t.Fatalf("resolve second: %v", err)
	}
	if second.Version.Version != "v2" {
		t.Fatalf("second version = %s, want v2", second.Version.Version)
	}
}
