package sandboxstaging

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"

	"claude-codex/internal/backend/sandboxnode"
	"claude-codex/internal/backend/sandboxoci"
)

// DevelopmentRecovery is an explicitly invoked startup gate, not an HTTP
// endpoint. It holds the Node-root process lock for the caller's lifetime.
// No execution is admitted if Docker inventory, cleanup or journal writes fail.
type DevelopmentRecovery struct {
	mu      sync.RWMutex
	lock    *sandboxnode.NodeLock
	Journal *sandboxnode.FileJournal
}

// hold prevents shutdown from releasing the process fence during a command.
func (r *DevelopmentRecovery) hold() (func(), error) {
	if r == nil {
		return nil, errors.New("development Node is not recovered")
	}
	r.mu.RLock()
	if r.lock == nil {
		r.mu.RUnlock()
		return nil, errors.New("development Node lock released")
	}
	return r.mu.RUnlock, nil
}
func (r *DevelopmentRecovery) alive() bool {
	if r == nil {
		return false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.lock != nil
}

func (r *DevelopmentRecovery) Close() error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.lock == nil {
		return nil
	}
	lock := r.lock
	r.lock = nil
	return lock.Close()
}
func ReconcileDevelopmentStartup(ctx context.Context, stager sandboxnode.Stager, journalPath string, runner sandboxoci.CommandRunner) (*DevelopmentRecovery, error) {
	if ctx == nil || runner == nil || journalPath == "" || !filepath.IsAbs(journalPath) ||
		stager.Root == "" || filepath.Clean(journalPath) == filepath.Join(stager.Root, ".node.lock") {
		return nil, errors.New("invalid Node recovery configuration")
	}
	rel, err := filepath.Rel(stager.Root, journalPath)
	if err != nil || rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))) {
		return nil, errors.New("journal must be outside the disposable task root")
	}
	lock, err := sandboxnode.AcquireNodeLock(stager.Root)
	if err != nil {
		return nil, err
	}
	fail := func(err error) (*DevelopmentRecovery, error) { _ = lock.Close(); return nil, err }
	journal, err := sandboxnode.OpenFileJournal(journalPath, 4096, 64, nil)
	if err != nil {
		return fail(err)
	}
	if err := sandboxoci.ReconcileDevelopmentContainers(ctx, runner); err != nil {
		return fail(err)
	}
	if _, err := stager.RemoveOrphanedInputs(); err != nil {
		return fail(err)
	}
	if _, err := journal.InvalidatePrepared(); err != nil {
		return fail(err)
	}
	return &DevelopmentRecovery{lock: lock, Journal: journal}, nil
}
