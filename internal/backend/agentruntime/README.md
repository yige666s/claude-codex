# C-end Web Agent Runtime

This package is the production-oriented API/runtime layer for a consumer web agent.
The old demo browser UI has been removed; browser traffic now goes through `cmd/agentapi`.

## Plan

| Area | Harness current state | Runtime addition |
|---|---|---|
| Chat | `harness/engine` can run turns | SSE/WebSocket events, token deltas when the runner supports streaming, cancellation, error events |
| Session | `state.Session` supports messages and helpers | User-scoped `SessionStore` interface with file, SQL, and object-store adapters |
| Memory | Local/session memory primitives exist | User-scoped `MemoryService` interface with file, SQL, and object-store adapters |
| Skill | `harness/skills` supports invocation | Skill-scoped prompts, timeout, allowed tools/env handoff |
| Tools | Registry is usable | Low-risk registry by default; write/execute behind explicit policy |
| Permission | Checker supports product handlers | Default deny for write/execute, no `ModeBypass` |
| Web runtime | `cmd/agentapi` owns the browser/API surface | Formal API layer with auth, rate limit, logs, health, SSE, WebSocket, and embedded UI |
| Auth | Header auth existed as a local baseline | JWT, signed session-cookie JWT, trusted gateway header, and local header modes |
| Rate limit | Single-process limiter | Memory, Redis, gateway/off modes |
| Observability | Plain logs | Request IDs, structured logs, audit logs, metrics, readiness checks, and stable API error envelopes |

## API Shape

- `GET /healthz`
- `GET /readyz`
- `GET /metrics`
- `POST /v1/auth/register`
- `POST /v1/auth/login`
- `POST /v1/auth/refresh`
- `POST /v1/auth/logout`
- `GET /v1/auth/me`
- `DELETE /v1/account`
- `GET /v1/data/export`
- `DELETE /v1/memory`
- `POST /v1/attachments`
- `GET /v1/attachments`
- `GET /v1/attachments/{id}`
- `DELETE /v1/attachments/{id}`
- `GET /v1/artifacts`
- `GET /v1/artifacts/{id}`
- `DELETE /v1/artifacts/{id}`
- `POST /v1/jobs`
- `GET /v1/jobs`
- `GET /v1/jobs/{id}`
- `GET /v1/jobs/{id}/events`
- `POST /v1/jobs/{id}/cancel`
- `POST /v1/sessions`
- `GET /v1/sessions`
- `GET /v1/sessions/{id}`
- `DELETE /v1/sessions/{id}`
- `DELETE /v1/sessions/{id}/memory`
- `POST /v1/sessions/{id}/messages`
- `GET /v1/sessions/{id}/ws`
- `POST /v1/sessions/{id}/cancel`
- `GET /v1/skills`
- `GET /v1/admin/skills`
- `PATCH /v1/admin/skills/{name}`
- `GET /v1/admin/skills/{name}/versions`
- `POST /v1/admin/skills/{name}/review`
- `GET /v1/admin/skills/{name}/executions`
- `GET /v1/admin/skills/{name}/analytics`
- `POST /v1/admin/skills/{name}/publish`
- `POST /v1/admin/skills/{name}/unpublish`
- `POST /v1/admin/skills/{name}/disable`
- `GET /v1/llm/status`

## Storage Boundary

The runtime includes:

- `FileSessionStore` / `FileMemoryService` for local deployment baselines.
- `SQLSessionStore` / `SQLMemoryService` for `database/sql` backed deployments, with schema migration, index creation, pool knobs, and `postgres` placeholder support.
- `agent_messages` stores first-class user/assistant/tool message rows while `agent_sessions.payload` remains as a compatibility snapshot.
- Postgres timestamp fields use readable `TIMESTAMPTZ` values; existing millisecond integer timestamp columns are migrated in place on startup.
- `SQLUserStore` for consumer accounts and refresh token storage.
- `SQLArtifactStore` for attachment and artifact metadata.
- `ObjectSessionStore` / `ObjectMemoryService` for object-store backed sessions and memory.
- `FileObjectStore` and `HTTPObjectStore` object-store implementations; HTTP object stores support bearer auth and request timeout configuration.
- `S3ObjectStore` for Cloudflare R2/S3-compatible attachments and artifacts.

All stores scope data by user ID. File/object paths use a user hash so consumer
identifiers are not directly embedded in storage paths.

Postgres-style configuration:

```bash
go run ./cmd/agentapi \
  -store-backend sql \
  -sql-driver postgres \
  -sql-dialect postgres \
  -sql-dsn "$DATABASE_URL" \
  -sql-max-open-conns 30 \
  -sql-max-idle-conns 10
```

