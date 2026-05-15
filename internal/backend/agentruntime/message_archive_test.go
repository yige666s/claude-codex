package agentruntime

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"claude-codex/internal/harness/state"
	publictypes "claude-codex/internal/public/types"
)

func TestMessageArchiveObjectStoreRoundTripsCompressedMessage(t *testing.T) {
	objects := NewFileObjectStore(t.TempDir())
	archive := NewMessageArchiveObjectStore(objects, "archive")
	message := state.Message{
		ID:          "msg-1",
		UserID:      "alice",
		SessionID:   "session-1",
		SeqNo:       7,
		Role:        state.MessageRoleUser,
		ContentType: state.MessageContentTypeText,
		Content:     "old but still restorable",
		CreatedAt:   time.Date(2026, 4, 10, 1, 2, 3, 0, time.UTC),
		UpdatedAt:   time.Date(2026, 4, 10, 1, 2, 3, 0, time.UTC),
	}
	record, err := archive.WriteMessage(context.Background(), message)
	if err != nil {
		t.Fatalf("write archive: %v", err)
	}
	if !strings.Contains(record.ArchiveURI, "year=2026/month=04") {
		t.Fatalf("unexpected archive key: %s", record.ArchiveURI)
	}
	restored, err := archive.ReadMessage(context.Background(), record.ArchiveURI, record.ArchiveChecksum)
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}
	if restored.ID != message.ID || restored.Content != message.Content {
		t.Fatalf("unexpected restored message: %#v", restored)
	}
	if restored.ArchiveURI != "" || restored.ArchiveChecksum != "" || restored.ArchivedAt != nil {
		t.Fatalf("archive metadata should not be nested in payload: %#v", restored)
	}
}

func TestSQLMessageArchiveWorkerArchivesAndHydratesFromObjectStore(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("AGENT_RUNTIME_TEST_PG_DSN"))
	if dsn == "" {
		t.Skip("set AGENT_RUNTIME_TEST_PG_DSN to run postgres integration test")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open pg: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	store := NewSQLSessionStoreWithDialect(db, SQLDialectPostgres)
	if err := store.Init(ctx); err != nil {
		t.Fatalf("init sql session store: %v", err)
	}
	objects := NewFileObjectStore(t.TempDir())
	store.SetMessageArchiveObjectStore(objects, "archive")
	archive := NewMessageArchiveObjectStore(objects, "archive")
	userID := "sql-message-archive-" + time.Now().UTC().Format("20060102T150405.000000000")
	defer func() { _ = store.DeleteUser(context.Background(), userID) }()

	session, err := store.Create(ctx, userID, t.TempDir())
	if err != nil {
		t.Fatalf("create sql session: %v", err)
	}
	old := time.Now().UTC().AddDate(0, 0, -45)
	session.Messages = []state.Message{{
		Role:        state.MessageRoleUser,
		ContentType: state.MessageContentTypeMultipart,
		Content:     strings.Repeat("archived payload ", 80),
		ContentParts: []publictypes.ContentBlock{
			{Type: "text", Text: "archived text part"},
		},
		ToolInput: json.RawMessage(`{"large":true}`),
		CreatedAt: old,
		UpdatedAt: old,
	}}
	if err := store.Save(ctx, userID, session); err != nil {
		t.Fatalf("save old message: %v", err)
	}

	worker := NewMessageArchiveWorker(store, archive, MessageArchiveWorkerConfig{
		ArchiveAfter:   30 * 24 * time.Hour,
		BatchSize:      10,
		ClearPGPayload: true,
	}, nil)
	worker.now = func() time.Time { return time.Now().UTC() }
	processed, err := worker.ProcessBatch(ctx)
	if err != nil {
		t.Fatalf("process archive batch: %v", err)
	}
	if processed != 1 {
		t.Fatalf("expected one processed message, got %d", processed)
	}

	var archiveURI, contentPartsRaw string
	if err := db.QueryRowContext(ctx, `SELECT archive_uri, content_parts::text FROM agent_messages WHERE user_id = $1 AND session_id = $2`, userID, session.ID).Scan(&archiveURI, &contentPartsRaw); err != nil {
		t.Fatalf("read raw archived row: %v", err)
	}
	if archiveURI == "" || contentPartsRaw != "[]" {
		t.Fatalf("expected SQL payload cleared and archive uri set, uri=%q parts=%q", archiveURI, contentPartsRaw)
	}

	messages, err := store.ListMessages(ctx, userID, session.ID)
	if err != nil {
		t.Fatalf("list hydrated messages: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("expected one hydrated message, got %#v", messages)
	}
	if !strings.Contains(messages[0].Content, "archived payload") || len(messages[0].ContentParts) != 1 || messages[0].ArchiveURI == "" {
		t.Fatalf("expected archived payload to hydrate from object store, got %#v", messages[0])
	}
}
