package agentruntime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"claude-codex/internal/harness/state"
)

type fakeMessageEmbeddingMetaStore struct {
	mu   sync.Mutex
	meta []MessageEmbeddingMeta
}

func (s *fakeMessageEmbeddingMetaStore) SaveMessageEmbeddingMeta(_ context.Context, meta MessageEmbeddingMeta) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.meta = append(s.meta, meta)
	return nil
}

func TestQdrantMessageVectorIndexerIndexesMessage(t *testing.T) {
	embeddingServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/projects/project-1/locations/global/publishers/google/models/gemini-embedding-2:embedContent") {
			t.Fatalf("unexpected embedding path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"embedding":{"values":[0.1,0.2,0.3]}}`))
	}))
	defer embeddingServer.Close()

	var createdCollection bool
	var gotCreate map[string]any
	var gotUpsert map[string]any
	qdrantServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/collections/agent_messages":
			http.NotFound(w, r)
		case r.Method == http.MethodPut && r.URL.Path == "/collections/agent_messages":
			createdCollection = true
			if err := json.NewDecoder(r.Body).Decode(&gotCreate); err != nil {
				t.Fatalf("decode create collection: %v", err)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"result":true,"status":"ok"}`))
		case r.Method == http.MethodPut && r.URL.Path == "/collections/agent_messages/points":
			if r.URL.Query().Get("wait") != "true" {
				t.Fatalf("expected wait=true, got %q", r.URL.RawQuery)
			}
			if err := json.NewDecoder(r.Body).Decode(&gotUpsert); err != nil {
				t.Fatalf("decode upsert: %v", err)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"result":{"operation_id":1,"status":"completed"},"status":"ok"}`))
		default:
			t.Fatalf("unexpected qdrant request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer qdrantServer.Close()

	metaStore := &fakeMessageEmbeddingMetaStore{}
	indexer := NewQdrantMessageVectorIndexer(MessageSearchConfig{
		QdrantEndpoint:        qdrantServer.URL,
		QdrantCollection:      "agent_messages",
		EmbeddingProvider:     messageEmbeddingProviderVertex,
		EmbeddingEndpoint:     embeddingServer.URL,
		EmbeddingAccessToken:  "token",
		EmbeddingProjectID:    "project-1",
		EmbeddingLocation:     "global",
		EmbeddingModel:        "gemini-embedding-2",
		EmbeddingDimensions:   3,
		EmbeddingTaskType:     "RETRIEVAL_DOCUMENT",
		EmbeddingAutoTruncate: true,
	}, metaStore)
	message := state.Message{
		ID:          "message-1",
		UserID:      "user-1",
		SessionID:   "session-1",
		SeqNo:       7,
		Role:        state.MessageRoleUser,
		ContentType: state.MessageContentTypeText,
		Content:     "hello semantic index",
		Status:      state.MessageStatusNormal,
		CreatedAt:   time.Date(2026, 5, 15, 1, 2, 3, 0, time.UTC),
	}
	if err := indexer.IndexMessage(context.Background(), message); err != nil {
		t.Fatalf("IndexMessage() error = %v", err)
	}
	if !createdCollection {
		t.Fatalf("expected collection creation")
	}
	vectors, ok := gotCreate["vectors"].(map[string]any)
	if !ok || vectors["size"] != float64(3) || vectors["distance"] != "Cosine" {
		t.Fatalf("unexpected collection payload: %#v", gotCreate)
	}
	points, ok := gotUpsert["points"].([]any)
	if !ok || len(points) != 1 {
		t.Fatalf("unexpected upsert payload: %#v", gotUpsert)
	}
	point := points[0].(map[string]any)
	vector := point["vector"].([]any)
	if len(vector) != 3 {
		t.Fatalf("unexpected vector: %#v", vector)
	}
	payload := point["payload"].(map[string]any)
	if payload["message_id"] != "message-1" || payload["user_id"] != "user-1" || payload["content"] != "hello semantic index" {
		t.Fatalf("unexpected payload: %#v", payload)
	}
	if payload["message_index"] != float64(6) || payload["status"] != float64(state.MessageStatusNormal) {
		t.Fatalf("unexpected numeric payload: %#v", payload)
	}
	if len(metaStore.meta) != 1 {
		t.Fatalf("expected one embedding meta record, got %#v", metaStore.meta)
	}
	if metaStore.meta[0].MessageID != "message-1" || metaStore.meta[0].ModelVersion != "vertex:gemini-embedding-2:3" {
		t.Fatalf("unexpected embedding meta: %#v", metaStore.meta[0])
	}
}

func TestQdrantMessageVectorIndexerDeletesMessage(t *testing.T) {
	var deletes []map[string]any
	qdrantServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/collections/agent_messages/points/delete" {
			t.Fatalf("unexpected qdrant request: %s %s", r.Method, r.URL.String())
		}
		if r.URL.Query().Get("wait") != "true" {
			t.Fatalf("expected wait=true, got %q", r.URL.RawQuery)
		}
		gotDelete := map[string]any{}
		if err := json.NewDecoder(r.Body).Decode(&gotDelete); err != nil {
			t.Fatalf("decode delete: %v", err)
		}
		deletes = append(deletes, gotDelete)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result":{"operation_id":1,"status":"completed"},"status":"ok"}`))
	}))
	defer qdrantServer.Close()

	indexer := NewQdrantMessageVectorIndexer(MessageSearchConfig{
		QdrantEndpoint:   qdrantServer.URL,
		QdrantCollection: "agent_messages",
	}, nil)
	message := state.Message{ID: "message-1", UserID: "user-1", SessionID: "session-1"}
	if err := indexer.DeleteMessage(context.Background(), message); err != nil {
		t.Fatalf("DeleteMessage() error = %v", err)
	}
	if len(deletes) != 2 {
		t.Fatalf("expected message point and attachment filter deletes, got %#v", deletes)
	}
	points, ok := deletes[0]["points"].([]any)
	if !ok || len(points) != 1 {
		t.Fatalf("unexpected delete payload: %#v", deletes[0])
	}
	want := messageVectorID("user-1", "message-1", 0)
	if points[0] != want {
		t.Fatalf("deleted point = %v, want %s", points[0], want)
	}
	if _, ok := deletes[1]["filter"].(map[string]any); !ok {
		t.Fatalf("expected attachment filter delete, got %#v", deletes[1])
	}
}

