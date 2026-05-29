# Provider Module

The provider module provides a unified interface for multiple LLM providers (Anthropic Claude, OpenAI GPT, Qwen, Google Gemini, ShortAPI).

## Supported Providers

### 1. Anthropic Claude
- **Provider name**: `anthropic` or `claude`
- **Default base URL**: `https://api.anthropic.com`
- **Authentication**: API Key
- **Supported models**:
  - claude-opus-4
  - claude-sonnet-4-5
  - claude-sonnet-3-5
  - claude-haiku-3-5
  - claude-3-opus-20240229
  - claude-3-sonnet-20240229
  - claude-3-haiku-20240307

### 2. OpenAI GPT
- **Provider name**: `openai` or `gpt`
- **Default base URL**: `https://api.openai.com/v1`
- **Authentication**: API Key (Bearer token)
- **Supported models**:
  - gpt-4
  - gpt-4-turbo
  - gpt-4-turbo-preview
  - gpt-4-0125-preview
  - gpt-4-1106-preview
  - gpt-4o
  - gpt-4o-mini
  - gpt-3.5-turbo
  - gpt-3.5-turbo-16k

### 3. Qwen / DashScope
- **Provider name**: `qwen`, `dashscope`, or `aliyun`
- **Default base URL**: `https://dashscope.aliyuncs.com/compatible-mode/v1`
- **Authentication**: API Key (Bearer token)
- **Supported models**:
  - qwen-plus
  - qwen-max
  - qwen-flash
  - qwen-turbo
  - qwen3.5-plus
  - qwen3.5-max
  - qwen3.5-flash

### 4. Google Gemini
- **Provider name**: `gemini` or `google`
- **Default base URL**: `https://generativelanguage.googleapis.com/v1beta`
- **Authentication**: API Key (query parameter)
- **Supported models**:
  - gemini-pro
  - gemini-pro-vision
  - gemini-1.5-pro
  - gemini-1.5-flash
  - gemini-2.0-flash-exp

### 5. ShortAPI
- **Provider name**: `shortapi` or `short`
- **Default base URL**: `https://api.shortapi.ai/v1`
- **Authentication**: API Key (Bearer token)
- **Default model**: `google/gemini-3.1-pro-preview`
- **Protocol**: OpenAI-compatible chat completions

## Configuration

### Using config.json

```json
{
  "provider": "anthropic",
  "model": "claude-sonnet-4-5",
  "api_key": "your-api-key-here",
  "api_base_url": "https://api.anthropic.com",
  "timeout_seconds": 600
}
```

### Using CLI commands

```bash
# Set provider
claude /config set provider anthropic

# Set API key
claude /config set api_key sk-ant-xxxxx

# Set model
claude /config set model claude-sonnet-4-5

# Set base URL (optional, for custom endpoints)
claude /config set api_base_url https://api.anthropic.com
```

### Alternative: Using api_token

For providers that use tokens instead of API keys:

```json
{
  "provider": "openai",
  "model": "gpt-4o",
  "api_token": "your-token-here"
}
```

Or via CLI:
```bash
claude /config set api_token your-token-here
```

## Usage Examples

### Example 1: Anthropic Claude

```json
{
  "provider": "anthropic",
  "model": "claude-sonnet-4-5",
  "api_key": "sk-ant-xxxxx",
  "api_base_url": "https://api.anthropic.com"
}
```

### Example 2: OpenAI GPT

```json
{
  "provider": "openai",
  "model": "gpt-4o",
  "api_key": "sk-xxxxx",
  "api_base_url": "https://api.openai.com/v1"
}
```

### Example 3: Google Gemini

```json
{
  "provider": "gemini",
  "model": "gemini-1.5-pro",
  "api_key": "AIzaSyxxxxx",
  "api_base_url": "https://generativelanguage.googleapis.com/v1beta"
}
```

### Example 4: Custom OpenAI-compatible endpoint

```json
{
  "provider": "openai",
  "model": "custom-model",
  "api_key": "your-key",
  "api_base_url": "http://localhost:8080/v1"
}
```

### Example 5: ShortAPI

```json
{
  "provider": "shortapi",
  "model": "google/gemini-3.1-pro-preview",
  "api_key": "your-shortapi-key",
  "api_base_url": "https://api.shortapi.ai/v1"
}
```

## Environment Variables

You can also set credentials via environment variables:

```bash
# For Anthropic
export ANTHROPIC_API_KEY=sk-ant-xxxxx

# For OpenAI
export OPENAI_API_KEY=sk-xxxxx

# For Gemini
export GEMINI_API_KEY=AIzaSyxxxxx

# For ShortAPI
export SHORTAPI_KEY=your-shortapi-key
```

## Programmatic Usage

```go
import "claude-codex/internal/provider"

// Create a provider factory
factory := provider.NewFactory()

// Create provider configuration
cfg := provider.Config{
    Provider: "anthropic",
    APIKey:   "sk-ant-xxxxx",
    Model:    "claude-sonnet-4-5",
    BaseURL:  "https://api.anthropic.com",
    Timeout:  600,
}

// Validate configuration
if err := factory.ValidateConfig(cfg); err != nil {
    log.Fatal(err)
}

// Create provider
p, err := factory.CreateProvider(cfg)
if err != nil {
    log.Fatal(err)
}

// Create message request
req := provider.MessageRequest{
    Model: cfg.Model,
    Messages: []provider.Message{
        {
            Role:    "user",
            Content: "Hello, how are you?",
        },
    },
    MaxTokens:   1000,
    Temperature: 0.7,
    System:      "You are a helpful assistant",
}

// Send request
resp, err := p.CreateMessage(context.Background(), req)
if err != nil {
    log.Fatal(err)
}

// Process response
fmt.Println(resp.Content[0].Text)
```

## Switching Between Providers

You can easily switch between providers by changing the configuration:

```bash
# Switch to OpenAI
claude /config set provider openai
claude /config set model gpt-4o
claude /config set api_key sk-xxxxx

# Switch to Gemini
claude /config set provider gemini
claude /config set model gemini-1.5-pro
claude /config set api_key AIzaSyxxxxx

# Switch back to Anthropic
claude /config set provider anthropic
claude /config set model claude-sonnet-4-5
claude /config set api_key sk-ant-xxxxx
```

## Custom Base URLs

All providers support custom base URLs for:
- Local development servers
- Proxy servers
- Custom API gateways
- Self-hosted models

Example:
```bash
claude /config set api_base_url http://localhost:8080/v1
```

## Error Handling

The provider module provides unified error handling across all providers:

```go
resp, err := provider.CreateMessage(ctx, req)
if err != nil {
    // Handle errors uniformly regardless of provider
    log.Printf("API error: %v", err)
    return
}
```

## Testing

Run provider tests:

```bash
go test ./internal/provider/...
```

## Notes

- All providers use the same unified `MessageRequest` and `MessageResponse` types
- Authentication methods are automatically handled based on provider type
- Base URLs default to official endpoints but can be customized
- Timeout defaults to 600 seconds but can be configured
- The factory pattern makes it easy to add new providers in the future
