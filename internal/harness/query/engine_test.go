package query

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"claude-codex/internal/harness/plannerapi"
	"claude-codex/internal/harness/state"
	htool "claude-codex/internal/harness/tool"
	toolkit "claude-codex/internal/harness/tools"
	"claude-codex/internal/public/types"
)

type runtimePlanner struct {
	call plannerapi.ToolCall
}

func (p runtimePlanner) Next(_ context.Context, session *state.Session, _ []toolkit.Descriptor) (plannerapi.Plan, error) {
	last := session.LastMessage()
	if last != nil && last.Role == "tool" {
		return plannerapi.Plan{
			AssistantText: "handled: " + last.ToolOutput,
			StopReason:    "end_turn",
		}, nil
	}
	return plannerapi.Plan{
		ToolCalls:  []plannerapi.ToolCall{p.call},
		StopReason: "tool_use",
	}, nil
}

func TestNewQueryEngine(t *testing.T) {
	config := &QueryEngineConfig{
		WorkingDir:     "/test",
		SessionID:      "test-session",
		FallbackModel:  "claude-sonnet-4-6",
		PermissionMode: "normal",
	}

	engine, err := NewQueryEngine(config)
	if err != nil {
		t.Fatalf("NewQueryEngine failed: %v", err)
	}

	if engine == nil {
		t.Fatal("Expected non-nil engine")
	}

	if engine.config != config {
		t.Error("Config not set correctly")
	}

	if engine.mutableMessages == nil {
		t.Error("Messages should be initialized")
	}

	if engine.discoveredSkillNames == nil {
		t.Error("Discovered skills should be initialized")
	}
}

