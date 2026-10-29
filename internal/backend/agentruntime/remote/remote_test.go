package remote

import (
	"testing"

	"claude-codex/internal/harness/anthropic"
)

func TestConvertSDKMessage_Assistant(t *testing.T) {
	msg := &SessionsMessage{
		Type: string(SDKMessageTypeAssistant),
		UUID: "test-uuid",
		Message: &anthropic.MessageResponse{
			ID:   "msg-123",
			Role: "assistant",
		},
	}

	result := ConvertSDKMessage(msg, nil)
	if result.Type != "message" {
		t.Errorf("Expected type 'message', got '%s'", result.Type)
	}
}

func TestConvertSDKMessage_Partial(t *testing.T) {
	msg := &SessionsMessage{
		Type: string(SDKMessageTypePartial),
		UUID: "test-uuid",
		Event: &anthropic.StreamEvent{
			Event: "content_block_delta",
		},
	}

	result := ConvertSDKMessage(msg, nil)
	if result.Type != "stream_event" {
		t.Errorf("Expected type 'stream_event', got '%s'", result.Type)
	}
	if result.StreamEvent == nil {
		t.Error("Expected StreamEvent to be set")
	}
}

func TestConvertSDKMessage_Result(t *testing.T) {
	msg := &SessionsMessage{
		Type:    string(SDKMessageTypeResult),
		UUID:    "test-uuid",
		Subtype: "success",
		Result:  "Task completed",
	}

	result := ConvertSDKMessage(msg, nil)
	if result.Type != "message" {
		t.Errorf("Expected type 'message', got '%s'", result.Type)
	}
}

func TestConvertSDKMessage_Ignored(t *testing.T) {
	msg := &SessionsMessage{
		Type: string(SDKMessageTypeAuthStatus),
		UUID: "test-uuid",
	}

	result := ConvertSDKMessage(msg, nil)
	if result.Type != "ignored" {
		t.Errorf("Expected type 'ignored', got '%s'", result.Type)
	}
}

func TestIsSessionEndMessage(t *testing.T) {
	msg := &SessionsMessage{
		Type:    string(SDKMessageTypeResult),
		Subtype: "success",
	}

	if !IsSessionEndMessage(msg) {
		t.Error("Expected IsSessionEndMessage to return true")
	}

	msg2 := &SessionsMessage{
		Type: string(SDKMessageTypeAssistant),
	}

	if IsSessionEndMessage(msg2) {
		t.Error("Expected IsSessionEndMessage to return false")
	}
}

func TestIsSuccessResult(t *testing.T) {
	msg := &SessionsMessage{
		Type:    string(SDKMessageTypeResult),
		Subtype: "success",
	}

	if !IsSuccessResult(msg) {
		t.Error("Expected IsSuccessResult to return true")
	}

	msg2 := &SessionsMessage{
		Type:    string(SDKMessageTypeResult),
		Subtype: "error",
	}

	if IsSuccessResult(msg2) {
		t.Error("Expected IsSuccessResult to return false")
	}
}

func TestGetResultText(t *testing.T) {
	msg := &SessionsMessage{
		Type:    string(SDKMessageTypeResult),
		Subtype: "success",
		Result:  "Task completed successfully",
	}

	result := GetResultText(msg)
	if result != "Task completed successfully" {
		t.Errorf("Expected 'Task completed successfully', got '%s'", result)
	}

	msg2 := &SessionsMessage{
		Type:    string(SDKMessageTypeResult),
		Subtype: "error",
	}

	result2 := GetResultText(msg2)
	if result2 != "" {
		t.Errorf("Expected empty string, got '%s'", result2)
	}
}

func TestCreateSyntheticAssistantMessage(t *testing.T) {
	request := &PermissionRequestInner{
		Subtype:   "permission",
		ToolUseID: "tool-123",
		ToolName:  "bash",
		Input: map[string]interface{}{
			"command": "ls -la",
		},
	}

	msg := CreateSyntheticAssistantMessage(request, "req-123")
	msgMap, ok := msg.(map[string]interface{})
	if !ok {
		t.Fatal("Expected map[string]interface{}")
	}

	if msgMap["type"] != "assistant" {
		t.Errorf("Expected type 'assistant', got '%v'", msgMap["type"])
	}

	if msgMap["uuid"] == "" {
		t.Error("Expected UUID to be set")
	}
}

func TestCreateToolStub(t *testing.T) {
	stub := CreateToolStub("custom_tool")

	if stub["name"] != "custom_tool" {
		t.Errorf("Expected name 'custom_tool', got '%v'", stub["name"])
	}

	if stub["is_enabled"] != true {
		t.Error("Expected is_enabled to be true")
	}

	if stub["needs_permissions"] != true {
		t.Error("Expected needs_permissions to be true")
	}
}

