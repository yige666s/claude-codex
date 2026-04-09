# Web UI Quick Start

A web-based chat interface for the Claude Agent harness.

## Run

```bash
# 1. Build
go build -o webui ./cmd/webui

# 2. Set API key
export ANTHROPIC_API_KEY="your-key"

# 3. Start server
./webui

# 4. Open browser
open http://localhost:8080
```

## Architecture

```
Browser ──WebSocket──▶ Go Server ──▶ Harness Engine ──▶ Anthropic API
                           │
                           └──▶ Tool Registry
```

The web UI directly integrates the harness engine, providing the same agent capabilities as the CLI but through a web interface.

## Documentation

See `internal/ui/web/README.md` for detailed documentation.
