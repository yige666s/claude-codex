# Token Estimation Service

Token estimation service provides both API-based and rough estimation methods for counting tokens in messages and content.

## Features

### 1. API-Based Token Counting (`counter.go`)
- **CountTokensWithAPI**: Count tokens for simple text content
- **CountMessagesTokensWithAPI**: Count tokens for messages and tools
- **CountToolsTokens**: Count tokens for tools only
- **CountTokensViaHaikuFallback**: Fast fallback using Haiku model
- **Thinking Block Support**: Handles extended thinking blocks
- **Tool Search Field Stripping**: Removes beta-only fields before counting

### 2. Rough Estimation (`estimation.go`)
- **RoughTokenCountEstimation**: Fast local estimation using bytes-per-token heuristic
- **BytesPerTokenForFileType**: File-type-specific token ratios
- **RoughTokenCountEstimationForFileType**: File-aware token estimation
- **RoughTokenCountEstimationForMessages**: Message array estimation
- **HasThinkingBlocks**: Detect thinking blocks in messages

### 3. Type Definitions (`types.go`)
- **TokenCounter**: Interface for token counting
- **FileTypeTokenEstimator**: Interface for file-type-aware estimation
- **Constants**: Token counting configuration

## Usage

### API-Based Counting

```go
import (
    "context"
    "claude-codex/internal/services/tokens"
    api "claude-codex/pkg/anthropic"
)

// Create a counter
client := api.NewClient(apiKey, baseURL, timeout)
counter := tokens.NewCounter(client, "claude-sonnet-4-6")

// Count tokens for simple text
ctx := context.Background()
count, err := counter.CountTokensWithAPI(ctx, "Hello, world!")
if err != nil {
    log.Fatal(err)
}
fmt.Printf("Token count: %d\n", count)

// Count tokens for messages
messages := []api.InputMessage{
    {
        Role: "user",
        Content: []api.ContentBlock{
            {Type: "text", Text: "What is the weather?"},
        },
    },
}

tools := []api.Tool{
    {
        Name:        "get_weather",
        Description: "Get weather information",
        InputSchema: []byte(`{"type": "object", "properties": {}}`),
    },
}

count, err = counter.CountMessagesTokensWithAPI(ctx, messages, tools)
```

### Rough Estimation

```go
// Simple text estimation
content := "This is some text content"
tokens := tokens.RoughTokenCountEstimation(content, tokens.DefaultBytesPerToken)
fmt.Printf("Estimated tokens: %d\n", tokens)

// File-type-aware estimation
jsCode := "function hello() { return 'world'; }"
tokens = tokens.RoughTokenCountEstimationForFileType(jsCode, ".js")
fmt.Printf("Estimated tokens for JS: %d\n", tokens)

// Get bytes per token for file type
ratio := tokens.BytesPerTokenForFileType(".json")
fmt.Printf("JSON uses %d bytes per token\n", ratio)

// Estimate tokens for messages
messages := []api.InputMessage{
    {
        Role: "user",
        Content: []api.ContentBlock{
            {Type: "text", Text: "Hello"},
        },
    },
}
tokens = tokens.RoughTokenCountEstimationForMessages(messages)
```

### Thinking Block Detection

```go
messages := []api.InputMessage{
    {
        Role: "assistant",
        Content: []api.ContentBlock{
            {Type: "thinking", Text: "Let me think..."},
            {Type: "text", Text: "Here's my answer"},
        },
    },
}

hasThinking := tokens.HasThinkingBlocks(messages)
fmt.Printf("Has thinking blocks: %v\n", hasThinking)
```

## Architecture

### Token Counting Flow

```
User Request
    ↓
Counter.CountTokensWithAPI()
    ↓
stripToolSearchFieldsFromMessages()  ← Remove beta-only fields
    ↓
HasThinkingBlocks()                  ← Check for thinking blocks
    ↓
Build CountTokensRequest
    ├─ Add thinking config if needed
    └─ Set max_tokens appropriately
    ↓
client.CountTokens()                 ← Call Anthropic API
    ↓
Return token count
```

