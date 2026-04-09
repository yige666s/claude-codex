package agent

import (
	"testing"
	"time"
)

func TestAgentTypes(t *testing.T) {
	t.Run("AgentDefinition", func(t *testing.T) {
		def := &AgentDefinition{
			AgentType:    "test-agent",
			WhenToUse:    "For testing",
			Tools:        []string{"Read", "Write"},
			MaxTurns:     10,
			Model:        ModelSonnet,
			Permission:   PermissionDefault,
			Source:       SourceBuiltIn,
			SystemPrompt: "Test prompt",
		}

		if def.AgentType != "test-agent" {
			t.Errorf("Expected agent type 'test-agent', got %s", def.AgentType)
		}
		if len(def.Tools) != 2 {
			t.Errorf("Expected 2 tools, got %d", len(def.Tools))
		}
	})

	t.Run("AgentInstance", func(t *testing.T) {
		instance := &AgentInstance{
			ID:         "test-id",
			Type:       "test-agent",
			Model:      "claude-sonnet-4",
			StartTime:  time.Now(),
			Status:     StatusStarting,
			TurnCount:  0,
			MaxTurns:   10,
			WorkingDir: "/test",
			Tools:      []string{"Read"},
			Messages:   []Message{},
		}

		if instance.Status != StatusStarting {
			t.Errorf("Expected status Starting, got %s", instance.Status)
		}
		if instance.TurnCount != 0 {
			t.Errorf("Expected turn count 0, got %d", instance.TurnCount)
		}
	})
}

func TestFork(t *testing.T) {
	t.Run("IsInForkChild", func(t *testing.T) {
		// Message without fork boilerplate
		messages := []Message{
			{
				Role: "user",
				Content: []ContentBlock{
					{Type: "text", Text: "Hello"},
				},
			},
		}
		if IsInForkChild(messages) {
			t.Error("Should not detect fork boilerplate in normal message")
		}

		// Message with fork boilerplate
		messagesWithFork := []Message{
			{
				Role: "user",
				Content: []ContentBlock{
					{Type: "text", Text: "<fork-boilerplate>test</fork-boilerplate>"},
				},
			},
		}
		if !IsInForkChild(messagesWithFork) {
			t.Error("Should detect fork boilerplate")
		}
	})

	t.Run("BuildForkedMessages", func(t *testing.T) {
		assistantMsg := Message{
			Role: "assistant",
			Content: []ContentBlock{
				{Type: "text", Text: "I'll help with that"},
				{
					Type:     "tool_use",
					ToolName: "Read",
					ToolID:   "tool-1",
				},
			},
		}

		directive := "Read the config file"
		forkedMsgs := BuildForkedMessages(directive, assistantMsg)

		if len(forkedMsgs) != 2 {
			t.Errorf("Expected 2 messages, got %d", len(forkedMsgs))
		}

		// First should be assistant message
		if forkedMsgs[0].Role != "assistant" {
			t.Errorf("Expected first message to be assistant, got %s", forkedMsgs[0].Role)
		}

		// Second should be user message with tool results and directive
		if forkedMsgs[1].Role != "user" {
			t.Errorf("Expected second message to be user, got %s", forkedMsgs[1].Role)
		}
	})
}

