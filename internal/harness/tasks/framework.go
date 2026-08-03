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
	registry      *TaskRegistry
	tasks         map[string]TaskState
	mu            sync.RWMutex
	subscribers   map[int]chan string
	nextSubscribe int
}

// cloneTaskState returns an immutable snapshot for callers. TaskManager keeps
// mutable task pointers internally and updates them under m.mu; returning those
// pointers would let status/output readers race with background workers.
func cloneTaskState(task TaskState) TaskState {
	switch typed := task.(type) {
	case *LocalAgentTaskState:
		cp := *typed
		cp.PendingMessages = append([]interface{}(nil), typed.PendingMessages...)
		cp.Messages = append([]interface{}(nil), typed.Messages...)
		cp.Result = cloneAgentTaskResult(typed.Result)
		cp.Progress = cloneAgentProgress(typed.Progress)
		return &cp
	case *InProcessTeammateTaskState:
		cp := *typed
		cp.PendingMessages = append([]interface{}(nil), typed.PendingMessages...)
		cp.Messages = append([]interface{}(nil), typed.Messages...)
		cp.Result = cloneAgentTaskResult(typed.Result)
		cp.Progress = cloneAgentProgress(typed.Progress)
		return &cp
	case *RemoteAgentTaskState:
		cp := *typed
		cp.Messages = append([]interface{}(nil), typed.Messages...)
		return &cp
	case *LocalShellTaskState:
		cp := *typed
		if typed.Result != nil {
			result := *typed.Result
			cp.Result = &result
		}
		return &cp
	case *LocalWorkflowTaskState:
		cp := *typed
		return &cp
	case *MonitorMCPTaskState:
		cp := *typed
		return &cp
	case *DreamTaskState:
		cp := *typed
		return &cp
	default:
		return task
	}
}

func cloneAgentTaskResult(result *AgentTaskResult) *AgentTaskResult {
	if result == nil {
		return nil
	}
	cp := *result
	return &cp
}

func cloneAgentProgress(progress *AgentProgress) *AgentProgress {
	if progress == nil {
		return nil
	}
	cp := *progress
	cp.RecentActivities = make([]ToolActivity, len(progress.RecentActivities))
	for i, activity := range progress.RecentActivities {
		cp.RecentActivities[i] = activity
		if activity.Input != nil {
			cp.RecentActivities[i].Input = make(map[string]interface{}, len(activity.Input))
			for key, value := range activity.Input {
				cp.RecentActivities[i].Input[key] = value
			}
		}
	}
	return &cp
}

// NewTaskManager creates a new task manager
func NewTaskManager() *TaskManager {
	manager := &TaskManager{
		registry:    NewTaskRegistry(),
		tasks:       make(map[string]TaskState),
		subscribers: make(map[int]chan string),
	}
	manager.Register(&LocalAgentTask{manager: manager})
	manager.Register(&InProcessTeammateTask{manager: manager})
	return manager
}

// SubscribeTerminalEvents returns a best-effort stream of task IDs that reached
// a terminal status. Call the returned function to unsubscribe.
func (m *TaskManager) SubscribeTerminalEvents(buffer int) (<-chan string, func()) {
	if buffer < 1 {
		buffer = 1
	}
	ch := make(chan string, buffer)
	m.mu.Lock()
	id := m.nextSubscribe
	m.nextSubscribe++
	m.subscribers[id] = ch
	m.mu.Unlock()
	return ch, func() {
		m.mu.Lock()
		if _, ok := m.subscribers[id]; ok {
			delete(m.subscribers, id)
		}
		m.mu.Unlock()
	}
}

func (m *TaskManager) emitTerminalEvent(taskID string) {
	if taskID == "" {
		return
	}
	m.mu.RLock()
	subscribers := make([]chan string, 0, len(m.subscribers))
	for _, ch := range m.subscribers {
		subscribers = append(subscribers, ch)
	}
	m.mu.RUnlock()
	for _, ch := range subscribers {
		select {
		case ch <- taskID:
		default:
		}
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
	if !ok {
		return nil, false
	}
	return cloneTaskState(task), true
}

// AddTask adds a task to the manager
func (m *TaskManager) AddTask(task TaskState) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tasks[task.GetID()] = task
}

