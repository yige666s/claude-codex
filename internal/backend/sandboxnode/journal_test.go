package sandboxnode

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func TestLeaseJournalIdempotencyAndCapacity(t *testing.T) {
	now := time.Date(2026, 10, 27, 0, 0, 0, 0, time.UTC)
	j, err := NewLeaseJournal(1, 2, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	spec := PrepareSpec{JobID: "job-a", UserID: "alice", PolicyRevision: "rev-1", IdempotencyKey: "key-1", Deadline: now.Add(time.Minute)}
	first, err := j.Prepare(spec)
	if err != nil || first.State != LeasePrepared {
		t.Fatalf("prepare = %+v, %v", first, err)
	}
	again, err := j.Prepare(spec)
	if err != nil || again.LeaseID != first.LeaseID {
		t.Fatalf("retry duplicated lease: %+v, %v", again, err)
	}
	changed := spec
	changed.IdempotencyKey = "key-2"
	if _, err := j.Prepare(changed); !errors.Is(err, ErrLeaseConflict) {
		t.Fatalf("conflict = %v", err)
	}
	changed.JobID = "job-b"
	if _, err := j.Prepare(changed); !errors.Is(err, ErrLeaseCapacity) {
		t.Fatalf("capacity = %v", err)
	}
	if got := j.Reap(time.Minute); got != 0 {
		t.Fatalf("premature reap = %d", got)
	}
	now = now.Add(2 * time.Minute)
	if got := j.Reap(time.Minute); got != 1 {
		t.Fatalf("expired lease not reaped: %d", got)
	}
	if _, err := j.Prepare(changed); err == nil {
		t.Fatal("expired deadline accepted")
	}
	changed.Deadline = now.Add(time.Minute)
	if _, err := j.Prepare(changed); err != nil {
		t.Fatal(err)
	}
}

func TestLeaseJournalEventsCancelAndDispose(t *testing.T) {
	now := time.Now()
	j, err := NewLeaseJournal(2, 2, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	lease, err := j.Prepare(PrepareSpec{JobID: "job-1", UserID: "alice", PolicyRevision: "rev-1", IdempotencyKey: "key-1", Deadline: now.Add(time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	for _, summary := range []string{"one", "two", "three"} {
		if _, err := j.AppendEvent(lease.LeaseID, "progress", summary); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := j.Events(lease.LeaseID, "alice", 0, 2); !errors.Is(err, ErrCursorExpired) {
		t.Fatalf("stale cursor = %v", err)
	}
	items, next, err := j.Events(lease.LeaseID, "alice", 1, 10)
	if err != nil || next != 3 || len(items) != 2 || items[0].Summary != "two" {
		t.Fatalf("events = %+v, %d, %v", items, next, err)
	}
	if _, _, err := j.Events(lease.LeaseID, "bob", 1, 2); !errors.Is(err, ErrLeaseMissing) {
		t.Fatalf("cross user events = %v", err)
	}
	if _, err := j.Cancel(lease.LeaseID, "bob"); !errors.Is(err, ErrLeaseMissing) {
		t.Fatalf("cross user cancel = %v", err)
	}
	canceled, err := j.Cancel(lease.LeaseID, "alice")
	if err != nil || canceled.State != LeaseCanceled {
		t.Fatalf("cancel = %+v, %v", canceled, err)
	}
	if _, err := j.AppendEvent(lease.LeaseID, "progress", "late"); err == nil {
		t.Fatal("event appended after cancel")
	}
	disposed, err := j.Dispose(lease.LeaseID, "alice")
	if err != nil || disposed.State != LeaseDisposed {
		t.Fatalf("dispose = %+v, %v", disposed, err)
	}
	again, err := j.Prepare(PrepareSpec{JobID: "job-1", UserID: "alice", PolicyRevision: "rev-1", IdempotencyKey: "key-1", Deadline: lease.Deadline})
	if err != nil || again.State != LeaseDisposed || again.LeaseID != lease.LeaseID {
		t.Fatalf("disposed retry = %+v, %v", again, err)
	}
}

func TestLeaseJournalRejectsInvalidBoundsAndIdentity(t *testing.T) {
	if _, err := NewLeaseJournal(0, 1, nil); err == nil {
		t.Fatal("zero capacity accepted")
	}
	if _, err := NewLeaseJournal(1, 4097, nil); err == nil {
		t.Fatal("unbounded event window accepted")
	}
	j, _ := NewLeaseJournal(1, 1, nil)
	if _, err := j.Prepare(PrepareSpec{JobID: "bad\njob", UserID: "alice", PolicyRevision: "rev", IdempotencyKey: "k", Deadline: time.Now().Add(time.Hour)}); err == nil {
		t.Fatal("invalid id accepted")
	}
}

func TestLeaseJournalConcurrentPrepareReturnsSingleLease(t *testing.T) {
	j, _ := NewLeaseJournal(1, 2, nil)
	spec := PrepareSpec{JobID: "job", UserID: "alice", PolicyRevision: "rev", IdempotencyKey: "key", Deadline: time.Now().Add(time.Minute)}
	var wg sync.WaitGroup
	ids := make(chan string, 24)
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			lease, err := j.Prepare(spec)
			if err != nil {
				ids <- "error: " + err.Error()
			} else {
				ids <- lease.LeaseID
			}
		}()
	}
	wg.Wait()
	close(ids)
	var first string
	for id := range ids {
		if first == "" {
			first = id
		}
		if id != first || len(id) != 32 {
			t.Fatalf("concurrent prepare diverged: %q / %q", first, id)
		}
	}
}
