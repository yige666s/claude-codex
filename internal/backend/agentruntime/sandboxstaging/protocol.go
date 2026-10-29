package sandboxstaging

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"time"

	"claude-codex/internal/backend/agentruntime/sandboxartifact"
	"claude-codex/internal/backend/agentruntime/sandboxnode"
	"claude-codex/internal/backend/agentruntime/sandboxoci"
	"claude-codex/internal/backend/agentruntime/sandboxpolicy"
	"claude-codex/internal/backend/agentruntime/sandboxtransfer"
)

// MaxStagingBody accounts for JSON/base64 overhead over an 8-MiB archive.
const MaxStagingBody = 16 << 20

type StageRequest struct {
	TaskID         string                `json:"task_id"`
	UserID         string                `json:"user_id"`
	SessionID      string                `json:"session_id"`
	PolicyRevision string                `json:"policy_revision"`
	Profile        sandboxpolicy.Profile `json:"profile"`
	SkillRef       sandboxartifact.Ref   `json:"skill_ref"`
	Deadline       time.Time             `json:"deadline"`
	Snapshot       []byte                `json:"snapshot"`
}
type StageReceipt struct {
	LeaseID        string              `json:"lease_id"`
	TaskID         string              `json:"task_id"`
	SkillRef       sandboxartifact.Ref `json:"skill_ref"`
	PolicyRevision string              `json:"policy_revision"`
}
type StageControl struct {
	LeaseID string `json:"lease_id"`
	UserID  string `json:"user_id"`
}

type stagedRecord struct {
	hash             string
	taskUserID       string
	receipt          StageReceipt
	task             *sandboxnode.StagedTask
	session          *sandboxoci.Session
	failed           bool
	cleanupUncertain bool
	collected        bool
	outputs          []sandboxtransfer.OutputFile
	executions       map[string]executedCommand
}

// StagingService is an opt-in *preparation-only* contract. Its only source of
// policy is the trusted Node configuration, not the signed request body.
// It has no command endpoint and cannot survive a process restart until
// persistent staged-task reconciliation has been implemented.
type StagingService struct {
	Stager          sandboxnode.Stager
	Journal         *sandboxnode.FileJournal
	Revision        string
	Profile         sandboxpolicy.Profile
	MaxActive       int
	mu              sync.Mutex
	records         map[string]*stagedRecord
	activeMu        sync.Mutex
	active          map[string]context.CancelFunc
	activeDeadlines map[string]time.Time
	fenced          map[string]bool
}

