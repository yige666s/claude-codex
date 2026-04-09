# Core Files Refactoring Plan

## Overview

Refactor the three highest-priority core TypeScript files to Go:
1. **Tool.ts** (794 lines) - Tool type definitions and interfaces
2. **QueryEngine.ts** (1297 lines) - Query lifecycle and session management
3. **query.ts** (1730 lines) - Query processing and execution loop

## Refactoring Order

### Phase 1: Tool.ts → Go
**Priority: 1 (Foundation)**
**Estimated Complexity: Medium**

Tool.ts defines the core types and interfaces used by both QueryEngine and query. Must be refactored first as it's a dependency for the other two.

#### Key Components:
- `ToolInputJSONSchema` - JSON schema for tool inputs
- `ToolUseContext` - Context passed to tool execution
- `ValidationResult` - Tool input validation results
- `ToolPermissionContext` - Permission management
- `CompactProgressEvent` - Compaction progress events
- `SetToolJSXFn` - UI callback types
- `CanUseToolFn` - Permission checking function

#### Go Package Structure:
```
internal/core/tool/
├── types.go           # Core tool types and interfaces
├── context.go         # ToolUseContext and related types
├── permissions.go     # Permission types and helpers
├── validation.go      # Input validation logic
└── tool_test.go       # Comprehensive tests
```

#### Key Types to Define:
```go
type ToolInputJSONSchema struct {
    Type                 string                 `json:"type"`
    Properties           map[string]interface{} `json:"properties,omitempty"`
    Required             []string               `json:"required,omitempty"`
    AdditionalProperties interface{}            `json:"additionalProperties,omitempty"`
}

type ValidationResult struct {
    Valid  bool
    Errors []ValidationError
}

type ToolUseContext struct {
    Options              ToolOptions
    AbortController      *AbortController
    ReadFileState        *FileStateCache
    GetAppState          func() AppState
    SetAppState          func(func(AppState) AppState)
    // ... many more fields
}

type ToolPermissionContext struct {
    Mode                           PermissionMode
    AdditionalWorkingDirectories   map[string]AdditionalWorkingDirectory
    AlwaysAllowRules               ToolPermissionRulesBySource
    AlwaysDenyRules                ToolPermissionRulesBySource
    AlwaysAskRules                 ToolPermissionRulesBySource
    IsBypassPermissionsModeAvailable bool
    // ... more fields
}

type CanUseToolFn func(
    tool Tool,
    input map[string]interface{},
    context *ToolUseContext,
    assistantMessage *AssistantMessage,
    toolUseID string,
    forceDecision *string,
) (*PermissionResult, error)
```

#### Dependencies:
- Message types (already defined in services)
- File state cache
- Abort controller
- App state types

---

### Phase 2: QueryEngine.ts → Go
**Priority: 2 (Session Management)**
**Estimated Complexity: High**

QueryEngine manages the conversation lifecycle and session state. Depends on Tool types.

#### Key Components:
- `QueryEngineConfig` - Configuration for query engine
- `QueryEngine` class - Main query engine implementation
- `submitMessage()` - Submit user message and get response stream
- Session state management (messages, usage, permissions)
- File cache management
- Orphaned permission handling
- Snip replay for history management

#### Go Package Structure:
```
internal/core/engine/
├── types.go           # QueryEngineConfig and related types
├── engine.go          # QueryEngine implementation
├── session.go         # Session state management
├── submit.go          # Message submission logic
├── snip.go            # History snipping/replay
└── engine_test.go     # Comprehensive tests
```