`cmd/agentapi` imports the `pgx` Postgres driver. Other `database/sql` drivers
must be imported by the final application if needed.

## Auth And Users

`cmd/agentapi` supports these auth modes:

- `-auth-mode jwt`: reads a bearer JWT and validates HS256, `exp`, optional `iss`, and optional `aud`.
- `-auth-mode cookie`: reads a JWT from `-session-cookie-name`.
- `-auth-mode trusted-header`: trusts an upstream gateway only when the configured secret header matches.
- `-auth-mode header`: local/dev mode using `-auth-token` plus `-user-header`.
- `-auth-mode auto`: tries configured JWT, cookie, trusted gateway, then local header.

Examples:

```bash
go run ./cmd/agentapi -auth-mode jwt -jwt-secret "$AGENT_API_JWT_SECRET"
go run ./cmd/agentapi -auth-mode cookie -session-cookie-secret "$AGENT_API_SESSION_COOKIE_SECRET"
go run ./cmd/agentapi -auth-mode trusted-header -trusted-secret-header X-Agent-Gateway-Secret -trusted-secret "$SECRET"
```

The runtime never trusts a request body user ID. The authenticated user identity
comes only from the configured auth layer.

Cookie auth hardening:

- Login/register/refresh set a signed HttpOnly session cookie when web security is configured.
- `-session-cookie-samesite lax|strict|none`, `-session-cookie-secure`, and `-session-cookie-domain` control cookie attributes. `SameSite=None` requires `Secure`.
- `-csrf-enabled` turns on double-submit CSRF protection for requests authenticated by the session cookie. The server sets a readable CSRF cookie and expects the same token in `X-CSRF-Token` (configurable with `-csrf-header-name`).
- `-cors-allowed-origins https://app.example.com,https://admin.example.com` enables CORS only for explicit origins. Credentials are enabled by default with `-cors-allow-credentials`.

The built-in consumer user system is enabled with:

```bash
go run ./cmd/agentapi \
  -store-backend sql \
  -sql-driver pgx \
  -sql-dialect postgres \
  -sql-dsn "$DATABASE_URL" \
  -enable-user-system \
  -auth-mode jwt \
  -jwt-secret "$AGENT_API_JWT_SECRET"
```

It creates:

- `agent_users`: user ID, email, bcrypt password hash, display name, status, timestamps.
- `agent_refresh_tokens`: hashed refresh token, user ID, expiry, revocation timestamp, user agent, IP.

The frontend uses `/v1/auth/register` and `/v1/auth/login` to receive an access
JWT and refresh token, then calls agent APIs with `Authorization: Bearer <jwt>`.
It persists the access token `expires_at`, refreshes before expiry, retries once
after authenticated API `401` responses, and reconnects job EventSource streams
with a fresh token.

## Data Lifecycle

The runtime supports consumer data controls:

- Delete one session with `DELETE /v1/sessions/{id}`. This also deletes that session's memory.
- Delete one session memory with `DELETE /v1/sessions/{id}/memory`.
- List user-visible memory items with `GET /v1/memory`; optional query parameters include `session_id`, `kind`, `category`, `visibility`, `status`, `q`, and `limit`.
- Edit one memory item with `PATCH /v1/memory/{id}`. User edits set `source=user_edit`, `confidence=1`, and recompute memory weight.
- Delete one memory item with `DELETE /v1/memory/{id}`.
- Delete all memory for a user with `DELETE /v1/memory`.
- Export user data with `GET /v1/data/export`; the payload includes user profile, sessions, and memory.
- SQL exports include a `messages` map keyed by session ID, sourced from `agent_messages` when available.
- Delete the account with `DELETE /v1/account`; this removes sessions, memory, refresh tokens, the user record, and the per-user workspace directory when configured.
- Apply retention cleanup on startup with `-retention-days N`, which prunes sessions and memory older than `N` days and removes expired/revoked refresh tokens past the same cutoff.

## Attachment And Artifact Storage

User-uploaded inputs and agent-generated outputs share SQL metadata plus object
storage, but they are separated by asset kind:

- Attachments are user-uploaded input files. Production uploads should use
  `POST /v1/attachments/presign`, direct `PUT` to the returned object-store
  URL, then `POST /v1/attachments/{id}/confirm`. Legacy multipart
  `POST /v1/attachments` remains available for file-backed local development.
- Artifacts are agent/skill/LLM-generated output files. Public artifact APIs only list, download, and delete generated outputs.
- Skill/runtime scopes receive an `ArtifactWriter`; `cmd/agentapi` exposes it to the model as the safe `Artifact` tool. The tool writes bytes through the runtime to the configured object store and records metadata as `kind=artifact`.
- Attachments and artifacts are checked by `AssetPolicy`: default max size is 64 MiB, dangerous/unknown extensions are rejected, and MIME types are restricted to common image, PDF, text, CSV, JSON, and Office document formats. Override size with `-asset-max-bytes`.
- `agent_artifacts`: asset ID, kind, user ID, session ID, object key, filename, content type, size, creation/deletion timestamps.
- Object bytes are stored in the configured object store.

