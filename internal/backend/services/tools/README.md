# Tools Service

The tools service provides tool orchestration, execution, and lifecycle management for the Claude Code Go implementation.

## Features

### 1. Tool Orchestration
- **Concurrent execution**: Parallel execution of concurrent-safe tools
- **Serial execution**: Sequential execution of non-concurrent tools
- **Batching strategy**: Automatic partitioning based on concurrency safety
- **Concurrency control**: Configurable max concurrent executions

### 2. Streaming Execution
- **Queue management**: Tool execution queue with status tracking
- **Result buffering**: In-order result yielding
- **Abort control**: Cancellation of pending and executing tools
- **Status tracking**: Real-time execution status monitoring

### 3. Hook System
- **Pre-tool hooks**: Permission checking before execution
- **Post-tool hooks**: Context modification after success
- **Failure hooks**: Error handling and recovery
- **Context injection**: Additional context from hooks

### 4. Execution Lifecycle
- Input validation with schemas
- Permission checking
- Pre-execution hooks
- Tool execution
- Post-execution hooks
- Error classification
- Result formatting

## Core Components

### types.go
Defines core types and interfaces:
- `ToolCall` - Tool invocation request
- `ToolResult` - Tool execution result
- `ToolExecutionContext` - Execution context
- `ToolExecutor` - Tool executor interface
- `ToolHook` - Hook interface
- `ToolRegistry` - Tool registration and management
- `AbortController` - Cancellation control

### executor.go
Tool execution implementation:
- `ExecuteTool()` - Execute single tool with full lifecycle
- `RunTools()` - Execute multiple tools with orchestration
- `partitionToolCalls()` - Partition tools by concurrency safety
- `runToolsConcurrently()` - Parallel execution with semaphore

### streaming.go
Streaming execution manager:
- `StreamingToolExecutor` - Queue-based streaming executor
- `AddTool()` - Add tool to execution queue
- `processQueue()` - Start tools when concurrency allows
- `executeTool()` - Execute single tool
- `GetRemainingResults()` - Yield results in order
- `WaitForCompletion()` - Wait for all tools to complete
- `Abort()` - Cancel all pending/executing tools

## Configuration

### Environment Variables

#### Concurrency Control
- `CLAUDE_CODE_MAX_TOOL_USE_CONCURRENCY` - Max concurrent tool executions (default: 10)

### Execution Options

```go
type ToolExecutionOptions struct {
    MaxConcurrency   int           // Max concurrent executions
    Timeout          time.Duration // Execution timeout
    EnableHooks      bool          // Enable hook system
    EnableAnalytics  bool          // Enable analytics logging
    EnableTelemetry  bool          // Enable telemetry
    ProgressCallback func(string)  // Progress message callback
}
```

## Usage Examples

### Register Tools

```go
import "github.com/ding/claude-code/claude-go/internal/services/tools"

// Create registry
registry := tools.NewToolRegistry()

// Register tool executor
registry.Register("bash", &BashToolExecutor{})
registry.Register("read", &ReadToolExecutor{})
registry.Register("write", &WriteToolExecutor{})
```

### Execute Single Tool

```go
ctx := &tools.ToolExecutionContext{
    Context:         context.Background(),
    AbortController: tools.NewAbortController(context.Background()),
    SessionID:       "session-123",
    QuerySource:     "repl_main_thread",
}

call := &tools.ToolCall{
    ID:    "tool-1",
    Name:  "bash",
    Input: map[string]interface{}{
        "command": "ls -la",
    },
}

opts := tools.DefaultToolExecutionOptions()
result, err := tools.ExecuteTool(ctx, call, registry, opts)
```

### Execute Multiple Tools

```go
calls := []*tools.ToolCall{
    {ID: "1", Name: "read", Input: map[string]interface{}{"file_path": "file1.go"}},
    {ID: "2", Name: "read", Input: map[string]interface{}{"file_path": "file2.go"}},
    {ID: "3", Name: "bash", Input: map[string]interface{}{"command": "go test"}},
}

results, err := tools.RunTools(ctx, calls, registry, execCtx, opts)
```

### Streaming Execution

