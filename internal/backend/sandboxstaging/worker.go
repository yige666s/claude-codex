package sandboxstaging

import (
	"context"
	"crypto/sha256"
	"errors"
	"time"

	"claude-codex/internal/backend/sandboxnode"
	"claude-codex/internal/backend/sandboxoci"
	"claude-codex/internal/backend/sandboxtransfer"
)

// DevelopmentWorker binds the already-durable lease to one OCI session. It
// has no HTTP route and cannot run unless startup reconciliation holds the
// exclusive Node root lock. It does not make native trust decisions.
type DevelopmentWorker struct {
	Recovery *DevelopmentRecovery
	Service  *StagingService
	Driver   sandboxoci.SessionDriver
}
type executedCommand struct {
	digest    [32]byte
	result    sandboxoci.Result
	completed bool
}

func (w *DevelopmentWorker) ready() error {
	if w == nil || w.Recovery == nil || w.Service == nil || w.Service.Journal == nil || w.Service.Journal != w.Recovery.Journal || w.Driver.Runner == nil || w.Service.Stager.Cache == nil ||
		w.Driver.Planner.SkillCacheRoot != w.Service.Stager.Cache.Dir || w.Driver.Planner.TaskRoot != w.Service.Stager.Root {
		return errors.New("development worker requires recovered exclusive Node")
	}
	return nil
}
func (w *DevelopmentWorker) Execute(ctx context.Context, control StageControl, executionID string, script []byte) (sandboxoci.Result, error) {
	if w == nil || w.Recovery == nil {
		return sandboxoci.Result{}, errors.New("development worker not recovered")
	}
	unlock, holdErr := w.Recovery.hold()
	if holdErr != nil {
		return sandboxoci.Result{}, holdErr
	}
	defer unlock()
	if err := w.ready(); err != nil {
		return sandboxoci.Result{}, err
	}
	if ctx == nil || ctx.Err() != nil || invalidStageIdentity(executionID) || len(executionID) != 32 || len(script) == 0 || len(script) > 64<<10 {
		return sandboxoci.Result{}, errors.New("invalid development command")
	}
	w.Service.mu.Lock()
	defer w.Service.mu.Unlock()
	snap, record, err := w.find(control)
	if err != nil {
		return sandboxoci.Result{}, err
	}
	if record.collected {
		return sandboxoci.Result{}, errors.New("development task already collected")
	}
	running, stop := context.WithCancel(ctx)
	if err := w.Service.registerActive(control.LeaseID, stop, snap.Deadline); err != nil {
		stop()
		return sandboxoci.Result{}, err
	}
	defer func() { w.Service.unregisterActive(control.LeaseID); stop() }()
	if record.executions == nil {
		record.executions = make(map[string]executedCommand)
	}
	digest := sha256.Sum256(script)
	if prior, exists := record.executions[executionID]; exists {
		if prior.digest != digest {
			return sandboxoci.Result{}, sandboxnode.ErrLeaseConflict
		}
		if !prior.completed {
			return sandboxoci.Result{}, sandboxnode.ErrLeaseConflict
		}
		return prior.result, nil
	}
	if len(record.executions) >= 64 {
		return sandboxoci.Result{}, errors.New("development command limit reached")
	}
	// A command may have side effects. Fencing its execution ID must be
	// durably recorded before docker exec. On lost replies, the old ID is
	// rejected rather than replayed; a restarted process cancels the lease.
	if _, err := w.Service.Journal.AppendEventForUser(control.LeaseID, control.UserID, "status", "execution claimed "+executionID); err != nil {
		return sandboxoci.Result{}, err
	}
	record.executions[executionID] = executedCommand{digest: digest}
	if record.session == nil {
		record.session, err = w.Driver.StartLease(running, w.Service.Profile, record.task.SkillPath, record.task.WorkspacePath, control.LeaseID)
		if err != nil {
			record.failed = true
			record.cleanupUncertain = errors.Is(err, sandboxoci.ErrCleanupUncertain)
			return sandboxoci.Result{}, err
		}
	}
	limited, cancel := context.WithDeadline(running, snap.Deadline)
	defer cancel()
	result, err := record.session.Run(limited, script)
	if err != nil {
		record.failed = true
		return sandboxoci.Result{}, err
	}
	record.executions[executionID] = executedCommand{digest: digest, result: result, completed: true}
	return result, nil
}
func (w *DevelopmentWorker) Collect(ctx context.Context, control StageControl) ([]sandboxtransfer.OutputFile, error) {
	if w == nil || w.Recovery == nil {
		return nil, errors.New("development worker not recovered")
	}
	unlock, holdErr := w.Recovery.hold()
	if holdErr != nil {
		return nil, holdErr
	}
	defer unlock()
	if err := w.ready(); err != nil {
		return nil, err
	}
	if ctx == nil || ctx.Err() != nil {
		return nil, errors.New("invalid collection context")
	}
	w.Service.mu.Lock()
	defer w.Service.mu.Unlock()
	snap, record, err := w.find(control)
	if err != nil {
		return nil, err
	}
	if record.collected {
		return cloneOutputFiles(record.outputs), nil
	}
	running, stop := context.WithCancel(ctx)
	if err := w.Service.registerActive(control.LeaseID, stop, snap.Deadline); err != nil {
		stop()
		return nil, err
	}
	defer func() { w.Service.unregisterActive(control.LeaseID); stop() }()
	if record.session == nil {
		record.outputs = []sandboxtransfer.OutputFile{}
		record.collected = true
		return []sandboxtransfer.OutputFile{}, nil
	}
	limited, cancel := context.WithDeadline(running, snap.Deadline)
	defer cancel()
	files, err := record.session.Collect(limited)
	if err != nil {
		record.failed = true
		return nil, err
	}
	record.outputs = cloneOutputFiles(files)
	record.collected = true
	return cloneOutputFiles(record.outputs), nil
}
func (w *DevelopmentWorker) find(control StageControl) (sandboxnode.LeaseSnapshot, *stagedRecord, error) {
	if invalidStageIdentity(control.LeaseID) || invalidStageIdentity(control.UserID) {
		return sandboxnode.LeaseSnapshot{}, nil, sandboxnode.ErrLeaseMissing
	}
	snap, err := w.Service.Journal.Snapshot(control.LeaseID, control.UserID)
	if err != nil || snap.State != sandboxnode.LeasePrepared || !time.Now().Before(snap.Deadline) {
		return sandboxnode.LeaseSnapshot{}, nil, sandboxnode.ErrLeaseMissing
	}
	record, ok := w.Service.records[snap.JobID]
	if !ok || record.failed || w.Service.isFenced(control.LeaseID) || record.receipt.LeaseID != control.LeaseID || record.taskUserID != control.UserID {
		return sandboxnode.LeaseSnapshot{}, nil, sandboxnode.ErrLeaseMissing
	}
	return snap, record, nil
}

func cloneOutputFiles(files []sandboxtransfer.OutputFile) []sandboxtransfer.OutputFile {
	out := make([]sandboxtransfer.OutputFile, len(files))
	for i, file := range files {
		out[i] = file
		out[i].Content = append([]byte(nil), file.Content...)
	}
	return out
}
