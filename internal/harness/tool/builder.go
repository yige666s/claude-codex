package tool

import (
	"context"
	"fmt"
)

// BaseTool provides default implementations for the Tool interface.
// Embed this in your tool implementations to get sensible defaults.
type BaseTool struct {
	name                string
	aliases             []string
	searchHint          string
	inputSchema         *ToolInputJSONSchema
	outputSchema        *ToolInputJSONSchema
	maxResultSizeChars  int
	isMCP               bool
	isLSP               bool
	shouldDefer         bool
	alwaysLoad          bool
	mcpInfo             *MCPInfo
	strict              bool
}

// ToolBuilder helps construct tools with default values.
type ToolBuilder struct {
	tool *BaseTool
}

// NewToolBuilder creates a new ToolBuilder with the given name.
func NewToolBuilder(name string) *ToolBuilder {
	return &ToolBuilder{
		tool: &BaseTool{
			name:               name,
			aliases:            []string{},
			searchHint:         "",
			maxResultSizeChars: 100000, // Default 100k chars
			isMCP:              false,
			isLSP:              false,
			shouldDefer:        false,
			alwaysLoad:         false,
			strict:             false,
		},
	}
}

// WithAliases sets the tool aliases.
func (b *ToolBuilder) WithAliases(aliases ...string) *ToolBuilder {
	b.tool.aliases = aliases
	return b
}

// WithSearchHint sets the search hint.
func (b *ToolBuilder) WithSearchHint(hint string) *ToolBuilder {
	b.tool.searchHint = hint
	return b
}

// WithInputSchema sets the input schema.
func (b *ToolBuilder) WithInputSchema(schema *ToolInputJSONSchema) *ToolBuilder {
	b.tool.inputSchema = schema
	return b
}

// WithOutputSchema sets the output schema.
func (b *ToolBuilder) WithOutputSchema(schema *ToolInputJSONSchema) *ToolBuilder {
	b.tool.outputSchema = schema
	return b
}

// WithMaxResultSizeChars sets the maximum result size in characters.
func (b *ToolBuilder) WithMaxResultSizeChars(size int) *ToolBuilder {
	b.tool.maxResultSizeChars = size
	return b
}

// WithMCP marks this as an MCP tool.
func (b *ToolBuilder) WithMCP(serverName, toolName string) *ToolBuilder {
	b.tool.isMCP = true
	b.tool.mcpInfo = &MCPInfo{
		ServerName: serverName,
		ToolName:   toolName,
	}
	return b
}

// WithLSP marks this as an LSP tool.
func (b *ToolBuilder) WithLSP() *ToolBuilder {
	b.tool.isLSP = true
	return b
}

// WithDefer marks this tool as deferred.
func (b *ToolBuilder) WithDefer() *ToolBuilder {
	b.tool.shouldDefer = true
	return b
}

// WithAlwaysLoad marks this tool to always load.
func (b *ToolBuilder) WithAlwaysLoad() *ToolBuilder {
	b.tool.alwaysLoad = true
	return b
}

// WithStrict enables strict mode for this tool.
func (b *ToolBuilder) WithStrict() *ToolBuilder {
	b.tool.strict = true
	return b
}

// Build returns the configured BaseTool.
func (b *ToolBuilder) Build() *BaseTool {
	return b.tool
}

// Default implementations for BaseTool

func (t *BaseTool) Name() string {
	return t.name
}

func (t *BaseTool) Aliases() []string {
	return t.aliases
}

func (t *BaseTool) SearchHint() string {
	return t.searchHint
}

func (t *BaseTool) InputSchema() *ToolInputJSONSchema {
	return t.inputSchema
}

func (t *BaseTool) OutputSchema() *ToolInputJSONSchema {
	return t.outputSchema
}

func (t *BaseTool) IsEnabled() bool {
	return true
}

func (t *BaseTool) IsConcurrencySafe(input map[string]interface{}) bool {
	return false
}

func (t *BaseTool) IsReadOnly(input map[string]interface{}) bool {
	return false
}

func (t *BaseTool) IsDestructive(input map[string]interface{}) bool {
	return false
}

func (t *BaseTool) IsOpenWorld(input map[string]interface{}) bool {
	return false
}

func (t *BaseTool) RequiresUserInteraction() bool {
	return false
}

