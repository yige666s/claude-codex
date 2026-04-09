package state

import (
	"testing"
)

func TestNewAppState(t *testing.T) {
	state := NewAppState()

	if state == nil {
		t.Fatal("NewAppState() returned nil")
	}

	if state.Tasks == nil {
		t.Error("Tasks map is nil")
	}

	if state.AgentNameRegistry == nil {
		t.Error("AgentNameRegistry map is nil")
	}

	if state.SessionHooks == nil {
		t.Error("SessionHooks map is nil")
	}

	if state.ToolPermissionContext.Mode != "default" {
		t.Errorf("Default permission mode = %s, want default", state.ToolPermissionContext.Mode)
	}

	if state.RemoteConnectionStatus != "connecting" {
		t.Errorf("RemoteConnectionStatus = %s, want connecting", state.RemoteConnectionStatus)
	}

	if !state.PromptSuggestionEnabled {
		t.Error("PromptSuggestionEnabled should be true by default")
	}
}

func TestStore(t *testing.T) {
	initialState := NewAppState()
	var changeCount int
	var lastOld, lastNew *AppState

	onChange := func(newState *AppState, oldState *AppState) {
		changeCount++
		lastOld = oldState
		lastNew = newState
	}

	store := NewStore(initialState, onChange)

	// Test GetState
	state := store.GetState()
	if state == nil {
		t.Fatal("GetState() returned nil")
	}

	// Test SetState
	store.SetState(func(prev *AppState) *AppState {
		newState := *prev
		newState.Verbose = true
		return &newState
	})

	if changeCount != 1 {
		t.Errorf("onChange called %d times, want 1", changeCount)
	}

	if lastOld == nil || lastNew == nil {
		t.Fatal("onChange not called with old and new state")
	}

	if lastOld.Verbose {
		t.Error("Old state should have Verbose=false")
	}

	if !lastNew.Verbose {
		t.Error("New state should have Verbose=true")
	}

	// Test that returning same state doesn't trigger onChange
	store.SetState(func(prev *AppState) *AppState {
		return prev
	})

	if changeCount != 1 {
		t.Errorf("onChange called %d times, want 1 (no change)", changeCount)
	}
}

func TestStoreSubscribe(t *testing.T) {
	store := NewStore(nil, nil)

	var callCount int
	listener := func() {
		callCount++
	}

	// Subscribe
	unsubscribe := store.Subscribe(listener)

	if store.GetListenerCount() != 1 {
		t.Errorf("Listener count = %d, want 1", store.GetListenerCount())
	}

	// Trigger state change
	store.SetState(func(prev *AppState) *AppState {
		newState := *prev
		newState.Verbose = true
		return &newState
	})

	if callCount != 1 {
		t.Errorf("Listener called %d times, want 1", callCount)
	}

	// Unsubscribe
	unsubscribe()

	if store.GetListenerCount() != 0 {
		t.Errorf("Listener count = %d, want 0 after unsubscribe", store.GetListenerCount())
	}

	// Trigger another change
	store.SetState(func(prev *AppState) *AppState {
		newState := *prev
		newState.FastMode = true
		return &newState
	})

	if callCount != 1 {
		t.Errorf("Listener called %d times, want 1 (no call after unsubscribe)", callCount)
	}
}

func TestAppStateHelpers(t *testing.T) {
	state := NewAppState()

	// Test permission mode
	state.SetPermissionMode("plan")
	if mode := state.GetPermissionMode(); mode != "plan" {
		t.Errorf("GetPermissionMode() = %s, want plan", mode)
	}

	// Test verbose
	state.SetVerbose(true)
	if !state.IsVerbose() {
		t.Error("IsVerbose() = false, want true")
	}

	// Test agent name registry
	state.RegisterAgentName("test-agent", "agent-123")
	if id, ok := state.GetAgentIDByName("test-agent"); !ok || id != "agent-123" {
		t.Errorf("GetAgentIDByName() = %s, %v, want agent-123, true", id, ok)
	}

	// Test file tracking
	state.TrackFile("/path/to/file.go")
	if !state.IsFileTracked("/path/to/file.go") {
		t.Error("IsFileTracked() = false, want true")
	}

	// Test auth version
	initialVersion := state.GetAuthVersion()
	state.IncrementAuthVersion()
	if state.GetAuthVersion() != initialVersion+1 {
		t.Errorf("GetAuthVersion() = %d, want %d", state.GetAuthVersion(), initialVersion+1)
	}
}

