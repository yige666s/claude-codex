package agentruntime

import (
	"context"
	"testing"
	"time"

	"claude-codex/internal/harness/state"
)

func TestMessageFullTextBackfillWorkerIndexesBatchesWithCursor(t *testing.T) {
	base := time.Date(2026, 7, 6, 1, 2, 3, 0, time.UTC)
	store := &fakeFullTextBackfillStore{messages: []state.Message{
		{ID: "m1", UserID: "alice", SessionID: "s1", Content: "他们都不知道", CreatedAt: base},
		{ID: "m2", UserID: "alice", SessionID: "s1", Content: "也许永远不会知道了", CreatedAt: base.Add(time.Second)},
		{ID: "m3", UserID: "bob", SessionID: "s2", Content: "中文回填", CreatedAt: base.Add(2 * time.Second)},
	}}
	indexer := &captureFullTextIndexer{}
	worker := NewMessageFullTextBackfillWorker(store, indexer, 2, time.Hour, nil)

	indexed, err := worker.BackfillOnce(context.Background())
	if err != nil {
		t.Fatalf("BackfillOnce() error = %v", err)
	}
	if indexed != 3 || indexer.calls != 3 {
		t.Fatalf("indexed=%d calls=%d messages=%#v", indexed, indexer.calls, indexer.messages)
	}
	if len(store.calls) != 2 {
		t.Fatalf("expected two batch reads, got %#v", store.calls)
	}
	if store.calls[1].afterID != "m2" || !store.calls[1].after.Equal(base.Add(time.Second)) {
		t.Fatalf("unexpected second cursor: %#v", store.calls[1])
	}
}

type fakeFullTextBackfillStore struct {
	messages []state.Message
	calls    []struct {
		after   time.Time
		afterID string
		limit   int
	}
}

func (s *fakeFullTextBackfillStore) ListMessagesForFullTextBackfill(_ context.Context, after time.Time, afterID string, limit int) ([]state.Message, error) {
	s.calls = append(s.calls, struct {
		after   time.Time
		afterID string
		limit   int
	}{after: after, afterID: afterID, limit: limit})
	out := make([]state.Message, 0, limit)
	for _, message := range s.messages {
		if !after.IsZero() {
			switch {
			case message.CreatedAt.Before(after):
				continue
			case message.CreatedAt.Equal(after) && message.ID <= afterID:
				continue
			}
		}
		out = append(out, message)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}
