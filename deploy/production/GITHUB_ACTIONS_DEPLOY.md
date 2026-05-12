# GitHub Actions main-branch deploy

This deployment path is for the test server at `155.94.154.240`.

Flow:

```text
push to main
  -> GitHub Actions deploy-main workflow
  -> SSH to the server
  -> /opt/agentapi/deploy.sh
  -> git fetch/reset main
  -> docker compose up -d --build
  -> health check
```

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

## Manual server test

Run this on the server after `/opt/agentapi/.env` is configured:

```bash
/opt/agentapi/deploy.sh
```

For a config-less smoke test that only checks build/startup wiring, skip the
readiness probe:

```bash
AGENTAPI_SKIP_HEALTHCHECK=1 /opt/agentapi/deploy.sh
```

Production-like deployments should keep the readiness probe enabled.
