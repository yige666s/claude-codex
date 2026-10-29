//go:build linux

package sandboxstaging

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"claude-codex/internal/backend/agentruntime/sandboxnode"
)

type recoveryDocker struct {
	mu            sync.Mutex
	names         []string
	failInventory bool
	failRemoval   bool
	calls         [][]string
}

func (d *recoveryDocker) Run(ctx context.Context, args []string, _ []byte, stdout, stderr io.Writer) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls = append(d.calls, append([]string(nil), args...))
	switch args[0] {
	case "ps":
		if d.failInventory {
			return errors.New("daemon unavailable")
		}
		_, err := io.WriteString(stdout, strings.Join(d.names, "\n"))
		return err
	case "rm":
		if d.failRemoval {
			return errors.New("daemon denied removal")
		}
		for i, name := range d.names {
			if name == args[2] {
				d.names = append(d.names[:i], d.names[i+1:]...)
				return nil
			}
		}
		return errors.New("missing container")
	default:
		return errors.New("unexpected Docker command")
	}
}
func TestRecoveryFencesOldLeaseRemovesOrphansAndHoldsExclusiveLock(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "tasks")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	orphan := filepath.Join(root, "task-leftover")
	if err := os.Mkdir(orphan, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(orphan, "input.txt"), []byte("discard"), 0600); err != nil {
		t.Fatal(err)
	}
	journalPath := filepath.Join(base, "leases.json")
	journal, err := sandboxnode.OpenFileJournal(journalPath, 4, 16, nil)
	if err != nil {
		t.Fatal(err)
	}
	lease, err := journal.Prepare(sandboxnode.PrepareSpec{JobID: "old-task", UserID: "alice", PolicyRevision: "v1", IdempotencyKey: "once", Deadline: time.Now().Add(time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	d := &recoveryDocker{names: []string{"agentapi-oci-" + lease.LeaseID}}
	stager := sandboxnode.Stager{Root: root}
	recovery, err := ReconcileDevelopmentStartup(context.Background(), stager, journalPath, d)
	if err != nil {
		t.Fatal(err)
	}
	defer recovery.Close()
	snapshot, err := recovery.Journal.Snapshot(lease.LeaseID, "alice")
	if err != nil || snapshot.State != sandboxnode.LeaseCanceled {
		t.Fatalf("unfenced lease: %+v %v", snapshot, err)
	}
	if _, err := os.Stat(orphan); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("orphan survived: %v", err)
	}
	if len(d.names) != 0 {
		t.Fatalf("container survived: %v", d.names)
	}
	if _, err := ReconcileDevelopmentStartup(context.Background(), stager, journalPath, d); err == nil {
		t.Fatal("second Node acquired active root")
	}
	if err := recovery.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := ReconcileDevelopmentStartup(context.Background(), stager, journalPath, d)
	if err != nil {
		t.Fatal(err)
	}
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
	tombstone, err := second.Journal.LookupJob("old-task")
	if err != nil || tombstone.State != sandboxnode.LeaseCanceled {
		t.Fatalf("fenced task tombstone missing: %+v %v", tombstone, err)
	}
}
func TestRecoveryFailsClosedOnDaemonOrUnexpectedInput(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "tasks")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	journal := filepath.Join(base, "journal.json")
	stager := sandboxnode.Stager{Root: root}
	d := &recoveryDocker{failInventory: true}
	if _, err := ReconcileDevelopmentStartup(context.Background(), stager, journal, d); err == nil {
		t.Fatal("inventory failure admitted Node")
	}
	d.failInventory = false
	d.names = []string{"agentapi-oci-" + strings.Repeat("a", 32)}
	d.failRemoval = true
	if _, err := ReconcileDevelopmentStartup(context.Background(), stager, journal, d); err == nil {
		t.Fatal("removal failure admitted Node")
	}
	d.failRemoval = false
	if err := os.WriteFile(filepath.Join(root, "unexpected"), []byte("leave me"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReconcileDevelopmentStartup(context.Background(), stager, journal, d); err == nil {
		t.Fatal("unknown scratch file ignored")
	}
	if _, err := os.Stat(filepath.Join(root, "unexpected")); err != nil {
		t.Fatal("unknown data deleted")
	}
	if _, err := ReconcileDevelopmentStartup(context.Background(), stager, filepath.Join(root, "nested.json"), d); err == nil {
		t.Fatal("journal under cleanup root accepted")
	}
}
