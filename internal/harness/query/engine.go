package query

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/ding/claude-code/claude-go/internal/harness/compact"
	"github.com/ding/claude-code/claude-go/internal/harness/input"
	"github.com/ding/claude-code/claude-go/internal/harness/mcp"
	"github.com/ding/claude-code/claude-go/internal/harness/storage"
	"github.com/ding/claude-code/claude-go/internal/public/types"
)

// QueryEngine owns the query lifecycle and session state for a conversation.
// One QueryEngine per conversation. Each SubmitMessage() call starts a new
// turn within the same conversation. State (messages, file cache, usage, etc.)
// persists across turns.
type QueryEngine struct {
	config                        *QueryEngineConfig
	mutableMessages               []types.Message
	abortController               context.CancelFunc
	permissionDenials             []PermissionDenial
	totalUsage                    Usage
	hasHandledOrphanedPermission  bool
	readFileState                 *FileStateCache
	discoveredSkillNames          map[string]bool
	loadedNestedMemoryPaths       map[string]bool
	sessionStorage                *storage.SessionStorage
	autoCompactor                 *compact.AutoCompactor
}

// QueryEngineConfig contains configuration for QueryEngine.
type QueryEngineConfig struct {
	// WorkingDir is the current working directory
	WorkingDir string

	// Tools available for execution
	Tools []Tool

	// Commands available for execution
	Commands []Command

	// MCPClients for MCP integration
	MCPClients []*mcp.Client

	// Agents available for delegation
	Agents []Agent

	// CanUseTool checks if a tool can be used
	CanUseTool CanUseToolFunc

	// InitialMessages to start the conversation
	InitialMessages []types.Message

	// ReadFileCache for file state tracking
	ReadFileCache *FileStateCache

	// CustomSystemPrompt overrides default system prompt
	CustomSystemPrompt string

	// AppendSystemPrompt appends to system prompt
	AppendSystemPrompt string

	// UserSpecifiedModel overrides default model
	UserSpecifiedModel string

	// FallbackModel is used when no model specified
	FallbackModel string

	// ThinkingConfig controls thinking behavior
	ThinkingConfig *ThinkingConfig

	// MaxTurns limits conversation turns
	MaxTurns int

	// MaxBudgetUSD limits spending
	MaxBudgetUSD float64

	// TaskBudget limits task resources
	TaskBudget *TaskBudget

	// JSONSchema for structured output
	JSONSchema map[string]any

	// Verbose enables detailed logging
	Verbose bool

	// ReplayUserMessages enables message replay
	ReplayUserMessages bool

	// IncludePartialMessages includes partial responses
	IncludePartialMessages bool

	// OrphanedPermission to handle on first submit
	OrphanedPermission *OrphanedPermission

	// SessionID for this conversation
	SessionID string

	// PermissionMode for tool execution
	PermissionMode string
}

// PermissionDenial represents a denied tool permission.
type PermissionDenial struct {
	ToolName   string
	ToolUseID  string
	ToolInput  map[string]interface{}
}

// Usage tracks API usage statistics.
type Usage struct {
	InputTokens  int
	OutputTokens int
	CacheReads   int
	CacheWrites  int
}

// Tool represents an available tool.
type Tool struct {
	Name        string
	Description string
	InputSchema map[string]interface{}
}

// Command represents a slash command.
type Command struct {
	Name        string
	Description string
	Handler     func(ctx context.Context, args string) error
}

// Agent represents an available agent.
type Agent struct {
	Type        string
	Name        string
	Description string
}

// CanUseToolFunc checks if a tool can be used.
type CanUseToolFunc func(
	ctx context.Context,
	tool Tool,
	input map[string]interface{},
	toolUseID string,
) (PermissionResult, error)

// PermissionResult represents the result of a permission check.
type PermissionResult struct {
	Behavior string // "allow", "deny", "prompt"
	Reason   string
}

// ThinkingConfig controls thinking behavior.
type ThinkingConfig struct {
	Type string // "adaptive", "enabled", "disabled"
}

// OrphanedPermission represents an orphaned permission to handle.
type OrphanedPermission struct {
	ToolName  string
	ToolUseID string
	Input     map[string]any
}

