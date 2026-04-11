// Package tasks implements the task management tools: TaskCreate, TaskGet,
// TaskList, TaskUpdate, TaskStop, TaskOutput.
//
// Tasks are stored in a package-level in-memory store. In a real deployment
// this would be backed by the tasks.Manager from internal/harness/tasks.
package tasks

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"sync"
	"time"

	"claude-codex/internal/harness/permissions"
	toolkit "claude-codex/internal/harness/tools"
)

// ---- Task model ----

type Task struct {
	ID          string
	Subject     string
	Description string
	ActiveForm  string
	Status      string // "pending"|"in_progress"|"completed"|"deleted"
	Owner       string
	Blocks      []string
	BlockedBy   []string
	Output      string
	CreatedAt   time.Time
}

// ---- In-memory store ----

var store = struct {
	mu    sync.RWMutex
	tasks map[string]*Task
}{tasks: make(map[string]*Task)}

func newTaskID() string {
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 6)
	for i := range b {
		b[i] = chars[rand.Intn(len(chars))]
	}
	return string(b)
}

// ---- TaskCreate ----

type taskCreateTool struct{}

func NewTaskCreateTool() toolkit.Tool { return &taskCreateTool{} }

func (t *taskCreateTool) Name() string { return "TaskCreate" }
func (t *taskCreateTool) Description() string {
	return `Use this tool to create a structured task list for your current coding session. This helps you track progress, organize complex tasks, and demonstrate thoroughness to the user.

Use this tool proactively in these scenarios:
- Complex multi-step tasks requiring 3 or more distinct steps
- Non-trivial tasks that require careful planning or multiple operations
- When the user provides multiple tasks (numbered or comma-separated)
- After receiving new instructions — immediately capture requirements as tasks
- When you start working on a task — mark it as in_progress BEFORE beginning work
- After completing a task — mark it as completed and add any new follow-up tasks

All tasks are created with status 'pending'.`
}
func (t *taskCreateTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "subject": {"type": "string", "description": "A brief title for the task"},
    "description": {"type": "string", "description": "What needs to be done"},
    "activeForm": {"type": "string", "description": "Present continuous form shown in spinner when in_progress (e.g., \"Running tests\")"}
  },
  "required": ["subject", "description"]
}`)
}
func (t *taskCreateTool) Permission() permissions.Level { return permissions.LevelWrite }
func (t *taskCreateTool) IsConcurrencySafe() bool       { return false }

func (t *taskCreateTool) Execute(_ context.Context, raw json.RawMessage) (toolkit.Result, error) {
	var in struct {
		Subject     string `json:"subject"`
		Description string `json:"description"`
		ActiveForm  string `json:"activeForm"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return toolkit.Result{}, err
	}
	if strings.TrimSpace(in.Subject) == "" {
		return toolkit.Result{}, fmt.Errorf("subject is required")
	}

	task := &Task{
		ID:          newTaskID(),
		Subject:     in.Subject,
		Description: in.Description,
		ActiveForm:  in.ActiveForm,
		Status:      "pending",
		CreatedAt:   time.Now(),
	}

	store.mu.Lock()
	store.tasks[task.ID] = task
	store.mu.Unlock()

	out, _ := json.Marshal(map[string]interface{}{
		"task": map[string]string{"id": task.ID, "subject": task.Subject},
	})
	return toolkit.Result{Output: string(out)}, nil
}

// ---- TaskGet ----

type taskGetTool struct{}

func NewTaskGetTool() toolkit.Tool { return &taskGetTool{} }

func (t *taskGetTool) Name() string { return "TaskGet" }
func (t *taskGetTool) Description() string {
	return `Use this tool to retrieve a task by its ID from the task list.

Use when:
- You need the full description and context before starting work on a task
- To understand task dependencies (what it blocks, what blocks it)
- After being assigned a task, to get complete requirements`
}
func (t *taskGetTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "taskId": {"type": "string", "description": "The ID of the task to retrieve"}
  },
  "required": ["taskId"]
}`)
}
func (t *taskGetTool) Permission() permissions.Level { return permissions.LevelRead }
func (t *taskGetTool) IsConcurrencySafe() bool       { return true }

func (t *taskGetTool) Execute(_ context.Context, raw json.RawMessage) (toolkit.Result, error) {
	var in struct {
		TaskID string `json:"taskId"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return toolkit.Result{}, err
	}

	store.mu.RLock()
	task, ok := store.tasks[in.TaskID]
	store.mu.RUnlock()

	if !ok {
		return toolkit.Result{}, fmt.Errorf("task not found: %s", in.TaskID)
	}

	out, _ := json.Marshal(map[string]interface{}{
		"id":          task.ID,
		"subject":     task.Subject,
		"description": task.Description,
		"activeForm":  task.ActiveForm,
		"status":      task.Status,
		"owner":       task.Owner,
		"blocks":      task.Blocks,
		"blockedBy":   task.BlockedBy,
	})
	return toolkit.Result{Output: string(out)}, nil
}

