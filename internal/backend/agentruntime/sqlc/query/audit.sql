-- name: InsertAuditRecord :exec
INSERT INTO agent_audit_logs (
	id,
	event,
	user_id,
	session_id,
	job_id,
	asset_id,
	request_id,
	ip_address,
	user_agent,
	metadata,
	created_at
) VALUES (
	$1,
	$2,
	$3,
	$4,
	$5,
	$6,
	$7,
	$8,
	$9,
	$10,
	$11
);

-- name: ListAuditRecords :many
SELECT
	id,
	event,
	user_id,
	session_id,
	job_id,
	asset_id,
	request_id,
	ip_address,
	user_agent,
	metadata,
	created_at
FROM agent_audit_logs
WHERE created_at >= sqlc.arg(since)
  AND (sqlc.narg(user_id)::text IS NULL OR user_id = sqlc.narg(user_id)::text)
  AND (sqlc.narg(event)::text IS NULL OR event = sqlc.narg(event)::text)
ORDER BY created_at DESC;