// NewQueryEngine creates a new QueryEngine instance.
func NewQueryEngine(config *QueryEngineConfig) (*QueryEngine, error) {
	if config == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}

	ctx, cancel := context.WithCancel(context.Background())
	_ = ctx // Will be used for abort controller

	// Initialize session storage
	homeDir := os.Getenv("HOME")
	if homeDir == "" {
		homeDir = "/tmp"
	}
	sessionStorage, err := storage.NewSessionStorage(homeDir, config.SessionID, config.WorkingDir)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create session storage: %w", err)
	}

	// Initialize auto-compactor
	autoCompactor := compact.NewAutoCompactor(&compact.AutoCompactConfig{
		Enabled:                true,
		Model:                  config.FallbackModel,
		ContextWindowSize:      200000, // Default context window
		CurrentTokenUsage:      0,
		MaxConsecutiveFailures: 3,
	})

	engine := &QueryEngine{
		config:                  config,
		mutableMessages:         config.InitialMessages,
		abortController:         cancel,
		permissionDenials:       []PermissionDenial{},
		totalUsage:              Usage{},
		readFileState:           config.ReadFileCache,
		discoveredSkillNames:    make(map[string]bool),
		loadedNestedMemoryPaths: make(map[string]bool),
		sessionStorage:          sessionStorage,
		autoCompactor:           autoCompactor,
	}

	if engine.mutableMessages == nil {
		engine.mutableMessages = []types.Message{}
	}

	return engine, nil
}

// SubmitMessage submits a new user message and returns a channel of response messages.
func (qe *QueryEngine) SubmitMessage(
	ctx context.Context,
	prompt interface{}, // string or []types.ContentBlock
	options *SubmitMessageOptions,
) (<-chan types.Message, error) {
	if options == nil {
		options = &SubmitMessageOptions{}
	}

	messageChan := make(chan types.Message, 100)

	go func() {
		defer close(messageChan)

		if err := qe.submitMessageInternal(ctx, prompt, options, messageChan); err != nil {
			// Send error message
			errorMsg := types.Message{
				Type:      types.MessageTypeSystem,
				Timestamp: time.Now(),
				Content: []types.ContentBlock{
					{Type: "text", Text: fmt.Sprintf("Error: %v", err)},
				},
			}
			messageChan <- errorMsg
		}
	}()

	return messageChan, nil
}

// SubmitMessageOptions contains options for SubmitMessage.
type SubmitMessageOptions struct {
	UUID   string
	IsMeta bool
}

// submitMessageInternal handles the internal message submission logic.
func (qe *QueryEngine) submitMessageInternal(
	ctx context.Context,
	prompt interface{},
	options *SubmitMessageOptions,
	messageChan chan<- types.Message,
) error {
	// Clear discovered skills for this turn
	qe.discoveredSkillNames = make(map[string]bool)

	startTime := time.Now()

	// Build system prompt if needed
	systemPrompt, err := qe.buildSystemPrompt(ctx)
	if err != nil {
		return fmt.Errorf("failed to build system prompt: %w", err)
	}

	// Handle orphaned permission (only once per engine lifetime)
	if qe.config.OrphanedPermission != nil && !qe.hasHandledOrphanedPermission {
		qe.hasHandledOrphanedPermission = true
		if err := qe.handleOrphanedPermission(ctx, messageChan); err != nil {
			return fmt.Errorf("failed to handle orphaned permission: %w", err)
		}
	}

	// Process user input
	inputResult, err := qe.processUserInput(ctx, prompt, options)
	if err != nil {
		return fmt.Errorf("failed to process user input: %w", err)
	}

	// Add messages from user input
	qe.mutableMessages = append(qe.mutableMessages, inputResult.Messages...)

	// Record to transcript
	if err := qe.recordTranscript(ctx); err != nil {
		// Log error but don't fail
		fmt.Printf("Warning: failed to record transcript: %v\n", err)
	}

	// Filter replayable messages
	replayableMessages := qe.filterReplayableMessages(inputResult.Messages)

	// Acknowledge messages if replay enabled
	if qe.config.ReplayUserMessages {
		for _, msg := range replayableMessages {
			messageChan <- msg
		}
	}

	// If shouldn't query, return early
	if !inputResult.ShouldQuery {
		return nil
	}

	// Execute query loop
	if err := qe.executeQueryLoop(ctx, systemPrompt, messageChan); err != nil {
		return fmt.Errorf("query loop failed: %w", err)
	}

	// Update total usage
	elapsed := time.Since(startTime)
	fmt.Printf("Query completed in %v\n", elapsed)

	return nil
}

