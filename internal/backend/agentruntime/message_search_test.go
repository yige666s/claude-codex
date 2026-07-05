package agentruntime

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"claude-codex/internal/harness/state"
	publictypes "claude-codex/internal/public/types"
)

type stubMessageSearchStore struct {
	results []MessageSearchResult
	err     error
	limit   int
	offset  int
	queries []string
}

func (s *stubMessageSearchStore) SearchMessages(ctx context.Context, userID, query string, limit, offset int) ([]MessageSearchResult, error) {
	s.limit = limit
	s.offset = offset
	s.queries = append(s.queries, query)
	if s.err != nil {
		return nil, s.err
	}
	return append([]MessageSearchResult(nil), s.results...), nil
}

type stubSemanticMessageSearcher struct {
	results []MessageSearchResult
	err     error
	limit   int
	queries []string
}

func (s *stubSemanticMessageSearcher) SearchSemanticMessages(ctx context.Context, userID, query string, limit int) ([]MessageSearchResult, error) {
	s.limit = limit
	s.queries = append(s.queries, query)
	if s.err != nil {
		return nil, s.err
	}
	return append([]MessageSearchResult(nil), s.results...), nil
}

func TestMessageSearchServiceFallbackUsesSQLStore(t *testing.T) {
	ctx := context.Background()
	fallback := &stubMessageSearchStore{results: []MessageSearchResult{{SessionID: "s1", MessageIndex: 1, Content: "hello postgres"}}}
	service := NewMessageSearchService(MessageSearchConfig{}, fallback)

	results, err := service.SearchMessages(ctx, "alice", "postgres", 10, 3)
	if err != nil {
		t.Fatalf("SearchMessages() error = %v", err)
	}
	if fallback.limit != 10 || fallback.offset != 3 {
		t.Fatalf("unexpected fallback page: limit=%d offset=%d", fallback.limit, fallback.offset)
	}
	if len(results) != 1 || results[0].Source != messageSearchBackendSQL {
		t.Fatalf("unexpected fallback results: %#v", results)
	}
}

func TestMessageSearchServiceElasticsearchDoesNotFallbackToSQL(t *testing.T) {
	fallback := &stubMessageSearchStore{results: []MessageSearchResult{{SessionID: "s1", MessageIndex: 1, Content: "sql result"}}}
	service := NewMessageSearchService(MessageSearchConfig{Backend: messageSearchBackendElasticsearch}, fallback)

	_, err := service.SearchMessages(context.Background(), "alice", "query", 10, 0)
	if err == nil {
		t.Fatal("expected full-text backend configuration error")
	}
	if fallback.limit != 0 || fallback.offset != 0 {
		t.Fatalf("elasticsearch search should not call SQL fallback, got limit=%d offset=%d", fallback.limit, fallback.offset)
	}
}

