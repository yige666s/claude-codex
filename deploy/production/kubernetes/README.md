# Kubernetes Reference Manifests

These manifests are production-oriented examples, not a complete platform
installer. They assume external managed Postgres, Redis, object storage,
provider credentials, and optional search/vector stores.

Apply order:

```bash
kubectl apply -f namespace.yaml
kubectl -n agentapi-prod apply -f secrets.example.yaml   # replace placeholders first
kubectl -n agentapi-prod apply -k .
```

For real production, store secrets in your cloud secret manager or External
Secrets controller instead of applying `secrets.example.yaml` directly.

## Image Tags

Set immutable image tags with kustomize or your CD system:

```bash
kubectl -n agentapi-prod set image deployment/agentapi \
  agentapi=ghcr.io/yige666s/claude-codex/agentapi:<commit-sha>

kubectl -n agentapi-prod set image deployment/agentworker \
  agentworker=ghcr.io/yige666s/claude-codex/agentapi:<commit-sha>

kubectl -n agentapi-prod set image deployment/agentweb \
  agentweb=ghcr.io/yige666s/claude-codex/agentweb:<commit-sha>
```

## API vs Worker

`agentapi` handles public traffic and disables worker loops.

`agentworker` uses the same image but is not exposed through an external
Service. It enables job, attachment, archive, and message-event workers.

Longer-term, a dedicated `cmd/agentworker` entrypoint would make this cleaner,
but the current split is enough to isolate traffic and scale workers
independently.

