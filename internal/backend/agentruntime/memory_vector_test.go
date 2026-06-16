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

func TestMemoryVectorServiceIndexesAndSearchesEpisodesSemantically(t *testing.T) {
	embeddingServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"embedding":{"values":[0.4,0.5,0.6]}}`))
	}))
	defer embeddingServer.Close()

	var episodeUpserts int
	var episodeSearches int
	qdrantServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/collections/agent_memory_episodes":
			http.NotFound(w, r)
		case r.Method == http.MethodPut && r.URL.Path == "/collections/agent_memory_episodes":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"result":true,"status":"ok"}`))
		case r.Method == http.MethodPut && r.URL.Path == "/collections/agent_memory_episodes/points":
			episodeUpserts++
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode episode upsert: %v", err)
			}
			points := body["points"].([]any)
			payload := points[0].(map[string]any)["payload"].(map[string]any)
			if payload["episode_id"] != "ep_cold_lake" || payload["user_id"] != "alice" || payload["status"] != MemoryEpisodeStatusActive {
				t.Fatalf("unexpected episode vector payload: %#v", payload)
			}
			if !strings.Contains(payload["content"].(string), "青海冷湖火星营地") {
				t.Fatalf("expected episode content in vector payload, got %#v", payload["content"])
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"result":{"operation_id":1,"status":"completed"},"status":"ok"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/collections/agent_memory_episodes/points/search":
			episodeSearches++
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"result":[{"score":0.91,"payload":{"episode_id":"ep_cold_lake","user_id":"alice","status":"active"}}],"status":"ok"}`))
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
		EpisodeCollection:      "agent_memory_episodes",
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
	episodes := memory.(MemoryEpisodeService)
	_, err := episodes.UpsertMemoryEpisode(ctx, "alice", MemoryEpisode{
		ID:         "ep_cold_lake",
		UserID:     "alice",
		SessionID:  "session-photo",
		Title:      "冷湖火星营地打卡推荐",
		Summary:    "用户讨论青海冷湖火星营地、雅丹地貌和冷色荒原航拍取景。",
		L0Abstract: "青海冷湖火星营地适合硬核科幻感打卡取景。",
		KeyTopics:  []string{"冷湖", "火星营地", "打卡"},
		SourceType: MemoryEpisodeSourceSession,
		SourceID:   "session:session-photo",
		Status:     MemoryEpisodeStatusActive,
		Visibility: MemoryVisibilityUser,
		Confidence: 0.9,
		Weight:     0.85,
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("upsert episode: %v", err)
	}
	if episodeUpserts != 1 {
		t.Fatalf("expected one episode vector upsert, got %d", episodeUpserts)
	}

	results, err := episodes.SearchMemoryEpisodes(ctx, "alice", "星球感拍摄", MemoryEpisodeSearchOptions{Limit: 3})
	if err != nil {
		t.Fatalf("search episodes: %v", err)
	}
	if episodeSearches != 1 {
		t.Fatalf("expected one episode vector search, got %d", episodeSearches)
	}
	if len(results) == 0 || results[0].Episode.ID != "ep_cold_lake" || results[0].Score <= 0 {
		t.Fatalf("expected semantic episode recall, got %#v", results)
	}
}

func TestMemoryVectorServiceReranksEpisodeVectorCandidates(t *testing.T) {
	embeddingServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"embedding":{"values":[0.4,0.5,0.6]}}`))
	}))
	defer embeddingServer.Close()

	var rerankCalled bool
	rerankServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/ranking" {
			t.Fatalf("unexpected rerank request: %s %s", r.Method, r.URL.String())
		}
		rerankCalled = true
		if got := r.Header.Get("Authorization"); got != "Bearer rerank-token" {
			t.Fatalf("unexpected rerank auth header: %q", got)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode rerank body: %v", err)
		}
		if body["model"] != "nvidia/rerank-test" {
			t.Fatalf("unexpected rerank model: %#v", body["model"])
		}
		passages := body["passages"].([]any)
		if len(passages) != 6 {
			t.Fatalf("expected 6 rerank passages, got %d", len(passages))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"rankings":[{"index":4,"logit":9},{"index":2,"logit":8},{"index":5,"logit":7},{"index":0,"logit":6},{"index":1,"logit":5},{"index":3,"logit":4}],"usage":{"prompt_tokens":10,"total_tokens":10}}`))
	}))
	defer rerankServer.Close()

	var episodeSearchLimit int
	qdrantServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/collections/agent_memory_episodes":
			http.NotFound(w, r)
		case r.Method == http.MethodPut && r.URL.Path == "/collections/agent_memory_episodes":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"result":true,"status":"ok"}`))
		case r.Method == http.MethodPut && r.URL.Path == "/collections/agent_memory_episodes/points":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"result":{"operation_id":1,"status":"completed"},"status":"ok"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/collections/agent_memory_episodes/points/search":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode qdrant search body: %v", err)
			}
			episodeSearchLimit = int(body["limit"].(float64))
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"result":[
				{"score":0.91,"payload":{"episode_id":"ep-0","user_id":"alice","status":"active"}},
				{"score":0.90,"payload":{"episode_id":"ep-1","user_id":"alice","status":"active"}},
				{"score":0.89,"payload":{"episode_id":"ep-2","user_id":"alice","status":"active"}},
				{"score":0.88,"payload":{"episode_id":"ep-3","user_id":"alice","status":"active"}},
				{"score":0.87,"payload":{"episode_id":"ep-4","user_id":"alice","status":"active"}},
				{"score":0.86,"payload":{"episode_id":"ep-5","user_id":"alice","status":"active"}}
			],"status":"ok"}`))
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
		EpisodeCollection:      "agent_memory_episodes",
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
		RerankEnabled:          true,
		RerankEndpoint:         rerankServer.URL,
		RerankAPIKey:           "rerank-token",
		RerankModel:            "nvidia/rerank-test",
		RerankCandidateLimit:   50,
		RerankResultLimit:      5,
		Timeout:                time.Second,
	}, nil)
	episodes := memory.(MemoryEpisodeService)
	for i := 0; i < 6; i++ {
		id := "ep-" + string(rune('0'+i))
		_, err := episodes.UpsertMemoryEpisode(ctx, "alice", MemoryEpisode{
			ID:         id,
			UserID:     "alice",
			SessionID:  "session-photo",
			Title:      "拍摄地点 " + id,
			Summary:    "用户讨论适合科幻感打卡的地点 " + id,
			L0Abstract: "地点 " + id + " 适合星球感拍摄。",
			KeyTopics:  []string{"拍摄", "打卡", id},
			SourceType: MemoryEpisodeSourceSession,
			SourceID:   "session:session-photo",
			Status:     MemoryEpisodeStatusActive,
			Visibility: MemoryVisibilityUser,
			Confidence: 0.9,
			Weight:     0.85,
			CreatedAt:  time.Now().UTC(),
			UpdatedAt:  time.Now().UTC().Add(time.Duration(i) * time.Second),
		})
		if err != nil {
			t.Fatalf("upsert episode %s: %v", id, err)
		}
	}

	results, err := episodes.SearchMemoryEpisodes(ctx, "alice", "星球感拍摄", MemoryEpisodeSearchOptions{})
	if err != nil {
		t.Fatalf("search episodes: %v", err)
	}
	if episodeSearchLimit != 50 {
		t.Fatalf("expected top50 vector candidate search, got limit=%d", episodeSearchLimit)
	}
	if !rerankCalled {
		t.Fatalf("expected rerank call")
	}
	if len(results) != 5 {
		t.Fatalf("expected top5 reranked episodes, got %d", len(results))
	}
	want := []string{"ep-4", "ep-2", "ep-5", "ep-0", "ep-1"}
	for idx, id := range want {
		if results[idx].Episode.ID != id {
			t.Fatalf("reranked result %d = %s, want %s; all=%#v", idx, results[idx].Episode.ID, id, results)
		}
	}
	if results[0].Score <= results[4].Score {
		t.Fatalf("expected normalized rerank scores to descend, got %#v", results)
	}
}

func TestMemoryEpisodeServiceFromKeepsVectorAndWorkflowWrappers(t *testing.T) {
	base := NewFileMemoryService(t.TempDir())
	vector := NewMemoryVectorService(base, MemoryVectorConfig{
		Enabled:             true,
		QdrantEndpoint:      "http://qdrant:6333",
		QdrantCollection:    "agent_memories",
		EpisodeCollection:   "agent_memory_episodes",
		EmbeddingProvider:   messageEmbeddingProviderNVIDIA,
		EmbeddingEndpoint:   "https://integrate.api.nvidia.com/v1",
		EmbeddingAPIKey:     "token",
		EmbeddingModel:      "nvidia/llama-nemotron-embed-1b-v2",
		EmbeddingDimensions: 768,
	}, nil)
	if _, ok := vector.(*MemoryVectorService); !ok {
		t.Fatalf("expected memory vector wrapper, got %T", vector)
	}
	gotVectorEpisodeService := memoryEpisodeServiceFrom(vector)
	if _, ok := gotVectorEpisodeService.(*MemoryVectorService); !ok {
		t.Fatalf("expected episode service to keep memory vector wrapper, got %T", gotVectorEpisodeService)
	}
	workflow := NewMemoryWorkflowService(vector, NewMemoryWorkflowStore(), ContextWorkflowEventSink{})
	if _, ok := workflow.(*MemoryWorkflowService); !ok {
		t.Fatalf("expected memory workflow wrapper, got %T", workflow)
	}
	gotWorkflowEpisodeService := memoryEpisodeServiceFrom(workflow)
	if _, ok := gotWorkflowEpisodeService.(*MemoryWorkflowService); !ok {
		t.Fatalf("expected episode service to keep workflow wrapper, got %T", gotWorkflowEpisodeService)
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
