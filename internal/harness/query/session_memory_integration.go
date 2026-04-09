package query

import (
	"context"
	"os"
	"path/filepath"
	"sync"

	"github.com/ding/claude-code/claude-go/internal/harness/memory"
	"github.com/ding/claude-code/claude-go/internal/harness/tool"
	"github.com/ding/claude-code/claude-go/internal/public/types"
)

var (
	globalSessionMemory     *memory.SessionMemory
	globalSessionMemoryOnce sync.Once
)

// getSessionMemory returns the singleton SessionMemory for the current session.
// The session memory file lives at ~/.claude/session-memory/<sessionID>.md.
func getSessionMemory() *memory.SessionMemory {
	globalSessionMemoryOnce.Do(func() {
		sessionID := getSessionID()
		dir := getSessionMemoryDir()
		cfg := memory.DefaultSessionMemoryConfig()
		globalSessionMemory = memory.NewSessionMemory(sessionID, dir, cfg)
	})
	return globalSessionMemory
}

// getSessionMemoryDir returns the directory for session memory files.
func getSessionMemoryDir() string {
	configHome := os.Getenv("CLAUDE_CONFIG_HOME")
	if configHome == "" {
		home, err := os.UserHomeDir()
		if err == nil {
			configHome = filepath.Join(home, ".claude")
		}
	}
	return filepath.Join(configHome, "session-memory")
}

// hasToolCallsInLastAssistantTurn returns true if the last assistant message
// contained any tool_use blocks.  Mirrors the TypeScript hasToolCallsInLastAssistantTurn.
func hasToolCallsInLastAssistantTurn(messages []types.Message) bool {
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		if msg.Type != types.MessageTypeAssistant {
			continue
		}
		for _, block := range msg.Content {
			if block.Type == "tool_use" {
				return true
			}
		}
		// Found last assistant message, no tool_use blocks
		return false
	}
	return false
}

// countToolCallsSinceID counts tool_use blocks in assistant messages that appear
// after the message with the given UUID.  Pass empty string to count all.
func countToolCallsSinceID(messages []types.Message, sinceUUID string) int {
	count := 0
	found := sinceUUID == ""
	for _, msg := range messages {
		if !found {
			if msg.UUID == sinceUUID {
				found = true
			}
			continue
		}
		if msg.Type != types.MessageTypeAssistant {
			continue
		}
		for _, block := range msg.Content {
			if block.Type == "tool_use" {
				count++
			}
		}
	}
	return count
}

// estimateTokenCount produces a rough token estimate for a message slice.
// Uses 4 chars ≈ 1 token, matching the TypeScript tokenCountWithEstimation heuristic.
func estimateTokenCount(messages []types.Message) int {
	total := 0
	for _, msg := range messages {
		for _, block := range msg.Content {
			total += len(block.Text) / 4
		}
	}
	return total
}

// SessionMemoryExtractionParams bundles all inputs for tryExtractSessionMemory.
type SessionMemoryExtractionParams struct {
	Ctx          context.Context
	Messages     []types.Message
	ToolUseCtx   *tool.ToolUseContext
	QuerySource  string
	SystemPrompt types.SystemPrompt
	UserContext  map[string]string
	SystemCtx   map[string]string
}

// tryExtractSessionMemory checks thresholds and, when met, fires a background
// extraction goroutine that runs a forked subagent to update the session notes file.
//
// This is the Go equivalent of the TypeScript extractSessionMemory post-sampling hook.
// It is called from handleStopHooks on the main REPL thread only.
func tryExtractSessionMemory(p SessionMemoryExtractionParams) {
	// Only extract on main REPL thread
	if p.QuerySource != "repl_main_thread" && p.QuerySource != "" {
		// Allow empty querySource (default) for simpler test/CLI use
		if p.QuerySource != "" {
			return
		}
	}

	sm := getSessionMemory()

	// Compute inputs for the threshold check
	tokenCount := estimateTokenCount(p.Messages)
	lastSummarizedID := sm.GetLastSummarizedMessageID()
	toolCallsSince := countToolCallsSinceID(p.Messages, lastSummarizedID)
	lastTurnHasTools := hasToolCallsInLastAssistantTurn(p.Messages)

	if !sm.ShouldExtractFull(tokenCount, toolCallsSince, lastTurnHasTools) {
		return
	}

	// Update lastSummarizedMessageID eagerly so a rapid second call won't
	// double-fire before the extraction goroutine completes.
	lastMsgUUID := ""
	if len(p.Messages) > 0 {
		lastMsgUUID = p.Messages[len(p.Messages)-1].UUID
	}

	sm.MarkExtractionStarted()

	// Run extraction in the background to avoid blocking the query turn.
	go func() {
		err := runSessionMemoryExtraction(p.Ctx, sm, p.Messages, p.ToolUseCtx, tokenCount, lastMsgUUID)
		if err != nil {
			// Extraction failed — reset in-progress so next turn can retry.
			sm.MarkExtractionCompleted(tokenCount, lastMsgUUID)
		}
	}()
}

