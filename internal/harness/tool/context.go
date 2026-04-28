package tool

import (
	"context"
	"sync"
)

// ToolUseContext contains all the context needed for tool execution.
// It provides access to configuration, state, and callbacks for tool operations.
type ToolUseContext struct {
	// Context for cancellation and timeouts
	Ctx context.Context

	// Options contains configuration for tool execution
	Options ToolOptions

	// State management
	State *ToolState

	// Callbacks for tool operations
	Callbacks *ToolCallbacks

	// Permission context
	PermissionContext *ToolPermissionContext

	// Query tracking
	QueryTracking *QueryChainTracking

	// Agent information
	AgentID   string
	AgentType string

	// Session information
	SessionID      string
	ConversationID string
	ToolUseID      string

	// Messages in the conversation
	Messages []interface{}

	// AbortController for cancellation
	AbortController *AbortController

	// File reading limits
	FileReadingLimits *FileReadingLimits

	// Glob limits
	GlobLimits *GlobLimits

	// Tool decisions cache
	ToolDecisions *sync.Map

	// Flags
	RequireCanUseTool       bool
	PreserveToolUseResults  bool
	UserModified            bool
	IsNonInteractiveSession bool

	// Critical system reminder (experimental)
	CriticalSystemReminder string

	// Content replacement state
	ContentReplacementState interface{}

	// Rendered SystemPrompt
	RenderedSystemPrompt interface{}

	// Local denial tracking
	LocalDenialTracking interface{}

	// Nested memory tracking
	NestedMemoryAttachmentTriggers *sync.Map
	LoadedNestedMemoryPaths        *sync.Map
	DynamicSkillDirTriggers        *sync.Map
	DiscoveredSkillNames           *sync.Map

	// ReadFileState tracks which files have been read/written/edited
	// Used by memory prefetch to filter out duplicate attachments
	ReadFileState map[string]bool
}

// ToolOptions contains configuration options for tool execution.
type ToolOptions struct {
	Commands                []interface{}
	Debug                   bool
	Verbose                 bool
	MainLoopModel           string
	Tools                   []Tool
	ThinkingConfig          interface{}
	MCPClients              []interface{}
	MCPResources            map[string][]interface{}
	AgentDefinitions        interface{}
	MaxBudgetUSD            *float64
	CustomSystemPrompt      string
	AppendSystemPrompt      string
	QuerySource             string
	OnToolUseStart          func(string, map[string]interface{})
	OnToolUseEnd            func(string, interface{}, error)
	OnToolUseProgress       func(string, interface{})
	OnToolUseCancel         func(string)
	OnToolUseRejected       func(string, string)
	OnToolUseError          func(string, error)
	OnToolUseValidationFail func(string, ValidationResult)
}

// ToolState manages mutable state during tool execution.
type ToolState struct {
	mu                             sync.RWMutex
	InProgressToolUseIDs           map[string]bool
	HasInterruptibleToolInProgress bool
	ResponseLength                 int
	FileHistoryState               interface{}
	AttributionState               interface{}
	FileStateCache                 interface{}
	SDKStatus                      interface{}
}

// ToolCallbacks contains callback functions for tool operations.
type ToolCallbacks struct {
	GetWorkingDirectory               func() string
	GetSessionID                      func() string
	GetMainThreadAgentType            func() string
	SetAppState                       func(func(interface{}) interface{})
	GetAppState                       func() interface{}
	GetToolPermissionContext          func() (*ToolPermissionContext, error)
	SetToolJSX                        func(interface{})
	AddNotification                   func(interface{})
	AppendSystemMessage               func(interface{})
	SendOSNotification                func(message, notificationType string)
	SetInProgressToolUseIDs           func(func(map[string]bool) map[string]bool)
	SetHasInterruptibleToolInProgress func(bool)
	SetResponseLength                 func(func(int) int)
	PushAPIMetricsEntry               func(ttftMs int)
	SetStreamMode                     func(mode string)
	OnCompactProgress                 func(event CompactProgressEvent)
	SetSDKStatus                      func(status interface{})
	OpenMessageSelector               func()
	UpdateFileHistoryState            func(func(interface{}) interface{})
	UpdateAttributionState            func(func(interface{}) interface{})
	SetConversationID                 func(id string)
	RequestPrompt                     func(sourceName, toolInputSummary string) func(interface{}) (interface{}, error)
	ElicitURL                         func(params interface{}, signal context.Context) (interface{}, error)
}

