package memory

import (
	"context"
	"testing"
	"time"
)

func TestNewExtractionService(t *testing.T) {
	service := NewExtractionService(2)
	if service == nil {
		t.Fatal("Expected non-nil service")
	}
	if service.workers != 2 {
		t.Errorf("Expected 2 workers, got %d", service.workers)
	}
}

func TestNewExtractionService_DefaultWorkers(t *testing.T) {
	service := NewExtractionService(0)
	if service.workers != 2 {
		t.Errorf("Expected default 2 workers, got %d", service.workers)
	}
}

func TestExtractionService_Start(t *testing.T) {
	service := NewExtractionService(2)
	ctx := context.Background()

	err := service.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start service: %v", err)
	}

	if !service.running {
		t.Error("Expected service to be running")
	}

	service.Stop()
}

func TestExtractionService_Start_AlreadyRunning(t *testing.T) {
	service := NewExtractionService(2)
	ctx := context.Background()

	service.Start(ctx)
	err := service.Start(ctx)

	if err == nil {
		t.Error("Expected error when starting already running service")
	}

	service.Stop()
}

func TestExtractionService_Stop(t *testing.T) {
	service := NewExtractionService(2)
	ctx := context.Background()

	service.Start(ctx)
	err := service.Stop()

	if err != nil {
		t.Fatalf("Failed to stop service: %v", err)
	}

	if service.running {
		t.Error("Expected service to be stopped")
	}
}

func TestExtractionService_Stop_NotRunning(t *testing.T) {
	service := NewExtractionService(2)
	err := service.Stop()

	if err == nil {
		t.Error("Expected error when stopping non-running service")
	}
}

func TestExtractionService_Submit(t *testing.T) {
	service := NewExtractionService(2)
	ctx := context.Background()
	service.Start(ctx)
	defer service.Stop()

	task := &ExtractionTask{
		SessionID: "test-session",
		Content:   "Test content",
		Type:      ExtractionTypeUser,
		Priority:  1,
	}

	err := service.Submit(task)
	if err != nil {
		t.Fatalf("Failed to submit task: %v", err)
	}

	if task.ID == "" {
		t.Error("Expected task ID to be generated")
	}
	if task.Status != TaskStatusPending {
		t.Errorf("Expected status pending, got %s", task.Status)
	}
}

func TestExtractionService_Submit_NotRunning(t *testing.T) {
	service := NewExtractionService(2)

	task := &ExtractionTask{
		SessionID: "test-session",
		Content:   "Test content",
		Type:      ExtractionTypeUser,
	}

	err := service.Submit(task)
	if err == nil {
		t.Error("Expected error when submitting to non-running service")
	}
}

func TestExtractionService_GetTask(t *testing.T) {
	service := NewExtractionService(2)
	ctx := context.Background()
	service.Start(ctx)
	defer service.Stop()

	task := &ExtractionTask{
		ID:        "test-task",
		SessionID: "test-session",
		Content:   "Test content",
		Type:      ExtractionTypeUser,
	}

	service.Submit(task)

	retrieved, err := service.GetTask("test-task")
	if err != nil {
		t.Fatalf("Failed to get task: %v", err)
	}
	if retrieved.ID != "test-task" {
		t.Errorf("Expected task ID 'test-task', got %s", retrieved.ID)
	}
}

func TestExtractionService_GetTask_NotFound(t *testing.T) {
	service := NewExtractionService(2)
	ctx := context.Background()
	service.Start(ctx)
	defer service.Stop()

	_, err := service.GetTask("nonexistent")
	if err == nil {
		t.Error("Expected error for nonexistent task")
	}
}

func TestExtractionService_ListTasks(t *testing.T) {
	service := NewExtractionService(2)
	ctx := context.Background()
	service.Start(ctx)
	defer service.Stop()

	task1 := &ExtractionTask{ID: "task1", SessionID: "session1", Content: "Content 1", Type: ExtractionTypeUser}
	task2 := &ExtractionTask{ID: "task2", SessionID: "session2", Content: "Content 2", Type: ExtractionTypeFeedback}

	service.Submit(task1)
	service.Submit(task2)

	tasks := service.ListTasks()
	if len(tasks) != 2 {
		t.Errorf("Expected 2 tasks, got %d", len(tasks))
	}
}

func TestExtractionService_ProcessTask(t *testing.T) {
	service := NewExtractionService(2)
	ctx := context.Background()
	service.Start(ctx)
	defer service.Stop()

	task := &ExtractionTask{
		ID:        "test-task",
		SessionID: "test-session",
		Content:   "Test content",
		Type:      ExtractionTypeUser,
	}

	service.Submit(task)

	// Wait for processing
	time.Sleep(100 * time.Millisecond)

	retrieved, _ := service.GetTask("test-task")
	if retrieved.Status != TaskStatusCompleted {
		t.Errorf("Expected status completed, got %s", retrieved.Status)
	}
	if retrieved.Result == nil {
		t.Error("Expected result to be set")
	}
}

func TestExtractionService_Stats(t *testing.T) {
	service := NewExtractionService(2)
	ctx := context.Background()
	service.Start(ctx)
	defer service.Stop()

	task1 := &ExtractionTask{ID: "task1", SessionID: "session1", Content: "Content 1", Type: ExtractionTypeUser}
	task2 := &ExtractionTask{ID: "task2", SessionID: "session2", Content: "Content 2", Type: ExtractionTypeFeedback}

	service.Submit(task1)
	service.Submit(task2)

	stats := service.Stats()
	if stats.TotalTasks != 2 {
		t.Errorf("Expected 2 total tasks, got %d", stats.TotalTasks)
	}
}

func TestExtractionService_Cleanup(t *testing.T) {
	service := NewExtractionService(2)
	ctx := context.Background()
	service.Start(ctx)
	defer service.Stop()

	task := &ExtractionTask{
		ID:        "old-task",
		SessionID: "test-session",
		Content:   "Test content",
		Type:      ExtractionTypeUser,
	}

	service.Submit(task)

	// Wait for completion
	time.Sleep(100 * time.Millisecond)

	// Manually set completion time to old
	service.mu.Lock()
	oldTime := time.Now().Add(-2 * time.Hour)
	service.tasks["old-task"].CompletedAt = &oldTime
	service.mu.Unlock()

	removed := service.Cleanup(1 * time.Hour)
	if removed != 1 {
		t.Errorf("Expected 1 removed task, got %d", removed)
	}

	_, err := service.GetTask("old-task")
	if err == nil {
		t.Error("Expected task to be removed")
	}
}
