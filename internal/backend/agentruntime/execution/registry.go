package execution

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"sync"
)

// ErrSessionTurnRunning is returned when a session already has an active turn.
var ErrSessionTurnRunning = errors.New("session turn is already running")

// ErrRuntimeShuttingDown is returned when new work is rejected during shutdown.
var ErrRuntimeShuttingDown = errors.New("runtime is shutting down")

// Registry owns the in-process execution lifecycle for sessions, jobs, and
// background work. Keeping this state outside Runtime prevents request
// orchestration from also owning synchronization and shutdown bookkeeping.
type Registry struct {
	mu              sync.Mutex
	wg              sync.WaitGroup
	running         map[string]context.CancelFunc
	runningJobTurns map[string]bool
	runningJobs     map[string]context.CancelFunc
	shuttingDown    bool
}

// NewRegistry creates an empty execution registry.
func NewRegistry() *Registry {
	return &Registry{
		running:         make(map[string]context.CancelFunc),
		runningJobTurns: make(map[string]bool),
		runningJobs:     make(map[string]context.CancelFunc),
	}
}

// Start registers a session turn and accounts for it in shutdown waiting.
func (r *Registry) Start(key string, cancel context.CancelFunc, jobScoped bool) error {
	if r == nil {
		return ErrRuntimeShuttingDown
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.shuttingDown {
		return ErrRuntimeShuttingDown
	}
	if _, running := r.running[key]; running {
		return ErrSessionTurnRunning
	}
	r.running[key] = cancel
	if jobScoped {
		r.runningJobTurns[key] = true
	}
	r.wg.Add(1)
	return nil
}

// Finish unregisters a session turn.
func (r *Registry) Finish(key string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	delete(r.running, key)
	delete(r.runningJobTurns, key)
	r.mu.Unlock()
	r.wg.Done()
}

// StartJob registers a durable job execution.
func (r *Registry) StartJob(jobID string, cancel context.CancelFunc) error {
	if r == nil {
		return ErrRuntimeShuttingDown
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.shuttingDown {
		return ErrRuntimeShuttingDown
	}
	r.runningJobs[jobID] = cancel
	r.wg.Add(1)
	return nil
}

// FinishJob unregisters a durable job execution.
func (r *Registry) FinishJob(jobID string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	delete(r.runningJobs, jobID)
	r.mu.Unlock()
	r.wg.Done()
}

// AcquireBackground reserves a shutdown wait slot for a short-lived
// background task. It returns false once shutdown has started.
func (r *Registry) AcquireBackground() bool {
	if r == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.shuttingDown {
		return false
	}
	r.wg.Add(1)
	return true
}

// DoneBackground releases a slot acquired with AcquireBackground.
func (r *Registry) DoneBackground() {
	if r == nil {
		return
	}
	r.wg.Done()
}

// BeginShutdown marks the registry as shutting down and returns the cancels
// that should be invoked by the caller. Job-scoped turns remain alive when a
// durable job queue is present so they can finish through the queue protocol.
func (r *Registry) BeginShutdown(durableJobQueuePresent bool) []context.CancelFunc {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	r.shuttingDown = true
	cancels := make([]context.CancelFunc, 0, len(r.running)+len(r.runningJobs))
	for key, cancel := range r.running {
		if durableJobQueuePresent && r.runningJobTurns[key] {
			continue
		}
		if cancel != nil {
			cancels = append(cancels, cancel)
		}
	}
	if !durableJobQueuePresent {
		for _, cancel := range r.runningJobs {
			if cancel != nil {
				cancels = append(cancels, cancel)
			}
		}
	}
	r.mu.Unlock()
	return cancels
}

// IsShuttingDown reports whether the registry has entered shutdown.
func (r *Registry) IsShuttingDown() bool {
	if r == nil {
		return true
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.shuttingDown
}

// Wait blocks until all registered work finishes or ctx is cancelled.
func (r *Registry) Wait(ctx context.Context) error {
	if r == nil {
		return nil
	}
	done := make(chan struct{})
	go func() {
		r.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// RunningSessionIDs returns the session IDs represented by the runtime's
// encoded user/session keys.
func (r *Registry) RunningSessionIDs(userID string) []string {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0)
	for key := range r.running {
		var parts []string
		if err := json.Unmarshal([]byte(key), &parts); err == nil && len(parts) == 2 && parts[0] == userID {
			out = append(out, parts[1])
		}
	}
	sort.Strings(out)
	return out
}

// CancelJob invokes the cancellation function of an active job, if present.
func (r *Registry) CancelJob(jobID string) bool {
	if r == nil {
		return false
	}
	r.mu.Lock()
	cancel, ok := r.runningJobs[jobID]
	r.mu.Unlock()
	if ok && cancel != nil {
		cancel()
	}
	return ok
}

// CancelSession invokes the cancellation function of an active session turn.
func (r *Registry) CancelSession(key string) bool {
	if r == nil {
		return false
	}
	r.mu.Lock()
	cancel, ok := r.running[key]
	r.mu.Unlock()
	if ok && cancel != nil {
		cancel()
	}
	return ok
}

// CancelAll returns every currently registered cancel function. It is useful
// to callers that need a forceful shutdown policy.
func (r *Registry) CancelAll() []context.CancelFunc {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]context.CancelFunc, 0, len(r.running)+len(r.runningJobs))
	for _, cancel := range r.running {
		if cancel != nil {
			out = append(out, cancel)
		}
	}
	for _, cancel := range r.runningJobs {
		if cancel != nil {
			out = append(out, cancel)
		}
	}
	return out
}

// KeyIsForSession is kept small and dependency-free so callers can build
// compatible keys without exposing Registry internals.
func KeyIsForSession(key, userID, sessionID string) bool {
	var parts []string
	if err := json.Unmarshal([]byte(strings.TrimSpace(key)), &parts); err != nil {
		return false
	}
	return len(parts) == 2 && parts[0] == userID && parts[1] == sessionID
}
