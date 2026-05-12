# AgentAPI Backup And Restore Runbook

This runbook covers the production data owned by `cmd/agentapi`:

- Postgres: users, refresh tokens, sessions, messages, memory, jobs, job events,
  artifact metadata, LLM usage, audit logs, and schema migrations.
- Object storage: attachment and generated artifact bytes under the configured
  Cloudflare R2 bucket and prefix.
- Runtime workspaces: per-user workspace files under
  `AGENT_API_USER_WORKSPACE_ROOT` when those files are not purely temporary.

## Recovery Targets

Set these with the product and operations team before launch:

- RPO: maximum acceptable data loss window.
- RTO: maximum acceptable restore time.
- Retention: daily backups for at least the configured data-retention period.
- Encryption: database dumps and object snapshots must be encrypted at rest.
- Access: backup credentials must be separate from application credentials.

## Backup

### Postgres

Use a custom-format dump so restore can be parallelized and selectively
inspected:

```bash
export DATABASE_URL='postgres://agentapi:REPLACE_ME@postgres.example.com:5432/agentapi?sslmode=require'
backup_id="$(date -u +%Y%m%dT%H%M%SZ)"
pg_dump "$DATABASE_URL" \
  --format=custom \
  --no-owner \
  --no-acl \
  --file="agentapi-postgres-${backup_id}.dump"
```

Recommended schedule:

- Full logical dump daily.
- WAL/PITR or managed database continuous backups for tighter RPO.
- Verify `agent_schema_migrations` is included in every dump.

### Object Storage

Mirror the configured Cloudflare R2 bucket/prefix through the S3-compatible API:

```bash
export S3_ALIAS=agentapi-prod
export CLOUDFLARE_ACCOUNT_ID='5c11ff96d03d238d51aef31150a87101'
export S3_ENDPOINT="https://${CLOUDFLARE_ACCOUNT_ID}.r2.cloudflarestorage.com"
export S3_ACCESS_KEY='REPLACE_WITH_R2_ACCESS_KEY_ID'
export S3_SECRET_KEY='REPLACE_WITH_R2_SECRET_ACCESS_KEY'
export S3_BUCKET='agentapi'
export S3_PREFIX='prod'

mc alias set "$S3_ALIAS" "$S3_ENDPOINT" "$S3_ACCESS_KEY" "$S3_SECRET_KEY"
backup_id="$(date -u +%Y%m%dT%H%M%SZ)"
mc mirror --overwrite "$S3_ALIAS/$S3_BUCKET/$S3_PREFIX" "./agentapi-objects-${backup_id}"
```

For production R2, prefer bucket versioning/lifecycle-protected snapshots where
available and keep R2 S3 keys separate from the `CLOUDFLARE_API_TOKEN` used for
bucket administration.

### Workspaces

If user workspaces contain durable generated files outside object storage, back
up the workspace root:

```bash
tar -C /var/lib/agentapi \
  -czf "agentapi-workspaces-${backup_id}.tar.gz" \
  workspaces
```

When all durable outputs are written to R2 artifacts, workspace backups
can be shorter-lived operational snapshots.

## Restore

Restore into a fresh environment first. Do not restore over a live production
database without an explicit maintenance window.

### Postgres

```bash
export RESTORE_DATABASE_URL='postgres://agentapi:REPLACE_ME@postgres-restore.example.com:5432/agentapi?sslmode=require'
createdb "$RESTORE_DATABASE_URL"
pg_restore "$RESTORE_DATABASE_URL" \
  --clean \
  --if-exists \
  --no-owner \
  --no-acl \
  --jobs=4 \
  agentapi-postgres-YYYYMMDDTHHMMSSZ.dump
```

After restore:

```sql
SELECT version, applied_at FROM agent_schema_migrations ORDER BY version;
SELECT COUNT(*) FROM agent_users;
SELECT COUNT(*) FROM agent_sessions;
SELECT COUNT(*) FROM agent_messages;
SELECT COUNT(*) FROM agent_artifacts WHERE deleted_at IS NULL;
```

### Object Storage

Restore object bytes before exposing the API:

```bash
mc alias set agentapi-restore "$S3_ENDPOINT" "$S3_ACCESS_KEY" "$S3_SECRET_KEY"
mc mb --ignore-existing agentapi-restore/agentapi
mc mirror --overwrite ./agentapi-objects-YYYYMMDDTHHMMSSZ agentapi-restore/agentapi/prod
```

### Workspaces

```bash
tar -C /var/lib/agentapi -xzf agentapi-workspaces-YYYYMMDDTHHMMSSZ.tar.gz
chown -R agentapi:agentapi /var/lib/agentapi/workspaces
```

## Verification

Before routing traffic to the restored API:

```bash
curl -fsS https://api.example.com/healthz
curl -fsS https://api.example.com/readyz
curl -fsS https://api.example.com/metrics
```

Then verify these product paths with a non-production test account:

- Login and refresh token rotation.
- Session list and message history.
- Job event replay for an existing completed job.
- Attachment or artifact download.
- New artifact creation to the restored object bucket.

## Local Compose Drill

For local restore rehearsal:

```bash
docker compose -f deploy/local/docker-compose.yml down -v
docker compose -f deploy/local/docker-compose.yml up -d postgres redis

export DATABASE_URL='postgres://agentapi:agentapi@localhost:5432/agentapi?sslmode=disable'
pg_restore "$DATABASE_URL" --clean --if-exists --no-owner --no-acl agentapi-postgres-YYYYMMDDTHHMMSSZ.dump

export CLOUDFLARE_ACCOUNT_ID='5c11ff96d03d238d51aef31150a87101'
export AGENT_API_ARTIFACT_S3_ENDPOINT="${CLOUDFLARE_ACCOUNT_ID}.r2.cloudflarestorage.com"
export AGENT_API_ARTIFACT_S3_ACCESS_KEY='REPLACE_WITH_R2_ACCESS_KEY_ID'
export AGENT_API_ARTIFACT_S3_SECRET_KEY='REPLACE_WITH_R2_SECRET_ACCESS_KEY'
export AGENT_API_ARTIFACT_S3_BUCKET='agentapi'
export AGENT_API_ARTIFACT_S3_PREFIX='restore-drill'

mc alias set r2 "https://${AGENT_API_ARTIFACT_S3_ENDPOINT}" "$AGENT_API_ARTIFACT_S3_ACCESS_KEY" "$AGENT_API_ARTIFACT_S3_SECRET_KEY"
mc mirror --overwrite ./agentapi-objects-YYYYMMDDTHHMMSSZ "r2/${AGENT_API_ARTIFACT_S3_BUCKET}/${AGENT_API_ARTIFACT_S3_PREFIX}"

docker compose -f deploy/local/docker-compose.yml up -d agentapi
curl -fsS http://localhost:8081/readyz
```

## Operational Notes

- Back up Postgres and object storage at approximately the same timestamp.
- Keep backup manifests with backup ID, source commit/image tag, schema
  migration versions, object bucket/prefix, and restore verification results.
- Test restores on a schedule; untested backups are only hopeful files.
- Rotate credentials immediately if a backup location or restore workstation is
  suspected to be compromised.
