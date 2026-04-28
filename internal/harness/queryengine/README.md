# QueryEngine Go Adapter

This package provides the Go-facing `QueryEngine` facade that matches the
TypeScript `src/QueryEngine.ts` role more closely than the old placeholder
implementation.

The actual query lifecycle now lives in `internal/harness/query`. This package
is the SDK-shaped adapter layer on top of that runtime.

## Overview

The QueryEngine facade handles:
- Message submission and streaming responses
- Session state management across multiple turns
- Stable session identity
- SDK-style result messages
- Conversion between adapter-facing messages and the shared query runtime

## Architecture

### Core Components

#### 1. **QueryEngine** (`engine.go`)
The main adapter that delegates to `internal/harness/query.QueryEngine`.

```go
engine := engine.NewQueryEngine(engine.QueryEngineConfig{
    Cwd:   "/path/to/project",
    Tools: tools,
    CanUseTool: permissionChecker,
    GetAppState: getState,
    SetAppState: setState,
})

// Submit a message and get streaming responses
ctx := context.Background()
ch, err := engine.SubmitMessage(ctx, "Hello, Claude!", nil)
for msg := range ch {
    // Handle SDKMessage
}
```

**Key Features:**
- Delegates turn execution to the shared query runtime
- Emits SDK-shaped streamed messages plus a terminal `result`
- Preserves a stable session ID and adapter-facing config

#### 2. **SessionState** (`session.go`)
Manages mutable state for a conversation session.

```go
state := engine.NewSessionState(initialMessages, readFileCache)

// Add messages
state.AddMessage(message)

// Track usage
state.UpdateUsage(usage)

// Record permission denials
state.AddPermissionDenial(denial)

// Skill discovery tracking
state.MarkSkillDiscovered("skill-name")

// Snapshot for persistence
snapshot := state.Snapshot()
json, _ := snapshot.ToJSON()
```

**Key Features:**
- Thread-safe with RWMutex
- Message filtering and querying
- Turn counting
- Skill discovery tracking
- Nested memory path tracking
- Serializable snapshots

#### 3. **Shared Runtime** (`../query`)
The concrete query lifecycle is implemented in `internal/harness/query`.
`queryengine` intentionally no longer carries a second placeholder execution
path.

#### 4. **SnipCompactor** (`snip.go`)
Manages history snipping and replay for memory efficiency.

```go
config := engine.DefaultSnipConfig()
config.MaxMessages = 100
config.PreserveRecentCount = 20

compactor := engine.NewSnipCompactor(config)
result, err := compactor.Snip(messages)

if result.Executed {
    fmt.Printf("Removed %d messages\n", result.RemovedCount)
    messages = result.Messages
}
```

**Key Features:**
- Configurable thresholds
- System message preservation
- Boundary message insertion
- Snip projection utilities
- Session merging

## Design Note

This package used to contain a second, incomplete engine implementation with
`Not yet implemented` placeholders. That was structurally divergent from the
TypeScript source, which has one `QueryEngine` plus helper modules under
`src/query/*`.

The current design is:

- `internal/harness/queryengine` = TS-aligned facade
- `internal/harness/query` = shared query lifecycle/runtime
- `internal/harness/engine` = thin UI-facing adapter that now defaults to the
  `queryengine -> query` runtime path

## Types

### QueryEngineConfig

Configuration for the QueryEngine:

```go
type QueryEngineConfig struct {
    Cwd                    string
    Tools                  []tool.Tool
    Commands               []interface{}
    MCPClients             []interface{}
    Agents                 []interface{}
    CanUseTool             CanUseToolFunc
    GetAppState            func() interface{}
    SetAppState            func(func(interface{}) interface{})
    InitialMessages        []Message
    ReadFileCache          interface{}
    CustomSystemPrompt     string
    AppendSystemPrompt     string
    UserSpecifiedModel     string
    FallbackModel          string
    ThinkingConfig         interface{}
    MaxTurns               int
    MaxBudgetUSD           *float64
    TaskBudget             *TaskBudget
    JSONSchema             map[string]interface{}
    Verbose                bool
    ReplayUserMessages     bool
    IncludePartialMessages bool
    HandleElicitation      func(params interface{}, signal context.Context) (interface{}, error)
    SetSDKStatus           func(status SDKStatus)
    AbortController        context.CancelFunc
    OrphanedPermission     *OrphanedPermission
    SnipReplay             SnipReplayFunc
}
```

