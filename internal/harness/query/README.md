# Query Package

The `query` package implements the core query execution loop for Claude Code. It orchestrates the interaction between the AI model, tools, and various recovery mechanisms.

## Overview

The query package is the heart of Claude Code's execution engine. It manages:

- **State Machine**: Handles transitions between tool use, recovery, and completion states
- **Tool Execution**: Orchestrates tool calls and processes results
- **Error Recovery**: Implements recovery strategies for max_output_tokens and prompt_too_long errors
- **Auto-Compaction**: Automatically compacts message history when token limits are approached
- **Token Budget**: Tracks and enforces token budgets with auto-continuation
- **Stop Hooks**: Executes hooks at turn boundaries for custom logic

## Architecture

### Core Components

1. **query.go** - Main entry point and orchestration
2. **loop.go** - Core execution loop with state machine
3. **types.go** - Type definitions for all query-related structures
4. **state.go** - State management across iterations
5. **recovery.go** - Error recovery strategies
6. **compact.go** - Auto-compaction and reactive compaction
7. **budget.go** - Token budget tracking and enforcement
8. **streaming.go** - Event streaming and message generation
9. **hooks.go** - Stop hook execution

## Usage

### Basic Query Execution

```go
import (
    "context"
    "github.com/claude-code/internal/core/query"
    "github.com/claude-code/internal/types"
)

func executeQuery() {
    ctx := context.Background()
    
    params := &query.QueryParams{
        Messages:       []types.Message{},
        [REDACTED]:   types.[REDACTED]{},
        UserContext:    map[string]string{},
        SystemContext:  map[string]string{},
        CanUseTool:     func(toolName string) bool { return true },
        ToolUseContext: toolContext,
        QuerySource:    "repl_main_thread",
    }
    
    eventChan, terminalChan, err := query.Query(ctx, params)
    if err != nil {
        log.Fatal(err)
    }
    
    // Process events
    for event := range eventChan {
        switch e := event.(type) {
        case types.AssistantMessage:
            fmt.Println("Assistant:", e.Message.Content)
        case types.StreamEvent:
            fmt.Println("Stream event:", e.Type)
        }
    }
    
    // Get terminal result
    terminal := <-terminalChan
    fmt.Println("Query completed:", terminal.Reason)
}
```

### With Token Budget

```go
params := &query.QueryParams{
    Messages:      messages,
    [REDACTED]:  [REDACTED],
    // ... other params
    TaskBudget: &query.TaskBudget{
        Total: 500000, // 500k tokens
    },
}

eventChan, terminalChan, err := query.Query(ctx, params)
```

### With Max Turns Limit

```go
maxTurns := 10
params := &query.QueryParams{
    Messages:      messages,
    [REDACTED]:  [REDACTED],
    // ... other params
    MaxTurns: &maxTurns,
}
```

## State Machine

The query loop implements a state machine with the following transitions:

### Terminal States (Exit Loop)

- `completed` - Normal completion
- `blocking_limit` - Hit token blocking limit
- `image_error` - Image size/resize error
- `model_error` - Model API error
- `aborted_streaming` - User aborted during streaming
- `aborted_tools` - User aborted during tool execution
- `prompt_too_long` - Unrecoverable prompt too long error
- `stop_hook_prevented` - Stop hook prevented continuation
- `hook_stopped` - Hook stopped execution
- `max_turns` - Reached max turns limit

### Continue States (Next Iteration)

- `tool_use` - Model requested tool use
- `reactive_compact_retry` - Retrying after reactive compaction
- `max_output_tokens_recovery` - Recovering from max output tokens
- `max_output_tokens_escalate` - Escalating to larger model
- `collapse_drain_retry` - Retrying after context collapse
- `stop_hook_blocking` - Stop hook returned blocking errors
- `token_budget_continuation` - Auto-continuing due to token budget
- `queued_command` - Processing queued command
- `next_turn` - Normal next turn after tool execution

## Recovery Mechanisms

### Max Output Tokens Recovery

When the model hits `max_output_tokens`, the query loop attempts up to 3 recovery strategies:

1. **First attempt**: Increase `max_output_tokens` by 4096
2. **Second attempt**: Increase by another 4096
3. **Third attempt**: Escalate to a larger model (e.g., Sonnet → Opus)

