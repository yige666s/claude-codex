package sandboxnode

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

var (
	ErrLeaseCapacity = errors.New("sandbox node lease capacity reached")
	ErrLeaseConflict = errors.New("sandbox node job idempotency conflict")
	ErrLeaseMissing  = errors.New("sandbox node lease not found")
	ErrCursorExpired = errors.New("sandbox node event cursor expired")
)

type LeaseState string

const (
	LeasePrepared LeaseState = "prepared"
	LeaseCanceled LeaseState = "canceled"
	LeaseDisposed LeaseState = "disposed"
)

// PrepareSpec contains identity and policy data only. No code, command, or
// claimed trust class is accepted by the journal.
type PrepareSpec struct {
	JobID          string    `json:"job_id"`
	UserID         string    `json:"user_id"`
	PolicyRevision string    `json:"policy_revision"`
	IdempotencyKey string    `json:"idempotency_key"`
	Deadline       time.Time `json:"deadline"`
}

type LeaseSnapshot struct {
	LeaseID        string     `json:"lease_id"`
	JobID          string     `json:"job_id"`
	UserID         string     `json:"user_id"`
	PolicyRevision string     `json:"policy_revision"`
	State          LeaseState `json:"state"`
	Deadline       time.Time  `json:"deadline"`
}

type LeaseEvent struct {
	Sequence int64     `json:"sequence"`
	Type     string    `json:"type"`
	Summary  string    `json:"summary"`
	At       time.Time `json:"at"`
}

type journalLease struct {
	LeaseSnapshot
	key     string
	events  []LeaseEvent
	lastSeq int64
}

// LeaseJournal is a bounded, in-memory protocol primitive for contract tests.
// It is NOT a durable queue or an executor; do not use it for production jobs
// until persistent fencing/reconciliation and policy verification exist.
type LeaseJournal struct {
	mu        sync.Mutex
	byJob     map[string]*journalLease
	byID      map[string]*journalLease
	maxLeases int
	maxEvents int
	now       func() time.Time
}

func NewLeaseJournal(maxLeases, maxEvents int, now func() time.Time) (*LeaseJournal, error) {
	if maxLeases <= 0 || maxEvents <= 0 || maxEvents > 4096 {
		return nil, errors.New("invalid sandbox node journal bounds")
	}
	if now == nil {
		now = time.Now
	}
	return &LeaseJournal{byJob: make(map[string]*journalLease), byID: make(map[string]*journalLease), maxLeases: maxLeases, maxEvents: maxEvents, now: now}, nil
}

