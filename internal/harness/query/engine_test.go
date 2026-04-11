package query

import (
	"context"
	"testing"
	"time"

	"claude-codex/internal/public/types"
)

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
