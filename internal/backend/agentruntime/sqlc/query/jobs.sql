-- name: InsertJob :exec
INSERT INTO agent_jobs (
	job_id,
	user_id,
	session_id,
	loop_goal_id,
	type,
	status,
	content,
	attachments,
	error,
	created_at,
	updated_at,
	started_at,
	finished_at
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
	$11,
	$12,
	$13
);

-- name: GetJob :one
SELECT
	job_id,
	user_id,
	session_id,
	loop_goal_id,
	type,
	status,
	content,
	attachments,
	error,
	created_at,
	updated_at,
	started_at,
	finished_at
FROM agent_jobs
WHERE user_id = $1
  AND job_id = $2;

-- name: ListJobs :many
SELECT
	job_id,
	user_id,
	session_id,
	loop_goal_id,
	type,
	status,
	content,
	attachments,
	error,
	created_at,
	updated_at,
	started_at,
	finished_at
FROM agent_jobs
WHERE user_id = $1
  AND (sqlc.arg('session_id')::text = '' OR session_id = sqlc.arg('session_id')::text)
ORDER BY updated_at DESC;

-- name: UpdateJobStatus :exec
UPDATE agent_jobs
SET status = $1,
	error = $2,
	updated_at = $3,
	started_at = COALESCE(started_at, $4),
	finished_at = COALESCE($5, finished_at)
WHERE user_id = $6
  AND job_id = $7;

-- name: InsertJobEvent :exec
INSERT INTO agent_job_events (
	event_id,
	job_id,
	user_id,
	session_id,
	type,
	payload,
	created_at
) VALUES (
	$1,
	$2,
	$3,
	$4,
	$5,
	$6,
	$7
);

-- name: ListJobEvents :many
SELECT
	event_id,
	job_id,
	user_id,
	session_id,
	type,
	payload,
	created_at
FROM agent_job_events
WHERE user_id = $1
  AND job_id = $2
  AND (sqlc.arg('after_id')::text = '' OR event_id > sqlc.arg('after_id')::text)
ORDER BY event_id ASC
LIMIT NULLIF(sqlc.arg('limit_count')::int, 0);

-- name: DeleteSessionJobs :exec
DELETE FROM agent_jobs
WHERE user_id = $1
  AND session_id = $2;

-- name: DeleteUserJobs :exec
DELETE FROM agent_jobs
WHERE user_id = $1;

-- name: PruneTerminalJobsBefore :execrows
DELETE FROM agent_jobs
WHERE updated_at < $1
  AND status IN ($2, $3, $4);