func (j *LeaseJournal) Prepare(spec PrepareSpec) (LeaseSnapshot, error) {
	if j == nil {
		return LeaseSnapshot{}, ErrLeaseMissing
	}
	if invalidIdentity(spec.JobID) || invalidIdentity(spec.UserID) || invalidIdentity(spec.PolicyRevision) || invalidIdentity(spec.IdempotencyKey) || spec.Deadline.IsZero() {
		return LeaseSnapshot{}, errors.New("invalid sandbox node prepare identity")
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if existing := j.byJob[spec.JobID]; existing != nil {
		if existing.key != spec.IdempotencyKey || existing.UserID != spec.UserID || existing.PolicyRevision != spec.PolicyRevision || !existing.Deadline.Equal(spec.Deadline) {
			return LeaseSnapshot{}, ErrLeaseConflict
		}
		return existing.LeaseSnapshot, nil
	}
	if !spec.Deadline.After(j.now()) {
		return LeaseSnapshot{}, errors.New("sandbox node prepare deadline has passed")
	}
	if len(j.byJob) >= j.maxLeases {
		return LeaseSnapshot{}, ErrLeaseCapacity
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return LeaseSnapshot{}, fmt.Errorf("generate sandbox node lease ID: %w", err)
	}
	lease := &journalLease{LeaseSnapshot: LeaseSnapshot{
		LeaseID: hex.EncodeToString(random[:]), JobID: spec.JobID, UserID: spec.UserID,
		PolicyRevision: spec.PolicyRevision, State: LeasePrepared, Deadline: spec.Deadline,
	}, key: spec.IdempotencyKey}
	j.byJob[spec.JobID], j.byID[lease.LeaseID] = lease, lease
	return lease.LeaseSnapshot, nil
}

func invalidIdentity(value string) bool {
	return value == "" || strings.TrimSpace(value) != value || len(value) > 128 || strings.ContainsAny(value, "\x00\r\n")
}

func (j *LeaseJournal) transition(leaseID, userID string, target LeaseState) (LeaseSnapshot, error) {
	if j == nil {
		return LeaseSnapshot{}, ErrLeaseMissing
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	lease := j.byID[leaseID]
	if lease == nil || invalidIdentity(userID) || lease.UserID != userID {
		return LeaseSnapshot{}, ErrLeaseMissing
	}
	if lease.State == LeasePrepared || target == LeaseDisposed {
		lease.State = target
	}
	return lease.LeaseSnapshot, nil
}

func (j *LeaseJournal) Snapshot(leaseID, userID string) (LeaseSnapshot, error) {
	if j == nil {
		return LeaseSnapshot{}, ErrLeaseMissing
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	lease := j.byID[leaseID]
	if lease == nil || invalidIdentity(userID) || lease.UserID != userID {
		return LeaseSnapshot{}, ErrLeaseMissing
	}
	return lease.LeaseSnapshot, nil
}

func (j *LeaseJournal) Cancel(leaseID, userID string) (LeaseSnapshot, error) {
	return j.transition(leaseID, userID, LeaseCanceled)
}

func (j *LeaseJournal) Dispose(leaseID, userID string) (LeaseSnapshot, error) {
	return j.transition(leaseID, userID, LeaseDisposed)
}

// AppendEvent is for the trusted worker only; summaries must never contain
// scripts, credentials or full output. Callers cannot append to terminal jobs.
func (j *LeaseJournal) AppendEvent(leaseID, eventType, summary string) (LeaseEvent, error) {
	if j == nil {
		return LeaseEvent{}, ErrLeaseMissing
	}
	if eventType != "progress" && eventType != "status" && eventType != "error" || len(summary) > 1024 {
		return LeaseEvent{}, errors.New("invalid sandbox node event")
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	lease := j.byID[leaseID]
	if lease == nil || lease.State != LeasePrepared {
		return LeaseEvent{}, ErrLeaseMissing
	}
	lease.lastSeq++
	event := LeaseEvent{Sequence: lease.lastSeq, Type: eventType, Summary: summary, At: j.now()}
	lease.events = append(lease.events, event)
	if len(lease.events) > j.maxEvents {
		lease.events = lease.events[len(lease.events)-j.maxEvents:]
	}
	return event, nil
}

func (j *LeaseJournal) Events(leaseID, userID string, cursor int64, limit int) ([]LeaseEvent, int64, error) {
	if j == nil {
		return nil, cursor, ErrLeaseMissing
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	lease := j.byID[leaseID]
	if lease == nil || invalidIdentity(userID) || lease.UserID != userID {
		return nil, cursor, ErrLeaseMissing
	}
	if cursor < 0 || cursor > lease.lastSeq || limit <= 0 {
		return nil, cursor, errors.New("invalid sandbox node event cursor or limit")
	}
	if len(lease.events) > 0 && cursor < lease.events[0].Sequence-1 {
		return nil, cursor, ErrCursorExpired
	}
	if limit > j.maxEvents {
		limit = j.maxEvents
	}
	out := make([]LeaseEvent, 0, limit)
	for _, event := range lease.events {
		if event.Sequence > cursor {
			out = append(out, event)
			if len(out) == limit {
				break
			}
		}
	}
	if len(out) > 0 {
		cursor = out[len(out)-1].Sequence
	}
	return out, cursor, nil
}

// Reap marks overdue jobs canceled and drops terminal tombstones only after
// deadline+retention. The eventual executor must separately stop processes.
func (j *LeaseJournal) Reap(retention time.Duration) int {
	if j == nil || retention < 0 {
		return 0
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	removed := 0
	now := j.now()
	for jobID, lease := range j.byJob {
		if lease.State == LeasePrepared && !now.Before(lease.Deadline) {
			lease.State = LeaseCanceled
		}
		if lease.State != LeasePrepared && !now.Before(lease.Deadline.Add(retention)) {
			delete(j.byJob, jobID)
			delete(j.byID, lease.LeaseID)
			removed++
		}
	}
	return removed
}
