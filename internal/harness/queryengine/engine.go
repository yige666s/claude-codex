// Package engine provides a TS-aligned QueryEngine facade over the Go query runtime.
package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"claude-codex/internal/harness/budget"
	"claude-codex/internal/harness/mcp"
	"claude-codex/internal/harness/query"
	"claude-codex/internal/harness/tool"
	publictypes "claude-codex/internal/public/types"

	"github.com/google/uuid"
)

// QueryEngine is the TS-aligned conversation engine facade.
//
// The concrete query lifecycle lives in internal/harness/query. This package
// keeps the QueryEngine surface stable and SDK-shaped while delegating the
// actual turn execution to the shared query runtime.
type QueryEngine struct {
	config        QueryEngineConfig
	innerConfig   *query.QueryEngineConfig
	inner         *query.QueryEngine
	initErr       error
	sessionID     string
	readFileState interface{}
}

// NewQueryEngine creates a new QueryEngine.
func NewQueryEngine(config QueryEngineConfig) *QueryEngine {
	sessionID := configSessionID(config)
	innerConfig := toQueryConfig(config, sessionID)

	inner, err := query.NewQueryEngine(innerConfig)

	return &QueryEngine{
		config:        config,
		innerConfig:   innerConfig,
		inner:         inner,
		initErr:       err,
		sessionID:     sessionID,
		readFileState: config.ReadFileCache,
	}
}

// SubmitMessage submits a new message and yields SDK-shaped events.
func (qe *QueryEngine) SubmitMessage(
	ctx context.Context,
	prompt interface{},
	options *SubmitOptions,
) (<-chan SDKMessage, error) {
	if qe.initErr != nil {
		return errorResultChannel(qe.sessionID, qe.initErr), nil
	}
	if qe.inner == nil {
		return nil, fmt.Errorf("query engine is not initialized")
	}

	if options == nil {
		options = &SubmitOptions{}
	}

	innerOptions := &query.SubmitMessageOptions{
		UUID:   options.UUID,
		IsMeta: options.IsMeta,
	}
	if qe.innerConfig != nil && qe.innerConfig.TokenBudget == nil {
		if promptText, ok := prompt.(string); ok {
			if parsed := budget.ParseTokenBudget(promptText); parsed > 0 {
				qe.innerConfig.TokenBudget = &parsed
			}
		}
	}

	queryChan, err := qe.inner.SubmitMessage(ctx, normalizePrompt(prompt), innerOptions)
	if err != nil {
		return nil, err
	}

	out := make(chan SDKMessage, 100)
	startedAt := time.Now()

	go func() {
		defer close(out)

		turnCount := 0
		var queryErrors []string
		for msg := range queryChan {
			if errText := querySystemErrorText(msg); errText != "" {
				queryErrors = append(queryErrors, errText)
				continue
			}
			normalized := qe.normalizeSDKMessage(msg)
			if normalized.Type == "user" {
				turnCount++
			}

			select {
			case out <- normalized:
			case <-ctx.Done():
				return
			}
		}

		if len(queryErrors) > 0 {
			result := SDKMessage{
				Type:              "result",
				Subtype:           "error_during_execution",
				SessionID:         qe.sessionID,
				UUID:              uuid.New().String(),
				DurationMS:        time.Since(startedAt).Milliseconds(),
				IsError:           true,
				NumTurns:          turnCount,
				Usage:             qe.GetTotalUsage(),
				PermissionDenials: qe.GetPermissionDenials(),
				Errors:            queryErrors,
			}
			select {
			case out <- result:
			case <-ctx.Done():
			}
			return
		}

		result := SDKMessage{
			Type:              "result",
			Subtype:           "success",
			SessionID:         qe.sessionID,
			UUID:              uuid.New().String(),
			DurationMS:        time.Since(startedAt).Milliseconds(),
			NumTurns:          turnCount,
			Usage:             qe.GetTotalUsage(),
			PermissionDenials: qe.GetPermissionDenials(),
		}

		select {
		case out <- result:
		case <-ctx.Done():
		}
	}()

	return out, nil
}