Presigned attachment upload flow:

1. Client calls `POST /v1/attachments/presign` with `session_id`, `filename`,
   `content_type`, and `size_bytes`.
2. AgentAPI validates filename/MIME/size, generates an attachment ID and S3/R2
   presigned `PUT` URL, and returns the required upload headers.
3. Client uploads the file bytes directly to S3/R2.
4. Client calls `POST /v1/attachments/{id}/confirm` with the same metadata.
5. AgentAPI verifies the object exists via `HEAD`, checks size/content type, and
   records the attachment metadata in SQL.

This keeps large file bytes off the AgentAPI request path while preserving
server-side authorization, validation, and metadata ownership.
For browser uploads, configure the S3/R2 bucket CORS policy to allow authenticated
`PUT` requests with the returned `Content-Type` header from the web origin.

Cloudflare R2 configuration:

```bash
export CLOUDFLARE_ACCOUNT_ID="5c11ff96d03d238d51aef31150a87101"
export AGENT_API_ARTIFACT_S3_ENDPOINT="${CLOUDFLARE_ACCOUNT_ID}.r2.cloudflarestorage.com"
export AGENT_API_ARTIFACT_S3_ACCESS_KEY="REPLACE_WITH_R2_ACCESS_KEY_ID"
export AGENT_API_ARTIFACT_S3_SECRET_KEY="REPLACE_WITH_R2_SECRET_ACCESS_KEY"

go run ./cmd/agentapi \
  -store-backend sql \
  -sql-driver pgx \
  -sql-dialect postgres \
  -sql-dsn "$DATABASE_URL" \
  -artifact-store s3 \
  -artifact-s3-endpoint "$AGENT_API_ARTIFACT_S3_ENDPOINT" \
  -artifact-s3-access-key "$AGENT_API_ARTIFACT_S3_ACCESS_KEY" \
  -artifact-s3-secret-key "$AGENT_API_ARTIFACT_S3_SECRET_KEY" \
  -artifact-s3-bucket agentapi \
  -artifact-s3-prefix dev \
  -artifact-s3-ssl
```

Use R2 S3 access keys from the Cloudflare dashboard. The
`CLOUDFLARE_API_TOKEN` used by `wrangler` can create/list buckets but cannot be
used as the S3-compatible object-store secret.

Deleting a session also deletes attachments and artifacts linked to that
session. Deleting an account deletes all attachments and artifacts for the user
and removes the object bytes.

Uploaded attachments are stored through the configured object store. Message
`content_parts` keep only sanitized attachment references, while SQL stores the
per-message attachment metadata in `agent_message_attachments` for replay,
search indexing, thumbnail extraction, and embedding workers. The built-in
message attachment worker can claim pending records, strip image metadata by
re-encoding JPEG/PNG bytes, generate JPEG thumbnails, and extract basic PDF/text
content into derived object-store keys. When message search is configured, the
worker also indexes extracted attachment text as independent attachment
documents/vectors that still point back to the parent message. Completion
updates `embedding_status`, `thumbnail_key`, and `extracted_text_key`.

Key attachment worker flags:

- `-message-attachment-worker-enabled` starts the worker when SQL message
  attachment storage and an object store are configured.
- `-message-attachment-worker-batch-size 25` and
  `-message-attachment-worker-poll-interval 5s` control queue polling.
- `-message-attachment-worker-process-timeout 30s` bounds one attachment.
- `-message-attachment-thumbnail-max-dimension 512` controls generated
  thumbnail dimensions.

## Rate Limiting

- `-rate-limit-backend memory` is the single-process default.
- `-rate-limit-backend redis -redis-url redis://:pw@host:6379/0?prefix=agentapi` enables distributed per-user limits.
- `-rate-limit-backend gateway` or `none` disables app-level limiting for deployments that enforce limits at the edge.

Redis limiting uses fixed windows keyed by user ID.

## Message Context Cache

- `-message-context-cache-backend memory` keeps loaded session context in the process-local cache.
- `-message-context-cache-backend redis -message-context-cache-redis-url redis://:pw@host:6379/1?prefix=agentapi:message:ctx` enables the shared Redis hot cache for `SessionLoadService`.
- `-message-context-cache-backend none` disables the hot cache and always reads from durable message storage.
- `-message-context-cache-ttl 24h` controls Redis cache expiry.

