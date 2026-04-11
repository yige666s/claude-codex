// Package cron implements CronCreate, CronDelete, and CronList tools.
package cron

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"time"

	"claude-codex/internal/harness/permissions"
	toolkit "claude-codex/internal/harness/tools"
)

// CronJob represents a scheduled cron job.
type CronJob struct {
	ID        string
	Cron      string
	Prompt    string
	Recurring bool
	Durable   bool
	CreatedAt time.Time
}

// cronStore is the in-memory job registry.
var cronStore = struct {
	mu   sync.RWMutex
	jobs map[string]*CronJob
}{jobs: make(map[string]*CronJob)}

func newJobID() string {
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 8)
	for i := range b {
		b[i] = chars[rand.Intn(len(chars))]
	}
	return string(b)
}

// ---- CronCreate ----

type createTool struct{}

func NewCronCreate() toolkit.Tool { return &createTool{} }

func (t *createTool) Name() string { return "CronCreate" }
func (t *createTool) Description() string {
	return `Schedule a prompt to be enqueued at a future time. Use for both recurring schedules and one-shot reminders.

Uses standard 5-field cron in the user's local timezone: minute hour day-of-month month day-of-week.

## One-shot tasks (recurring: false)
For "remind me at X" requests — fire once then auto-delete.

## Recurring jobs (recurring: true, the default)
For "every N minutes" / "every hour" / "weekdays at 9am" requests.

## Avoid the :00 and :30 minute marks when the task allows it — pick an off-minute to spread load.

Jobs live only in this Claude session unless durable=true.
Recurring tasks auto-expire after 7 days.`
}
func (t *createTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "cron": {"type": "string", "description": "Standard 5-field cron expression in local time"},
    "prompt": {"type": "string", "description": "The prompt to enqueue at each fire time"},
    "recurring": {"type": "boolean", "default": true, "description": "true = fire on every match; false = fire once then auto-delete"},
    "durable": {"type": "boolean", "default": false, "description": "true = persist across restarts; false = in-memory only"}
  },
  "required": ["cron", "prompt"]
}`)
}
func (t *createTool) Permission() permissions.Level { return permissions.LevelWrite }
func (t *createTool) IsConcurrencySafe() bool       { return false }

func (t *createTool) Execute(_ context.Context, raw json.RawMessage) (toolkit.Result, error) {
	var in struct {
		Cron      string `json:"cron"`
		Prompt    string `json:"prompt"`
		Recurring *bool  `json:"recurring"`
		Durable   *bool  `json:"durable"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return toolkit.Result{}, err
	}
	if strings.TrimSpace(in.Cron) == "" {
		return toolkit.Result{}, fmt.Errorf("cron expression is required")
	}
	if strings.TrimSpace(in.Prompt) == "" {
		return toolkit.Result{}, fmt.Errorf("prompt is required")
	}

	recurring := true
	if in.Recurring != nil {
		recurring = *in.Recurring
	}
	durable := false
	if in.Durable != nil {
		durable = *in.Durable
	}

	job := &CronJob{
		ID:        newJobID(),
		Cron:      in.Cron,
		Prompt:    in.Prompt,
		Recurring: recurring,
		Durable:   durable,
		CreatedAt: time.Now(),
	}

	cronStore.mu.Lock()
	cronStore.jobs[job.ID] = job
	cronStore.mu.Unlock()

	return toolkit.Result{Output: fmt.Sprintf("Scheduled job %s: '%s' at cron '%s' (recurring=%v, durable=%v)", job.ID, job.Prompt, job.Cron, recurring, durable)}, nil
}

// ---- CronDelete ----

type deleteTool struct{}

func NewCronDelete() toolkit.Tool { return &deleteTool{} }

func (t *deleteTool) Name() string { return "CronDelete" }
func (t *deleteTool) Description() string {
	return "Cancel a cron job previously scheduled with CronCreate. Removes it from the in-memory session store."
}
func (t *deleteTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "id": {"type": "string", "description": "Job ID returned by CronCreate"}
  },
  "required": ["id"]
}`)
}
func (t *deleteTool) Permission() permissions.Level { return permissions.LevelWrite }
func (t *deleteTool) IsConcurrencySafe() bool       { return false }

func (t *deleteTool) Execute(_ context.Context, raw json.RawMessage) (toolkit.Result, error) {
	var in struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return toolkit.Result{}, err
	}

	cronStore.mu.Lock()
	_, ok := cronStore.jobs[in.ID]
	delete(cronStore.jobs, in.ID)
	cronStore.mu.Unlock()

	if !ok {
		return toolkit.Result{}, fmt.Errorf("cron job not found: %s", in.ID)
	}
	return toolkit.Result{Output: fmt.Sprintf("Deleted cron job %s.", in.ID)}, nil
}

// ---- CronList ----

type listTool struct{}

func NewCronList() toolkit.Tool { return &listTool{} }

func (t *listTool) Name() string { return "CronList" }
func (t *listTool) Description() string {
	return "List all cron jobs scheduled via CronCreate in this session."
}
func (t *listTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type": "object", "properties": {}}`)
}
func (t *listTool) Permission() permissions.Level { return permissions.LevelRead }
func (t *listTool) IsConcurrencySafe() bool       { return true }

func (t *listTool) Execute(_ context.Context, _ json.RawMessage) (toolkit.Result, error) {
	cronStore.mu.RLock()
	jobs := make([]*CronJob, 0, len(cronStore.jobs))
	for _, j := range cronStore.jobs {
		jobs = append(jobs, j)
	}
	cronStore.mu.RUnlock()

	if len(jobs) == 0 {
		return toolkit.Result{Output: "No cron jobs scheduled."}, nil
	}

	var sb strings.Builder
	for _, j := range jobs {
		fmt.Fprintf(&sb, "ID: %s | Cron: %s | Recurring: %v | Durable: %v | Prompt: %s\n",
			j.ID, j.Cron, j.Recurring, j.Durable, j.Prompt)
	}
	return toolkit.Result{Output: strings.TrimRight(sb.String(), "\n")}, nil
}