func TestConfiguredToolCallValidatesDescriptorSchema(t *testing.T) {
	var executed bool
	tool := newConfiguredToolFromDescriptor(toolkit.Descriptor{
		Name:        "search",
		Description: "Search",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}},"required":["query"]}`),
	}, func(context.Context, string, json.RawMessage) (string, error) {
		executed = true
		return "ok", nil
	})

	_, err := tool.Call(context.Background(), map[string]interface{}{"query": 123}, htool.NewToolUseContext(context.Background()))
	if err == nil {
		t.Fatal("expected schema validation error")
	}
	if executed {
		t.Fatal("tool executor should not run after validation failure")
	}
}

func TestNewQueryEngine_WithInitialMessages(t *testing.T) {
	initialMessages := []types.Message{
		{
			Type:      types.MessageTypeUser,
			Timestamp: time.Now(),
			Content: []types.ContentBlock{
				{Type: "text", Text: "Hello"},
			},
		},
	}

	config := &QueryEngineConfig{
		WorkingDir:      "/test",
		SessionID:       "test-session",
		InitialMessages: initialMessages,
		FallbackModel:   "claude-sonnet-4-6",
		PermissionMode:  "normal",
	}

	engine, err := NewQueryEngine(config)
	if err != nil {
		t.Fatalf("NewQueryEngine failed: %v", err)
	}

	if len(engine.mutableMessages) != 1 {
		t.Errorf("Expected 1 initial message, got %d", len(engine.mutableMessages))
	}
}

func TestNewQueryEngine_NilConfig(t *testing.T) {
	_, err := NewQueryEngine(nil)
	if err == nil {
		t.Error("Expected error for nil config")
	}
}

func TestQueryEngine_GetMessages(t *testing.T) {
	config := &QueryEngineConfig{
		WorkingDir:     "/test",
		SessionID:      "test-session",
		FallbackModel:  "claude-sonnet-4-6",
		PermissionMode: "normal",
	}

	engine, err := NewQueryEngine(config)
	if err != nil {
		t.Fatalf("NewQueryEngine failed: %v", err)
	}

	messages := engine.GetMessages()
	if messages == nil {
		t.Error("Expected non-nil messages")
	}

	if len(messages) != 0 {
		t.Errorf("Expected 0 messages, got %d", len(messages))
	}
}

func TestQueryEngine_GetUsage(t *testing.T) {
	config := &QueryEngineConfig{
		WorkingDir:     "/test",
		SessionID:      "test-session",
		FallbackModel:  "claude-sonnet-4-6",
		PermissionMode: "normal",
	}

	engine, err := NewQueryEngine(config)
	if err != nil {
		t.Fatalf("NewQueryEngine failed: %v", err)
	}

	usage := engine.GetUsage()
	if usage.InputTokens != 0 {
		t.Errorf("Expected 0 input tokens, got %d", usage.InputTokens)
	}
	if usage.OutputTokens != 0 {
		t.Errorf("Expected 0 output tokens, got %d", usage.OutputTokens)
	}
}

func TestQueryEngine_GetPermissionDenials(t *testing.T) {
	config := &QueryEngineConfig{
		WorkingDir:     "/test",
		SessionID:      "test-session",
		FallbackModel:  "claude-sonnet-4-6",
		PermissionMode: "normal",
	}

	engine, err := NewQueryEngine(config)
	if err != nil {
		t.Fatalf("NewQueryEngine failed: %v", err)
	}

	denials := engine.GetPermissionDenials()
	if denials == nil {
		t.Error("Expected non-nil denials")
	}

	if len(denials) != 0 {
		t.Errorf("Expected 0 denials, got %d", len(denials))
	}
}

func TestQueryEngine_SubmitMessage_String(t *testing.T) {
	config := &QueryEngineConfig{
		WorkingDir:     "/test",
		SessionID:      "test-session",
		FallbackModel:  "claude-sonnet-4-6",
		PermissionMode: "normal",
	}

	engine, err := NewQueryEngine(config)
	if err != nil {
		t.Fatalf("NewQueryEngine failed: %v", err)
	}

	ctx := context.Background()
	messageChan, err := engine.SubmitMessage(ctx, "Hello, world!", nil)
	if err != nil {
		t.Fatalf("SubmitMessage failed: %v", err)
	}

	if messageChan == nil {
		t.Fatal("Expected non-nil message channel")
	}

	// Collect messages (with timeout)
	timeout := time.After(5 * time.Second)
	var messages []types.Message

	for {
		select {
		case msg, ok := <-messageChan:
			if !ok {
				// Channel closed
				goto done
			}
			messages = append(messages, msg)
		case <-timeout:
			t.Fatal("Timeout waiting for messages")
		}
	}

done:
	// Should have at least processed the input
	if len(engine.GetMessages()) == 0 {
		t.Error("Expected messages to be added to engine")
	}
}

func TestQueryEngine_SubmitMessage_ContentBlocks(t *testing.T) {
	config := &QueryEngineConfig{
		WorkingDir:     "/test",
		SessionID:      "test-session",
		FallbackModel:  "claude-sonnet-4-6",
		PermissionMode: "normal",
	}

	engine, err := NewQueryEngine(config)
	if err != nil {
		t.Fatalf("NewQueryEngine failed: %v", err)
	}

	ctx := context.Background()
	contentBlocks := []types.ContentBlock{
		{Type: "text", Text: "Hello"},
		{Type: "text", Text: "World"},
	}

	messageChan, err := engine.SubmitMessage(ctx, contentBlocks, nil)
	if err != nil {
		t.Fatalf("SubmitMessage failed: %v", err)
	}

	if messageChan == nil {
		t.Fatal("Expected non-nil message channel")
	}

	// Drain channel
	timeout := time.After(5 * time.Second)
	for {
		select {
		case _, ok := <-messageChan:
			if !ok {
				goto done
			}
		case <-timeout:
			t.Fatal("Timeout waiting for messages")
		}
	}

done:
	if len(engine.GetMessages()) == 0 {
		t.Error("Expected messages to be added to engine")
	}
}

func TestQueryEngine_SubmitMessage_WithOptions(t *testing.T) {
	config := &QueryEngineConfig{
		WorkingDir:     "/test",
		SessionID:      "test-session",
		FallbackModel:  "claude-sonnet-4-6",
		PermissionMode: "normal",
	}

	engine, err := NewQueryEngine(config)
	if err != nil {
		t.Fatalf("NewQueryEngine failed: %v", err)
	}

	ctx := context.Background()
	options := &SubmitMessageOptions{
		UUID:   "test-uuid",
		IsMeta: true,
	}

	messageChan, err := engine.SubmitMessage(ctx, "Test message", options)
	if err != nil {
		t.Fatalf("SubmitMessage failed: %v", err)
	}

	// Drain channel
	timeout := time.After(5 * time.Second)
	for {
		select {
		case _, ok := <-messageChan:
			if !ok {
				goto done
			}
		case <-timeout:
			t.Fatal("Timeout waiting for messages")
		}
	}

done:
	messages := engine.GetMessages()
	if len(messages) == 0 {
		t.Fatal("Expected messages to be added")
	}

	// Check that meta flag was set
	userMsg := messages[0]
	if !userMsg.IsMeta {
		t.Error("Expected IsMeta to be true")
	}
}

func TestQueryEngine_SubmitMessage_ExecutesOrphanedPermissionTool(t *testing.T) {
	config := &QueryEngineConfig{
		WorkingDir:     t.TempDir(),
		SessionID:      "orphan-session",
		FallbackModel:  "claude-sonnet-4-6",
		PermissionMode: "normal",
		Tools:          []htool.Tool{htool.NewToolBuilder("fake_tool").Build()},
		InitialMessages: []types.Message{{
			Type: types.MessageTypeAssistant,
			Content: []types.ContentBlock{{
				Type:  "tool_use",
				ID:    "tool-1",
				Name:  "fake_tool",
				Input: map[string]interface{}{"path": "old"},
			}},
		}},
		OrphanedPermission: &OrphanedPermission{
			ToolName:  "fake_tool",
			ToolUseID: "tool-1",
			Input:     map[string]any{"path": "approved"},
		},
		ExecuteTool: func(ctx context.Context, name string, input json.RawMessage) (string, error) {
			if name != "fake_tool" {
				t.Fatalf("unexpected tool name %q", name)
			}
			var decoded map[string]string
			if err := json.Unmarshal(input, &decoded); err != nil {
				t.Fatalf("invalid input: %v", err)
			}
			if decoded["path"] != "approved" {
				t.Fatalf("orphaned permission input was not used: %#v", decoded)
			}
			return "executed orphaned tool", nil
		},
	}

	engine, err := NewQueryEngine(config)
	if err != nil {
		t.Fatalf("NewQueryEngine failed: %v", err)
	}

	ch, err := engine.SubmitMessage(context.Background(), "resume", nil)
	if err != nil {
		t.Fatalf("SubmitMessage failed: %v", err)
	}
	for range ch {
	}

	messages := engine.GetMessages()
	var result *types.Message
	for i := range messages {
		for _, block := range messages[i].Content {
			if block.Type == "tool_result" && block.ToolUseID == "tool-1" {
				result = &messages[i]
			}
		}
	}
	if result == nil {
		t.Fatalf("expected tool_result message, got %#v", messages)
	}
	if got := result.Content[0].Content; got != "executed orphaned tool" {
		t.Fatalf("unexpected tool result content %q", got)
	}
}

func TestQueryEngine_SubmitMessage_UsesPlannerRuntime(t *testing.T) {
	config := &QueryEngineConfig{
		WorkingDir:     t.TempDir(),
		SessionID:      "runtime-session",
		FallbackModel:  "claude-sonnet-4-6",
		PermissionMode: "normal",
		Planner: runtimePlanner{
			call: plannerapi.ToolCall{
				ID:    "tool-1",
				Name:  "fake_tool",
				Input: json.RawMessage(`{"path":"README.md"}`),
			},
		},
		ToolDescriptors: []toolkit.Descriptor{{Name: "fake_tool"}},
		ExecuteTool: func(context.Context, string, json.RawMessage) (string, error) {
			return "tool output", nil
		},
		MaxTurns: 3,
	}

	engine, err := NewQueryEngine(config)
	if err != nil {
		t.Fatalf("NewQueryEngine failed: %v", err)
	}

	ch, err := engine.SubmitMessage(context.Background(), "run tool", nil)
	if err != nil {
		t.Fatalf("SubmitMessage failed: %v", err)
	}

	for range ch {
	}

	messages := engine.GetMessages()
	if len(messages) < 4 {
		t.Fatalf("expected planner runtime to append assistant/tool messages, got %#v", messages)
	}

	last := messages[len(messages)-1]
	if last.Type != types.MessageTypeAssistant {
		t.Fatalf("expected final assistant message, got %#v", last)
	}
	if got := last.Content[0].Text; got != "handled: tool output" {
		t.Fatalf("unexpected final assistant content %q", got)
	}
}

func TestQueryEngine_RuntimeToolsFromDescriptors(t *testing.T) {
	config := &QueryEngineConfig{
		WorkingDir:    t.TempDir(),
		SessionID:     "runtime-tools",
		FallbackModel: "claude-sonnet-4-6",
		ToolDescriptors: []toolkit.Descriptor{{
			Name:        "fake_tool",
			Description: "Fake tool description",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`),
		}},
	}

	engine, err := NewQueryEngine(config)
	if err != nil {
		t.Fatalf("NewQueryEngine failed: %v", err)
	}

	tools := engine.runtimeTools()
	if len(tools) != 1 {
		t.Fatalf("expected 1 runtime tool, got %d", len(tools))
	}
	if tools[0].Name() != "fake_tool" {
		t.Fatalf("unexpected runtime tool name %q", tools[0].Name())
	}
	desc, err := tools[0].Description(nil, htool.DescriptionOptions{})
	if err != nil {
		t.Fatalf("runtime tool description error: %v", err)
	}
	if desc != "Fake tool description" {
		t.Fatalf("unexpected runtime tool description %q", desc)
	}
	if schema := tools[0].InputSchema(); schema == nil || schema.Type != "object" {
		t.Fatalf("expected runtime tool schema to be preserved, got %#v", schema)
	}
}