func querySystemErrorText(msg publictypes.Message) string {
	if msg.Type != publictypes.MessageTypeSystem || !msg.IsApiErrorMessage {
		return ""
	}
	text := strings.TrimSpace(publicSystemMessageText(msg))
	if text == "" {
		return "query execution failed"
	}
	return strings.TrimSpace(strings.TrimPrefix(text, "Error:"))
}

func publicSystemMessageText(msg publictypes.Message) string {
	parts := make([]string, 0, len(msg.Content))
	for _, block := range msg.Content {
		switch {
		case strings.TrimSpace(block.Text) != "":
			parts = append(parts, strings.TrimSpace(block.Text))
		case strings.TrimSpace(block.Content) != "":
			parts = append(parts, strings.TrimSpace(block.Content))
		}
	}
	return strings.Join(parts, "\n")
}

// Interrupt aborts the current query execution.
func (qe *QueryEngine) Interrupt() {
	if qe.inner != nil {
		qe.inner.Abort()
	}
}

// GetMessages returns a copy of the current message history.
func (qe *QueryEngine) GetMessages() []Message {
	if qe.inner == nil {
		return cloneMessages(qe.config.InitialMessages)
	}

	publicMessages := qe.inner.GetMessages()
	out := make([]Message, 0, len(publicMessages))
	for _, msg := range publicMessages {
		out = append(out, fromPublicMessage(msg))
	}
	return out
}

// GetReadFileState returns the tracked read file cache reference.
func (qe *QueryEngine) GetReadFileState() interface{} {
	return qe.readFileState
}

// GetSessionID returns the stable session ID for this engine.
func (qe *QueryEngine) GetSessionID() string {
	return qe.sessionID
}

// SetModel updates the user-specified model override.
func (qe *QueryEngine) SetModel(model string) {
	qe.config.UserSpecifiedModel = model
	if qe.innerConfig != nil {
		qe.innerConfig.UserSpecifiedModel = model
	}
}

// GetTotalUsage returns accumulated usage in SDK-facing shape.
func (qe *QueryEngine) GetTotalUsage() *Usage {
	if qe.inner == nil {
		return EmptyUsage()
	}

	usage := qe.inner.GetUsage()
	return &Usage{
		InputTokens:              usage.InputTokens,
		OutputTokens:             usage.OutputTokens,
		CacheCreationInputTokens: usage.CacheWrites,
		CacheReadInputTokens:     usage.CacheReads,
	}
}

// GetPermissionDenials returns permission denials in SDK-facing shape.
func (qe *QueryEngine) GetPermissionDenials() []PermissionDenial {
	if qe.inner == nil {
		return nil
	}

	denials := qe.inner.GetPermissionDenials()
	out := make([]PermissionDenial, 0, len(denials))
	for _, denial := range denials {
		out = append(out, PermissionDenial{
			ToolName:  denial.ToolName,
			ToolUseID: denial.ToolUseID,
			ToolInput: denial.ToolInput,
		})
	}
	return out
}

// Ask is a convenience wrapper around QueryEngine for one-shot usage.
func Ask(ctx context.Context, config QueryEngineConfig, prompt interface{}) (<-chan SDKMessage, error) {
	engine := NewQueryEngine(config)
	return engine.SubmitMessage(ctx, prompt, nil)
}

func (qe *QueryEngine) normalizeSDKMessage(msg publictypes.Message) SDKMessage {
	normalized := SDKMessage{
		Type:      string(msg.Type),
		Subtype:   msg.Subtype,
		SessionID: qe.sessionID,
		UUID:      msg.UUID,
		Timestamp: timestampPtr(msg.Timestamp),
		Message:   fromPublicMessage(msg),
		Event:     msg.Event,
	}

	if msg.Type == publictypes.MessageTypeStreamEvent {
		normalized.Type = "stream_event"
	}

	return normalized
}

func configSessionID(config QueryEngineConfig) string {
	if config.SessionID != "" {
		return config.SessionID
	}
	return "session-" + uuid.NewString()
}

