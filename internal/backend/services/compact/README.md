package compact

// README for the compact service

/*
# Compact Service

The compact service handles conversation compaction and context window management for Claude Code.

## Features

### 1. Auto-Compaction (`autocompact.go`)
- **GetEffectiveContextWindowSize**: Calculate usable context window
- **GetAutoCompactThreshold**: Determine when to trigger auto-compaction
- **CalculateTokenWarningState**: Track token usage and warning thresholds
- **ShouldTriggerAutoCompact**: Check if auto-compaction should trigger
- **Circuit Breaker**: Stop retrying after consecutive failures

### 2. Microcompaction (`microcompact.go`)
- **TimeBasedMicrocompact**: Clear old tool results based on time gaps
- **EstimateMessageTokens**: Estimate token count for messages
- **CalculateToolResultTokens**: Calculate tokens for tool results
- **StripImagesFromMessages**: Remove images to reduce token usage
- **IsCompactableTool**: Check if a tool can be compacted

### 3. Message Grouping (`grouping.go`)
- **GroupMessagesByAPIRound**: Group messages by API round-trip boundaries
- **HasTextBlocks**: Check if message contains text content
- **GetToolResultIDs**: Extract tool_use_ids from tool results
- **HasToolUseWithIDs**: Check for tool_use blocks with specific IDs

### 4. Compaction Prompts (`prompt.go`)
- **GetCompactPrompt**: Generate compaction prompts (full or partial)
- **FormatCompactSummary**: Format and clean summary output
- **GetCompactUserSummaryMessage**: Create user-facing summary message
- **NoToolsPreamble**: Instruction to prevent tool use during compaction

### 5. Core Compaction (`compact.go`)
- **CompactConversation**: Perform full conversation compaction
- **PartialCompact**: Compact recent messages only
- **BuildPostCompactMessages**: Build message array after compaction
- **ShouldExcludeFromPostCompactRestore**: Check file exclusion rules

### 6. Type Definitions (`types.go`)
- **CompactionResult**: Result of compaction operation
- **AutoCompactTrackingState**: Track auto-compaction state
- **RecompactionInfo**: Information about recompaction
- **TokenWarningState**: Token usage warning state
- **CompactableTools**: Map of tools that can be compacted

## Constants

### Buffer Tokens
- `AutoCompactBufferTokens`: 13,000 tokens
- `WarningThresholdBufferTokens`: 20,000 tokens
- `ErrorThresholdBufferTokens`: 20,000 tokens
- `ManualCompactBufferTokens`: 3,000 tokens

### Limits
- `MaxOutputTokensForSummary`: 20,000 tokens
- `MaxConsecutiveAutoCompactFailures`: 3 attempts
- `ImageMaxTokenSize`: 2,000 tokens
- `TimeBasedMCClearedMessage`: "[Old tool result content cleared]"

### Post-Compact Restoration
- `PostCompactMaxFilesToRestore`: 5 files
- `PostCompactTokenBudget`: 50,000 tokens
- `PostCompactMaxTokensPerFile`: 5,000 tokens
- `PostCompactMaxTokensPerSkill`: 5,000 tokens
- `PostCompactSkillsTokenBudget`: 25,000 tokens

### Streaming
- `MaxCompactStreamingRetries`: 2 retries

## Usage

### Auto-Compaction

```go
import (
    "context"
    "github.com/ding/claude-code/claude-go/internal/services/compact"
    api "github.com/ding/claude-code/claude-go/pkg/anthropic"
)

// Check if auto-compaction should trigger
tracking := &compact.AutoCompactTrackingState{
    Compacted:           false,
    TurnCounter:         1,
    TurnID:              "turn-123",
    ConsecutiveFailures: 0,
}

shouldCompact := compact.ShouldTriggerAutoCompact(
    tokenUsage,
    model,
    contextWindow,
    tracking,
    turnID,
)

if shouldCompact {
    result, err := compact.TryAutoCompact(
        ctx,
        messages,
        model,
        contextWindow,
        tracking,
        turnID,
    )
}
```

### Token Warning State

```go
state := compact.CalculateTokenWarningState(
    tokenUsage,
    model,
    contextWindow,
    autoCompactEnabled,
)

if state.IsAboveWarningThreshold {
    // Show warning to user
}

if state.IsAboveAutoCompactThreshold {
    // Trigger auto-compaction
}
```

### Manual Compaction

```go
result, err := compact.CompactConversation(
    ctx,
    messages,
    model,
    client,
    suppressFollowUpQuestions,
    customInstructions,
    isAutoCompact,
)

if err != nil {
    log.Fatal(err)
}

fmt.Printf("Compacted %d messages, freed %d tokens\n",
    result.CompactedMessages, result.TokensFreed)
```

### Microcompaction

```go
// Time-based microcompaction
compactedMessages, tokensSaved := compact.TimeBasedMicrocompact(
    messages,
    gapThresholdMinutes,
    keepRecent,
)

// Strip images from messages
strippedMessages := compact.StripImagesFromMessages(messages)
```

## Architecture

### Auto-Compaction Flow

```
Token Usage Check
    ↓
ShouldTriggerAutoCompact()
    ├─ Check if enabled
    ├─ Check circuit breaker
    ├─ Check turn ID
    └─ Check threshold
    ↓
TryAutoCompact()
    ↓
CompactConversation()
    ├─ Strip images
    ├─ Build prompt
    ├─ Call API
    └─ Format summary
    ↓
Return CompactionResult
```

### Microcompaction Flow

```
Messages + Time Gap
    ↓
TimeBasedMicrocompact()
    ├─ Find last assistant message
    ├─ Calculate time gap
    ├─ Identify tool results
    ├─ Keep recent N results
    └─ Clear old results
    ↓
Return compacted messages + tokens saved
```

## Environment Variables

- `CLAUDE_CODE_AUTO_COMPACT_WINDOW`: Override context window size
- `CLAUDE_AUTOCOMPACT_PCT_OVERRIDE`: Override auto-compact threshold percentage
- `CLAUDE_CODE_BLOCKING_LIMIT_OVERRIDE`: Override blocking limit
- `DISABLE_COMPACT`: Disable all compaction
- `DISABLE_AUTO_COMPACT`: Disable only auto-compaction

## Testing

Run tests:
```bash
go test ./internal/services/compact
```

Run tests with coverage:
```bash
go test -cover ./internal/services/compact
```

## Migration from TypeScript

This module is a port of `src/services/compact/`:

### Ported Features
- ✅ Auto-compaction with circuit breaker
- ✅ Token warning thresholds
- ✅ Microcompaction (time-based)
- ✅ Image stripping
- ✅ Message grouping by API round
- ✅ Compaction prompts
- ✅ Summary formatting

### Simplified Features
- ⚠️ Session memory compaction (simplified)
- ⚠️ Post-compact file restoration (placeholder)
- ⚠️ Cached microcompact (not ported - Bun-specific)

### Not Ported
- ❌ Hook system integration (handled at higher level)
- ❌ Analytics integration (separate service)
- ❌ Prompt cache break detection (API-specific)
- ❌ Session transcript handling (separate service)

## Best Practices

1. **Enable auto-compaction**: Let the system manage context automatically
2. **Monitor token usage**: Use CalculateTokenWarningState regularly
3. **Handle circuit breaker**: Respect consecutive failure limits
4. **Strip images**: Always strip images before compaction
5. **Preserve recent messages**: Keep recent context for continuity
6. **Test thresholds**: Use environment variables for testing

## Performance

### Auto-Compaction
- **Trigger**: ~180K tokens (for 200K context window)
- **Latency**: ~5-10s (API call + processing)
- **Token savings**: 50-80% typically

### Microcompaction
- **Trigger**: Time-based (60+ minutes gap)
- **Latency**: <100ms (local processing)
- **Token savings**: 10-30% typically

## Future Enhancements

- [ ] Session memory compaction
- [ ] Post-compact file restoration
- [ ] Partial compaction (backward/forward)
- [ ] Streaming compaction
- [ ] Compaction quality metrics
- [ ] Adaptive threshold tuning
*/