### Message

Represents a conversation message:

```go
type Message struct {
    Type            string
    UUID            string
    Timestamp       time.Time
    Message         interface{}
    Content         interface{}
    Subtype         string
    IsMeta          bool
    Data            interface{}
    Event           interface{}
    ToolUseID       string
    Attachment      interface{}
    CompactMetadata *CompactMetadata
    // ... additional fields
}
```

### SDKMessage

SDK protocol message for streaming:

```go
type SDKMessage struct {
    Type              string
    Subtype           string
    Message           interface{}
    SessionID         string
    ParentToolUseID   *string
    UUID              string
    Timestamp         *time.Time
    IsReplay          bool
    IsSynthetic       bool
    Event             interface{}
    DurationMS        int64
    DurationAPIMS     int64
    IsError           bool
    NumTurns          int
    Result            string
    StopReason        string
    TotalCostUSD      float64
    Usage             *Usage
    ModelUsage        map[string]*Usage
    PermissionDenials []PermissionDenial
    StructuredOutput  interface{}
    FastModeState     interface{}
    Errors            []string
    CompactMetadata   map[string]interface{}
}
```

### Usage

Token and cost tracking:

```go
type Usage struct {
    InputTokens              int
    OutputTokens             int
    CacheCreationInputTokens int
    CacheReadInputTokens     int
}
```

## Usage Examples

### Basic Query

```go
config := engine.QueryEngineConfig{
    Cwd:   "/project",
    Tools: tools,
    CanUseTool: func(tool tool.Tool, input map[string]interface{}, 
                     toolCtx *tool.ToolUseContext, assistantMessage interface{}, 
                     toolUseID string, forceDecision bool) (*engine.PermissionResult, error) {
        return &engine.PermissionResult{Behavior: "allow"}, nil
    },
    GetAppState: func() interface{} { return appState },
    SetAppState: func(f func(interface{}) interface{}) { 
        appState = f(appState) 
    },
}

qe := engine.NewQueryEngine(config)
ctx := context.Background()

ch, err := qe.SubmitMessage(ctx, "Write a hello world program", nil)
if err != nil {
    log.Fatal(err)
}

for msg := range ch {
    switch msg.Type {
    case "result":
        if msg.IsError {
            fmt.Printf("Error: %v\n", msg.Errors)
        } else {
            fmt.Printf("Result: %s\n", msg.Result)
        }
    case "stream_event":
        // Handle streaming events
    }
}
```

### Multi-Turn Conversation

```go
qe := engine.NewQueryEngine(config)
ctx := context.Background()

// First turn
ch1, _ := qe.SubmitMessage(ctx, "Create a file called test.txt", nil)
for msg := range ch1 {
    // Handle responses
}

// Second turn (state persists)
ch2, _ := qe.SubmitMessage(ctx, "Now add some content to it", nil)
for msg := range ch2 {
    // Handle responses
}

// Get conversation history
messages := qe.GetMessages()
fmt.Printf("Total messages: %d\n", len(messages))

// Get usage statistics
usage := qe.GetTotalUsage()
fmt.Printf("Total tokens: %d\n", usage.InputTokens + usage.OutputTokens)
```

### With History Snipping

```go
config := engine.QueryEngineConfig{
    // ... other config
    SnipReplay: func(yieldedSystemMsg engine.Message, store []engine.Message) *engine.SnipReplayResult {
        if !engine.IsSnipBoundaryMessage(yieldedSystemMsg) {
            return nil
        }
        
        snipConfig := engine.DefaultSnipConfig()
        snipConfig.Force = true
        
        result, _ := engine.SnipCompactIfNeeded(store, snipConfig)
        return &engine.SnipReplayResult{
            Messages: result.Messages,
            Executed: result.Executed,
        }
    },
}

qe := engine.NewQueryEngine(config)
// Messages will be automatically snipped when threshold is reached
```

