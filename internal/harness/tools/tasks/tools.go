// Package tasks implements the TaskCreate, TaskGet, TaskList, TaskUpdate,
// TaskStop, and TaskOutput tools.
package tasks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"claude-codex/internal/harness/permissions"
	coretasks "claude-codex/internal/harness/tasks"
	toolkit "claude-codex/internal/harness/tools"
)

type TaskStatus string

const (
	TaskStatusPending    TaskStatus = "pending"
	TaskStatusInProgress TaskStatus = "in_progress"
	TaskStatusCompleted  TaskStatus = "completed"
)

type Task struct {
	ID          string         `json:"id"`
	Subject     string         `json:"subject"`
	Description string         `json:"description"`
	ActiveForm  string         `json:"activeForm,omitempty"`
	Status      TaskStatus     `json:"status"`
	Owner       string         `json:"owner,omitempty"`
	Blocks      []string       `json:"blocks"`
	BlockedBy   []string       `json:"blockedBy"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

const highWaterMarkFile = ".highwatermark"

var taskFileMu sync.Mutex

func currentTaskListID() string {
	if id := strings.TrimSpace(os.Getenv("CLAUDE_CODE_TASK_LIST_ID")); id != "" {
		return id
	}
	if id := strings.TrimSpace(os.Getenv("CLAUDE_CODE_TEAM_NAME")); id != "" {
		return id
	}
	return "default"
}

func sanitizePathComponent(input string) string {
	var b strings.Builder
	for _, r := range input {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '_' || r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	if b.Len() == 0 {
		return "default"
	}
	return b.String()
}

func tasksDir(taskListID string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude", "tasks", sanitizePathComponent(taskListID)), nil
}

func ensureTasksDir(taskListID string) (string, error) {
	dir, err := tasksDir(taskListID)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

func taskPath(taskListID, taskID string) (string, error) {
	dir, err := ensureTasksDir(taskListID)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, sanitizePathComponent(taskID)+".json"), nil
}

func highWaterMarkPath(taskListID string) (string, error) {
	dir, err := ensureTasksDir(taskListID)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, highWaterMarkFile), nil
}

func readHighWaterMark(taskListID string) int {
	path, err := highWaterMarkPath(taskListID)
	if err != nil {
		return 0
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	value, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0
	}
	return value
}

func writeHighWaterMark(taskListID string, value int) error {
	path, err := highWaterMarkPath(taskListID)
	if err != nil {
		return err
	}
	return os.WriteFile(path, []byte(strconv.Itoa(value)), 0o644)
}

func findHighestTaskIDFromFiles(taskListID string) int {
	dir, err := ensureTasksDir(taskListID)
	if err != nil {
		return 0
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	highest := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".json") || strings.HasPrefix(name, ".") {
			continue
		}
		id := strings.TrimSuffix(name, ".json")
		value, err := strconv.Atoi(id)
		if err == nil && value > highest {
			highest = value
		}
	}
	return highest
}

func findHighestTaskID(taskListID string) int {
	fromFiles := findHighestTaskIDFromFiles(taskListID)
	fromMark := readHighWaterMark(taskListID)
	if fromMark > fromFiles {
		return fromMark
	}
	return fromFiles
}

func readTask(taskListID, taskID string) (*Task, error) {
	path, err := taskPath(taskListID, taskID)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var task Task
	if err := json.Unmarshal(data, &task); err != nil {
		return nil, err
	}
	normalizeTask(&task)
	return &task, nil
}

func normalizeTask(task *Task) {
	if task.Blocks == nil {
		task.Blocks = []string{}
	}
	if task.BlockedBy == nil {
		task.BlockedBy = []string{}
	}
	if task.Status == "" {
		task.Status = TaskStatusPending
	}
}

func writeTask(taskListID string, task *Task) error {
	normalizeTask(task)
	path, err := taskPath(taskListID, task.ID)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(task, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func createTask(taskListID string, task Task) (*Task, error) {
	taskFileMu.Lock()
	defer taskFileMu.Unlock()

	id := strconv.Itoa(findHighestTaskID(taskListID) + 1)
	task.ID = id
	task.Status = TaskStatusPending
	normalizeTask(&task)
	if err := writeTask(taskListID, &task); err != nil {
		return nil, err
	}
	return &task, nil
}

func listTasks(taskListID string) ([]*Task, error) {
	dir, err := ensureTasksDir(taskListID)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	tasks := make([]*Task, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".json") || strings.HasPrefix(name, ".") {
			continue
		}
		task, err := readTask(taskListID, strings.TrimSuffix(name, ".json"))
		if err != nil || task == nil {
			continue
		}
		tasks = append(tasks, task)
	}
	sort.Slice(tasks, func(i, j int) bool {
		left, leftErr := strconv.Atoi(tasks[i].ID)
		right, rightErr := strconv.Atoi(tasks[j].ID)
		if leftErr == nil && rightErr == nil {
			return left < right
		}
		return tasks[i].ID < tasks[j].ID
	})
	return tasks, nil
}

func updateTask(taskListID, taskID string, apply func(task *Task) (bool, error)) (*Task, bool, error) {
	taskFileMu.Lock()
	defer taskFileMu.Unlock()

	task, err := readTask(taskListID, taskID)
	if err != nil || task == nil {
		return nil, false, err
	}
	changed, err := apply(task)
	if err != nil {
		return nil, false, err
	}
	if changed {
		if err := writeTask(taskListID, task); err != nil {
			return nil, false, err
		}
	}
	return task, true, nil
}

func deleteTask(taskListID, taskID string) (bool, error) {
	taskFileMu.Lock()
	defer taskFileMu.Unlock()

	numericID, err := strconv.Atoi(taskID)
	if err == nil && numericID > readHighWaterMark(taskListID) {
		if err := writeHighWaterMark(taskListID, numericID); err != nil {
			return false, err
		}
	}
	path, err := taskPath(taskListID, taskID)
	if err != nil {
		return false, err
	}
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}

	allTasks, err := listTasks(taskListID)
	if err != nil {
		return true, err
	}
	for _, task := range allTasks {
		newBlocks := removeString(task.Blocks, taskID)
		newBlockedBy := removeString(task.BlockedBy, taskID)
		if len(newBlocks) != len(task.Blocks) || len(newBlockedBy) != len(task.BlockedBy) {
			task.Blocks = newBlocks
			task.BlockedBy = newBlockedBy
			if err := writeTask(taskListID, task); err != nil {
				return true, err
			}
		}
	}
	return true, nil
}

func blockTask(taskListID, fromTaskID, toTaskID string) (bool, error) {
	taskFileMu.Lock()
	defer taskFileMu.Unlock()

	fromTask, err := readTask(taskListID, fromTaskID)
	if err != nil || fromTask == nil {
		return false, err
	}
	toTask, err := readTask(taskListID, toTaskID)
	if err != nil || toTask == nil {
		return false, err
	}
	changed := false
	if !containsString(fromTask.Blocks, toTaskID) {
		fromTask.Blocks = append(fromTask.Blocks, toTaskID)
		changed = true
	}
	if !containsString(toTask.BlockedBy, fromTaskID) {
		toTask.BlockedBy = append(toTask.BlockedBy, fromTaskID)
		changed = true
	}
	if changed {
		if err := writeTask(taskListID, fromTask); err != nil {
			return false, err
		}
		if err := writeTask(taskListID, toTask); err != nil {
			return false, err
		}
	}
	return changed, nil
}

func containsString(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

func removeString(items []string, target string) []string {
	result := make([]string, 0, len(items))
	for _, item := range items {
		if item != target {
			result = append(result, item)
		}
	}
	return result
}

func validTaskStatus(status TaskStatus) bool {
	return status == TaskStatusPending || status == TaskStatusInProgress || status == TaskStatusCompleted
}

func writeJSON(value any) (toolkit.Result, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return toolkit.Result{}, err
	}
	return toolkit.Result{Output: string(data)}, nil
}

func decodeStrict(raw json.RawMessage, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

type taskCreateTool struct{}

func NewTaskCreateTool() toolkit.Tool { return &taskCreateTool{} }

func (t *taskCreateTool) Name() string { return "TaskCreate" }
func (t *taskCreateTool) Description() string {
	return `Use this tool to create a structured task list for your current coding session.`
}
func (t *taskCreateTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"subject":{"type":"string"},"description":{"type":"string"},"activeForm":{"type":"string"},"metadata":{"type":"object"}},"required":["subject","description"],"additionalProperties":false}`)
}
func (t *taskCreateTool) Permission() permissions.Level { return permissions.LevelWrite }
func (t *taskCreateTool) IsConcurrencySafe() bool       { return true }

