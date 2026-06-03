package agentruntime

import (
	"context"
	"testing"
	"time"
)

type countingEmbedder struct {
	count int
}

func (e *countingEmbedder) EmbedQuery(context.Context, string) ([]float32, error) {
	e.count++
	return []float32{float32(e.count), 0.2, 0.3}, nil
}

func TestCachedQueryEmbedderCachesByModelAndQuery(t *testing.T) {
	base := &countingEmbedder{}
	cache := NewMemoryCacheStore(time.Hour)
	embedder := NewCachedQueryEmbedder(base, MessageSearchConfig{
		EmbeddingProvider: "openai",
		EmbeddingModel:    "text-embedding",
		CacheStore:        cache,
		CacheDefaultTTL:   time.Hour,
	})

	first, err := embedder.EmbedQuery(context.Background(), "hello")
	if err != nil {
		t.Fatalf("first embed: %v", err)
	}
	second, err := embedder.EmbedQuery(context.Background(), "hello")
	if err != nil {
		t.Fatalf("second embed: %v", err)
	}
	if base.count != 1 {
		t.Fatalf("base embed count = %d, want 1", base.count)
	}
	if first[0] != second[0] {
		t.Fatalf("cached vector changed: first=%#v second=%#v", first, second)
	}

	if _, err := embedder.EmbedQuery(context.Background(), "different"); err != nil {
		t.Fatalf("different embed: %v", err)
	}
	if base.count != 2 {
		t.Fatalf("base embed count after different query = %d, want 2", base.count)
	}
}
