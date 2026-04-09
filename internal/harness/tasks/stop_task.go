package tasks

import (
	"fmt"
)

// StopTaskError represents an error when stopping a task
type StopTaskError struct {
	Message string
	Code    StopTaskErrorCode
}

// StopTaskErrorCode represents the type of stop task error
type StopTaskErrorCode string

const (
	StopTaskErrorNotFound        StopTaskErrorCode = "not_found"
	StopTaskErrorNotRunning      StopTaskErrorCode = "not_running"
	StopTaskErrorUnsupportedType StopTaskErrorCode = "unsupported_type"
)

func (e *StopTaskError) Error() string {
	return e.Message
}

// NewStopTaskError creates a new stop task error
func NewStopTaskError(message string, code StopTaskErrorCode) *StopTaskError {
	return &StopTaskError{
		Message: message,
		Code:    code,
	}
}

// StopTaskContext provides context for stopping a task
type StopTaskContext struct {
	GetAppState func() interface{}
	SetAppState SetAppState
}

// StopTaskResult represents the result of stopping a task
type StopTaskResult struct {
	TaskID   string
	TaskType string
	Command  *string
}

// StopTask looks up a task by ID, validates it is running, kills it, and marks it as notified
func StopTask(taskID string, context StopTaskContext, manager *TaskManager) (*StopTaskResult, error) {
	appState := context.GetAppState()
	_ = appState // TODO: Extract task from app state

	task, ok := manager.GetTask(taskID)
	if !ok {
		return nil, NewStopTaskError(
			fmt.Sprintf("No task found with ID: %s", taskID),
			StopTaskErrorNotFound,
		)
	}

	if task.GetStatus() != TaskStatusRunning {
		return nil, NewStopTaskError(
			fmt.Sprintf("Task %s is not running (status: %s)", taskID, task.GetStatus()),
			StopTaskErrorNotRunning,
		)
	}

	taskImpl, ok := manager.registry.Get(task.GetType())
	if !ok {
		return nil, NewStopTaskError(
			fmt.Sprintf("Unsupported task type: %s", task.GetType()),
			StopTaskErrorUnsupportedType,
		)
	}

	if err := taskImpl.Kill(taskID, context.SetAppState); err != nil {
		return nil, err
	}

	// Bash: suppress the "exit code 137" notification (noise)
	// Agent tasks: don't suppress — the AbortError catch sends a notification
	var command *string
	if shellTask, ok := task.(*LocalShellTaskState); ok {
		suppressed := false
		context.SetAppState(func(prev interface{}) interface{} {
			// TODO: Mark task as notified
			suppressed = true
			return prev
		})

		// Emit SDK event if suppressed
		if suppressed {
			// TODO: Emit task terminated SDK event
		}

		command = &shellTask.Command
	} else {
		desc := task.GetDescription()
		command = &desc
	}

	return &StopTaskResult{
		TaskID:   taskID,
		TaskType: string(task.GetType()),
		Command:  command,
	}, nil
}
