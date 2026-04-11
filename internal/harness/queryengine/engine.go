// Package engine provides the QueryEngine for managing conversation lifecycle.
package engine

import (
	"context"
	"sync"
	"time"

	"claude-codex/internal/harness/tool"
	"github.com/google/uuid"
)

// QueryEngine owns the query lifecycle and session state for a conversation.
// It extracts the core logic into a standalone struct that can be used by both
// the headless/SDK path and the REPL.
//
// One QueryEngine per conversation. Each SubmitMessage() call starts a new
// turn within the same conversation. State (messages, file cache, usage, etc.)
// persists across turns.
type QueryEngine struct {
	config                      QueryEngineConfig
	mutableMessages             []Message
	abortCtx                    context.Context
	abortCancel                 context.CancelFunc
	permissionDenials           []PermissionDenial
	totalUsage                  *Usage
	hasHandledOrphanedPermission bool
	readFileState               interface{}

	// Turn-scoped skill discovery tracking (feeds was_discovered on
	// tengu_skill_tool_invocation). Must persist across the two
	// processUserInputContext rebuilds inside SubmitMessage, but is cleared
	// at the start of each SubmitMessage to avoid unbounded growth across
	// many turns in SDK mode.
	discoveredSkillNames    map[string]bool
	loadedNestedMemoryPaths map[string]bool

	mu sync.RWMutex
}

// NewQueryEngine creates a new QueryEngine with the given configuration.
func NewQueryEngine(config QueryEngineConfig) *QueryEngine {
	ctx := context.Background()
	if config.AbortController != nil {
		// If a cancel func is provided, we need to create a context that uses it
		ctx, _ = context.WithCancel(ctx)
	}

	abortCtx, abortCancel := context.WithCancel(ctx)

	initialMessages := config.InitialMessages
	if initialMessages == nil {
		initialMessages = []Message{}
	}

	return &QueryEngine{
		config:                   config,
		mutableMessages:          initialMessages,
		abortCtx:                 abortCtx,
		abortCancel:              abortCancel,
		permissionDenials:        []PermissionDenial{},
		totalUsage:               EmptyUsage(),
		readFileState:            config.ReadFileCache,
		discoveredSkillNames:     make(map[string]bool),
		loadedNestedMemoryPaths:  make(map[string]bool),
	}
}

// SubmitMessage submits a new message to the conversation and streams back responses.
// This is the main entry point for each turn in the conversation.
func (qe *QueryEngine) SubmitMessage(
	ctx context.Context,
	prompt interface{}, // string or []ContentBlockParam
	options *SubmitOptions,
) (<-chan SDKMessage, error) {
	qe.mu.Lock()
	// Clear turn-scoped tracking
	qe.discoveredSkillNames = make(map[string]bool)
	qe.mu.Unlock()

	if options == nil {
		options = &SubmitOptions{}
	}

	// Create output channel for streaming messages
	out := make(chan SDKMessage, 100)

	// Start async processing
	go qe.submitMessageAsync(ctx, prompt, options, out)

	return out, nil
}

// submitMessageAsync handles the actual message submission and streaming.
func (qe *QueryEngine) submitMessageAsync(
	ctx context.Context,
	prompt interface{},
	options *SubmitOptions,
	out chan<- SDKMessage,
) {
	defer close(out)

	startTime := time.Now()

	// Merge contexts for cancellation
	mergedCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	go func() {
		select {
		case <-qe.abortCtx.Done():
			cancel()
		case <-mergedCtx.Done():
		}
	}()

	// TODO: Implement the full submission logic
	// This is a placeholder that shows the structure

	// 1. Process user input (handle slash commands, etc.)
	// 2. Handle orphaned permissions if present
	// 3. Build SystemPrompt and context
	// 4. Yield system init message
	// 5. Enter query loop
	// 6. Stream responses
	// 7. Handle snip replay if configured
	// 8. Track usage and permissions
	// 9. Yield final result

	// Placeholder result
	out <- SDKMessage{
		Type:         "result",
		Subtype:      "success",
		SessionID:    qe.GetSessionID(),
		UUID:         uuid.New().String(),
		DurationMS:   time.Since(startTime).Milliseconds(),
		Result:       "Not yet implemented",
		Usage:        qe.totalUsage,
		NumTurns:     1,
	}
}

