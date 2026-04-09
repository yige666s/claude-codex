package tasks

import (
	"fmt"
	"sync"
	"time"
)

const (
	// PollIntervalMs is the standard polling interval for all tasks
	PollIntervalMs = 1000

	// StoppedDisplayMs is the duration to display killed tasks before eviction
	StoppedDisplayMs = 3000

	// PanelGraceMs is the grace period for terminal local_agent tasks in the coordinator panel
	PanelGraceMs = 30000
)

// TaskAttachment represents a task status update attachment
type TaskAttachment struct {
	Type         string     `json:"type"` // "task_status"
	TaskID       string     `json:"taskId"`
	ToolUseID    string     `json:"toolUseId,omitempty"`
	TaskType     TaskType   `json:"taskType"`
	Status       TaskStatus `json:"status"`
	Description  string     `json:"description"`
	DeltaSummary *string    `json:"deltaSummary"` // New output since last attachment
}

// SetAppState is a function type for updating app state
type SetAppState func(updater func(prev interface{}) interface{})

// UpdateTaskState updates a task's state in AppState
func UpdateTaskState(taskID string, setAppState SetAppState, updater func(task TaskState) TaskState) {
	setAppState(func(prev interface{}) interface{} {
		// Type assertion to get tasks map
		// This is a simplified version - actual implementation would need proper state management
		return prev
	})
}

// RegisterTask registers a new task in AppState
func RegisterTask(task TaskState, setAppState SetAppState) {
	setAppState(func(prev interface{}) interface{} {
		// Type assertion and task registration logic
		// This is a simplified version - actual implementation would need proper state management
		return prev
	})
}

// EvictTerminalTask eagerly evicts a terminal task from AppState
func EvictTerminalTask(taskID string, setAppState SetAppState) {
	setAppState(func(prev interface{}) interface{} {
		// Type assertion and eviction logic
		// This is a simplified version - actual implementation would need proper state management
		return prev
	})
}

// CreateTaskStateBase creates a base task state
func CreateTaskStateBase(id string, taskType TaskType, description string, toolUseID string, outputPath string) TaskStateBase {
	return TaskStateBase{
		ID:           id,
		Type:         taskType,
		Status:       TaskStatusPending,
		Description:  description,
		ToolUseID:    toolUseID,
		StartTime:    time.Now().UnixMilli(),
		OutputFile:   outputPath,
		OutputOffset: 0,
		Notified:     false,
	}
}

// TaskManager manages task lifecycle and state
type TaskManager struct {
	registry *TaskRegistry
	tasks    map[string]TaskState
	mu       sync.RWMutex
}

// NewTaskManager creates a new task manager
func NewTaskManager() *TaskManager {
	return &TaskManager{
		registry: NewTaskRegistry(),
		tasks:    make(map[string]TaskState),
	}
}

// Register registers a task implementation
func (m *TaskManager) Register(task Task) {
	m.registry.Register(task)
}

// GetTask retrieves a task by ID
func (m *TaskManager) GetTask(taskID string) (TaskState, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	task, ok := m.tasks[taskID]
	return task, ok
}

// AddTask adds a task to the manager
func (m *TaskManager) AddTask(task TaskState) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tasks[task.GetID()] = task
}

// RemoveTask removes a task from the manager
func (m *TaskManager) RemoveTask(taskID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.tasks, taskID)
}

// GetRunningTasks returns all running tasks
func (m *TaskManager) GetRunningTasks() []TaskState {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var running []TaskState
	for _, task := range m.tasks {
		if task.GetStatus() == TaskStatusRunning {
			running = append(running, task)
		}
	}
	return running
}

// GetBackgroundTasks returns all background tasks
func (m *TaskManager) GetBackgroundTasks() []TaskState {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var background []TaskState
	for _, task := range m.tasks {
		if IsBackgroundTask(task) {
			background = append(background, task)
		}
	}
	return background
}

// KillTask kills a task by ID
func (m *TaskManager) KillTask(taskID string, setAppState SetAppState) error {
	task, ok := m.GetTask(taskID)
	if !ok {
		return fmt.Errorf("task not found: %s", taskID)
	}

	taskImpl, ok := m.registry.Get(task.GetType())
	if !ok {
		return fmt.Errorf("no implementation for task type: %s", task.GetType())
	}

	return taskImpl.Kill(taskID, setAppState)
}

// EvictTerminalTasks removes all terminal tasks that have been notified
func (m *TaskManager) EvictTerminalTasks() {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now().UnixMilli()
	for id, task := range m.tasks {
		if !IsTerminalTaskStatus(task.GetStatus()) {
			continue
		}

		// Check if task has been notified (simplified - actual implementation would check state)
		// Check grace period for agent tasks
		if agentTask, ok := task.(*LocalAgentTaskState); ok {
			if agentTask.EvictAfter != nil && *agentTask.EvictAfter > now {
				continue
			}
		}

		delete(m.tasks, id)
	}
}
