# Provider Configuration Examples

This directory contains example configurations for different LLM providers.

## Anthropic Claude (Default)

```json
{
  "provider": "anthropic",
  "model": "claude-sonnet-4-5",
  "api_key": "sk-ant-xxxxx",
  "api_base_url": "https://api.anthropic.com",
  "timeout_seconds": 600
}
```

**Available models:**
- claude-opus-4
- claude-sonnet-4-5
- claude-sonnet-3-5
- claude-haiku-3-5

**Get API key:** https://console.anthropic.com/

## OpenAI GPT

```json
{
  "provider": "openai",
  "model": "gpt-4o",
  "api_key": "sk-xxxxx",
  "api_base_url": "https://api.openai.com/v1",
  "timeout_seconds": 600
}
```

**Available models:**
- gpt-4o
- gpt-4o-mini
- gpt-4-turbo
- gpt-4
- gpt-3.5-turbo

**Get API key:** https://platform.openai.com/api-keys

## Google Gemini

```json
{
  "provider": "gemini",
  "model": "gemini-1.5-pro",
  "api_key": "AIzaSyxxxxx",
  "api_base_url": "https://generativelanguage.googleapis.com/v1beta",
  "timeout_seconds": 600
}
```

**Available models:**
- gemini-1.5-pro
- gemini-1.5-flash
- gemini-2.0-flash-exp
- gemini-pro

**Get API key:** https://makersuite.google.com/app/apikey

## Using CLI to Configure

### Set Provider and Model

```bash
# Anthropic Claude
claude /config set provider anthropic
claude /config set model claude-sonnet-4-5
claude /config set api_key sk-ant-xxxxx

# OpenAI GPT
claude /config set provider openai
claude /config set model gpt-4o
claude /config set api_key sk-xxxxx

# Google Gemini
claude /config set provider gemini
claude /config set model gemini-1.5-pro
claude /config set api_key AIzaSyxxxxx
```

### View Current Configuration

```bash
claude /config show
```

### Custom Base URL

For local development or custom endpoints:

```bash
claude /config set api_base_url http://localhost:8080/v1
```

## Environment Variables

You can also use environment variables (recommended for security):

```bash
# Set in your shell profile (~/.bashrc, ~/.zshrc, etc.)
export ANTHROPIC_API_KEY=sk-ant-xxxxx
export OPENAI_API_KEY=sk-xxxxx
export GEMINI_API_KEY=AIzaSyxxxxx
```

## Security Best Practices

1. **Never commit API keys to version control**
2. Use environment variables for API keys
3. Use `.gitignore` to exclude config files with keys
4. Rotate API keys regularly
5. Use separate keys for development and production

## Switching Between Providers

You can easily switch between providers without losing your configuration:

```bash
# Switch to OpenAI
claude /config set provider openai
claude /config set model gpt-4o

# Switch back to Anthropic
claude /config set provider anthropic
claude /config set model claude-sonnet-4-5
```

The API keys for each provider are stored separately, so you don't need to re-enter them when switching.
