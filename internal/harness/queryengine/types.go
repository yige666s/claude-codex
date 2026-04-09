// Package engine provides the core query engine for managing conversation lifecycle and session state.
// It handles message submission, streaming responses, session state management, and history snipping.
package engine

import (
	"context"
	"sync"
	"time"

	"github.com/ding/claude-code/claude-go/internal/harness/tool"
)

// QueryEngineConfig contains all configuration options for the QueryEngine.
type QueryEngineConfig struct {
	// Working directory for the session
	Cwd string

	// Tools available for execution
	Tools []tool.Tool

	// Commands available (slash commands, etc.)
	Commands []interface{}

	// MCP client connections
	MCPClients []interface{}

	// Agent definitions
	Agents []interface{}

	// Permission checker function
	CanUseTool CanUseToolFunc

	// State management callbacks
	GetAppState func() interface{}
	SetAppState func(func(interface{}) interface{})

	// Initial messages to start the conversation with
	InitialMessages []Message

	// File state cache for tracking read files
	ReadFileCache interface{}

	// Custom SystemPrompt override
	CustomSystemPrompt string

	// Additional SystemPrompt to append
	AppendSystemPrompt string

	// User-specified model override
	UserSpecifiedModel string

	// Fallback model if primary fails
	FallbackModel string

	// Thinking configuration
	ThinkingConfig interface{}

	// Maximum number of turns before stopping
	MaxTurns int

	// Maximum budget in USD
	MaxBudgetUSD *float64

	// Task budget tracking
	TaskBudget *TaskBudget

	// JSON schema for structured output
	JSONSchema map[string]interface{}

	// Verbose logging
	Verbose bool

	// Replay user messages in SDK mode
	ReplayUserMessages bool

	// Include partial streaming messages
	IncludePartialMessages bool

	// Handler for URL elicitations
	HandleElicitation func(params interface{}, signal context.Context) (interface{}, error)

	// SDK status callback
	SetSDKStatus func(status SDKStatus)

	// Abort controller for cancellation
	AbortController context.CancelFunc

	// Orphaned permission to handle
	OrphanedPermission *OrphanedPermission

	// Snip replay callback for history management
	SnipReplay SnipReplayFunc
}

// TaskBudget tracks budget allocation for a task.
type TaskBudget struct {
	Total float64
	mu    sync.RWMutex
}

// CanUseToolFunc is the function signature for permission checking.
type CanUseToolFunc func(
	tool tool.Tool,
	input map[string]interface{},
	toolCtx *tool.ToolUseContext,
	assistantMessage interface{},
	toolUseID string,
	forceDecision bool,
) (*PermissionResult, error)

// PermissionResult represents the result of a permission check.
type PermissionResult struct {
	Behavior string // "allow", "deny", "ask"
	Reason   string
}

// SnipReplayFunc handles snip boundary messages and returns replayed history.
type SnipReplayFunc func(yieldedSystemMsg Message, store []Message) *SnipReplayResult

// SnipReplayResult contains the result of a snip replay operation.
type SnipReplayResult struct {
	Messages []Message
	Executed bool
}

// OrphanedPermission represents a permission that was granted but not yet used.
type OrphanedPermission struct {
	ToolName string
	Input    map[string]interface{}
}

// Message represents a conversation message.
type Message struct {
	Type      string                 `json:"type"` // "user", "assistant", "system", "progress", "attachment", "stream_event", "tombstone"
	UUID      string                 `json:"uuid"`
	Timestamp time.Time              `json:"timestamp"`
	Message   interface{}            `json:"message,omitempty"`
	Content   interface{}            `json:"content,omitempty"`
	Subtype   string                 `json:"subtype,omitempty"`
	IsMeta    bool                   `json:"is_meta,omitempty"`
	Data      interface{}            `json:"data,omitempty"`
	Event     interface{}            `json:"event,omitempty"`
	ToolUseID string                 `json:"tool_use_id,omitempty"`
	Attachment interface{}           `json:"attachment,omitempty"`

	// Compact metadata
	CompactMetadata *CompactMetadata `json:"compact_metadata,omitempty"`

	// Additional fields
	IsCompactSummary           bool   `json:"is_compact_summary,omitempty"`
	IsVisibleInTranscriptOnly  bool   `json:"is_visible_in_transcript_only,omitempty"`
	ToolUseResult              bool   `json:"tool_use_result,omitempty"`
	IsApiErrorMessage          bool   `json:"is_api_error_message,omitempty"`
}

