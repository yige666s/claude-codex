package tools

import (
	"context"
	"time"
)

// ToolStatus represents the execution status of a tool
type ToolStatus string

const (
	ToolStatusQueued    ToolStatus = "queued"
	ToolStatusExecuting ToolStatus = "executing"
	ToolStatusCompleted ToolStatus = "completed"
	ToolStatusYielded   ToolStatus = "yielded"
	ToolStatusFailed    ToolStatus = "failed"
	ToolStatusAborted   ToolStatus = "aborted"
)

// PermissionDecision represents the result of permission checking
type PermissionDecision string

const (
	PermissionAllow PermissionDecision = "allow"
	PermissionDeny  PermissionDecision = "deny"
	PermissionAsk   PermissionDecision = "ask"
)

// ToolCall represents a tool invocation request
type ToolCall struct {
	ID         string                 `json:"id"`
	Name       string                 `json:"name"`
	Input      map[string]interface{} `json:"input"`
	Type       string                 `json:"type,omitempty"`
	CacheWrite bool                   `json:"cache_write,omitempty"`
}

// ToolResult represents the result of a tool execution
type ToolResult struct {
	ToolUseID string      `json:"tool_use_id"`
	Content   interface{} `json:"content"`
	IsError   bool        `json:"is_error,omitempty"`
	Type      string      `json:"type,omitempty"`
}

// ToolExecutionContext contains context for tool execution
type ToolExecutionContext struct {
	Context           context.Context
	AbortController   *AbortController
	SessionID         string
	QuerySource       string
	Model             string
	AdditionalContext []string
	ProgressCallback  func(message string)
}

// ToolExecutionResult contains the result and metadata from tool execution
type ToolExecutionResult struct {
	Result            *ToolResult
	Duration          time.Duration
	Error             error
	PermissionDecision PermissionDecision
	AdditionalContext []string
	ModifiedContext   bool
}

// ToolHookContext contains context passed to hooks
type ToolHookContext struct {
	ToolName    string
	ToolInput   map[string]interface{}
	ToolResult  *ToolResult
	Error       error
	SessionID   string
	QuerySource string
}

// ToolHookResult contains the result from hook execution
type ToolHookResult struct {
	Decision          PermissionDecision
	BlockingError     error
	AdditionalContext []string
	ModifiedContext   bool
}

// AbortController manages cancellation of tool execution
type AbortController struct {
	ctx    context.Context
	cancel context.CancelFunc
}

// NewAbortController creates a new abort controller
func NewAbortController(parent context.Context) *AbortController {
	ctx, cancel := context.WithCancel(parent)
	return &AbortController{
		ctx:    ctx,
		cancel: cancel,
	}
}

// Context returns the context
func (a *AbortController) Context() context.Context {
	return a.ctx
}

// Abort cancels the context
func (a *AbortController) Abort() {
	a.cancel()
}

// IsAborted checks if the context is cancelled
func (a *AbortController) IsAborted() bool {
	select {
	case <-a.ctx.Done():
		return true
	default:
		return false
	}
}

// ToolExecutor defines the interface for tool execution
type ToolExecutor interface {
	Execute(ctx *ToolExecutionContext, call *ToolCall) (*ToolResult, error)
	IsConcurrentSafe() bool
	ValidateInput(input map[string]interface{}) error
}

// ToolHook defines the interface for tool hooks
type ToolHook interface {
	PreToolUse(ctx *ToolHookContext) (*ToolHookResult, error)
	PostToolUse(ctx *ToolHookContext) (*ToolHookResult, error)
	PostToolUseFailure(ctx *ToolHookContext) (*ToolHookResult, error)
}

// ToolRegistry manages registered tools
type ToolRegistry struct {
	tools map[string]ToolExecutor
	hooks []ToolHook
}

// NewToolRegistry creates a new tool registry
func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{
		tools: make(map[string]ToolExecutor),
		hooks: make([]ToolHook, 0),
	}
}

// Register registers a tool executor
func (r *ToolRegistry) Register(name string, executor ToolExecutor) {
	r.tools[name] = executor
}

// Get retrieves a tool executor by name
func (r *ToolRegistry) Get(name string) (ToolExecutor, bool) {
	executor, ok := r.tools[name]
	return executor, ok
}

// AddHook adds a tool hook
func (r *ToolRegistry) AddHook(hook ToolHook) {
	r.hooks = append(r.hooks, hook)
}

// GetHooks returns all registered hooks
func (r *ToolRegistry) GetHooks() []ToolHook {
	return r.hooks
}

// Constants for tool execution
const (
	// MaxToolUseConcurrency is the default max concurrent tool executions
	MaxToolUseConcurrency = 10

	// DefaultToolTimeout is the default timeout for tool execution
	DefaultToolTimeout = 5 * time.Minute
)

// ToolExecutionOptions contains options for tool execution
type ToolExecutionOptions struct {
	MaxConcurrency   int
	Timeout          time.Duration
	EnableHooks      bool
	EnableAnalytics  bool
	EnableTelemetry  bool
	ProgressCallback func(message string)
}

// DefaultToolExecutionOptions returns default execution options
func DefaultToolExecutionOptions() *ToolExecutionOptions {
	return &ToolExecutionOptions{
		MaxConcurrency:  MaxToolUseConcurrency,
		Timeout:         DefaultToolTimeout,
		EnableHooks:     true,
		EnableAnalytics: true,
		EnableTelemetry: true,
	}
}
