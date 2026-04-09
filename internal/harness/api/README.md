# Claude API Client for Go

A clean, idiomatic Go implementation of the Claude API client, migrated from the TypeScript version.

## Features

- ✅ Non-streaming and streaming API requests
- ✅ Automatic retry logic with exponential backoff
- ✅ Context-aware request handling
- ✅ Comprehensive error handling
- ✅ Token counting utilities
- ✅ Request validation
- ✅ Thread-safe streaming processors
- ✅ Full test coverage

## Installation

```bash
go get github.com/yourusername/claude-go/internal/harness/api
```

## Quick Start

### Non-Streaming Request

```go
package main

import (
    "context"
    "fmt"
    "log"
    
    "github.com/yourusername/claude-go/internal/harness/api"
)

func main() {
    client := api.NewClient(api.ClientOptions{
        APIKey: "your-api-key",
    })
    
    resp, err := client.CreateMessage(context.Background(), api.CreateMessageRequest{
        Model:     "claude-3-sonnet-20240229",
        MaxTokens: 1024,
        Messages: []api.Message{
            {
                Role:    "user",
                Content: "Hello! What is the capital of France?",
            },
        },
    })
    if err != nil {
        log.Fatal(err)
    }
    
    fmt.Println(resp.Content[0].Text)
}
```

### Streaming Request

```go
eventChan, err := client.CreateMessageStream(context.Background(), api.CreateMessageRequest{
    Model:     "claude-3-sonnet-20240229",
    MaxTokens: 1024,
    Messages: []api.Message{
        {
            Role:    "user",
            Content: "Count from 1 to 5.",
        },
    },
})
if err != nil {
    log.Fatal(err)
}

// Option 1: Use text handler
handler := api.NewTextStreamHandler()
handler.OnText = func(text string) {
    fmt.Print(text)
}

if err := api.ProcessStream(context.Background(), eventChan, handler.Handle); err != nil {
    log.Fatal(err)
}

fmt.Println("\nFinal:", handler.GetText())
```

### Stream to Complete Response

```go
eventChan, err := client.CreateMessageStream(context.Background(), req)
if err != nil {
    log.Fatal(err)
}

resp, err := api.StreamToResponse(context.Background(), eventChan)
if err != nil {
    log.Fatal(err)
}

fmt.Println(resp.Content[0].Text)
```

## Configuration

### Client Options

```go
client := api.NewClient(api.ClientOptions{
    APIKey:     "your-api-key",
    BaseURL:    "https://api.anthropic.com", // Optional, defaults to official API
    MaxRetries: 3,                            // Optional, defaults to 3
    Timeout:    60 * time.Second,            // Optional, defaults to 60s
    UserAgent:  "my-app/1.0",                // Optional, defaults to "claude-go/1.0"
})
```

### Retry Configuration

The client automatically retries on:
- Rate limit errors (429)
- Server errors (5xx)

With exponential backoff:
- Initial backoff: 1 second
- Max backoff: 60 seconds
- Backoff factor: 2.0

## API Reference

### Types

#### `CreateMessageRequest`
```go
type CreateMessageRequest struct {
    Model         string    // Required: Model identifier
    Messages      []Message // Required: Conversation messages
    MaxTokens     int       // Required: Maximum tokens to generate
    System        string    // Optional: [REDACTED]
    Temperature   float64   // Optional: Sampling temperature
    Tools         []Tool    // Optional: Available tools
    Stream        bool      // Optional: Enable streaming
    Metadata      *Metadata // Optional: Request metadata
    StopSequences []string  // Optional: Stop sequences
}
```

#### `Response`
```go
type Response struct {
    ID           string         // Message ID
    Type         string         // Response type
    Role         string         // Role (assistant)
    Content      []ContentBlock // Response content
    Model        string         // Model used
    StopReason   string         // Why generation stopped
    Usage        Usage          // Token usage
    RequestID    string         // Request ID from headers
    ResponseTime time.Duration  // Response time
}
```

#### `StreamEvent`
```go
type StreamEvent struct {
    Type  string      // Event type
    Index int         // Content block index
    Delta *Delta      // Content delta
    Usage *Usage      // Token usage
    Error *ErrorBlock // Error information
}
```

### Functions

#### `NewClient(opts ClientOptions) *Client`
Creates a new API client with the specified options.

#### `CreateMessage(ctx context.Context, req CreateMessageRequest) (*Response, error)`
Sends a non-streaming request and returns the complete response.

#### `CreateMessageStream(ctx context.Context, req CreateMessageRequest) (<-chan StreamEvent, error)`
Sends a streaming request and returns a channel of events.

#### `StreamToResponse(ctx context.Context, eventChan <-chan StreamEvent) (*Response, error)`
Converts a stream of events into a complete Response.

#### `ProcessStream(ctx context.Context, eventChan <-chan StreamEvent, handler StreamHandler) error`
Processes a stream with a custom handler function.

#### `CountTokens(text string) int`
Estimates token count for text (rough approximation: ~4 chars per token).

#### `ValidateRequest(req CreateMessageRequest) error`
Validates a request before sending to the API.

## Error Handling

The client provides structured error handling:

```go
resp, err := client.CreateMessage(ctx, req)
if err != nil {
    if apiErr, ok := err.(*api.APIError); ok {
        fmt.Printf("API Error %d: %s\n", apiErr.StatusCode, apiErr.Message)
        // Handle specific error types
        switch apiErr.StatusCode {
        case 429:
            // Rate limit
        case 401:
            // Authentication error
        case 500:
            // Server error
        }
    }
}
```

## Stream Processing

### Text Stream Handler

Simple handler for accumulating text:

```go
handler := api.NewTextStreamHandler()
handler.OnText = func(text string) {
    fmt.Print(text) // Print each delta as it arrives
}

api.ProcessStream(ctx, eventChan, handler.Handle)
fmt.Println(handler.GetText()) // Get complete text
```

### Stream Collector

Collect all events for later processing:

```go
collector := api.NewStreamCollector()
api.ProcessStream(ctx, eventChan, collector.Collect)

events := collector.GetEvents()
for _, event := range events {
    // Process events
}
```

### Custom Stream Handler

```go
handler := func(event api.StreamEvent) error {
    switch event.Type {
    case "content_block_delta":
        if event.Delta != nil {
            fmt.Print(event.Delta.Text)
        }
    case "message_delta":
        if event.Usage != nil {
            fmt.Printf("\nTokens: %d\n", event.Usage.OutputTokens)
        }
    case "error":
        return fmt.Errorf("stream error: %s", event.Error.Message)
    }
    return nil
}

api.ProcessStream(ctx, eventChan, handler)
```

## Testing

Run the test suite:

```bash
go test ./internal/harness/api/... -v
```

Run with coverage:

```bash
go test ./internal/harness/api/... -cover
```

## Comparison with TypeScript Version

The Go implementation provides equivalent functionality to the TypeScript version with these differences:

### Simplified
- Removed enterprise-specific features (prompt caching, beta headers, etc.)
- Focused on core API functionality
- Cleaner error handling with Go idioms

### Enhanced
- Thread-safe streaming processors
- Context-aware cancellation
- Idiomatic Go patterns (channels, goroutines)
- Comprehensive test coverage

### Core Features Maintained
- ✅ Streaming and non-streaming requests
- ✅ Retry logic with exponential backoff
- ✅ Error handling
- ✅ Token counting
- ✅ Request validation

## Examples

See the `examples/` directory for complete working examples:

- `api_example.go` - Comprehensive usage examples

## License

MIT
