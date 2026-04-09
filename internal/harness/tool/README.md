# Tool Package

The `tool` package provides core types and interfaces for tool execution in Claude Code. It defines the Tool interface, validation types, and related structures for managing tool calls, permissions, and execution context.

## Overview

This package is a Go implementation of the TypeScript `Tool.ts` module from Claude Code. It maintains all the functionality while following Go idioms and best practices.

## Package Structure

- **types.go** - Core tool types, interfaces, and validation
- **context.go** - Tool execution context and state management
- **permissions.go** - Permission context and rules
- **builder.go** - Tool builder pattern and default implementations
- **tool_test.go** - Tests for types and builder
- **context_test.go** - Tests for context management
- **permissions_test.go** - Tests for permissions

## Key Types

### Tool Interface

The `Tool` interface defines the contract that all tools must implement:

```go
type Tool interface {
    Name() string
    Aliases() []string
    Description(input map[string]interface{}, opts DescriptionOptions) (string, error)
    Call(ctx context.Context, args map[string]interface{}, toolCtx *ToolUseContext) (*ToolResult, error)
    ValidateInput(input map[string]interface{}, toolCtx *ToolUseContext) (ValidationResult, error)
    CheckPermissions(input map[string]interface{}, toolCtx *ToolUseContext) (*PermissionResult, error)
    // ... and many more methods
}
```

### ToolUseContext

`ToolUseContext` contains all the context needed for tool execution:

```go
type ToolUseContext struct {
    Ctx               context.Context
    Options           ToolOptions
    State             *ToolState
    Callbacks         *ToolCallbacks
    PermissionContext *ToolPermissionContext
    // ... and more fields
}
```

### ToolPermissionContext

`ToolPermissionContext` manages permission-related context:

```go
type ToolPermissionContext struct {
    Mode                         PermissionMode
    AdditionalWorkingDirectories map[string]AdditionalWorkingDirectory
    AlwaysAllowRules            ToolPermissionRulesBySource
    AlwaysDenyRules             ToolPermissionRulesBySource
    AlwaysAskRules              ToolPermissionRulesBySource
    // ... and more fields
}
```

## Usage Examples

### Creating a Tool

Use the `ToolBuilder` to create tools with sensible defaults:

```go
tool := NewToolBuilder("my-tool").
    WithAliases("alias1", "alias2").
    WithSearchHint("performs specific operations").
    WithMaxResultSizeChars(50000).
    WithMCP("server-name", "tool-name").
    Build()
```

### Creating a Tool Use Context

```go
ctx := context.Background()
toolCtx := NewToolUseContext(ctx)

// Configure options
toolCtx.Options.Debug = true
toolCtx.Options.Verbose = true
toolCtx.Options.MainLoopModel = "claude-3-opus"

// Set permission context
permCtx := NewToolPermissionContext()
permCtx.SetMode(PermissionModeAuto)
toolCtx.PermissionContext = permCtx
```

### Implementing a Custom Tool

Embed `BaseTool` to get default implementations:

```go
type MyTool struct {
    *BaseTool
}

func NewMyTool() *MyTool {
    return &MyTool{
        BaseTool: NewToolBuilder("my-tool").
            WithSearchHint("does something useful").
            Build(),
    }
}

func (t *MyTool) Description(input map[string]interface{}, opts DescriptionOptions) (string, error) {
    return "My tool does something useful", nil
}

func (t *MyTool) Call(ctx context.Context, args map[string]interface{}, toolCtx *ToolUseContext) (*ToolResult, error) {
    // Implement tool logic here
    return &ToolResult{
        Data: "result data",
    }, nil
}
```

### Validation

```go
// Validate input
result, err := tool.ValidateInput(input, toolCtx)
if err != nil {
    return err
}

if !result.Valid {
    return fmt.Errorf("validation failed: %s (code: %d)", result.Message, result.ErrorCode)
}
```

### Permission Checking

```go
// Check permissions
permResult, err := tool.CheckPermissions(input, toolCtx)
if err != nil {
    return err
}

switch permResult.Behavior {
case PermissionAllow:
    // Proceed with execution
case PermissionDeny:
    return fmt.Errorf("permission denied: %s", permResult.Reason)
case PermissionAsk:
    // Prompt user for permission
}
```

### Finding Tools

```go
tools := []Tool{tool1, tool2, tool3}

// Find by name or alias
tool := FindToolByName(tools, "my-tool")
if tool == nil {
    return fmt.Errorf("tool not found")
}

// Check if tool matches name
if ToolMatchesName(tool, "alias1") {
    // Tool matches
}
```

## Thread Safety

The package provides thread-safe operations where needed:

- `ToolState` uses mutexes for concurrent access to in-progress tool IDs and response length
- `ToolPermissionContext` uses mutexes for concurrent access to permission settings
- `ToolUseContext` uses `sync.Map` for concurrent access to tool decisions and tracking data

## Permission Modes

The package supports four permission modes:

- **default** - Standard permission checking
- **auto** - Automated permission decisions
- **bypass** - Bypass permission checks
- **plan** - Plan mode with special permission handling

## Validation Results

Validation results indicate whether input is valid:

```go
// Success
result := NewValidationSuccess()

// Error
result := NewValidationError("Invalid input", 400)
```

## Permission Results

Permission results indicate how to handle a tool call:

```go
result := &PermissionResult{
    Behavior:     PermissionAllow,
    UpdatedInput: modifiedInput,
    Reason:       "optional reason",
}
```

## Tool Result

Tool execution returns a `ToolResult`:

```go
result := &ToolResult{
    Data:        "result data",
    NewMessages: []interface{}{...},
    ContextModifier: func(ctx *ToolUseContext) {
        // Modify context after execution
    },
    MCPMeta: &MCPMeta{
        Meta: map[string]interface{}{"key": "value"},
    },
}
```

## Testing

The package includes comprehensive tests:

```bash
go test ./internal/core/tool/...
```

Run with coverage:

```bash
go test -cover ./internal/core/tool/...
```

Run with race detection:

```bash
go test -race ./internal/core/tool/...
```

## Design Decisions

### Go Idioms

- Uses interfaces instead of TypeScript's structural typing
- Uses `context.Context` for cancellation and timeouts
- Uses `sync.Map` for concurrent access to shared data
- Uses `sync.RWMutex` for read-heavy concurrent access patterns
- Uses channels where appropriate (though not heavily in this package)

### Type Safety

- Uses `map[string]interface{}` for flexible input/output (similar to TypeScript's `Record<string, unknown>`)
- Provides type-safe accessors for common fields
- Uses pointer types for optional fields

### Error Handling

- Returns errors instead of throwing exceptions
- Provides detailed error messages
- Uses `ValidationResult` for validation-specific errors

### Immutability

- `ToolPermissionContext` provides thread-safe access with mutexes
- Clone methods create deep copies where needed
- Context methods return new instances rather than mutating

## Future Enhancements

Potential improvements for future versions:

1. **Generic Type Support** - Use Go generics for type-safe tool input/output
2. **Schema Validation** - Integrate with JSON Schema validation libraries
3. **Middleware Pattern** - Add middleware support for tool execution pipeline
4. **Plugin System** - Support dynamic tool loading
5. **Metrics** - Add built-in metrics collection for tool execution

## Related Packages

This package is part of the Claude Code Go implementation:

- `internal/core/query` - Query engine (to be implemented)
- `internal/core/message` - Message types (to be implemented)
- `internal/core/permissions` - Permission system (to be implemented)

## License

This package is part of Claude Code and follows the same license.