func (t *taskCreateTool) Execute(_ context.Context, raw json.RawMessage) (toolkit.Result, error) {
	var in struct {
		Subject     string         `json:"subject"`
		Description string         `json:"description"`
		ActiveForm  string         `json:"activeForm"`
		Metadata    map[string]any `json:"metadata"`
	}
	if err := decodeStrict(raw, &in); err != nil {
		return toolkit.Result{}, err
	}
	if strings.TrimSpace(in.Subject) == "" {
		return toolkit.Result{}, fmt.Errorf("subject is required")
	}
	if strings.TrimSpace(in.Description) == "" {
		return toolkit.Result{}, fmt.Errorf("description is required")
	}

	task, err := createTask(currentTaskListID(), Task{
		Subject:     in.Subject,
		Description: in.Description,
		ActiveForm:  in.ActiveForm,
		Owner:       "",
		Blocks:      []string{},
		BlockedBy:   []string{},
		Metadata:    in.Metadata,
	})
	if err != nil {
		return toolkit.Result{}, err
	}
	return writeJSON(map[string]any{
		"task": map[string]string{"id": task.ID, "subject": task.Subject},
	})
}

type taskGetTool struct{ manager *coretasks.TaskManager }

func NewTaskGetTool() toolkit.Tool { return NewTaskGetToolWithManager(coretasks.DefaultManager()) }
func NewTaskGetToolWithManager(manager *coretasks.TaskManager) toolkit.Tool {
	return &taskGetTool{manager: manager}
}

