package coordinator

import (
	"context"
	"strings"
	"testing"
	"time"

	coretasks "claude-codex/internal/harness/tasks"
)

func TestCoordinatorMode(t *testing.T) {
	t.Run("IsCoordinatorMode", func(t *testing.T) {
		// Save original env
		original := GetCurrentMode()
		defer func() {
			if original == ModeCoordinator {
				t.Setenv(EnvCoordinatorMode, "1")
			} else {
				t.Setenv(EnvCoordinatorMode, "")
			}
		}()

		// Test disabled
		t.Setenv(EnvCoordinatorMode, "")
		if IsCoordinatorMode() {
			t.Error("Expected coordinator mode to be disabled")
		}

		// Test enabled
		t.Setenv(EnvCoordinatorMode, "1")
		if !IsCoordinatorMode() {
			t.Error("Expected coordinator mode to be enabled")
		}
	})

	t.Run("MatchSessionMode", func(t *testing.T) {
		// Test no stored mode
		msg := MatchSessionMode("")
		if msg != "" {
			t.Errorf("Expected empty message for no stored mode, got: %s", msg)
		}

		// Test mode switch to coordinator
		t.Setenv(EnvCoordinatorMode, "")
		msg = MatchSessionMode(ModeCoordinator)
		if msg == "" {
			t.Error("Expected message when switching to coordinator mode")
		}
		if !IsCoordinatorMode() {
			t.Error("Expected coordinator mode to be enabled after switch")
		}

		// Test mode switch to normal
		msg = MatchSessionMode(ModeNormal)
		if msg == "" {
			t.Error("Expected message when switching to normal mode")
		}
		if IsCoordinatorMode() {
			t.Error("Expected coordinator mode to be disabled after switch")
		}
	})
}

func TestGetWorkerTools(t *testing.T) {
	allTools := []string{
		"Agent", "SendMessage", "Bash", "Read", "Edit", "Write",
		"TeamCreate", "TeamDelete", "SyntheticOutput",
	}

	t.Run("SimpleMode", func(t *testing.T) {
		tools := GetWorkerTools(true, allTools)
		expected := []string{BashToolName, FileReadToolName, FileEditToolName}

		if len(tools) != len(expected) {
			t.Errorf("Expected %d tools, got %d", len(expected), len(tools))
		}

		for i, tool := range tools {
			if tool != expected[i] {
				t.Errorf("Expected tool %s at index %d, got %s", expected[i], i, tool)
			}
		}
	})

	t.Run("FullMode", func(t *testing.T) {
		tools := GetWorkerTools(false, allTools)

		// Should filter out internal tools
		for _, tool := range tools {
			if internalWorkerTools[tool] {
				t.Errorf("Internal tool %s should not be in worker tools", tool)
			}
		}

		// Should include non-internal tools
		if len(tools) == 0 {
			t.Error("Expected some tools in full mode")
		}
	})
}

func TestGetCoordinatorUserContext(t *testing.T) {
	t.Run("DisabledMode", func(t *testing.T) {
		t.Setenv(EnvCoordinatorMode, "")

		ctx := GetCoordinatorUserContext([]MCPClient{}, "", []string{})
		if len(ctx) != 0 {
			t.Error("Expected empty context when coordinator mode is disabled")
		}
	})

	t.Run("EnabledMode", func(t *testing.T) {
		t.Setenv(EnvCoordinatorMode, "1")

		mcpClients := []MCPClient{
			{Name: "server1"},
			{Name: "server2"},
		}
		allTools := []string{"Bash", "Read", "Edit"}

		ctx := GetCoordinatorUserContext(mcpClients, "/tmp/scratchpad", allTools)

		if len(ctx) == 0 {
			t.Error("Expected non-empty context when coordinator mode is enabled")
		}

		content, ok := ctx["workerToolsContext"]
		if !ok {
			t.Error("Expected workerToolsContext in context")
		}

		// Check content includes tools
		if content == "" {
			t.Error("Expected non-empty content")
		}
	})
}

func TestGetCoordinatorSystemPrompt(t *testing.T) {
	t.Run("SimpleMode", func(t *testing.T) {
		t.Setenv(EnvSimpleMode, "1")

		prompt := GetCoordinatorSystemPrompt()
		if prompt == "" {
			t.Error("Expected non-empty system prompt")
		}

		// Should mention limited tools
		if !contains(prompt, "Bash") {
			t.Error("Expected prompt to mention Bash tool")
		}
	})

	t.Run("FullMode", func(t *testing.T) {
		t.Setenv(EnvSimpleMode, "")

		prompt := GetCoordinatorSystemPrompt()
		if prompt == "" {
			t.Error("Expected non-empty system prompt")
		}

		// Should mention skills
		if !contains(prompt, "skills") && !contains(prompt, "Skill") {
			t.Error("Expected prompt to mention skills in full mode")
		}
		if !contains(prompt, TaskOutputToolName) {
			t.Error("Expected prompt to mention TaskOutput tool")
		}
	})
}

func TestCoordinatorTaskNotificationAndTools(t *testing.T) {
	notification := FormatTaskNotification(TaskNotification{
		TaskID:      "agent-1",
		Status:      "completed",
		Summary:     "done <ok>",
		Result:      "changed & verified",
		TotalTokens: 12,
		ToolUses:    3,
		DurationMS:  45,
	})
	for _, want := range []string{
		"<task-notification>",
		"<task-id>agent-1</task-id>",
		"<status>completed</status>",
		"<summary>done &lt;ok&gt;</summary>",
		"<result>changed &amp; verified</result>",
		"<total_tokens>12</total_tokens>",
	} {
		if !contains(notification, want) {
			t.Fatalf("notification missing %q: %s", want, notification)
		}
	}

	tools := CoordinatorToolNames()
	for _, want := range []string{AgentToolName, SendMessageToolName, TaskStopToolName, TaskOutputToolName, TeamCreateToolName, TeamDeleteToolName} {
		found := false
		for _, got := range tools {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("CoordinatorToolNames missing %s: %#v", want, tools)
		}
	}
}

