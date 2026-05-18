package tasks

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGenerateTaskID(t *testing.T) {
	tests := []struct {
		name     string
		taskType TaskType
		prefix   string
	}{
		{"local_bash", TaskTypeLocalBash, "b"},
		{"local_agent", TaskTypeLocalAgent, "a"},
		{"remote_agent", TaskTypeRemoteAgent, "r"},
		{"in_process_teammate", TaskTypeInProcessTeammate, "t"},
		{"local_workflow", TaskTypeLocalWorkflow, "w"},
		{"monitor_mcp", TaskTypeMonitorMCP, "m"},
		{"dream", TaskTypeDream, "d"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, err := GenerateTaskID(tt.taskType)
			if err != nil {
				t.Fatalf("GenerateTaskID() error = %v", err)
			}

			if len(id) != 9 {
				t.Errorf("GenerateTaskID() length = %d, want 9", len(id))
			}

			if id[0] != tt.prefix[0] {
				t.Errorf("GenerateTaskID() prefix = %c, want %c", id[0], tt.prefix[0])
			}

			// Check all characters are from alphabet
			for i := 1; i < len(id); i++ {
				found := false
				for j := 0; j < len(taskIDAlphabet); j++ {
					if id[i] == taskIDAlphabet[j] {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("GenerateTaskID() contains invalid character: %c", id[i])
				}
			}
		})
	}
}

func TestGenerateMainSessionTaskID(t *testing.T) {
	id, err := GenerateMainSessionTaskID()
	if err != nil {
		t.Fatalf("GenerateMainSessionTaskID() error = %v", err)
	}

	if len(id) != 9 {
		t.Errorf("GenerateMainSessionTaskID() length = %d, want 9", len(id))
	}

	if id[0] != 's' {
		t.Errorf("GenerateMainSessionTaskID() prefix = %c, want s", id[0])
	}
}

func TestIsTerminalTaskStatus(t *testing.T) {
	tests := []struct {
		name     string
		status   TaskStatus
		expected bool
	}{
		{"pending", TaskStatusPending, false},
		{"running", TaskStatusRunning, false},
		{"completed", TaskStatusCompleted, true},
		{"failed", TaskStatusFailed, true},
		{"killed", TaskStatusKilled, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsTerminalTaskStatus(tt.status)
			if result != tt.expected {
				t.Errorf("IsTerminalTaskStatus(%s) = %v, want %v", tt.status, result, tt.expected)
			}
		})
	}
}

