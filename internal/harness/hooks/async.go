package hooks

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// AsyncHookManager manages asynchronous hook execution.
type AsyncHookManager struct {
	pending map[string]*AsyncHook
	mu      sync.RWMutex
}

// AsyncHook represents an asynchronously executing hook.
type AsyncHook struct {
	ID        string
	Hook      Hook
	Input     *HookInput
	StartedAt time.Time
	Done      chan *HookResult
	Result    *HookResult
	Error     error
	cancel    context.CancelFunc
}

// NewAsyncHookManager creates a new async hook manager.
func NewAsyncHookManager() *AsyncHookManager {
	return &AsyncHookManager{
		pending: make(map[string]*AsyncHook),
	}
}

// Start starts an async hook execution.
func (m *AsyncHookManager) Start(ctx context.Context, hook Hook, input *HookInput) (string, error) {
	id := fmt.Sprintf("%s-%d", hook.Name(), time.Now().UnixNano())

	execCtx, cancel := context.WithCancel(ctx)
	asyncHook := &AsyncHook{
		ID:        id,
		Hook:      hook,
		Input:     input,
		StartedAt: time.Now(),
		Done:      make(chan *HookResult, 1),
		cancel:    cancel,
	}

	m.mu.Lock()
	m.pending[id] = asyncHook
	m.mu.Unlock()

	// Start execution in background
	go m.execute(execCtx, asyncHook)

	return id, nil
}

// execute runs the hook in background.
func (m *AsyncHookManager) execute(ctx context.Context, asyncHook *AsyncHook) {
	defer func() {
		if r := recover(); r != nil {
			err := fmt.Errorf("hook panicked: %v", r)
			result := &HookResult{
				Continue:      true,
				BlockingError: err.Error(),
			}
			m.setOutcome(asyncHook, result, err)
			asyncHook.Done <- result
		}
		close(asyncHook.Done)
	}()

	// Use hook's timeout if specified
	timeout := asyncHook.Hook.Timeout()
	if timeout == 0 {
		timeout = DefaultTimeout
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	result, err := asyncHook.Hook.Execute(ctx, asyncHook.Input)
	m.setOutcome(asyncHook, result, err)

	if result != nil {
		asyncHook.Done <- result
	}
}

func (m *AsyncHookManager) setOutcome(asyncHook *AsyncHook, result *HookResult, err error) {
	m.mu.Lock()
	asyncHook.Result = result
	asyncHook.Error = err
	m.mu.Unlock()
}

// Wait waits for an async hook to complete.
func (m *AsyncHookManager) Wait(id string, timeout time.Duration) (*HookResult, error) {
	m.mu.RLock()
	asyncHook, exists := m.pending[id]
	m.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("async hook %s not found", id)
	}

	select {
	case result := <-asyncHook.Done:
		m.mu.Lock()
		err := asyncHook.Error
		delete(m.pending, id)
		m.mu.Unlock()
		return result, err
	case <-time.After(timeout):
		return nil, fmt.Errorf("timeout waiting for async hook %s", id)
	}
}

// GetStatus returns the status of an async hook.
func (m *AsyncHookManager) GetStatus(id string) (*AsyncHookStatus, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	asyncHook, exists := m.pending[id]
	if !exists {
		return nil, fmt.Errorf("async hook %s not found", id)
	}

	status := &AsyncHookStatus{
		ID:        asyncHook.ID,
		HookName:  asyncHook.Hook.Name(),
		StartedAt: asyncHook.StartedAt,
		Duration:  time.Since(asyncHook.StartedAt),
		Completed: asyncHook.Result != nil,
	}

	return status, nil
}

// ListPending returns all pending async hooks.
func (m *AsyncHookManager) ListPending() []*AsyncHookStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	statuses := make([]*AsyncHookStatus, 0, len(m.pending))
	for _, asyncHook := range m.pending {
		statuses = append(statuses, &AsyncHookStatus{
			ID:        asyncHook.ID,
			HookName:  asyncHook.Hook.Name(),
			StartedAt: asyncHook.StartedAt,
			Duration:  time.Since(asyncHook.StartedAt),
			Completed: asyncHook.Result != nil,
		})
	}

	return statuses
}

// Cancel cancels an async hook execution.
func (m *AsyncHookManager) Cancel(id string) error {
	m.mu.Lock()
	asyncHook, exists := m.pending[id]
	if !exists {
		m.mu.Unlock()
		return fmt.Errorf("async hook %s not found", id)
	}
	delete(m.pending, id)
	m.mu.Unlock()
	if asyncHook.cancel != nil {
		asyncHook.cancel()
	}
	return nil
}

// Cleanup removes completed async hooks older than the specified duration.
func (m *AsyncHookManager) Cleanup(maxAge time.Duration) int {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	removed := 0

	for id, asyncHook := range m.pending {
		if asyncHook.Result != nil && now.Sub(asyncHook.StartedAt) > maxAge {
			delete(m.pending, id)
			removed++
		}
	}

	return removed
}

// Count returns the number of pending async hooks.
func (m *AsyncHookManager) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.pending)
}

// AsyncHookStatus represents the status of an async hook.
type AsyncHookStatus struct {
	ID        string
	HookName  string
	StartedAt time.Time
	Duration  time.Duration
	Completed bool
}