The Redis URL accepts the same database path style as the rate limiter and a
`prefix` query parameter. When Redis is selected, `/readyz` includes a
`message_context_cache` Redis ping check.

## Session List Cache

- `-session-list-cache-backend redis -session-list-cache-redis-url redis://:pw@host:6379/1?prefix=agentapi:session:list` enables the Redis session list cache for SQL-backed sessions.
- `-session-list-cache-ttl 10m` controls how long each user's list snapshot stays warm.
- `GET /v1/sessions?limit=50&offset=100` uses the same runtime path and can be served from Redis once the user list has been warmed.

The cache stores session metadata only: a per-user sorted set orders session IDs
by `updated_at`, and a per-user hash stores the session-list JSON payload. It
does not store transcript messages.

## Message Archive

SQL message storage supports a two-layer retention path for old payloads:
PostgreSQL keeps message/session indexes plus `archive_uri`,
`archive_checksum`, and `archived_at`, while the full message payload is written
to the configured artifact object store.

- `-message-archive-worker-enabled` starts the background archive worker.
- `-message-archive-after 720h` archives messages older than 30 days.
- `-message-archive-prefix message-archive` controls the object-store prefix.
- `-message-archive-clear-pg-payload=true` clears large SQL payload fields after
  the gzip archive object is uploaded and checksummed.

When the artifact store is S3/R2, archived message objects are stored under keys
such as `message-archive/year=2026/month=04/user_hash=.../session_id=.../*.json.gz`.
Session and message reads transparently hydrate archived payloads from the
object store when it is configured.

## Message Events And Kafka

- `-message-events-backend local` is the default and keeps asynchronous message
  post-processing in the AgentAPI process.
- `-message-events-backend kafka -message-events-kafka-brokers host:9092`
  publishes `message.created` events to Kafka topic `agent.messages`, keyed by
  `session_id`.
- `-message-events-backend dual` publishes to Kafka while also keeping local
  in-process processing enabled.
- `-message-events-kafka-consumer-enabled` starts the built-in consumer worker.
  The current handler consumes Kafka message events and writes semantic vectors
  to Qdrant when `-message-search-backend semantic` or `hybrid` is configured.
- `-message-events-kafka-dlq-topic agent.messages.dlq` enables a dead-letter
  topic after configured retries fail.
- `-message-events-processed-lock-backend redis` enables Redis idempotency locks
  keyed by processor and `message_id`; failed processing releases the lock so
  Kafka redelivery can retry.

Kafka readiness is reported as `kafka_message_events` when Kafka publishing or
the built-in consumer is enabled.

## Message Search

- `-message-search-backend sql` keeps the local SQL/file fallback.
- `-message-search-backend elasticsearch -message-search-endpoint http://localhost:9200 -message-search-index agent_messages` uses Elasticsearch for full-text search.
- `-message-search-backend opensearch` uses the same endpoint/index/auth flags for OpenSearch.
- `-message-search-backend semantic` uses Qdrant plus an embedding provider.
- `-message-search-backend hybrid` runs full-text and semantic search, then fuses results with RRF.

Environment variables follow the `AGENT_API_MESSAGE_SEARCH_*` prefix, for example
`AGENT_API_MESSAGE_SEARCH_ENDPOINT`, `AGENT_API_MESSAGE_SEARCH_QDRANT_ENDPOINT`,
and `AGENT_API_MESSAGE_SEARCH_EMBEDDING_ENDPOINT`.

Semantic search supports OpenAI-compatible embeddings and Vertex AI Gemini
embeddings. For Vertex AI, set `AGENT_API_MESSAGE_SEARCH_EMBEDDING_PROVIDER=vertex`,
`AGENT_API_MESSAGE_SEARCH_EMBEDDING_PROJECT_ID`, `AGENT_API_MESSAGE_SEARCH_EMBEDDING_LOCATION`,
and `AGENT_API_MESSAGE_SEARCH_EMBEDDING_MODEL`. The local compose defaults are
wired for project `vigilant-router-378708`, location `global`, model
`gemini-embedding-2`, query task `RETRIEVAL_QUERY`, index task
`RETRIEVAL_DOCUMENT`, and 768 dimensions. Authentication uses
`AGENT_API_MESSAGE_SEARCH_EMBEDDING_TOKEN`, `VERTEX_ACCESS_TOKEN`,
`GOOGLE_OAUTH_ACCESS_TOKEN`, `GOOGLE_ACCESS_TOKEN`, service account env vars, or
`gcloud auth print-access-token`.

When the backend is `semantic` or `hybrid`, message writes also enqueue an
asynchronous vector indexing job. The worker extracts searchable message text,
generates an embedding, creates the Qdrant collection on first use, upserts the
point payload, and records the vector ID/model version in SQL embedding metadata.

