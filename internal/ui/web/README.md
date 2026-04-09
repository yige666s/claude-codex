# Web UI for Claude Agent

A simple web interface for the Claude Agent harness framework.

## Features

- Real-time chat interface using WebSocket
- Session management
- Streaming responses
- Clean, modern UI
- Direct integration with Go harness engine
- **Skills system** - Load and execute custom skills from `.md` files
- **Slash commands** - Use `/skills` to list available skills, `/skillname` to invoke

## Architecture

```
Browser (WebSocket) ──▶ Go HTTP Server ──▶ Harness Engine ──▶ Anthropic API
                                              ├── Tools
                                              ├── State Management
                                              └── Permissions
```

## Quick Start

### 1. Build the Web UI server

```bash
cd claude-go
go build -o webui ./cmd/webui
```

### 2. Set your API credentials

```bash
export ANTHROPIC_API_KEY="your-api-key-here"
# Or use API token
export ANTHROPIC_API_TOKEN="your-token-here"

# Optional: custom API base URL
export ANTHROPIC_BASE_URL="https://api.anthropic.com"
```

### 3. Run the server

```bash
./webui
```

Or specify custom options:

```bash
./webui -addr :3000 -model claude-opus-4-6 -api-base-url https://custom.api.com
```

### 4. Open in browser

Navigate to `http://localhost:8080` (or your custom port)

## Usage

1. Type your message in the input box
2. Press Enter or click Send
3. The assistant will respond using the harness engine
4. All conversations are maintained in sessions

### Using Skills

Skills are custom commands that extend the assistant's capabilities:

- Type `/skills` to list all available skills
- Type `/skillname args` to invoke a skill (e.g., `/hello John`)
- Skills are loaded from:
  - `~/.claude/skills/` (user global skills)
  - `./.claude/skills/` (project-specific skills)

### Creating Custom Skills

Create a `.md` file in `.claude/skills/`:

```markdown
---
name: "My Skill"
description: "What this skill does"
user_invocable: true
arguments: ["arg1", "arg2"]
allowed_tools: ["bash", "read"]
---

# Skill Content

Your instructions for Claude go here.

You can use {{arg1}} and {{arg2}} placeholders.
```

## Configuration

### Command-line flags

- `-addr` - Server address (default: `:8080`)
- `-api-key` - API key (or use `ANTHROPIC_API_KEY` env var)
- `-api-token` - API token (alternative to api-key, or use `ANTHROPIC_API_TOKEN` env var)
- `-api-base-url` - API base URL (or use `ANTHROPIC_BASE_URL` env var, default: `https://api.anthropic.com`)
- `-model` - Model to use (default: `claude-sonnet-4-5`)

### Adding Tools

Edit `cmd/webui/main.go` to register additional tools:

```go
registry := tools.NewRegistry()

// Add tools
registry.Register(bash.New())
registry.Register(file.NewRead())
registry.Register(file.NewWrite())
// ... more tools
```

## Project Structure

```
internal/ui/web/
├── server/
│   └── server.go       # WebSocket server and harness integration
├── static/
│   └── index.html      # Frontend UI
└── README.md           # This file

cmd/webui/
└── main.go             # Entry point
```

## API Protocol

### WebSocket Messages

**Client → Server:**
```json
{
  "type": "chat",
  "content": "Your message here"
}
```

**Server → Client:**
```json
{
  "type": "message",
  "role": "user|assistant",
  "content": "Message content"
}
```

```json
{
  "type": "error",
  "error": "Error message"
}
```

```json
{
  "type": "done"
}
```

## Development

### Prerequisites

- Go 1.21+
- Anthropic API key

### Testing

```bash
# Run the server
go run ./cmd/webui -api-key your-key

# Open http://localhost:8080 in your browser
```

## Implementation Notes

- Uses `ModeBypass` for permissions (auto-approves all tool calls)
- Sessions are stored in memory (not persisted)
- WebSocket protocol is simple JSON messages
- Static HTML file is served from `internal/ui/web/static/`

## Future Enhancements

- [ ] Streaming token-by-token responses
- [ ] Tool execution visualization
- [ ] Multi-session management UI
- [ ] File upload support
- [ ] Export conversation history
- [ ] Dark/light theme toggle
- [ ] Markdown rendering for responses
- [ ] Code syntax highlighting
- [ ] Session persistence
- [ ] Authentication

## License

Same as the main project.