// ---- TaskList ----

type taskListTool struct{}

func NewTaskListTool() toolkit.Tool { return &taskListTool{} }

func (t *taskListTool) Name() string { return "TaskList" }
func (t *taskListTool) Description() string {
	return `Use this tool to list all tasks in the task list.

Use to:
- See what tasks are available to work on (status: 'pending', no owner, not blocked)
- Check overall progress on the project
- Find tasks that are blocked and need dependencies resolved
- After completing a task, to check for newly unblocked work`
}
func (t *taskListTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type": "object", "properties": {}}`)
}
func (t *taskListTool) Permission() permissions.Level { return permissions.LevelRead }
func (t *taskListTool) IsConcurrencySafe() bool       { return true }

func (t *taskListTool) Execute(_ context.Context, _ json.RawMessage) (toolkit.Result, error) {
	store.mu.RLock()
	tasks := make([]*Task, 0, len(store.tasks))
	for _, task := range store.tasks {
		if task.Status != "deleted" {
			tasks = append(tasks, task)
		}
	}
	store.mu.RUnlock()

	sort.Slice(tasks, func(i, j int) bool {
		return tasks[i].CreatedAt.Before(tasks[j].CreatedAt)
	})

	if len(tasks) == 0 {
		return toolkit.Result{Output: "No tasks."}, nil
	}

	var sb strings.Builder
	for _, task := range tasks {
		blocked := ""
		if len(task.BlockedBy) > 0 {
			blocked = fmt.Sprintf(" [blocked by: %s]", strings.Join(task.BlockedBy, ", "))
		}
		owner := ""
		if task.Owner != "" {
			owner = fmt.Sprintf(" (owner: %s)", task.Owner)
		}
		fmt.Fprintf(&sb, "#%s [%s] %s%s%s\n", task.ID, task.Status, task.Subject, owner, blocked)
	}
	return toolkit.Result{Output: strings.TrimRight(sb.String(), "\n")}, nil
}

// ---- TaskUpdate ----

type taskUpdateTool struct{}

func NewTaskUpdateTool() toolkit.Tool { return &taskUpdateTool{} }

func (t *taskUpdateTool) Name() string { return "TaskUpdate" }
func (t *taskUpdateTool) Description() string {
	return `Use this tool to update a task in the task list.

Use to:
- Mark tasks as resolved: set status to "completed" when done, "deleted" to remove
- Update task details when requirements change
- Set up dependencies with addBlocks/addBlockedBy
- Claim tasks by setting owner

IMPORTANT: Only mark a task as completed when you have FULLY accomplished it.
Status progresses: pending → in_progress → completed`
}
func (t *taskUpdateTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "taskId": {"type": "string", "description": "The ID of the task to update"},
    "status": {"type": "string", "enum": ["pending", "in_progress", "completed", "deleted"]},
    "subject": {"type": "string"},
    "description": {"type": "string"},
    "activeForm": {"type": "string"},
    "owner": {"type": "string"},
    "addBlocks": {"type": "array", "items": {"type": "string"}, "description": "Task IDs that this task blocks"},
    "addBlockedBy": {"type": "array", "items": {"type": "string"}, "description": "Task IDs that block this task"}
  },
  "required": ["taskId"]
}`)
}
func (t *taskUpdateTool) Permission() permissions.Level { return permissions.LevelWrite }
func (t *taskUpdateTool) IsConcurrencySafe() bool       { return false }

func (t *taskUpdateTool) Execute(_ context.Context, raw json.RawMessage) (toolkit.Result, error) {
	var in struct {
		TaskID      string   `json:"taskId"`
		Status      *string  `json:"status"`
		Subject     *string  `json:"subject"`
		Description *string  `json:"description"`
		ActiveForm  *string  `json:"activeForm"`
		Owner       *string  `json:"owner"`
		AddBlocks   []string `json:"addBlocks"`
		AddBlockedBy []string `json:"addBlockedBy"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return toolkit.Result{}, err
	}

	store.mu.Lock()
	task, ok := store.tasks[in.TaskID]
	if !ok {
		store.mu.Unlock()
		return toolkit.Result{}, fmt.Errorf("task not found: %s", in.TaskID)
	}

	if in.Status != nil {
		task.Status = *in.Status
	}
	if in.Subject != nil {
		task.Subject = *in.Subject
	}
	if in.Description != nil {
		task.Description = *in.Description
	}
	if in.ActiveForm != nil {
		task.ActiveForm = *in.ActiveForm
	}
	if in.Owner != nil {
		task.Owner = *in.Owner
	}
	for _, id := range in.AddBlocks {
		task.Blocks = appendUnique(task.Blocks, id)
	}
	for _, id := range in.AddBlockedBy {
		task.BlockedBy = appendUnique(task.BlockedBy, id)
	}
	store.mu.Unlock()

	return toolkit.Result{Output: fmt.Sprintf("Task %s updated (status: %s).", in.TaskID, task.Status)}, nil
}