For Elasticsearch-backed full-text search, enable
`AGENT_API_MESSAGE_SEARCH_INDEX_MANAGEMENT_ENABLED=true` to let AgentAPI create
and maintain the message index lifecycle. The bootstrap step writes an ILM
policy, a rollover index template, and an initial `{alias}-000001` write index
when the alias is missing. AgentAPI rechecks this bootstrap state every minute,
so an accidentally deleted write alias/index is recreated without falling back
to SQL search. Text fields use Chinese IK analyzers by default:
`AGENT_API_MESSAGE_SEARCH_INDEX_ANALYZER=ik_max_word` for indexing and
`AGENT_API_MESSAGE_SEARCH_INDEX_SEARCH_ANALYZER=ik_smart` for querying. The ES
cluster must have the IK plugin installed before enabling this in production.
Do not expose unauthenticated Elasticsearch to the public internet; local compose
binds ES to `127.0.0.1` by default via `AGENT_API_ELASTICSEARCH_HOST`.

Managed ES indices roll over at 30 days through ILM. The AgentAPI maintenance
worker additionally downgrades old backing indices to read-only with fewer
replicas after `AGENT_API_MESSAGE_SEARCH_INDEX_DOWNGRADE_AFTER` (default `2160h`,
90 days) and closes them after `AGENT_API_MESSAGE_SEARCH_INDEX_CLOSE_AFTER`
(default `4320h`, 180 days). Set
`AGENT_API_MESSAGE_SEARCH_INDEX_MAINTENANCE_INTERVAL` and
`AGENT_API_MESSAGE_SEARCH_INDEX_MAINTENANCE_BATCH_LIMIT` to tune maintenance
frequency and per-pass work.

## LLM Governance

`cmd/agentapi` wraps provider planners with a governance layer:

- Fallbacks: `-llm-fallbacks openai:gpt-4o-mini,custom:local-model`.
- Retries: `-llm-max-attempts` and `-llm-retry-backoff`.
- Timeout tiers: `-llm-chat-timeout` for normal chat calls and `-llm-skill-timeout` for skill/workflow calls.
- Quotas: `-llm-daily-token-quota`, `-llm-daily-request-quota`, and `-llm-daily-cost-quota-usd`.
- Estimated cost: `-llm-input-cost-per-million` and `-llm-output-cost-per-million`.
- Runtime model selection: Admin UI updates SQL-backed runtime config; CLI model flags are only startup defaults.
- Runtime model catalog: Admin UI model options are loaded from the SQL-backed
  `agent_runtime_config` row keyed by `llm_model_catalog`; first startup seeds
  this row from built-in defaults when it is missing.
- Health: retryable failures update per-backend health and open a temporary circuit breaker after `-llm-failure-threshold`; cooldown is controlled by `-llm-circuit-cooldown`.

When SQL storage is enabled, successful and failed provider attempts are stored
in `agent_llm_usage` with user ID, session ID, request ID, skill name, provider,
model, estimated tokens, estimated cost, status, error text, latency, and
timestamp. Non-SQL local runs keep usage in memory.

The authenticated `GET /v1/llm/status` endpoint returns the current backend
health snapshot and active governance limits.

## Observability

The runtime exposes a first production observability baseline:

- Every request gets `X-Request-ID`; structured logs include request ID, method, path, status, bytes, and latency.
- Error JSON keeps the legacy `error` field and adds stable `code`, `message`, and `request_id` fields for frontend handling and support lookup.
- `GET /metrics` returns Prometheus-style counters for total requests, status buckets, route buckets, errors, rate-limit blocks, audit write failures, and aggregate latency.
- `GET /readyz` runs configured readiness checks for SQL, object storage, Redis when enabled, LLM config/credentials, and LLM backend health. `GET /healthz` remains a lightweight process liveness check.
- `agent_audit_logs` records key consumer operations when SQL storage is enabled. File/local runs use an in-memory audit logger.

Audited operations include register/login/refresh/logout, account deletion, data export, session create/delete, memory deletion, attachment create/delete, artifact deletion, job creation/cancellation, and routed long-running chat-to-job execution.

## Container Image

Build the API image with:

```bash
docker build -f Dockerfile.agentapi -t claude-codex-agentapi:local .
```

The image runs `agentapi` from `/workspace`, stores local fallback data under
`/var/lib/agentapi`, exposes port `8081`, and includes a `/healthz`
healthcheck. It also includes the Docker CLI so deployments that intentionally
use `-skill-shell-runner docker` can mount a Docker socket or provide a remote
Docker endpoint; production deployments should treat that socket as privileged
infrastructure.

Run the local production-style stack with Postgres, Cloudflare R2, Redis, and
AgentAPI:

