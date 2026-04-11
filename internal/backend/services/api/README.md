# API Service

The API service provides robust API client functionality with retry logic, error handling, and multi-provider support for the Claude Code Go implementation.

## Features

### 1. Multi-Provider Support
- **First Party**: Direct Anthropic API
- **AWS Bedrock**: AWS-hosted Claude models
- **Vertex AI**: Google Cloud Vertex AI
- **Azure Foundry**: Microsoft Azure deployment

### 2. Retry Logic
- Exponential backoff with configurable delays
- Circuit breaker for consecutive 529 errors
- Query source-based retry policies
- Persistent retry mode for unattended sessions
- Automatic fallback to alternative models

### 3. Error Handling
- Comprehensive error classification
- Prompt-too-long detection and parsing
- Media size error detection
- Rate limit error handling
- Connection and SSL error detection

### 4. Usage Tracking
- Real-time API utilization monitoring
- Rate limit tracking across multiple time windows
- Extra usage credit tracking
- Warning thresholds and notifications

## Core Components

### types.go
Defines core types and constants:
- `RetryContext` - Context for retry operations
- `RetryOptions` - Retry configuration
- `ErrorClassification` - Error categorization
- `Utilization` - API usage and rate limits
- Constants for retry behavior and error messages

### errors.go
Error detection and classification:
- `IsPromptTooLongError()` - Detect prompt length errors
- `ParsePromptTooLongTokenCounts()` - Extract token counts
- `IsMediaSizeError()` - Detect media size errors
- `ClassifyAPIError()` - Categorize errors
- `FormatAPIError()` - Format errors for display

### retry.go
Retry logic implementation:
- `WithRetry()` - Execute with retry logic
- `RetryWithFallback()` - Retry with model fallback
- `CalculateBackoff()` - Exponential backoff calculation
- `RetryState` - Track retry attempts and state

### client.go
API client creation and management:
- `NewClient()` - Create configured API client
- `CreateMessageWithRetry()` - Create message with retries
- `CreateMessageWithFallback()` - Create with fallback model
- Multi-provider configuration

### usage.go
API usage and rate limit tracking:
- `FetchUtilization()` - Get current API usage
- `IsRateLimitApproaching()` - Check rate limit thresholds
- `GetHighestUtilization()` - Get peak utilization
- `FormatUtilization()` - Format usage for display

## Configuration

### Environment Variables

#### Authentication
- `ANTHROPIC_API_KEY` - Anthropic API key
- `CLAUDE_CODE_API_KEY` - Alternative API key variable

#### AWS Bedrock
- `AWS_REGION` or `AWS_DEFAULT_REGION` - AWS region (default: us-east-1)
- `ANTHROPIC_SMALL_FAST_MODEL_AWS_REGION` - Region override for Haiku

#### Azure Foundry
- `ANTHROPIC_FOUNDRY_RESOURCE` - Azure resource name
- `ANTHROPIC_FOUNDRY_BASE_URL` - Alternative base URL
- `ANTHROPIC_FOUNDRY_API_KEY` - Foundry API key

#### Vertex AI
- `ANTHROPIC_VERTEX_PROJECT_ID` - GCP project ID (required)
- `CLOUD_ML_REGION` - Default GCP region
- Model-specific region overrides:
  - `VERTEX_REGION_CLAUDE_3_5_HAIKU`
  - `VERTEX_REGION_CLAUDE_HAIKU_4_5`
  - `VERTEX_REGION_CLAUDE_3_5_SONNET`
  - `VERTEX_REGION_CLAUDE_3_7_SONNET`

#### Retry Behavior
- `CLAUDE_CODE_MAX_RETRIES` - Override default max retries (default: 10)
- `CLAUDE_CODE_UNATTENDED_RETRY` - Enable persistent retry mode

## Usage Examples

### Basic Client Creation

```go
import "claude-codex/internal/services/api"

// Create client with default configuration
client, err := api.NewClient(ctx, api.ClientConfig{
    Model: "claude-sonnet-4-6",
    MaxRetries: 3,
})
```

### Create Message with Retry

```go
response, err := client.CreateMessageWithRetry(ctx, 
    api.MessageRequest{
        Model: "claude-sonnet-4-6",
        Messages: messages,
        MaxTokens: 4096,
    },
    api.RetryOptions{
        MaxRetries: 5,
        QuerySource: "repl_main_thread",
    },
)
```

### Create Message with Fallback

```go
response, err := client.CreateMessageWithFallback(ctx,
    api.MessageRequest{
        Model: "claude-opus-4-6",
        Messages: messages,
        MaxTokens: 4096,
    },
    api.RetryOptions{
        MaxRetries: 3,
        FallbackModel: "claude-sonnet-4-6",
    },
)
```

### Check API Utilization

```go
usageClient := api.NewUsageClient("https://api.claude.ai")
utilization, err := usageClient.FetchUtilization(ctx, authToken)

if utilization.IsRateLimitApproaching(80.0) {
    fmt.Println(api.GetRateLimitWarningMessage(utilization))
}
```

## Constants

### Retry Behavior
- `DefaultMaxRetries`: 10
- `Max529Retries`: 3
- `BaseDelayMS`: 500ms
- `PersistentMaxBackoffMS`: 5 minutes
- `MinCooldownMS`: 10 minutes

### Error Messages
- `APIErrorMessagePrefix`: "API Error"
- `PromptTooLongErrorMessage`: "Prompt is too long"
- `Repeated529ErrorMessage`: "API is experiencing high demand"

## Error Types

### CannotRetryError
Wraps errors that cannot be retried after exhausting retry attempts.

### FallbackTriggeredError
Indicates a fallback model was used due to errors with the primary model.

## Query Sources

Foreground query sources that retry on 529 errors:
- `repl_main_thread` - Main REPL queries
- `sdk` - SDK queries
- `agent:*` - Agent queries
- `compact` - Compaction operations
- `verification_agent` - Verification queries
- `auto_mode` - Auto-mode security classifiers

Background sources (no 529 retry):
- Summaries, titles, suggestions, classifiers

## Testing

Run tests:
```bash
go test ./internal/services/api/...
```

Run with coverage:
```bash
go test -cover ./internal/services/api/...
```

## Integration

The API service integrates with:
- `pkg/anthropic` - Anthropic API client
- Compact service - For auto-compaction
- OAuth service - For token management
- Analytics service - For event logging

## Performance Characteristics

- **Retry latency**: 500ms base, exponential backoff
- **Max backoff**: 5 minutes (persistent mode)
- **Circuit breaker**: 3 consecutive 529 errors
- **Utilization check**: 5 second timeout
- **Heartbeat interval**: 30 seconds (persistent mode)

## Next Steps

After API service completion, the next service to refactor is: **tools service**
