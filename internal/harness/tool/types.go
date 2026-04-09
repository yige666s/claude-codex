package tool

import (
	"context"
	"encoding/json"
	"errors"
)

// Common errors
var (
	ErrToolNotFound        = errors.New("tool not found")
	ErrToolDisabled        = errors.New("tool is disabled")
	ErrInvalidInput        = errors.New("invalid tool input")
	ErrPermissionDenied    = errors.New("permission denied")
	ErrToolExecutionFailed = errors.New("tool execution failed")
)

// ToolInputJSONSchema represents a JSON Schema for tool input validation.
// It follows the JSON Schema specification for object types.
type ToolInputJSONSchema struct {
	Type       string                            `json:"type"`
	Properties map[string]interface{}            `json:"properties,omitempty"`
	Required   []string                          `json:"required,omitempty"`
	Additional map[string]interface{}            `json:"-"` // For additional schema fields
}

// ValidationResult represents the result of input validation.
type ValidationResult struct {
	Valid      bool   `json:"valid"`
	Message    string `json:"message,omitempty"`
	ErrorCode  int    `json:"error_code,omitempty"`
}

// NewValidationSuccess creates a successful validation result.
func NewValidationSuccess() ValidationResult {
	return ValidationResult{Valid: true}
}

// NewValidationError creates a failed validation result with a message and error code.
func NewValidationError(message string, errorCode int) ValidationResult {
	return ValidationResult{
		Valid:     false,
		Message:   message,
		ErrorCode: errorCode,
	}
}

// ToolResult represents the result of a tool execution.
type ToolResult struct {
	Data            interface{}            `json:"data"`
	NewMessages     []interface{}          `json:"new_messages,omitempty"`
	ContextModifier func(*ToolUseContext)  `json:"-"`
	MCPMeta         *MCPMeta               `json:"mcp_meta,omitempty"`
}

// MCPMeta contains MCP protocol metadata.
type MCPMeta struct {
	Meta              map[string]interface{} `json:"_meta,omitempty"`
	StructuredContent map[string]interface{} `json:"structured_content,omitempty"`
}

// SearchOrReadInfo indicates whether a tool operation is a search, read, or list operation.
type SearchOrReadInfo struct {
	IsSearch bool `json:"is_search"`
	IsRead   bool `json:"is_read"`
	IsList   bool `json:"is_list"`
}

// MCPInfo contains MCP server and tool names for MCP tools.
type MCPInfo struct {
	ServerName string `json:"server_name"`
	ToolName   string `json:"tool_name"`
}

// InterruptBehavior defines how a tool responds to interruption.
type InterruptBehavior string

const (
	InterruptCancel InterruptBehavior = "cancel" // Stop the tool and discard its result
	InterruptBlock  InterruptBehavior = "block"  // Keep running; the new message waits
)

// Tool defines the interface that all tools must implement.
// Tools are the primary mechanism for Claude to interact with the system.
type Tool interface {
	// Name returns the primary name of the tool.
	Name() string

	// Aliases returns optional alternative names for backwards compatibility.
	Aliases() []string

	// SearchHint returns a one-line capability phrase for ToolSearch keyword matching.
	SearchHint() string

	// Description returns the tool's description for the given input and options.
	Description(input map[string]interface{}, opts DescriptionOptions) (string, error)

	// Call executes the tool with the given arguments and context.
	Call(ctx context.Context, args map[string]interface{}, toolCtx *ToolUseContext) (*ToolResult, error)

	// InputSchema returns the JSON schema for validating tool input.
	InputSchema() *ToolInputJSONSchema

	// OutputSchema returns the JSON schema for validating tool output (optional).
	OutputSchema() *ToolInputJSONSchema

	// ValidateInput checks if the input is valid for this tool.
	ValidateInput(input map[string]interface{}, toolCtx *ToolUseContext) (ValidationResult, error)

	// CheckPermissions determines if the user should be asked for permission.
	CheckPermissions(input map[string]interface{}, toolCtx *ToolUseContext) (*PermissionResult, error)

	// IsEnabled returns whether this tool is currently enabled.
	IsEnabled() bool

	// IsConcurrencySafe returns whether this tool can be safely called concurrently.
	IsConcurrencySafe(input map[string]interface{}) bool

	// IsReadOnly returns whether this tool only reads data without modifying it.
	IsReadOnly(input map[string]interface{}) bool

	// IsDestructive returns whether this tool performs irreversible operations.
	IsDestructive(input map[string]interface{}) bool

	// IsOpenWorld returns whether this tool can access external resources.
	IsOpenWorld(input map[string]interface{}) bool

	// RequiresUserInteraction returns whether this tool requires user interaction.
	RequiresUserInteraction() bool

	// InterruptBehavior returns how this tool responds to interruption.
	InterruptBehavior() InterruptBehavior

	// IsSearchOrReadCommand returns information about search/read operations.
	IsSearchOrReadCommand(input map[string]interface{}) *SearchOrReadInfo

	// UserFacingName returns a human-readable name for display.
	UserFacingName(input map[string]interface{}) string

	// GetActivityDescription returns a present-tense activity description for spinner display.
	GetActivityDescription(input map[string]interface{}) string

	// ToAutoClassifierInput returns a compact representation for the auto-mode security classifier.
	ToAutoClassifierInput(input map[string]interface{}) interface{}

	// GetPath returns the file path if this tool operates on a file.
	GetPath(input map[string]interface{}) string

	// PreparePermissionMatcher prepares a matcher for hook if conditions.
	PreparePermissionMatcher(input map[string]interface{}) (func(pattern string) bool, error)

	// BackfillObservableInput mutates input to add legacy/derived fields for observers.
	BackfillObservableInput(input map[string]interface{})

	// InputsEquivalent checks if two inputs are functionally equivalent.
	InputsEquivalent(a, b map[string]interface{}) bool

	// IsTransparentWrapper returns true if this tool delegates all rendering to its progress handler.
	IsTransparentWrapper() bool

	// MaxResultSizeChars returns the maximum size in characters for tool result before persistence.
	MaxResultSizeChars() int

	// IsMCP returns whether this is an MCP tool.
	IsMCP() bool

	// IsLSP returns whether this is an LSP tool.
	IsLSP() bool

	// ShouldDefer returns whether this tool should be deferred (sent with defer_loading: true).
	ShouldDefer() bool

	// AlwaysLoad returns whether this tool should never be deferred.
	AlwaysLoad() bool

	// MCPInfo returns MCP server and tool names for MCP tools.
	MCPInfo() *MCPInfo

	// Strict returns whether strict mode is enabled for this tool.
	Strict() bool
}