// CompactMetadata contains metadata for compact boundary messages.
type CompactMetadata struct {
	PreservedSegment *PreservedSegment `json:"preserved_segment,omitempty"`
}

// PreservedSegment identifies the preserved portion of history.
type PreservedSegment struct {
	TailUUID string `json:"tail_uuid"`
}

// SDKMessage represents a message in the SDK protocol.
type SDKMessage struct {
	Type              string                 `json:"type"`
	Subtype           string                 `json:"subtype,omitempty"`
	Message           interface{}            `json:"message,omitempty"`
	SessionID         string                 `json:"session_id"`
	ParentToolUseID   *string                `json:"parent_tool_use_id"`
	UUID              string                 `json:"uuid"`
	Timestamp         *time.Time             `json:"timestamp,omitempty"`
	IsReplay          bool                   `json:"is_replay,omitempty"`
	IsSynthetic       bool                   `json:"is_synthetic,omitempty"`
	Event             interface{}            `json:"event,omitempty"`
	DurationMS        int64                  `json:"duration_ms,omitempty"`
	DurationAPIMS     int64                  `json:"duration_api_ms,omitempty"`
	IsError           bool                   `json:"is_error,omitempty"`
	NumTurns          int                    `json:"num_turns,omitempty"`
	Result            string                 `json:"result,omitempty"`
	StopReason        string                 `json:"stop_reason,omitempty"`
	TotalCostUSD      float64                `json:"total_cost_usd,omitempty"`
	Usage             *Usage                 `json:"usage,omitempty"`
	ModelUsage        map[string]*Usage      `json:"model_usage,omitempty"`
	PermissionDenials []PermissionDenial     `json:"permission_denials,omitempty"`
	StructuredOutput  interface{}            `json:"structured_output,omitempty"`
	FastModeState     interface{}            `json:"fast_mode_state,omitempty"`
	Errors            []string               `json:"errors,omitempty"`
	CompactMetadata   map[string]interface{} `json:"compact_metadata,omitempty"`
}

// Usage tracks token and cost usage.
type Usage struct {
	InputTokens        int `json:"input_tokens"`
	OutputTokens       int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens,omitempty"`
}

// PermissionDenial records a denied tool permission.
type PermissionDenial struct {
	ToolName  string                 `json:"tool_name"`
	ToolUseID string                 `json:"tool_use_id"`
	ToolInput map[string]interface{} `json:"tool_input"`
}

// SDKStatus represents the current status of the SDK session.
type SDKStatus struct {
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

// SubmitOptions contains options for submitting a message.
type SubmitOptions struct {
	UUID   string
	IsMeta bool
}

// StreamEvent represents a streaming event from the API.
type StreamEvent struct {
	Type    string      `json:"type"`
	Message interface{} `json:"message,omitempty"`
	Usage   *Usage      `json:"usage,omitempty"`
	Delta   interface{} `json:"delta,omitempty"`
}

// AccumulateUsage adds usage from one Usage to another.
func AccumulateUsage(total, current *Usage) *Usage {
	if total == nil {
		total = &Usage{}
	}
	if current == nil {
		return total
	}
	return &Usage{
		InputTokens:              total.InputTokens + current.InputTokens,
		OutputTokens:             total.OutputTokens + current.OutputTokens,
		CacheCreationInputTokens: total.CacheCreationInputTokens + current.CacheCreationInputTokens,
		CacheReadInputTokens:     total.CacheReadInputTokens + current.CacheReadInputTokens,
	}
}

// UpdateUsage updates total usage with delta usage.
func UpdateUsage(total, delta *Usage) *Usage {
	return AccumulateUsage(total, delta)
}

// EmptyUsage returns a zero-valued Usage.
func EmptyUsage() *Usage {
	return &Usage{}
}
