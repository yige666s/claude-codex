package agentruntime

import (
	"context"
	"testing"
)

func TestMemoryChatStreamStoreLatestActiveForSession(t *testing.T) {
	store := NewMemoryChatStreamStore()
	ctx := context.Background()
	terminalRun, err := store.CreateRun(ctx, "alice", "session-1")
	if err != nil {
		t.Fatalf("create terminal run: %v", err)
	}
	if _, err := store.Append(ctx, terminalRun.RunID, "alice", "session-1", Event{Type: "done"}); err != nil {
		t.Fatalf("append terminal event: %v", err)
	}
	activeRun, err := store.CreateRun(ctx, "alice", "session-1")
	if err != nil {
		t.Fatalf("create active run: %v", err)
	}
	last, err := store.Append(ctx, activeRun.RunID, "alice", "session-1", Event{Type: "delta", Content: "hello"})
	if err != nil {
		t.Fatalf("append active event: %v", err)
	}

	active, err := store.LatestActiveForSession(ctx, "alice", "session-1")
	if err != nil {
		t.Fatalf("latest active run: %v", err)
	}
	if active == nil || active.RunID != activeRun.RunID || active.LastEventID != last.ID || active.Terminal {
		t.Fatalf("unexpected active run: %#v, want run=%s last=%s", active, activeRun.RunID, last.ID)
	}
	if other, err := store.LatestActiveForSession(ctx, "bob", "session-1"); err != nil || other != nil {
		t.Fatalf("wrong user should not see run: run=%#v err=%v", other, err)
	}
}

func TestMemoryChatStreamStoreJobHandoffIsTerminal(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryChatStreamStore()
	run, err := store.CreateRunWithID(ctx, "alice", "session-1", "run-handoff")
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	if _, err := store.Append(ctx, run.RunID, "alice", "session-1", Event{Type: "job", JobID: "job-1"}); err != nil {
		t.Fatalf("append job: %v", err)
	}
	if _, err := store.Append(ctx, run.RunID, "alice", "session-1", Event{Type: "job_handoff", JobID: "job-1"}); err != nil {
		t.Fatalf("append handoff: %v", err)
	}
	active, err := store.LatestActiveForSession(ctx, "alice", "session-1")
	if err != nil {
		t.Fatalf("latest active: %v", err)
	}
	if active != nil {
		t.Fatalf("handed-off chat run remained active: %#v", active)
	}
	_, terminal, err := store.ListAfter(ctx, "alice", run.RunID, "", 10)
	if err != nil {
		t.Fatalf("list handed-off run: %v", err)
	}
	if !terminal {
		t.Fatal("handoff run was not terminal")
	}
}