func TestIsBackgroundTask(t *testing.T) {
	tests := []struct {
		name     string
		task     TaskState
		expected bool
	}{
		{
			name: "running backgrounded",
			task: &LocalShellTaskState{
				TaskStateBase: TaskStateBase{
					Status: TaskStatusRunning,
				},
				IsBackgrounded: true,
			},
			expected: true,
		},
		{
			name: "running foreground",
			task: &LocalShellTaskState{
				TaskStateBase: TaskStateBase{
					Status: TaskStatusRunning,
				},
				IsBackgrounded: false,
			},
			expected: false,
		},
		{
			name: "completed backgrounded",
			task: &LocalShellTaskState{
				TaskStateBase: TaskStateBase{
					Status: TaskStatusCompleted,
				},
				IsBackgrounded: true,
			},
			expected: false,
		},
		{
			name: "pending backgrounded",
			task: &LocalShellTaskState{
				TaskStateBase: TaskStateBase{
					Status: TaskStatusPending,
				},
				IsBackgrounded: true,
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsBackgroundTask(tt.task)
			if result != tt.expected {
				t.Errorf("IsBackgroundTask() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestTaskRegistry(t *testing.T) {
	registry := NewTaskRegistry()

	// Create a mock task
	mockTask := &mockTask{
		name:     "test",
		taskType: TaskTypeLocalBash,
	}

	// Register task
	registry.Register(mockTask)

	// Get task
	task, ok := registry.Get(TaskTypeLocalBash)
	if !ok {
		t.Fatal("Registry.Get() task not found")
	}

	if task.GetName() != "test" {
		t.Errorf("Registry.Get() name = %s, want test", task.GetName())
	}

	// Get non-existent task
	_, ok = registry.Get(TaskTypeLocalAgent)
	if ok {
		t.Error("Registry.Get() found non-existent task")
	}

	// Get all tasks
	all := registry.GetAll()
	if len(all) != 1 {
		t.Errorf("Registry.GetAll() length = %d, want 1", len(all))
	}
}

func TestTaskManager(t *testing.T) {
	manager := NewTaskManager()

	// Create a mock task state
	taskState := &LocalShellTaskState{
		TaskStateBase: TaskStateBase{
			ID:          "test123",
			Type:        TaskTypeLocalBash,
			Status:      TaskStatusRunning,
			Description: "test task",
		},
		IsBackgrounded: true,
	}

	// Add task
	manager.AddTask(taskState)

	// Get task
	task, ok := manager.GetTask("test123")
	if !ok {
		t.Fatal("TaskManager.GetTask() task not found")
	}

	if task.GetID() != "test123" {
		t.Errorf("TaskManager.GetTask() ID = %s, want test123", task.GetID())
	}

	// Get running tasks
	running := manager.GetRunningTasks()
	if len(running) != 1 {
		t.Errorf("TaskManager.GetRunningTasks() length = %d, want 1", len(running))
	}

	// Get background tasks
	background := manager.GetBackgroundTasks()
	if len(background) != 1 {
		t.Errorf("TaskManager.GetBackgroundTasks() length = %d, want 1", len(background))
	}

	// Remove task
	manager.RemoveTask("test123")

	// Verify removed
	_, ok = manager.GetTask("test123")
	if ok {
		t.Error("TaskManager.GetTask() found removed task")
	}
}

// Mock task implementation for testing
type mockTask struct {
	name     string
	taskType TaskType
}

func (m *mockTask) GetName() string {
	return m.name
}

func (m *mockTask) GetType() TaskType {
	return m.taskType
}

func (m *mockTask) Kill(taskId string, setAppState func(updater func(prev interface{}) interface{})) error {
	return nil
}

func TestTaskManagerStartLocalAgentCompletesAndWritesOutput(t *testing.T) {
	manager := NewTaskManager()
	task, err := manager.StartLocalAgent(context.Background(), StartLocalAgentOptions{
		Prompt:         "inspect files",
		Description:    "inspect",
		AgentType:      "explore",
		Model:          "sonnet",
		OutputFile:     t.TempDir() + "/agent.output",
		IsBackgrounded: true,
		Runner: func(_ context.Context, req LocalAgentRunRequest) (string, error) {
			if req.Prompt != "inspect files" || req.AgentType != "explore" {
				t.Fatalf("unexpected request: %+v", req)
			}
			return "done", nil
		},
	})
	if err != nil {
		t.Fatalf("StartLocalAgent() error = %v", err)
	}
	waitForTaskStatus(t, manager, task.ID, TaskStatusCompleted)
	got, ok := manager.GetTask(task.ID)
	if !ok {
		t.Fatalf("task not found")
	}
	agentTask := got.(*LocalAgentTaskState)
	if agentTask.Result == nil || agentTask.Result.Output != "done" {
		t.Fatalf("unexpected result: %+v", agentTask.Result)
	}
	output, err := ReadTaskOutput(agentTask.OutputFile, 0, DefaultMaxReadBytes)
	if err != nil {
		t.Fatalf("ReadTaskOutput() error = %v", err)
	}
	if output != "done" {
		t.Fatalf("unexpected output: %q", output)
	}
}

func TestTaskManagerLocalAgentFailureAndMessages(t *testing.T) {
	manager := NewTaskManager()
	task, err := manager.StartLocalAgent(context.Background(), StartLocalAgentOptions{
		Prompt:     "inspect files",
		OutputFile: t.TempDir() + "/agent.output",
		Runner: func(_ context.Context, req LocalAgentRunRequest) (string, error) {
			return "", errors.New("boom")
		},
	})
	if err != nil {
		t.Fatalf("StartLocalAgent() error = %v", err)
	}
	waitForTaskStatus(t, manager, task.ID, TaskStatusFailed)
	got, _ := manager.GetTask(task.ID)
	agentTask := got.(*LocalAgentTaskState)
	if agentTask.Result == nil || !strings.Contains(agentTask.Result.Error, "boom") {
		t.Fatalf("unexpected failed result: %+v", agentTask.Result)
	}

	running, err := manager.StartLocalAgent(context.Background(), StartLocalAgentOptions{
		Prompt: "wait",
		Runner: func(ctx context.Context, req LocalAgentRunRequest) (string, error) {
			<-ctx.Done()
			return "", ctx.Err()
		},
	})
	if err != nil {
		t.Fatalf("StartLocalAgent() running error = %v", err)
	}
	if err := manager.QueueLocalAgentMessage(running.ID, "continue"); err != nil {
		t.Fatalf("QueueLocalAgentMessage() error = %v", err)
	}
	messages, err := manager.DrainLocalAgentMessages(running.ID)
	if err != nil {
		t.Fatalf("DrainLocalAgentMessages() error = %v", err)
	}
	if len(messages) != 1 || messages[0] != "continue" {
		t.Fatalf("unexpected messages: %#v", messages)
	}
	if err := manager.KillTask(running.ID, func(updater func(prev interface{}) interface{}) {}); err != nil {
		t.Fatalf("KillTask() error = %v", err)
	}
	waitForTaskStatus(t, manager, running.ID, TaskStatusKilled)
}

func TestTaskManagerInProcessTeammateLifecycleAndMessages(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	manager := NewTaskManager()
	requests := make(chan InProcessTeammateRunRequest, 1)
	task, err := manager.StartInProcessTeammate(context.Background(), StartInProcessTeammateOptions{
		Prompt:      "help with auth",
		Description: "auth teammate",
		Name:        "reviewer",
		TeamName:    "alpha",
		AgentType:   "worker",
		OutputFile:  t.TempDir() + "/teammate.output",
		Runner: func(_ context.Context, req InProcessTeammateRunRequest) (string, error) {
			requests <- req
			return "teammate done", nil
		},
	})
	if err != nil {
		t.Fatalf("StartInProcessTeammate() error = %v", err)
	}
	req := <-requests
	if req.TeammateID != "reviewer@alpha" || req.Name != "reviewer" || req.TeamName != "alpha" {
		t.Fatalf("unexpected teammate request: %+v", req)
	}
	waitForTaskStatus(t, manager, task.ID, TaskStatusCompleted)
	got, _ := manager.GetTask(task.ID)
	teammate := got.(*InProcessTeammateTaskState)
	if teammate.Result == nil || teammate.Result.Output != "teammate done" {
		t.Fatalf("unexpected teammate result: %+v", teammate.Result)
	}

	running, err := manager.StartInProcessTeammate(context.Background(), StartInProcessTeammateOptions{
		Prompt:   "wait",
		Name:     "builder",
		TeamName: "alpha",
		Runner: func(ctx context.Context, req InProcessTeammateRunRequest) (string, error) {
			<-ctx.Done()
			return "", ctx.Err()
		},
	})
	if err != nil {
		t.Fatalf("StartInProcessTeammate() running error = %v", err)
	}
	if err := manager.QueueInProcessTeammateMessage("builder@alpha", "continue"); err != nil {
		t.Fatalf("QueueInProcessTeammateMessage() error = %v", err)
	}
	messages, err := manager.DrainInProcessTeammateMessages(running.ID)
	if err != nil {
		t.Fatalf("DrainInProcessTeammateMessages() error = %v", err)
	}
	if len(messages) != 1 || messages[0] != "continue" {
		t.Fatalf("unexpected teammate messages: %#v", messages)
	}
	if err := manager.KillTask(running.ID, func(updater func(prev interface{}) interface{}) {}); err != nil {
		t.Fatalf("KillTask() error = %v", err)
	}
	waitForTaskStatus(t, manager, running.ID, TaskStatusKilled)
}

func TestTaskManagerSnapshotRestoreMarksRunningTasksKilled(t *testing.T) {
	manager := NewTaskManager()
	block := make(chan struct{})
	task, err := manager.StartLocalAgent(context.Background(), StartLocalAgentOptions{
		Prompt:         "long task",
		Description:    "Long task",
		AgentType:      "worker",
		WorkingDir:     "/tmp/work",
		IsBackgrounded: true,
		Runner: func(ctx context.Context, _ LocalAgentRunRequest) (string, error) {
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-block:
				return "done", nil
			}
		},
	})
	if err != nil {
		t.Fatalf("StartLocalAgent() error = %v", err)
	}
	snapshot := manager.ExportSnapshot()
	restored := NewTaskManager()
	if err := restored.RestoreSnapshot(snapshot); err != nil {
		t.Fatalf("RestoreSnapshot() error = %v", err)
	}
	restoredTask, ok := restored.GetTask(task.ID)
	if !ok {
		t.Fatalf("restored task not found")
	}
	if restoredTask.GetStatus() != TaskStatusKilled {
		t.Fatalf("expected running task to restore as killed, got %s", restoredTask.GetStatus())
	}
	local := restoredTask.(*LocalAgentTaskState)
	if local.WorkingDir != "/tmp/work" || local.AgentType != "worker" {
		t.Fatalf("restored task lost metadata: %#v", local)
	}
	close(block)
}

func TestTaskManagerEmitsTerminalEvents(t *testing.T) {
	manager := NewTaskManager()
	events, unsubscribe := manager.SubscribeTerminalEvents(1)
	defer unsubscribe()
	task, err := manager.StartLocalAgent(context.Background(), StartLocalAgentOptions{
		Prompt:         "short task",
		Description:    "Short task",
		AgentType:      "worker",
		WorkingDir:     "/tmp/work",
		IsBackgrounded: true,
		Runner: func(context.Context, LocalAgentRunRequest) (string, error) {
			return "done", nil
		},
	})
	if err != nil {
		t.Fatalf("StartLocalAgent() error = %v", err)
	}
	select {
	case got := <-events:
		if got != task.ID {
			t.Fatalf("unexpected terminal event %q, want %q", got, task.ID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for terminal event")
	}
}

func TestTaskManagerLoadSnapshotIfExistsAndRuntimePath(t *testing.T) {
	manager := NewTaskManager()
	path := filepath.Join(t.TempDir(), "missing", "tasks.json")
	if err := manager.LoadSnapshotIfExists(path); err != nil {
		t.Fatalf("LoadSnapshotIfExists missing path error = %v", err)
	}

	task, err := manager.StartInProcessTeammate(context.Background(), StartInProcessTeammateOptions{
		Prompt:         "work",
		Description:    "Worker",
		Name:           "reviewer",
		TeamName:       "alpha",
		AgentType:      "reviewer",
		WorkingDir:     "/tmp/work",
		IsBackgrounded: true,
		Runner: func(context.Context, InProcessTeammateRunRequest) (string, error) {
			return "done", nil
		},
	})
	if err != nil {
		t.Fatalf("StartInProcessTeammate() error = %v", err)
	}
	waitForTaskStatus(t, manager, task.ID, TaskStatusCompleted)
	if err := manager.SaveSnapshot(path); err != nil {
		t.Fatalf("SaveSnapshot() error = %v", err)
	}

	restored := NewTaskManager()
	if err := restored.LoadSnapshotIfExists(path); err != nil {
		t.Fatalf("LoadSnapshotIfExists() error = %v", err)
	}
	restoredTask, ok := restored.GetTask(task.ID)
	if !ok {
		t.Fatalf("restored task missing")
	}
	teammate := restoredTask.(*InProcessTeammateTaskState)
	if teammate.Name != "reviewer" || teammate.Result == nil || teammate.Result.Output != "done" {
		t.Fatalf("unexpected restored teammate: %#v", teammate)
	}

	first := RuntimeSnapshotPath(t.TempDir(), "/tmp/project-a")
	second := RuntimeSnapshotPath(t.TempDir(), "/tmp/project-a")
	if filepath.Base(first) != filepath.Base(second) {
		t.Fatalf("expected stable snapshot basename, got %q and %q", first, second)
	}
}

func waitForTaskStatus(t *testing.T, manager *TaskManager, taskID string, status TaskStatus) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		task, ok := manager.GetTask(taskID)
		if ok && task.GetStatus() == status {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	task, _ := manager.GetTask(taskID)
	t.Fatalf("task %s did not reach %s, got %#v", taskID, status, task)
}