// buildSystemPrompt builds the system prompt for the query.
func (qe *QueryEngine) buildSystemPrompt(ctx context.Context) (types.SystemPrompt, error) {
	workingDir := qe.config.WorkingDir
	if workingDir == "" {
		workingDir, _ = os.Getwd()
	}

	// Use the context package to collect real system/user context.
	userCtx, sysCtx, err := globalSystemPromptBuilder.CollectContext(ctx, workingDir, true)
	if err != nil {
		// Non-fatal: fall back to minimal context.
		userCtx = map[string]string{}
		sysCtx = map[string]string{}
	}

	// Merge any caller-provided context overrides.
	for k, v := range map[string]string{
		"workingDir": workingDir,
		"sessionID":  qe.config.SessionID,
	} {
		if userCtx[k] == "" {
			userCtx[k] = v
		}
	}

	model := qe.config.UserSpecifiedModel
	if model == "" {
		model = qe.config.FallbackModel
	}
	if model == "" {
		model = "claude-sonnet-4-6"
	}

	return globalSystemPromptBuilder.BuildSystemPrompt(
		ctx,
		userCtx,
		sysCtx,
		qe.config.CustomSystemPrompt,
		qe.config.AppendSystemPrompt,
		model,
		qe.config.MCPClients...,
	)
}

// processUserInput processes the user's input.
func (qe *QueryEngine) processUserInput(
	ctx context.Context,
	prompt interface{},
	options *SubmitMessageOptions,
) (*input.ProcessUserInputResult, error) {
	inputOpts := &input.ProcessUserInputOptions{
		Input: prompt,
		Mode:  "prompt",
		Context: &input.ProcessUserInputContext{
			WorkingDir:     qe.config.WorkingDir,
			SessionID:      qe.config.SessionID,
			PermissionMode: qe.config.PermissionMode,
			Messages:       qe.mutableMessages,
		},
		UUID:   options.UUID,
		IsMeta: options.IsMeta,
	}

	return input.ProcessUserInput(ctx, inputOpts)
}

// recordTranscript records messages to the transcript.
func (qe *QueryEngine) recordTranscript(ctx context.Context) error {
	// Record all messages
	for _, msg := range qe.mutableMessages {
		transcriptMsg := convertToTranscriptMessage(msg)
		if err := qe.sessionStorage.RecordMessage(transcriptMsg); err != nil {
			return err
		}
	}
	return nil
}

// convertToTranscriptMessage converts types.Message to storage.TranscriptMessage
func convertToTranscriptMessage(msg types.Message) *storage.TranscriptMessage {
	transcriptMsg := &storage.TranscriptMessage{
		BaseEntry: storage.BaseEntry{
			Type:      storage.EntryType(msg.Type),
			Timestamp: msg.Timestamp.Format(time.RFC3339),
		},
		UUID: msg.UUID,
	}

	// Determine role from message type
	switch msg.Type {
	case types.MessageTypeUser:
		transcriptMsg.Role = "user"
	case types.MessageTypeAssistant:
		transcriptMsg.Role = "assistant"
	case types.MessageTypeSystem:
		transcriptMsg.Role = "system"
	}

	// Convert content blocks to JSON string
	if len(msg.Content) > 0 {
		contentJSON, err := json.Marshal(msg.Content)
		if err == nil {
			transcriptMsg.Content = string(contentJSON)
		}
	}

	return transcriptMsg
}

// filterReplayableMessages filters messages that should be replayed.
func (qe *QueryEngine) filterReplayableMessages(messages []types.Message) []types.Message {
	var replayable []types.Message
	for _, msg := range messages {
		if msg.Type == types.MessageTypeUser && !msg.IsMeta {
			replayable = append(replayable, msg)
		}
	}
	return replayable
}