// DescriptionOptions contains options for generating tool descriptions.
type DescriptionOptions struct {
	IsNonInteractiveSession bool
	ToolPermissionContext   *ToolPermissionContext
	Tools                   []Tool
}

// PermissionResult represents the result of a permission check.
type PermissionResult struct {
	Behavior     PermissionBehavior     `json:"behavior"`
	UpdatedInput map[string]interface{} `json:"updated_input,omitempty"`
	Reason       string                 `json:"reason,omitempty"`
}

// PermissionBehavior defines how to handle a tool call based on permissions.
type PermissionBehavior string

const (
	PermissionAllow PermissionBehavior = "allow"
	PermissionDeny  PermissionBehavior = "deny"
	PermissionAsk   PermissionBehavior = "ask"
)

// QueryChainTracking tracks query chain information.
type QueryChainTracking struct {
	ChainID string `json:"chain_id"`
	Depth   int    `json:"depth"`
}

// CompactProgressEvent represents progress events during compaction.
type CompactProgressEvent struct {
	Type     string `json:"type"`
	HookType string `json:"hook_type,omitempty"`
}

// ToolMatchesName checks if a tool matches the given name (primary name or alias).
func ToolMatchesName(tool Tool, name string) bool {
	if tool.Name() == name {
		return true
	}
	for _, alias := range tool.Aliases() {
		if alias == name {
			return true
		}
	}
	return false
}

// FindToolByName finds a tool by name or alias from a list of tools.
func FindToolByName(tools []Tool, name string) Tool {
	for _, tool := range tools {
		if ToolMatchesName(tool, name) {
			return tool
		}
	}
	return nil
}

// MarshalJSON implements custom JSON marshaling for ToolInputJSONSchema.
func (s *ToolInputJSONSchema) MarshalJSON() ([]byte, error) {
	m := map[string]interface{}{
		"type": s.Type,
	}
	if s.Properties != nil {
		m["properties"] = s.Properties
	}
	if s.Required != nil {
		m["required"] = s.Required
	}
	for k, v := range s.Additional {
		m[k] = v
	}
	return json.Marshal(m)
}

// UnmarshalJSON implements custom JSON unmarshaling for ToolInputJSONSchema.
func (s *ToolInputJSONSchema) UnmarshalJSON(data []byte) error {
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		return err
	}

	if t, ok := m["type"].(string); ok {
		s.Type = t
	}
	if props, ok := m["properties"].(map[string]interface{}); ok {
		s.Properties = props
	}
	if req, ok := m["required"].([]interface{}); ok {
		s.Required = make([]string, len(req))
		for i, r := range req {
			if str, ok := r.(string); ok {
				s.Required[i] = str
			}
		}
	}

	// Store additional fields
	s.Additional = make(map[string]interface{})
	for k, v := range m {
		if k != "type" && k != "properties" && k != "required" {
			s.Additional[k] = v
		}
	}

	return nil
}

// CanUseToolFn is a function type for checking if a tool can be used.
// It determines whether the tool should be allowed, denied, or requires user permission.
type CanUseToolFn func(
	tool Tool,
	input map[string]interface{},
	toolUseContext *ToolUseContext,
	assistantMessage interface{},
	toolUseID string,
	forceDecision *string,
) (*PermissionResult, error)
