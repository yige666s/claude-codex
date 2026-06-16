package agentruntime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNVIDIARerankerUsesRankingEndpoint(t *testing.T) {
	var gotPath string
	var gotAuth string
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode rerank body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"rankings":[{"index":1,"logit":3.5},{"index":0,"logit":1.25}],"usage":{"prompt_tokens":8,"total_tokens":8}}`))
	}))
	defer server.Close()

	reranker := NewNVIDIAReranker(MemoryVectorConfig{
		Enabled:           true,
		RerankEnabled:     true,
		RerankEndpoint:    server.URL + "/v1",
		RerankAPIKey:      "token",
		RerankModel:       "nvidia/llama-nemotron-rerank-1b-v2",
		RerankTimeout:     time.Second,
		RerankTruncate:    "END",
		QdrantEndpoint:    "http://qdrant:6333",
		QdrantCollection:  "agent_memories",
		EmbeddingProvider: messageEmbeddingProviderNVIDIA,
		EmbeddingEndpoint: "https://integrate.api.nvidia.com/v1",
		EmbeddingAPIKey:   "token",
		EmbeddingModel:    "nvidia/llama-nemotron-embed-1b-v2",
	})
	if reranker == nil {
		t.Fatalf("expected reranker")
	}
	results, err := reranker.Rerank(context.Background(), "query text", []RerankPassage{
		{ID: "a", Text: "first passage"},
		{ID: "b", Text: "second passage"},
	})
	if err != nil {
		t.Fatalf("rerank: %v", err)
	}
	if gotPath != "/v1/ranking" {
		t.Fatalf("unexpected path: %s", gotPath)
	}
	if gotAuth != "Bearer token" {
		t.Fatalf("unexpected auth: %q", gotAuth)
	}
	if gotBody["model"] != "nvidia/llama-nemotron-rerank-1b-v2" || gotBody["truncate"] != "END" {
		t.Fatalf("unexpected body: %#v", gotBody)
	}
	if len(results) != 2 || results[0].Index != 1 || results[0].Score != 3.5 {
		t.Fatalf("unexpected rerank results: %#v", results)
	}
}
