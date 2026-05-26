-- name: GetRuntimeConfig :one
SELECT payload
FROM agent_runtime_config
WHERE config_key = $1;

-- name: UpsertRuntimeConfig :exec
INSERT INTO agent_runtime_config (config_key, payload, updated_at)
VALUES ($1, $2, $3)
ON CONFLICT (config_key) DO UPDATE SET
	payload = excluded.payload,
	updated_at = excluded.updated_at;
