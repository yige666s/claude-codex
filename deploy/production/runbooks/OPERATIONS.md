# Production Operations Runbook

## Daily Checks

```bash
kubectl -n agentapi-prod get deploy,pods,hpa
kubectl -n agentapi-prod get events --sort-by=.lastTimestamp | tail -40
curl -fsS https://api.example.com/healthz
curl -fsS https://api.example.com/readyz
```

Review:

- API pod restarts
- worker pod restarts
- job queue lag
- Redis connection errors
- Postgres connection saturation
- R2/S3 errors
- Live error codes and disconnect rate

## API Incident

Symptoms:

- `/readyz` failing
- elevated 5xx
- chat streams disconnecting
- login failures

Actions:

1. Check current rollout:

   ```bash
   kubectl -n agentapi-prod rollout status deployment/agentapi
   kubectl -n agentapi-prod logs deploy/agentapi --tail=200
   ```

2. Check dependencies:

   ```bash
   kubectl -n agentapi-prod describe deploy/agentapi
   ```

3. If a new release caused the incident, rollback:

   ```bash
   kubectl -n agentapi-prod rollout undo deployment/agentapi
   ```

4. If dependency-related, fail over or scale the dependency according to the
   provider runbook.

## Worker Incident

Symptoms:

- jobs remain queued
- attachment extraction stalls
- archive backlog grows

Actions:

```bash
kubectl -n agentapi-prod logs deploy/agentworker --tail=300
kubectl -n agentapi-prod rollout status deployment/agentworker
kubectl -n agentapi-prod scale deployment/agentworker --replicas=4
```

If Redis was unavailable, workers should claim idle pending stream entries after
`AGENT_API_JOB_QUEUE_CLAIM_IDLE`.

## Live Incident

Symptoms:

- Live connects slowly
- microphone starts but no transcript
- credential/provider errors
- frequent reconnects

Actions:

1. Check ingress WebSocket support and idle timeout.
2. Check API logs for structured Live error codes.
3. Verify Vertex credentials and project/location.
4. Temporarily disable Live if needed:

   ```bash
   kubectl -n agentapi-prod set env deployment/agentapi AGENT_API_LIVE_ENABLED=false
   ```

5. Re-enable after provider credential or network issue is resolved.

## Scaling

Scale API for HTTP/Live load:

```bash
kubectl -n agentapi-prod scale deployment/agentapi --replicas=5
```

Scale workers for queue lag:

```bash
kubectl -n agentapi-prod scale deployment/agentworker --replicas=6
```

Before scaling far, confirm Postgres max connections and Vertex/provider quotas.

## Secret Rotation

1. Add new secret values to the secret manager.
2. Update Kubernetes Secret.
3. Roll API and worker deployments.
4. Verify login, chat, artifact storage, and provider calls.
5. Revoke old credentials.

For JWT secret rotation, active access/refresh tokens signed with the old
secret may be invalidated unless dual-secret support is added.

## Backup And Restore

Use `../BACKUP_RESTORE.md`. Do not restore directly into live production
without a maintenance window and explicit incident approval.

