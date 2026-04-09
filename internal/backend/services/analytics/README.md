# Analytics Service

The analytics service provides event logging and tracking for the Claude Code Go implementation.

## Features

### 1. Event Logging
- **Synchronous logging**: Immediate event logging
- **Asynchronous logging**: Non-blocking event logging
- **Event queueing**: Queue events before sink attachment
- **Automatic draining**: Drain queued events when sink attaches

### 2. Event Sampling
- **Configurable sampling**: Per-event sampling rates
- **Sample rate metadata**: Automatic sample rate annotation
- **Default sampling**: 100% sampling by default
- **Dynamic updates**: Update sampling rates at runtime

### 3. Privacy Protection
- **PII field stripping**: Remove _PROTO_ prefixed fields
- **Tool name sanitization**: Redact MCP tool names
- **Analytics disable**: Respect privacy settings
- **Environment checks**: Disable in test/3P environments

### 4. Sink Management
- **Pluggable sinks**: Support multiple analytics backends
- **Lazy initialization**: Attach sink after startup
- **Queue management**: Bounded queue with overflow handling
- **Idempotent attachment**: Safe to call multiple times

## Core Components

### types.go
Defines core types and interfaces:
- `EventMetadata` - Event metadata map
- `Event` - Queued event structure
- `Sink` - Analytics backend interface
- `Config` - Analytics configuration
- Environment check functions

### service.go
Service implementation:
- `Service` - Main analytics service
- `AttachSink()` - Attach analytics backend
- `LogEvent()` - Log event synchronously
- `LogEventAsync()` - Log event asynchronously
- `StripProtoFields()` - Remove PII-tagged fields
- `SanitizeToolName()` - Sanitize tool names
- `IsAnalyticsDisabled()` - Check if analytics disabled

### sampling.go
Sampling logic:
- `shouldSample()` - Determine if event should be sampled
- `CheckSampling()` - Check sampling for event
- `UpdateSamplingRates()` - Update sampling configuration
- `GetSamplingRates()` - Get current sampling rates

## Configuration

### Analytics Config

```go
type Config struct {
    Disabled          bool                   // Disable all analytics
    DatadogEnabled    bool                   // Enable Datadog backend
    FirstPartyEnabled bool                   // Enable 1P logging
    SamplingRates     map[string]float64     // Per-event sampling rates
}
```

### Environment Checks

Analytics is automatically disabled when:
- Running in test environment
- Using Bedrock provider
- Using Vertex provider
- Using Foundry provider
- Telemetry is disabled by user

## Usage Examples

### Create Service

```go
import "github.com/ding/claude-code/claude-go/internal/services/analytics"

config := &analytics.Config{
    Disabled:       false,
    SamplingRates: map[string]float64{
        "high_volume_event": 0.1,  // 10% sampling
        "normal_event":      1.0,  // 100% sampling
    },
}

service := analytics.NewService(config)
```

### Attach Sink

```go
// Implement the Sink interface
type MySink struct{}

func (s *MySink) LogEvent(eventName string, metadata analytics.EventMetadata) {
    // Send to analytics backend
}

func (s *MySink) LogEventAsync(eventName string, metadata analytics.EventMetadata) error {
    // Send to analytics backend asynchronously
    return nil
}

// Attach sink
sink := &MySink{}
service.AttachSink(sink)
```

### Log Events

```go
// Synchronous logging
service.LogEvent("user_login", analytics.EventMetadata{
    "user_id": "123",
    "method":  "oauth",
})

// Asynchronous logging
err := service.LogEventAsync("api_call", analytics.EventMetadata{
    "endpoint": "/api/messages",
    "duration": 150,
})
```

### Event Queueing

```go
// Events logged before sink attachment are queued
service.LogEvent("early_event", analytics.EventMetadata{})

// Attach sink later - queued events are drained automatically
service.AttachSink(sink)
```

### Sampling Configuration

