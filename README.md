# AgentAPI

AgentAPI is a Go backend plus React web frontend for a consumer-facing
agent workspace. It supports authenticated chat sessions, long-running jobs,
skills, attachments, generated artifacts, memory, admin operations, audit logs,
and production-oriented deployment through Docker Compose.

The repository still contains the local TUI/CLI harness, but the main product
surface is now `cmd/agentapi` + `apps/web`.

## Contents

- [Current Status](#current-status)
- [Repository Layout](#repository-layout)
- [Core Capabilities](#core-capabilities)
- [Quick Start](#quick-start)
- [Configuration](#configuration)
- [Deployment](#deployment)
- [Operations](#operations)
- [Development Commands](#development-commands)
- [Security Notes](#security-notes)
- [Related Docs](#related-docs)

## Current Status

The current production path is:

- Backend: `cmd/agentapi`
- Frontend: `apps/web`
- Database: Postgres
- Cache/rate limiting: Redis
- Object storage: Cloudflare R2 through the S3-compatible API
- LLM provider: Vertex AI with service account credentials
- Email provider: Resend for registration verification
- Deployment: Docker Compose, with GitHub Actions deploy-on-main support

Implemented product areas include:

- JWT user auth with refresh tokens and optional email verification.
- Session chat with streaming, reconnect/replay support, and attachment-aware
  messages.
- Job execution and job event timeline replay.
- Attachment and generated artifact storage through R2.
- Skill registry, product skill listing, skill execution history, review checks,
  and admin management APIs/UI.
- Memory capture, governance, user controls, deletion/export, maintenance, and
  quality feedback.
- Admin console for skills, users, sessions/jobs/artifacts, health/cost, audit,
  and risk operations.
- Risk control and abuse prevention primitives, including rate limits, risk
  events, admin review actions, and audit logging.

Still intentionally deferred:

- Product analytics beyond the operational metrics already exposed.
- Advanced vector/multimodal memory retrieval enhancements.
- Some deeper enterprise-style governance workflows that are not required for
  the current consumer-focused launch path.

## Repository Layout

| Path | Purpose |
| --- | --- |
| `cmd/agentapi` | Product backend entrypoint. |
| `apps/web` | React/Vite product frontend and admin console. |
| `internal/backend/agentruntime` | Web agent runtime: auth, sessions, memory, jobs, skills, artifacts, admin/risk APIs. |
| `internal/backend/googleauth` | Service-account OAuth token exchange used for Vertex AI. |
| `internal/harness` | Local agent harness, providers, tools, state, skills, and CLI-oriented runtime pieces. |
| `.claude/skills` | Local skills exposed to AgentAPI, including docx and Vertex image artifact generation. |
| `deploy/local` | Local Docker Compose stack. |
| `deploy/production` | Production deployment examples, frontend nginx config, backup/restore runbook. |
| `.github/workflows/deploy-main.yml` | Push-to-main deployment workflow for the test server. |

## Core Capabilities

### Web Product

- Login, registration, email verification, logout, and account deletion.
- Session list, chat transcript, streaming responses, global search, and
  responsive desktop/mobile layout.
- Attachment upload with progress, preview/download/delete, and message
  attachment sending.
- Artifact list, preview/download/delete, and generated artifact surfacing.
- Skill browser with category grouping, search, detail modal, and insertion into
  chat/job flows.
- Settings modal with memory controls and data/account actions.

The site logo is stored at `apps/web/public/logo.png` and is used as the
browser icon and in-app brand mark.

### Agent Runtime

- SQL-backed user, session, message, memory, skill, job, artifact, audit, and
  risk records.
- SSE chat stream and job stream replay.
- Governed LLM execution with retries, request/token quotas, usage/cost
  accounting, and fallback hooks.
- Skill execution with policy merge, sandbox settings, allowed env/tool
  controls, generated artifact registration, and execution telemetry.
- Memory extraction, abstraction, scoring/maintenance, redaction, and
  user-controlled opt-in/out.

### Admin And Operations

- Admin token protected `/admin` UI.
- Skill registry and policy management.
- User status management.
- Session/job/artifact troubleshooting.
- Runtime health, readiness, and cost panels.
- Audit log and risk review console.

## Quick Start

### Prerequisites

- Go `1.24.4` or compatible with `go.mod`.
- Node.js `22` for `apps/web`.
- Docker and Docker Compose for the local full stack.
- Postgres, Redis, and Cloudflare R2 credentials for compose/production.
- Vertex AI service account JSON for the default LLM provider.

### Local Web Stack

The recommended local path is Docker Compose:

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

More local compose details are in [deploy/local/README.md](deploy/local/README.md).

### Frontend Dev Server

Run the backend first, then:

```bash
cd apps/web
npm ci
npm run dev
```

Vite proxies `/v1`, `/healthz`, `/readyz`, and `/metrics` to
`http://localhost:8081` by default.

### Backend Without Compose

For a direct backend run:

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

## Configuration

The production template is [deploy/production/.env.example](deploy/production/.env.example).
Do not commit real `.env` files or service account JSON files.

Important environment groups:

| Group | Key Variables |
| --- | --- |
| Auth | `AGENT_API_JWT_SECRET`, `AGENT_API_ADMIN_TOKEN`, JWT/session settings |
| Email | `AGENT_API_EMAIL_PROVIDER=resend`, `AGENT_API_RESEND_API_KEY`, `AGENT_API_EMAIL_FROM`, `AGENT_API_EMAIL_PUBLIC_BASE_URL` |
| Storage | `AGENT_API_ARTIFACT_S3_ENDPOINT`, `AGENT_API_ARTIFACT_S3_ACCESS_KEY`, `AGENT_API_ARTIFACT_S3_SECRET_KEY`, `AGENT_API_ARTIFACT_S3_BUCKET`, `AGENT_API_ARTIFACT_S3_PREFIX` |
| Vertex | `GOOGLE_APPLICATION_CREDENTIALS`, `VERTEX_PROJECT_ID`, `GOOGLE_CLOUD_PROJECT`, `VERTEX_LOCATION` |
| Runtime | `AGENT_API_SKILL_DIRS`, `AGENT_API_SKILL_POLICY`, `AGENT_API_OPERATION_RATE_LIMITS`, `AGENT_API_RETENTION_DAYS` |
| Frontend | `VITE_AGENT_API_BASE_URL` for split frontend/API origins |

### Vertex AI Credentials

Production should use a service account JSON, not short-lived access-token
environment variables.

For Docker Compose, mount a host secrets directory and point the container at
the mounted path:

```env
AGENT_API_SECRETS_DIR=/opt/agentapi/secrets
GOOGLE_APPLICATION_CREDENTIALS=/run/agentapi/secrets/vertex-service-account.json
VERTEX_PROJECT_ID=your-gcp-project
GOOGLE_CLOUD_PROJECT=your-gcp-project
VERTEX_LOCATION=us-central1
```

The service account needs enough permission to call Vertex AI, for example
`roles/aiplatform.user` on the target project.

## Deployment

The current deployment model is Docker Compose on a server:

```bash
cd /opt/agentapi/repo
docker compose --env-file /opt/agentapi/.env -f deploy/local/docker-compose.yml up -d --build
```

The GitHub Actions workflow in `.github/workflows/deploy-main.yml` can deploy
automatically after pushes to `main`. It SSHes into the server, updates the repo,
and rebuilds/restarts the compose stack.

Frontend production options:

- Same origin: serve `apps/web/dist` and reverse-proxy `/v1`, `/healthz`,
  `/readyz`, and `/metrics` to AgentAPI.
- Split origin: build with `VITE_AGENT_API_BASE_URL=https://api.example.com`
  and configure CORS on AgentAPI.

See [deploy/production/FRONTEND.md](deploy/production/FRONTEND.md).

## Operations

Useful checks:

```bash
curl -fsS http://localhost:8081/healthz
curl -fsS http://localhost:8081/readyz
curl -fsS http://localhost:8081/metrics
```

Common server checks:

```bash
docker ps
docker logs --tail=100 claude-codex-agentapi
docker logs --tail=100 claude-codex-agentweb
```

Backup and restore guidance is in
[deploy/production/BACKUP_RESTORE.md](deploy/production/BACKUP_RESTORE.md).

## Development Commands

```bash
make fmt
make test
go test ./internal/backend/agentruntime ./internal/backend/googleauth ./internal/harness/provider ./cmd/agentapi
go build ./cmd/agentapi

cd apps/web
npm run build
npm run test
npm run e2e
```

The repository also includes the local TUI:

```bash
make run-tui
```

## Security Notes

- Never commit `.env` files, R2 keys, Resend keys, admin tokens, or service
  account JSON files.
- `secrets/` is gitignored and intended for local secret material only.
- Prefer service account credentials for Vertex AI.
- Keep `/run/agentapi/secrets` mounted read-only in containers.
- Restrict CORS to exact frontend origins when using split-origin hosting.
- Use HTTPS before enabling cookie auth in production.
- Keep Cloudflare R2 S3 keys separate from Cloudflare API tokens.

## Related Docs

- [apps/web/README.md](apps/web/README.md)
- [deploy/local/README.md](deploy/local/README.md)
- [deploy/production/.env.example](deploy/production/.env.example)
- [deploy/production/FRONTEND.md](deploy/production/FRONTEND.md)
- [deploy/production/BACKUP_RESTORE.md](deploy/production/BACKUP_RESTORE.md)
- [internal/backend/agentruntime/README.md](internal/backend/agentruntime/README.md)
- [internal/backend/agentruntime/PRODUCTION_PROGRESS.md](internal/backend/agentruntime/PRODUCTION_PROGRESS.md)

## License

This project is licensed under the [MIT License](./LICENSE).