// runSessionMemoryExtraction performs the actual extraction:
// 1. Ensures the session memory file exists.
// 2. Reads current content.
// 3. Calls the Claude API via a restricted forked-agent-style invocation with
//    only the Edit tool allowed, targeting the memory file.
func runSessionMemoryExtraction(
	ctx context.Context,
	sm *memory.SessionMemory,
	messages []types.Message,
	toolUseCtx *tool.ToolUseContext,
	tokenCount int,
	lastMsgUUID string,
) error {
	// Ensure directory and template file exist.
	if err := sm.Initialize(); err != nil {
		return err
	}

	// Read current notes.
	currentNotes, err := sm.LoadContent()
	if err != nil {
		return err
	}

	// Build the update prompt.
	notesPath := sm.Path()
	prompt := memory.BuildSessionMemoryUpdatePrompt(notesPath, currentNotes)

	// Run the extraction via the forked agent mechanism.
	// We use a minimal QueryParams with the session memory update prompt appended
	// to the messages, restricted canUseTool (Edit on memory file only).
	extractionMessages := make([]types.Message, len(messages))
	copy(extractionMessages, messages)
	extractionMessages = append(extractionMessages, types.Message{
		Type: types.MessageTypeUser,
		Content: []types.ContentBlock{
			{Type: "text", Text: prompt},
		},
		IsMeta: true,
	})

	forkedParams := &QueryParams{
		Messages:    extractionMessages,
		SystemPrompt: types.SystemPrompt{},
		CanUseTool:  createMemoryFileCanUseTool(notesPath),
		QuerySource: "session_memory",
		MaxTurns:    intPtr(5),
		ToolUseContext: &tool.ToolUseContext{
			Ctx:     ctx,
			Options: toolUseCtx.Options,
		},
	}

	_, termChan, err := Query(ctx, forkedParams)
	if err != nil {
		sm.MarkExtractionCompleted(tokenCount, lastMsgUUID)
		return err
	}

	// Wait for the forked query to finish (it will stop after edits).
	if termChan != nil {
		select {
		case <-termChan:
		case <-ctx.Done():
		}
	}

	sm.MarkExtractionCompleted(tokenCount, lastMsgUUID)
	return nil
}

// WaitForPendingSessionMemoryExtraction blocks until any in-progress extraction
// finishes or times out.  Called at query start to avoid reading stale notes.
func WaitForPendingSessionMemoryExtraction() {
	sm := getSessionMemory()
	if sm.IsExtractionInProgress() {
		sm.WaitForExtraction(
			memory.DefaultExtractionWaitTimeout,
			memory.DefaultExtractionStaleThreshold,
		)
	}
}

// createMemoryFileCanUseTool returns a CanUseToolFn that only permits the Edit
// tool on the exact memory file path.  Mirrors createMemoryFileCanUseTool in
// sessionMemory.ts.
func createMemoryFileCanUseTool(memoryPath string) tool.CanUseToolFn {
	return func(
		t tool.Tool,
		input map[string]interface{},
		toolUseContext *tool.ToolUseContext,
		assistantMessage interface{},
		toolUseID string,
		forceDecision *string,
	) (*tool.PermissionResult, error) {
		if t == nil {
			return &tool.PermissionResult{Behavior: tool.PermissionDeny, Reason: "nil tool"}, nil
		}
		// Only allow Edit tool
		if t.Name() != "Edit" {
			return &tool.PermissionResult{Behavior: tool.PermissionDeny, Reason: "only Edit is allowed for session memory extraction"}, nil
		}
		// Only allow edits to the specific memory file
		if path, ok := input["file_path"].(string); ok && path == memoryPath {
			return &tool.PermissionResult{Behavior: tool.PermissionAllow}, nil
		}
		return &tool.PermissionResult{Behavior: tool.PermissionDeny, Reason: "edit target does not match session memory path"}, nil
	}
}

func intPtr(i int) *int { return &i }