// FileReadingLimits defines limits for file reading operations.
type FileReadingLimits struct {
	MaxTokens    *int
	MaxSizeBytes *int64
}

// GlobLimits defines limits for glob operations.
type GlobLimits struct {
	MaxResults *int
}

// NewToolUseContext creates a new ToolUseContext with default values.
func NewToolUseContext(ctx context.Context) *ToolUseContext {
	return &ToolUseContext{
		Ctx:                            ctx,
		State:                          NewToolState(),
		Callbacks:                      &ToolCallbacks{},
		ToolDecisions:                  &sync.Map{},
		NestedMemoryAttachmentTriggers: &sync.Map{},
		LoadedNestedMemoryPaths:        &sync.Map{},
		DynamicSkillDirTriggers:        &sync.Map{},
		DiscoveredSkillNames:           &sync.Map{},
		ReadFileState:                  make(map[string]bool),
	}
}

// NewToolState creates a new ToolState with default values.
func NewToolState() *ToolState {
	return &ToolState{
		InProgressToolUseIDs: make(map[string]bool),
	}
}

// AddInProgressToolUseID adds a tool use ID to the in-progress set.
func (s *ToolState) AddInProgressToolUseID(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.InProgressToolUseIDs[id] = true
}

// RemoveInProgressToolUseID removes a tool use ID from the in-progress set.
func (s *ToolState) RemoveInProgressToolUseID(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.InProgressToolUseIDs, id)
}

// IsToolUseInProgress checks if a tool use ID is in progress.
func (s *ToolState) IsToolUseInProgress(id string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.InProgressToolUseIDs[id]
}

// GetInProgressToolUseIDs returns a copy of the in-progress tool use IDs.
func (s *ToolState) GetInProgressToolUseIDs() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ids := make([]string, 0, len(s.InProgressToolUseIDs))
	for id := range s.InProgressToolUseIDs {
		ids = append(ids, id)
	}
	return ids
}

// SetHasInterruptibleToolInProgress sets whether there's an interruptible tool in progress.
func (s *ToolState) SetHasInterruptibleToolInProgress(value bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.HasInterruptibleToolInProgress = value
}

// GetHasInterruptibleToolInProgress returns whether there's an interruptible tool in progress.
func (s *ToolState) GetHasInterruptibleToolInProgress() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.HasInterruptibleToolInProgress
}

// IncrementResponseLength increments the response length by the given delta.
func (s *ToolState) IncrementResponseLength(delta int) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ResponseLength += delta
	return s.ResponseLength
}

// GetResponseLength returns the current response length.
func (s *ToolState) GetResponseLength() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.ResponseLength
}

// WithContext returns a new ToolUseContext with the given context.
func (t *ToolUseContext) WithContext(ctx context.Context) *ToolUseContext {
	newCtx := *t
	newCtx.Ctx = ctx
	return &newCtx
}

// WithPermissionContext returns a new ToolUseContext with the given permission context.
func (t *ToolUseContext) WithPermissionContext(permCtx *ToolPermissionContext) *ToolUseContext {
	newCtx := *t
	newCtx.PermissionContext = permCtx
	return &newCtx
}

// SetTools replaces the configured tool list for this context.
func (t *ToolUseContext) SetTools(tools []Tool) {
	t.Options.Tools = append([]Tool(nil), tools...)
}

// Tools returns a snapshot of the configured tools.
func (t *ToolUseContext) Tools() []Tool {
	return append([]Tool(nil), t.Options.Tools...)
}

// FindToolByName looks up a configured tool by primary name or alias.
func (t *ToolUseContext) FindToolByName(name string) Tool {
	return FindToolByName(t.Options.Tools, name)
}

// Clone creates a shallow copy of the ToolUseContext.
// Note: Shared state (maps, callbacks) are not deep copied.
func (t *ToolUseContext) Clone() *ToolUseContext {
	newCtx := *t
	return &newCtx
}