```go
// Update sampling rates
service.UpdateSamplingRates(map[string]float64{
    "frequent_event": 0.01,  // 1% sampling
    "rare_event":     1.0,   // 100% sampling
})

// Get current rates
rates := service.GetSamplingRates()
```

### Privacy Protection

```go
// Strip PII-tagged fields
metadata := analytics.EventMetadata{
    "user_id":       "123",
    "_PROTO_email":  "user@example.com",  // Will be stripped
    "_PROTO_name":   "John Doe",          // Will be stripped
}

cleaned := analytics.StripProtoFields(metadata)
// cleaned only contains "user_id"

// Sanitize tool names
toolName := analytics.SanitizeToolName("mcp__server__tool")
// Returns "mcp_tool"

builtInTool := analytics.SanitizeToolName("Bash")
// Returns "Bash"
```

### Check Analytics Status

```go
if analytics.IsAnalyticsDisabled() {
    // Analytics is disabled, skip logging
    return
}

service.LogEvent("event_name", metadata)
```

## Constants

### Queue Management
- `MaxQueueSize`: 1000 events
- `DefaultSampleRate`: 1.0 (100%)

## Event Metadata

Event metadata is a map of key-value pairs:
- Keys: string
- Values: any JSON-serializable type
- Special keys:
  - `sample_rate`: Automatically added when sampled
  - `_PROTO_*`: PII-tagged fields (stripped for general backends)

## Sampling

### How Sampling Works

1. Check if event has configured sample rate
2. If no rate configured, use default (100%)
3. Generate random number between 0 and 1
4. If random < sample rate, log the event
5. Add `sample_rate` to metadata if sampled

### Sample Rate Interpretation

- `1.0`: Log 100% of events
- `0.5`: Log 50% of events
- `0.1`: Log 10% of events
- `0.0`: Log 0% of events (disabled)

## Privacy Features

### PII Field Stripping

Fields prefixed with `_PROTO_` are considered PII-tagged and are:
- Stripped before sending to general-access backends (Datadog)
- Kept for privileged backends (1P logging with access controls)

### Tool Name Sanitization

MCP tool names reveal user-specific server configurations:
- Format: `mcp__<server>__<tool>`
- Sanitized to: `mcp_tool`
- Built-in tools (Bash, Read, Write) are not sanitized

## Testing

Run tests:
```bash
go test ./internal/services/analytics/...
```

Run with coverage:
```bash
go test -cover ./internal/services/analytics/...
```

## Integration

The analytics service integrates with:
- Datadog for metrics and events
- First-party event logging for internal analytics
- Privacy settings for telemetry control
- Environment detection for automatic disabling

## Performance Characteristics

- **Event queueing**: O(1) append, bounded at 1000 events
- **Queue draining**: Asynchronous, non-blocking
- **Sampling**: O(1) random number generation
- **Metadata stripping**: O(n) where n is number of keys
- **Sink attachment**: Idempotent, thread-safe

## Thread Safety

All public methods are thread-safe:
- `AttachSink()`: Protected by mutex
- `LogEvent()`: Protected by RWMutex
- `LogEventAsync()`: Protected by RWMutex
- `UpdateSamplingRates()`: Protected by mutex
- `GetSamplingRates()`: Protected by RWMutex

## Error Handling

- Sink errors are logged but don't fail the operation
- Queue overflow drops oldest events
- Sampling failures default to logging the event
- Missing sink queues events for later delivery

## Best Practices

1. **Attach sink early**: Call `AttachSink()` during app initialization
2. **Use async for high-volume**: Use `LogEventAsync()` for frequent events
3. **Configure sampling**: Set appropriate sample rates for high-volume events
4. **Sanitize PII**: Use `_PROTO_` prefix for PII fields
5. **Check disabled state**: Respect `IsAnalyticsDisabled()` for privacy

## Next Steps

The analytics service is complete. All services in the services layer have been refactored to Go.
