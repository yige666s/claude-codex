# Local AgentAPI Stack

This compose stack runs the production-oriented local baseline:

- `agentapi` on `http://localhost:8081`
- Postgres on `localhost:5432`
- Redis on `localhost:6379`
- Cloudflare R2 for attachments and artifacts

Start it with:

```bash
export VERTEX_ACCESS_TOKEN="$(gcloud auth print-access-token)"
export VERTEX_PROJECT_ID="vigilant-router-378708"
export VERTEX_LOCATION="us-central1"
export AGENT_API_ARTIFACT_S3_ACCESS_KEY="REPLACE_WITH_R2_ACCESS_KEY_ID"
export AGENT_API_ARTIFACT_S3_SECRET_KEY="REPLACE_WITH_R2_SECRET_ACCESS_KEY"
docker compose -f deploy/local/docker-compose.yml up --build
```

The default stack uses Vertex (`gemini-2.5-pro`), SQL storage, Cloudflare R2
artifacts, Redis rate limiting, JWT auth, and the built-in user system. Override
provider settings with environment variables such as `AGENT_API_LLM_PROVIDER`,
`AGENT_API_MODEL`, `OPENAI_API_KEY`, `DASHSCOPE_API_KEY`, `GEMINI_API_KEY`, or
`VERTEX_*`.

Cloudflare R2 defaults:

```bash
export AGENT_API_ARTIFACT_S3_ENDPOINT="5c11ff96d03d238d51aef31150a87101.r2.cloudflarestorage.com"
export AGENT_API_ARTIFACT_S3_BUCKET="agentapi"
export AGENT_API_ARTIFACT_S3_PREFIX="local"
export AGENT_API_ARTIFACT_S3_SSL=true
```

Use an R2 S3 access key and secret from the Cloudflare dashboard. The
`CLOUDFLARE_API_TOKEN` used by `wrangler` cannot be used as the S3 secret.

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
