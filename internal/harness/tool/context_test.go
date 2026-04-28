package tool

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestNewToolUseContext(t *testing.T) {
	ctx := context.Background()
	toolCtx := NewToolUseContext(ctx)

	if toolCtx.Ctx != ctx {
		t.Error("Expected context to be set")
	}

	if toolCtx.State == nil {
		t.Error("Expected state to be initialized")
	}

	if toolCtx.Callbacks == nil {
		t.Error("Expected callbacks to be initialized")
	}

	if toolCtx.ToolDecisions == nil {
		t.Error("Expected tool decisions to be initialized")
	}
}

func TestToolState_InProgressToolUseIDs(t *testing.T) {
	state := NewToolState()

	// Test adding IDs
	state.AddInProgressToolUseID("tool1")
	state.AddInProgressToolUseID("tool2")

	if !state.IsToolUseInProgress("tool1") {
		t.Error("Expected tool1 to be in progress")
	}

	if !state.IsToolUseInProgress("tool2") {
		t.Error("Expected tool2 to be in progress")
	}

	if state.IsToolUseInProgress("tool3") {
		t.Error("Expected tool3 to not be in progress")
	}

	// Test getting IDs
	ids := state.GetInProgressToolUseIDs()
	if len(ids) != 2 {
		t.Errorf("Expected 2 in-progress IDs, got %d", len(ids))
	}

	// Test removing IDs
	state.RemoveInProgressToolUseID("tool1")
	if state.IsToolUseInProgress("tool1") {
		t.Error("Expected tool1 to not be in progress after removal")
	}

	if !state.IsToolUseInProgress("tool2") {
		t.Error("Expected tool2 to still be in progress")
	}
}

func TestToolState_HasInterruptibleToolInProgress(t *testing.T) {
	state := NewToolState()

	if state.GetHasInterruptibleToolInProgress() {
		t.Error("Expected no interruptible tool in progress initially")
	}

	state.SetHasInterruptibleToolInProgress(true)
	if !state.GetHasInterruptibleToolInProgress() {
		t.Error("Expected interruptible tool in progress")
	}

	state.SetHasInterruptibleToolInProgress(false)
	if state.GetHasInterruptibleToolInProgress() {
		t.Error("Expected no interruptible tool in progress")
	}
}

func TestToolState_ResponseLength(t *testing.T) {
	state := NewToolState()

	if state.GetResponseLength() != 0 {
		t.Errorf("Expected initial response length 0, got %d", state.GetResponseLength())
	}

	length := state.IncrementResponseLength(100)
	if length != 100 {
		t.Errorf("Expected response length 100, got %d", length)
	}

	length = state.IncrementResponseLength(50)
	if length != 150 {
		t.Errorf("Expected response length 150, got %d", length)
	}

	if state.GetResponseLength() != 150 {
		t.Errorf("Expected response length 150, got %d", state.GetResponseLength())
	}
}

func TestToolState_Concurrency(t *testing.T) {
	state := NewToolState()
	var wg sync.WaitGroup

	// Test concurrent additions
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			state.AddInProgressToolUseID(string(rune('a' + id)))
		}(i)
	}

	wg.Wait()

	ids := state.GetInProgressToolUseIDs()
	if len(ids) != 100 {
		t.Errorf("Expected 100 in-progress IDs, got %d", len(ids))
	}

	// Test concurrent increments
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			state.IncrementResponseLength(1)
		}()
	}

	wg.Wait()

	if state.GetResponseLength() != 100 {
		t.Errorf("Expected response length 100, got %d", state.GetResponseLength())
	}
}

func TestToolUseContext_WithContext(t *testing.T) {
	ctx1 := context.Background()
	toolCtx := NewToolUseContext(ctx1)

	ctx2, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	newToolCtx := toolCtx.WithContext(ctx2)

	if newToolCtx.Ctx != ctx2 {
		t.Error("Expected new context to be set")
	}

	if toolCtx.Ctx != ctx1 {
		t.Error("Expected original context to be unchanged")
	}
}

func TestToolUseContext_WithPermissionContext(t *testing.T) {
	ctx := context.Background()
	toolCtx := NewToolUseContext(ctx)

	permCtx := NewToolPermissionContext()
	permCtx.SetMode(PermissionModeAuto)

	newToolCtx := toolCtx.WithPermissionContext(permCtx)

	if newToolCtx.PermissionContext != permCtx {
		t.Error("Expected permission context to be set")
	}

	if toolCtx.PermissionContext != nil {
		t.Error("Expected original permission context to be unchanged")
	}
}