// Interrupt aborts the current query execution.
func (qe *QueryEngine) Interrupt() {
	qe.abortCancel()
}

// GetMessages returns a read-only view of the conversation messages.
func (qe *QueryEngine) GetMessages() []Message {
	qe.mu.RLock()
	defer qe.mu.RUnlock()

	// Return a copy to prevent external modification
	messages := make([]Message, len(qe.mutableMessages))
	copy(messages, qe.mutableMessages)
	return messages
}

// GetReadFileState returns the current file state cache.
func (qe *QueryEngine) GetReadFileState() interface{} {
	qe.mu.RLock()
	defer qe.mu.RUnlock()
	return qe.readFileState
}

// GetSessionID returns the current session ID.
func (qe *QueryEngine) GetSessionID() string {
	// TODO: Integrate with actual session management
	return "session-" + uuid.New().String()
}

// SetModel updates the model configuration.
func (qe *QueryEngine) SetModel(model string) {
	qe.mu.Lock()
	defer qe.mu.Unlock()
	qe.config.UserSpecifiedModel = model
}

// GetTotalUsage returns the accumulated usage across all turns.
func (qe *QueryEngine) GetTotalUsage() *Usage {
	qe.mu.RLock()
	defer qe.mu.RUnlock()
	return qe.totalUsage
}

// GetPermissionDenials returns all permission denials in this session.
func (qe *QueryEngine) GetPermissionDenials() []PermissionDenial {
	qe.mu.RLock()
	defer qe.mu.RUnlock()

	denials := make([]PermissionDenial, len(qe.permissionDenials))
	copy(denials, qe.permissionDenials)
	return denials
}

// addMessage appends a message to the conversation history (thread-safe).
func (qe *QueryEngine) addMessage(msg Message) {
	qe.mu.Lock()
	defer qe.mu.Unlock()
	qe.mutableMessages = append(qe.mutableMessages, msg)
}

// addPermissionDenial records a permission denial (thread-safe).
func (qe *QueryEngine) addPermissionDenial(denial PermissionDenial) {
	qe.mu.Lock()
	defer qe.mu.Unlock()
	qe.permissionDenials = append(qe.permissionDenials, denial)
}

// updateUsage accumulates usage statistics (thread-safe).
func (qe *QueryEngine) updateUsage(usage *Usage) {
	qe.mu.Lock()
	defer qe.mu.Unlock()
	qe.totalUsage = AccumulateUsage(qe.totalUsage, usage)
}

// wrapCanUseTool wraps the permission checker to track denials.
func (qe *QueryEngine) wrapCanUseTool(
	tool tool.Tool,
	input map[string]interface{},
	toolCtx *tool.ToolUseContext,
	assistantMessage interface{},
	toolUseID string,
	forceDecision bool,
) (*PermissionResult, error) {
	result, err := qe.config.CanUseTool(
		tool,
		input,
		toolCtx,
		assistantMessage,
		toolUseID,
		forceDecision,
	)

	if err != nil {
		return nil, err
	}

	// Track denials for SDK reporting
	if result.Behavior != "allow" {
		qe.addPermissionDenial(PermissionDenial{
			ToolName:  sdkCompatToolName(tool.Name()),
			ToolUseID: toolUseID,
			ToolInput: input,
		})
	}

	return result, nil
}

// sdkCompatToolName converts internal tool names to SDK-compatible names.
func sdkCompatToolName(name string) string {
	// TODO: Implement any necessary name transformations
	return name
}

// Ask is a convenience wrapper around QueryEngine for one-shot usage.
// It creates a new QueryEngine, submits a single message, and returns the response stream.
func Ask(ctx context.Context, config QueryEngineConfig, prompt interface{}) (<-chan SDKMessage, error) {
	engine := NewQueryEngine(config)
	return engine.SubmitMessage(ctx, prompt, nil)
}