func (s *StagingService) prepare(ctx context.Context, req StageRequest) (StageReceipt, error) {
	if s == nil || s.Journal == nil || s.Revision == "" || s.MaxActive != 1 || s.records == nil ||
		invalidStageIdentity(req.TaskID) || invalidStageIdentity(req.UserID) || invalidStageIdentity(req.SessionID) ||
		req.PolicyRevision != s.Revision || !reflect.DeepEqual(req.Profile, s.Profile) ||
		sandboxpolicy.ValidateFileOCIProfile(s.Profile) != nil || req.SkillRef.Validate() != nil ||
		req.Deadline.Before(time.Now()) || req.Deadline.After(time.Now().Add(time.Hour)) ||
		len(req.Snapshot) == 0 || len(req.Snapshot) > sandboxtransfer.HardMaxBytes+sandboxtransfer.HardMaxFiles*4096+2048 {
		return StageReceipt{}, errors.New("invalid or unauthorized task stage")
	}
	raw, err := json.Marshal(req)
	if err != nil {
		return StageReceipt{}, err
	}
	hash := sha256.Sum256(raw)
	key := hex.EncodeToString(hash[:])
	s.mu.Lock()
	defer s.mu.Unlock()
	if prior, ok := s.records[req.TaskID]; ok {
		if prior.hash != key {
			return StageReceipt{}, sandboxnode.ErrLeaseConflict
		}
		snap, err := s.Journal.Snapshot(prior.receipt.LeaseID, req.UserID)
		if err != nil || snap.State != sandboxnode.LeasePrepared {
			return StageReceipt{}, sandboxnode.ErrLeaseConflict
		}
		return prior.receipt, nil
	}
	if _, err := s.Journal.LookupJob(req.TaskID); err == nil {
		return StageReceipt{}, sandboxnode.ErrLeaseConflict
	} else if !errors.Is(err, sandboxnode.ErrLeaseMissing) {
		return StageReceipt{}, err
	}
	if len(s.records) >= s.MaxActive {
		return StageReceipt{}, sandboxnode.ErrLeaseCapacity
	}
	task, err := s.Stager.Stage(ctx, req.SkillRef, req.Snapshot)
	if err != nil {
		return StageReceipt{}, err
	}
	lease, err := s.Journal.Prepare(sandboxnode.PrepareSpec{JobID: req.TaskID, UserID: req.UserID, PolicyRevision: req.PolicyRevision, IdempotencyKey: key, Deadline: req.Deadline})
	if err != nil {
		_ = task.Release()
		return StageReceipt{}, err
	}
	// A journal entry left by a prior process is not resumed from guessed
	// filesystem state. A restart must reconcile before accepting jobs.
	if lease.State != sandboxnode.LeasePrepared {
		_ = task.Release()
		return StageReceipt{}, sandboxnode.ErrLeaseConflict
	}
	receipt := StageReceipt{LeaseID: lease.LeaseID, TaskID: req.TaskID, SkillRef: req.SkillRef, PolicyRevision: req.PolicyRevision}
	s.records[req.TaskID] = &stagedRecord{hash: key, taskUserID: req.UserID, receipt: receipt, task: task}
	return receipt, nil
}
func (s *StagingService) dispose(control StageControl, cancel bool) error {
	if s == nil || s.Journal == nil || invalidStageIdentity(control.LeaseID) || invalidStageIdentity(control.UserID) {
		return sandboxnode.ErrLeaseMissing
	}
	// Authenticate user ownership before touching the active cancellation map.
	if _, err := s.Journal.Snapshot(control.LeaseID, control.UserID); err != nil {
		return err
	}
	s.fenceAndCancel(control.LeaseID)
	s.mu.Lock()
	defer s.mu.Unlock()
	lease, err := s.Journal.Snapshot(control.LeaseID, control.UserID)
	if err != nil {
		return err
	}
	record, ok := s.records[lease.JobID]
	if !ok {
		if lease.State == sandboxnode.LeaseCanceled || lease.State == sandboxnode.LeaseDisposed {
			return nil
		}
		return sandboxnode.ErrLeaseMissing
	}
	if record.receipt.LeaseID != control.LeaseID {
		return sandboxnode.ErrLeaseMissing
	}
	if record.cleanupUncertain {
		return sandboxoci.ErrCleanupUncertain
	}
	if record.session != nil {
		if err := record.session.Close(); err != nil {
			return err
		}
	}
	if cancel {
		_, err = s.Journal.Cancel(control.LeaseID, control.UserID)
	} else {
		_, err = s.Journal.Dispose(control.LeaseID, control.UserID)
	}
	if err != nil {
		return err
	}
	if err = record.task.Release(); err != nil {
		return err
	}
	delete(s.records, lease.JobID)
	return nil
}

// ReapExpired disposes in-memory staged input when a lease deadline passes.
// A process restart still needs separate orphan reconciliation and fencing;
// this timer alone is not sufficient for production execution.
func (s *StagingService) ReapExpired(now time.Time) (int, error) {
	if s == nil || s.Journal == nil {
		return 0, errors.New("staging service unavailable")
	}
	s.cancelExpiredActive(now)
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 0
	for id, record := range s.records {
		snap, err := s.Journal.Snapshot(record.receipt.LeaseID, record.taskUserID)
		if err != nil {
			return count, err
		}
		if snap.Deadline.After(now) {
			continue
		}
		if record.cleanupUncertain {
			return count, sandboxoci.ErrCleanupUncertain
		}
		s.fenceAndCancel(snap.LeaseID)
		if record.session != nil {
			if err := record.session.Close(); err != nil {
				return count, err
			}
		}
		if _, err := s.Journal.Cancel(snap.LeaseID, snap.UserID); err != nil {
			return count, err
		}
		if err := record.task.Release(); err != nil {
			return count, err
		}
		delete(s.records, id)
		count++
	}
	return count, nil
}

