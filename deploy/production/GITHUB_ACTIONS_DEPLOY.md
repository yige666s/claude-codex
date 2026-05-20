# GitHub Actions main-branch deploy

This deployment path is for the test server at `155.94.154.240`.

Flow:

```text
push to main
  -> GitHub Actions deploy-main workflow
  -> build agentapi / agentweb images on GitHub-hosted runners
  -> push images to GitHub Container Registry (ghcr.io)
  -> SSH to the server
  -> refresh /opt/agentapi/deploy.sh from the checked-out repository
  -> /opt/agentapi/deploy.sh with AGENTAPI_DEPLOY_MODE=pull
  -> git fetch/reset main
  -> docker compose pull agentapi agentweb
  -> docker compose up -d --no-build
  -> health check
```

The test server should not build application images locally. It is a small host
and local Go/Node builds compete with Elasticsearch and the API process. The
workflow publishes:

```text
ghcr.io/yige666s/claude-codex/agentapi:<commit-sha>
ghcr.io/yige666s/claude-codex/agentweb:<commit-sha>
ghcr.io/yige666s/claude-codex/agentapi:main
ghcr.io/yige666s/claude-codex/agentweb:main
```

Deploys use the immutable commit SHA tags.

## Server layout

```text
/opt/agentapi/deploy.sh      # server-side deploy entrypoint
/opt/agentapi/repo           # checked-out repository
/opt/agentapi/.env           # server-only runtime secrets and config
/opt/agentapi/.env.example   # copied template
```

`/opt/agentapi/.env` must not be committed. It should contain the R2, auth,
LLM, Redis, and Postgres settings for the server.

## Required GitHub Actions secrets

Configure these in GitHub repository settings:

```text
DEPLOY_HOST=155.94.154.240
DEPLOY_USER=root
DEPLOY_PORT=22
DEPLOY_SSH_KEY=<private key that can SSH to the server>
```

The public half of the deploy key must be present in
`/root/.ssh/authorized_keys` on the server.

The workflow uses the built-in `GITHUB_TOKEN` to push to GHCR and logs the
server into GHCR for the deploy pull. Repository Actions permissions must allow
package writes.

## Manual server test

Run this on the server after `/opt/agentapi/.env` is configured:

```bash
/opt/agentapi/deploy.sh
```

To test the GHCR pull path manually:

```bash
AGENTAPI_DEPLOY_MODE=pull \
AGENT_API_IMAGE=ghcr.io/yige666s/claude-codex/agentapi:main \
AGENT_WEB_IMAGE=ghcr.io/yige666s/claude-codex/agentweb:main \
/opt/agentapi/deploy.sh
```

For a config-less smoke test that only checks build/startup wiring, skip the
readiness probe:

```bash
AGENTAPI_SKIP_HEALTHCHECK=1 /opt/agentapi/deploy.sh
```

Production-like deployments should keep the readiness probe enabled.