func (t *taskGetTool) Name() string { return "TaskGet" }
func (t *taskGetTool) Description() string {
	return `Use this tool to retrieve a task by its ID from the task list.`
}
func (t *taskGetTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"taskId":{"type":"string"}},"required":["taskId"],"additionalProperties":false}`)
}
func (t *taskGetTool) Permission() permissions.Level { return permissions.LevelRead }
func (t *taskGetTool) IsConcurrencySafe() bool       { return true }

func (t *taskGetTool) taskManager() *coretasks.TaskManager {
	if t != nil && t.manager != nil {
		return t.manager
	}
	return coretasks.DefaultManager()
}

func (t *taskGetTool) Execute(_ context.Context, raw json.RawMessage) (toolkit.Result, error) {
	var in struct {
		TaskID string `json:"taskId"`
	}
	if err := decodeStrict(raw, &in); err != nil {
		return toolkit.Result{}, err
	}
	if runtimeTask, ok := t.taskManager().GetTask(in.TaskID); ok {
		return writeJSON(map[string]any{"task": runtimeTaskOutputPayload(runtimeTask)})
	}
	task, err := readTask(currentTaskListID(), in.TaskID)
	if err != nil {
		return toolkit.Result{}, err
	}
	if task == nil {
		return writeJSON(map[string]any{"task": nil})
	}
	return writeJSON(map[string]any{
		"task": map[string]any{
			"id":          task.ID,
			"subject":     task.Subject,
			"description": task.Description,
			"status":      task.Status,
			"blocks":      task.Blocks,
			"blockedBy":   task.BlockedBy,
		},
	})
}

type taskListTool struct{ manager *coretasks.TaskManager }

func NewTaskListTool() toolkit.Tool { return NewTaskListToolWithManager(coretasks.DefaultManager()) }
func NewTaskListToolWithManager(manager *coretasks.TaskManager) toolkit.Tool {
	return &taskListTool{manager: manager}
}

func (t *taskListTool) Name() string { return "TaskList" }
func (t *taskListTool) Description() string {
	return `Use this tool to list all tasks in the task list.`
}
func (t *taskListTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)
}
func (t *taskListTool) Permission() permissions.Level { return permissions.LevelRead }
func (t *taskListTool) IsConcurrencySafe() bool       { return true }

func (t *taskListTool) taskManager() *coretasks.TaskManager {
	if t != nil && t.manager != nil {
		return t.manager
	}
	return coretasks.DefaultManager()
}

func (t *taskListTool) Execute(_ context.Context, raw json.RawMessage) (toolkit.Result, error) {
	var in struct{}
	if len(raw) > 0 {
		if err := decodeStrict(raw, &in); err != nil {
			return toolkit.Result{}, err
		}
	}
	allTasks, err := listTasks(currentTaskListID())
	if err != nil {
		return toolkit.Result{}, err
	}
	resolved := make(map[string]struct{}, len(allTasks))
	for _, task := range allTasks {
		if task.Status == TaskStatusCompleted {
			resolved[task.ID] = struct{}{}
		}
	}
	type listedTask struct {
		ID        string     `json:"id"`
		Subject   string     `json:"subject"`
		Status    TaskStatus `json:"status"`
		Owner     string     `json:"owner,omitempty"`
		BlockedBy []string   `json:"blockedBy"`
	}
	manager := t.taskManager()
	output := make([]any, 0, len(allTasks)+len(manager.ListTasks()))
	for _, task := range allTasks {
		if internal, _ := task.Metadata["_internal"].(bool); internal {
			continue
		}
		blockedBy := make([]string, 0, len(task.BlockedBy))
		for _, blocker := range task.BlockedBy {
			if _, ok := resolved[blocker]; !ok {
				blockedBy = append(blockedBy, blocker)
			}
		}
		output = append(output, listedTask{
			ID:        task.ID,
			Subject:   task.Subject,
			Status:    task.Status,
			Owner:     task.Owner,
			BlockedBy: blockedBy,
		})
	}
	for _, task := range manager.ListTasks() {
		output = append(output, runtimeTaskOutputPayload(task))
	}
	return writeJSON(map[string]any{"tasks": output})
}

type taskUpdateTool struct{}

func NewTaskUpdateTool() toolkit.Tool { return &taskUpdateTool{} }

func (t *taskUpdateTool) Name() string { return "TaskUpdate" }
func (t *taskUpdateTool) Description() string {
	return `Use this tool to update a task in the task list.`
}
func (t *taskUpdateTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"taskId":{"type":"string"},"status":{"type":"string","enum":["pending","in_progress","completed","deleted"]},"subject":{"type":"string"},"description":{"type":"string"},"activeForm":{"type":"string"},"owner":{"type":"string"},"addBlocks":{"type":"array","items":{"type":"string"}},"addBlockedBy":{"type":"array","items":{"type":"string"}},"metadata":{"type":"object"}},"required":["taskId"],"additionalProperties":false}`)
}
func (t *taskUpdateTool) Permission() permissions.Level { return permissions.LevelWrite }
func (t *taskUpdateTool) IsConcurrencySafe() bool       { return true }

