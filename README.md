# claude-codex

> `claude-codex` is a Go rewrite of the core capabilities from `claude-code` (TypeScript).  
> Current status: **runnable and testable, but not yet fully feature-parity with the TS version**.

---

## Table of Contents

- [Overview](#overview)
- [Usability Baseline](#usability-baseline)
- [Quickstart](#quickstart)
- [Configuration Paths and Meanings](#configuration-paths-and-meanings)
- [Refactoring Status by Module](#refactoring-status-by-module)
- [TUI vs WebUI](#tui-vs-webui)
- [Notes](#notes)
- [Common Commands](#common-commands)
- [License](#license)

---

## Overview

| Item | Description |
|---|---|
| Repository | `claude-codex` |
| TS reference source | `claude-code/src` |
| Go version | `1.24.4` |
| Main dependencies | Cobra, Bubble Tea, Gorilla WebSocket |
| Entrypoints | `cmd/tui`, `cmd/webui` |

### Refactor Positioning

- The Go implementation uses a layered architecture: `app / backend / harness / public / ui`.
- The TS implementation remains much larger, especially in `commands`, `components`, `tools`, and `utils`.
- The Go side already has core capabilities, but some areas are still under active completion.

### Current Incomplete Signals (Observed)

- Around **117** TODO/FIXME markers under `internal`.
- Around **65** of them are in `internal/harness/query` + `internal/harness/queryengine`.

> Conclusion: suitable for development validation, gradual rollout, and continuous refactoring; not recommended yet as a final full replacement for TS.

---

## Usability Baseline

Verified in this repository:

- ✅ `go test ./...`
- ✅ `go build ./cmd/tui && go build ./cmd/webui`
- ✅ `go run ./cmd/tui --help`
- ✅ `go run ./cmd/tui /help`
- ✅ `go run ./cmd/webui -h`

> Note: **passing tests does not equal full feature parity**. Please evaluate usage scope together with the refactoring and notes sections below.

---

## Quickstart

### 1) Check environment

```bash
go version
```

Recommended: keep it aligned with `go.mod`.

### 2) Start TUI/CLI

```bash
cd claude-codex
go run ./cmd/tui --help
```

Common slash commands:

```bash
go run ./cmd/tui /help
go run ./cmd/tui /model
go run ./cmd/tui /limits
```

Or:

```bash
make run-tui
```

### 3) Start WebUI

```bash
cd claude-codex
export ANTHROPIC_API_KEY="your-api-key"
go run ./cmd/webui -addr :8080 -model claude-sonnet-4-6
```

Open in browser: `http://localhost:8080`

Or:

```bash
make run-webui
```

### 4) Regression checks

```bash
cd claude-codex
go test ./...
```

---

## Configuration Paths and Meanings

`claude-codex` currently supports two configuration layers: global + workspace.

- Global config: `~/.claude-codex/config.json`
- Custom global home: set `CLAUDE_GO_HOME`; effective path is `${CLAUDE_GO_HOME}/config.json`
- Workspace config: `<your-project>/.claude-codex/config.json`

When workspace config exists, same-name fields override the global ones (currently including model, permission mode, theme, timeout, max turns, telemetry, OAuth, MCP, etc.).

### Field Meanings (`config.json`)

| Field | Meaning |
|---|---|
| `schema_version` | Config schema version; normalized/migrated automatically |
| `backend` | Backend type (supports `anthropic` / `openai` protocol) |
| `provider` | LLM provider (for example `anthropic` / `openai`) |
| `model` | Default model name |
| `permission_mode` | Permission mode: `default` / `plan` / `bypass` / `auto` |
| `theme` | UI theme: `dark` or `light` |
| `api_base_url` | API base URL |
| `api_key` / `api_token` | API credentials (prefer env vars; never commit) |
| `timeout_seconds` | Request timeout in seconds |
| `max_turns` | Max turns in one session (minimum 1) |
| `secret_store` | Secret storage mode: `auto` / `plaintext` / `keychain` |
| `plugin_dir` | Plugin directory path |
| `bridge_secret` | Bridge authentication secret |
| `telemetry.enabled` | Whether telemetry is enabled |
| `telemetry.exporter` | Telemetry exporters (comma-separated) |
| `telemetry.endpoint` | Telemetry endpoint |
| `telemetry.insecure` | Whether insecure telemetry transport is allowed |
| `telemetry.service_name` | Telemetry service name (default `claude-codex`) |
| `oauth.client_id` / `oauth.client_secret` | OAuth client credentials |
| `oauth.auth_url` / `oauth.token_url` | OAuth authorize/token endpoints |
| `oauth.scopes` | OAuth scopes |
| `oauth.redirect_host` / `oauth.redirect_port` | OAuth redirect listen address |
| `mcp_servers` | MCP server list |

---

## Refactoring Status by Module

### Areas with solid usable baseline

- `internal/harness/*`: core framework capabilities such as agent, engine, tools, state, skills
- `internal/backend/services/*`: analytics, api, tokens, tools, oauth, and related services
- `internal/app/cli/*`: main CLI and a set of slash commands
- `internal/ui/web/server`: Web-side server entry

### Areas still under active refactoring

- `Query / QueryEngine`: TODO-dense and currently the largest incomplete source
- Some tools and edge features: directories exist, but behavior/coverage is still being filled in
- Capability mapping against large TS `utils` is not fully converged yet

---

## TUI vs WebUI

| Dimension | TUI (`cmd/tui`) | WebUI (`cmd/webui`) |
|---|---|---|
| Interaction style | Terminal CLI / TUI | Browser + HTTP/WebSocket |
| Core tech | Cobra + Bubble Tea | Web server + `/ws` |
| Typical usage | Local development, scripting workflows | Visual chat, demos, integration testing |
| Session profile | CLI-oriented workflow | Mostly in-memory session flow currently |
| Permission strategy | Follows CLI runtime config | Includes `ModeBypass` in current implementation (development-oriented) |
| Risk point | Command coverage still being filled | Security/permission hardening needed before production |

---

## Notes

1. This is a project under active refactoring, not a final completed state.  
2. Before production use, harden WebUI permissions and network exposure first.  
3. Inject API keys via environment variables only; avoid hardcoding/committing.  
4. After every change, at minimum run: `go test ./...`.  
5. Executables named `tui`/`webui` in repo root are binaries; they are different from source entrypoints `cmd/tui`/`cmd/webui`.

---

## Common Commands

```bash
make fmt         # Format code
make test        # Run tests
make run-tui     # Start TUI
make run-webui   # Start WebUI
make clean       # Clean binaries
```

---

## License

This project is licensed under the [MIT License](./LICENSE).

---