func toQueryConfig(config QueryEngineConfig, sessionID string) *query.QueryEngineConfig {
	queryConfig := &query.QueryEngineConfig{
		WorkingDir:             config.Cwd,
		InitialMessages:        toPublicMessages(config.InitialMessages),
		ReadFileCache:          asQueryFileStateCache(config.ReadFileCache),
		Planner:                config.Planner,
		ToolDescriptors:        config.ToolDescriptors,
		CustomSystemPrompt:     config.CustomSystemPrompt,
		AppendSystemPrompt:     config.AppendSystemPrompt,
		UserSpecifiedModel:     config.UserSpecifiedModel,
		FallbackModel:          config.FallbackModel,
		MaxTurns:               config.MaxTurns,
		JSONSchema:             config.JSONSchema,
		Verbose:                config.Verbose,
		ReplayUserMessages:     config.ReplayUserMessages,
		IncludePartialMessages: config.IncludePartialMessages,
		SessionID:              sessionID,
		PermissionMode:         "default",
	}

	if config.MaxBudgetUSD != nil {
		queryConfig.MaxBudgetUSD = *config.MaxBudgetUSD
	}
	if config.TaskBudget != nil {
		queryConfig.TaskBudget = &query.TaskBudget{Total: int(config.TaskBudget.Total)}
	}
	if config.TokenBudget != nil {
		queryConfig.TokenBudget = config.TokenBudget
	}
	if config.ThinkingConfig != nil {
		queryConfig.ThinkingConfig = &query.ThinkingConfig{Type: extractThinkingType(config.ThinkingConfig)}
	}
	if config.OrphanedPermission != nil {
		queryConfig.OrphanedPermission = &query.OrphanedPermission{
			ToolName:  config.OrphanedPermission.ToolName,
			ToolUseID: "",
			Input:     config.OrphanedPermission.Input,
		}
	}

	queryConfig.Tools = append([]tool.Tool(nil), config.Tools...)
	queryConfig.MCPClients = append([]*mcp.Client(nil), config.MCPClients...)
	queryConfig.HookExecutor = config.HookExecutor
	queryConfig.CanUseTool = wrapCanUseTool(config)
	if config.ExecuteTool != nil {
		queryConfig.ExecuteTool = func(ctx context.Context, toolUseID, name string, input json.RawMessage) (string, error) {
			return config.ExecuteTool(ctx, toolUseID, name, input)
		}
	}

	return queryConfig
}

func wrapCanUseTool(config QueryEngineConfig) query.CanUseToolFunc {
	if config.CanUseTool == nil {
		return func(ctx context.Context, toolDef tool.Tool, input map[string]interface{}, toolUseID string) (query.PermissionResult, error) {
			return query.PermissionResult{Behavior: "allow"}, nil
		}
	}

	return func(ctx context.Context, toolDef tool.Tool, input map[string]interface{}, toolUseID string) (query.PermissionResult, error) {
		result, err := config.CanUseTool(toolDef, input, nil, nil, toolUseID, false)
		if err != nil {
			return query.PermissionResult{}, err
		}
		if result == nil {
			return query.PermissionResult{Behavior: "allow"}, nil
		}
		return query.PermissionResult{
			Behavior: result.Behavior,
			Reason:   result.Reason,
		}, nil
	}
}

func asQueryFileStateCache(cache interface{}) *query.FileStateCache {
	typed, _ := cache.(*query.FileStateCache)
	return typed
}

func normalizePrompt(prompt interface{}) interface{} {
	switch v := prompt.(type) {
	case string:
		return v
	case []publictypes.ContentBlock:
		return v
	default:
		return prompt
	}
}

func toPublicMessages(messages []Message) []publictypes.Message {
	out := make([]publictypes.Message, 0, len(messages))
	for _, msg := range messages {
		out = append(out, toPublicMessage(msg))
	}
	return out
}

func cloneMessages(messages []Message) []Message {
	out := make([]Message, len(messages))
	copy(out, messages)
	return out
}

