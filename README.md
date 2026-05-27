# claude-codex

This repository contains two related projects:

1. **AgentAPI**: the primary product. It is a consumer-facing agent workspace with a Go backend, a React web frontend, a production runtime, and deployment assets.
2. **claudecode Go refactor**: the secondary project. It ports Claude Code's TypeScript CLI/TUI, orchestration, tools, runtime, and platform capabilities to Go.

Chinese documentation is available in [README_zh.md](README_zh.md).

---

## Part 1: AgentAPI, the Primary Project

AgentAPI is the main product surface in this repository. It combines the `cmd/agentapi` Go backend and the `apps/web` React frontend to provide authenticated agent workspaces, chat sessions, long-running jobs, skills, attachments, generated artifacts, memory, admin operations, audit logs, risk controls, and production deployment assets.

### Current Positioning

- **Backend entrypoint**: `cmd/agentapi`
- **Frontend entrypoint**: `apps/web`
- **Core backend runtime**: `internal/backend/agentruntime`
- **Production assembly layer**: `internal/backend/agentapi`
- **Default database**: Postgres
- **Cache and rate limiting**: Redis
- **Object storage**: S3-compatible storage, with Cloudflare R2 used by the production template
- **Default LLM provider**: Vertex AI, while the runtime keeps a multi-provider abstraction
- **Deployment model**: Docker Compose, with local and production examples in this repository

### Core Capabilities

- User registration, login, refresh tokens, email verification, logout, and account deletion.
- Session lists, chat transcripts, SSE streaming, reconnect support, and event replay.
- Long-running jobs, cancellation, job status, and timeline events.
- Attachment upload, download, delete, preview, and message attachment sending.
- Generated artifact registration, listing, preview, download, and deletion.
- Skill registry, skill browsing, execution history, policy controls, and admin management.
- Memory extraction, organization, scoring, maintenance, deletion, export, and user controls.
- Admin console for users, skills, sessions, jobs, artifacts, health, cost, audit, and risk review.
- Production infrastructure integrations with Postgres, Redis, Kafka, S3/R2, Qdrant/Elastic, and Prometheus.

### Repository Layout

| Path | Purpose |
| --- | --- |
| `cmd/agentapi` | AgentAPI backend binary entrypoint. |
| `apps/web` | React/Vite product frontend and `/admin` operations console. |
| `internal/backend/agentapi` | AgentAPI configuration, dependency wiring, runtime lifecycle, and workers. |
| `internal/backend/agentruntime` | Core web agent runtime for HTTP APIs, WebSockets, sessions, jobs, memory, skills, artifacts, admin, and risk APIs. |
| `internal/backend/services` | Product services such as analytics, api, compact, context, cost, history, mcp, oauth, tasks, tokens, tools, voice, and x402. |
| `internal/backend/googleauth` | Google service-account helper for Vertex AI access tokens. |
| `.claude/skills` | Local skills that AgentAPI can load. |
| `deploy/local` | Local Docker Compose development stack. |
| `deploy/production` | Production deployment examples, frontend nginx config, release, sizing, and backup/restore docs. |
| `docs/api/agentapi.openapi.yaml` | AgentAPI OpenAPI description. |

### Technology Stack

- **Backend**: Go 1.24.4, chi, gorilla/websocket, pgx, goose, Prometheus client, Redis, Kafka, MinIO S3 client, JWT.
- **Frontend**: React 18, Vite, TypeScript, Tailwind CSS 4, Radix UI, lucide-react, Framer Motion, Vitest, Playwright.
- **Deployment**: Docker Compose, with a path toward separate API/worker services, managed Postgres/Redis/object storage, and Kubernetes-style production deployment.

### Quick Start: Local Full Stack

Docker Compose is the recommended way to run the full AgentAPI stack locally:

```bash
mkdir -p secrets
# Put a Vertex-enabled service account JSON here:
# secrets/vertex-service-account.json

export GOOGLE_APPLICATION_CREDENTIALS="secrets/vertex-service-account.json"
export VERTEX_PROJECT_ID="REPLACE_WITH_GCP_PROJECT"
export VERTEX_LOCATION="us-central1"
export AGENT_API_ARTIFACT_S3_ACCESS_KEY="REPLACE_WITH_R2_ACCESS_KEY_ID"
export AGENT_API_ARTIFACT_S3_SECRET_KEY="REPLACE_WITH_R2_SECRET_ACCESS_KEY"

docker compose -f deploy/local/docker-compose.yml up --build
```

Open:

- Web app: `http://localhost:8080`
- AgentAPI: `http://localhost:8081`
- Health: `http://localhost:8081/healthz`
- Readiness: `http://localhost:8081/readyz`

See [deploy/local/README.md](deploy/local/README.md) for more details.

### Frontend Development

Start the backend first, then run:

```bash
cd apps/web
npm ci
npm run dev
```

The Vite dev server proxies `/v1`, `/healthz`, `/readyz`, and `/metrics` to `http://localhost:8081` by default. Override the backend target with:

```bash
AGENT_API_DEV_TARGET=http://localhost:8082 npm run dev
```

### Backend Without Compose

