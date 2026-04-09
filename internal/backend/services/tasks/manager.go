package tasks

import (
	"context"
	"sync"
)

// Manager manages task lifecycle and state.
type Manager struct {
	mu     sync.RWMutex
	tasks  map[string]*TaskState
	cancel map[string]context.CancelFunc
}

// NewManager creates a new task manager.
func NewManager() *Manager {
	return &Manager{
		tasks:  make(map[string]*TaskState),
		cancel: make(map[string]context.CancelFunc),
	}
}

// AddTask adds a new task to the manager.
func (m *Manager) AddTask(task *TaskState, cancelFunc context.CancelFunc) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tasks[task.ID] = task
	if cancelFunc != nil {
		m.cancel[task.ID] = cancelFunc
	}
}

// GetTask retrieves a task by ID.
func (m *Manager) GetTask(id string) (*TaskState, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	task, exists := m.tasks[id]
	return task, exists
}

// UpdateTaskStatus updates the status of a task.
func (m *Manager) UpdateTaskStatus(id string, status TaskStatus) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	task, exists := m.tasks[id]
	if !exists {
		return ErrTaskNotFound
	}

	task.Status = status
	if status.IsTerminal() && task.EndTime == 0 {
		task.EndTime = task.StartTime + (task.TotalPausedMs / 1000)
	}
	return nil
}

// KillTask cancels a running task.
func (m *Manager) KillTask(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	task, exists := m.tasks[id]
	if !exists {
		return ErrTaskNotFound
	}

	if task.Status.IsTerminal() {
		return ErrTaskAlreadyTerminated
	}

	// Cancel the task context
	if cancelFunc, exists := m.cancel[id]; exists {
		cancelFunc()
		delete(m.cancel, id)
	}

	task.Status = TaskStatusKilled
	return nil
}

// RemoveTask removes a task from the manager.
func (m *Manager) RemoveTask(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Cancel if still running
	if cancelFunc, exists := m.cancel[id]; exists {
		cancelFunc()
		delete(m.cancel, id)
	}

	delete(m.tasks, id)
}

// ListTasks returns all tasks.
func (m *Manager) ListTasks() []*TaskState {
	m.mu.RLock()
	defer m.mu.RUnlock()

	tasks := make([]*TaskState, 0, len(m.tasks))
	for _, task := range m.tasks {
		tasks = append(tasks, task)
	}
	return tasks
}

// ListTasksByType returns tasks of a specific type.
func (m *Manager) ListTasksByType(taskType TaskType) []*TaskState {
	m.mu.RLock()
	defer m.mu.RUnlock()

	tasks := make([]*TaskState, 0)
	for _, task := range m.tasks {
		if task.Type == taskType {
			tasks = append(tasks, task)
		}
	}
	return tasks
}

// ListTasksByStatus returns tasks with a specific status.
func (m *Manager) ListTasksByStatus(status TaskStatus) []*TaskState {
	m.mu.RLock()
	defer m.mu.RUnlock()

	tasks := make([]*TaskState, 0)
	for _, task := range m.tasks {
		if task.Status == status {
			tasks = append(tasks, task)
		}
	}
	return tasks
}

// CleanupTerminatedTasks removes all terminated tasks.
func (m *Manager) CleanupTerminatedTasks() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	count := 0
	for id, task := range m.tasks {
		if task.Status.IsTerminal() {
			delete(m.tasks, id)
			delete(m.cancel, id)
			count++
		}
	}
	return count
}

// SetNotified marks a task as notified.
func (m *Manager) SetNotified(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	task, exists := m.tasks[id]
	if !exists {
		return ErrTaskNotFound
	}

	task.Notified = true
	return nil
}

// UpdateOutputOffset updates the output file offset for a task.
func (m *Manager) UpdateOutputOffset(id string, offset int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	task, exists := m.tasks[id]
	if !exists {
		return ErrTaskNotFound
	}

	task.OutputOffset = offset
	return nil
}
