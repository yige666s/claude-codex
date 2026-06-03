package agentruntime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"claude-codex/internal/harness/state"
)

func TestMemoryVectorServiceIndexesAndRetrievesMemory(t *testing.T) {
	embeddingServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"embedding":{"values":[0.1,0.2,0.3]}}`))
	}))
	defer embeddingServer.Close()

	var upserts int
	var searches int
	var deletes int
	qdrantServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/collections/agent_memories":
			http.NotFound(w, r)
		case r.Method == http.MethodPut && r.URL.Path == "/collections/agent_memories":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"result":true,"status":"ok"}`))
		case r.Method == http.MethodPut && r.URL.Path == "/collections/agent_memories/points":
			upserts++
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode upsert: %v", err)
			}
			points := body["points"].([]any)
			payload := points[0].(map[string]any)["payload"].(map[string]any)
			if payload["user_id"] != "alice" || payload["status"] != MemoryStatusActive {
				t.Fatalf("unexpected memory vector payload: %#v", payload)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"result":{"operation_id":1,"status":"completed"},"status":"ok"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/collections/agent_memories/points/search":
			searches++
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"result":[{"score":0.93,"payload":{"memory_id":"memory-csv","user_id":"alice","status":"active"}}],"status":"ok"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/collections/agent_memories/points/delete":
			deletes++
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"result":{"operation_id":1,"status":"completed"},"status":"ok"}`))
		default:
			t.Fatalf("unexpected qdrant request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer qdrantServer.Close()

	ctx := context.Background()
	base := NewFileMemoryService(t.TempDir())
	memory := NewMemoryVectorService(base, MemoryVectorConfig{
		Enabled:                true,
		QdrantEndpoint:         qdrantServer.URL,
		QdrantCollection:       "agent_memories",
		EmbeddingProvider:      messageEmbeddingProviderVertex,
		EmbeddingEndpoint:      embeddingServer.URL,
		EmbeddingAccessToken:   "token",
		EmbeddingProjectID:     "project-1",
		EmbeddingLocation:      "global",
		EmbeddingModel:         "gemini-embedding-2",
		EmbeddingDimensions:    3,
		EmbeddingTaskType:      "RETRIEVAL_QUERY",
		EmbeddingIndexTaskType: "RETRIEVAL_DOCUMENT",
		EmbeddingAutoTruncate:  true,
		Timeout:                time.Second,
	}, nil)
	items := memory.(MemoryItemService)

	csv := newConversationMemoryItem("alice", "session-1", "User prefers CSV exports for project Orion")
	csv.ID = "memory-csv"
	if _, err := items.UpdateMemoryItem(ctx, "alice", csv); err != nil {
		t.Fatalf("update csv memory: %v", err)
	}
	other := newConversationMemoryItem("alice", "session-1", "User likes short responses")
	other.ID = "memory-other"
	if _, err := items.UpdateMemoryItem(ctx, "alice", other); err != nil {
		t.Fatalf("update other memory: %v", err)
	}
	if upserts != 2 {
		t.Fatalf("expected two memory vector upserts, got %d", upserts)
	}

	session := stateSessionWithUserMessage("session-1", "please export Orion data")
	contextText, err := memory.LoadContext(ctx, "alice", session)
	if err != nil {
		t.Fatalf("load memory context: %v", err)
	}
	if searches != 1 {
		t.Fatalf("expected one memory vector search, got %d", searches)
	}
	if !strings.Contains(contextText, "CSV exports") {
		t.Fatalf("expected vector-retrieved memory in context, got %q", contextText)
	}

	if err := items.DeleteMemoryItem(ctx, "alice", "memory-csv"); err != nil {
		t.Fatalf("delete memory: %v", err)
	}
	if deletes != 1 {
		t.Fatalf("expected memory vector delete, got %d", deletes)
	}
}

func TestMemoryVectorServiceCachesRetrievalAndInvalidatesOnUpdate(t *testing.T) {
	embeddingServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"embedding":{"values":[0.1,0.2,0.3]}}`))
	}))
	defer embeddingServer.Close()

	var searches int
	qdrantServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/collections/agent_memories":
			http.NotFound(w, r)
		case r.Method == http.MethodPut && r.URL.Path == "/collections/agent_memories":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"result":true,"status":"ok"}`))
		case r.Method == http.MethodPut && r.URL.Path == "/collections/agent_memories/points":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"result":{"operation_id":1,"status":"completed"},"status":"ok"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/collections/agent_memories/points/search":
			searches++
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"result":[{"score":0.93,"payload":{"memory_id":"memory-csv","user_id":"alice","status":"active"}}],"status":"ok"}`))
		default:
			t.Fatalf("unexpected qdrant request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer qdrantServer.Close()

	ctx := context.Background()
	base := NewFileMemoryService(t.TempDir())
	memory := NewMemoryVectorService(base, MemoryVectorConfig{
		Enabled:                true,
		QdrantEndpoint:         qdrantServer.URL,
		QdrantCollection:       "agent_memories",
		EmbeddingProvider:      messageEmbeddingProviderVertex,
		EmbeddingEndpoint:      embeddingServer.URL,
		EmbeddingAccessToken:   "token",
		EmbeddingProjectID:     "project-1",
		EmbeddingLocation:      "global",
		EmbeddingModel:         "gemini-embedding-2",
		EmbeddingDimensions:    3,
		EmbeddingTaskType:      "RETRIEVAL_QUERY",
		EmbeddingIndexTaskType: "RETRIEVAL_DOCUMENT",
		EmbeddingAutoTruncate:  true,
		Timeout:                time.Second,
		CacheStore:             NewMemoryCacheStore(time.Hour),
		CacheDefaultTTL:        time.Hour,
	}, nil)
	items := memory.(MemoryItemService)

	csv := newConversationMemoryItem("alice", "session-1", "User prefers CSV exports for project Orion")
	csv.ID = "memory-csv"
	if _, err := items.UpdateMemoryItem(ctx, "alice", csv); err != nil {
		t.Fatalf("update csv memory: %v", err)
	}

	session := stateSessionWithUserMessage("session-1", "please export Orion data")
	first, err := memory.LoadContext(ctx, "alice", session)
	if err != nil {
		t.Fatalf("load first memory context: %v", err)
	}
	second, err := memory.LoadContext(ctx, "alice", session)
	if err != nil {
		t.Fatalf("load second memory context: %v", err)
	}
	if searches != 1 {
		t.Fatalf("expected cached second retrieval to avoid qdrant search, got %d searches", searches)
	}
	if !strings.Contains(first, "CSV exports") || !strings.Contains(second, "CSV exports") {
		t.Fatalf("expected cached context to include CSV memory, first=%q second=%q", first, second)
	}
	current, err := items.GetMemoryItem(ctx, "alice", "memory-csv")
	if err != nil {
		t.Fatalf("get memory after cached load: %v", err)
	}
	if current.AccessCount < 2 {
		t.Fatalf("expected cached load to still record injections, access_count=%d", current.AccessCount)
	}

	current.Content = "User now prefers JSON exports for project Orion"
	if _, err := items.UpdateMemoryItem(ctx, "alice", current); err != nil {
		t.Fatalf("update changed memory: %v", err)
	}
	third, err := memory.LoadContext(ctx, "alice", session)
	if err != nil {
		t.Fatalf("load third memory context: %v", err)
	}
	if searches != 2 {
		t.Fatalf("expected memory update to invalidate retrieval cache, got %d searches", searches)
	}
	if !strings.Contains(third, "JSON exports") {
		t.Fatalf("expected fresh context after invalidation, got %q", third)
	}
}

func stateSessionWithUserMessage(sessionID, content string) *state.Session {
	session := state.NewSession("")
	session.ID = sessionID
	session.AddUserMessage(content)
	return session
}
