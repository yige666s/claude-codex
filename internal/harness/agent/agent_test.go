package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
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
		if len(forkedMsgs[1].Content) < 2 {
			t.Fatalf("Expected tool result and directive, got %#v", forkedMsgs[1].Content)
		}
		result := forkedMsgs[1].Content[0]
		if result.Type != "tool_result" || result.ToolUseID != "tool-1" {
			t.Fatalf("Expected first user block to answer tool-1, got %#v", result)
		}
		if result.Result != ForkToolResultPlaceholder || result.IsError {
			t.Fatalf("Unexpected fork placeholder result: %#v", result)
		}
	})
}

func TestAgentConfigFromRunOptionsAppliesDefinitionExecutionFields(t *testing.T) {
	def := &AgentDefinition{
		AgentType:     "writer",
		Model:         ModelInherit,
		SystemPrompt:  "Base system",
		InitialPrompt: "Always be concise.",
		Memory:        "local",
		Skills:        []string{"review", "commit"},
		OmitClaudeMd:  true,
	}
	config, agentCtx, err := agentConfigFromRunOptions(AgentRunOptions{
		Definition:     def,
		Prompt:         "Summarize changes",
		AgentName:      "writer-1",
		TeamName:       "alpha",
		InvocationKind: InvocationTeammate,
		PendingMessages: func(context.Context) []string {
			return []string{"follow up"}
		},
	})
	if err != nil {
		t.Fatalf("agentConfigFromRunOptions() error = %v", err)
	}
	if !strings.Contains(config.InitialPrompt, "Always be concise.") || !strings.Contains(config.InitialPrompt, "Summarize changes") {
		t.Fatalf("initial prompt did not combine definition and user prompt: %q", config.InitialPrompt)
	}
	if config.SystemPrompt == nil || !strings.Contains(*config.SystemPrompt, "Memory policy: local") || !strings.Contains(*config.SystemPrompt, "Requested skills: review, commit") {
		t.Fatalf("system prompt missing definition execution context: %#v", config.SystemPrompt)
	}
	if config.PendingMessages == nil {
		t.Fatal("expected pending message provider to be carried into AgentConfig")
	}
	if agentCtx.InvocationKind != InvocationTeammate || agentCtx.SubagentName != "writer-1" || agentCtx.TeamName != "alpha" {
		t.Fatalf("unexpected agent context: %#v", agentCtx)
	}
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

func TestResolveAgentToolsMatchesTSSurface(t *testing.T) {
	def := &AgentDefinition{
		AgentType:       "reviewer",
		Tools:           []string{"Read", "Bash(git:*)", "Agent(worker, explore)", "Missing"},
		DisallowedTools: []string{"Bash(rm:*)"},
		Permission:      PermissionDefault,
		Source:          SourceUserSettings,
	}
	resolved := resolveAgentTools(def, []string{"Read", "Bash", "Agent", "mcp__notes__search"}, false, false)
	if strings.Join(resolved.ValidTools, ",") != "Agent(worker, explore),Bash(git:*),Read" {
		t.Fatalf("unexpected valid tool specs: %#v", resolved.ValidTools)
	}
	if len(resolved.InvalidTools) != 1 || resolved.InvalidTools[0] != "Missing" {
		t.Fatalf("unexpected invalid tools: %#v", resolved.InvalidTools)
	}
	if strings.Join(resolved.ResolvedTools, ",") != "Read" {
		t.Fatalf("subagents should not resolve Agent or disallowed Bash, got %#v", resolved.ResolvedTools)
	}
	if strings.Join(resolved.AllowedAgentTypes, ",") != "worker,explore" {
		t.Fatalf("unexpected allowed agent types: %#v", resolved.AllowedAgentTypes)
	}
}

func TestAgentIDHelpers(t *testing.T) {
	id := FormatAgentID("researcher", "alpha")
	name, team, ok := ParseAgentID(id)
	if !ok || name != "researcher" || team != "alpha" {
		t.Fatalf("unexpected parsed agent id: %q %q %v", name, team, ok)
	}
	requestID := GenerateRequestID("resume", id)
	requestType, timestamp, parsedID, ok := ParseRequestID(requestID)
	if !ok || requestType != "resume" || timestamp == 0 || parsedID != id {
		t.Fatalf("unexpected parsed request id: %q %d %q %v", requestType, timestamp, parsedID, ok)
	}
	if _, _, ok := ParseAgentID("missing-separator"); ok {
		t.Fatal("expected invalid agent id")
	}
	if _, _, _, ok := ParseRequestID("resume-nope@researcher@alpha"); ok {
		t.Fatal("expected invalid request id timestamp")
	}
}

func TestLoadAgentDefinitionsSupportsTSFrontmatter(t *testing.T) {
	cwd := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CLAUDE_CONFIG_HOME", filepath.Join(t.TempDir(), ".claude"))
	ClearAgentDefinitionsCache()
	t.Cleanup(ClearAgentDefinitionsCache)

	agentDir := filepath.Join(cwd, ".claude", "agents")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("mkdir agent dir: %v", err)
	}
	content := `---
name: reviewer
description: Reviews code
tools: [Read, "Bash(git:*)"]
disallowedTools: [Write]
permissionMode: plan
maxTurns: 7
initialPrompt: Start here
isolation: worktree
memory: local
mcpServers: [notes, search]
requiredMcpServers: [notes]
skills: [review]
background: true
omitClaudeMd: true
color: cyan
effort: high
---
You review changes.
`
	if err := os.WriteFile(filepath.Join(agentDir, "reviewer.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("write agent: %v", err)
	}

	result, err := GetAgentDefinitionsWithOverrides(cwd, false)
	if err != nil {
		t.Fatalf("load agents: %v", err)
	}
	var reviewer *AgentDefinition
	for _, def := range result.Agents {
		if def.AgentType == "reviewer" {
			reviewer = def
			break
		}
	}
	if reviewer == nil {
		t.Fatalf("reviewer agent not loaded: %#v", result.Agents)
	}
	if reviewer.Source != SourceProjectSettings {
		t.Fatalf("expected project source, got %q", reviewer.Source)
	}
	if reviewer.WhenToUse != "Reviews code" || reviewer.SystemPrompt != "You review changes." {
		t.Fatalf("unexpected parsed definition: %+v", reviewer)
	}
	if reviewer.Permission != PermissionMode("plan") || reviewer.MaxTurns != 7 {
		t.Fatalf("unexpected permission/max turns: %+v", reviewer)
	}
	if reviewer.InitialPrompt != "Start here" || reviewer.Isolation != "worktree" || reviewer.Memory != "local" {
		t.Fatalf("missing TS fields: %+v", reviewer)
	}
	if !reviewer.Background || !reviewer.OmitClaudeMd || reviewer.Color != "cyan" || reviewer.Effort != "high" {
		t.Fatalf("missing metadata fields: %+v", reviewer)
	}
	if strings.Join(reviewer.MCPServers, ",") != "notes,search" {
		t.Fatalf("unexpected MCP servers: %#v", reviewer.MCPServers)
	}
	if strings.Join(reviewer.RequiredMCPServers, ",") != "notes" {
		t.Fatalf("unexpected required MCP servers: %#v", reviewer.RequiredMCPServers)
	}
	if strings.Join(reviewer.Skills, ",") != "review" {
		t.Fatalf("unexpected skills: %#v", reviewer.Skills)
	}
}

func TestAgentDefinitionOverridePriority(t *testing.T) {
	cwd := t.TempDir()
	homeConfig := filepath.Join(t.TempDir(), ".claude")
	t.Setenv("CLAUDE_CONFIG_HOME", homeConfig)
	managedSettings := filepath.Join(t.TempDir(), "managed-settings.json")
	t.Setenv("CLAUDE_GO_MANAGED_SETTINGS_PATH", managedSettings)
	ClearAgentDefinitionsCache()
	ClearPluginDefinitions()
	ClearFlagDefinitions()
	t.Cleanup(func() {
		ClearAgentDefinitionsCache()
		ClearPluginDefinitions()
		ClearFlagDefinitions()
	})

	writeAgent := func(dir, prompt string) {
		t.Helper()
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
		content := "---\nname: reviewer\ndescription: " + prompt + "\n---\n" + prompt + "\n"
		if err := os.WriteFile(filepath.Join(dir, "reviewer.md"), []byte(content), 0o644); err != nil {
			t.Fatalf("write agent: %v", err)
		}
	}

	RegisterPluginDefinitions([]*AgentDefinition{{
		AgentType:    "reviewer",
		WhenToUse:    "plugin",
		Source:       SourcePlugin,
		SystemPrompt: "plugin",
	}})
	writeAgent(filepath.Join(homeConfig, "agents"), "user")
	writeAgent(filepath.Join(cwd, ".claude", "agents"), "project")
	RegisterFlagDefinitions([]*AgentDefinition{{
		AgentType:    "reviewer",
		WhenToUse:    "flag",
		SystemPrompt: "flag",
	}})
	writeAgent(filepath.Join(filepath.Dir(managedSettings), ".claude", "agents"), "policy")

	result, err := GetAgentDefinitionsWithOverrides(cwd, false)
	if err != nil {
		t.Fatalf("load agents: %v", err)
	}
	var reviewer *AgentDefinition
	for _, def := range result.Agents {
		if def.AgentType == "reviewer" {
			reviewer = def
			break
		}
	}
	if reviewer == nil {
		t.Fatalf("reviewer agent not loaded")
	}
	if reviewer.Source != SourcePolicySettings || reviewer.SystemPrompt != "policy" {
		t.Fatalf("policy should override flag/project/user/plugin, got %+v", reviewer)
	}
}

func TestAgentDefinitionsLoadParentProjectDirsWithNearestOverride(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatalf("mkdir child: %v", err)
	}
	t.Setenv("CLAUDE_CONFIG_HOME", filepath.Join(t.TempDir(), ".claude"))
	t.Setenv("CLAUDE_GO_MANAGED_SETTINGS_PATH", filepath.Join(t.TempDir(), "managed-settings.json"))
	ClearAgentDefinitionsCache()
	t.Cleanup(ClearAgentDefinitionsCache)

	writeAgent := func(base, prompt string) {
		t.Helper()
		dir := filepath.Join(base, ".claude", "agents")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir agent dir: %v", err)
		}
		content := "---\nname: reviewer\ndescription: " + prompt + "\n---\n" + prompt + "\n"
		if err := os.WriteFile(filepath.Join(dir, "reviewer.md"), []byte(content), 0o644); err != nil {
			t.Fatalf("write agent: %v", err)
		}
	}
	writeAgent(root, "parent")
	writeAgent(child, "child")

	result, err := GetAgentDefinitionsWithOverrides(child, false)
	if err != nil {
		t.Fatalf("load agents: %v", err)
	}
	var reviewer *AgentDefinition
	for _, def := range result.Agents {
		if def.AgentType == "reviewer" {
			reviewer = def
			break
		}
	}
	if reviewer == nil || reviewer.SystemPrompt != "child" {
		t.Fatalf("nearest project agent should override parent, got %+v", reviewer)
	}
}