func (t *BaseTool) InterruptBehavior() InterruptBehavior {
	return InterruptBlock
}

func (t *BaseTool) IsSearchOrReadCommand(input map[string]interface{}) *SearchOrReadInfo {
	return &SearchOrReadInfo{
		IsSearch: false,
		IsRead:   false,
		IsList:   false,
	}
}

func (t *BaseTool) UserFacingName(input map[string]interface{}) string {
	return t.name
}

func (t *BaseTool) GetActivityDescription(input map[string]interface{}) string {
	return ""
}

func (t *BaseTool) ToAutoClassifierInput(input map[string]interface{}) interface{} {
	return ""
}

func (t *BaseTool) GetPath(input map[string]interface{}) string {
	return ""
}

func (t *BaseTool) PreparePermissionMatcher(input map[string]interface{}) (func(pattern string) bool, error) {
	return func(pattern string) bool { return false }, nil
}

func (t *BaseTool) BackfillObservableInput(input map[string]interface{}) {
	// No-op by default
}

func (t *BaseTool) InputsEquivalent(a, b map[string]interface{}) bool {
	return false
}

func (t *BaseTool) IsTransparentWrapper() bool {
	return false
}

func (t *BaseTool) MaxResultSizeChars() int {
	return t.maxResultSizeChars
}

func (t *BaseTool) IsMCP() bool {
	return t.isMCP
}

func (t *BaseTool) IsLSP() bool {
	return t.isLSP
}

func (t *BaseTool) ShouldDefer() bool {
	return t.shouldDefer
}

func (t *BaseTool) AlwaysLoad() bool {
	return t.alwaysLoad
}

func (t *BaseTool) MCPInfo() *MCPInfo {
	return t.mcpInfo
}

func (t *BaseTool) Strict() bool {
	return t.strict
}

// ValidateInput provides a default implementation that always succeeds.
func (t *BaseTool) ValidateInput(input map[string]interface{}, toolCtx *ToolUseContext) (ValidationResult, error) {
	return NewValidationSuccess(), nil
}

// CheckPermissions provides a default implementation that always allows.
func (t *BaseTool) CheckPermissions(input map[string]interface{}, toolCtx *ToolUseContext) (*PermissionResult, error) {
	return &PermissionResult{
		Behavior:     PermissionAllow,
		UpdatedInput: input,
	}, nil
}

// Description must be implemented by concrete tools.
func (t *BaseTool) Description(input map[string]interface{}, opts DescriptionOptions) (string, error) {
	return "", fmt.Errorf("Description not implemented for tool %s", t.name)
}

// Call must be implemented by concrete tools.
func (t *BaseTool) Call(ctx context.Context, args map[string]interface{}, toolCtx *ToolUseContext) (*ToolResult, error) {
	return nil, fmt.Errorf("Call not implemented for tool %s", t.name)
}

// BuildTool creates a Tool with default values merged with the provided definition.
// This is the Go equivalent of the TypeScript buildTool function.
func BuildTool(builder *ToolBuilder) Tool {
	return builder.Build()
}

// ToolDefaults provides default implementations for optional tool methods.
var ToolDefaults = struct {
	IsEnabled           func() bool
	IsConcurrencySafe   func(map[string]interface{}) bool
	IsReadOnly          func(map[string]interface{}) bool
	IsDestructive       func(map[string]interface{}) bool
	CheckPermissions    func(map[string]interface{}, *ToolUseContext) (*PermissionResult, error)
	ToAutoClassifierInput func(map[string]interface{}) interface{}
	UserFacingName      func(string) func(map[string]interface{}) string
}{
	IsEnabled: func() bool {
		return true
	},
	IsConcurrencySafe: func(input map[string]interface{}) bool {
		return false
	},
	IsReadOnly: func(input map[string]interface{}) bool {
		return false
	},
	IsDestructive: func(input map[string]interface{}) bool {
		return false
	},
	CheckPermissions: func(input map[string]interface{}, ctx *ToolUseContext) (*PermissionResult, error) {
		return &PermissionResult{
			Behavior:     PermissionAllow,
			UpdatedInput: input,
		}, nil
	},
	ToAutoClassifierInput: func(input map[string]interface{}) interface{} {
		return ""
	},
	UserFacingName: func(name string) func(map[string]interface{}) string {
		return func(input map[string]interface{}) string {
			return name
		}
	},
}
