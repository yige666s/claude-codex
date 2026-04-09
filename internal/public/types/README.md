# internal/types

This package provides common types used across the Claude Code core packages.

## Overview

The `types` package contains shared type definitions for messages, state, configuration, permissions, tools, hooks, notifications, and other core concepts used throughout the Claude Code system.

## Key Components

### Messages (`message.go`)
- `Message`: Core message type supporting user, assistant, system, progress, and tool messages
- `ContentBlock`: Represents text, tool use, tool result, and thinking blocks
- Message constructors: `NewUserMessage`, `NewAssistantMessage`, `NewProgressMessage`

### State (`state.go`)
- `AppState`: Global application state with session information and conversation history
- `FileStateCache`: Tracks files read during the session
- `Usage`: Token and cost usage tracking
- `SDKStatus`: SDK session status

### Configuration (`config.go`)
- `Command`: Slash command definitions
- `ThinkingConfig`: Extended thinking configuration
- `MCPServerConnection`: MCP server connection settings
- `AgentDefinition`: Agent template definitions
- `SessionConfig`: Session-level configuration
- `GlobalConfig`: Global application configuration

### Permissions (`permissions.go`)
- `PermissionMode`: Permission handling modes (default, plan, bypass, auto)
- `PermissionLevel`: Permission levels (none, read, write, execute)
- `PermissionResult`: Result of permission checks
- `PermissionChecker`: Interface for permission checking

### Tools (`tools.go`)
- `Tool`: Interface for tool implementations
- `ToolDescriptor`: Tool capability descriptions
- `ToolCall`: Tool execution requests
- `ToolResult`: Tool execution results
- `ProgressAwareTool`: Interface for tools with progress reporting
- `ProgressReporter`: Progress reporting interface

### Hooks (`hooks.go`)
- `HookType`: Types of hooks (PreToolUse, PostToolUse, etc.)
- `HookProgress`: Progress information from hooks
- `PromptRequest`/`PromptResponse`: User prompt handling
- `HookResult`: Hook execution results

### Notifications (`notifications.go`)
- `Notification`: Notification messages with types and priorities
- `NotificationConfig`: Notification system configuration
- Support for multiple channels (Telegram, Discord, Slack, Email)

### Events (`events.go`)
- `Event`: System events with types and data
- `EventBus`: Event subscription and publishing interface
- Event types for session lifecycle, messages, tools, and errors

### Streaming (`streaming.go`)
- `StreamEvent`: Streaming API events
- `StreamHandler`: Interface for handling streaming events
- Support for message deltas, content blocks, and usage updates

### Errors (`errors.go`)
- `Error`: Structured errors with type, message, details, and suggestions
- Error types: validation, permission, not found, timeout, network, API, internal
- Helper functions for creating specific error types

### Utilities (`uuid.go`)
- `UUID()`: Generate random UUID v4
- `ShortID()`: Generate short random IDs
- `ValidateUUID()`: Validate UUID format

## Usage Examples

### Creating Messages

```go
import "github.com/ding/claude-code/claude-go/internal/types"

// Create a user message
msg := types.NewUserMessage("Hello, Claude!")

// Create an assistant message
response := types.NewAssistantMessage("Hello! How can I help you?")

// Create a progress message
progress := types.NewProgressMessage("Read", "running", 0.5)
```

### Managing State

```go
// Create application state
state := types.NewAppState(sessionID, workingDir)

// Add a message
state.AddMessage(msg)

// Update usage
state.UpdateUsage(types.Usage{
    InputTokens: 100,
    OutputTokens: 50,
})

// Set metadata
state.SetMetadata("key", "value")
```

### Permission Checking

```go
// Create permission result
result := types.AllowPermission("User approved")

// Check permission
if result.IsAllowed() {
    // Proceed with action
}

// Parse permission mode
mode, ok := types.ParsePermissionMode("bypass")
```

### Tool Execution

```go
// Create tool result
result := types.NewToolResult("Operation completed successfully")

// Add metadata
result.WithMetadata("duration_ms", 150)

// Create error result
errorResult := types.NewToolError("File not found")
```

### Error Handling

```go
// Create structured error
err := types.NewValidationError("Invalid input", "Field 'name' is required")

// Create permission error with suggestion
err := types.NewPermissionError(
    "Write access denied",
    "Run with --permission-mode bypass or approve the prompt",
)

// Add metadata
err.WithMetadata("field", "name")
```

## Thread Safety

Types with mutable state (like `AppState` and `FileStateCache`) use mutexes for thread-safe operations. Always use the provided methods rather than accessing fields directly.

## Design Principles

1. **Immutability**: Most types are designed to be immutable after creation
2. **Builder Pattern**: Many types support method chaining for configuration
3. **JSON Serialization**: All types include JSON tags for serialization
4. **Documentation**: All exported types and functions are documented
5. **Go Idioms**: Follows Go best practices and conventions