func (t *taskUpdateTool) Execute(_ context.Context, raw json.RawMessage) (toolkit.Result, error) {
	var in struct {
		TaskID       string                     `json:"taskId"`
		Status       *string                    `json:"status"`
		Subject      *string                    `json:"subject"`
		Description  *string                    `json:"description"`
		ActiveForm   *string                    `json:"activeForm"`
		Owner        *string                    `json:"owner"`
		AddBlocks    []string                   `json:"addBlocks"`
		AddBlockedBy []string                   `json:"addBlockedBy"`
		Metadata     map[string]json.RawMessage `json:"metadata"`
	}
	if err := decodeStrict(raw, &in); err != nil {
		return toolkit.Result{}, err
	}
	taskListID := currentTaskListID()
	existing, err := readTask(taskListID, in.TaskID)
	if err != nil {
		return toolkit.Result{}, err
	}
	if existing == nil {
		return writeJSON(map[string]any{
			"success":       false,
			"taskId":        in.TaskID,
			"updatedFields": []string{},
			"error":         "Task not found",
		})
	}

	if in.Status != nil && *in.Status == "deleted" {
		deleted, err := deleteTask(taskListID, in.TaskID)
		if err != nil {
			return toolkit.Result{}, err
		}
		result := map[string]any{
			"success":       deleted,
			"taskId":        in.TaskID,
			"updatedFields": []string{},
		}
		if deleted {
			result["updatedFields"] = []string{"deleted"}
			result["statusChange"] = map[string]string{"from": string(existing.Status), "to": "deleted"}
		} else {
			result["error"] = "Failed to delete task"
		}
		return writeJSON(result)
	}

	updatedFields := make([]string, 0)
	var statusChange map[string]string
	_, ok, err := updateTask(taskListID, in.TaskID, func(task *Task) (bool, error) {
		changed := false
		if in.Subject != nil && *in.Subject != task.Subject {
			task.Subject = *in.Subject
			updatedFields = append(updatedFields, "subject")
			changed = true
		}
		if in.Description != nil && *in.Description != task.Description {
			task.Description = *in.Description
			updatedFields = append(updatedFields, "description")
			changed = true
		}
		if in.ActiveForm != nil && *in.ActiveForm != task.ActiveForm {
			task.ActiveForm = *in.ActiveForm
			updatedFields = append(updatedFields, "activeForm")
			changed = true
		}
		if in.Owner != nil && *in.Owner != task.Owner {
			task.Owner = *in.Owner
			updatedFields = append(updatedFields, "owner")
			changed = true
		}
		if len(in.Metadata) > 0 {
			merged := map[string]any{}
			for key, value := range task.Metadata {
				merged[key] = value
			}
			for key, rawValue := range in.Metadata {
				if string(rawValue) == "null" {
					delete(merged, key)
					continue
				}
				var value any
				if err := json.Unmarshal(rawValue, &value); err != nil {
					return false, err
				}
				merged[key] = value
			}
			if !reflect.DeepEqual(task.Metadata, merged) {
				task.Metadata = merged
				updatedFields = append(updatedFields, "metadata")
				changed = true
			}
		}
		if in.Status != nil {
			status := TaskStatus(*in.Status)
			if !validTaskStatus(status) {
				return false, fmt.Errorf("invalid status: %s", *in.Status)
			}
			if status != task.Status {
				statusChange = map[string]string{"from": string(task.Status), "to": string(status)}
				task.Status = status
				updatedFields = append(updatedFields, "status")
				changed = true
			}
		}
		return changed, nil
	})
	if err != nil {
		return toolkit.Result{}, err
	}
	if !ok {
		return writeJSON(map[string]any{
			"success":       false,
			"taskId":        in.TaskID,
			"updatedFields": []string{},
			"error":         "Task not found",
		})
	}

	if len(in.AddBlocks) > 0 {
		changed := false
		for _, id := range in.AddBlocks {
			didChange, err := blockTask(taskListID, in.TaskID, id)
			if err != nil {
				return toolkit.Result{}, err
			}
			changed = changed || didChange
		}
		if changed {
			updatedFields = append(updatedFields, "blocks")
		}
	}
	if len(in.AddBlockedBy) > 0 {
		changed := false
		for _, id := range in.AddBlockedBy {
			didChange, err := blockTask(taskListID, id, in.TaskID)
			if err != nil {
				return toolkit.Result{}, err
			}
			changed = changed || didChange
		}
		if changed {
			updatedFields = append(updatedFields, "blockedBy")
		}
	}

	result := map[string]any{
		"success":       true,
		"taskId":        in.TaskID,
		"updatedFields": updatedFields,
	}
	if statusChange != nil {
		result["statusChange"] = statusChange
	}
	return writeJSON(result)
}