```bash
go run ./cmd/agentapi \
  -addr :8081 \
  -store-backend sql \
  -sql-driver pgx \
  -sql-dialect postgres \
  -sql-dsn "$AGENT_API_SQL_DSN" \
  -enable-user-system \
  -auth-mode jwt \
  -jwt-secret "$AGENT_API_JWT_SECRET" \
  -llm-provider vertex \
  -model gemini-2.5-flash
```

### Production Configuration

The production environment template is [deploy/production/.env.example](deploy/production/.env.example). Important variable groups:

| Group | Key Variables |
| --- | --- |
| Auth | `AGENT_API_JWT_SECRET`, `AGENT_API_ADMIN_TOKEN`, JWT/session settings |
| Email | `AGENT_API_EMAIL_PROVIDER=resend`, `AGENT_API_RESEND_API_KEY`, `AGENT_API_EMAIL_FROM` |
| Storage | `AGENT_API_ARTIFACT_S3_ENDPOINT`, `AGENT_API_ARTIFACT_S3_ACCESS_KEY`, `AGENT_API_ARTIFACT_S3_SECRET_KEY`, `AGENT_API_ARTIFACT_S3_BUCKET` |
| Vertex | `GOOGLE_APPLICATION_CREDENTIALS`, `VERTEX_PROJECT_ID`, `GOOGLE_CLOUD_PROJECT`, `VERTEX_LOCATION` |
| Runtime | `AGENT_API_SKILL_DIRS`, `AGENT_API_SKILL_POLICY`, `AGENT_API_OPERATION_RATE_LIMITS`, `AGENT_API_RETENTION_DAYS` |
| Frontend | `VITE_AGENT_API_BASE_URL` for split frontend/API origins |

Production should prefer a service account JSON over short-lived access-token environment variables:

```env
AGENT_API_SECRETS_DIR=/opt/agentapi/secrets
GOOGLE_APPLICATION_CREDENTIALS=/run/agentapi/secrets/vertex-service-account.json
VERTEX_PROJECT_ID=your-gcp-project
GOOGLE_CLOUD_PROJECT=your-gcp-project
VERTEX_LOCATION=us-central1
```

### Deployment And Operations

Example Compose deployment on a test server:

```bash
cd /opt/agentapi/repo
docker compose --env-file /opt/agentapi/.env -f deploy/local/docker-compose.yml up -d --build
```

Useful checks:

```bash
curl -fsS http://localhost:8081/healthz
curl -fsS http://localhost:8081/readyz
curl -fsS http://localhost:8081/metrics

docker ps
docker logs --tail=100 claude-codex-agentapi
docker logs --tail=100 claude-codex-agentweb
```

More deployment docs:

- [deploy/production/README.md](deploy/production/README.md)
- [deploy/production/FRONTEND.md](deploy/production/FRONTEND.md)
- [deploy/production/RELEASE_FLOW.md](deploy/production/RELEASE_FLOW.md)
- [deploy/production/SIZING.md](deploy/production/SIZING.md)
- [deploy/production/BACKUP_RESTORE.md](deploy/production/BACKUP_RESTORE.md)

---

## Part 2: claudecode Go Refactor, the Secondary Project

The claudecode Go refactor is the second major workstream in this repository. It ports Claude Code's TypeScript implementation to Go. It is not the primary product entrypoint today, but it provides reusable agent runtime, tools, permissions, skills, memory, MCP, provider, and orchestration capabilities for AgentAPI and local agent development.

### Current Positioning

- **Local TUI entrypoint**: `cmd/tui`
- **Application assembly layer**: `internal/app`
- **Terminal UI**: `internal/ui/tui`
- **Core harness**: `internal/harness`
- **Remote/server boundaries**: `internal/backend/bridge`, `internal/backend/remote`, `internal/backend/upstreamproxy`
- **Public foundation packages**: `internal/public`

The default local execution chain is:

```text
cmd/tui
  -> internal/app/cli
  -> internal/harness/engine
  -> queryRuntime
  -> internal/harness/queryengine
  -> internal/harness/query
```

### Core Module Map

