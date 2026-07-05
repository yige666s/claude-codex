package agentruntime

import (
	"context"
	"testing"
	"time"
)

func TestTaskInboxAggregatesJobsAndArtifacts(t *testing.T) {
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

	inbox, err := runtime.TaskInbox(ctx, "alice", TaskInboxOptions{SessionID: session.ID})
	if err != nil {
		t.Fatalf("task inbox: %v", err)
	}
	if inbox.Groups[TaskInboxGroupCompleted] != 1 {
		t.Fatalf("completed group = %d, want 1, items=%#v", inbox.Groups[TaskInboxGroupCompleted], inbox.Items)
	}
	var completed *TaskInboxItem
	for i := range inbox.Items {
		item := &inbox.Items[i]
		if item.JobID == job.ID {
			completed = item
		}
	}
	if completed == nil || completed.ArtifactCount != 1 || completed.PrimaryArtifactID != artifact.ID || completed.NextAction != "Open artifact" {
		t.Fatalf("completed item missing artifact details: %#v", completed)
	}
	if !completed.SessionAvailable {
		t.Fatalf("completed item should point at an available session: %#v", completed)
	}
}

func TestTaskInboxHidesItemsWhenSessionDeleted(t *testing.T) {
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
		Content:   "generate a dog image",
	}, JobTypeSkill)
	if err != nil {
		t.Fatalf("create job: %v", err)
	}
	now := time.Now().UTC()
	if err := runtime.jobs.UpdateJobStatus(ctx, "alice", job.ID, JobStatusFailed, "sql: no rows in result set", now); err != nil {
		t.Fatalf("update job: %v", err)
	}
	artifact, err := runtime.CreateArtifact(WithJobID(ctx, job.ID), "alice", session.ID, "dog.png", "image/png", []byte("png"))
	if err != nil {
		t.Fatalf("create artifact: %v", err)
	}
	if err := runtime.DeleteSession(ctx, "alice", session.ID); err != nil {
		t.Fatalf("delete session: %v", err)
	}

	inbox, err := runtime.TaskInbox(ctx, "alice", TaskInboxOptions{})
	if err != nil {
		t.Fatalf("task inbox after deleted session: %v", err)
	}
	for _, item := range inbox.Items {
		if item.JobID == job.ID || item.ArtifactID == artifact.ID {
			t.Fatalf("deleted-session inbox item should be hidden: %#v", item)
		}
	}
	if inbox.Groups[TaskInboxGroupFailed] != 0 || inbox.Groups[TaskInboxGroupCompleted] != 0 {
		t.Fatalf("deleted-session inbox item should not affect group counts: groups=%#v items=%#v", inbox.Groups, inbox.Items)
	}
}
