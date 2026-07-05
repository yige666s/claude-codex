package agentruntime

import (
	"context"
	"testing"
	"time"
)

type countingMessageSearchStore struct {
	count int
}

func (s *countingMessageSearchStore) SearchMessages(_ context.Context, userID, query string, limit, offset int) ([]MessageSearchResult, error) {
	s.count++
	return []MessageSearchResult{{
		MessageID: "m-" + query,
		SessionID: "s1",
		Role:      "user",
		Snippet:   query,
		Score:     float64(s.count),
		Source:    "sql",
	}}, nil
}

func TestMessageSearchServiceCachesAndInvalidatesUserResults(t *testing.T) {
	store := &countingMessageSearchStore{}
	service := NewMessageSearchService(MessageSearchConfig{
		Backend:         messageSearchBackendSQL,
		CacheStore:      NewMemoryCacheStore(time.Hour),
		CacheDefaultTTL: time.Hour,
	}, store)
	ctx := context.Background()

	first, err := service.SearchMessages(ctx, "alice", "cache me", 10, 0)
	if err != nil {
		t.Fatalf("first search: %v", err)
	}
	second, err := service.SearchMessages(ctx, "alice", "cache me", 10, 0)
	if err != nil {
		t.Fatalf("second search: %v", err)
	}
	if store.count != 1 {
		t.Fatalf("search store count = %d, want 1", store.count)
	}
	if first[0].Score != second[0].Score {
		t.Fatalf("expected cached score, first=%#v second=%#v", first, second)
	}

	if err := service.InvalidateUserCache(ctx, "alice"); err != nil {
		t.Fatalf("invalidate user: %v", err)
	}
	third, err := service.SearchMessages(ctx, "alice", "cache me", 10, 0)
	if err != nil {
		t.Fatalf("third search: %v", err)
	}
	if store.count != 2 {
		t.Fatalf("search store count after invalidate = %d, want 2", store.count)
	}
	if third[0].Score == second[0].Score {
		t.Fatalf("expected fresh result after invalidate, second=%#v third=%#v", second, third)
	}
}

type emptyThenResultMessageSearchStore struct {
	count int
}

func (s *emptyThenResultMessageSearchStore) SearchMessages(_ context.Context, userID, query string, limit, offset int) ([]MessageSearchResult, error) {
	s.count++
	if s.count == 1 {
		return []MessageSearchResult{}, nil
	}
	return []MessageSearchResult{{
		MessageID: "m-" + query,
		SessionID: "s1",
		Role:      "user",
		Snippet:   query,
		Score:     float64(s.count),
		Source:    "elasticsearch",
	}}, nil
}

func TestMessageSearchServiceDoesNotCacheEmptyResults(t *testing.T) {
	store := &emptyThenResultMessageSearchStore{}
	service := NewMessageSearchService(MessageSearchConfig{
		Backend:         messageSearchBackendSQL,
		CacheStore:      NewMemoryCacheStore(time.Hour),
		CacheDefaultTTL: time.Hour,
	}, store)
	ctx := context.Background()

	first, err := service.SearchMessages(ctx, "alice", "eventual index", 10, 0)
	if err != nil {
		t.Fatalf("first search: %v", err)
	}
	if len(first) != 0 {
		t.Fatalf("first search len = %d, want 0", len(first))
	}

	second, err := service.SearchMessages(ctx, "alice", "eventual index", 10, 0)
	if err != nil {
		t.Fatalf("second search: %v", err)
	}
	if len(second) != 1 {
		t.Fatalf("second search len = %d, want 1", len(second))
	}
	if store.count != 2 {
		t.Fatalf("search store count = %d, want 2", store.count)
	}
}