type taskStopTool struct{ manager *coretasks.TaskManager }

func NewTaskStopTool() toolkit.Tool { return NewTaskStopToolWithManager(coretasks.DefaultManager()) }
func NewTaskStopToolWithManager(manager *coretasks.TaskManager) toolkit.Tool {
	return &taskStopTool{manager: manager}
}

func (t *taskStopTool) Name() string { return "TaskStop" }
func (t *taskStopTool) Description() string {
	return `Stop an in-progress task by its ID.`
}
func (t *taskStopTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"task_id":{"type":"string"},"shell_id":{"type":"string"}},"additionalProperties":false}`)
}
func (t *taskStopTool) Permission() permissions.Level { return permissions.LevelWrite }
func (t *taskStopTool) IsConcurrencySafe() bool       { return true }

func (t *taskStopTool) taskManager() *coretasks.TaskManager {
	if t != nil && t.manager != nil {
		return t.manager
	}
	return coretasks.DefaultManager()
}

func (t *taskStopTool) Execute(_ context.Context, raw json.RawMessage) (toolkit.Result, error) {
	var in struct {
		TaskID  string `json:"task_id"`
		ShellID string `json:"shell_id"`
	}
	if err := decodeStrict(raw, &in); err != nil {
		return toolkit.Result{}, err
	}
	id := strings.TrimSpace(in.TaskID)
	if id == "" {
		id = strings.TrimSpace(in.ShellID)
	}
	if id == "" {
		return toolkit.Result{}, fmt.Errorf("missing required parameter: task_id")
	}
	manager := t.taskManager()
	if runtimeTask, ok := manager.GetTask(id); ok {
		if runtimeTask.GetStatus() != coretasks.TaskStatusRunning {
			return toolkit.Result{}, fmt.Errorf("task %s is not running (status: %s)", id, runtimeTask.GetStatus())
		}
		if err := manager.KillTask(id, func(updater func(prev interface{}) interface{}) {}); err != nil {
			return toolkit.Result{}, err
		}
		return writeJSON(map[string]any{
			"message":   fmt.Sprintf("Successfully stopped task: %s (%s)", id, runtimeTask.GetDescription()),
			"task_id":   id,
			"task_type": string(runtimeTask.GetType()),
			"command":   runtimeTask.GetDescription(),
		})
	}
	task, err := readTask(currentTaskListID(), id)
	if err != nil {
		return toolkit.Result{}, err
	}
	if task == nil {
		return toolkit.Result{}, fmt.Errorf("no task found with ID: %s", id)
	}
	if task.Status != TaskStatusInProgress {
		return toolkit.Result{}, fmt.Errorf("task %s is not running (status: %s)", id, task.Status)
	}
	_, _, err = updateTask(currentTaskListID(), id, func(task *Task) (bool, error) {
		task.Status = TaskStatusCompleted
		return true, nil
	})
	if err != nil {
		return toolkit.Result{}, err
	}
	return writeJSON(map[string]any{
		"message":   fmt.Sprintf("Successfully stopped task: %s (%s)", id, task.Subject),
		"task_id":   id,
		"task_type": "task",
		"command":   task.Subject,
	})
}

type taskOutputTool struct{ manager *coretasks.TaskManager }

func NewTaskOutputTool() toolkit.Tool {
	return NewTaskOutputToolWithManager(coretasks.DefaultManager())
}
func NewTaskOutputToolWithManager(manager *coretasks.TaskManager) toolkit.Tool {
	return &taskOutputTool{manager: manager}
}

func (t *taskOutputTool) Name() string { return "TaskOutput" }
func (t *taskOutputTool) Description() string {
	return `Retrieves status and output for a task.`
}
func (t *taskOutputTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"task_id":{"type":"string"},"block":{"type":"boolean","default":true},"timeout":{"type":"number","default":30000}},"required":["task_id"],"additionalProperties":false}`)
}
func (t *taskOutputTool) Permission() permissions.Level { return permissions.LevelRead }
func (t *taskOutputTool) IsConcurrencySafe() bool       { return true }