| Module Area | Purpose |
| --- | --- |
| `internal/app/cli` | Cobra command tree, slash commands, mode selection, TUI registry, MCP stdio, bridge continuation, and engine construction. |
| `internal/app/config`, `settings` | Local config, environment overrides, settings schema, MDM/managed settings, and tool permission config. |
| `internal/app/bootstrap`, `entrypoints`, `migrations` | Startup lifecycle, session/worktree/tmux initialization, migrations, and shutdown flow. |
| `internal/app/auth`, `securestorage` | OAuth, trusted device support, keychain/plaintext secret store. |
| `internal/ui/tui` | Bubble Tea TUI with model/update/view, overlays, permission broker, markdown rendering, and command adaptation. |
| `internal/harness/engine` | Local execution facade connecting planner, tool registry, permission checker, session, and query runtime. |
| `internal/harness/queryengine`, `query` | TS-aligned query runtime for the turn loop, model calls, tool execution, budgets, auto compact, and SDK-shaped events. |
| `internal/harness/tools`, `tool` | Tool registry, simple tools, rich tools, ToolUseContext, StreamingExecutor, and tool families such as bash/file/search/web/mcp/lsp/tasks/team/worktree. |
| `internal/harness/permissions` | default/plan/bypass/auto permission modes, rule parsing, approval requests, and TUI/bridge decision channels. |
| `internal/harness/agent`, `coordinator`, `swarm` | Sub-agents, teams, workers, tmux/iTerm2/in-process teammate backends, and orchestration. |
| `internal/harness/plugins`, `skills` | Plugin manifests, skills, agents, hooks, MCP config loading, and runtime consumption. |
| `internal/harness/state`, `storage` | Session state, messages, usage, JSONL transcripts, and snapshots. |
| `internal/harness/memory`, `memdir`, `compact`, `budget` | Memory, context compaction, token budgets, and compatibility layers. |
| `internal/harness/provider`, `api`, `anthropic` | Provider abstraction for Anthropic, OpenAI, Qwen, Gemini, Bedrock, Vertex, and custom endpoints. |
| `internal/harness/mcp` | MCP server/client/resource support over stdio, HTTP, and SSE. |
| `internal/public` | Shared types, schemas, errors, filesystem helpers, and rate-limit foundations. |

### Capability Modules Worth Tracking Separately

These are not just small tool folders; they affect runtime safety, background execution, platform capabilities, or observability:

- `internal/harness/analysis`: command/tool analysis, read-only detection, and safety guidance foundations.
- `internal/harness/sandboxadapter`: sandbox policy and tool execution boundary adapters.
- `internal/harness/websandbox`: web/browser sandbox capabilities.
- `internal/harness/computeruse`: computer/browser control capabilities.
- `internal/harness/hooks`: lifecycle hooks and plugin extension points.
- `internal/harness/background`: background jobs and long-running task foundations.
- `internal/harness/tasks`: task state, output, stop behavior, snapshots, and tool surface.
- `internal/harness/telemetry`: events, tracing, and runtime observability.
- `internal/harness/powershell`: Windows PowerShell execution support.

Peripheral capability modules are usually considered together:

- `internal/harness/github`
- `internal/harness/deeplink`
- `internal/harness/teleport`
- `internal/harness/dxt`
- `internal/harness/nativeinstaller`
- `internal/harness/vim`
- `internal/harness/buddy`
- `internal/harness/ultraplan`

### Running The Local TUI

```bash
make run-tui
```

Or run it directly:

```bash
go run ./cmd/tui
```

Provider configuration can be supplied through environment variables or `~/.claude-codex/config.json`. The config home can be overridden with `CLAUDE_GO_HOME`. Supported provider families include Anthropic, OpenAI, Qwen, Gemini, Bedrock, Vertex, and custom OpenAI-compatible endpoints.

### Refactor Status

High-level status:

- The core execution chain is closed; `engine` is now a thin adapter and the default runtime goes through `queryengine -> query`.
- Tools, permissions, skills, tasks, memory, compact, provider, MCP, and storage have usable core paths.
- The CLI command matrix exists, but command coverage and TS-level details are still being filled in.
- The TUI has a working skeleton, but it remains the largest gap compared with the TypeScript implementation.
- bridge/remote/server, plugin management UI, GitHub/git, computer/browser, and other peripheral capabilities still need migration and UX work.

Detailed refactor docs:

- [docs/REFACTORING_MODULES.md](docs/REFACTORING_MODULES.md)
- [docs/MIGRATION_STATUS.md](docs/MIGRATION_STATUS.md)
- [docs/REFACTORING_SUMMARY.md](docs/REFACTORING_SUMMARY.md)
- [docs/REFACTOR_PROGRESS.md](docs/REFACTOR_PROGRESS.md)
- [docs/refactor/CORE_REFACTOR_STATUS.md](docs/refactor/CORE_REFACTOR_STATUS.md)
- [docs/Architecture-diagram/claudecode Go 重构子项目完整架构图.drawio_副本.pdf](docs/Architecture-diagram/claudecode%20Go%20重构子项目完整架构图.drawio_副本.pdf)
- [docs/Architecture-diagram/claudecode Go 重构：internal 全模块地图.drawio_副本.pdf](docs/Architecture-diagram/claudecode%20Go%20重构：internal%20全模块地图.drawio_副本.pdf)

---

## Development Commands

```bash
make fmt
make test
go test ./internal/backend/agentruntime ./internal/backend/googleauth ./internal/harness/provider ./cmd/agentapi
go build ./cmd/agentapi
go build ./cmd/tui

cd apps/web
npm ci
npm run build
npm run test
npm run e2e
```

## Security Notes

- Never commit `.env` files, R2 keys, Resend keys, admin tokens, JWT secrets, or service account JSON files.
- `secrets/` is intended for local secret material only and should remain gitignored.
- Prefer service account credentials for Vertex AI in production.
- Mount `/run/agentapi/secrets` read-only inside containers.
- Restrict CORS to exact frontend origins when using split-origin hosting.
- Use HTTPS before enabling cookie auth.
- Keep Cloudflare R2 S3 keys separate from Cloudflare API tokens.

## License

This project is licensed under the [MIT License](./LICENSE).