```bash
export VERTEX_ACCESS_TOKEN="$(gcloud auth print-access-token)"
export VERTEX_PROJECT_ID="vigilant-router-378708"
export VERTEX_LOCATION="us-central1"
export AGENT_API_ARTIFACT_S3_ACCESS_KEY="REPLACE_WITH_R2_ACCESS_KEY_ID"
export AGENT_API_ARTIFACT_S3_SECRET_KEY="REPLACE_WITH_R2_SECRET_ACCESS_KEY"
docker compose -f deploy/local/docker-compose.yml up --build
```

The local compose file mounts `.claude` read-only into `/workspace/.claude` for
skill loading and persists API, Postgres, and Redis data in named Docker
volumes. Attachment and artifact bytes are stored in Cloudflare R2.

Use `deploy/production/.env.example` as the production configuration checklist.
It documents required secrets, supported environment variables, and the startup
flags that still need to be passed by the deployment command.
Use `deploy/production/BACKUP_RESTORE.md` for the Postgres, object storage, and
workspace backup/restore runbook.

On `SIGINT` or `SIGTERM`, `cmd/agentapi` marks readiness as unavailable, stops
accepting new HTTP work, closes live job SSE streams, cancels running chat/job
contexts, and waits up to `-shutdown-timeout` before returning. Container
orchestrators should set a termination grace period slightly longer than that
timeout.

## Formal Frontend

The product-facing frontend lives in `apps/web`. It is a Vite + React +
TypeScript app that consumes the existing `/v1` API and keeps the backend
runtime separate from the browser product surface.

```bash
cd apps/web
npm install
npm run dev
```

The dev server proxies API calls to `http://localhost:8081` by default. Override
with `AGENT_API_DEV_TARGET` when the backend uses another address.
When running the Vite app locally, include `http://localhost:5173` in
`-cors-allowed-origins` / `AGENT_API_CORS_ALLOWED_ORIGINS`.

For production, build `apps/web` as static assets and serve them from a static
host/CDN or the provided `Dockerfile.agentweb` nginx image. The preferred
topology is same-origin: serve the SPA and reverse-proxy `/v1/*`, `/healthz`,
`/readyz`, and `/metrics` to `cmd/agentapi`, leaving
`VITE_AGENT_API_BASE_URL` unset so browser calls stay relative. Split-origin
deployments should build with `VITE_AGENT_API_BASE_URL=https://api.example.com`
and set `AGENT_API_CORS_ALLOWED_ORIGINS` to the exact frontend origin.
See `deploy/production/FRONTEND.md` for the deployment contract.

## Durable Jobs

Long-running chat or skill workflows run as durable jobs instead of staying
coupled to a single browser request. Product users use the normal chat entry;
the runtime routes long-running work to jobs internally.

- The chat API can auto-route `POST /v1/sessions/{id}/messages` to a job and
  returns an SSE `job` event containing `job_id` and the job payload.
- API clients can still create explicit jobs with `POST /v1/jobs`:

```json
{
  "session_id": "20260508T101310Z-6964c87940a8",
  "content": "/vertex-image-artifact a tiny blue robot",
  "type": "skill"
}
```

- Job state is stored in `agent_jobs` with `queued`, `running`, `succeeded`,
  `failed`, or `cancelled` status.
- Job execution is dispatched through Redis Streams. `cmd/agentapi` writes
  `job_id`/`user_id` work items to `-job-queue-stream` and the built-in worker
  consumes them with `-job-queue-consumer-group`. Unacknowledged jobs stay in
  the Redis pending list and can be claimed by another worker after
  `-job-queue-claim-idle`, so API/worker restarts do not strand in-flight work.
- Job event realtime fanout uses Redis Pub/Sub. Each instance persists events
  to `agent_job_events`, publishes the record to `-job-event-fanout-channel`,
  and every other instance injects the event into its local SSE broker. The
  database replay path remains authoritative if Pub/Sub delivery is missed.
- Job events are stored in `agent_job_events` and mirror the normal runtime
  event stream: `start`, `message`, `delta`, `error`, `done`, and `cancelled`.
- Replay events with `GET /v1/jobs/{id}/events`.
- Stream/replay events with `GET /v1/jobs/{id}/events?stream=1`; use
  `after_id` or the SSE `Last-Event-ID` header to resume after the last seen
  event.
- Streaming clients receive `id:` fields for browser EventSource reconnects.
  Online streams use an in-process fanout broker after the durable replay step;
  slow clients are disconnected when their per-connection buffer fills.
- Cancel with `POST /v1/jobs/{id}/cancel`.
- Generated artifacts carry `job_id` when they are created during job execution.
- User data export includes jobs and job events.
- The embedded UI includes a Jobs drawer with job list, timeline replay, live
  updates, and cancellation controls.