func TestQdrantMessageVectorIndexerIndexesAttachmentText(t *testing.T) {
	embeddingServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"embedding":{"values":[0.1,0.2,0.3]}}`))
	}))
	defer embeddingServer.Close()

	var gotUpsert map[string]any
	qdrantServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/collections/agent_messages":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"result":{"status":"green"},"status":"ok"}`))
		case r.Method == http.MethodPut && r.URL.Path == "/collections/agent_messages/points":
			if err := json.NewDecoder(r.Body).Decode(&gotUpsert); err != nil {
				t.Fatalf("decode upsert: %v", err)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"result":{"operation_id":1,"status":"completed"},"status":"ok"}`))
		default:
			t.Fatalf("unexpected qdrant request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer qdrantServer.Close()

	metaStore := &fakeMessageEmbeddingMetaStore{}
	indexer := NewQdrantMessageVectorIndexer(MessageSearchConfig{
		QdrantEndpoint:        qdrantServer.URL,
		QdrantCollection:      "agent_messages",
		EmbeddingProvider:     messageEmbeddingProviderVertex,
		EmbeddingEndpoint:     embeddingServer.URL,
		EmbeddingAccessToken:  "token",
		EmbeddingProjectID:    "project-1",
		EmbeddingLocation:     "global",
		EmbeddingModel:        "gemini-embedding-2",
		EmbeddingDimensions:   3,
		EmbeddingTaskType:     "RETRIEVAL_DOCUMENT",
		EmbeddingAutoTruncate: true,
	}, metaStore)
	attachment := state.MessageAttachment{
		ID:        "attachment-1",
		MessageID: "message-1",
		SessionID: "session-1",
		UserID:    "user-1",
		FileName:  "notes.txt",
		FileType:  messageAttachmentFileTypeText,
		MimeType:  "text/plain",
		CreatedAt: time.Date(2026, 5, 15, 1, 2, 3, 0, time.UTC),
	}
	if err := indexer.IndexAttachmentText(context.Background(), attachment, "attachment semantic text"); err != nil {
		t.Fatalf("IndexAttachmentText() error = %v", err)
	}
	points, ok := gotUpsert["points"].([]any)
	if !ok || len(points) != 1 {
		t.Fatalf("unexpected upsert payload: %#v", gotUpsert)
	}
	point := points[0].(map[string]any)
	payload := point["payload"].(map[string]any)
	if payload["source_type"] != messageIndexSourceAttachment || payload["attachment_id"] != "attachment-1" || payload["message_id"] != "message-1" {
		t.Fatalf("unexpected attachment payload: %#v", payload)
	}
	if !strings.Contains(payload["content"].(string), "attachment semantic text") {
		t.Fatalf("unexpected attachment content: %#v", payload)
	}
	if len(metaStore.meta) != 1 || metaStore.meta[0].MessageID != "message-1" || metaStore.meta[0].VectorID == "" {
		t.Fatalf("unexpected embedding meta: %#v", metaStore.meta)
	}
}

func TestQdrantMessageVectorIndexerDeletesAttachmentText(t *testing.T) {
	var gotDelete map[string]any
	qdrantServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/collections/agent_messages/points/delete" {
			t.Fatalf("unexpected qdrant request: %s %s", r.Method, r.URL.String())
		}
		if err := json.NewDecoder(r.Body).Decode(&gotDelete); err != nil {
			t.Fatalf("decode delete: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result":{"operation_id":1,"status":"completed"},"status":"ok"}`))
	}))
	defer qdrantServer.Close()

	indexer := NewQdrantMessageVectorIndexer(MessageSearchConfig{
		QdrantEndpoint:   qdrantServer.URL,
		QdrantCollection: "agent_messages",
	}, nil)
	attachment := state.MessageAttachment{ID: "attachment-1", UserID: "user-1", MessageID: "message-1", SessionID: "session-1"}
	if err := indexer.DeleteAttachmentText(context.Background(), attachment); err != nil {
		t.Fatalf("DeleteAttachmentText() error = %v", err)
	}
	filter, ok := gotDelete["filter"].(map[string]any)
	if !ok || filter["must"] == nil {
		t.Fatalf("unexpected delete payload: %#v", gotDelete)
	}
}

func TestMessageVectorIndexingEnabledRequiresSemanticBackend(t *testing.T) {
	config := MessageSearchConfig{
		Backend:             messageSearchBackendSQL,
		QdrantEndpoint:      "http://qdrant:6333",
		QdrantCollection:    "agent_messages",
		EmbeddingProvider:   messageEmbeddingProviderVertex,
		EmbeddingProjectID:  "project-1",
		EmbeddingLocation:   "global",
		EmbeddingModel:      "gemini-embedding-2",
		EmbeddingDimensions: 768,
	}
	if messageVectorIndexingEnabled(config) {
		t.Fatalf("did not expect vector indexing for sql backend")
	}
	config.Backend = messageSearchBackendHybrid
	if !messageVectorIndexingEnabled(config) {
		t.Fatalf("expected vector indexing for hybrid backend")
	}
}