func TestQueryEngine_Abort(t *testing.T) {
	config := &QueryEngineConfig{
		WorkingDir:     "/test",
		SessionID:      "test-session",
		FallbackModel:  "claude-sonnet-4-6",
		PermissionMode: "normal",
	}

	engine, err := NewQueryEngine(config)
	if err != nil {
		t.Fatalf("NewQueryEngine failed: %v", err)
	}

	// Should not panic
	engine.Abort()
}

func TestQueryEngine_Close(t *testing.T) {
	config := &QueryEngineConfig{
		WorkingDir:     "/test",
		SessionID:      "test-session",
		FallbackModel:  "claude-sonnet-4-6",
		PermissionMode: "normal",
	}

	engine, err := NewQueryEngine(config)
	if err != nil {
		t.Fatalf("NewQueryEngine failed: %v", err)
	}

	err = engine.Close()
	if err != nil {
		t.Errorf("Close failed: %v", err)
	}
}

func TestQueryEngine_BuildSystemPrompt(t *testing.T) {
	config := &QueryEngineConfig{
		WorkingDir:         "/test",
		SessionID:          "test-session",
		FallbackModel:      "claude-sonnet-4-6",
		CustomSystemPrompt: "You are a helpful assistant.",
		PermissionMode:     "normal",
	}

	engine, err := NewQueryEngine(config)
	if err != nil {
		t.Fatalf("NewQueryEngine failed: %v", err)
	}

	ctx := context.Background()
	systemPrompt, err := engine.buildSystemPrompt(ctx)
	if err != nil {
		t.Fatalf("buildSystemPrompt failed: %v", err)
	}

	if systemPrompt.Content == "" && len(systemPrompt.Parts) == 0 {
		t.Error("Expected non-empty system prompt")
	}
}

