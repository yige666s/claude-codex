package agentruntime

import (
	"context"
	"sort"
	"strings"
	"time"
)

const (
	TaskInboxGroupRunning     = "running"
	TaskInboxGroupNeedsReview = "needs_review"
	TaskInboxGroupFailed      = "failed"
	TaskInboxGroupBlocked     = "blocked"
	TaskInboxGroupCompleted   = "completed"
	TaskInboxGroupScheduled   = "scheduled"
)

type TaskInboxOptions struct {
	SessionID string
	Limit     int
}

type TaskInboxResponse struct {
	Items       []TaskInboxItem `json:"items"`
	Groups      map[string]int  `json:"groups"`
	GeneratedAt time.Time       `json:"generated_at"`
}

type TaskInboxItem struct {
	ID                string                 `json:"id"`
	Kind              string                 `json:"kind"`
	Group             string                 `json:"group"`
	Title             string                 `json:"title"`
	Status            string                 `json:"status"`
	SessionID         string                 `json:"session_id,omitempty"`
	JobID             string                 `json:"job_id,omitempty"`
	LoopGoalID        string                 `json:"loop_goal_id,omitempty"`
	LoopRunID         string                 `json:"loop_run_id,omitempty"`
	ArtifactID        string                 `json:"artifact_id,omitempty"`
	Trigger           string                 `json:"trigger,omitempty"`
	LastEvent         string                 `json:"last_event,omitempty"`
	LastEventAt       *time.Time             `json:"last_event_at,omitempty"`
	ArtifactCount     int                    `json:"artifact_count"`
	PrimaryArtifactID string                 `json:"primary_artifact_id,omitempty"`
	NextAction        string                 `json:"next_action,omitempty"`
	NotificationType  string                 `json:"notification_type,omitempty"`
	Review            *TaskInboxReviewAction `json:"review,omitempty"`
	CreatedAt         time.Time              `json:"created_at"`
	UpdatedAt         time.Time              `json:"updated_at"`
}

type TaskInboxReviewAction struct {
	Kind       string `json:"kind"`
	RunID      string `json:"run_id,omitempty"`
	StepID     string `json:"step_id,omitempty"`
	ActionHash string `json:"action_hash,omitempty"`
}

func (r *Runtime) TaskInbox(ctx context.Context, userID string, opts TaskInboxOptions) (*TaskInboxResponse, error) {
	opts.SessionID = strings.TrimSpace(opts.SessionID)
	if opts.Limit <= 0 {
		opts.Limit = 100
	}
	if opts.Limit > 300 {
		opts.Limit = 300
	}

	jobs, err := r.ListJobs(ctx, userID, opts.SessionID)
	if err != nil {
		return nil, err
	}
	goals, err := r.ListLoopGoals(ctx, LoopGoalFilter{UserID: userID, SessionID: opts.SessionID, Limit: opts.Limit})
	if err != nil {
		return nil, err
	}
	artifacts, err := r.ListArtifacts(ctx, userID, opts.SessionID)
	if err != nil {
		return nil, err
	}

	artifactsByJob := make(map[string][]*Artifact)
	artifactSeen := make(map[string]struct{})
	for _, artifact := range artifacts {
		if artifact == nil {
			continue
		}
		if jobID := strings.TrimSpace(artifact.JobID); jobID != "" {
			artifactsByJob[jobID] = append(artifactsByJob[jobID], artifact)
		}
	}

	items := make([]TaskInboxItem, 0, len(jobs)+len(goals)+len(artifacts))
	for _, job := range jobs {
		if job == nil {
			continue
		}
		jobArtifacts := artifactsByJob[job.ID]
		for _, artifact := range jobArtifacts {
			artifactSeen[artifact.ID] = struct{}{}
		}
		item := taskInboxItemFromJob(ctx, r, userID, job, jobArtifacts)
		items = append(items, item)
	}
	for _, goal := range goals {
		if goal == nil {
			continue
		}
		items = append(items, taskInboxItemFromLoopGoal(goal, artifactsByJob[goal.JobID]))
	}
	for _, artifact := range artifacts {
		if artifact == nil {
			continue
		}
		if _, ok := artifactSeen[artifact.ID]; ok {
			continue
		}
		items = append(items, taskInboxItemFromArtifact(artifact))
	}

	sort.SliceStable(items, func(i, j int) bool {
		return items[i].UpdatedAt.After(items[j].UpdatedAt)
	})
	if len(items) > opts.Limit {
		items = items[:opts.Limit]
	}
	groups := map[string]int{
		TaskInboxGroupRunning:     0,
		TaskInboxGroupNeedsReview: 0,
		TaskInboxGroupFailed:      0,
		TaskInboxGroupBlocked:     0,
		TaskInboxGroupCompleted:   0,
		TaskInboxGroupScheduled:   0,
	}
	for _, item := range items {
		groups[item.Group]++
	}
	return &TaskInboxResponse{Items: items, Groups: groups, GeneratedAt: time.Now().UTC()}, nil
}

