package tasks

import (
	"context"
	"sync"
	"time"
)

// TaskType represents the type of task
type TaskType string

const (
	TaskTypeLocalBash          TaskType = "local_bash"
	TaskTypeLocalAgent         TaskType = "local_agent"
	TaskTypeRemoteAgent        TaskType = "remote_agent"
	TaskTypeInProcessTeammate  TaskType = "in_process_teammate"
	TaskTypeLocalWorkflow      TaskType = "local_workflow"
	TaskTypeMonitorMCP         TaskType = "monitor_mcp"
	TaskTypeDream              TaskType = "dream"
)

// TaskStatus represents the status of a task
type TaskStatus string

const (
	TaskStatusPending   TaskStatus = "pending"
	TaskStatusRunning   TaskStatus = "running"
	TaskStatusCompleted TaskStatus = "completed"
	TaskStatusFailed    TaskStatus = "failed"
	TaskStatusKilled    TaskStatus = "killed"
)

// IsTerminalTaskStatus returns true if the status is terminal
func IsTerminalTaskStatus(status TaskStatus) bool {
	return status == TaskStatusCompleted || status == TaskStatusFailed || status == TaskStatusKilled
}

// TaskStateBase contains fields shared by all task states
type TaskStateBase struct {
	ID             string     `json:"id"`
	Type           TaskType   `json:"type"`
	Status         TaskStatus `json:"status"`
	Description    string     `json:"description"`
	ToolUseID      string     `json:"toolUseId,omitempty"`
	StartTime      int64      `json:"startTime"`
	EndTime        *int64     `json:"endTime,omitempty"`
	TotalPausedMs  *int64     `json:"totalPausedMs,omitempty"`
	OutputFile     string     `json:"outputFile"`
	OutputOffset   int        `json:"outputOffset"`
	Notified       bool       `json:"notified"`
}

// TaskHandle represents a handle to a running task
type TaskHandle struct {
	TaskID  string
	Cleanup func()
}

// TaskContext provides context for task execution
type TaskContext struct {
	Ctx           context.Context
	Cancel        context.CancelFunc
	GetAppState   func() interface{}
	SetAppState   func(updater func(prev interface{}) interface{})
}

// Task interface defines operations for all task types
type Task interface {
	// GetName returns the task name
	GetName() string

	// GetType returns the task type
	GetType() TaskType

	// Kill stops the task
	Kill(taskId string, setAppState func(updater func(prev interface{}) interface{})) error
}

// LocalShellSpawnInput represents input for spawning a shell task
type LocalShellSpawnInput struct {
	Command     string
	Description string
	Timeout     *int
	ToolUseID   string
	AgentID     string
	Kind        string // "bash" or "monitor"
}

// BashTaskKind represents the kind of bash task
type BashTaskKind string

const (
	BashTaskKindBash    BashTaskKind = "bash"
	BashTaskKindMonitor BashTaskKind = "monitor"
)

// LocalShellTaskState represents the state of a local shell task
type LocalShellTaskState struct {
	TaskStateBase
	Command                         string           `json:"command"`
	Result                          *ShellResult     `json:"result,omitempty"`
	CompletionStatusSentInAttachment bool            `json:"completionStatusSentInAttachment"`
	ShellCommand                    interface{}      `json:"shellCommand"` // Will be *exec.Cmd
	UnregisterCleanup               func()           `json:"-"`
	CleanupTimeoutID                *time.Timer      `json:"-"`
	LastReportedTotalLines          int              `json:"lastReportedTotalLines"`
	IsBackgrounded                  bool             `json:"isBackgrounded"`
	AgentID                         string           `json:"agentId,omitempty"`
	Kind                            BashTaskKind     `json:"kind,omitempty"`
}

// ShellResult represents the result of a shell command
type ShellResult struct {
	Code        int  `json:"code"`
	Interrupted bool `json:"interrupted"`
}

// LocalAgentTaskState represents the state of a local agent task
type LocalAgentTaskState struct {
	TaskStateBase
	AgentID                 string                 `json:"agentId"`
	Prompt                  string                 `json:"prompt"`
	SelectedAgent           interface{}            `json:"selectedAgent"` // AgentDefinition
	AgentType               string                 `json:"agentType"`
	AbortController         context.CancelFunc     `json:"-"`
	UnregisterCleanup       func()                 `json:"-"`
	Retrieved               bool                   `json:"retrieved"`
	LastReportedToolCount   int                    `json:"lastReportedToolCount"`
	LastReportedTokenCount  int                    `json:"lastReportedTokenCount"`
	IsBackgrounded          bool                   `json:"isBackgrounded"`
	PendingMessages         []interface{}          `json:"pendingMessages"`
	Retain                  bool                   `json:"retain"`
	DiskLoaded              bool                   `json:"diskLoaded"`
	Progress                *AgentProgress         `json:"progress,omitempty"`
	Messages                []interface{}          `json:"messages,omitempty"`
	EvictAfter              *int64                 `json:"evictAfter,omitempty"`
}

// AgentProgress represents progress information for an agent task
type AgentProgress struct {
	TokenCount       int             `json:"tokenCount"`
	ToolUseCount     int             `json:"toolUseCount"`
	RecentActivities []ToolActivity  `json:"recentActivities"`
}

// ToolActivity represents a tool usage activity
type ToolActivity struct {
	ToolName string                 `json:"toolName"`
	Input    map[string]interface{} `json:"input"`
}

