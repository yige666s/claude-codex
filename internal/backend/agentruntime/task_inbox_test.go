package agentruntime

import (
	"context"
	"testing"
	"time"
)

func TestTaskInboxAggregatesJobsLoopsAndArtifacts(t *testing.T) {
	runtime := testRuntime(t)
	runtime.SetJobStore(NewMemoryJobStore())
	runtime.SetArtifactService(NewArtifactService(newMemoryArtifactStore(), NewFileObjectStore(t.TempDir()), "artifacts"))

	ctx := context.Background()
	session, err := runtime.CreateSession(ctx, "alice", "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	job, err := runtime.CreateJob(ctx, ChatRequest{
		UserID:    "alice",
		SessionID: session.ID,
		Content:   "write a short report",
	}, JobTypeChat)
	if err != nil {
		t.Fatalf("create job: %v", err)
	}
	now := time.Now().UTC()
	if err := runtime.jobs.UpdateJobStatus(ctx, "alice", job.ID, JobStatusSucceeded, "", now); err != nil {
		t.Fatalf("update job: %v", err)
	}
	artifact, err := runtime.CreateArtifact(WithJobID(ctx, job.ID), "alice", session.ID, "report.md", "text/markdown", []byte("# report"))
	if err != nil {
		t.Fatalf("create artifact: %v", err)
	}

	goal, err := runtime.CreateLoopGoal(ctx, &LoopGoal{
		UserID:        "alice",
		SessionID:     session.ID,
		Objective:     "review release plan",
		WorkflowRunID: "run-review",
		Status:        LoopGoalStatusReviewPending,
		Metadata: map[string]any{
			"review_step_id":     "step-1",
			"review_action_hash": "hash-1",
		},
	})
	if err != nil {
		t.Fatalf("create loop goal: %v", err)
	}

	inbox, err := runtime.TaskInbox(ctx, "alice", TaskInboxOptions{SessionID: session.ID})
	if err != nil {
		t.Fatalf("task inbox: %v", err)
	}
	if inbox.Groups[TaskInboxGroupCompleted] != 1 {
		t.Fatalf("completed group = %d, want 1, items=%#v", inbox.Groups[TaskInboxGroupCompleted], inbox.Items)
	}
	if inbox.Groups[TaskInboxGroupNeedsReview] != 1 {
		t.Fatalf("needs review group = %d, want 1, items=%#v", inbox.Groups[TaskInboxGroupNeedsReview], inbox.Items)
	}

	var completed, review *TaskInboxItem
	for i := range inbox.Items {
		item := &inbox.Items[i]
		if item.JobID == job.ID {
			completed = item
		}
		if item.LoopGoalID == goal.ID {
			review = item
		}
	}
	if completed == nil || completed.ArtifactCount != 1 || completed.PrimaryArtifactID != artifact.ID || completed.NextAction != "Open artifact" {
		t.Fatalf("completed item missing artifact details: %#v", completed)
	}
	if review == nil || review.Review == nil || review.Review.RunID != "run-review" || review.Review.StepID != "step-1" || review.NextAction != "Review" {
		t.Fatalf("review item missing review action: %#v", review)
	}
}