func appendUnique(s []string, v string) []string {
	for _, x := range s {
		if x == v {
			return s
		}
	}
	return append(s, v)
}

// ---- TaskStop ----

type taskStopTool struct{}

func NewTaskStopTool() toolkit.Tool { return &taskStopTool{} }

func (t *taskStopTool) Name() string { return "TaskStop" }
func (t *taskStopTool) Description() string {
	return `Stop a running background task by its ID. Returns a success or failure status.

Use this when you need to terminate a long-running task.`
}
func (t *taskStopTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "task_id": {"type": "string", "description": "The ID of the background task to stop"}
  },
  "required": ["task_id"]
}`)
}
func (t *taskStopTool) Permission() permissions.Level { return permissions.LevelWrite }
func (t *taskStopTool) IsConcurrencySafe() bool       { return false }

func (t *taskStopTool) Execute(_ context.Context, raw json.RawMessage) (toolkit.Result, error) {
	var in struct {
		TaskID string `json:"task_id"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return toolkit.Result{}, err
	}

	store.mu.Lock()
	task, ok := store.tasks[in.TaskID]
	if ok {
		task.Status = "killed"
	}
	store.mu.Unlock()

	if !ok {
		return toolkit.Result{}, fmt.Errorf("task not found: %s", in.TaskID)
	}
	return toolkit.Result{Output: fmt.Sprintf("Task %s stopped.", in.TaskID)}, nil
}

// ---- TaskOutput ----

type taskOutputTool struct{}

func NewTaskOutputTool() toolkit.Tool { return &taskOutputTool{} }

func (t *taskOutputTool) Name() string { return "TaskOutput" }
func (t *taskOutputTool) Description() string {
	return `Retrieves output from a running or completed task.

Use block=true (default) to wait for task completion.
Use block=false for non-blocking check of current status.`
}
func (t *taskOutputTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "task_id": {"type": "string", "description": "The task ID to get output from"},
    "block": {"type": "boolean", "default": true, "description": "Whether to wait for completion"},
    "timeout": {"type": "number", "default": 30000, "description": "Max wait time in ms"}
  },
  "required": ["task_id"]
}`)
}
func (t *taskOutputTool) Permission() permissions.Level { return permissions.LevelRead }
func (t *taskOutputTool) IsConcurrencySafe() bool       { return true }

func (t *taskOutputTool) Execute(ctx context.Context, raw json.RawMessage) (toolkit.Result, error) {
	var in struct {
		TaskID  string  `json:"task_id"`
		Block   *bool   `json:"block"`
		Timeout *int    `json:"timeout"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return toolkit.Result{}, err
	}

	block := true
	if in.Block != nil {
		block = *in.Block
	}
	timeoutMs := 30000
	if in.Timeout != nil {
		timeoutMs = *in.Timeout
	}

	deadline := time.Now().Add(time.Duration(timeoutMs) * time.Millisecond)

	for {
		store.mu.RLock()
		task, ok := store.tasks[in.TaskID]
		store.mu.RUnlock()

		if !ok {
			return toolkit.Result{}, fmt.Errorf("task not found: %s", in.TaskID)
		}

		terminal := task.Status == "completed" || task.Status == "failed" || task.Status == "killed"
		if terminal || !block {
			out := task.Output
			if out == "" {
				out = fmt.Sprintf("Task %s: status=%s", in.TaskID, task.Status)
			}
			return toolkit.Result{Output: out}, nil
		}

		select {
		case <-ctx.Done():
			return toolkit.Result{}, ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}

		if time.Now().After(deadline) {
			return toolkit.Result{Output: fmt.Sprintf("Task %s: timeout waiting for completion (status: %s)", in.TaskID, task.Status)}, nil
		}
	}
}
