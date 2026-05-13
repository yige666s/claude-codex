package query

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	appconfig "claude-codex/internal/app/config"
	"claude-codex/internal/harness/compact"
	"claude-codex/internal/harness/input"
	"claude-codex/internal/harness/mcp"
	"claude-codex/internal/harness/plannerapi"
	"claude-codex/internal/harness/state"
	"claude-codex/internal/harness/storage"
	htool "claude-codex/internal/harness/tool"
	toolkit "claude-codex/internal/harness/tools"
	"claude-codex/internal/public/types"
)

// QueryEngine owns the query lifecycle and session state for a conversation.
// One QueryEngine per conversation. Each SubmitMessage() call starts a new
// turn within the same conversation. State (messages, file cache, usage, etc.)
// persists across turns.
type QueryEngine struct {
	config                       *QueryEngineConfig
	mutableMessages              []types.Message
	abortController              context.CancelFunc
	permissionDenials            []PermissionDenial
	totalUsage                   Usage
	hasHandledOrphanedPermission bool
	readFileState                *FileStateCache
	discoveredSkillNames         map[string]bool
	loadedNestedMemoryPaths      map[string]bool
	sessionStorage               *storage.SessionStorage
	autoCompactor                *compact.AutoCompactor
}

// QueryEngineConfig contains configuration for QueryEngine.
type QueryEngineConfig struct {
	// WorkingDir is the current working directory
	WorkingDir string

	// Tools available for execution
	Tools []htool.Tool

	// Planner used by runtime-backed execution
	Planner plannerapi.Planner

	// ToolDescriptors exposed to the planner
	ToolDescriptors []toolkit.Descriptor

	// ExecuteTool executes a planned tool call
	ExecuteTool func(ctx context.Context, name string, input json.RawMessage) (string, error)

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

	// TokenBudget controls TS-style auto-continuation until a token target is reached.
	TokenBudget *int

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
	ToolName  string
	ToolUseID string
	ToolInput map[string]interface{}
}

