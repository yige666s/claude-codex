# Local AgentAPI Stack

This compose stack runs the production-oriented local baseline:

- `agentapi` on `http://localhost:8081`
- Postgres on `localhost:5432`
- Redis on `localhost:6379`
- Optional MinIO/S3 on `localhost:9000` via the `minio` profile
- Optional Elasticsearch and Qdrant search infrastructure via the `search` profile

Start it with:

```bash
docker compose -f deploy/local/docker-compose.yml up --build
```

The default stack uses SQL storage, S3-compatible artifacts from the configured
endpoint, rate limiting disabled by default, Redis message context hot cache, JWT auth, and
the built-in user system.
Override provider settings with
environment variables such as `AGENT_API_LLM_PROVIDER`, `NVIDIA_API_KEY`,
`OPENAI_API_KEY`, `DASHSCOPE_API_KEY`, `GEMINI_API_KEY`, or `VERTEX_*`.
Runtime model selection is managed from the Admin UI and stored in SQL.
For deterministic local infrastructure tests that should not call an external
model provider, set `AGENT_API_LLM_PROVIDER=simple`, `AGENT_API_MODEL=simple`,
and route models to `simple`; this uses the local simple planner while still
exercising HTTP, job queue, Redis Streams, SSE, SQL, and the web UI.
Live voice defaults to xAI Realtime and requires `XAI_API_KEY` plus the
`XAI_LIVE_*` settings. Vertex Live remains available by setting
`AGENT_API_LIVE_PROVIDER=vertex`; in that mode prefer `VERTEX_ACCESS_TOKEN` or
`GOOGLE_APPLICATION_CREDENTIALS_JSON` for test environments. If you mount a
local service-account file with `AGENT_API_SECRETS_MOUNT`, set
`AGENT_API_CONTAINER_GOOGLE_APPLICATION_CREDENTIALS=/run/agentapi/secrets/vertex-service-account.json`.
Attachment upload uses the presigned S3-compatible flow: AgentAPI signs the
upload, the browser PUTs directly to the object store, then confirms metadata
back to AgentAPI. File-backed local runs fall back to the legacy multipart path.
The message attachment worker is enabled by default for SQL deployments and
processes pending per-message attachments into object-store thumbnails and extracted
text objects.

To use local MinIO instead of a remote S3/R2 endpoint:

```bash
docker compose --profile minio -f deploy/local/docker-compose.yml up --build
```

Run the message-module verification stack:

```bash
deploy/local/verify-message-module.sh
```

To include Elasticsearch and Qdrant in the local stack:

```bash
docker compose --profile search -f deploy/local/docker-compose.yml up --build
```

The local stack defaults semantic retrieval to NVIDIA embeddings:
`AGENT_API_MESSAGE_SEARCH_EMBEDDING_PROVIDER=nvidia`,
`AGENT_API_MESSAGE_SEARCH_EMBEDDING_ENDPOINT=https://integrate.api.nvidia.com/v1`,
`AGENT_API_MESSAGE_SEARCH_EMBEDDING_MODEL=nvidia/llama-nemotron-embed-1b-v2`,
and `AGENT_API_MESSAGE_SEARCH_EMBEDDING_DIMENSIONS=768`. Set
`AGENT_API_MESSAGE_SEARCH_BACKEND=semantic` or `hybrid` plus
`AGENT_API_MESSAGE_SEARCH_EMBEDDING_API_KEY` to query Qdrant. L2 episodic
memory retrieval uses the same embedding model and can rerank the top 50
vector candidates with
`AGENT_API_MEMORY_VECTOR_RERANK_ENDPOINT=https://ai.api.nvidia.com/v1/retrieval/nvidia/llama-nemotron-rerank-1b-v2/reranking` and
`AGENT_API_MEMORY_VECTOR_RERANK_MODEL=nvidia/llama-nemotron-rerank-1b-v2`,
returning the top 5 memories by default.
With `AGENT_API_MESSAGE_SEARCH_BACKEND=elasticsearch` or `hybrid`, local compose
defaults `AGENT_API_MESSAGE_SEARCH_INDEX_MANAGEMENT_ENABLED=true` to bootstrap ES
ILM, rollover templates, and IK analyzer mappings. AgentAPI also runs a
background full-text backfill that copies existing SQL messages into ES so saved
chat history is searchable without per-query SQL fallback. The local
Elasticsearch image is built from `deploy/local/elasticsearch-ik.Dockerfile` and
installs the IK plugin that provides the default `ik_max_word` / `ik_smart`
analyzers. Override `AGENT_API_ELASTICSEARCH_VERSION` and
`AGENT_API_ELASTICSEARCH_IK_PLUGIN_URL` when upgrading Elasticsearch or testing a
pinned plugin artifact.

AgentAPI also defaults `AGENT_API_MESSAGE_CONTEXT_CACHE_BACKEND=redis` locally
and stores loaded session-context windows in Redis DB 1 with prefix
`agentapi:message:ctx`. Override `AGENT_API_MESSAGE_CONTEXT_CACHE_TTL` or set
the backend to `memory` / `none` when testing cache behavior.

Kafka message events are available through the optional `kafka` profile:

```bash
AGENT_API_MESSAGE_EVENTS_BACKEND=kafka \
AGENT_API_MESSAGE_EVENTS_KAFKA_CONSUMER_ENABLED=true \
AGENT_API_MESSAGE_SEARCH_BACKEND=elasticsearch \
docker compose --profile kafka --profile search -f deploy/local/docker-compose.yml up --build
```

The producer writes `message.created` events to `agent.messages`. The built-in
consumer uses Redis processed locks and drives the configured search indexers:
Elasticsearch/OpenSearch for full-text backends and Qdrant for semantic or
hybrid backends. The local verification defaults to Elasticsearch so the
Redpanda pipeline can be tested without depending on an external embedding
provider; set `AGENT_API_MESSAGE_SEARCH_BACKEND=hybrid` explicitly when testing
the semantic branch as well.

Run the Redpanda message-event verification:

```bash
AGENT_API_MESSAGE_EVENTS_BACKEND=dual deploy/local/verify-redpanda-message-events.sh
AGENT_API_MESSAGE_EVENTS_BACKEND=kafka deploy/local/verify-redpanda-message-events.sh
```

The verification starts Redpanda plus the search services, publishes a real chat
message, confirms the event is present in the configured Redpanda topic
(isolated per run by default), and waits for `/v1/search/messages` to return the
asynchronously indexed message.

Useful checks:

```bash
curl http://localhost:8081/healthz
curl http://localhost:8081/readyz
curl http://localhost:8081/metrics
```

Reset local state:

```bash
docker compose -f deploy/local/docker-compose.yml down -v
```

Notes:

- The host `.claude` directory is mounted read-only at `/workspace/.claude` so
  local skills are available in the container.
- The default skill shell runner is `local` inside the API container. To test
  Docker-backed skill sandboxes from inside compose, run with
  `AGENT_API_SKILL_SHELL_RUNNER=docker` and provide a Docker socket or remote
  Docker endpoint as privileged infrastructure.
- `docker compose stop agentapi` sends `SIGTERM`; AgentAPI drains for
  `AGENT_API_SHUTDOWN_TIMEOUT` and compose allows `AGENT_API_STOP_GRACE_PERIOD`
  before force-stopping the container.