// handleOrphanedPermission handles an orphaned permission.
func (qe *QueryEngine) handleOrphanedPermission(
	ctx context.Context,
	messageChan chan<- types.Message,
) error {
	orphaned := qe.config.OrphanedPermission
	if orphaned == nil {
		return nil
	}

	// Find the tool use block in the assistant message
	var toolUseBlock *types.ContentBlock
	for i := range qe.mutableMessages {
		msg := &qe.mutableMessages[i]
		if msg.Type != types.MessageTypeAssistant {
			continue
		}
		for j := range msg.Content {
			block := &msg.Content[j]
			if block.Type == "tool_use" && block.ID == orphaned.ToolUseID {
				toolUseBlock = block
				break
			}
		}
		if toolUseBlock != nil {
			break
		}
	}

	if toolUseBlock == nil {
		return fmt.Errorf("tool use block not found for orphaned permission: %s", orphaned.ToolUseID)
	}

	// Find the tool definition
	var toolDef *Tool
	for i := range qe.config.Tools {
		if qe.config.Tools[i].Name == toolUseBlock.Name {
			toolDef = &qe.config.Tools[i]
			break
		}
	}

	if toolDef == nil {
		return fmt.Errorf("tool definition not found: %s", toolUseBlock.Name)
	}

	// Use the input from orphaned permission (may be updated by user)
	finalInput := orphaned.Input
	if finalInput == nil {
		finalInput = toolUseBlock.Input
	}

	// Create a permission result that always allows (since user already approved)
	permissionResult := PermissionResult{
		Behavior: "allow",
		Reason:   "Orphaned permission - user already approved",
	}

	// Check if the tool use is already in messages (for CCR resume)
	alreadyPresent := false
	for i := range qe.mutableMessages {
		msg := &qe.mutableMessages[i]
		if msg.Type != types.MessageTypeAssistant {
			continue
		}
		for j := range msg.Content {
			block := &msg.Content[j]
			if block.Type == "tool_use" && block.ID == orphaned.ToolUseID {
				alreadyPresent = true
				break
			}
		}
		if alreadyPresent {
			break
		}
	}

	// If not already present, add the assistant message
	if !alreadyPresent {
		assistantMsg := types.Message{
			Type:      types.MessageTypeAssistant,
			UUID:      orphaned.ToolUseID + "-assistant",
			Timestamp: time.Now(),
			Content: []types.ContentBlock{
				{
					Type:  "tool_use",
					ID:    orphaned.ToolUseID,
					Name:  toolUseBlock.Name,
					Input: finalInput,
				},
			},
		}
		qe.mutableMessages = append(qe.mutableMessages, assistantMsg)

		// Record to transcript
		transcriptMsg := convertToTranscriptMessage(assistantMsg)
		if err := qe.sessionStorage.RecordMessage(transcriptMsg); err != nil {
			return fmt.Errorf("failed to record assistant message: %w", err)
		}

		// Send to channel
		messageChan <- assistantMsg
	}

	// Execute the tool
	// TODO: Implement tool execution
	// For now, create a placeholder tool result
	toolResultMsg := types.Message{
		Type:      types.MessageTypeUser,
		UUID:      orphaned.ToolUseID + "-result",
		Timestamp: time.Now(),
		Content: []types.ContentBlock{
			{
				Type:      "tool_result",
				ToolUseID: orphaned.ToolUseID,
				Content:   "Tool execution completed (placeholder)",
			},
		},
	}
	qe.mutableMessages = append(qe.mutableMessages, toolResultMsg)

	// Record to transcript
	transcriptMsg := convertToTranscriptMessage(toolResultMsg)
	if err := qe.sessionStorage.RecordMessage(transcriptMsg); err != nil {
		return fmt.Errorf("failed to record tool result: %w", err)
	}

	// Send to channel
	messageChan <- toolResultMsg

	// Track permission result
	_ = permissionResult // Will be used when we implement full permission tracking

	return nil
}

// executeQueryLoop executes the main query loop.
func (qe *QueryEngine) executeQueryLoop(
	ctx context.Context,
	systemPrompt types.SystemPrompt,
	messageChan chan<- types.Message,
) error {
	// TODO: Implement query loop execution
	// This will call the existing Query function with appropriate parameters
	return nil
}

// GetMessages returns the current message history.
func (qe *QueryEngine) GetMessages() []types.Message {
	return qe.mutableMessages
}

// GetUsage returns the total usage statistics.
func (qe *QueryEngine) GetUsage() Usage {
	return qe.totalUsage
}

// GetPermissionDenials returns all permission denials.
func (qe *QueryEngine) GetPermissionDenials() []PermissionDenial {
	return qe.permissionDenials
}

// Abort aborts the current query.
func (qe *QueryEngine) Abort() {
	if qe.abortController != nil {
		qe.abortController()
	}
}

// Close closes the query engine and releases resources.
func (qe *QueryEngine) Close() error {
	qe.Abort()
	// TODO: Close session storage and other resources
	return nil
}