- Key job queue flags:
  `-job-queue-redis-url redis://redis:6379/2`,
  `-job-queue-stream agentapi:jobs`,
  `-job-queue-consumer-group agentapi-job-workers`,
  `-job-worker-enabled=true`, `-job-queue-claim-idle 1m`, and
  `-job-queue-lock-ttl 2m`.
- Key job event fanout flags:
  `-job-event-fanout-enabled=true` and
  `-job-event-fanout-channel agentapi:job-events`.
- Skills can request durable execution through frontmatter metadata:

```yaml
metadata:
  agentapi:
    run_as_job: true
    produces_artifacts: true
```

The parser also accepts `metadata.job`, `metadata.long_running`,
`metadata.produces_artifacts`, and nested `agentapi` / `runtime` / `openclaw`
`execution: job` markers. In addition, artifact-heavy or obviously long-running
natural-language requests, such as PPT/image/video/report/batch/crawl/codebase
analysis requests, are routed to jobs by default.

## Safety Defaults

- API requests require an authenticated user ID.
- Sessions use per-user sandbox workspaces when `-user-workspace-root` is configured.
- Request-provided `working_dir` is ignored by default unless `-allow-custom-working-dir` is set and no user workspace root is configured.
- Read/search/web/skill tools are enabled by default inside the resolved workspace.
- Skills are loaded from bundled skills plus `-skill-dirs` / `AGENT_API_SKILL_DIRS`, a comma-separated list of directories containing `skill-name/SKILL.md` folders. Add `~/.claude/skills` or `<workspace>/.claude/skills` to that list when those default-style locations should be used.
- Write/edit/bash tools require `cmd/agentapi -allow-dangerous-tools`.
- Server-side permission policy denies write/execute unless explicitly enabled.
- Consumer-invocable skills are loaded from code-controlled skill directories into the `agent_skills` registry. New user-invocable, non-hidden skills are published on first sync; after that the database `status` controls whether each skill is visible and invocable.
- Admin skill APIs require normal user authentication plus `-admin-token` / `AGENT_API_ADMIN_TOKEN` through the `X-Admin-Token` header. The SQL registry supports listing, metadata edits, version history, pre-publish review, publish, unpublish, and disable without restarting the server.
- Skill registry metadata can carry runtime policy under `metadata.policy` or `metadata.agentapi.policy`. Supported keys include `allowed_tools`, `allowed_env`, `network_allowlist`, `artifact_content_types`, `shell_timeout`, and `sandbox` (`image`, `network`, `memory`, `cpus`, `pids_limit`, `tmpfs_size`, `max_output_bytes`). These policy values are enforced for skill shell execution, model-visible tools, WebFetch/WebSearch, and Artifact outputs.
- Direct consumer skill executions are recorded in `agent_skill_executions` when SQL storage is enabled, including skill name, user/session/job/request IDs, status, duration, error text, and policy snapshot metadata. Admin APIs can list recent executions or fetch aggregate success/failure/latency analytics per skill.
- Skill frontmatter shell commands must match the skill's `allowed-tools` shell patterns when configured, and are capped by `-skill-shell-timeout` (default 90s).
- Skill frontmatter shell commands can run in one-shot Docker sandboxes with `-skill-shell-runner docker`. Each command runs in a fresh container with the user workspace mounted at `/workspace`, the skill directory mounted read-only at `/skill`, `--read-only`, `--cap-drop ALL`, `no-new-privileges`, CPU/memory/pid limits, and a tmpfs `/tmp`; the container is removed after execution.
- Docker sandbox network defaults to `none`. Use `-skill-sandbox-network bridge` only for published skills that require outbound calls, and pass secrets through the skill's explicit `metadata.openclaw.requires.env` list.
- Docker sandbox images can be warmed at API startup with `-skill-sandbox-prepull-images`. Job event replay records `sandbox_metric` events for sandboxed command duration, image, network mode, output size, and success/failure.
- WebFetch/WebSearch allow all domains when `-network-allowlist` is empty. Set an explicit comma-separated domain list for production deployments that need restricted egress.
- Cross-origin browser access is denied unless `-cors-allowed-origins` includes the request origin.

Example production safety shape:

```bash
go run ./cmd/agentapi \
  -user-workspace-root /srv/agentapi/workspaces \
  -skill-shell-runner docker \
  -skill-sandbox-image python:3.12-slim \
  -skill-sandbox-network none \
  -skill-sandbox-memory 512m \
  -skill-sandbox-cpus 1 \
  -skill-sandbox-prepull-images python:3.12-slim,node:22-alpine
```

## LLM Providers

`cmd/agentapi` supports provider selection with `-llm-provider`:

