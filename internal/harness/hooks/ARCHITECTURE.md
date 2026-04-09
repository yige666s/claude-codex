# Hook System Architecture for Go

## Overview

The Hook system allows users to execute custom commands at various lifecycle points in Claude Code. This document outlines a simplified, modular Go implementation.

## Design Principles

1. **Simplicity**: Avoid the 17K line complexity of TypeScript version
2. **Modularity**: Clear separation of concerns
3. **Extensibility**: Easy to add new hook types
4. **Type Safety**: Leverage Go's type system
5. **Performance**: Efficient execution with proper concurrency control

## Architecture

### Core Components

```
internal/harness/hooks/
├── types.go           # Core types and interfaces
├── registry.go        # Hook registration and management
├── executor.go        # Hook execution engine
├── events.go          # Hook event definitions
├── context.go         # Hook execution context
├── result.go          # Hook result aggregation
├── async.go           # Async hook handling
└── builtin/           # Built-in hooks
    ├── pretooluse.go
    ├── posttooluse.go
    ├── sessionstart.go
    └── ...
```

### Key Types

#### HookEvent
Defines when a hook can be triggered:
- `PreToolUse` - Before tool execution
- `PostToolUse` - After tool execution
- `PostToolUseFailure` - After tool execution failure
- `SessionStart` - At session start
- `SessionEnd` - At session end
- `UserPromptSubmit` - When user submits prompt
- `PermissionRequest` - When permission is requested
- `PermissionDenied` - When permission is denied
- `Notification` - For notifications
- `Stop` - When stopping
- `StopFailure` - When stop fails

#### Hook Interface
```go
type Hook interface {
    Name() string
    Event() HookEvent
    Execute(ctx context.Context, input HookInput) (*HookResult, error)
    IsAsync() bool
    Timeout() time.Duration
}
```

#### HookInput
Context passed to hooks:
```go
type HookInput struct {
    Event       HookEvent
    SessionID   string
    WorkingDir  string
    Tool        *ToolInfo        // For tool-related hooks
    Message     *Message         // For message-related hooks
    Permission  *PermissionInfo  // For permission-related hooks
    Metadata    map[string]any
}
```

#### HookResult
Result from hook execution:
```go
type HookResult struct {
    Continue            bool
    SuppressOutput      bool
    StopReason          string
    SystemMessage       string
    AdditionalContext   string
    PermissionDecision  *PermissionDecision
    UpdatedInput        map[string]any
    BlockingError       string
}
```

### Hook Registry

Manages hook registration and lookup:

```go
type Registry struct {
    hooks map[HookEvent][]Hook
    mu    sync.RWMutex
}

func (r *Registry) Register(hook Hook) error
func (r *Registry) Unregister(name string, event HookEvent) error
func (r *Registry) GetHooks(event HookEvent) []Hook
func (r *Registry) Clear()
```

### Hook Executor

Executes hooks with proper error handling and timeout:

```go
type Executor struct {
    registry *Registry
    timeout  time.Duration
}

func (e *Executor) Execute(ctx context.Context, event HookEvent, input HookInput) (*AggregatedResult, error)
func (e *Executor) executeHook(ctx context.Context, hook Hook, input HookInput) (*HookResult, error)
func (e *Executor) aggregateResults(results []*HookResult) *AggregatedResult
```

### Async Hook Handling

For hooks that run in background:

```go
type AsyncHookRegistry struct {
    pending map[string]*AsyncHook
    mu      sync.RWMutex
}

type AsyncHook struct {
    ID        string
    Hook      Hook
    Input     HookInput
    StartedAt time.Time
    Done      chan *HookResult
}
```

## Execution Flow

1. **Registration Phase**
   - Hooks register themselves with the Registry
   - Each hook specifies its event type and configuration

2. **Trigger Phase**
   - Event occurs (e.g., PreToolUse)
   - Executor retrieves all hooks for that event
   - Hooks are executed in registration order

3. **Execution Phase**
   - For sync hooks: Execute sequentially with timeout
   - For async hooks: Start goroutine and track in AsyncHookRegistry
   - Collect results from each hook

4. **Aggregation Phase**
   - Combine results from all hooks
   - Apply precedence rules (e.g., any "deny" blocks execution)
   - Return aggregated result to caller

## Hook Types

