# Backend Package Layout

This directory is organized by runtime ownership and shared infrastructure.

## Agent API

- `agentapi/config`: command-line and environment configuration.
- `agentapi/bootstrap`: dependency construction and wiring.
- `agentapi/run`: server, worker, and shutdown lifecycle.

## Agent Runtime

- `agentruntime`: the main backend runtime surface. It owns the HTTP API,
  route registration, runtime orchestration, stores, jobs, WebSocket handling,
  migrations, and sqlc-generated database access.
- `agentruntime/migrations`: goose migrations for the runtime database schema.
- `agentruntime/sqlc` and `agentruntime/dbsqlc`: sqlc query definitions and
  generated database code.

## Bridge And Remote

- `bridge`: local bridge server/client code and remote work-secret handling.
- `remote`: remote WebSocket client lifecycle and reconnect behavior.
- `upstreamproxy`: proxy support for upstream backend calls.

## Shared Infrastructure

- `httpclient`: shared outbound HTTP client helpers for JSON, retries, logging,
  metrics, and request context propagation.
- `httpjson`: shared inbound HTTP JSON decoding, encoding, validation, and error
  response helpers.
- `retry`: shared retry policy and backoff behavior.
- `workers`: shared worker and scheduler lifecycle primitives.
- `pubsub`: shared publish/subscribe interfaces and in-memory implementation.
- `googleauth`: Google service-account authentication helpers.

## Product Services

- `services/*`: product-specific service clients and integrations. New service
  code should reuse shared infrastructure above instead of adding local HTTP,
  retry, or lifecycle implementations.

## Legacy

- `legacy/server`: older standalone backend server package retained for
  compatibility and regression tests. Do not add new framework work here unless
  it is needed to keep legacy behavior compiling. New HTTP backend work should
  go through `agentruntime` and `agentapi`.

## Migration Rules

- New HTTP routes should be registered through the chi router in
  `agentruntime/routes.go`.
- New request/response JSON handling should use `httpjson`.
- New outbound HTTP calls should use `httpclient`.
- New retry behavior should use `retry.Policy`.
- New goroutine or scheduler lifecycle code should use `workers.Group`.
- Postgres schema changes should be represented by goose migrations and sqlc
  queries where practical.
