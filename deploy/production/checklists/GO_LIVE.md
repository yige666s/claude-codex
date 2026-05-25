# Production Go-Live Checklist

## Infrastructure

```text
[ ] Managed HA Postgres is provisioned with TLS.
[ ] Postgres PITR or equivalent continuous backup is enabled.
[ ] Managed HA Redis is provisioned with persistence.
[ ] R2/S3 bucket exists for artifacts and attachments.
[ ] Backup R2/S3 bucket or prefix exists with separate credentials.
[ ] Optional Elasticsearch/OpenSearch endpoint is provisioned.
[ ] Optional Qdrant endpoint is provisioned.
[ ] DNS records exist for frontend and API origins.
[ ] HTTPS certificates are active.
[ ] Load balancer supports SSE and WebSocket.
[ ] Load balancer idle timeout is greater than Live session timeout.
```

## Security

```text
[ ] JWT secret is at least 32 random bytes.
[ ] Admin token is at least 32 random bytes.
[ ] CORS allowed origins are exact production origins.
[ ] Cookie auth, if enabled, has Secure and CSRF enabled.
[ ] Vertex service account has only required Vertex permissions.
[ ] R2/S3 runtime key is scoped to the artifact bucket/prefix when possible.
[ ] Backup credentials are separate from runtime credentials.
[ ] API pods do not mount the host Docker socket.
[ ] Sandbox/skill execution runs on isolated worker nodes or a restricted runtime.
[ ] Network egress policy is defined for provider/object/search endpoints.
```

## Application Config

```text
[ ] deploy/production/check-env.mjs passes.
[ ] API workers are disabled on public API deployment.
[ ] Worker deployment has job/attachment/archive workers enabled.
[ ] AGENT_API_ARTIFACT_STORE=s3.
[ ] AGENT_API_RATE_LIMIT_BACKEND=redis.
[ ] AGENT_API_JOB_EVENT_FANOUT_ENABLED=true.
[ ] AGENT_API_JOB_QUEUE_REDIS_URL points to production Redis.
[ ] AGENT_API_SQL_MAX_OPEN_CONNS is safe for total replica count.
[ ] AGENT_API_LIVE_ENABLED matches launch decision.
[ ] AGENT_API_BACKUP_ENABLED=true.
```

## Observability

```text
[ ] /metrics is scraped by Prometheus or equivalent.
[ ] API and worker logs are centralized.
[ ] Alerts exist for API 5xx, readiness failures, Redis errors, Postgres errors, and job queue lag.
[ ] Alerts exist for Live error-rate spikes and provider credential errors.
[ ] Dashboards show API latency, active Live sessions, worker throughput, queue lag, Postgres connections, Redis memory, and object storage errors.
```

## Release

```text
[ ] Staging deploy uses the same image SHA intended for production.
[ ] Staging smoke tests pass.
[ ] Production deploy uses immutable commit SHA tags.
[ ] Rollback image SHA is known.
[ ] Restore drill has been completed within the last release cycle.
[ ] Production smoke tests are ready with a dedicated test account.
```

