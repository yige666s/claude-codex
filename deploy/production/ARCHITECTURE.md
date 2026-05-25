# Production Architecture

## Goals

The production architecture should remove the single-node assumptions from the
test server deployment:

- tolerate one API pod failure without user-visible outage
- recover queued jobs after API or worker restart
- keep uploaded/generated assets outside local disk
- keep credentials and browser security explicit
- isolate unsafe skill execution away from public API traffic
- make rollbacks predictable through immutable image tags

## Runtime Components

### agentweb

`agentweb` is a static Vite frontend. Production can serve it from CDN/static
hosting or from the `agentweb` container behind an ingress.

Recommended replicas: 2 when containerized.

### agentapi

`agentapi` serves:

- REST APIs under `/v1`
- SSE chat/job streams
- Live WebSocket sessions
- health/readiness/metrics endpoints

Recommended replicas: 3+.

The API deployment should disable background workers:

```env
AGENT_API_JOB_WORKER_ENABLED=false
AGENT_API_MESSAGE_ATTACHMENT_WORKER_ENABLED=false
AGENT_API_MESSAGE_ARCHIVE_WORKER_ENABLED=false
AGENT_API_MESSAGE_EVENTS_KAFKA_CONSUMER_ENABLED=false
```

### agentworker

`agentworker` currently uses the same `agentapi` image and entrypoint, but is
not exposed through a public Service. It runs background work:

- Redis Streams job execution
- attachment processing
- message archive to object storage
- message event/indexing consumer when enabled

Recommended replicas: 2+.

The worker deployment should enable workers and can leave the internal HTTP
server running only for pod health probes.

### Postgres

Postgres is the source of truth for users, sessions, messages, memory, jobs,
artifacts, audit records, and schema migrations.

Use a managed HA Postgres service or a production-grade Postgres cluster with:

- TLS required
- point-in-time recovery
- daily logical backups
- monitored connection count
- explicit max connections per API/worker pool

### Redis

Redis backs distributed operational state:

- rate limits
- message context/session list caches
- message sequence allocation
- job queue via Redis Streams
- job event fanout via pub/sub
- processed locks for async message events

Use managed Redis HA or Sentinel/Cluster with persistence enabled. The app can
replay persisted job events from Postgres, but Redis instability still hurts
queue latency and realtime fanout.

### Object Storage

Use S3-compatible storage such as Cloudflare R2 for:

- attachments
- generated artifacts
- archived message payloads

Do not store production artifacts only on pod or node-local disks.

### Search And Vector Stores

Postgres-first search is acceptable for early production. If enhanced retrieval
is enabled:

- Elasticsearch/OpenSearch for full text
- Qdrant for message and memory vectors
- Vertex embeddings for text embeddings

Run these as managed services where possible. Avoid single-node search in
production.

### Live Voice

Live WebSocket traffic is handled by `agentapi` pods. Requirements:

- ingress/load balancer supports WebSocket upgrade
- idle timeout is greater than `AGENT_API_LIVE_SESSION_TIMEOUT`
- API pod termination grace exceeds `AGENT_API_SHUTDOWN_TIMEOUT`
- provider credential errors are surfaced through user-safe messages

No sticky session is required for normal HTTP/SSE replay, but one Live
WebSocket connection stays attached to the API pod that accepted it.

## Security Boundaries

Public API pods must not mount `/var/run/docker.sock`.

Skill shell execution should run on a separate worker/sandbox path. Safer
production options, in order:

1. Kubernetes Jobs with restricted runtime, network policy, and resource limits.
2. gVisor/Kata/Firecracker-backed execution nodes.
3. A dedicated remote Docker daemon reachable only from worker pods.
4. As a temporary bridge only: worker pods on isolated nodes with Docker socket
   access and strict network policy.

Keep:

```env
AGENT_API_SKILL_SANDBOX_NETWORK=none
AGENT_API_ALLOW_CUSTOM_WORKING_DIR=false
```

Set `AGENT_API_NETWORK_ALLOWLIST` in production when WebFetch/WebSearch egress
must be constrained.

## Traffic Flow

```text
Browser
  -> HTTPS ingress/CDN
  -> agentweb static files
  -> /v1, /healthz, /readyz, /metrics, /live/ws
  -> agentapi Service
  -> Postgres / Redis / R2 / Vertex

agentapi
  -> Redis Streams job queue
  -> Redis job event pub/sub

agentworker
  -> Redis Streams
  -> Postgres
  -> R2/S3
  -> Vertex/search/vector stores
```

## Scaling Guidance

Start with:

| Component | Replicas | Scale Signal |
| --- | ---: | --- |
| agentweb | 2 | HTTP RPS/CDN origin load |
| agentapi | 3 | CPU, p95 latency, active WebSockets |
| agentworker | 2 | Redis job stream lag, attachment queue age |

Do not scale API and worker from the same metric. They have different failure
modes and resource profiles.

