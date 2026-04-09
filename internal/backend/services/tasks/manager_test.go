package tasks

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateTaskID(t *testing.T) {
	tests := []struct {
		name       string
		taskType   TaskType
		wantPrefix string
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
			id := GenerateTaskID(tt.taskType)
			assert.NotEmpty(t, id)
			assert.Equal(t, tt.wantPrefix, string(id[0]))
			assert.Equal(t, 9, len(id)) // prefix + 8 chars
		})
	}
}

func TestGenerateTaskID_Uniqueness(t *testing.T) {
	ids := make(map[string]bool)
	for i := 0; i < 1000; i++ {
		id := GenerateTaskID(TaskTypeLocalBash)
		assert.False(t, ids[id], "duplicate ID generated: %s", id)
		ids[id] = true
	}
}

func TestTaskStatus_IsTerminal(t *testing.T) {
	tests := []struct {
		status   TaskStatus
		terminal bool
	}{
		{TaskStatusPending, false},
		{TaskStatusRunning, false},
		{TaskStatusCompleted, true},
		{TaskStatusFailed, true},
		{TaskStatusKilled, true},
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			assert.Equal(t, tt.terminal, tt.status.IsTerminal())
		})
	}
}

func TestNewTaskState(t *testing.T) {
	task := NewTaskState(TaskTypeLocalBash, "test command", "tool-123", "/tmp/output.txt")

	assert.NotEmpty(t, task.ID)
	assert.Equal(t, TaskTypeLocalBash, task.Type)
	assert.Equal(t, TaskStatusPending, task.Status)
	assert.Equal(t, "test command", task.Description)
	assert.Equal(t, "tool-123", task.ToolUseID)
	assert.Equal(t, "/tmp/output.txt", task.OutputFile)
	assert.Greater(t, task.StartTime, int64(0))
	assert.Equal(t, int64(0), task.OutputOffset)
	assert.False(t, task.Notified)
}

func TestManager_AddAndGetTask(t *testing.T) {
	manager := NewManager()
	task := NewTaskState(TaskTypeLocalBash, "test", "", "/tmp/out")

	manager.AddTask(task, nil)

	retrieved, exists := manager.GetTask(task.ID)
	require.True(t, exists)
	assert.Equal(t, task.ID, retrieved.ID)
	assert.Equal(t, task.Description, retrieved.Description)
}

func TestManager_UpdateTaskStatus(t *testing.T) {
	manager := NewManager()
	task := NewTaskState(TaskTypeLocalBash, "test", "", "/tmp/out")
	manager.AddTask(task, nil)

	err := manager.UpdateTaskStatus(task.ID, TaskStatusRunning)
	require.NoError(t, err)

	retrieved, _ := manager.GetTask(task.ID)
	assert.Equal(t, TaskStatusRunning, retrieved.Status)

	// Update to terminal status
	err = manager.UpdateTaskStatus(task.ID, TaskStatusCompleted)
	require.NoError(t, err)

	retrieved, _ = manager.GetTask(task.ID)
	assert.Equal(t, TaskStatusCompleted, retrieved.Status)
	assert.Greater(t, retrieved.EndTime, int64(0))
}

func TestManager_UpdateTaskStatus_NotFound(t *testing.T) {
	manager := NewManager()
	err := manager.UpdateTaskStatus("nonexistent", TaskStatusRunning)
	assert.ErrorIs(t, err, ErrTaskNotFound)
}

func TestManager_KillTask(t *testing.T) {
	manager := NewManager()
	task := NewTaskState(TaskTypeLocalBash, "test", "", "/tmp/out")

	ctx, cancel := context.WithCancel(context.Background())
	manager.AddTask(task, cancel)
	manager.UpdateTaskStatus(task.ID, TaskStatusRunning)

	err := manager.KillTask(task.ID)
	require.NoError(t, err)

	retrieved, _ := manager.GetTask(task.ID)
	assert.Equal(t, TaskStatusKilled, retrieved.Status)

	// Verify context was cancelled
	select {
	case <-ctx.Done():
		// Expected
	case <-time.After(100 * time.Millisecond):
		t.Fatal("context was not cancelled")
	}
}

func TestManager_KillTask_AlreadyTerminated(t *testing.T) {
	manager := NewManager()
	task := NewTaskState(TaskTypeLocalBash, "test", "", "/tmp/out")
	manager.AddTask(task, nil)
	manager.UpdateTaskStatus(task.ID, TaskStatusCompleted)

	err := manager.KillTask(task.ID)
	assert.ErrorIs(t, err, ErrTaskAlreadyTerminated)
}