#### Key Types to Define:
```go
type QueryEngineConfig struct {
    Cwd                     string
    Tools                   Tools
    Commands                []Command
    MCPClients              []MCPServerConnection
    Agents                  []AgentDefinition
    CanUseTool              CanUseToolFn
    GetAppState             func() AppState
    SetAppState             func(func(AppState) AppState)
    InitialMessages         []Message
    ReadFileCache           *FileStateCache
    CustomSystemPrompt      string
    AppendSystemPrompt      string
    UserSpecifiedModel      string
    FallbackModel           string
    ThinkingConfig          *ThinkingConfig
    MaxTurns                int
    MaxBudgetUsd            float64
    TaskBudget              *TaskBudget
    JSONSchema              map[string]interface{}
    Verbose                 bool
    ReplayUserMessages      bool
    HandleElicitation       ElicitationHandler
    IncludePartialMessages  bool
    SetSDKStatus            func(SDKStatus)
    AbortController         *AbortController
    OrphanedPermission      *OrphanedPermission
    SnipReplay              SnipReplayFn
}

type QueryEngine struct {
    config                      *QueryEngineConfig
    mutableMessages             []Message
    abortController             *AbortController
    permissionDenials           []SDKPermissionDenial
    totalUsage                  NonNullableUsage
    hasHandledOrphanedPermission bool
    readFileState               *FileStateCache
    discoveredSkillNames        map[string]bool
    loadedNestedMemoryPaths     map[string]bool
    mu                          sync.RWMutex
}

func NewQueryEngine(config *QueryEngineConfig) *QueryEngine
func (qe *QueryEngine) SubmitMessage(prompt string, options *SubmitOptions) <-chan SDKMessage
func (qe *QueryEngine) GetMessages() []Message
func (qe *QueryEngine) GetTotalUsage() NonNullableUsage
func (qe *QueryEngine) Abort()
```

#### Key Features:
- Async message streaming via channels
- Session state persistence across turns
- Permission denial tracking
- Usage tracking and budget management
- File cache management
- Skill discovery tracking
- Nested memory path tracking
- Orphaned permission handling
- Snip replay for history management

---

### Phase 3: query.ts → Go
**Priority: 3 (Query Loop)**
**Estimated Complexity: Very High**

The query loop is the most complex component, handling the entire query execution flow with compaction, tool execution, error recovery, and streaming.

#### Key Components:
- `QueryParams` - Parameters for query execution
- `query()` - Main query generator function
- `queryLoop()` - Core query execution loop
- State management across iterations
- Auto-compaction tracking
- Max output tokens recovery
- Tool execution and result handling
- Streaming event generation
- Token budget tracking
- Task budget management
- Stop hooks
- Thinking block handling

#### Go Package Structure:
```
internal/core/query/
├── types.go           # QueryParams, State, and related types
├── query.go           # Main query function
├── loop.go            # Query loop implementation
├── state.go           # State management
├── compact.go         # Compaction logic
├── recovery.go        # Error recovery (max_output_tokens, etc.)
├── streaming.go       # Event streaming
├── budget.go          # Token and task budget tracking
├── hooks.go           # Stop hooks handling
└── query_test.go      # Comprehensive tests
```

#### Key Types to Define:
```go
type QueryParams struct {
    Messages                 []Message
    SystemPrompt             SystemPrompt
    UserContext              map[string]string
    SystemContext            map[string]string
    CanUseTool               CanUseToolFn
    ToolUseContext           *ToolUseContext
    FallbackModel            string
    QuerySource              QuerySource
    MaxOutputTokensOverride  int
    MaxTurns                 int
    SkipCacheWrite           bool
    TaskBudget               *TaskBudget
    Deps                     *QueryDeps
}

type State struct {
    Messages                     []Message
    ToolUseContext               *ToolUseContext
    AutoCompactTracking          *AutoCompactTrackingState
    MaxOutputTokensRecoveryCount int
    HasAttemptedReactiveCompact  bool
    MaxOutputTokensOverride      int
    PendingToolUseSummary        chan *ToolUseSummaryMessage
    StopHookActive               bool
    TurnCount                    int
    Transition                   *Continue
}

type QueryConfig struct {
    // Snapshot of immutable env/statsig/session state
    // Feature flags intentionally excluded
}

func Query(params *QueryParams) <-chan QueryEvent
func queryLoop(params *QueryParams, consumedCommandUuids []string) <-chan QueryEvent
```

#### Key Features:
- Async generator pattern using Go channels
- State management across loop iterations
- Auto-compaction with tracking
- Max output tokens recovery (up to 3 attempts)
- Reactive compaction on prompt_too_long
- Tool execution with streaming
- Token budget tracking and enforcement
- Task budget management
- Stop hooks (pre/post execution)
- Thinking block preservation
- Error recovery and retry logic
- Command lifecycle notifications
- Content replacement for tool results
- Sampling and analytics integration

#### Complex Algorithms:
1. **Query Loop State Machine**:
   - Continue conditions: tool_use, max_output_tokens, prompt_too_long
   - Terminal conditions: stop_reason, max_turns, budget_exceeded, error
   - State transitions with recovery paths

