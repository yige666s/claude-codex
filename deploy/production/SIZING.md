# Production Sizing Guide

This guide describes practical server sizing for AgentAPI production-like
environments. Use it together with `ARCHITECTURE.md`; sizing does not replace
the production architecture requirement that durable services run outside the
application nodes.

## Tiers

### prod-config-smoke

Use this tier only to verify production configuration correctness: environment
variables, image startup, migrations, TLS, CORS, cookies, Postgres, Redis,
object storage, Vertex credentials, worker queue, and basic Live connectivity.

| Component | Minimum |
| --- | ---: |
| App server | 1 node, 2 vCPU, 4 GB RAM, 40 GB SSD |
| Postgres | 1 vCPU, 2 GB RAM |
| Redis | 512 MB to 1 GB RAM |
| `agentweb` | 1 replica |
| `agentapi` | 1 replica |
| `agentworker` | 1 replica |
| Object storage | External R2/S3 |

For this smoke tier, app, Postgres, and Redis may temporarily run on the same
server. Do not treat this as production availability or capacity evidence.

### prod-staging

Use this tier for real E2E, internal beta, and release validation.

| Component | Recommended |
| --- | ---: |
| App server | 1 node, 4 vCPU, 8 GB RAM, 80 GB SSD |
| Better staging | 2 nodes, 4 vCPU, 8 GB RAM each |
| Postgres | 2 vCPU, 4 GB RAM, automated backups |
| Redis | 1 GB to 2 GB RAM |
| `agentweb` | 1 to 2 replicas |
| `agentapi` | 1 to 2 replicas |
| `agentworker` | 1 to 2 replicas |
| Object storage | External R2/S3 |

Prefer external Postgres and Redis even in staging when the goal is to validate
the production topology.

### prod-ha

Use this tier for the first real production deployment.

| Component | Recommended baseline |
| --- | ---: |
| App nodes | 3 nodes, 4 vCPU, 16 GB RAM, 100 GB SSD each |
| Heavier Live/job usage | 3 nodes, 8 vCPU, 32 GB RAM each |
| Postgres | Managed HA, 2 to 4 vCPU, 8 to 16 GB RAM |
| Redis | Managed HA, 2 vCPU, 4 to 8 GB RAM |
| `agentweb` | 2 replicas |
| `agentapi` | 3 replicas |
| `agentworker` | 2 replicas |
| Object storage | External R2/S3 |

If Live, artifact generation, or file processing becomes dominant, move workers
to a separate node pool and size that pool independently.

## Kubernetes Resource Baseline

The reference manifests start with these container requests and limits:

| Component | Replicas | Request | Limit |
| --- | ---: | --- | --- |
| `agentweb` | 2 | 100m CPU, 128 MiB RAM | 500m CPU, 512 MiB RAM |
| `agentapi` | 3 | 500m CPU, 512 MiB RAM | 2 CPU, 2 GiB RAM |
| `agentworker` | 2 | 500m CPU, 768 MiB RAM | 2 CPU, 3 GiB RAM |

Tune these values from metrics rather than guessing. API scaling should follow
HTTP latency, 5xx rate, active Live sessions, and WebSocket pressure. Worker
scaling should follow Redis stream lag, pending jobs, attachment queue age, and
provider quota usage.

## Placement Rules

- Production should not run app containers, Postgres, and Redis on one server.
- Keep Postgres and Redis managed or on dedicated HA infrastructure.
- Keep artifact and attachment bytes in R2/S3, not node-local disks.
- Keep API and worker replicas independently scalable.
- Use single-server deployment only for configuration smoke tests.

## Upgrade Path

1. Start with `prod-config-smoke` for deployment correctness.
2. Move to `prod-staging` before inviting internal users.
3. Promote to `prod-ha` before real production traffic.
4. Split worker nodes when job execution or file processing affects API latency.
5. Add dedicated search/vector infrastructure only after Postgres-first search
   and memory retrieval show measurable bottlenecks.
