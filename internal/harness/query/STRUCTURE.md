# Query Package Structure

```
internal/core/query/
├── types.go              (206 lines) - Core type definitions
│   ├── QueryParams       - Input parameters
│   ├── State             - Mutable iteration state
│   ├── QueryConfig       - Immutable configuration
│   ├── Terminal          - Exit reasons
│   ├── Continue          - Continuation reasons
│   ├── BudgetTracker     - Token budget tracking
│   └── Constants         - Terminal/Continue reason constants
│
├── query.go              (106 lines) - Main entry point
│   ├── Query()           - Public API, returns channels
│   ├── buildQueryConfig() - Configuration builder
│   └── productionDeps()  - Dependency injection
│
├── loop.go               (666 lines) - Core execution loop
│   ├── queryLoop()       - Main state machine
│   ├── State transitions - Tool use, recovery, completion
│   ├── API call handling - Streaming, fallback
│   ├── Tool execution    - Orchestration
│   └── Turn management   - Max turns, queued commands
│
├── state.go              (163 lines) - State management
│   ├── SaveState()       - Persist state
│   ├── LoadState()       - Restore state
│   ├── CloneState()      - Deep copy
│   └── Update helpers    - State mutation functions
│
├── recovery.go           (138 lines) - Error recovery
│   ├── handleMaxOutputTokensRecovery() - 3-attempt strategy
│   ├── handlePromptTooLongRecovery()   - Compaction retry
│   ├── recoverFromImageError()         - Image handling
│   └── preserveThinkingBlocks()        - Thinking rules
│
├── compact.go            (97 lines) - Auto-compaction
│   ├── performCompaction()         - Execute compaction
│   ├── performReactiveCompaction() - Prompt-too-long recovery
│   ├── shouldAutoCompact()         - Decision logic
│   └── calculateTokenWarningState() - Token thresholds
│
├── budget.go             (91 lines) - Token budget
│   ├── createBudgetTracker()  - Initialize tracker
│   ├── checkTokenBudget()     - Continue/stop decision
│   └── Diminishing returns    - <500 token detection
│
├── streaming.go          (158 lines) - Event streaming
│   ├── streamModelResponse()  - Process streaming
│   ├── generateStreamEvent()  - Event creation
│   └── Emit functions         - Various event types
│
├── hooks.go              (62 lines) - Stop hooks
│   ├── handleStopHooks()      - Orchestration
│   ├── executeStopHooks()     - Stop hook execution
│   ├── executeTaskCompletedHooks() - Task hooks
│   └── executeTeammateIdleHooks()  - Teammate hooks
│
├── query_test.go         (498 lines) - Comprehensive tests
│   ├── TestQuery_BasicExecution
│   ├── TestQuery_MaxOutputTokensRecovery
│   ├── TestQuery_ToolUseExecution
│   ├── TestQuery_MaxTurnsLimit
│   ├── TestQuery_ContextCancellation
│   ├── TestBudgetTracker_* (3 tests)
│   ├── TestState_* (2 tests)
│   ├── TestRecovery_* (2 tests)
│   └── Benchmarks (2)
│
├── README.md             (350 lines) - Package documentation
│   ├── Overview
│   ├── Usage examples
│   ├── State machine
│   ├── Recovery mechanisms
│   ├── Token budget
│   ├── Testing guide
│   └── Migration notes
│
├── REFACTORING_SUMMARY.md (300 lines) - Refactoring details
│   ├── Files created
│   ├── Design decisions
│   ├── State machine
│   ├── Recovery mechanisms
│   ├── TODO items
│   └── Next steps
│
└── STRUCTURE.md          (This file) - Visual structure

Total: 2,185 lines of Go code + 650 lines of documentation
```

## Data Flow

