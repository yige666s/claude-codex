# AgentAPI Production Deployment

This directory contains the production deployment plan, reference manifests,
configuration templates, and operations runbooks for AgentAPI.

The local Docker Compose stack is useful for development and the current test
server, but production should run as a multi-instance deployment with external
stateful services.

## Directory Map

| Path | Purpose |
| --- | --- |
| `.env.example` | Canonical production environment template. Copy into a secret manager or platform env store. |
| `check-env.mjs` | Preflight checker for production environment files. |
| `ARCHITECTURE.md` | Recommended HA production architecture and component boundaries. |
| `RELEASE_FLOW.md` | CI/CD, staging, promotion, rollback, and smoke-test flow. |
| `SIZING.md` | Server sizing tiers for config smoke, staging, and HA production. |
| `FRONTEND.md` | Frontend hosting, same-origin/split-origin, CORS, and cache policy. |
| `BACKUP_RESTORE.md` | Postgres, object storage, and workspace backup/restore runbook. |
| `GITHUB_ACTIONS_DEPLOY.md` | Current single-node test-server deploy flow. |
| `checklists/GO_LIVE.md` | Production launch checklist. |
| `runbooks/OPERATIONS.md` | Day-2 operations, incident, scaling, and rollback notes. |
| `kubernetes/` | Reference Kubernetes manifests for API, worker, web, ingress, config, and secrets. |

## Recommended Production Shape

```text
CDN / HTTPS load balancer
  -> agentweb static frontend
  -> agentapi Deployment, 3+ replicas

agentworker Deployment, 2+ replicas

External managed services:
  - Postgres HA
  - Redis HA
  - S3/R2 object storage
  - Vertex AI / Gemini Live
  - Qdrant and Elasticsearch/OpenSearch when enhanced search is enabled
```

The API deployment should handle user traffic, streaming, and Live WebSocket
connections. Worker deployments should handle durable jobs, attachment
processing, archive jobs, and async message indexing.

## Environment Preflight

Run the checker before any production rollout:

```bash
node deploy/production/check-env.mjs /path/to/prod.env
```

The checker only prints variable names and categories. It does not print secret
values.

## Deployment Rules

- Do not run production from `deploy/local/docker-compose.yml`.
- Do not run production Postgres, Redis, Elasticsearch, Qdrant, or object
  storage as single-node sidecar containers.
- Do not mount the host Docker socket into public API pods.
- Deploy immutable image tags, preferably commit SHA tags, not mutable `main`.
- Keep API and worker replicas independently scalable.
- Keep all durable data in Postgres, Redis, and object storage; local volumes
  are only for transient workspaces/staging.

## First Production Rollout

1. Provision managed Postgres, Redis, R2/S3, and optional search/vector stores.
2. Create runtime secrets from `.env.example`.
3. Run `check-env.mjs`.
4. Deploy `kubernetes/` manifests to staging.
5. Run smoke tests from `RELEASE_FLOW.md`.
6. Promote the same image tags to production.
7. Verify `/healthz`, `/readyz`, `/metrics`, login, chat, job, artifact, and
   Live basic connectivity.
