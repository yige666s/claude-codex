# Backend Package Layout

This directory has three top-level ownership areas: API entrypoints, the agent
runtime and its infrastructure, and product services.

## Agent API

- `agentapi/config`: command-line and environment configuration.
- `agentapi/bootstrap`: dependency construction and wiring.
- `agentapi/run`: server, worker, and shutdown lifecycle.
- `agentapi/bridge`: local bridge server/client and remote work-secret handling.
- `agentapi/legacy/server`: older standalone HTTP server kept for compatibility.

## Agent Runtime

- `agentruntime`: the main backend runtime surface. It owns the HTTP API,
  route registration, runtime orchestration, stores, jobs, WebSocket handling,
  migrations, and sqlc-generated database access.
- `agentruntime/migrations`: goose migrations for the runtime database schema.
- `agentruntime/sqlc` and `agentruntime/dbsqlc`: sqlc query definitions and
  generated database code.
- `agentruntime/remote`: remote WebSocket client lifecycle and reconnect behavior.
- `agentruntime/upstreamproxy`: proxy support for upstream backend calls.
- `agentruntime/sandbox*`: published artifacts, policy, broker, node protocol,
  OCI execution, staging and bounded transfer.

## Shared Infrastructure

- `agentruntime/httpclient`: shared outbound HTTP client helpers for JSON, retries, logging,
  metrics, and request context propagation.
- `agentruntime/httpjson`: shared inbound HTTP JSON decoding, encoding, validation, and error
  response helpers.
- `agentruntime/retry`: shared retry policy and backoff behavior.
- `agentruntime/workers`: shared worker and scheduler lifecycle primitives.
- `agentruntime/pubsub`: shared publish/subscribe interfaces and in-memory implementation.
- `agentruntime/googleauth`: Google service-account authentication helpers.

## Product Services

- `services/*`: product-specific service clients and integrations. New service
  code should reuse shared infrastructure above instead of adding local HTTP,
  retry, or lifecycle implementations.

## Migration Rules

- New HTTP routes should be registered through the chi router in
  `agentruntime/routes.go`.
- New request/response JSON handling should use `agentruntime/httpjson`.
- New outbound HTTP calls should use `agentruntime/httpclient`.
- New retry behavior should use `agentruntime/retry.Policy`.
- New goroutine or scheduler lifecycle code should use `agentruntime/workers.Group`.
- Keep new HTTP routes in `agentruntime`, not `agentapi/legacy/server`.
- Postgres schema changes should be represented by goose migrations and sqlc
  queries where practical.