func TestToolUseContext_Clone(t *testing.T) {
	ctx := context.Background()
	toolCtx := NewToolUseContext(ctx)
	toolCtx.AgentID = "agent1"
	toolCtx.SessionID = "session1"
	toolCtx.UserModified = true

	cloned := toolCtx.Clone()

	if cloned.AgentID != "agent1" {
		t.Errorf("Expected agent ID 'agent1', got %s", cloned.AgentID)
	}

	if cloned.SessionID != "session1" {
		t.Errorf("Expected session ID 'session1', got %s", cloned.SessionID)
	}

	if !cloned.UserModified {
		t.Error("Expected user modified to be true")
	}

	// Modify clone and verify original is unchanged
	cloned.AgentID = "agent2"
	if toolCtx.AgentID != "agent1" {
		t.Error("Expected original agent ID to be unchanged")
	}
}

func TestToolUseContext_Tools(t *testing.T) {
	toolCtx := NewToolUseContext(context.Background())
	toolA := NewToolBuilder("tool-a").Build()
	toolB := NewToolBuilder("tool-b").Build()

	toolCtx.SetTools([]Tool{toolA, toolB})

	if got := toolCtx.FindToolByName("tool-a"); got == nil || got.Name() != "tool-a" {
		t.Fatalf("expected to find tool-a, got %#v", got)
	}
	if got := toolCtx.FindToolByName("missing"); got != nil {
		t.Fatalf("expected missing tool lookup to return nil, got %#v", got)
	}

	snapshot := toolCtx.Tools()
	if len(snapshot) != 2 {
		t.Fatalf("expected 2 tools in snapshot, got %d", len(snapshot))
	}

	snapshot[0] = toolB
	if got := toolCtx.FindToolByName("tool-a"); got == nil || got.Name() != "tool-a" {
		t.Fatalf("expected context lookup to remain stable after snapshot mutation, got %#v", got)
	}
}

func TestFileReadingLimits(t *testing.T) {
	maxTokens := 1000
	maxBytes := int64(10000)

	limits := &FileReadingLimits{
		MaxTokens:    &maxTokens,
		MaxSizeBytes: &maxBytes,
	}

	if *limits.MaxTokens != 1000 {
		t.Errorf("Expected max tokens 1000, got %d", *limits.MaxTokens)
	}

	if *limits.MaxSizeBytes != 10000 {
		t.Errorf("Expected max size bytes 10000, got %d", *limits.MaxSizeBytes)
	}
}

func TestGlobLimits(t *testing.T) {
	maxResults := 100

	limits := &GlobLimits{
		MaxResults: &maxResults,
	}

	if *limits.MaxResults != 100 {
		t.Errorf("Expected max results 100, got %d", *limits.MaxResults)
	}
}

func TestToolOptions(t *testing.T) {
	maxBudget := 10.0
	opts := ToolOptions{
		Debug:              true,
		Verbose:            true,
		MainLoopModel:      "claude-3-opus",
		MaxBudgetUSD:       &maxBudget,
		CustomSystemPrompt: "Custom prompt",
		QuerySource:        "test",
	}

	if !opts.Debug {
		t.Error("Expected debug to be true")
	}

	if !opts.Verbose {
		t.Error("Expected verbose to be true")
	}

	if opts.MainLoopModel != "claude-3-opus" {
		t.Errorf("Expected main loop model 'claude-3-opus', got %s", opts.MainLoopModel)
	}

	if *opts.MaxBudgetUSD != 10.0 {
		t.Errorf("Expected max budget 10.0, got %f", *opts.MaxBudgetUSD)
	}

	if opts.CustomSystemPrompt != "Custom prompt" {
		t.Errorf("Expected custom SystemPrompt 'Custom prompt', got %s", opts.CustomSystemPrompt)
	}

	if opts.QuerySource != "test" {
		t.Errorf("Expected query source 'test', got %s", opts.QuerySource)
	}
}

func TestToolCallbacks(t *testing.T) {
	callbacks := &ToolCallbacks{
		GetWorkingDirectory: func() string {
			return "/test/dir"
		},
		GetSessionID: func() string {
			return "session123"
		},
		SetStreamMode: func(mode string) {
			// Test callback
		},
	}

	if callbacks.GetWorkingDirectory() != "/test/dir" {
		t.Errorf("Expected working directory '/test/dir', got %s", callbacks.GetWorkingDirectory())
	}

	if callbacks.GetSessionID() != "session123" {
		t.Errorf("Expected session ID 'session123', got %s", callbacks.GetSessionID())
	}

	// Test that SetStreamMode doesn't panic
	callbacks.SetStreamMode("test")
}