// Usage tracks API usage statistics.
type Usage struct {
	InputTokens  int
	OutputTokens int
	CacheReads   int
	CacheWrites  int
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
	tool htool.Tool,
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
	homeDir, err := appconfig.AppHome()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to resolve app home: %w", err)
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

	// Record the newly added input messages to transcript storage.
	if err := qe.recordMessages(inputResult.Messages); err != nil {
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

// recordMessages records a message slice to the transcript store.
func (qe *QueryEngine) recordMessages(messages []types.Message) error {
	for _, msg := range messages {
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
	var toolDef htool.Tool
	for _, configuredTool := range qe.runtimeTools() {
		if configuredTool.Name() == toolUseBlock.Name {
			toolDef = configuredTool
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

	var output string
	var isError bool
	if qe.config.ExecuteTool == nil {
		output = fmt.Sprintf("tool executor not configured for %s", toolUseBlock.Name)
		isError = true
	} else {
		inputJSON, err := json.Marshal(finalInput)
		if err != nil {
			output = fmt.Sprintf("failed to encode tool input: %v", err)
			isError = true
		} else if result, err := qe.config.ExecuteTool(ctx, toolUseBlock.Name, inputJSON); err != nil {
			output = err.Error()
			isError = true
		} else {
			output = result
		}
	}

	toolResultMsg := createToolResultMessage(orphaned.ToolUseID, output, isError)
	toolResultMsg.UUID = orphaned.ToolUseID + "-result"
	qe.mutableMessages = append(qe.mutableMessages, toolResultMsg)

	// Record to transcript
	transcriptMsg := convertToTranscriptMessage(toolResultMsg)
	if err := qe.sessionStorage.RecordMessage(transcriptMsg); err != nil {
		return fmt.Errorf("failed to record tool result: %w", err)
	}

	// Send to channel
	messageChan <- toolResultMsg

	return nil
}

// executeQueryLoop executes the main query loop.
func (qe *QueryEngine) executeQueryLoop(
	ctx context.Context,
	systemPrompt types.SystemPrompt,
	messageChan chan<- types.Message,
) error {
	toolCtx := htool.NewToolUseContext(ctx)
	toolCtx.SessionID = qe.config.SessionID
	toolCtx.Options.MainLoopModel = qe.selectedModel()
	toolCtx.SetTools(qe.runtimeTools())
	toolCtx.AbortController = htool.NewAbortController()
	toolCtx.ReadFileState = map[string]bool{}
	if qe.readFileState != nil {
		toolCtx.State.FileStateCache = qe.readFileState
	}

	maxTurns := qe.config.MaxTurns
	var maxTurnsPtr *int
	if maxTurns > 0 {
		maxTurnsPtr = &maxTurns
	}
	tokenBudget := qe.config.TokenBudget

	deps := productionDeps()
	deps.CompactService = NewLocalCompactService(qe.selectedModel())
	if qe.config.Planner != nil {
		deps.CallModel = qe.plannerModelCaller(systemPrompt)
	}

	events, terminal, err := Query(ctx, &QueryParams{
		Messages:       qe.mutableMessages,
		SystemPrompt:   systemPrompt,
		CanUseTool:     qe.toolPermissionAdapter(),
		ToolUseContext: toolCtx,
		FallbackModel:  qe.config.FallbackModel,
		QuerySource:    "sdk",
		MaxTurns:       maxTurnsPtr,
		TokenBudget:    tokenBudget,
		TaskBudget:     qe.config.TaskBudget,
		Deps:           deps,
	})
	if err != nil {
		return err
	}

	emitted := make([]types.Message, 0, 4)
	for event := range events {
		switch msg := event.(type) {
		case types.Message:
			emitted = append(emitted, msg)
			messageChan <- msg
		case types.AssistantMessage:
			emitted = append(emitted, msg.Message)
			messageChan <- msg.Message
		}
	}
	result := <-terminal
	if result.Error != nil {
		return result.Error
	}
	if result.Reason == TerminalReasonMaxTurns {
		return fmt.Errorf("query exceeded max turns (%d)", maxTurns)
	}
	if len(result.Messages) > 0 {
		qe.mutableMessages = result.Messages
	} else {
		qe.mutableMessages = append(qe.mutableMessages, emitted...)
	}
	qe.totalUsage = usageFromMessages(qe.mutableMessages)
	if len(emitted) > 0 {
		return qe.recordMessages(emitted)
	}
	return nil
}

func (qe *QueryEngine) runtimeTools() []htool.Tool {
	if len(qe.config.Tools) > 0 {
		return append([]htool.Tool(nil), qe.config.Tools...)
	}
	return configuredToolsFromDescriptors(qe.config.ToolDescriptors, qe.config.ExecuteTool)
}

func (qe *QueryEngine) executePlannedTool(ctx context.Context, call plannerapi.ToolCall) (string, error) {
	if qe.config.ExecuteTool == nil {
		return "", fmt.Errorf("tool executor not configured for %s", call.Name)
	}
	return qe.config.ExecuteTool(ctx, call.Name, call.Input)
}

func (qe *QueryEngine) selectedModel() string {
	if qe.config.UserSpecifiedModel != "" {
		return qe.config.UserSpecifiedModel
	}
	if qe.config.FallbackModel != "" {
		return qe.config.FallbackModel
	}
	return "claude-sonnet-4-6"
}

func (qe *QueryEngine) toolPermissionAdapter() htool.CanUseToolFn {
	return func(toolDef htool.Tool, input map[string]interface{}, toolUseContext *htool.ToolUseContext, assistantMessage interface{}, toolUseID string, forceDecision *string) (*htool.PermissionResult, error) {
		if qe.config.CanUseTool == nil {
			return &htool.PermissionResult{Behavior: htool.PermissionAllow, UpdatedInput: input}, nil
		}
		result, err := qe.config.CanUseTool(toolUseContext.Ctx, toolDef, input, toolUseID)
		if err != nil {
			return nil, err
		}
		behavior := htool.PermissionBehavior(result.Behavior)
		if behavior == "" {
			behavior = htool.PermissionAllow
		}
		return &htool.PermissionResult{Behavior: behavior, UpdatedInput: input, Reason: result.Reason}, nil
	}
}

func (qe *QueryEngine) plannerModelCaller(systemPrompt types.SystemPrompt) ModelCaller {
	return func(ctx context.Context, params *ModelCallParams) (<-chan types.Message, error) {
		session := qe.runtimeSession(systemPrompt)
		session.Messages = publicMessagesToStateMessages(params.Messages)
		plan, err := qe.config.Planner.Next(ctx, session, qe.config.ToolDescriptors)
		if err != nil {
			return nil, err
		}
		msg := types.Message{
			Type:       types.MessageTypeAssistant,
			UUID:       types.UUID(),
			Timestamp:  time.Now().UTC(),
			StopReason: plan.StopReason,
			Content:    plannerContentBlocks(plan),
		}
		ch := make(chan types.Message, 1)
		ch <- msg
		close(ch)
		return ch, nil
	}
}

func plannerContentBlocks(plan plannerapi.Plan) []types.ContentBlock {
	blocks := make([]types.ContentBlock, 0, len(plan.ToolCalls)+1)
	if strings.TrimSpace(plan.AssistantText) != "" {
		blocks = append(blocks, types.ContentBlock{Type: "text", Text: plan.AssistantText})
	}
	for _, call := range plan.ToolCalls {
		blocks = append(blocks, types.ContentBlock{
			Type:  "tool_use",
			ID:    call.ID,
			Name:  call.Name,
			Input: rawJSONToMap(call.Input),
		})
	}
	return blocks
}

func (qe *QueryEngine) runtimeSession(systemPrompt types.SystemPrompt) *state.Session {
	session := state.NewSession(qe.config.WorkingDir)
	session.ID = qe.config.SessionID
	session.Messages = publicMessagesToStateMessages(qe.mutableMessages)
	session.Usage = calculateStateUsage(session.Messages)
	if session.StartedAt.IsZero() {
		session.StartedAt = time.Now().UTC()
	}
	session.UpdatedAt = session.StartedAt

	if prompt := strings.TrimSpace(systemPrompt.Content); prompt != "" && !hasHiddenContext(session.Messages, prompt) {
		systemMessage := state.Message{
			Role:      "user",
			Content:   prompt,
			Hidden:    true,
			CreatedAt: session.StartedAt,
		}
		session.Messages = append([]state.Message{systemMessage}, session.Messages...)
		session.Usage.RecordInput(prompt)
	}

	return session
}

func (qe *QueryEngine) syncRuntimeSession(session *state.Session) {
	qe.mutableMessages = stateMessagesToPublicMessages(session.Messages)
	qe.totalUsage = Usage{
		InputTokens:  session.Usage.InputTokens,
		OutputTokens: session.Usage.OutputTokens,
		CacheReads:   0,
		CacheWrites:  0,
	}
}

func publicMessagesToStateMessages(messages []types.Message) []state.Message {
	out := make([]state.Message, 0, len(messages))
	for _, msg := range messages {
		if converted, ok := publicMessageToStateMessage(msg); ok {
			out = append(out, converted)
		}
	}
	return out
}

func publicMessageToStateMessage(msg types.Message) (state.Message, bool) {
	createdAt := msg.Timestamp
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}

	switch msg.Type {
	case types.MessageTypeAssistant:
		content, toolCalls := publicAssistantContent(msg.Content)
		return state.Message{
			Role:      "assistant",
			Content:   content,
			ToolCalls: toolCalls,
			CreatedAt: createdAt,
		}, true
	case types.MessageTypeTool:
		return state.Message{
			Role:       "tool",
			ToolCallID: msg.ToolUseID,
			ToolName:   publicToolName(msg.Message),
			ToolInput:  publicToolInput(msg.Message),
			ToolOutput: publicToolOutput(msg.Content),
			CreatedAt:  createdAt,
		}, true
	default:
		if toolMsg, ok := publicToolResultStateMessage(msg, createdAt); ok {
			return toolMsg, true
		}
		return state.Message{
			Role:          "user",
			Content:       publicContentText(msg.Content),
			ContentBlocks: append([]types.ContentBlock(nil), msg.Content...),
			Hidden:        msg.IsMeta || msg.Type == types.MessageTypeSystem,
			CreatedAt:     createdAt,
		}, true
	}
}

func publicToolResultStateMessage(msg types.Message, createdAt time.Time) (state.Message, bool) {
	for _, block := range msg.Content {
		if block.Type != "tool_result" {
			continue
		}
		return state.Message{
			Role:       "tool",
			ToolCallID: block.ToolUseID,
			ToolName:   publicToolName(msg.Message),
			ToolOutput: firstNonEmpty(block.Content, block.Text),
			CreatedAt:  createdAt,
		}, true
	}
	return state.Message{}, false
}

func stateMessagesToPublicMessages(messages []state.Message) []types.Message {
	out := make([]types.Message, 0, len(messages))
	for _, msg := range messages {
		out = append(out, stateMessageToPublicMessage(&msg))
	}
	return out
}

func stateMessageToPublicMessage(msg *state.Message) types.Message {
	if msg == nil {
		return types.Message{}
	}

	timestamp := msg.CreatedAt
	if timestamp.IsZero() {
		timestamp = time.Now().UTC()
	}

	switch msg.Role {
	case "assistant":
		return types.Message{
			Type:      types.MessageTypeAssistant,
			UUID:      types.UUID(),
			Timestamp: timestamp,
			Content:   stateAssistantContentBlocks(msg),
		}
	case "tool":
		return types.Message{
			Type:      types.MessageTypeTool,
			UUID:      types.UUID(),
			Timestamp: timestamp,
			ToolUseID: msg.ToolCallID,
			Message: map[string]any{
				"tool_name":  msg.ToolName,
				"tool_input": rawJSONToMap(msg.ToolInput),
			},
			Content: []types.ContentBlock{{
				Type:      "tool_result",
				ToolUseID: msg.ToolCallID,
				Content:   msg.ToolOutput,
			}},
		}
	default:
		content := msg.ContentBlocks
		if len(content) == 0 {
			content = []types.ContentBlock{{
				Type: "text",
				Text: msg.Content,
			}}
		}
		return types.Message{
			Type:      types.MessageTypeUser,
			UUID:      types.UUID(),
			Timestamp: timestamp,
			IsMeta:    msg.Hidden,
			Content:   append([]types.ContentBlock(nil), content...),
		}
	}
}

func publicAssistantContent(content []types.ContentBlock) (string, []state.ToolCall) {
	textParts := make([]string, 0, len(content))
	toolCalls := make([]state.ToolCall, 0)
	for _, block := range content {
		switch block.Type {
		case "text":
			if block.Text != "" {
				textParts = append(textParts, block.Text)
			}
		case "tool_use":
			input, _ := json.Marshal(block.Input)
			toolCalls = append(toolCalls, state.ToolCall{
				ID:    block.ID,
				Name:  block.Name,
				Input: input,
			})
		}
	}
	return strings.Join(textParts, "\n"), toolCalls
}

func stateAssistantContentBlocks(msg *state.Message) []types.ContentBlock {
	blocks := make([]types.ContentBlock, 0, len(msg.ToolCalls)+1)
	if strings.TrimSpace(msg.Content) != "" {
		blocks = append(blocks, types.ContentBlock{
			Type: "text",
			Text: msg.Content,
		})
	}
	for _, call := range msg.ToolCalls {
		blocks = append(blocks, types.ContentBlock{
			Type:  "tool_use",
			ID:    call.ID,
			Name:  call.Name,
			Input: rawJSONToMap(call.Input),
		})
	}
	return blocks
}

func publicContentText(content []types.ContentBlock) string {
	parts := make([]string, 0, len(content))
	for _, block := range content {
		if block.Type == "text" && block.Text != "" {
			parts = append(parts, block.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func publicToolOutput(content []types.ContentBlock) string {
	for _, block := range content {
		if block.Type == "tool_result" && block.Content != "" {
			return block.Content
		}
		if block.Type == "text" && block.Text != "" {
			return block.Text
		}
	}
	return ""
}

func publicToolName(message interface{}) string {
	meta, ok := message.(map[string]any)
	if !ok {
		return ""
	}
	name, _ := meta["tool_name"].(string)
	return name
}

func publicToolInput(message interface{}) json.RawMessage {
	meta, ok := message.(map[string]any)
	if !ok {
		return nil
	}
	input, _ := meta["tool_input"].(map[string]any)
	if input == nil {
		return nil
	}
	data, err := json.Marshal(input)
	if err != nil {
		return nil
	}
	return data
}

func rawJSONToMap(raw json.RawMessage) map[string]any {
	if len(raw) == 0 {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil
	}
	return out
}

func plannerToolCallsToState(calls []plannerapi.ToolCall) []state.ToolCall {
	out := make([]state.ToolCall, len(calls))
	for i, call := range calls {
		out[i] = state.ToolCall{
			ID:    call.ID,
			Name:  call.Name,
			Input: call.Input,
		}
	}
	return out
}

func hasHiddenContext(messages []state.Message, content string) bool {
	for _, msg := range messages {
		if msg.Role == "user" && msg.Hidden && msg.Content == content {
			return true
		}
	}
	return false
}

func calculateStateUsage(messages []state.Message) state.Usage {
	usage := state.Usage{}
	for _, msg := range messages {
		switch msg.Role {
		case "user":
			usage.RecordInput(msg.Content)
		case "assistant":
			usage.RecordOutput(msg.Content)
		case "tool":
			usage.RecordOutput(msg.ToolOutput)
		}
	}
	return usage
}

func usageFromMessages(messages []types.Message) Usage {
	usage := Usage{}
	for _, msg := range messages {
		tokens := estimateMessageTokens([]types.Message{msg})
		switch msg.Type {
		case types.MessageTypeAssistant:
			usage.OutputTokens += tokens
		default:
			usage.InputTokens += tokens
		}
	}
	return usage
}

func formatToolExecutionError(name string, err error) string {
	if err == nil {
		return ""
	}
	if name == "" {
		return err.Error()
	}
	return fmt.Sprintf("%s: %v", name, err)
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
