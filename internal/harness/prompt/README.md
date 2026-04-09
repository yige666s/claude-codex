# Prompt Package

The `prompt` package provides a robust system for building and managing system prompts for Claude API calls, with built-in caching and section management.

## Overview

This package is a Go port of the TypeScript `systemPromptType.ts` system, providing:

- **Immutable [REDACTED] type**: Thread-safe prompt management
- **Section-based composition**: Build prompts from reusable sections
- **Intelligent caching**: Avoid recomputing static sections
- **Cache invalidation**: Support for dynamic sections that need fresh data

## Core Types

### [REDACTED]

An immutable collection of prompt strings that form the system context for Claude API calls.

```go
sections := []string{"identity", "capabilities", "rules"}
prompt := prompt.NewSystemPrompt(sections)

// Get sections (returns a copy)
sections := prompt.Sections()

// Check if empty
if prompt.IsEmpty() {
    // handle empty prompt
}
```

### SystemPromptSection

Represents a single section with optional caching behavior.

```go
// Cached section (computed once)
section := prompt.NewSection("identity", func() (string, error) {
    return "You are Claude, an AI assistant...", nil
})

// Uncached section (recomputed every turn)
section := prompt.NewUncachedSection("timestamp", func() (string, error) {
    return fmt.Sprintf("Current time: %s", time.Now()), nil
}, "needs fresh timestamp")
```

## Builder

The `Builder` manages section resolution and caching.

```go
builder := prompt.NewBuilder()
ctx := context.Background()

sections := []*prompt.SystemPromptSection{
    prompt.NewSection("identity", computeIdentity),
    prompt.NewSection("capabilities", computeCapabilities),
    prompt.NewUncachedSection("context", computeContext, "dynamic context"),
}

// Build prompt (cached sections reused)
[REDACTED], err := builder.BuildFromSections(ctx, sections)
if err != nil {
    return err
}

// Clear cache (e.g., on /clear or /compact)
builder.ClearCache()

// Get cache statistics
stats := builder.GetCacheStats()
fmt.Printf("Cache size: %d sections\n", stats.Size)
```

## Cache Management

The `SectionCache` provides fine-grained cache control.

```go
cache := prompt.NewSectionCache()

// Store section
cache.Set("identity", "You are Claude...")

// Retrieve section
if value, found := cache.Get("identity"); found {
    fmt.Println(value)
}

// Check existence
if cache.Has("identity") {
    // section is cached
}

// Invalidate old entries
removed := cache.InvalidateOlderThan(1 * time.Hour)

// Clear all
cache.Clear()

// Get statistics
stats := cache.Stats()
```

## Prompt Context

The `PromptContext` holds all context needed to build system prompts.

```go
promptCtx := prompt.NewPromptContext()
promptCtx.CustomSystemPrompt = "Custom instructions..."
promptCtx.AppendSystemPrompt = "Additional context..."
promptCtx.MainLoopModel = "[REDACTED]"
promptCtx.UserContext = map[string]string{
    "claudeMd": "# Project instructions...",
}
promptCtx.SystemContext = map[string]string{
    "gitStatus": "Current branch: main...",
}

[REDACTED], err := builder.BuildSystemPrompt(ctx, promptCtx)
```

## Merging Prompts

Combine multiple prompts into one.

```go
prompt1 := prompt.NewSystemPrompt([]string{"section1", "section2"})
prompt2 := prompt.NewSystemPrompt([]string{"section3"})

merged := prompt.MergePrompts(prompt1, prompt2)
// Result: ["section1", "section2", "section3"]
```

## Usage Patterns

### Basic Prompt Building

```go
builder := prompt.NewBuilder()

// Simple string-based prompt
sections := []string{
    "You are Claude, an AI assistant.",
    "You help users with coding tasks.",
}
[REDACTED] := builder.BuildFromStrings(sections)
```

### Section-Based Building with Caching

```go
builder := prompt.NewBuilder()

sections := []*prompt.SystemPromptSection{
    prompt.NewSection("identity", func() (string, error) {
        // Computed once, then cached
        return loadIdentityPrompt()
    }),
    
    prompt.NewSection("tools", func() (string, error) {
        // Computed once, then cached
        return buildToolsPrompt(availableTools)
    }),
    
    prompt.NewUncachedSection("context", func() (string, error) {
        // Recomputed every turn
        return getCurrentContext()
    }, "context changes every turn"),
}

[REDACTED], err := builder.BuildFromSections(ctx, sections)
```

### Cache Lifecycle Management

```go
// On conversation start
builder := prompt.NewBuilder()

// During conversation - sections are cached
for i := 0; i < 10; i++ {
    prompt, _ := builder.BuildFromSections(ctx, sections)
    // Cached sections reused
}

// On /clear or /compact command
builder.ClearCache()

// On periodic cleanup
builder.cache.InvalidateOlderThan(1 * time.Hour)
```

## Design Principles

1. **Immutability**: [REDACTED] is immutable to prevent accidental modifications
2. **Thread Safety**: All operations are thread-safe with proper locking
3. **Lazy Computation**: Sections are only computed when needed
4. **Cache Efficiency**: Static sections are cached to avoid redundant computation
5. **Explicit Cache Breaking**: Dynamic sections explicitly opt-out of caching

## Comparison with TypeScript

| TypeScript | Go | Notes |
|------------|-----|-------|
| `[REDACTED]` (branded type) | `[REDACTED]` (struct) | Go uses struct with mutex for thread safety |
| `asSystemPrompt()` | `NewSystemPrompt()` | Go uses constructor pattern |
| `systemPromptSection()` | `NewSection()` | Similar API |
| `DANGEROUS_uncachedSystemPromptSection()` | `NewUncachedSection()` | Explicit cache-breaking |
| `resolveSystemPromptSections()` | `Builder.BuildFromSections()` | Go uses builder pattern |
| Lodash memoize | `SectionCache` | Custom cache implementation |

## Testing

Run tests with:

```bash
cd /Users/ding/projectSrc/claude-code/claude-go/internal/harness/prompt
go test -v -cover
```

Expected coverage: >60%

## Thread Safety

All public methods are thread-safe:
- `[REDACTED]`: Uses `sync.RWMutex` for read/write operations
- `Builder`: Uses `sync.RWMutex` for cache access
- `SectionCache`: Uses `sync.RWMutex` for all operations

## Performance Considerations

- **Cache hits**: O(1) lookup with map-based cache
- **Section computation**: Only computed once for cached sections
- **Memory**: Cached sections remain in memory until cleared
- **Concurrency**: Read operations can proceed in parallel

## Future Enhancements

- [ ] TTL-based cache expiration
- [ ] Cache size limits with LRU eviction
- [ ] Metrics and observability hooks
- [ ] Compression for large sections
- [ ] Persistent cache storage