- `anthropic` uses the native Anthropic client and token streaming.
- `openai` uses the OpenAI-compatible provider with `OPENAI_API_KEY`.
- `qwen` uses Alibaba Cloud Model Studio / DashScope OpenAI-compatible chat
  completions with `DASHSCOPE_API_KEY` or `QWEN_API_KEY`; override the default
  China endpoint with `DASHSCOPE_BASE_URL` or `QWEN_BASE_URL` when needed.
- `gemini` uses the Gemini API key provider with `GEMINI_API_KEY`.
- `vertex` uses Gemini on Vertex AI through REST `generateContent` and Claude
  partner models through Anthropic `rawPredict`; set `VERTEX_ACCESS_TOKEN` or
  `GOOGLE_OAUTH_ACCESS_TOKEN`, plus `VERTEX_PROJECT_ID` /
  `GOOGLE_CLOUD_PROJECT` and optionally `VERTEX_LOCATION`. Claude short model
  names such as `claude-sonnet-4-5@20250929` use
  `VERTEX_ANTHROPIC_LOCATION`, which defaults to the global endpoint.
  Local runs also try `gcloud auth print-access-token` when no token is set or
  when Vertex returns `401 Unauthorized`, then retry the request once.
- `shortapi` uses ShortAPI's OpenAI-compatible `/v1/chat/completions`
  endpoint with `SHORTAPI_KEY`; the default model is
  `google/gemini-3.1-pro-preview`.
- `custom` uses the OpenAI-compatible provider with `-api-base-url`.

Examples:

```bash
go run ./cmd/agentapi -llm-provider openai -model gpt-4o-mini
go run ./cmd/agentapi -llm-provider qwen -model qwen-plus
go run ./cmd/agentapi -llm-provider gemini -model gemini-1.5-flash
go run ./cmd/agentapi -llm-provider shortapi -model google/gemini-3.1-pro-preview
go run ./cmd/agentapi -llm-provider custom -api-base-url http://localhost:11434/v1 -api-key local -model llama3.1
go run ./cmd/agentapi -llm-provider vertex -model gemini-1.5-pro
go run ./cmd/agentapi -llm-provider vertex -model claude-sonnet-4-5@20250929
```

### Gemini Live mode

`cmd/agentapi` can expose a dedicated Gemini Live bridge at
`GET /v1/sessions/{session_id}/live/ws` when `AGENT_API_LIVE_ENABLED=true`.
This is separate from the normal chat `generateContent` path. The browser sends
base64 PCM audio frames:

```json
{"type":"audio","mime_type":"audio/pcm;rate=16000","data":"...base64..."}
```

The server connects to Vertex `BidiGenerateContent`, requests audio output plus
input/output transcription, streams `live_audio` events back to the browser, and
persists completed transcription turns as normal user/assistant messages. Those
messages continue through `MessageWriteService`, memory extraction, Kafka,
Elasticsearch, and Qdrant indexing.

Relevant settings:

- `AGENT_API_LIVE_MODEL=gemini-live-2.5-flash-preview-native-audio-09-2025`
- `AGENT_API_LIVE_VERTEX_LOCATION=us-central1`
- `AGENT_API_LIVE_VOICE_NAME=Puck`
- `AGENT_API_LIVE_LANGUAGE_CODE=zh-CN`
- `AGENT_API_LIVE_INPUT_TRANSCRIPTION_ENABLED=true`
- `AGENT_API_LIVE_OUTPUT_TRANSCRIPTION_ENABLED=true`
- `AGENT_API_LIVE_VAD_START_SENSITIVITY=START_SENSITIVITY_HIGH`
- `AGENT_API_LIVE_VAD_END_SENSITIVITY=END_SENSITIVITY_HIGH`
- `AGENT_API_LIVE_VAD_PREFIX_PADDING=150ms`
- `AGENT_API_LIVE_VAD_SILENCE_DURATION=350ms`

`AGENT_API_LIVE_VOICE_NAME` accepts the Gemini Live prebuilt voices from the
Vertex Live voice configuration docs: `Achernar`, `Achird`, `Algenib`,
`Algieba`, `Alnilam`, `Aoede`, `Autonoe`, `Callirrhoe`, `Charon`, `Despina`,
`Enceladus`, `Erinome`, `Fenrir`, `Gacrux`, `Iapetus`, `Kore`, `Laomedeia`,
`Leda`, `Orus`, `Puck`, `Pulcherrima`, `Rasalgethi`, `Sadachbia`,
`Sadaltager`, `Schedar`, `Sulafat`, `Umbriel`, `Vindemiatrix`, `Zephyr`, and
`Zubenelgenubi`. Names are normalized case-insensitively before the Live setup
message is sent.