func TestRemoteSessionManager_Connect(t *testing.T) {
	config := RemoteSessionConfig{
		SessionID: "test-session",
		GetAccessToken: func() string {
			return "test-token"
		},
		OrgUUID: "test-org",
	}

	callbacks := RemoteSessionCallbacks{
		OnMessage: func(message SDKMessage) {
			// Handle message
		},
	}

	manager := NewRemoteSessionManager(config, callbacks)

	if manager == nil {
		t.Fatal("Expected manager to be created")
	}

	if manager.GetSessionID() != "test-session" {
		t.Errorf("Expected session ID 'test-session', got '%s'", manager.GetSessionID())
	}
}

func TestRemoteSessionManager_RespondToPermission(t *testing.T) {
	config := RemoteSessionConfig{
		SessionID: "test-session",
		GetAccessToken: func() string {
			return "test-token"
		},
		OrgUUID: "test-org",
	}

	callbacks := RemoteSessionCallbacks{}
	manager := NewRemoteSessionManager(config, callbacks)

	// Add a pending request
	manager.mu.Lock()
	manager.pendingPermissionRequests["req-123"] = &PermissionRequestInner{
		ToolUseID: "tool-123",
		ToolName:  "bash",
	}
	manager.mu.Unlock()

	// Try to respond without websocket connection
	response := RemotePermissionResponse{
		Behavior: "allow",
		UpdatedInput: map[string]interface{}{
			"command": "ls",
		},
	}

	err := manager.RespondToPermission("req-123", response)
	// Expected to fail because websocket is nil
	if err == nil {
		t.Error("Expected error when websocket is not connected")
	}

	// Verify request was removed even on error
	manager.mu.RLock()
	_, exists := manager.pendingPermissionRequests["req-123"]
	manager.mu.RUnlock()

	if exists {
		t.Error("Expected pending request to be removed")
	}

	// Test with non-existent request
	err = manager.RespondToPermission("non-existent", response)
	if err == nil {
		t.Error("Expected error for non-existent request")
	}
}

func TestCreateRemoteSessionConfig(t *testing.T) {
	config := CreateRemoteSessionConfig(
		"session-123",
		func() string { return "token" },
		"org-456",
		true,
		false,
	)

	if config.SessionID != "session-123" {
		t.Errorf("Expected SessionID 'session-123', got '%s'", config.SessionID)
	}

	if config.OrgUUID != "org-456" {
		t.Errorf("Expected OrgUUID 'org-456', got '%s'", config.OrgUUID)
	}

	if !config.HasInitialPrompt {
		t.Error("Expected HasInitialPrompt to be true")
	}

	if config.ViewerOnly {
		t.Error("Expected ViewerOnly to be false")
	}

	token := config.GetAccessToken()
	if token != "token" {
		t.Errorf("Expected token 'token', got '%s'", token)
	}
}

func TestSDKMessageTypes(t *testing.T) {
	tests := []struct {
		name     string
		msgType  SDKMessageType
		expected string
	}{
		{"Assistant", SDKMessageTypeAssistant, "assistant"},
		{"User", SDKMessageTypeUser, "user"},
		{"Partial", SDKMessageTypePartial, "partial_assistant"},
		{"Result", SDKMessageTypeResult, "result"},
		{"System", SDKMessageTypeSystem, "system"},
		{"Status", SDKMessageTypeStatus, "status"},
		{"ToolProgress", SDKMessageTypeToolProgress, "tool_progress"},
		{"AuthStatus", SDKMessageTypeAuthStatus, "auth_status"},
		{"ToolUseSummary", SDKMessageTypeToolUseSummary, "tool_use_summary"},
		{"RateLimitEvent", SDKMessageTypeRateLimitEvent, "rate_limit_event"},
		{"CompactBoundary", SDKMessageTypeCompactBoundary, "compact_boundary"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.msgType) != tt.expected {
				t.Errorf("Expected '%s', got '%s'", tt.expected, string(tt.msgType))
			}
		})
	}
}

func TestWebSocketStates(t *testing.T) {
	tests := []struct {
		name     string
		state    WebSocketState
		expected string
	}{
		{"Connecting", WebSocketStateConnecting, "connecting"},
		{"Connected", WebSocketStateConnected, "connected"},
		{"Closed", WebSocketStateClosed, "closed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.state) != tt.expected {
				t.Errorf("Expected '%s', got '%s'", tt.expected, string(tt.state))
			}
		})
	}
}

func TestPermanentCloseCodes(t *testing.T) {
	if !PermanentCloseCodes[4003] {
		t.Error("Expected 4003 to be a permanent close code")
	}

	if PermanentCloseCodes[4001] {
		t.Error("Expected 4001 to not be a permanent close code")
	}
}

func TestConstants(t *testing.T) {
	if ReconnectDelayMS != 2000 {
		t.Errorf("Expected ReconnectDelayMS to be 2000, got %d", ReconnectDelayMS)
	}

	if MaxReconnectAttempts != 5 {
		t.Errorf("Expected MaxReconnectAttempts to be 5, got %d", MaxReconnectAttempts)
	}

	if PingIntervalMS != 30000 {
		t.Errorf("Expected PingIntervalMS to be 30000, got %d", PingIntervalMS)
	}

	if MaxSessionNotFoundRetries != 3 {
		t.Errorf("Expected MaxSessionNotFoundRetries to be 3, got %d", MaxSessionNotFoundRetries)
	}
}