// NewStagingHandler is deliberately not attached to cmd/sandboxnode or
// AgentAPI. Register it only after admission, crash recovery and resource
// controls are reviewed. It uses bounded signed bodies, no bearer fallback.
func NewStagingHandler(key []byte, service *StagingService, shared ...*sandboxnode.TaskSigner) (http.Handler, error) {
	signer, err := sandboxnode.NewTaskSigner(key, nil)
	if err != nil {
		return nil, err
	}
	if len(shared) > 0 {
		if len(shared) != 1 || shared[0] == nil {
			return nil, errors.New("invalid shared task signer")
		}
		signer = shared[0]
	}
	if service == nil || service.Journal == nil || service.MaxActive != 1 || service.Revision == "" || service.records != nil || sandboxpolicy.ValidateFileOCIProfile(service.Profile) != nil {
		return nil, errors.New("invalid staging service")
	}
	service.records = make(map[string]*stagedRecord)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/tasks/prepare", func(w http.ResponseWriter, r *http.Request) {
		var req StageRequest
		if err := strictStageJSON(r.Body, &req); err != nil {
			http.Error(w, "invalid stage request", http.StatusBadRequest)
			return
		}
		receipt, err := service.prepare(r.Context(), req)
		if err != nil {
			http.Error(w, "stage rejected", http.StatusConflict)
			return
		}
		writeJSON(w, http.StatusOK, receipt)
	})
	for _, operation := range []string{"cancel", "dispose"} {
		op := operation
		mux.HandleFunc("POST /v1/tasks/"+op, func(w http.ResponseWriter, r *http.Request) {
			var control StageControl
			if err := strictStageJSON(r.Body, &control); err != nil {
				http.Error(w, "invalid task control", http.StatusBadRequest)
				return
			}
			if err := service.dispose(control, op == "cancel"); err != nil {
				http.Error(w, "task control rejected", http.StatusConflict)
				return
			}
			writeJSON(w, http.StatusOK, map[string]string{"status": "disposed"})
		})
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != http.MethodPost || (r.URL.Path != "/v1/tasks/prepare" && r.URL.Path != "/v1/tasks/cancel" && r.URL.Path != "/v1/tasks/dispose") || r.URL.RawQuery != "" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Content-Type") != "application/json" {
			http.Error(w, "invalid content type", http.StatusUnsupportedMediaType)
			return
		}
		max := int64(16 << 10)
		if r.URL.Path == "/v1/tasks/prepare" {
			max = MaxStagingBody
		}
		data, err := io.ReadAll(io.LimitReader(r.Body, max+1))
		r.Body.Close()
		if err != nil || int64(len(data)) > max {
			http.Error(w, "task body too large", http.StatusRequestEntityTooLarge)
			return
		}
		if signer.Verify(r, data) != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(data))
		mux.ServeHTTP(w, r)
	}), nil
}
func strictStageJSON(input io.Reader, out any) error {
	dec := json.NewDecoder(input)
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return err
	}
	var more any
	if err := dec.Decode(&more); !errors.Is(err, io.EOF) {
		return errors.New("extra JSON")
	}
	return nil
}

func invalidStageIdentity(value string) bool {
	if value == "" || strings.TrimSpace(value) != value || len(value) > 128 {
		return true
	}
	for _, r := range value {
		if r == 0 || r == 10 || r == 13 {
			return true
		}
	}
	return false
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

// registerActive is used only after the exclusive task record lock is held.
func (s *StagingService) registerActive(id string, cancel context.CancelFunc, deadline time.Time) error {
	s.activeMu.Lock()
	defer s.activeMu.Unlock()
	if s.fenced[id] {
		return sandboxnode.ErrLeaseConflict
	}
	if s.active == nil {
		s.active = map[string]context.CancelFunc{}
	}
	if s.active[id] != nil {
		return sandboxnode.ErrLeaseConflict
	}
	s.active[id] = cancel
	if s.activeDeadlines == nil {
		s.activeDeadlines = map[string]time.Time{}
	}
	s.activeDeadlines[id] = deadline
	return nil
}
func (s *StagingService) unregisterActive(id string) {
	s.activeMu.Lock()
	defer s.activeMu.Unlock()
	delete(s.active, id)
	delete(s.activeDeadlines, id)
}
func (s *StagingService) fenceAndCancel(id string) {
	s.activeMu.Lock()
	defer s.activeMu.Unlock()
	if s.fenced == nil {
		s.fenced = map[string]bool{}
	}
	s.fenced[id] = true
	if stop := s.active[id]; stop != nil {
		stop()
	}
}
func (s *StagingService) isFenced(id string) bool {
	s.activeMu.Lock()
	defer s.activeMu.Unlock()
	return s.fenced[id]
}

// Stop overdue docker exec before waiting on the task lock held by its worker.
func (s *StagingService) cancelExpiredActive(now time.Time) {
	s.activeMu.Lock()
	defer s.activeMu.Unlock()
	if s.fenced == nil {
		s.fenced = map[string]bool{}
	}
	for id, deadline := range s.activeDeadlines {
		if !deadline.After(now) {
			s.fenced[id] = true
			if stop := s.active[id]; stop != nil {
				stop()
			}
		}
	}
}