### Rough Estimation Flow

```
Content + File Type
    ↓
BytesPerTokenForFileType()           ← Get ratio for file type
    ↓
RoughTokenCountEstimation()          ← Calculate: bytes / ratio
    ↓
Return estimated tokens
```

## Constants

### Token Counting
- `TokenCountThinkingBudget`: 1024 tokens (minimum thinking budget)
- `TokenCountMaxTokens`: 2048 tokens (max tokens for counting with thinking)
- `DefaultBytesPerToken`: 4 bytes per token (default ratio)

### File Type Ratios
- **Dense formats** (JSON, XML, YAML): 3 bytes/token
- **Code** (JS, TS, Python, Go, etc.): 3 bytes/token
- **Markup** (HTML, CSS): 3 bytes/token
- **Plain text** (TXT, Markdown): 4 bytes/token
- **Default**: 4 bytes/token

## API Integration

### Anthropic API Endpoints
- **Count Tokens**: `POST /v1/messages/count_tokens`
- **Beta Header**: `anthropic-beta: token-counting-2024-11-01`

### Request Format
```json
{
  "model": "claude-sonnet-4-6",
  "messages": [...],
  "tools": [...],
  "max_tokens": 1,
  "thinking": {
    "type": "enabled",
    "budget_tokens": 1024
  }
}
```

### Response Format
```json
{
  "input_tokens": 42
}
```

## Tool Search Beta Support

The service automatically strips tool search beta fields before counting:
- **tool_use blocks**: Removes `caller` field
- **tool_result blocks**: Removes `tool_reference` content blocks

This ensures token counting works even when tool search beta is not enabled.

## Thinking Block Support

When messages contain thinking blocks:
- `max_tokens` is set to 2048 (API requirement)
- `thinking.budget_tokens` is set to 1024
- `thinking.type` is set to "enabled"

Thinking block types:
- `thinking`: Regular thinking block
- `redacted_thinking`: Redacted thinking block

## Error Handling

```go
count, err := counter.CountTokensWithAPI(ctx, content)
if err != nil {
    // API error - fall back to rough estimation
    count = tokens.RoughTokenCountEstimation(content, tokens.DefaultBytesPerToken)
}
```

## Testing

Run tests:
```bash
go test ./internal/services/tokens
```

Run tests with coverage:
```bash
go test -cover ./internal/services/tokens
```

## Migration from TypeScript

This module is a complete port of `src/services/tokenEstimation.ts`:

### Ported Features
- ✅ API-based token counting
- ✅ Rough estimation
- ✅ File-type-aware estimation
- ✅ Thinking block detection
- ✅ Tool search field stripping
- ✅ Haiku fallback

### Not Ported
- ❌ Bedrock provider support (not needed in Go backend)
- ❌ Vertex provider support (not needed in Go backend)
- ❌ VCR recording (testing utility, not core feature)

### Key Differences
- Go uses custom API client instead of official SDK
- Simpler type system (no union types)
- Direct struct access instead of interface{}
- No async/await (uses context.Context)

## Best Practices

1. **Use API counting for accuracy**: When exact token counts matter
2. **Use rough estimation for speed**: When approximate counts are sufficient
3. **Cache token counts**: Avoid repeated API calls for same content
4. **Handle errors gracefully**: Fall back to rough estimation on API errors
5. **Choose appropriate model**: Use Haiku for fast, cheap counting
6. **Consider file types**: Use file-type-aware estimation for better accuracy

## Performance

### API-Based Counting
- **Latency**: ~100-500ms (network + API processing)
- **Accuracy**: Exact token count
- **Cost**: Minimal (count_tokens is cheap)

### Rough Estimation
- **Latency**: <1ms (local calculation)
- **Accuracy**: ±20% typically
- **Cost**: Free

## Future Enhancements

- [ ] Token count caching
- [ ] Batch token counting
- [ ] Streaming token estimation
- [ ] More sophisticated estimation algorithms
- [ ] Per-model token counting
