-- name: InsertEvaluationRun :exec
INSERT INTO agent_eval_runs (
	id,
	name,
	status,
	trigger,
	scope,
	started_at,
	completed_at,
	total,
	passed,
	failed,
	warning,
	metrics,
	threshold_status,
	summary
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
	$14
);

-- name: UpdateEvaluationRun :execrows
UPDATE agent_eval_runs
SET name = $1,
	status = $2,
	trigger = $3,
	scope = $4,
	started_at = $5,
	completed_at = $6,
	total = $7,
	passed = $8,
	failed = $9,
	warning = $10,
	metrics = $11,
	threshold_status = $12,
	summary = $13
WHERE id = $14;

-- name: GetEvaluationRun :one
SELECT
	id,
	name,
	status,
	trigger,
	scope,
	started_at,
	completed_at,
	total,
	passed,
	failed,
	warning,
	metrics,
	threshold_status,
	summary
FROM agent_eval_runs
WHERE id = $1;

-- name: ListEvaluationRuns :many
SELECT
	id,
	name,
	status,
	trigger,
	scope,
	started_at,
	completed_at,
	total,
	passed,
	failed,
	warning,
	metrics,
	threshold_status,
	summary
FROM agent_eval_runs
WHERE (sqlc.arg('status')::text = '' OR status = sqlc.arg('status')::text)
  AND (sqlc.arg('trigger')::text = '' OR trigger = sqlc.arg('trigger')::text)
ORDER BY started_at DESC
LIMIT NULLIF(sqlc.arg('limit_count')::int, 0);

-- name: InsertEvaluationResult :exec
INSERT INTO agent_eval_results (
	id,
	run_id,
	subject_type,
	subject_id,
	user_id,
	session_id,
	job_id,
	skill_name,
	provider,
	model,
	status,
	score,
	input,
	output,
	metrics,
	findings,
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
	$13,
	$14,
	$15,
	$16,
	$17
);

-- name: ListEvaluationResults :many
SELECT
	id,
	run_id,
	subject_type,
	subject_id,
	user_id,
	session_id,
	job_id,
	skill_name,
	provider,
	model,
	status,
	score,
	input,
	output,
	metrics,
	findings,
	created_at
FROM agent_eval_results
WHERE (sqlc.arg('run_id')::text = '' OR run_id = sqlc.arg('run_id')::text)
  AND (sqlc.arg('status')::text = '' OR status = sqlc.arg('status')::text)
  AND (sqlc.arg('subject_type')::text = '' OR subject_type = sqlc.arg('subject_type')::text)
  AND (sqlc.arg('user_id')::text = '' OR user_id = sqlc.arg('user_id')::text)
  AND (sqlc.arg('session_id')::text = '' OR session_id = sqlc.arg('session_id')::text)
  AND (sqlc.arg('job_id')::text = '' OR job_id = sqlc.arg('job_id')::text)
  AND (sqlc.arg('skill_name')::text = '' OR skill_name = sqlc.arg('skill_name')::text)
  AND (sqlc.arg('provider')::text = '' OR provider = sqlc.arg('provider')::text)
  AND (sqlc.arg('model')::text = '' OR model = sqlc.arg('model')::text)
ORDER BY created_at DESC
LIMIT NULLIF(sqlc.arg('limit_count')::int, 0);

-- name: InsertEvaluationReview :exec
INSERT INTO agent_eval_reviews (
	id,
	result_id,
	status,
	reviewer,
	note,
	created_at,
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

-- name: UpdateEvaluationReview :execrows
UPDATE agent_eval_reviews
SET status = $1,
	reviewer = $2,
	note = $3,
	updated_at = $4
WHERE id = $5;

-- name: GetEvaluationReview :one
SELECT
	id,
	result_id,
	status,
	reviewer,
	note,
	created_at,
	updated_at
FROM agent_eval_reviews
WHERE id = $1;

-- name: ListEvaluationReviews :many
SELECT
	id,
	result_id,
	status,
	reviewer,
	note,
	created_at,
	updated_at
FROM agent_eval_reviews
WHERE (sqlc.arg('result_id')::text = '' OR result_id = sqlc.arg('result_id')::text)
  AND (sqlc.arg('status')::text = '' OR status = sqlc.arg('status')::text)
ORDER BY updated_at DESC
LIMIT NULLIF(sqlc.arg('limit_count')::int, 0);