func TestDrainTaskNotificationsFormatsTerminalRuntimeTasksOnce(t *testing.T) {
	manager := coretasks.NewTaskManager()
	task, err := manager.StartLocalAgent(context.Background(), coretasks.StartLocalAgentOptions{
		Prompt:         "inspect auth",
		Description:    "Inspect auth",
		AgentType:      "worker",
		IsBackgrounded: true,
		Runner: func(context.Context, coretasks.LocalAgentRunRequest) (string, error) {
			return "done", nil
		},
	})
	if err != nil {
		t.Fatalf("StartLocalAgent() error = %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if current, ok := manager.GetTask(task.ID); ok && coretasks.IsTerminalTaskStatus(current.GetStatus()) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	notifications := DrainTaskNotifications(manager)
	if len(notifications) != 1 {
		t.Fatalf("expected one notification, got %#v", notifications)
	}
	if !strings.Contains(notifications[0], "<task-notification>") || !strings.Contains(notifications[0], "done") {
		t.Fatalf("unexpected notification: %s", notifications[0])
	}
	if again := DrainTaskNotifications(manager); len(again) != 0 {
		t.Fatalf("notification should only drain once, got %#v", again)
	}
}

func TestForwardTaskNotificationsWakesOnTerminalEvent(t *testing.T) {
	manager := coretasks.NewTaskManager()
	out := make(chan string, 1)
	stop := ForwardTaskNotifications(manager, out)
	defer stop()

	task, err := manager.StartLocalAgent(context.Background(), coretasks.StartLocalAgentOptions{
		Prompt:         "work",
		Description:    "Worker",
		AgentType:      "worker",
		WorkingDir:     "/tmp/work",
		IsBackgrounded: true,
		Runner: func(context.Context, coretasks.LocalAgentRunRequest) (string, error) {
			return "done", nil
		},
	})
	if err != nil {
		t.Fatalf("StartLocalAgent() error = %v", err)
	}

	select {
	case notification := <-out:
		if !strings.Contains(notification, "<task-notification>") || !strings.Contains(notification, task.AgentID) {
			t.Fatalf("unexpected notification: %s", notification)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for forwarded notification")
	}
}

func TestManagerWorkerTracking(t *testing.T) {
	config := Config{
		Enabled:    true,
		SimpleMode: false,
	}
	manager := NewManager(config)

	t.Run("RegisterWorker", func(t *testing.T) {
		manager.RegisterWorker("agent-1", "Test worker")

		worker, ok := manager.GetWorker("agent-1")
		if !ok {
			t.Error("Expected to find registered worker")
		}
		if worker.AgentID != "agent-1" {
			t.Errorf("Expected agent ID 'agent-1', got %s", worker.AgentID)
		}
		if worker.Status != "running" {
			t.Errorf("Expected status 'running', got %s", worker.Status)
		}
	})

	t.Run("UpdateWorkerStatus", func(t *testing.T) {
		manager.UpdateWorkerStatus("agent-1", "completed")

		worker, ok := manager.GetWorker("agent-1")
		if !ok {
			t.Error("Expected to find worker")
		}
		if worker.Status != "completed" {
			t.Errorf("Expected status 'completed', got %s", worker.Status)
		}
		if worker.EndTime == nil {
			t.Error("Expected EndTime to be set for completed worker")
		}
	})

	t.Run("ListActiveWorkers", func(t *testing.T) {
		manager.RegisterWorker("agent-2", "Active worker")

		active := manager.ListActiveWorkers()
		if len(active) != 1 {
			t.Errorf("Expected 1 active worker, got %d", len(active))
		}
		if active[0].AgentID != "agent-2" {
			t.Errorf("Expected agent-2, got %s", active[0].AgentID)
		}
	})

	t.Run("RemoveWorker", func(t *testing.T) {
		manager.RemoveWorker("agent-1")

		_, ok := manager.GetWorker("agent-1")
		if ok {
			t.Error("Expected worker to be removed")
		}
	})
}

func TestGetWorkerContext(t *testing.T) {
	config := Config{
		Enabled:       true,
		SimpleMode:    false,
		ScratchpadDir: "/tmp/scratch",
		MCPClients: []MCPClient{
			{Name: "mcp1"},
			{Name: "mcp2"},
		},
	}
	manager := NewManager(config)

	allTools := []string{"Bash", "Read", "Edit", "Write"}
	ctx := manager.GetWorkerContext(allTools)

	if len(ctx.AvailableTools) == 0 {
		t.Error("Expected available tools")
	}
	if len(ctx.MCPServers) != 2 {
		t.Errorf("Expected 2 MCP servers, got %d", len(ctx.MCPServers))
	}
	if ctx.ScratchpadDir != "/tmp/scratch" {
		t.Errorf("Expected scratchpad dir '/tmp/scratch', got %s", ctx.ScratchpadDir)
	}
}

func contains(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 &&
		(s == substr || len(s) >= len(substr) &&
			(s[:len(substr)] == substr || s[len(s)-len(substr):] == substr ||
				findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
