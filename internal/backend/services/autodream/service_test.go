package autodream

import (
	"testing"
	"time"

	"claude-codex/internal/harness/state"
)

func TestShouldRunAndLock(t *testing.T) {
	service := NewService(DefaultConfig())
	sessions := []*state.Session{{ID: "a"}, {ID: "b"}, {ID: "c"}, {ID: "d"}, {ID: "e"}}
	if !service.ShouldRun(time.Now().Add(-25*time.Hour), sessions) {
		t.Fatal("expected autodream to run")
	}
	base := t.TempDir()
	ok, err := TryAcquireLock(base)
	if err != nil || !ok {
		t.Fatalf("TryAcquireLock err=%v ok=%v", err, ok)
	}
	ok, err = TryAcquireLock(base)
	if err != nil || ok {
		t.Fatalf("expected second lock acquisition to fail cleanly")
	}
	if err := ReleaseLock(base); err != nil {
		t.Fatalf("ReleaseLock: %v", err)
	}
}