func TestNotifications(t *testing.T) {
	state := NewAppState()

	notification := Notification{
		ID:        "notif-1",
		Type:      "info",
		Message:   "Test notification",
		Timestamp: 123456789,
	}

	// Add notification
	state.AddNotification(notification)

	if len(state.Notifications.Queue) != 1 {
		t.Errorf("Notification queue length = %d, want 1", len(state.Notifications.Queue))
	}

	// Set current notification
	state.SetCurrentNotification(&notification)

	current := state.GetCurrentNotification()
	if current == nil {
		t.Fatal("GetCurrentNotification() returned nil")
	}

	if current.ID != "notif-1" {
		t.Errorf("Current notification ID = %s, want notif-1", current.ID)
	}
}

func TestFileHistory(t *testing.T) {
	state := NewAppState()

	snapshot := FileSnapshot{
		Path:      "/test/file.go",
		Content:   "package main",
		Timestamp: 123456789,
		Sequence:  1,
	}

	initialSeq := state.FileHistory.SnapshotSequence

	state.AddFileSnapshot(snapshot)

	if len(state.FileHistory.Snapshots) != 1 {
		t.Errorf("Snapshots length = %d, want 1", len(state.FileHistory.Snapshots))
	}

	if state.FileHistory.SnapshotSequence != initialSeq+1 {
		t.Errorf("SnapshotSequence = %d, want %d", state.FileHistory.SnapshotSequence, initialSeq+1)
	}
}

func TestMCPTools(t *testing.T) {
	state := NewAppState()

	tool := MCPTool{
		Name:        "test-tool",
		Description: "A test tool",
		Schema:      map[string]interface{}{"type": "object"},
	}

	state.AddMCPTool(tool)

	tools := state.GetMCPTools()
	if len(tools) != 1 {
		t.Errorf("MCP tools length = %d, want 1", len(tools))
	}

	if tools[0].Name != "test-tool" {
		t.Errorf("Tool name = %s, want test-tool", tools[0].Name)
	}
}

func TestPlugins(t *testing.T) {
	state := NewAppState()

	plugin := LoadedPlugin{
		ID:      "plugin-1",
		Name:    "Test Plugin",
		Version: "1.0.0",
		Path:    "/path/to/plugin",
	}

	state.AddPlugin(plugin)

	plugins := state.GetEnabledPlugins()
	if len(plugins) != 1 {
		t.Errorf("Enabled plugins length = %d, want 1", len(plugins))
	}

	if plugins[0].ID != "plugin-1" {
		t.Errorf("Plugin ID = %s, want plugin-1", plugins[0].ID)
	}
}

func TestConcurrentAccess(t *testing.T) {
	store := NewStore(nil, nil)

	// Test concurrent reads and writes
	done := make(chan bool)

	// Writer goroutine
	go func() {
		for i := 0; i < 100; i++ {
			store.SetState(func(prev *AppState) *AppState {
				newState := *prev
				newState.AuthVersion = i
				return &newState
			})
		}
		done <- true
	}()

	// Reader goroutine
	go func() {
		for i := 0; i < 100; i++ {
			_ = store.GetState()
		}
		done <- true
	}()

	// Wait for both goroutines
	<-done
	<-done

	// Verify final state
	finalState := store.GetState()
	if finalState.AuthVersion != 99 {
		t.Errorf("Final AuthVersion = %d, want 99", finalState.AuthVersion)
	}
}
