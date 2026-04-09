# API Service Refactoring Progress

## Status: ✅ COMPLETED

The API service has been successfully refactored from TypeScript to Go.

## Files Created

### Core Implementation
- `types.go` - Type definitions, constants, and error types
- `errors.go` - Error detection, classification, and formatting
- `retry.go` - Retry logic with exponential backoff
- `client.go` - Multi-provider API client creation
- `usage.go` - API utilization and rate limit tracking

### Testing & Documentation
- `api_test.go` - Comprehensive test suite
- `README.md` - Complete documentation

## Test Results

All tests passing:
```
✅ TestParsePromptTooLongTokenCounts
✅ TestGetPromptTooLongTokenGap
✅ TestIsMediaSizeError
✅ TestIs529Error
✅ TestClassifyAPIError
✅ TestCalculateBackoff
✅ TestRetryStateShouldRetry
✅ TestShouldRetry529
✅ TestWithRetry
✅ TestUtilizationMethods
```

## Features Implemented

### 1. Multi-Provider Support
- ✅ First Party (Anthropic API)
- ✅ AWS Bedrock configuration
- ✅ Vertex AI configuration
- ✅ Azure Foundry configuration
- ✅ Provider detection from environment

### 2. Retry Logic
- ✅ Exponential backoff calculation
- ✅ Circuit breaker for 529 errors
- ✅ Query source-based retry policies
- ✅ Persistent retry mode
- ✅ Retry state tracking
- ✅ Model fallback support

### 3. Error Handling
- ✅ Error classification system
- ✅ Prompt-too-long detection
- ✅ Token count parsing
- ✅ Media size error detection
- ✅ 529/429 error detection
- ✅ Connection error detection
- ✅ SSL error detection
- ✅ Retry-after header extraction

### 4. Usage Tracking
- ✅ Utilization fetching
- ✅ Rate limit tracking
- ✅ Multiple time window support
- ✅ Extra usage credit tracking
- ✅ Warning threshold detection
- ✅ Utilization formatting

### 5. Client Management
- ✅ Client creation with configuration
- ✅ Message creation with retry
- ✅ Message creation with fallback
- ✅ Provider-specific configuration
- ✅ Timeout management

## Constants Defined

### Retry Behavior
- `DefaultMaxRetries`: 10
- `FloorOutputTokens`: 3000
- `Max529Retries`: 3
- `BaseDelayMS`: 500ms
- `PersistentMaxBackoffMS`: 5 minutes
- `PersistentResetCapMS`: 6 hours
- `HeartbeatIntervalMS`: 30 seconds
- `DefaultFastModeFallbackMS`: 30 minutes
- `ShortRetryThresholdMS`: 20 seconds
- `MinCooldownMS`: 10 minutes

### Error Messages
- `APIErrorMessagePrefix`: "API Error"
- `PromptTooLongErrorMessage`: "Prompt is too long"
- `CreditBalanceTooLowErrorMessage`: "Credit balance is too low"
- `InvalidAPIKeyErrorMessage`: "Not logged in · Please run /login"
- `Repeated529ErrorMessage`: "API is experiencing high demand"

## Environment Variables Supported

### Authentication
- `ANTHROPIC_API_KEY` - Anthropic API key
- `CLAUDE_CODE_API_KEY` - Alternative API key

### AWS Bedrock
- `AWS_REGION` / `AWS_DEFAULT_REGION` - AWS region
- `ANTHROPIC_SMALL_FAST_MODEL_AWS_REGION` - Region override for Haiku

### Azure Foundry
- `ANTHROPIC_FOUNDRY_RESOURCE` - Azure resource name
- `ANTHROPIC_FOUNDRY_BASE_URL` - Alternative base URL
- `ANTHROPIC_FOUNDRY_API_KEY` - Foundry API key

### Vertex AI
- `ANTHROPIC_VERTEX_PROJECT_ID` - GCP project ID
- `CLOUD_ML_REGION` - Default GCP region
- Model-specific region overrides

### Retry Behavior
- `CLAUDE_CODE_MAX_RETRIES` - Override max retries
- `CLAUDE_CODE_UNATTENDED_RETRY` - Enable persistent retry

## Error Classification

Implemented error types:
- `ErrorClassRateLimit` - 529/429 errors
- `ErrorClassAuthenticationFailed` - 401/403 errors
- `ErrorClassServerError` - 5xx errors
- `ErrorClassConnectionError` - Connection failures
- `ErrorClassSSLCertError` - SSL/TLS errors
- `ErrorClassInvalidRequest` - 400 errors
- `ErrorClassUnknown` - Unclassified errors

## Query Sources

Foreground sources (retry on 529):
- `repl_main_thread` - Main REPL queries
- `sdk` - SDK queries
- `agent:*` - Agent queries
- `compact` - Compaction operations
- `verification_agent` - Verification queries
- `auto_mode` - Auto-mode classifiers

## API Integration

The service integrates with:
- `pkg/anthropic` - Anthropic API client
- Message request/response types
- Rate limit tracking
- Token counting

## Simplified vs TypeScript

### Fully Ported
- Retry logic with exponential backoff
- Error classification and detection
- Multi-provider support
- Usage tracking
- Rate limit monitoring
- Circuit breaker pattern
- Query source-based policies

### Simplified
- OAuth token management (basic structure)
- Fast mode handling (placeholder)
- Hook integration (handled elsewhere)

### Not Ported (Handled Elsewhere)
- Analytics events
- Session state management
- Hook system integration
- Detailed OAuth flow

## Next Steps

The API service is complete and ready for integration. Next service to refactor: **tools service**

## Integration Points

The API service will be used by:
1. Main query loop for API calls
2. Compact service for compaction
3. Agent system for delegated queries
4. Command handlers for user requests
5. Background tasks for async operations

## Performance Characteristics

- **Retry latency**: 500ms base, exponential backoff
- **Max backoff**: 5 minutes (persistent mode)
- **Circuit breaker**: 3 consecutive 529 errors
- **Utilization check**: 5 second timeout
- **Default timeout**: 30 seconds per request
- **Max retries**: 10 (configurable)