```
User Request
    ↓
Query() ─────────────────────────────────────────┐
    ↓                                             │
queryLoop() ←─────────────────────────────┐      │
    ↓                                      │      │
┌───────────────────────────────────┐     │      │
│   State Machine Iteration         │     │      │
├───────────────────────────────────┤     │      │
│ 1. Check queued commands          │     │      │
│ 2. Auto-compaction check          │     │      │
│ 3. API call (streaming)           │     │      │
│ 4. Process assistant messages     │     │      │
│ 5. Handle errors/recovery         │     │      │
│ 6. Execute stop hooks             │     │      │
│ 7. Check token budget             │     │      │
│ 8. Execute tools                  │     │      │
│ 9. Update state                   │     │      │
└───────────────────────────────────┘     │      │
    │                                      │      │
    ├─ Continue? ─────────────────────────┘      │
    │                                             │
    └─ Terminal ──────────────────────────────────┤
                                                  │
                                                  ↓
                                          Event Channel
                                          Terminal Channel
```

## State Transitions

```
Initial State
    ↓
┌─────────────────────────────────────────────────┐
│              Query Loop Iteration               │
└─────────────────────────────────────────────────┘
    │
    ├─→ Queued Command? ──→ Continue (queued_command)
    │
    ├─→ Auto-compact? ──→ Compact ──→ Continue
    │
    ├─→ API Call ──→ Streaming
    │                   │
    │                   ├─→ Fallback? ──→ Retry with fallback model
    │                   │
    │                   └─→ Assistant Messages
    │                           │
    │                           ├─→ Tool Use? ──→ Execute Tools ──→ Continue (tool_use)
    │                           │
    │                           ├─→ Max Output Tokens? ──→ Recovery ──→ Continue (recovery)
    │                           │
    │                           └─→ Prompt Too Long? ──→ Compact ──→ Continue (compact_retry)
    │
    ├─→ Stop Hooks ──→ Blocking? ──→ Continue (stop_hook_blocking)
    │              └─→ Prevented? ──→ Terminal (stop_hook_prevented)
    │
    ├─→ Token Budget ──→ Continue? ──→ Continue (token_budget_continuation)
    │                └─→ Stop? ──→ Terminal (completed)
    │
    ├─→ Max Turns? ──→ Terminal (max_turns)
    │
    └─→ Completed ──→ Terminal (completed)
```

## Recovery Flow

```
Max Output Tokens Error
    ↓
Attempt 1: +4096 tokens ──→ Success? ──→ Continue
    │                           │
    └─→ Fail ──→ Attempt 2: +4096 tokens ──→ Success? ──→ Continue
                    │                           │
                    └─→ Fail ──→ Attempt 3: Escalate model ──→ Success? ──→ Continue
                                    │                           │
                                    └─→ Fail ──→ Terminal (model_error)

Prompt Too Long Error
    ↓
Context Collapse Drain ──→ Success? ──→ Continue
    │                           │
    └─→ Fail ──→ Reactive Compaction ──→ Success? ──→ Continue
                    │                           │
                    └─→ Fail ──→ Terminal (prompt_too_long)
```

## Token Budget Flow

```
Turn Completes
    ↓
Check Token Budget
    │
    ├─→ No budget set ──→ Stop
    │
    ├─→ Subagent ──→ Stop
    │
    ├─→ < 90% of budget ──→ Diminishing returns?
    │                           │
    │                           ├─→ Yes ──→ Stop (completion_event)
    │                           │
    │                           └─→ No ──→ Continue (auto-continuation)
    │
    └─→ ≥ 90% of budget ──→ Stop (completion_event)
```

## Integration Points

```
Query Package
    │
    ├─→ Tool Package (internal/core/tool)
    │   ├── Tool execution
    │   ├── Streaming executor
    │   └── Tool context
    │
    ├─→ Compact Service (internal/services/compact)
    │   ├── Auto-compaction
    │   ├── Reactive compaction
    │   └── Token calculation
    │
    ├─→ API Service (internal/services/api)
    │   ├── Model API calls
    │   ├── Streaming responses
    │   └── Error handling
    │
    └─→ Types Package (internal/types)
        ├── Message types
        ├── Assistant messages
        └── Stream events
```

## Key Metrics

- **Total Lines**: 2,185 lines of Go code
- **Test Coverage**: 12 unit tests + 2 benchmarks
- **Files**: 10 Go files + 3 documentation files
- **State Transitions**: 9 terminal + 9 continue states
- **Recovery Strategies**: 3 for max tokens + 2 for prompt too long
- **Thread Safety**: Mutex-protected state and budget tracking
- **Performance**: Buffered channels, async summaries, streaming execution