func toPublicMessage(msg Message) publictypes.Message {
	return publictypes.Message{
		Type:                      publictypes.MessageType(msg.Type),
		UUID:                      msg.UUID,
		Timestamp:                 msg.Timestamp,
		Message:                   msg.Message,
		Content:                   toPublicContentBlocks(msg.Content),
		Subtype:                   msg.Subtype,
		IsMeta:                    msg.IsMeta,
		Data:                      msg.Data,
		Event:                     msg.Event,
		ToolUseID:                 msg.ToolUseID,
		Attachment:                msg.Attachment,
		CompactMetadata:           toPublicCompactMetadata(msg.CompactMetadata),
		IsCompactSummary:          msg.IsCompactSummary,
		IsVisibleInTranscriptOnly: msg.IsVisibleInTranscriptOnly,
		ToolUseResult:             msg.ToolUseResult,
		IsApiErrorMessage:         msg.IsApiErrorMessage,
	}
}

func fromPublicMessage(msg publictypes.Message) Message {
	return Message{
		Type:                      string(msg.Type),
		UUID:                      msg.UUID,
		Timestamp:                 msg.Timestamp,
		Message:                   msg.Message,
		Content:                   fromPublicContentBlocks(msg.Content),
		Subtype:                   msg.Subtype,
		IsMeta:                    msg.IsMeta,
		Data:                      msg.Data,
		Event:                     msg.Event,
		ToolUseID:                 msg.ToolUseID,
		Attachment:                msg.Attachment,
		CompactMetadata:           fromPublicCompactMetadata(msg.CompactMetadata),
		IsCompactSummary:          msg.IsCompactSummary,
		IsVisibleInTranscriptOnly: msg.IsVisibleInTranscriptOnly,
		ToolUseResult:             msg.ToolUseResult,
		IsApiErrorMessage:         msg.IsApiErrorMessage,
	}
}

func toPublicContentBlocks(content interface{}) []publictypes.ContentBlock {
	switch typed := content.(type) {
	case nil:
		return nil
	case string:
		return []publictypes.ContentBlock{{Type: "text", Text: typed}}
	case []publictypes.ContentBlock:
		return typed
	default:
		return nil
	}
}

func fromPublicContentBlocks(content []publictypes.ContentBlock) interface{} {
	if len(content) == 0 {
		return nil
	}
	if len(content) == 1 && content[0].Type == "text" && content[0].Text != "" {
		return content[0].Text
	}
	return content
}

func toPublicCompactMetadata(meta *CompactMetadata) *publictypes.CompactMetadata {
	if meta == nil {
		return nil
	}
	result := &publictypes.CompactMetadata{}
	if meta.PreservedSegment != nil {
		result.PreservedSegment = &publictypes.PreservedSegment{
			TailUUID: meta.PreservedSegment.TailUUID,
		}
	}
	return result
}

func fromPublicCompactMetadata(meta *publictypes.CompactMetadata) *CompactMetadata {
	if meta == nil {
		return nil
	}
	result := &CompactMetadata{}
	if meta.PreservedSegment != nil {
		result.PreservedSegment = &PreservedSegment{
			TailUUID: meta.PreservedSegment.TailUUID,
		}
	}
	return result
}

func extractThinkingType(value interface{}) string {
	switch typed := value.(type) {
	case string:
		return typed
	case map[string]interface{}:
		if kind, ok := typed["type"].(string); ok {
			return kind
		}
	}
	return ""
}

func timestampPtr(ts time.Time) *time.Time {
	if ts.IsZero() {
		return nil
	}
	return &ts
}

func errorResultChannel(sessionID string, err error) <-chan SDKMessage {
	out := make(chan SDKMessage, 1)
	out <- SDKMessage{
		Type:      "result",
		Subtype:   "error_during_execution",
		SessionID: sessionID,
		UUID:      uuid.New().String(),
		IsError:   true,
		Errors:    []string{err.Error()},
	}
	close(out)
	return out
}