func taskInboxItemFromJob(ctx context.Context, r *Runtime, userID string, job *Job, artifacts []*Artifact) TaskInboxItem {
	group := taskInboxGroupForJob(job)
	lastEvent, lastEventAt := taskInboxLastJobEvent(ctx, r, userID, job)
	if lastEvent == "" {
		lastEvent = taskInboxJobLastEvent(job)
	}
	item := TaskInboxItem{
		ID:                "job:" + job.ID,
		Kind:              "job",
		Group:             group,
		Title:             taskInboxTitle(job.Content, job.Type+" job"),
		Status:            job.Status,
		SessionID:         job.SessionID,
		JobID:             job.ID,
		LoopGoalID:        job.LoopGoalID,
		Trigger:           job.Type,
		LastEvent:         lastEvent,
		LastEventAt:       lastEventAt,
		ArtifactCount:     len(artifacts),
		PrimaryArtifactID: taskInboxPrimaryArtifactID(artifacts),
		NextAction:        taskInboxNextAction(group, len(artifacts) > 0),
		NotificationType:  taskInboxNotificationType(group),
		CreatedAt:         job.CreatedAt,
		UpdatedAt:         job.UpdatedAt,
	}
	if item.LastEventAt == nil {
		item.LastEventAt = &job.UpdatedAt
	}
	return item
}

func taskInboxItemFromLoopGoal(goal *LoopGoal, artifacts []*Artifact) TaskInboxItem {
	group := taskInboxGroupForLoopGoal(goal)
	item := TaskInboxItem{
		ID:                "loop:" + goal.ID,
		Kind:              "loop",
		Group:             group,
		Title:             taskInboxTitle(goal.Objective, "Loop goal"),
		Status:            goal.Status,
		SessionID:         goal.SessionID,
		JobID:             goal.JobID,
		LoopGoalID:        goal.ID,
		LoopRunID:         goal.WorkflowRunID,
		Trigger:           firstNonEmptyString(goal.Trigger.Type, LoopTriggerManual),
		LastEvent:         taskInboxLoopLastEvent(goal),
		LastEventAt:       &goal.UpdatedAt,
		ArtifactCount:     len(artifacts),
		PrimaryArtifactID: taskInboxPrimaryArtifactID(artifacts),
		NextAction:        taskInboxNextAction(group, len(artifacts) > 0),
		NotificationType:  taskInboxNotificationType(group),
		CreatedAt:         goal.CreatedAt,
		UpdatedAt:         goal.UpdatedAt,
	}
	if group == TaskInboxGroupNeedsReview && goal.WorkflowRunID != "" {
		item.Review = &TaskInboxReviewAction{
			Kind:       "loop_run",
			RunID:      goal.WorkflowRunID,
			StepID:     taskInboxStringMetadata(goal.Metadata, "review_step_id"),
			ActionHash: taskInboxStringMetadata(goal.Metadata, "review_action_hash"),
		}
	}
	return item
}

func taskInboxItemFromArtifact(artifact *Artifact) TaskInboxItem {
	return TaskInboxItem{
		ID:                "artifact:" + artifact.ID,
		Kind:              "artifact",
		Group:             TaskInboxGroupCompleted,
		Title:             taskInboxTitle(artifact.Filename, "Artifact"),
		Status:            "created",
		SessionID:         artifact.SessionID,
		JobID:             artifact.JobID,
		ArtifactID:        artifact.ID,
		LastEvent:         "Artifact created",
		LastEventAt:       &artifact.CreatedAt,
		ArtifactCount:     1,
		PrimaryArtifactID: artifact.ID,
		NextAction:        "Open artifact",
		NotificationType:  "job_completed",
		CreatedAt:         artifact.CreatedAt,
		UpdatedAt:         artifact.CreatedAt,
	}
}

