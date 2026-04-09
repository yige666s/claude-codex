package tasks

import (
	"testing"
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
