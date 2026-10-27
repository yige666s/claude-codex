package sandboxnode

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

type fileLease struct {
	LeaseSnapshot
	Key     string       `json:"key"`
	Events  []LeaseEvent `json:"events"`
	LastSeq int64        `json:"last_seq"`
}
type fileSnapshot struct {
	Version string      `json:"version"`
	Leases  []fileLease `json:"leases"`
}

// FileJournal persists a bounded simulation lease journal. It contains no
// commands and must not be used as a production execution scheduler.
type FileJournal struct {
	mu       sync.Mutex
	journal  *LeaseJournal
	path     string
	poisoned bool
}

func OpenFileJournal(path string, maxLeases, maxEvents int, now func() time.Time) (*FileJournal, error) {
	if !filepath.IsAbs(path) {
		return nil, errors.New("journal path must be absolute")
	}
	j, err := NewLeaseJournal(maxLeases, maxEvents, now)
	if err != nil {
		return nil, err
	}
	f := &FileJournal{journal: j, path: path}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return f, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || (runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0) {
		return nil, errors.New("journal file must be private and regular")
	}
	if info.Size() > 8<<20 {
		return nil, errors.New("journal file too large")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var snap fileSnapshot
	if err = json.Unmarshal(raw, &snap); err != nil {
		return nil, fmt.Errorf("decode sandbox journal: %w", err)
	}
	if snap.Version != ProtocolVersion {
		return nil, errors.New("sandbox journal version mismatch")
	}
	if len(snap.Leases) > maxLeases {
		return nil, errors.New("journal exceeds configured capacity")
	}
	for _, record := range snap.Leases {
		if invalidIdentity(record.JobID) || invalidIdentity(record.UserID) || invalidIdentity(record.PolicyRevision) || invalidIdentity(record.Key) || len(record.LeaseID) != 32 || record.Deadline.IsZero() || record.LastSeq < 0 || len(record.Events) > maxEvents || (record.State != LeasePrepared && record.State != LeaseCanceled && record.State != LeaseDisposed) {
			return nil, errors.New("invalid sandbox journal record")
		}
		if j.byJob[record.JobID] != nil || j.byID[record.LeaseID] != nil {
			return nil, errors.New("duplicate sandbox journal record")
		}
		lease := &journalLease{LeaseSnapshot: record.LeaseSnapshot, key: record.Key, events: record.Events, lastSeq: record.LastSeq}
		j.byJob[record.JobID], j.byID[record.LeaseID] = lease, lease
	}
	return f, nil
}

func (f *FileJournal) persist() error {
	snap := fileSnapshot{Version: ProtocolVersion, Leases: make([]fileLease, 0, len(f.journal.byJob))}
	f.journal.mu.Lock()
	for _, l := range f.journal.byJob {
		snap.Leases = append(snap.Leases, fileLease{LeaseSnapshot: l.LeaseSnapshot, Key: l.key, Events: l.events, LastSeq: l.lastSeq})
	}
	f.journal.mu.Unlock()
	raw, err := json.Marshal(snap)
	if err != nil {
		return err
	}
	if len(raw) > 8<<20 {
		return errors.New("journal snapshot too large")
	}
	dir := filepath.Dir(f.path)
	temp, err := os.CreateTemp(dir, ".sandbox-journal-*")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	if err = temp.Chmod(0o600); err != nil {
		temp.Close()
		return err
	}
	if _, err = temp.Write(raw); err != nil {
		temp.Close()
		return err
	}
	if err = temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err = temp.Close(); err != nil {
		return err
	}
	return os.Rename(temp.Name(), f.path)
}

func (f *FileJournal) mutate(fn func() error) error {
	if f == nil {
		return ErrLeaseMissing
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.poisoned {
		return errors.New("sandbox journal persistence failed; admission disabled")
	}
	if err := fn(); err != nil {
		return err
	}
	if err := f.persist(); err != nil {
		f.poisoned = true
		return fmt.Errorf("persist sandbox journal: %w", err)
	}
	return nil
}
func (f *FileJournal) Prepare(spec PrepareSpec) (LeaseSnapshot, error) {
	var v LeaseSnapshot
	err := f.mutate(func() error { var e error; v, e = f.journal.Prepare(spec); return e })
	return v, err
}
func (f *FileJournal) Cancel(id, user string) (LeaseSnapshot, error) {
	var v LeaseSnapshot
	err := f.mutate(func() error { var e error; v, e = f.journal.Cancel(id, user); return e })
	return v, err
}
func (f *FileJournal) Dispose(id, user string) (LeaseSnapshot, error) {
	var v LeaseSnapshot
	err := f.mutate(func() error { var e error; v, e = f.journal.Dispose(id, user); return e })
	return v, err
}
func (f *FileJournal) AppendEvent(id, kind, summary string) (LeaseEvent, error) {
	var v LeaseEvent
	err := f.mutate(func() error { var e error; v, e = f.journal.AppendEvent(id, kind, summary); return e })
	return v, err
}
func (f *FileJournal) Events(id, user string, cursor int64, limit int) ([]LeaseEvent, int64, error) {
	if f == nil {
		return nil, cursor, ErrLeaseMissing
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.poisoned {
		return nil, cursor, errors.New("sandbox journal persistence failed")
	}
	return f.journal.Events(id, user, cursor, limit)
}
func (f *FileJournal) Reap(retention time.Duration) (int, error) {
	var n int
	err := f.mutate(func() error { n = f.journal.Reap(retention); return nil })
	return n, err
}

func (f *FileJournal) Snapshot(id, user string) (LeaseSnapshot, error) {
	if f == nil {
		return LeaseSnapshot{}, ErrLeaseMissing
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.poisoned {
		return LeaseSnapshot{}, errors.New("sandbox journal persistence failed")
	}
	return f.journal.Snapshot(id, user)
}
func (f *FileJournal) AppendEventForUser(id, user, kind, summary string) (LeaseEvent, error) {
	var event LeaseEvent
	err := f.mutate(func() error {
		lease, e := f.journal.Snapshot(id, user)
		if e != nil {
			return e
		}
		if lease.State != LeasePrepared {
			return ErrLeaseMissing
		}
		event, e = f.journal.AppendEvent(id, kind, summary)
		return e
	})
	return event, err
}