### Sync Hooks
Execute immediately and block until complete:
- PreToolUse (permission checking)
- PermissionRequest (permission decisions)
- UserPromptSubmit (input validation)

### Async Hooks
Execute in background without blocking:
- PostToolUse (logging, analytics)
- SessionEnd (cleanup)
- Notification (user notifications)

## Configuration

Hooks can be configured via:
1. Code (built-in hooks)
2. Configuration files (user hooks)
3. Environment variables (feature flags)

```go
type HookConfig struct {
    Name     string
    Event    HookEvent
    Command  string        // Shell command to execute
    Timeout  time.Duration
    Async    bool
    Enabled  bool
    Matcher  string        // Optional regex matcher
}
```

## Built-in Hooks

### PreToolUse Hook
Validates and potentially modifies tool input before execution:
```go
type PreToolUseHook struct {
    validator ToolValidator
}

func (h *PreToolUseHook) Execute(ctx context.Context, input HookInput) (*HookResult, error) {
    // Validate tool input
    // Check permissions
    // Optionally modify input
    return &HookResult{
        Continue: true,
        PermissionDecision: &PermissionDecision{
            Behavior: "allow",
        },
    }, nil
}
```

### PostToolUse Hook
Processes tool output after execution:
```go
type PostToolUseHook struct {
    logger Logger
}

func (h *PostToolUseHook) Execute(ctx context.Context, input HookInput) (*HookResult, error) {
    // Log tool execution
    // Collect metrics
    // Optionally modify output
    return &HookResult{
        Continue: true,
        AdditionalContext: "Tool executed successfully",
    }, nil
}
```

## Error Handling

1. **Hook Execution Errors**
   - Non-blocking errors: Log and continue
   - Blocking errors: Stop execution and return error

2. **Timeout Handling**
   - Each hook has a timeout (default: 30s)
   - Timeout triggers cancellation
   - Async hooks can have longer timeouts

3. **Panic Recovery**
   - Recover from panics in hook execution
   - Log panic and treat as non-blocking error

## Testing Strategy

1. **Unit Tests**
   - Test individual hook implementations
   - Test registry operations
   - Test executor logic

2. **Integration Tests**
   - Test hook execution flow
   - Test async hook handling
   - Test result aggregation

3. **Mock Hooks**
   - Create mock hooks for testing
   - Simulate various scenarios (success, failure, timeout)

## Performance Considerations

1. **Concurrency**
   - Async hooks run in separate goroutines
   - Use sync.WaitGroup for coordination
   - Limit concurrent async hooks (max 10)

2. **Memory**
   - Clean up completed async hooks
   - Limit hook result size
   - Use object pools for frequent allocations

3. **Latency**
   - Minimize sync hook execution time
   - Use timeouts to prevent hanging
   - Cache hook lookups

## Migration from TypeScript

Key differences from TypeScript implementation:

1. **Simplified Event Model**
   - Fewer event types initially
   - Can add more as needed

2. **No Shell Command Execution (Initially)**
   - Focus on Go-native hooks first
   - Can add shell command support later

3. **Cleaner Async Model**
   - Use Go's goroutines and channels
   - Simpler than Promise-based approach

4. **Type Safety**
   - Leverage Go's type system
   - Compile-time checks instead of runtime validation

## Implementation Phases

### Phase 1: Core Framework (Week 7)
- [ ] Define core types and interfaces
- [ ] Implement Registry
- [ ] Implement Executor
- [ ] Add basic error handling

### Phase 2: Built-in Hooks (Week 7)
- [ ] PreToolUse hook
- [ ] PostToolUse hook
- [ ] SessionStart hook
- [ ] PermissionRequest hook

### Phase 3: Async Support (Week 7)
- [ ] Async hook registry
- [ ] Background execution
- [ ] Result tracking

### Phase 4: Testing (Week 7)
- [ ] Unit tests for all components
- [ ] Integration tests
- [ ] Performance benchmarks

## Future Enhancements

1. **Shell Command Hooks**
   - Execute user-defined shell commands
   - Parse JSON output
   - Handle stdin/stdout

2. **Hook Chaining**
   - Allow hooks to trigger other hooks
   - Build complex workflows

3. **Conditional Execution**
   - Execute hooks based on conditions
   - Pattern matching on input

4. **Hook Marketplace**
   - Share hooks with community
   - Install hooks from registry