func TestHTTPMessageFullTextSearcherUsesKeywordCompatibleExactFilters(t *testing.T) {
	var requestBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		data, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal request body: %v", err)
		}
		requestBody = string(data)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"hits":{"hits":[{"_score":1,"_source":{"message_id":"m1","session_id":"s1","seq_no":1,"role":"user","content":"needle","created_at":"2026-07-05T00:00:00Z"}}]}}`))
	}))
	defer server.Close()

	searcher := NewHTTPMessageFullTextSearcher(MessageSearchConfig{
		Backend:  messageSearchBackendElasticsearch,
		Endpoint: server.URL,
		Index:    "agent_messages",
		Timeout:  time.Second,
	})
	results, err := searcher.SearchMessages(context.Background(), "alice", "needle", 10, 0)
	if err != nil {
		t.Fatalf("SearchMessages() error = %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected one result, got %#v", results)
	}
	for _, field := range []string{"user_id.keyword", "role.keyword"} {
		if !strings.Contains(requestBody, field) {
			t.Fatalf("request body missing %s: %s", field, requestBody)
		}
	}
}

func TestMessageSearchServiceHybridDoesNotFallbackToSQL(t *testing.T) {
	fallback := &stubMessageSearchStore{results: []MessageSearchResult{{SessionID: "s1", MessageIndex: 1, Content: "sql result"}}}
	service := &MessageSearchService{
		config:   normalizeMessageSearchConfig(MessageSearchConfig{Backend: messageSearchBackendHybrid}),
		fallback: fallback,
	}

	results, err := service.SearchMessages(context.Background(), "alice", "query", 10, 0)
	if err != nil {
		t.Fatalf("SearchMessages() error = %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected no results without configured ES/semantic backends, got %#v", results)
	}
	if fallback.limit != 0 || fallback.offset != 0 {
		t.Fatalf("hybrid search should not call SQL fallback, got limit=%d offset=%d", fallback.limit, fallback.offset)
	}
}

func TestMessageSearchServiceHybridMergesWithRRF(t *testing.T) {
	now := time.Now()
	keyword := &stubMessageSearchStore{results: []MessageSearchResult{
		{SessionID: "s1", MessageIndex: 0, Content: "keyword one", CreatedAt: now.Add(time.Minute), Source: "elasticsearch"},
		{SessionID: "s2", MessageIndex: 0, Content: "keyword two", CreatedAt: now, Source: "elasticsearch"},
	}}
	semantic := &stubSemanticMessageSearcher{results: []MessageSearchResult{
		{SessionID: "s2", MessageIndex: 0, Content: "semantic two", CreatedAt: now, Source: "qdrant"},
		{SessionID: "s3", MessageIndex: 0, Content: "semantic three", CreatedAt: now.Add(-time.Minute), Source: "qdrant"},
	}}
	service := &MessageSearchService{
		config:   normalizeMessageSearchConfig(MessageSearchConfig{Backend: messageSearchBackendHybrid, RRFK: 60}),
		fallback: keyword,
		keyword:  keyword,
		semantic: semantic,
	}

	results, err := service.SearchMessages(context.Background(), "alice", "query", 10, 0)
	if err != nil {
		t.Fatalf("SearchMessages() error = %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 merged results, got %d: %#v", len(results), results)
	}
	if results[0].SessionID != "s2" {
		t.Fatalf("expected repeated keyword+semantic hit to rank first, got %#v", results)
	}
	if results[0].Score <= results[1].Score {
		t.Fatalf("expected first result score to lead, got %#v", results)
	}
}

func TestMessageSearchServiceHybridPropagatesConfiguredBackendError(t *testing.T) {
	expected := errors.New("search unavailable")
	service := &MessageSearchService{
		config:  normalizeMessageSearchConfig(MessageSearchConfig{Backend: messageSearchBackendHybrid}),
		keyword: &stubMessageSearchStore{err: expected},
	}

	_, err := service.SearchMessages(context.Background(), "alice", "query", 10, 0)
	if !errors.Is(err, expected) {
		t.Fatalf("expected backend error, got %v", err)
	}
}

func TestMessageSearchServiceHybridUsesDynamicRecallWindow(t *testing.T) {
	keyword := &stubMessageSearchStore{}
	semantic := &stubSemanticMessageSearcher{}
	service := &MessageSearchService{
		config: normalizeMessageSearchConfig(MessageSearchConfig{
			Backend:            messageSearchBackendHybrid,
			DynamicTopKEnabled: true,
			MinRecallWindow:    25,
			MaxRecallWindow:    80,
		}),
		keyword:  keyword,
		semantic: semantic,
	}

	_, err := service.SearchMessages(context.Background(), "alice", "上次 那个 登录 bug", 5, 0)
	if err != nil {
		t.Fatalf("SearchMessages() error = %v", err)
	}
	if keyword.limit != 65 || semantic.limit != 65 {
		t.Fatalf("expected dynamic recall window 65, got keyword=%d semantic=%d", keyword.limit, semantic.limit)
	}
}

func TestMessageSearchServiceHybridExpandsWithRewrittenQuery(t *testing.T) {
	keyword := &stubMessageSearchStore{results: []MessageSearchResult{
		{SessionID: "s1", MessageIndex: 0, Content: "weak initial hit", Source: "elasticsearch", Score: 0.01},
	}}
	semantic := &stubSemanticMessageSearcher{}
	service := &MessageSearchService{
		config: normalizeMessageSearchConfig(MessageSearchConfig{
			Backend:              messageSearchBackendHybrid,
			QueryRewriteEnabled:  true,
			MultiTurnEnabled:     true,
			LowConfidenceScore:   0.50,
			RerankEnabled:        false,
			RerankCandidateLimit: 10,
			DynamicTopKEnabled:   false,
			MinRecallWindow:      20,
			MaxRecallWindow:      80,
			RRFK:                 60,
		}),
		keyword:  keyword,
		semantic: semantic,
	}

	_, err := service.SearchMessages(context.Background(), "alice", "请问 帮我 查一下 postgres timeout", 10, 0)
	if err != nil {
		t.Fatalf("SearchMessages() error = %v", err)
	}
	if len(keyword.queries) < 2 {
		t.Fatalf("expected rewritten retrieval pass, got queries=%#v", keyword.queries)
	}
	if keyword.queries[1] != "postgres timeout" {
		t.Fatalf("expected rewritten query, got %#v", keyword.queries)
	}
}

func TestMessageSearchServiceHybridReranksRelevantCandidate(t *testing.T) {
	now := time.Now()
	keyword := &stubMessageSearchStore{results: []MessageSearchResult{
		{SessionID: "s1", MessageIndex: 0, Content: "general discussion", CreatedAt: now, Source: "elasticsearch", Score: 10},
		{SessionID: "s2", MessageIndex: 0, Content: "postgres timeout root cause and fix", CreatedAt: now, Source: "elasticsearch", Score: 1},
	}}
	service := &MessageSearchService{
		config: normalizeMessageSearchConfig(MessageSearchConfig{
			Backend:              messageSearchBackendHybrid,
			RerankEnabled:        true,
			RerankCandidateLimit: 10,
		}),
		keyword: keyword,
	}

	results, err := service.SearchMessages(context.Background(), "alice", "postgres timeout", 10, 0)
	if err != nil {
		t.Fatalf("SearchMessages() error = %v", err)
	}
	if len(results) < 2 || results[0].SessionID != "s2" {
		t.Fatalf("expected relevant candidate to rerank first, got %#v", results)
	}
}

func TestMessageSearchServiceHybridRunsRAGWorkflow(t *testing.T) {
	now := time.Now()
	keyword := &stubMessageSearchStore{results: []MessageSearchResult{
		{SessionID: "s1", MessageIndex: 0, Content: "postgres timeout notes", CreatedAt: now, Source: "elasticsearch"},
	}}
	semantic := &stubSemanticMessageSearcher{results: []MessageSearchResult{
		{SessionID: "s2", MessageIndex: 0, Content: "database timeout root cause", CreatedAt: now, Source: "qdrant"},
	}}
	service := &MessageSearchService{
		config: normalizeMessageSearchConfig(MessageSearchConfig{
			Backend:              messageSearchBackendHybrid,
			QueryRewriteEnabled:  true,
			DynamicTopKEnabled:   true,
			RerankEnabled:        true,
			RerankCandidateLimit: 10,
		}),
		keyword:  keyword,
		semantic: semantic,
	}
	service.workflow = newMessageSearchWorkflowEngine(service)

	results, err := service.SearchMessages(context.Background(), "alice", "postgres timeout", 10, 0)
	if err != nil {
		t.Fatalf("SearchMessages() error = %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected workflow search results, got %#v", results)
	}
	store := service.workflow.Store().(*MemoryWorkflowStore)
	store.mu.Lock()
	if len(store.runs) != 1 {
		store.mu.Unlock()
		t.Fatalf("expected one workflow run, got %d", len(store.runs))
	}
	var run *WorkflowRun
	for _, item := range store.runs {
		run = cloneWorkflowRun(item)
		break
	}
	store.mu.Unlock()
	if run.Name != ragSearchWorkflowName || run.Status != WorkflowStatusSucceeded || run.State["result_count"].(float64) != 2 {
		t.Fatalf("unexpected workflow run: %#v", run)
	}
	steps, err := service.workflow.Store().ListWorkflowStepRuns(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("ListWorkflowStepRuns() error = %v", err)
	}
	expectedSteps := []string{"normalize_query", "query_rewrite", "hybrid_retrieve", "rerank", "select_results"}
	if len(steps) != len(expectedSteps) {
		t.Fatalf("expected %d workflow steps, got %#v", len(expectedSteps), steps)
	}
	for i, step := range steps {
		if step.StepName != expectedSteps[i] || step.Status != WorkflowStepStatusSucceeded {
			t.Fatalf("unexpected step %d: %#v", i, step)
		}
	}
}

func TestMessageSearchServiceBuildsVertexSemanticSearcher(t *testing.T) {
	service := NewMessageSearchService(MessageSearchConfig{
		Backend:              messageSearchBackendSemantic,
		QdrantEndpoint:       "http://qdrant:6333",
		EmbeddingProvider:    messageEmbeddingProviderVertex,
		EmbeddingProjectID:   "project-1",
		EmbeddingLocation:    "global",
		EmbeddingModel:       "gemini-embedding-2",
		EmbeddingAccessToken: "token",
	}, &stubMessageSearchStore{})
	if service.semantic == nil {
		t.Fatalf("expected vertex-backed semantic searcher to be configured")
	}
}

func TestHTTPMessageFullTextIndexerWritesAndDeletesDocuments(t *testing.T) {
	var requests []struct {
		Method string
		Path   string
		Body   map[string]any
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		item := struct {
			Method string
			Path   string
			Body   map[string]any
		}{Method: r.Method, Path: r.URL.Path}
		if r.Method == http.MethodPut {
			if err := json.NewDecoder(r.Body).Decode(&item.Body); err != nil {
				t.Fatalf("decode index body: %v", err)
			}
			w.WriteHeader(http.StatusCreated)
		} else if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNotFound)
		} else {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
		requests = append(requests, item)
	}))
	defer server.Close()

	indexer := NewHTTPMessageFullTextIndexer(MessageSearchConfig{
		Backend:  messageSearchBackendElasticsearch,
		Endpoint: server.URL,
		Index:    "agent_messages",
		Timeout:  time.Second,
	})
	message := state.Message{
		ID:            "message-1",
		UserID:        "alice",
		SessionID:     "session-1",
		SeqNo:         3,
		Role:          state.MessageRoleAssistant,
		Content:       "中文订单查询",
		ContentParts:  []publictypes.ContentBlock{{Type: "text", Text: "补充文本"}},
		Status:        state.MessageStatusNormal,
		IsContextUsed: true,
		CreatedAt:     time.Date(2026, 5, 15, 1, 2, 3, 0, time.UTC),
	}
	if err := indexer.IndexMessage(context.Background(), message); err != nil {
		t.Fatalf("index message: %v", err)
	}
	message.Hidden = true
	if err := indexer.IndexMessage(context.Background(), message); err != nil {
		t.Fatalf("delete hidden message: %v", err)
	}
	if len(requests) != 2 {
		t.Fatalf("expected put and delete requests, got %#v", requests)
	}
	if requests[0].Method != http.MethodPut || requests[0].Path != "/agent_messages/_doc/message-1" {
		t.Fatalf("unexpected index request: %#v", requests[0])
	}
	if requests[0].Body["message_id"] != "message-1" || requests[0].Body["content"] != "中文订单查询" || requests[0].Body["message_index"].(float64) != 2 {
		t.Fatalf("unexpected index payload: %#v", requests[0].Body)
	}
	parts, ok := requests[0].Body["content_parts"].([]any)
	if !ok || len(parts) != 1 {
		t.Fatalf("unexpected content parts: %#v", requests[0].Body["content_parts"])
	}
	if requests[1].Method != http.MethodDelete || requests[1].Path != "/agent_messages/_doc/message-1" {
		t.Fatalf("unexpected delete request: %#v", requests[1])
	}
}

func TestHTTPMessageFullTextIndexerWritesAndDeletesAttachmentDocuments(t *testing.T) {
	var requests []struct {
		Method string
		Path   string
		Body   map[string]any
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		item := struct {
			Method string
			Path   string
			Body   map[string]any
		}{Method: r.Method, Path: r.URL.Path}
		if err := json.NewDecoder(r.Body).Decode(&item.Body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		requests = append(requests, item)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	indexer := NewHTTPMessageFullTextIndexer(MessageSearchConfig{
		Backend:  messageSearchBackendElasticsearch,
		Endpoint: server.URL,
		Index:    "agent_messages",
		Timeout:  time.Second,
	})
	attachment := state.MessageAttachment{
		ID:        "attachment-1",
		MessageID: "message-1",
		SessionID: "session-1",
		UserID:    "alice",
		FileName:  "notes.txt",
		FileType:  messageAttachmentFileTypeText,
		MimeType:  "text/plain",
		CreatedAt: time.Date(2026, 5, 15, 1, 2, 3, 0, time.UTC),
	}
	if err := indexer.IndexAttachmentText(context.Background(), attachment, "附件独立索引文本"); err != nil {
		t.Fatalf("index attachment text: %v", err)
	}
	if err := indexer.DeleteAttachmentText(context.Background(), attachment); err != nil {
		t.Fatalf("delete attachment text: %v", err)
	}
	if len(requests) != 2 {
		t.Fatalf("expected index and delete-by-query requests, got %#v", requests)
	}
	if requests[0].Method != http.MethodPut || requests[0].Path != "/agent_messages/_doc/attachment:alice:message-1:attachment-1:0" {
		t.Fatalf("unexpected attachment index request: %#v", requests[0])
	}
	if requests[0].Body["source_type"] != messageIndexSourceAttachment || requests[0].Body["attachment_id"] != "attachment-1" || !strings.Contains(requests[0].Body["content"].(string), "附件独立索引文本") {
		t.Fatalf("unexpected attachment index payload: %#v", requests[0].Body)
	}
	if requests[1].Method != http.MethodPost || requests[1].Path != "/agent_messages/_delete_by_query" {
		t.Fatalf("unexpected attachment delete request: %#v", requests[1])
	}
	query := requests[1].Body["query"].(map[string]any)
	if query["bool"] == nil {
		t.Fatalf("expected delete query body, got %#v", requests[1].Body)
	}
}

func TestVertexAIEmbeddingServiceEmbedQuery(t *testing.T) {
	var gotPath string
	var gotAuth string
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"embedding":{"values":[0.1,0.2,0.3]},"truncated":false}`))
	}))
	defer server.Close()

	service := NewVertexAIEmbeddingService(MessageSearchConfig{
		EmbeddingProvider:     messageEmbeddingProviderVertex,
		EmbeddingEndpoint:     server.URL,
		EmbeddingProjectID:    "project-1",
		EmbeddingLocation:     "global",
		EmbeddingModel:        "gemini-embedding-2",
		EmbeddingAccessToken:  "vertex-token",
		EmbeddingDimensions:   1536,
		EmbeddingTaskType:     "RETRIEVAL_QUERY",
		EmbeddingAutoTruncate: true,
	})
	vector, err := service.EmbedQuery(context.Background(), "search text")
	if err != nil {
		t.Fatalf("EmbedQuery() error = %v", err)
	}
	if len(vector) != 3 || vector[0] != float32(0.1) || vector[2] != float32(0.3) {
		t.Fatalf("unexpected vector: %#v", vector)
	}
	if gotAuth != "Bearer vertex-token" {
		t.Fatalf("unexpected authorization header: %q", gotAuth)
	}
	if !strings.HasSuffix(gotPath, "/projects/project-1/locations/global/publishers/google/models/gemini-embedding-2:embedContent") {
		t.Fatalf("unexpected vertex embedContent path: %s", gotPath)
	}
	content, ok := gotBody["content"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected content: %#v", gotBody["content"])
	}
	parts, ok := content["parts"].([]any)
	if !ok || len(parts) != 1 {
		t.Fatalf("unexpected parts: %#v", content["parts"])
	}
	part, ok := parts[0].(map[string]any)
	if !ok || part["text"] != "search text" {
		t.Fatalf("unexpected part payload: %#v", parts[0])
	}
	if gotBody["taskType"] != "RETRIEVAL_QUERY" || gotBody["outputDimensionality"] != float64(1536) || gotBody["autoTruncate"] != true {
		t.Fatalf("unexpected embedContent body: %#v", gotBody)
	}
}

