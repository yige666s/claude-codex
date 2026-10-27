package sandboxnode

import (
	"path/filepath"
	"testing"
	"time"
)

func TestFileJournalSurvivesRestartAndRejectsDuplicateJob(t *testing.T) {
	path := filepath.Join(t.TempDir(), "leases.json")
	now := time.Now().UTC()
	open := func() *FileJournal {
		f, err := OpenFileJournal(path, 4, 3, func() time.Time { return now })
		if err != nil {
			t.Fatal(err)
		}
		return f
	}
	first := open()
	spec := PrepareSpec{JobID: "job", UserID: "alice", PolicyRevision: "rev", IdempotencyKey: "key", Deadline: now.Add(time.Minute)}
	lease, err := first.Prepare(spec)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.AppendEvent(lease.LeaseID, "status", "prepared"); err != nil {
		t.Fatal(err)
	}
	second := open()
	retry, err := second.Prepare(spec)
	if err != nil || retry.LeaseID != lease.LeaseID {
		t.Fatalf("restart retry=%+v %v", retry, err)
	}
	events, _, err := second.Events(lease.LeaseID, "alice", 0, 3)
	if err != nil || len(events) != 1 {
		t.Fatalf("restart events=%+v %v", events, err)
	}
	if _, err := second.Dispose(lease.LeaseID, "alice"); err != nil {
		t.Fatal(err)
	}
	third := open()
	retry, err = third.Prepare(spec)
	if err != nil || retry.State != LeaseDisposed || retry.LeaseID != lease.LeaseID {
		t.Fatalf("tombstone retry=%+v %v", retry, err)
	}
}
