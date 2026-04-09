package tasks

import (
	"crypto/rand"
	"fmt"
	"time"
)

// TaskType represents the type of a task.
type TaskType string

const (
	TaskTypeLocalBash         TaskType = "local_bash"
	TaskTypeLocalAgent        TaskType = "local_agent"
	TaskTypeRemoteAgent       TaskType = "remote_agent"
	TaskTypeInProcessTeammate TaskType = "in_process_teammate"
	TaskTypeLocalWorkflow     TaskType = "local_workflow"
	TaskTypeMonitorMCP        TaskType = "monitor_mcp"
	TaskTypeDream             TaskType = "dream"
)

// TaskStatus represents the status of a task.
type TaskStatus string

const (
	TaskStatusPending   TaskStatus = "pending"
	TaskStatusRunning   TaskStatus = "running"
	TaskStatusCompleted TaskStatus = "completed"
	TaskStatusFailed    TaskStatus = "failed"
	TaskStatusKilled    TaskStatus = "killed"
)

// IsTerminal returns true if the status is terminal (won't transition further).
func (s TaskStatus) IsTerminal() bool {
	return s == TaskStatusCompleted || s == TaskStatusFailed || s == TaskStatusKilled
}

// TaskState represents the state of a task.
type TaskState struct {
	ID            string     `json:"id"`
	Type          TaskType   `json:"type"`
	Status        TaskStatus `json:"status"`
	Description   string     `json:"description"`
	ToolUseID     string     `json:"tool_use_id,omitempty"`
	StartTime     int64      `json:"start_time"`
	EndTime       int64      `json:"end_time,omitempty"`
	TotalPausedMs int64      `json:"total_paused_ms,omitempty"`
	OutputFile    string     `json:"output_file"`
	OutputOffset  int64      `json:"output_offset"`
	Notified      bool       `json:"notified"`
}

// Task ID prefixes for different task types.
var taskIDPrefixes = map[TaskType]string{
	TaskTypeLocalBash:         "b",
	TaskTypeLocalAgent:        "a",
	TaskTypeRemoteAgent:       "r",
	TaskTypeInProcessTeammate: "t",
	TaskTypeLocalWorkflow:     "w",
	TaskTypeMonitorMCP:        "m",
	TaskTypeDream:             "d",
}

// Case-insensitive-safe alphabet for task IDs.
const taskIDAlphabet = "0123456789abcdefghijklmnopqrstuvwxyz"

// GenerateTaskID generates a unique task ID with type-specific prefix.
// Format: {prefix}{8 random chars from alphabet}
// Example: "b3k7m9n2q1" for local_bash
func GenerateTaskID(taskType TaskType) string {
	prefix := taskIDPrefixes[taskType]
	if prefix == "" {
		prefix = "x"
	}

	bytes := make([]byte, 8)
	if _, err := rand.Read(bytes); err != nil {
		// Fallback to timestamp-based ID
		return fmt.Sprintf("%s%d", prefix, time.Now().UnixNano())
	}

	id := prefix
	for i := 0; i < 8; i++ {
		id += string(taskIDAlphabet[int(bytes[i])%len(taskIDAlphabet)])
	}
	return id
}

// NewTaskState creates a new task state.
func NewTaskState(taskType TaskType, description, toolUseID, outputFile string) *TaskState {
	return &TaskState{
		ID:           GenerateTaskID(taskType),
		Type:         taskType,
		Status:       TaskStatusPending,
		Description:  description,
		ToolUseID:    toolUseID,
		StartTime:    time.Now().UnixMilli(),
		OutputFile:   outputFile,
		OutputOffset: 0,
		Notified:     false,
	}
}
