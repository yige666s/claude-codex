# Context Module

The context module provides context management for Claude Code, including system context, user context, git status, and context window management.

## Features

### 1. System Context (`context.go`)
- **Git Status**: Retrieves and formats git repository information
- **Cache Breaker**: System prompt injection for cache breaking (debugging)
- **Memoization**: Caches context for the duration of the conversation

### 2. User Context (`context.go`)
- **CLAUDE.md Loading**: Loads CLAUDE.md files from working directory and parent directories
- **Current Date**: Provides current date in ISO format
- **Global CLAUDE.md**: Supports global CLAUDE.md in `~/.claude/`

### 3. Git Integration (`git.go`)
- **Repository Detection**: Checks if directory is a git repository
- **Branch Information**: Current branch and main branch detection
- **Status**: Git status with 2k character truncation
- **Recent Commits**: Last 5 commits
- **User Information**: Git user name

### 4. Context Window Management (`window.go`)
- **Model Detection**: Automatic context window size detection
- **1M Context Support**: Detects models supporting 1M context window
- **Max Output Tokens**: Model-specific max output token configuration
- **Usage Calculation**: Context window usage percentage calculation

## Usage

### Get System Context

```go
import "github.com/ding/claude-code/claude-go/internal/context"

// Get system context with git status
ctx, err := context.GetSystemContext("/path/to/repo", true)
if err != nil {
    log.Fatal(err)
}

// Access git status
if gitStatus, ok := ctx["gitStatus"]; ok {
    fmt.Println(gitStatus)
}
```

### Get User Context

```go
// Get user context with CLAUDE.md
ctx, err := context.GetUserContext("/path/to/repo", false)
if err != nil {
    log.Fatal(err)
}

// Access CLAUDE.md content
if claudeMd, ok := ctx["claudeMd"]; ok {
    fmt.Println(claudeMd)
}

// Access current date
fmt.Println(ctx["currentDate"])
```

### Context Window Management

```go
// Get context window size for a model
windowSize := context.GetContextWindowForModel("claude-sonnet-4-6")
fmt.Printf("Context window: %d tokens\n", windowSize)

// Get max output tokens
maxTokens := context.GetModelMaxOutputTokens("claude-sonnet-4-6")
fmt.Printf("Default: %d, Upper limit: %d\n", maxTokens.Default, maxTokens.UpperLimit)

// Calculate usage percentages
usage := &context.TokenUsage{
    InputTokens:              100_000,
    CacheCreationInputTokens: 0,
    CacheReadInputTokens:     0,
}
used, remaining := context.CalculateContextPercentages(usage, 200_000)
fmt.Printf("Used: %d%%, Remaining: %d%%\n", used, remaining)
```

### Git Status

```go
// Get git status
gitInfo, err := context.GetGitStatus("/path/to/repo")
if err != nil {
    log.Fatal(err)
}

// Format for display
formatted := context.FormatGitStatus(gitInfo)
fmt.Println(formatted)
```

## Architecture

### Caching Strategy
- **System Context**: Cached once per conversation using `sync.Once`
- **User Context**: Cached once per conversation using `sync.Once`
- **Git Status**: Cached once per conversation using `sync.Once`
- **Thread-Safe**: All caches protected by `sync.RWMutex`

### Cache Invalidation
```go
// Clear system context cache
context.ClearSystemContextCache()

// Clear user context cache
context.ClearUserContextCache()

// Clear git status cache
context.ClearGitStatusCache()

// Set system prompt injection (clears all caches)
context.SetSystemPromptInjection("test-injection")
```

## Environment Variables

### Context Window
- `CLAUDE_CODE_MAX_CONTEXT_TOKENS`: Override context window size
- `CLAUDE_CODE_DISABLE_1M_CONTEXT`: Disable 1M context support

### CLAUDE.md
- `CLAUDE_CODE_DISABLE_CLAUDE_MDS`: Disable CLAUDE.md loading

## Model Support

### 1M Context Window
- `claude-sonnet-4-6`
- `claude-opus-4-6`
- Models with `[1m]` suffix

### Max Output Tokens
- **Opus 4.6**: 64k default, 128k upper limit
- **Sonnet 4.6**: 32k default, 128k upper limit
- **Sonnet 4**: 32k default, 64k upper limit
- **Claude 3 Opus**: 4k default, 4k upper limit
- **Claude 3 Sonnet**: 8k default, 8k upper limit

## Testing

Run tests:
```bash
go test ./internal/context
```

Run tests with coverage:
```bash
go test -cover ./internal/context
```

## Implementation Notes

### Git Status Truncation
Git status is truncated to 2000 characters to prevent excessive context usage. Users are instructed to use BashTool if they need more information.

### CLAUDE.md Discovery
The module searches for CLAUDE.md files in:
1. Global: `~/.claude/CLAUDE.md`
2. Current directory and all parent directories up to root

Multiple CLAUDE.md files are concatenated with `---` separator.

### Thread Safety
All public functions are thread-safe. Caches use `sync.RWMutex` for concurrent read access and exclusive write access.

### Memoization
Context is memoized for the duration of the conversation to avoid repeated expensive operations (git commands, file I/O).

## Migration from TypeScript

This module is a complete port of the TypeScript context handling logic:
- `src/context.ts` → `context.go`, `git.go`
- `src/utils/context.ts` → `window.go`
- `src/commands/context/context.tsx` → (UI command, not ported)

Key differences:
- Go uses `sync.Once` for memoization instead of lodash `memoize`
- Go uses `sync.RWMutex` for thread safety
- Git commands use `os/exec` instead of Node.js child_process
- CLAUDE.md loading uses `os.ReadFile` instead of fs.readFile
