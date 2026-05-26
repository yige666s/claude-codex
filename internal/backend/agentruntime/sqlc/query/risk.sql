-- name: InsertRiskEvent :exec
INSERT INTO agent_risk_events (
	id,
	user_id,
	session_id,
	job_id,
	asset_id,
	request_id,
	ip_address,
	operation,
	reason,
	risk_level,
	score_delta,
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
	$11,
	$12,
	$13
);

-- name: InsertRiskReview :exec
INSERT INTO agent_risk_reviews (
	id,
	risk_event_id,
	user_id,
	session_id,
	job_id,
	asset_id,
	request_id,
	ip_address,
	operation,
	reason,
	risk_level,
	priority,
	status,
	assigned_to,
	resolution,
	note,
	metadata,
	created_at,
	updated_at,
	resolved_at
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
	$13,
	$14,
	$15,
	$16,
	$17,
	$18,
	$19,
	$20
);

-- name: GetRiskScore :one
SELECT
	subject_type,
	subject_id,
	score,
	risk_level,
	event_count,
	last_event_at,
	updated_at
FROM agent_risk_scores
WHERE subject_type = $1
  AND subject_id = $2;

-- name: InsertRiskScore :exec
INSERT INTO agent_risk_scores (
	subject_type,
	subject_id,
	score,
	risk_level,
	event_count,
	last_event_at,
	updated_at
) VALUES (
	$1,
	$2,
	$3,
	$4,
	$5,
	$6,
	$7
);

-- name: UpdateRiskScore :exec
UPDATE agent_risk_scores
SET score = $1,
	risk_level = $2,
	event_count = $3,
	last_event_at = $4,
	updated_at = $5
WHERE subject_type = $6
  AND subject_id = $7;

-- name: ListRiskEvents :many
SELECT
	id,
	user_id,
	session_id,
	job_id,
	asset_id,
	request_id,
	ip_address,
	operation,
	reason,
	risk_level,
	score_delta,
	metadata,
	created_at
FROM agent_risk_events
WHERE created_at >= sqlc.arg('since')
  AND (sqlc.arg('user_id')::text = '' OR user_id = sqlc.arg('user_id')::text)
  AND (sqlc.arg('session_id')::text = '' OR session_id = sqlc.arg('session_id')::text)
  AND (sqlc.arg('ip_address')::text = '' OR ip_address = sqlc.arg('ip_address')::text)
  AND (sqlc.arg('operation')::text = '' OR sqlc.arg('operation')::text = 'all' OR operation = sqlc.arg('operation')::text)
  AND (sqlc.arg('risk_level')::text = '' OR sqlc.arg('risk_level')::text = 'all' OR risk_level = sqlc.arg('risk_level')::text)
ORDER BY created_at DESC;

-- name: ListRiskScores :many
SELECT
	subject_type,
	subject_id,
	score,
	risk_level,
	event_count,
	last_event_at,
	updated_at
FROM agent_risk_scores
WHERE (
	sqlc.arg('subject_type')::text = ''
	OR (subject_type = sqlc.arg('subject_type')::text AND subject_id = sqlc.arg('subject_id')::text)
)
ORDER BY score DESC, updated_at DESC
LIMIT 100;

-- name: ListRiskReviews :many
SELECT
	id,
	risk_event_id,
	user_id,
	session_id,
	job_id,
	asset_id,
	request_id,
	ip_address,
	operation,
	reason,
	risk_level,
	priority,
	status,
	assigned_to,
	resolution,
	note,
	metadata,
	created_at,
	updated_at,
	resolved_at
FROM agent_risk_reviews
WHERE created_at >= sqlc.arg('since')
  AND (sqlc.arg('user_id')::text = '' OR user_id = sqlc.arg('user_id')::text)
  AND (sqlc.arg('status')::text = '' OR sqlc.arg('status')::text = 'all' OR status = sqlc.arg('status')::text)
  AND (sqlc.arg('risk_level')::text = '' OR sqlc.arg('risk_level')::text = 'all' OR risk_level = sqlc.arg('risk_level')::text)
  AND (sqlc.arg('operation')::text = '' OR sqlc.arg('operation')::text = 'all' OR operation = sqlc.arg('operation')::text)
ORDER BY updated_at DESC;

-- name: UpdateRiskReview :execrows
UPDATE agent_risk_reviews
SET status = $1,
	assigned_to = $2,
	resolution = $3,
	note = $4,
	updated_at = $5,
	resolved_at = $6
WHERE id = $7;

-- name: GetRiskReview :one
SELECT
	id,
	risk_event_id,
	user_id,
	session_id,
	job_id,
	asset_id,
	request_id,
	ip_address,
	operation,
	reason,
	risk_level,
	priority,
	status,
	assigned_to,
	resolution,
	note,
	metadata,
	created_at,
	updated_at,
	resolved_at
FROM agent_risk_reviews
WHERE id = $1;
