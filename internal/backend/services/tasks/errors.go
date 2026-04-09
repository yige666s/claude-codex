package tasks

import "errors"

var (
	// ErrTaskNotFound is returned when a task is not found.
	ErrTaskNotFound = errors.New("task not found")

	// ErrTaskAlreadyTerminated is returned when trying to modify a terminated task.
	ErrTaskAlreadyTerminated = errors.New("task already terminated")

	// ErrInvalidTaskType is returned for invalid task types.
	ErrInvalidTaskType = errors.New("invalid task type")
)