// RemoteAgentTaskState represents the state of a remote agent task
type RemoteAgentTaskState struct {
	TaskStateBase
	AgentID           string        `json:"agentId"`
	Prompt            string        `json:"prompt"`
	IsBackgrounded    bool          `json:"isBackgrounded"`
	Retrieved         bool          `json:"retrieved"`
	Retain            bool          `json:"retain"`
	DiskLoaded        bool          `json:"diskLoaded"`
	Messages          []interface{} `json:"messages,omitempty"`
	EvictAfter        *int64        `json:"evictAfter,omitempty"`
}

// InProcessTeammateTaskState represents the state of an in-process teammate task
type InProcessTeammateTaskState struct {
	TaskStateBase
	TeammateID    string `json:"teammateId"`
	IsBackgrounded bool  `json:"isBackgrounded"`
}

// LocalWorkflowTaskState represents the state of a local workflow task
type LocalWorkflowTaskState struct {
	TaskStateBase
	WorkflowName   string `json:"workflowName"`
	IsBackgrounded bool   `json:"isBackgrounded"`
}

// MonitorMCPTaskState represents the state of a monitor MCP task
type MonitorMCPTaskState struct {
	TaskStateBase
	ServerName     string `json:"serverName"`
	IsBackgrounded bool   `json:"isBackgrounded"`
}

// DreamTaskState represents the state of a dream task
type DreamTaskState struct {
	TaskStateBase
	IsBackgrounded bool `json:"isBackgrounded"`
}

// TaskState is a union type for all task states
type TaskState interface {
	GetID() string
	GetType() TaskType
	GetStatus() TaskStatus
	GetDescription() string
	GetIsBackgrounded() bool
}

// Implement TaskState interface for all task types

func (t *LocalShellTaskState) GetID() string              { return t.ID }
func (t *LocalShellTaskState) GetType() TaskType          { return t.Type }
func (t *LocalShellTaskState) GetStatus() TaskStatus      { return t.Status }
func (t *LocalShellTaskState) GetDescription() string     { return t.Description }
func (t *LocalShellTaskState) GetIsBackgrounded() bool    { return t.IsBackgrounded }

func (t *LocalAgentTaskState) GetID() string              { return t.ID }
func (t *LocalAgentTaskState) GetType() TaskType          { return t.Type }
func (t *LocalAgentTaskState) GetStatus() TaskStatus      { return t.Status }
func (t *LocalAgentTaskState) GetDescription() string     { return t.Description }
func (t *LocalAgentTaskState) GetIsBackgrounded() bool    { return t.IsBackgrounded }

func (t *RemoteAgentTaskState) GetID() string             { return t.ID }
func (t *RemoteAgentTaskState) GetType() TaskType         { return t.Type }
func (t *RemoteAgentTaskState) GetStatus() TaskStatus     { return t.Status }
func (t *RemoteAgentTaskState) GetDescription() string    { return t.Description }
func (t *RemoteAgentTaskState) GetIsBackgrounded() bool   { return t.IsBackgrounded }

func (t *InProcessTeammateTaskState) GetID() string           { return t.ID }
func (t *InProcessTeammateTaskState) GetType() TaskType       { return t.Type }
func (t *InProcessTeammateTaskState) GetStatus() TaskStatus   { return t.Status }
func (t *InProcessTeammateTaskState) GetDescription() string  { return t.Description }
func (t *InProcessTeammateTaskState) GetIsBackgrounded() bool { return t.IsBackgrounded }

func (t *LocalWorkflowTaskState) GetID() string             { return t.ID }
func (t *LocalWorkflowTaskState) GetType() TaskType         { return t.Type }
func (t *LocalWorkflowTaskState) GetStatus() TaskStatus     { return t.Status }
func (t *LocalWorkflowTaskState) GetDescription() string    { return t.Description }
func (t *LocalWorkflowTaskState) GetIsBackgrounded() bool   { return t.IsBackgrounded }

func (t *MonitorMCPTaskState) GetID() string              { return t.ID }
func (t *MonitorMCPTaskState) GetType() TaskType          { return t.Type }
func (t *MonitorMCPTaskState) GetStatus() TaskStatus      { return t.Status }
func (t *MonitorMCPTaskState) GetDescription() string     { return t.Description }
func (t *MonitorMCPTaskState) GetIsBackgrounded() bool    { return t.IsBackgrounded }

func (t *DreamTaskState) GetID() string               { return t.ID }
func (t *DreamTaskState) GetType() TaskType           { return t.Type }
func (t *DreamTaskState) GetStatus() TaskStatus       { return t.Status }
func (t *DreamTaskState) GetDescription() string      { return t.Description }
func (t *DreamTaskState) GetIsBackgrounded() bool     { return t.IsBackgrounded }

// IsBackgroundTask checks if a task should be shown in the background tasks indicator
func IsBackgroundTask(task TaskState) bool {
	status := task.GetStatus()
	if status != TaskStatusRunning && status != TaskStatusPending {
		return false
	}
	// Foreground tasks (isBackgrounded == false) are not yet "background tasks"
	if !task.GetIsBackgrounded() {
		return false
	}
	return true
}

// TaskRegistry manages task implementations
type TaskRegistry struct {
	mu    sync.RWMutex
	tasks map[TaskType]Task
}

// NewTaskRegistry creates a new task registry
func NewTaskRegistry() *TaskRegistry {
	return &TaskRegistry{
		tasks: make(map[TaskType]Task),
	}
}

// Register registers a task implementation
func (r *TaskRegistry) Register(task Task) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tasks[task.GetType()] = task
}

// Get retrieves a task implementation by type
func (r *TaskRegistry) Get(taskType TaskType) (Task, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	task, ok := r.tasks[taskType]
	return task, ok
}

// GetAll returns all registered tasks
func (r *TaskRegistry) GetAll() map[TaskType]Task {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make(map[TaskType]Task, len(r.tasks))
	for k, v := range r.tasks {
		result[k] = v
	}
	return result
}