// UpdateTask updates a task atomically and stores the returned state.
func (m *TaskManager) UpdateTask(taskID string, updater func(TaskState) (TaskState, error)) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	task, ok := m.tasks[taskID]
	if !ok {
		return fmt.Errorf("task not found: %s", taskID)
	}
	next, err := updater(task)
	if err != nil {
		return err
	}
	if next == nil {
		delete(m.tasks, taskID)
		return nil
	}
	m.tasks[taskID] = next
	return nil
}

// ListTasks returns a snapshot of all tracked runtime tasks.
func (m *TaskManager) ListTasks() []TaskState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	tasks := make([]TaskState, 0, len(m.tasks))
	for _, task := range m.tasks {
		tasks = append(tasks, cloneTaskState(task))
	}
	return tasks
}

// DrainTerminalNotifications returns terminal runtime tasks that have not yet
// emitted a coordinator notification and marks them as notified.
func (m *TaskManager) DrainTerminalNotifications() []TaskState {
	m.mu.Lock()
	defer m.mu.Unlock()
	var drained []TaskState
	for _, task := range m.tasks {
		if !IsTerminalTaskStatus(task.GetStatus()) || taskNotified(task) {
			continue
		}
		setTaskNotified(task, true)
		drained = append(drained, cloneTaskState(task))
	}
	return drained
}

// DrainTerminalNotification returns one terminal runtime task by ID if it has
// not yet emitted a coordinator notification, then marks it as notified.
func (m *TaskManager) DrainTerminalNotification(taskID string) (TaskState, bool) {
	if taskID == "" {
		return nil, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	task, ok := m.tasks[taskID]
	if !ok || !IsTerminalTaskStatus(task.GetStatus()) || taskNotified(task) {
		return nil, false
	}
	setTaskNotified(task, true)
	return cloneTaskState(task), true
}

func taskNotified(task TaskState) bool {
	switch typed := task.(type) {
	case *LocalAgentTaskState:
		return typed.Notified
	case *InProcessTeammateTaskState:
		return typed.Notified
	case *RemoteAgentTaskState:
		return typed.Notified
	case *LocalShellTaskState:
		return typed.Notified
	case *LocalWorkflowTaskState:
		return typed.Notified
	case *MonitorMCPTaskState:
		return typed.Notified
	case *DreamTaskState:
		return typed.Notified
	default:
		return false
	}
}

func setTaskNotified(task TaskState, value bool) {
	switch typed := task.(type) {
	case *LocalAgentTaskState:
		typed.Notified = value
	case *InProcessTeammateTaskState:
		typed.Notified = value
	case *RemoteAgentTaskState:
		typed.Notified = value
	case *LocalShellTaskState:
		typed.Notified = value
	case *LocalWorkflowTaskState:
		typed.Notified = value
	case *MonitorMCPTaskState:
		typed.Notified = value
	case *DreamTaskState:
		typed.Notified = value
	}
}

// FindInProcessTeammate returns a running teammate task by task ID, teammate
// ID, name, or name@team identifier.
func (m *TaskManager) FindInProcessTeammate(nameOrID string) (*InProcessTeammateTaskState, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, task := range m.tasks {
		teammate, ok := task.(*InProcessTeammateTaskState)
		if !ok {
			continue
		}
		if teammate.ID == nameOrID || teammate.TeammateID == nameOrID || teammate.Name == nameOrID {
			return cloneTaskState(teammate).(*InProcessTeammateTaskState), true
		}
		if teammate.TeamName != "" && teammate.Name+"@"+teammate.TeamName == nameOrID {
			return cloneTaskState(teammate).(*InProcessTeammateTaskState), true
		}
	}
	return nil, false
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
			running = append(running, cloneTaskState(task))
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
			background = append(background, cloneTaskState(task))
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

var defaultManager = NewTaskManager()

// DefaultManager returns the process-local runtime task manager used by
// harness tools for background agent tasks.
func DefaultManager() *TaskManager {
	return defaultManager
}
