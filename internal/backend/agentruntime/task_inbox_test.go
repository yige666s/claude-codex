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
}
