// Package cron implements CronCreate, CronDelete, and CronList tools.
package cron

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

const maxJobs = 50

// CronJob represents a scheduled cron job.
type CronJob struct {
	ID        string
	Cron      string
	Prompt    string
	Recurring bool
	Durable   bool
	CreatedAt time.Time
}

type CreateOutput struct {
	ID            string `json:"id"`
	Cron          string `json:"cron"`
	HumanSchedule string `json:"humanSchedule"`
	Recurring     bool   `json:"recurring"`
	Durable       bool   `json:"durable,omitempty"`
}

type DeleteOutput struct {
	ID string `json:"id"`
}

type ListOutput struct {
	Jobs []JobOutput `json:"jobs"`
}

type JobOutput struct {
	ID            string `json:"id"`
	Cron          string `json:"cron"`
	HumanSchedule string `json:"humanSchedule"`
	Prompt        string `json:"prompt"`
	Recurring     bool   `json:"recurring,omitempty"`
	Durable       bool   `json:"durable,omitempty"`
}

// cronStore is the in-memory job registry.
var cronStore = struct {
	mu   sync.RWMutex
	jobs map[string]*CronJob
}{jobs: make(map[string]*CronJob)}

func ResetForTest() {
	cronStore.mu.Lock()
	defer cronStore.mu.Unlock()
	cronStore.jobs = make(map[string]*CronJob)
}

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
	if err := validateCronExpression(in.Cron); err != nil {
		return toolkit.Result{}, err
	}

	recurring := true
	if in.Recurring != nil {
		recurring = *in.Recurring
	}
	durable := false
	if in.Durable != nil {
		durable = *in.Durable
	}

	cronStore.mu.Lock()
	defer cronStore.mu.Unlock()
	if len(cronStore.jobs) >= maxJobs {
		return toolkit.Result{}, fmt.Errorf("Too many scheduled jobs (max %d). Cancel one first", maxJobs)
	}
	job := &CronJob{
		ID:        newJobID(),
		Cron:      strings.TrimSpace(in.Cron),
		Prompt:    strings.TrimSpace(in.Prompt),
		Recurring: recurring,
		Durable:   durable,
		CreatedAt: time.Now(),
	}
	cronStore.jobs[job.ID] = job

	data, err := json.Marshal(CreateOutput{
		ID:            job.ID,
		Cron:          job.Cron,
		HumanSchedule: cronToHuman(job.Cron),
		Recurring:     job.Recurring,
		Durable:       job.Durable,
	})
	if err != nil {
		return toolkit.Result{}, err
	}
	return toolkit.Result{Output: string(data)}, nil
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
	data, err := json.Marshal(DeleteOutput{ID: in.ID})
	if err != nil {
		return toolkit.Result{}, err
	}
	return toolkit.Result{Output: string(data)}, nil
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

	sort.Slice(jobs, func(i, j int) bool {
		return jobs[i].CreatedAt.Before(jobs[j].CreatedAt)
	})
	out := ListOutput{Jobs: make([]JobOutput, 0, len(jobs))}
	for _, j := range jobs {
		out.Jobs = append(out.Jobs, JobOutput{
			ID:            j.ID,
			Cron:          j.Cron,
			HumanSchedule: cronToHuman(j.Cron),
			Prompt:        j.Prompt,
			Recurring:     j.Recurring,
			Durable:       j.Durable,
		})
	}
	data, err := json.Marshal(out)
	if err != nil {
		return toolkit.Result{}, err
	}
	return toolkit.Result{Output: string(data)}, nil
}

func validateCronExpression(expr string) error {
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return fmt.Errorf("invalid cron expression %q: expected 5 fields", expr)
	}
	for _, field := range fields {
		if strings.TrimSpace(field) == "" {
			return fmt.Errorf("invalid cron expression %q: empty field", expr)
		}
	}
	return nil
}

func cronToHuman(expr string) string {
	return strings.Join(strings.Fields(expr), " ")
}