### Session Management

```go
manager := engine.NewSessionManager()

// Create sessions
session1 := manager.CreateSession(nil, nil)
session2 := manager.CreateSession(initialMessages, cache)

// Retrieve session
session, err := manager.GetSession(session1.GetSessionID())

// List all sessions
sessionIDs := manager.ListSessions()

// Cleanup inactive sessions
ctx := context.Background()
removed := manager.CleanupInactiveSessions(ctx, 24*time.Hour)
fmt.Printf("Removed %d inactive sessions\n", removed)
```

### Permission Tracking

```go
config := engine.QueryEngineConfig{
    CanUseTool: func(tool tool.Tool, input map[string]interface{}, 
                     toolCtx *tool.ToolUseContext, assistantMessage interface{}, 
                     toolUseID string, forceDecision bool) (*engine.PermissionResult, error) {
        // Check if tool is allowed
        if tool.Name() == "bash" {
            return &engine.PermissionResult{
                Behavior: "deny",
                Reason:   "Bash execution not allowed",
            }, nil
        }
        return &engine.PermissionResult{Behavior: "allow"}, nil
    },
    // ... other config
}

qe := engine.NewQueryEngine(config)
// ... submit messages

// Get all permission denials
denials := qe.GetPermissionDenials()
for _, denial := range denials {
    fmt.Printf("Denied: %s (ID: %s)\n", denial.ToolName, denial.ToolUseID)
}
```

## Thread Safety

All public methods are thread-safe:

- `QueryEngine` uses `sync.RWMutex` for state protection
- `SessionState` uses `sync.RWMutex` for concurrent access
- `SessionManager` uses `sync.RWMutex` for session map access
- Channel-based streaming is inherently thread-safe

## Testing

Run tests:

```bash
cd internal/core/engine
go test -v
```

Run with race detector:

```bash
go test -race -v
```

Run benchmarks:

```bash
go test -bench=. -benchmem
```

## Integration with Tool Package

The QueryEngine integrates with `internal/core/tool`:

```go
import "claude-codex/internal/core/tool"

// Tools are passed in config
config := engine.QueryEngineConfig{
    Tools: []tool.Tool{
        readTool,
        writeTool,
        bashTool,
    },
    // ...
}

// Permission checking uses tool.Tool interface
CanUseTool: func(t tool.Tool, input map[string]interface{}, 
                 toolCtx *tool.ToolUseContext, ...) (*engine.PermissionResult, error) {
    // Check permissions based on tool properties
    if t.IsDestructive(input) {
        // Ask user for confirmation
    }
    return &engine.PermissionResult{Behavior: "allow"}, nil
}
```

## Differences from TypeScript Implementation

### Async Patterns
- **TypeScript**: Uses `AsyncGenerator` for streaming
- **Go**: Uses channels (`<-chan SDKMessage`)

### Cancellation
- **TypeScript**: Uses `AbortController`
- **Go**: Uses `context.Context`

### Thread Safety
- **TypeScript**: Single-threaded (no explicit locking needed)
- **Go**: Explicit mutex protection for shared state

### Error Handling
- **TypeScript**: Throws exceptions
- **Go**: Returns errors explicitly

### Type System
- **TypeScript**: Structural typing with interfaces
- **Go**: Nominal typing with explicit interface implementation

## Future Enhancements

- [ ] Complete `submitMessageAsync` implementation
- [ ] Integrate with actual query loop (query.go)
- [ ] Add cost calculation utilities
- [ ] Implement structured output validation
- [ ] Add metrics and observability hooks
- [ ] Support for streaming tool progress
- [ ] Enhanced error recovery
- [ ] Session persistence to disk
- [ ] Distributed session management

## Related Packages

- `internal/core/tool` - Tool interface and execution
- `internal/state` - Session persistence
- `internal/permissions` - Permission checking
- `pkg/anthropic` - Claude API client

## License

See LICENSE file in repository root.
