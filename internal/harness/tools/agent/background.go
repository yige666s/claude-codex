package agent

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type BackgroundStatus string

const (
	BackgroundRunning   BackgroundStatus = "running"
	BackgroundCompleted BackgroundStatus = "completed"
	BackgroundFailed    BackgroundStatus = "failed"
	BackgroundKilled    BackgroundStatus = "killed"
)

type BackgroundTask struct {
	ID          string
	Request     Request
	Status      BackgroundStatus
	Output      string
	Error       string
	StartedAt   time.Time
	CompletedAt *time.Time
	cancel      context.CancelFunc
}

type BackgroundManager struct {
	mu     sync.RWMutex
	tasks  map[string]*BackgroundTask
	nextID func() string
}

func NewBackgroundManager() *BackgroundManager {
	return &BackgroundManager{
		tasks: make(map[string]*BackgroundTask),
		nextID: func() string {
			return fmt.Sprintf("agent-%d", time.Now().UnixNano())
		},
	}
}

func (m *BackgroundManager) Start(parent context.Context, req Request, run Runner) (*BackgroundTask, error) {
	if m == nil {
		return nil, fmt.Errorf("background manager is not configured")
	}
	if run == nil {
		return nil, fmt.Errorf("agent runner is not configured")
	}
	ctx, cancel := context.WithCancel(context.WithoutCancel(parent))
	task := &BackgroundTask{
		ID:        m.nextID(),
		Request:   req,
		Status:    BackgroundRunning,
		StartedAt: time.Now(),
		cancel:    cancel,
	}
	m.mu.Lock()
	m.tasks[task.ID] = task
	m.mu.Unlock()

	go func() {
		output, err := run(ctx, req)
		now := time.Now()
		m.mu.Lock()
		defer m.mu.Unlock()
		task.CompletedAt = &now
		switch {
		case err == nil:
			task.Status = BackgroundCompleted
			task.Output = output
		case ctx.Err() != nil:
			task.Status = BackgroundKilled
			task.Error = ctx.Err().Error()
		default:
			task.Status = BackgroundFailed
			task.Error = err.Error()
		}
	}()

	return task, nil
}

func (m *BackgroundManager) Get(id string) (*BackgroundTask, bool) {
	if m == nil {
		return nil, false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	task, ok := m.tasks[id]
	if !ok {
		return nil, false
	}
	cp := *task
	cp.cancel = nil
	return &cp, true
}

func (m *BackgroundManager) List() []BackgroundTask {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]BackgroundTask, 0, len(m.tasks))
	for _, task := range m.tasks {
		cp := *task
		cp.cancel = nil
		out = append(out, cp)
	}
	return out
}

func (m *BackgroundManager) Kill(id string) bool {
	if m == nil {
		return false
	}
	m.mu.RLock()
	task, ok := m.tasks[id]
	m.mu.RUnlock()
	if !ok || task.cancel == nil {
		return false
	}
	task.cancel()
	return true
}

var defaultBackgroundManager = NewBackgroundManager()

func GetBackgroundTask(id string) (*BackgroundTask, bool) {
	return defaultBackgroundManager.Get(id)
}

func ListBackgroundTasks() []BackgroundTask {
	return defaultBackgroundManager.List()
}