func TestFilterAgentsByMCPRequirements(t *testing.T) {
	defs := []*AgentDefinition{
		{AgentType: "open", RequiredMCPServers: nil},
		{AgentType: "notes", RequiredMCPServers: []string{"notes"}},
		{AgentType: "both", RequiredMCPServers: []string{"notes", "search"}},
		{AgentType: "missing", RequiredMCPServers: []string{"github"}},
	}
	filtered := FilterAgentsByMCPRequirements(defs, []string{"local-notes-server", "web-search"})
	var names []string
	for _, def := range filtered {
		names = append(names, string(def.AgentType))
	}
	if strings.Join(names, ",") != "open,notes,both" {
		t.Fatalf("unexpected MCP-filtered agents: %#v", names)
	}
	if !HasRequiredMCPServers(&AgentDefinition{RequiredMCPServers: []string{"NOTES"}}, []string{"local-notes-server"}) {
		t.Fatal("required MCP matching should be case-insensitive")
	}
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

func TestAgentRunOptionsBuildsConfigAndContext(t *testing.T) {
	def := &AgentDefinition{
		AgentType:  "reviewer",
		Tools:      []string{"Read"},
		MaxTurns:   10,
		Model:      ModelInherit,
		Permission: PermissionDefault,
	}
	systemPrompt := "custom system"
	config, agentCtx, err := agentConfigFromRunOptions(AgentRunOptions{
		Definition:      def,
		Prompt:          "inspect files",
		ParentAgentID:   "parent-1",
		ParentSessionID: "session-1",
		ParentModel:     "sonnet",
		ModelOverride:   "opus",
		WorkingDir:      "/tmp/project",
		MaxTurns:        3,
		TeamName:        "alpha",
		AgentName:       "review-worker",
		InvocationKind:  InvocationTeammate,
		SystemPrompt:    &systemPrompt,
	})
	if err != nil {
		t.Fatalf("agentConfigFromRunOptions() error = %v", err)
	}
	if config.Definition.Model != ModelOption("opus") || config.InitialPrompt != "inspect files" {
		t.Fatalf("unexpected config: %+v", config)
	}
	if config.ParentID == nil || *config.ParentID != AgentID("parent-1") {
		t.Fatalf("unexpected parent id: %+v", config.ParentID)
	}
	if config.MaxTurns == nil || *config.MaxTurns != 3 {
		t.Fatalf("unexpected max turns: %+v", config.MaxTurns)
	}
	if agentCtx.ParentSessionID != "session-1" || agentCtx.TeamName != "alpha" || agentCtx.SubagentName != "review-worker" {
		t.Fatalf("unexpected agent context: %+v", agentCtx)
	}
	if agentCtx.InvocationKind != InvocationTeammate || agentCtx.AgentType != "reviewer" || agentCtx.InvocationID == "" {
		t.Fatalf("unexpected invocation context: %+v", agentCtx)
	}
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