2. **Compaction Strategy**:
   - Auto-compact on message count threshold
   - Reactive compact on prompt_too_long error
   - Snip compact for history management
   - Task budget remaining tracking across compacts

3. **Error Recovery**:
   - Max output tokens: retry with increased limit (up to 3 times)
   - Prompt too long: trigger reactive compaction
   - Tool errors: inject error results and continue
   - Missing tool results: generate error blocks

4. **Streaming Event Generation**:
   - RequestStartEvent before API call
   - StreamEvent during response streaming
   - Message on completion
   - TombstoneMessage for replaced content
   - ToolUseSummaryMessage for background tasks

---

## Implementation Strategy

### Step 1: Tool Types (Week 1)
1. Create `internal/core/tool/` package
2. Define core types in `types.go`
3. Implement ToolUseContext in `context.go`
4. Implement permission types in `permissions.go`
5. Implement validation in `validation.go`
6. Write comprehensive tests
7. Document all types and functions

### Step 2: QueryEngine (Week 2)
1. Create `internal/core/engine/` package
2. Define QueryEngineConfig in `types.go`
3. Implement QueryEngine struct in `engine.go`
4. Implement session management in `session.go`
5. Implement SubmitMessage in `submit.go`
6. Implement snip replay in `snip.go`
7. Write comprehensive tests
8. Document all types and functions

### Step 3: Query Loop (Week 3-4)
1. Create `internal/core/query/` package
2. Define QueryParams and State in `types.go`
3. Implement main query function in `query.go`
4. Implement query loop in `loop.go`
5. Implement state management in `state.go`
6. Implement compaction in `compact.go`
7. Implement recovery in `recovery.go`
8. Implement streaming in `streaming.go`
9. Implement budget tracking in `budget.go`
10. Implement hooks in `hooks.go`
11. Write comprehensive tests for each component
12. Integration tests for full query flow
13. Document all types and functions

---

## Testing Strategy

### Unit Tests
- Test each type's methods independently
- Mock dependencies (API client, tools, etc.)
- Test error conditions and edge cases
- Test concurrent access patterns

### Integration Tests
- Test full query flow end-to-end
- Test compaction triggers and recovery
- Test tool execution and streaming
- Test budget enforcement
- Test permission handling

### Performance Tests
- Benchmark query loop performance
- Test memory usage with large message histories
- Test concurrent query execution
- Test streaming throughput

---

## Migration Considerations

### Breaking Changes
- Go doesn't have generators, use channels instead
- Go doesn't have async/await, use goroutines and channels
- Go doesn't have optional chaining, use explicit nil checks
- Go doesn't have union types, use interfaces or type switches

### Compatibility
- Maintain same API surface for SDK consumers
- Keep same event types and streaming protocol
- Preserve same error handling behavior
- Keep same analytics events

### Dependencies
- Compact service (already refactored)
- API service (already refactored)
- Tools service (already refactored)
- Analytics service (already refactored)
- OAuth service (already refactored)

---

## Success Criteria

### Tool Types
- [ ] All types defined with proper Go idioms
- [ ] All validation logic ported
- [ ] All permission types ported
- [ ] 100% test coverage
- [ ] Documentation complete

### QueryEngine
- [ ] Session management working
- [ ] Message submission streaming
- [ ] Permission tracking working
- [ ] Usage tracking working
- [ ] File cache integration working
- [ ] 100% test coverage
- [ ] Documentation complete

### Query Loop
- [ ] Full query loop working
- [ ] Compaction working (auto, reactive, snip)
- [ ] Error recovery working
- [ ] Tool execution working
- [ ] Streaming working
- [ ] Budget tracking working
- [ ] Hooks working
- [ ] 100% test coverage
- [ ] Documentation complete

---

## Timeline

- **Week 1**: Tool types refactoring
- **Week 2**: QueryEngine refactoring
- **Week 3-4**: Query loop refactoring
- **Week 5**: Integration testing and documentation

Total estimated time: 5 weeks

---

## Next Steps

1. Start with Tool.ts refactoring
2. Create `internal/core/tool/` package structure
3. Define core types in `types.go`
4. Implement each component file by file
5. Write tests as we go
6. Document thoroughly

Ready to begin Phase 1: Tool.ts refactoring.
