package agentruntime

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestRedisChatStreamStoreReadsAfterEventID(t *testing.T) {
	rawURL := os.Getenv("AGENT_RUNTIME_TEST_REDIS_URL")
	if rawURL == "" {
		t.Skip("set AGENT_RUNTIME_TEST_REDIS_URL to run redis integration test")
	}
	client, err := NewRedisClientFromURL(rawURL)
	if err != nil {
		t.Fatalf("new redis client: %v", err)
	}
	defer client.Close()

	prefix := "agentapi:test:chat-events:" + newSortableID()
	store := NewRedisChatStreamStore(client, RedisChatStreamConfig{
		Prefix: prefix,
		TTL:    time.Minute,
		MaxLen: 100,
		Block:  time.Millisecond,
	})
	ctx := context.Background()
	run, err := store.CreateRun(ctx, "alice", "session-1")
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	t.Cleanup(func() {
		_ = client.Del(context.Background(),
			store.streamKey(run.RunID),
			store.indexKey(run.RunID),
			store.metaKey(run.RunID),
			store.activeKey("alice", "session-1"),
		).Err()
	})

	first, err := store.Append(ctx, run.RunID, "alice", "session-1", Event{Type: "delta", Content: "one"})
	if err != nil {
		t.Fatalf("append first: %v", err)
	}
	second, err := store.Append(ctx, run.RunID, "alice", "session-1", Event{Type: "done"})
	if err != nil {
		t.Fatalf("append second: %v", err)
	}

	all, terminal, err := store.ListAfter(ctx, "alice", run.RunID, "", 10)
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if len(all) != 2 || all[0].ID != first.ID || all[1].ID != second.ID || !terminal {
		t.Fatalf("unexpected full replay: events=%#v terminal=%t", all, terminal)
	}

	resumed, terminal, err := store.BlockRead(ctx, "alice", run.RunID, first.ID, 10, time.Millisecond)
	if err != nil {
		t.Fatalf("resume after first: %v", err)
	}
	if len(resumed) != 1 || resumed[0].ID != second.ID || resumed[0].Event.Type != "done" || !terminal {
		t.Fatalf("unexpected resumed replay: events=%#v terminal=%t", resumed, terminal)
	}

	if active, err := store.LatestActiveForSession(ctx, "alice", "session-1"); err != nil || active != nil {
		t.Fatalf("terminal run should not be active: run=%#v err=%v", active, err)
	}
	if _, _, err := store.ListAfter(ctx, "bob", run.RunID, "", 10); err == nil {
		t.Fatal("wrong user should not read chat run events")
	}
}

func TestRedisChatStreamStoreLatestActiveForSession(t *testing.T) {
	rawURL := os.Getenv("AGENT_RUNTIME_TEST_REDIS_URL")
	if rawURL == "" {
		t.Skip("set AGENT_RUNTIME_TEST_REDIS_URL to run redis integration test")
	}
	client, err := NewRedisClientFromURL(rawURL)
	if err != nil {
		t.Fatalf("new redis client: %v", err)
	}
	defer client.Close()

	prefix := "agentapi:test:chat-events:" + newSortableID()
	store := NewRedisChatStreamStore(client, RedisChatStreamConfig{
		Prefix: prefix,
		TTL:    time.Minute,
		MaxLen: 100,
		Block:  time.Millisecond,
	})
	ctx := context.Background()
	run, err := store.CreateRun(ctx, "alice", "session-1")
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	t.Cleanup(func() {
		_ = client.Del(context.Background(),
			store.streamKey(run.RunID),
			store.indexKey(run.RunID),
			store.metaKey(run.RunID),
			store.activeKey("alice", "session-1"),
		).Err()
	})
	last, err := store.Append(ctx, run.RunID, "alice", "session-1", Event{Type: "answer_delta", Content: "working"})
	if err != nil {
		t.Fatalf("append event: %v", err)
	}

	active, err := store.LatestActiveForSession(ctx, "alice", "session-1")
	if err != nil {
		t.Fatalf("latest active: %v", err)
	}
	if active == nil || active.RunID != run.RunID || active.LastEventID != last.ID || active.Terminal || active.Status != "running" {
		t.Fatalf("unexpected active run: %#v", active)
	}
	if other, err := store.LatestActiveForSession(ctx, "bob", "session-1"); err != nil || other != nil {
		t.Fatalf("wrong user should not see run: run=%#v err=%v", other, err)
	}
}
