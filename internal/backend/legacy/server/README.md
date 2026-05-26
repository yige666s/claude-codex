# Legacy Server

This package contains the older standalone backend server implementation. It is
kept under `internal/backend/legacy/server` so existing behavior and tests remain
available while the active backend surface lives in `internal/backend/agentruntime`
and `internal/backend/agentapi`.

Do not add new backend framework work here. Prefer the shared packages in
`internal/backend/httpjson`, `internal/backend/httpclient`, `internal/backend/retry`,
`internal/backend/workers`, and the chi routes in `internal/backend/agentruntime`.