func TestManager(t *testing.T) {
	t.Run("RegisterAndGetDefinition", func(t *testing.T) {
		executor := NewExecutor(nil)
		manager := NewManager(executor)

		def := &AgentDefinition{
			AgentType:    "test-agent",
			WhenToUse:    "For testing",
			Tools:        []string{"*"},
			MaxTurns:     10,
			Model:        ModelSonnet,
			Permission:   PermissionDefault,
			Source:       SourceBuiltIn,
			SystemPrompt: "Test",
		}

		err := manager.RegisterDefinition(def)
		if err != nil {
			t.Fatalf("Failed to register definition: %v", err)
		}

		retrieved, err := manager.GetDefinition("test-agent")
		if err != nil {
			t.Fatalf("Failed to get definition: %v", err)
		}

		if retrieved.AgentType != def.AgentType {
			t.Errorf("Expected agent type %s, got %s", def.AgentType, retrieved.AgentType)
		}
	})

	t.Run("ListDefinitions", func(t *testing.T) {
		executor := NewExecutor(nil)
		manager := NewManager(executor)

		def1 := &AgentDefinition{
			AgentType: "agent-1",
			Tools:     []string{"*"},
			Model:     ModelSonnet,
		}
		def2 := &AgentDefinition{
			AgentType: "agent-2",
			Tools:     []string{"*"},
			Model:     ModelHaiku,
		}

		manager.RegisterDefinition(def1)
		manager.RegisterDefinition(def2)

		defs := manager.ListDefinitions()
		if len(defs) != 2 {
			t.Errorf("Expected 2 definitions, got %d", len(defs))
		}
	})

	t.Run("InitializeBuiltInAgents", func(t *testing.T) {
		executor := NewExecutor(nil)
		manager := NewManager(executor)

		err := manager.InitializeBuiltInAgents()
		if err != nil {
			t.Fatalf("Failed to initialize built-in agents: %v", err)
		}

		// Check fork agent
		forkDef, err := manager.GetDefinition(FORK_SUBAGENT_TYPE)
		if err != nil {
			t.Errorf("Fork agent not registered: %v", err)
		}
		if forkDef.Model != ModelInherit {
			t.Errorf("Fork agent should inherit model, got %s", forkDef.Model)
		}

		// Check general-purpose agent
		_, err = manager.GetDefinition("general-purpose")
		if err != nil {
			t.Errorf("General-purpose agent not registered: %v", err)
		}

		// Check explore agent
		_, err = manager.GetDefinition("explore")
		if err != nil {
			t.Errorf("Explore agent not registered: %v", err)
		}
	})
}

func TestProgressTracker(t *testing.T) {
	t.Run("StartAndStopTracking", func(t *testing.T) {
		executor := NewExecutor(nil)
		tracker := NewProgressTracker(executor)

		agentID := AgentID("test-agent-1")

		// Start tracking
		tracker.StartTracking(agentID)

		// Should be tracking
		summary := tracker.GetSummary(agentID)
		if summary != "" {
			// Summary might be empty if agent not in executor
			t.Logf("Summary: %s", summary)
		}

		// Stop tracking
		tracker.StopTracking(agentID)

		// Give it a moment to clean up
		time.Sleep(10 * time.Millisecond)
	})
}

func TestExecutor(t *testing.T) {
	t.Run("CreateInstance", func(t *testing.T) {
		executor := NewExecutor(nil)

		def := &AgentDefinition{
			AgentType:  "test",
			Tools:      []string{"*"},
			MaxTurns:   50,
			Model:      ModelSonnet,
			Permission: PermissionDefault,
		}

		config := AgentConfig{
			Definition:    def,
			ParentModel:   "claude-sonnet-4",
			InitialPrompt: "Test prompt",
			WorkingDir:    "/test",
		}

		instance := executor.createInstance(config)

		if instance.Type != "test" {
			t.Errorf("Expected type 'test', got %s", instance.Type)
		}
		if instance.MaxTurns != 50 {
			t.Errorf("Expected max turns 50, got %d", instance.MaxTurns)
		}
		if instance.Status != StatusStarting {
			t.Errorf("Expected status Starting, got %s", instance.Status)
		}
	})

	t.Run("ListInstances", func(t *testing.T) {
		executor := NewExecutor(nil)

		instances := executor.ListInstances()
		if len(instances) != 0 {
			t.Errorf("Expected 0 instances, got %d", len(instances))
		}
	})
}

func TestHelperFunctions(t *testing.T) {
	t.Run("generateAgentID", func(t *testing.T) {
		id1 := generateAgentID()
		time.Sleep(1 * time.Millisecond) // Ensure different timestamps
		id2 := generateAgentID()

		if id1 == id2 {
			t.Error("Generated IDs should be unique")
		}
		if id1 == "" {
			t.Error("Generated ID should not be empty")
		}
	})

	t.Run("formatDuration", func(t *testing.T) {
		tests := []struct {
			duration time.Duration
			expected string
		}{
			{30 * time.Second, "30s"},
			{90 * time.Second, "1m30s"},
			{3600 * time.Second, "1h0m"},
		}

		for _, tt := range tests {
			result := formatDuration(tt.duration)
			if result != tt.expected {
				t.Errorf("formatDuration(%v) = %s, want %s", tt.duration, result, tt.expected)
			}
		}
	})
}
