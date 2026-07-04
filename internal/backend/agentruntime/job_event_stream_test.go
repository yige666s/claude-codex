package agentruntime

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestRedisJobEventStreamStoreReadsAfterJobEventID(t *testing.T) {
	rawURL := os.Getenv("AGENT_RUNTIME_TEST_REDIS_URL")
	if rawURL == "" {
		t.Skip("set AGENT_RUNTIME_TEST_REDIS_URL to run redis integration test")
	}
	client, err := NewRedisClientFromURL(rawURL)
	if err != nil {
		t.Fatalf("new redis client: %v", err)
	}
	defer client.Close()

	prefix := "agentapi:test:job-events:" + newSortableID()
	store := NewRedisJobEventStreamStore(client, RedisJobEventStreamConfig{
		Prefix: prefix,
		TTL:    time.Minute,
		MaxLen: 100,
	})
	jobID := "job-" + newSortableID()
	ctx := context.Background()
	t.Cleanup(func() {
		_ = client.Del(context.Background(), prefix+":job:"+jobID+":events", prefix+":job:"+jobID+":event-ids").Err()
	})

	first := &JobEvent{
		ID:        NewJobEventID(),
		JobID:     jobID,
		UserID:    "alice",
		SessionID: "session-1",
		Type:      "delta",
		Event:     Event{Type: "delta", JobID: jobID, SessionID: "session-1", Content: "one"},
		CreatedAt: time.Now().UTC(),
	}
	second := &JobEvent{
		ID:        NewJobEventID(),
		JobID:     jobID,
		UserID:    "alice",
		SessionID: "session-1",
		Type:      "done",
		Event:     Event{Type: "done", JobID: jobID, SessionID: "session-1"},
		CreatedAt: time.Now().UTC(),
	}
	if err := store.AppendJobEvent(ctx, first); err != nil {
		t.Fatalf("append first event: %v", err)
	}
	if err := store.AppendJobEvent(ctx, second); err != nil {
		t.Fatalf("append second event: %v", err)
	}

	all, err := store.BlockReadJobEvents(ctx, "alice", jobID, "", 10, time.Millisecond)
	if err != nil {
		t.Fatalf("read all events: %v", err)
	}
	if len(all) != 2 || all[0].ID != first.ID || all[1].ID != second.ID {
		t.Fatalf("unexpected initial stream read: %#v", all)
	}

	resumed, err := store.BlockReadJobEvents(ctx, "alice", jobID, first.ID, 10, time.Millisecond)
	if err != nil {
		t.Fatalf("resume after first event: %v", err)
	}
	if len(resumed) != 1 || resumed[0].ID != second.ID || resumed[0].Event.Type != "done" {
		t.Fatalf("unexpected resumed stream read: %#v", resumed)
	}
}