func taskInboxGroupForJob(job *Job) string {
	switch job.Status {
	case JobStatusQueued, JobStatusRunning:
		return TaskInboxGroupRunning
	case JobStatusFailed:
		if strings.Contains(strings.ToLower(job.Error), "blocked") {
			return TaskInboxGroupBlocked
		}
		return TaskInboxGroupFailed
	case JobStatusCancelled:
		return TaskInboxGroupBlocked
	case JobStatusSucceeded:
		return TaskInboxGroupCompleted
	default:
		if strings.Contains(strings.ToLower(job.Status), "review") {
			return TaskInboxGroupNeedsReview
		}
		return TaskInboxGroupRunning
	}
}

func taskInboxGroupForLoopGoal(goal *LoopGoal) string {
	switch goal.Status {
	case LoopGoalStatusRunning:
		return TaskInboxGroupRunning
	case LoopGoalStatusPending:
		if strings.EqualFold(goal.Trigger.Type, LoopTriggerSchedule) {
			return TaskInboxGroupScheduled
		}
		return TaskInboxGroupRunning
	case LoopGoalStatusReviewPending:
		return TaskInboxGroupNeedsReview
	case LoopGoalStatusFailed:
		return TaskInboxGroupFailed
	case LoopGoalStatusBlocked, LoopGoalStatusBudgetExceeded, LoopGoalStatusCancelled:
		return TaskInboxGroupBlocked
	case LoopGoalStatusSucceeded:
		return TaskInboxGroupCompleted
	default:
		return TaskInboxGroupRunning
	}
}

func taskInboxLastJobEvent(ctx context.Context, r *Runtime, userID string, job *Job) (string, *time.Time) {
	events, err := r.ListJobEvents(ctx, userID, job.ID, "", 0)
	if err != nil || len(events) == 0 {
		return "", nil
	}
	latest := events[len(events)-1]
	if latest == nil {
		return "", nil
	}
	message := firstNonEmptyString(latest.Event.Content, latest.Event.Error, latest.Event.Type, latest.Type)
	return message, &latest.CreatedAt
}

func taskInboxJobLastEvent(job *Job) string {
	if job.Error != "" {
		return job.Error
	}
	switch job.Status {
	case JobStatusQueued:
		return "Job queued"
	case JobStatusRunning:
		return "Job running"
	case JobStatusSucceeded:
		return "Job completed"
	case JobStatusFailed:
		return "Job failed"
	case JobStatusCancelled:
		return "Job cancelled"
	default:
		return "Job updated"
	}
}

func taskInboxLoopLastEvent(goal *LoopGoal) string {
	switch goal.Status {
	case LoopGoalStatusReviewPending:
		return "Review required"
	case LoopGoalStatusBudgetExceeded:
		return "Quota blocked"
	case LoopGoalStatusBlocked:
		return "Loop blocked"
	case LoopGoalStatusSucceeded:
		return "Loop completed"
	case LoopGoalStatusFailed:
		return "Loop failed"
	case LoopGoalStatusPending:
		if strings.EqualFold(goal.Trigger.Type, LoopTriggerSchedule) {
			return "Loop scheduled"
		}
		return "Loop pending"
	default:
		return "Loop " + goal.Status
	}
}

func taskInboxNextAction(group string, hasArtifact bool) string {
	switch group {
	case TaskInboxGroupNeedsReview:
		return "Review"
	case TaskInboxGroupFailed:
		return "Inspect failure"
	case TaskInboxGroupBlocked:
		return "Resolve blocker"
	case TaskInboxGroupCompleted:
		if hasArtifact {
			return "Open artifact"
		}
		return "Open result"
	case TaskInboxGroupScheduled:
		return "Open schedule"
	default:
		return "Open task"
	}
}

func taskInboxNotificationType(group string) string {
	switch group {
	case TaskInboxGroupNeedsReview:
		return "review_required"
	case TaskInboxGroupFailed:
		return "job_failed"
	case TaskInboxGroupBlocked:
		return "quota_blocked"
	case TaskInboxGroupCompleted:
		return "job_completed"
	case TaskInboxGroupScheduled:
		return "loop_triggered"
	default:
		return "job_updated"
	}
}

func taskInboxPrimaryArtifactID(artifacts []*Artifact) string {
	if len(artifacts) == 0 || artifacts[0] == nil {
		return ""
	}
	return artifacts[0].ID
}

func taskInboxTitle(value, fallback string) string {
	value = strings.Join(strings.Fields(value), " ")
	if value == "" {
		return fallback
	}
	const max = 96
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	return string(runes[:max-3]) + "..."
}

func taskInboxStringMetadata(metadata map[string]any, key string) string {
	if metadata == nil {
		return ""
	}
	value, _ := metadata[key].(string)
	return strings.TrimSpace(value)
}