```go
executor := tools.NewStreamingToolExecutor(ctx, registry, opts)

// Add tools dynamically
executor.AddTool(&tools.ToolCall{ID: "1", Name: "read", Input: input1})
executor.AddTool(&tools.ToolCall{ID: "2", Name: "write", Input: input2})

// Wait for completion
results, err := executor.WaitForCompletion(context.Background())

// Or get results incrementally
for {
    results, err := executor.GetRemainingResults()
    if len(results) > 0 {
        // Process results
    }
    if allDone {
        break
    }
}
```

### Register Hooks

```go
type MyHook struct{}

func (h *MyHook) PreToolUse(ctx *tools.ToolHookContext) (*tools.ToolHookResult, error) {
    // Check permissions
    if !hasPermission(ctx.ToolName) {
        return &tools.ToolHookResult{
            Decision: tools.PermissionDeny,
        }, nil
    }
    return nil, nil
}

func (h *MyHook) PostToolUse(ctx *tools.ToolHookContext) (*tools.ToolHookResult, error) {
    // Add context after success
    return &tools.ToolHookResult{
        AdditionalContext: []string{"Tool executed successfully"},
    }, nil
}

func (h *MyHook) PostToolUseFailure(ctx *tools.ToolHookContext) (*tools.ToolHookResult, error) {
    // Handle failure
    return nil, nil
}

registry.AddHook(&MyHook{})
```

### Implement Tool Executor

```go
type MyToolExecutor struct{}

func (e *MyToolExecutor) Execute(ctx *tools.ToolExecutionContext, call *tools.ToolCall) (*tools.ToolResult, error) {
    // Execute tool logic
    result := performWork(call.Input)
    
    return &tools.ToolResult{
        ToolUseID: call.ID,
        Content:   result,
        IsError:   false,
    }, nil
}

func (e *MyToolExecutor) IsConcurrentSafe() bool {
    return true // Can run in parallel with other tools
}

func (e *MyToolExecutor) ValidateInput(input map[string]interface{}) error {
    // Validate required fields
    if _, ok := input["required_field"]; !ok {
        return errors.New("missing required_field")
    }
    return nil
}
```

## Constants

### Tool Status
- `ToolStatusQueued` - Tool is queued for execution
- `ToolStatusExecuting` - Tool is currently executing
- `ToolStatusCompleted` - Tool completed successfully
- `ToolStatusYielded` - Tool result has been yielded
- `ToolStatusFailed` - Tool execution failed
- `ToolStatusAborted` - Tool was aborted

### Permission Decisions
- `PermissionAllow` - Allow tool execution
- `PermissionDeny` - Deny tool execution
- `PermissionAsk` - Ask user for permission

### Defaults
- `MaxToolUseConcurrency`: 10
- `DefaultToolTimeout`: 5 minutes

## Tool Partitioning Strategy

The orchestrator automatically partitions tool calls into batches:

1. **Concurrent-safe tools** are grouped together and executed in parallel
2. **Non-concurrent tools** are executed alone in serial
3. Batches are executed sequentially
4. Within a batch, tools run concurrently up to `MaxConcurrency`

Example:
```
Input: [Read, Read, Bash, Read, Write]
       [safe, safe, unsafe, safe, unsafe]

Batches: [[Read, Read], [Bash], [Read], [Write]]
         [concurrent]   [serial] [serial] [serial]
```

## Hook Execution Order

1. **Pre-tool hooks** (all hooks, in registration order)
   - Permission checking
   - Context injection
   - Blocking errors

2. **Tool execution**

3. **Post-tool hooks** (success or failure)
   - Context modification
   - Analytics logging
   - Error recovery

## Error Handling

Tool execution errors are classified and handled appropriately:
- Validation errors stop execution immediately
- Permission denied errors return without execution
- Execution errors trigger failure hooks
- Hook errors are logged but don't fail execution

## Testing

Run tests:
```bash
go test ./internal/services/tools/...
```

Run with coverage:
```bash
go test -cover ./internal/services/tools/...
```

## Integration

The tools service integrates with:
- Tool implementations (Bash, Read, Write, Edit, etc.)
- Hook system for permission checking
- Analytics service for logging
- Telemetry for tracing
- MCP servers for external tools

## Performance Characteristics

- **Concurrent execution**: Up to 10 tools in parallel (configurable)
- **Queue processing**: 100ms polling interval
- **Default timeout**: 5 minutes per tool
- **Hook overhead**: Minimal, hooks run synchronously
- **Result buffering**: In-order yielding with status tracking

## Next Steps

After tools service completion, the next service to refactor is: **analytics service**
