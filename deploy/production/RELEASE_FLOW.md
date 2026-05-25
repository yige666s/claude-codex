# Production Release Flow

## Image Build

GitHub Actions should build and push both images for every merge to `main`:

```text
ghcr.io/yige666s/claude-codex/agentapi:<commit-sha>
ghcr.io/yige666s/claude-codex/agentweb:<commit-sha>
```

Production deploys should use the commit SHA tag. The mutable `main` tag is
acceptable for test environments only.

## Promotion Stages

1. Build images.
2. Deploy to staging.
3. Run environment preflight:

   ```bash
   node deploy/production/check-env.mjs /path/to/staging.env
   ```

4. Run smoke tests:
   - login
   - chat
   - job creation and event replay
   - attachment upload
   - artifact generation/download
   - object storage write/read
   - Live WebSocket connection
5. Require manual approval for production.
6. Deploy the same image SHA to production.
7. Run production smoke tests with a dedicated test account.

## Deployment Order

For a normal release:

1. Apply config and secret changes.
2. Run migrations through the first new `agentapi` pod startup.
3. Roll API pods with max unavailable 1.
4. Roll worker pods.
5. Roll web pods/static frontend.
6. Verify readiness and product smoke tests.

If a release contains schema changes, keep the migration compatible with both
old and new app versions before rolling workers broadly.

## Rollback

Rollback target:

- previous `agentapi:<commit-sha>`
- previous `agentweb:<commit-sha>`
- unchanged secrets/config unless the release changed config

Rollback command shape for Kubernetes:

```bash
kubectl -n agentapi-prod rollout undo deployment/agentapi
kubectl -n agentapi-prod rollout undo deployment/agentworker
kubectl -n agentapi-prod rollout undo deployment/agentweb
```

If a release includes destructive schema changes, do not rollback blindly.
Restore from backup only after an explicit incident decision.

## Required Gates

- `go test ./...`
- `npm test` in `apps/web`
- `npm run build` in `apps/web`
- `git diff --check`
- `check-env.mjs` against the target environment
- staging smoke tests

## Smoke Test Checklist

Use a non-admin test account.

```text
[ ] GET /healthz returns 200
[ ] GET /readyz returns 200
[ ] Login succeeds
[ ] Create new chat session
[ ] Send normal chat message and receive final assistant response
[ ] Upload small text or image attachment
[ ] Start a generated artifact job
[ ] Job event stream reconnect/replay works
[ ] Artifact preview/download works
[ ] Global search returns recent message
[ ] Live WebSocket connects and handles credential/permission errors cleanly
[ ] Admin health panel loads with Postgres, Redis, worker, and Live status
```