func (t *taskOutputTool) taskManager() *coretasks.TaskManager {
	if t != nil && t.manager != nil {
		return t.manager
	}
	return coretasks.DefaultManager()
}

func (t *taskOutputTool) Execute(ctx context.Context, raw json.RawMessage) (toolkit.Result, error) {
	var in struct {
		TaskID  string `json:"task_id"`
		Block   *bool  `json:"block"`
		Timeout *int   `json:"timeout"`
	}
	if err := decodeStrict(raw, &in); err != nil {
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
	if _, ok := t.taskManager().GetTask(in.TaskID); ok {
		return t.executeRuntimeTaskOutput(ctx, in.TaskID, block, timeoutMs)
	}
	deadline := time.Now().Add(time.Duration(timeoutMs) * time.Millisecond)
	for {
		task, err := readTask(currentTaskListID(), in.TaskID)
		if err != nil {
			return toolkit.Result{}, err
		}
		if task == nil {
			return toolkit.Result{}, fmt.Errorf("no task found with ID: %s", in.TaskID)
		}
		ready := task.Status == TaskStatusCompleted
		if ready || !block {
			status := "not_ready"
			if ready {
				status = "success"
			}
			return writeJSON(map[string]any{
				"retrieval_status": status,
				"task": map[string]any{
					"task_id":     task.ID,
					"task_type":   "task",
					"status":      task.Status,
					"description": task.Description,
					"output":      "",
				},
			})
		}
		if time.Now().After(deadline) {
			return writeJSON(map[string]any{
				"retrieval_status": "timeout",
				"task": map[string]any{
					"task_id":     task.ID,
					"task_type":   "task",
					"status":      task.Status,
					"description": task.Description,
					"output":      "",
				},
			})
		}
		select {
		case <-ctx.Done():
			return toolkit.Result{}, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func (t *taskOutputTool) executeRuntimeTaskOutput(ctx context.Context, taskID string, block bool, timeoutMs int) (toolkit.Result, error) {
	deadline := time.Now().Add(time.Duration(timeoutMs) * time.Millisecond)
	manager := t.taskManager()
	for {
		task, ok := manager.GetTask(taskID)
		if !ok {
			return toolkit.Result{}, fmt.Errorf("no task found with ID: %s", taskID)
		}
		ready := coretasks.IsTerminalTaskStatus(task.GetStatus())
		if ready || !block {
			return runtimeTaskOutputResult(task, ready)
		}
		if time.Now().After(deadline) {
			return writeJSON(map[string]any{
				"retrieval_status": "timeout",
				"task":             runtimeTaskOutputPayload(task),
			})
		}
		select {
		case <-ctx.Done():
			return toolkit.Result{}, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func runtimeTaskOutputResult(task coretasks.TaskState, ready bool) (toolkit.Result, error) {
	status := "not_ready"
	if ready {
		status = "success"
		if task.GetStatus() == coretasks.TaskStatusFailed || task.GetStatus() == coretasks.TaskStatusKilled {
			status = "failed"
		}
	}
	return writeJSON(map[string]any{
		"retrieval_status": status,
		"task":             runtimeTaskOutputPayload(task),
	})
}

func runtimeTaskOutputPayload(task coretasks.TaskState) map[string]any {
	payload := map[string]any{
		"task_id":     task.GetID(),
		"task_type":   string(task.GetType()),
		"status":      string(task.GetStatus()),
		"description": task.GetDescription(),
		"output":      "",
	}
	if local, ok := task.(*coretasks.LocalAgentTaskState); ok {
		output, _ := coretasks.ReadTaskOutput(local.OutputFile, 0, coretasks.DefaultMaxReadBytes)
		payload["agent_id"] = local.AgentID
		payload["agent_type"] = local.AgentType
		payload["working_dir"] = local.WorkingDir
		payload["worktree_path"] = local.WorktreePath
		payload["worktree_branch"] = local.WorktreeBranch
		payload["output"] = output
		payload["result"] = local.Result
		payload["pending_messages"] = len(local.PendingMessages)
	}
	if teammate, ok := task.(*coretasks.InProcessTeammateTaskState); ok {
		output, _ := coretasks.ReadTaskOutput(teammate.OutputFile, 0, coretasks.DefaultMaxReadBytes)
		payload["agent_id"] = teammate.TeammateID
		payload["teammate_id"] = teammate.TeammateID
		payload["name"] = teammate.Name
		payload["team_name"] = teammate.TeamName
		payload["agent_type"] = teammate.AgentType
		payload["working_dir"] = teammate.WorkingDir
		payload["worktree_path"] = teammate.WorktreePath
		payload["worktree_branch"] = teammate.WorktreeBranch
		payload["output"] = output
		payload["result"] = teammate.Result
		payload["pending_messages"] = len(teammate.PendingMessages)
	}
	return payload
}