func TestQueryEngine_FilterReplayableMessages(t *testing.T) {
	config := &QueryEngineConfig{
		WorkingDir:     "/test",
		SessionID:      "test-session",
		FallbackModel:  "claude-sonnet-4-6",
		PermissionMode: "normal",
	}

	engine, err := NewQueryEngine(config)
	if err != nil {
		t.Fatalf("NewQueryEngine failed: %v", err)
	}

	messages := []types.Message{
		{
			Type:   types.MessageTypeUser,
			IsMeta: false,
			Content: []types.ContentBlock{
				{Type: "text", Text: "User message"},
			},
		},
		{
			Type:   types.MessageTypeUser,
			IsMeta: true,
			Content: []types.ContentBlock{
				{Type: "text", Text: "Meta message"},
			},
		},
		{
			Type: types.MessageTypeAssistant,
			Content: []types.ContentBlock{
				{Type: "text", Text: "Assistant message"},
			},
		},
	}

	replayable := engine.filterReplayableMessages(messages)

	if len(replayable) != 1 {
		t.Errorf("Expected 1 replayable message, got %d", len(replayable))
	}

	if replayable[0].Type != types.MessageTypeUser {
		t.Error("Expected user message")
	}

	if replayable[0].IsMeta {
		t.Error("Expected non-meta message")
	}
}