func TestNVIDIAEmbeddingServiceEmbedQueryUsesNIMFields(t *testing.T) {
	var gotAuth string
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/embeddings" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[{"object":"embedding","index":0,"embedding":[0.1,0.2,0.3]}],"model":"nvidia/test","usage":{"prompt_tokens":2,"total_tokens":2}}`))
	}))
	defer server.Close()

	service := NewOpenAIEmbeddingService(MessageSearchConfig{
		EmbeddingProvider:     messageEmbeddingProviderNVIDIA,
		EmbeddingEndpoint:     server.URL,
		EmbeddingAPIKey:       "nvidia-token",
		EmbeddingModel:        "nvidia/test",
		EmbeddingDimensions:   1024,
		EmbeddingTaskType:     "RETRIEVAL_QUERY",
		EmbeddingAutoTruncate: true,
	})
	vector, err := service.EmbedQuery(context.Background(), "classify this")
	if err != nil {
		t.Fatalf("EmbedQuery() error = %v", err)
	}
	if len(vector) != 3 || vector[0] != float32(0.1) || vector[2] != float32(0.3) {
		t.Fatalf("unexpected vector: %#v", vector)
	}
	if gotAuth != "Bearer nvidia-token" {
		t.Fatalf("unexpected authorization header: %q", gotAuth)
	}
	if gotBody["input"] != "classify this" || gotBody["model"] != "nvidia/test" {
		t.Fatalf("unexpected core payload: %#v", gotBody)
	}
	if gotBody["input_type"] != "query" || gotBody["modality"] != "text" || gotBody["embedding_type"] != "float" || gotBody["encoding_format"] != "float" || gotBody["truncate"] != "END" {
		t.Fatalf("unexpected NVIDIA NIM payload: %#v", gotBody)
	}
	if gotBody["dimensions"] != float64(1024) {
		t.Fatalf("unexpected dimensions: %#v", gotBody["dimensions"])
	}
}