```go
// Automatic recovery - no code needed
// The loop handles this internally
```

### Prompt Too Long Recovery

When the prompt exceeds token limits, the query loop attempts:

1. **Context collapse drain**: Cheap, keeps granular context
2. **Reactive compaction**: Full summary of message history

```go
// Automatic recovery - no code needed
// Enable reactive compaction via feature flag
```

## Auto-Compaction

Auto-compaction automatically summarizes message history when approaching token limits:

```go
// Auto-compaction is triggered based on:
// - Token count thresholds
// - Turn counter
// - Model context window size
// - Auto-compact enabled flag

// The loop handles this automatically
```

## Token Budget

The token budget feature allows auto-continuation when under 90% of budget:

```go
params := &query.QueryParams{
    // ... other params
    TaskBudget: &query.TaskBudget{
        Total: 500000, // 500k tokens
    },
}

// The loop will automatically continue if:
// - Under 90% of budget
// - Not showing diminishing returns
// - Not a subagent
```

### Diminishing Returns Detection

The budget tracker stops auto-continuation if:
- 3+ continuations have occurred
- Last two deltas were both < 500 tokens

## Stop Hooks

Stop hooks execute at turn boundaries for custom logic:

```go
// Stop hooks are configured externally
// The query loop executes them automatically

// Hook types:
// - Stop hooks: Execute after each turn
// - Task completed hooks: Execute when tasks complete
// - Teammate idle hooks: Execute when teammates are idle
```

## Testing

Run the comprehensive test suite:

```bash
go test ./internal/core/query -v
```

Run specific tests:

```bash
go test ./internal/core/query -run TestQuery_MaxOutputTokensRecovery
```

Run benchmarks:

```bash
go test ./internal/core/query -bench=.
```

## Thread Safety

The query package is designed for concurrent use:

- **State**: Each query has isolated state
- **Budget Tracker**: Uses mutex for thread-safe updates
- **Channels**: All event streaming uses Go channels

## Performance Considerations

1. **Channel Buffering**: Event channels are buffered (100) to prevent blocking
2. **Async Tool Summaries**: Tool use summaries are generated asynchronously
3. **Streaming Tool Execution**: Tools can execute during model streaming
4. **State Cloning**: State is cloned efficiently for recovery attempts

## Integration Points

### Tool Execution

```go
import "github.com/claude-code/internal/core/tool"

// Tools are executed via the tool package
// Results are streamed back through the event channel
```

### Compaction Service

```go
import "github.com/claude-code/internal/services/compact"

// Compaction is handled by the compact service
// The query loop triggers compaction based on token thresholds
```

### API Service

```go
import "github.com/claude-code/internal/services/api"

// API calls are made through the API service
// The query loop handles streaming and error recovery
```

## Error Handling

The query loop handles errors at multiple levels:

1. **Model Errors**: Caught and returned as terminal state
2. **Tool Errors**: Converted to tool_result messages
3. **Compaction Errors**: Tracked with consecutive failure counter
4. **Hook Errors**: Logged but don't stop execution

## Debugging

Enable debug logging:

```go
// Set environment variable
os.Setenv("CLAUDE_CODE_DEBUG", "true")

// Or use the debug flag
params.Deps = &query.QueryDeps{
    // ... custom deps for debugging
}
```

## Migration from TypeScript

Key differences from the TypeScript implementation:

1. **Channels vs AsyncGenerator**: Go uses channels instead of async generators
2. **Explicit Context**: Context is passed explicitly for cancellation
3. **Struct-based State**: State is a struct instead of multiple variables
4. **No Feature Flags**: Feature flags are checked at runtime, not compile-time
5. **Explicit Error Handling**: Errors are returned explicitly, not thrown

## Future Enhancements

Planned improvements:

- [ ] Implement context collapse drain
- [ ] Add media recovery for image/PDF errors
- [ ] Implement snip compaction
- [ ] Add task summary generation
- [ ] Implement job classification
- [ ] Add memory extraction
- [ ] Implement auto-dream
- [ ] Add prompt suggestions

## Contributing

When contributing to the query package:

1. Add tests for new features
2. Update this README
3. Ensure thread safety
4. Document state transitions
5. Add benchmarks for performance-critical code

## License

Copyright (c) Anthropic. All rights reserved.