func TestManager_RemoveTask(t *testing.T) {
	manager := NewManager()
	task := NewTaskState(TaskTypeLocalBash, "test", "", "/tmp/out")
	manager.AddTask(task, nil)

	manager.RemoveTask(task.ID)

	_, exists := manager.GetTask(task.ID)
	assert.False(t, exists)
}

func TestManager_ListTasks(t *testing.T) {
	manager := NewManager()

	task1 := NewTaskState(TaskTypeLocalBash, "test1", "", "/tmp/out1")
	task2 := NewTaskState(TaskTypeLocalAgent, "test2", "", "/tmp/out2")

	manager.AddTask(task1, nil)
	manager.AddTask(task2, nil)

	tasks := manager.ListTasks()
	assert.Len(t, tasks, 2)
}

func TestManager_ListTasksByType(t *testing.T) {
	manager := NewManager()

	task1 := NewTaskState(TaskTypeLocalBash, "test1", "", "/tmp/out1")
	task2 := NewTaskState(TaskTypeLocalAgent, "test2", "", "/tmp/out2")
	task3 := NewTaskState(TaskTypeLocalBash, "test3", "", "/tmp/out3")

	manager.AddTask(task1, nil)
	manager.AddTask(task2, nil)
	manager.AddTask(task3, nil)

	bashTasks := manager.ListTasksByType(TaskTypeLocalBash)
	assert.Len(t, bashTasks, 2)

	agentTasks := manager.ListTasksByType(TaskTypeLocalAgent)
	assert.Len(t, agentTasks, 1)
}

func TestManager_ListTasksByStatus(t *testing.T) {
	manager := NewManager()

	task1 := NewTaskState(TaskTypeLocalBash, "test1", "", "/tmp/out1")
	task2 := NewTaskState(TaskTypeLocalBash, "test2", "", "/tmp/out2")

	manager.AddTask(task1, nil)
	manager.AddTask(task2, nil)
	manager.UpdateTaskStatus(task1.ID, TaskStatusRunning)

	runningTasks := manager.ListTasksByStatus(TaskStatusRunning)
	assert.Len(t, runningTasks, 1)

	pendingTasks := manager.ListTasksByStatus(TaskStatusPending)
	assert.Len(t, pendingTasks, 1)
}

func TestManager_CleanupTerminatedTasks(t *testing.T) {
	manager := NewManager()

	task1 := NewTaskState(TaskTypeLocalBash, "test1", "", "/tmp/out1")
	task2 := NewTaskState(TaskTypeLocalBash, "test2", "", "/tmp/out2")
	task3 := NewTaskState(TaskTypeLocalBash, "test3", "", "/tmp/out3")

	manager.AddTask(task1, nil)
	manager.AddTask(task2, nil)
	manager.AddTask(task3, nil)

	manager.UpdateTaskStatus(task1.ID, TaskStatusCompleted)
	manager.UpdateTaskStatus(task2.ID, TaskStatusFailed)
	// task3 remains pending

	count := manager.CleanupTerminatedTasks()
	assert.Equal(t, 2, count)

	tasks := manager.ListTasks()
	assert.Len(t, tasks, 1)
	assert.Equal(t, task3.ID, tasks[0].ID)
}

func TestManager_SetNotified(t *testing.T) {
	manager := NewManager()
	task := NewTaskState(TaskTypeLocalBash, "test", "", "/tmp/out")
	manager.AddTask(task, nil)

	err := manager.SetNotified(task.ID)
	require.NoError(t, err)

	retrieved, _ := manager.GetTask(task.ID)
	assert.True(t, retrieved.Notified)
}

func TestManager_UpdateOutputOffset(t *testing.T) {
	manager := NewManager()
	task := NewTaskState(TaskTypeLocalBash, "test", "", "/tmp/out")
	manager.AddTask(task, nil)

	err := manager.UpdateOutputOffset(task.ID, 1024)
	require.NoError(t, err)

	retrieved, _ := manager.GetTask(task.ID)
	assert.Equal(t, int64(1024), retrieved.OutputOffset)
}

func TestManager_ConcurrentAccess(t *testing.T) {
	manager := NewManager()

	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func(idx int) {
			task := NewTaskState(TaskTypeLocalBash, "test", "", "/tmp/out")
			manager.AddTask(task, nil)
			manager.UpdateTaskStatus(task.ID, TaskStatusRunning)
			manager.UpdateTaskStatus(task.ID, TaskStatusCompleted)
			done <- true
		}(i)
	}

	for i := 0; i < 10; i++ {
		<-done
	}

	tasks := manager.ListTasks()
	assert.Len(t, tasks, 10)
}
