-- name: UpsertLoopGoal :exec
INSERT INTO agent_loop_goals (
	id,
	user_id,
	session_id,
	job_id,
	workflow_run_id,
	status,
	objective,
	task_type,
	deliverable,
	rubric_json,
	budget_json,
	trigger_json,
	stop_policy_json,
	metadata_json,
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
	$13,
	$14,
	$15,
	$16,
	$17,
	$18
)
ON CONFLICT (id) DO UPDATE SET
	user_id = EXCLUDED.user_id,
	session_id = EXCLUDED.session_id,
	job_id = EXCLUDED.job_id,
	workflow_run_id = EXCLUDED.workflow_run_id,
	status = EXCLUDED.status,
	objective = EXCLUDED.objective,
	task_type = EXCLUDED.task_type,
	deliverable = EXCLUDED.deliverable,
	rubric_json = EXCLUDED.rubric_json,
	budget_json = EXCLUDED.budget_json,
	trigger_json = EXCLUDED.trigger_json,
	stop_policy_json = EXCLUDED.stop_policy_json,
	metadata_json = EXCLUDED.metadata_json,
	updated_at = EXCLUDED.updated_at,
	started_at = EXCLUDED.started_at,
	finished_at = EXCLUDED.finished_at;

-- name: GetLoopGoal :one
SELECT
	id,
	user_id,
	session_id,
	job_id,
	workflow_run_id,
	status,
	objective,
	task_type,
	deliverable,
	rubric_json,
	budget_json,
	trigger_json,
	stop_policy_json,
	metadata_json,
	created_at,
	updated_at,
	started_at,
	finished_at
FROM agent_loop_goals
WHERE user_id = $1
  AND id = $2;

-- name: GetLoopGoalByWorkflowRun :one
SELECT
	id,
	user_id,
	session_id,
	job_id,
	workflow_run_id,
	status,
	objective,
	task_type,
	deliverable,
	rubric_json,
	budget_json,
	trigger_json,
	stop_policy_json,
	metadata_json,
	created_at,
	updated_at,
	started_at,
	finished_at
FROM agent_loop_goals
WHERE user_id = $1
  AND workflow_run_id = $2;

-- name: ListLoopGoals :many
SELECT
	id,
	user_id,
	session_id,
	job_id,
	workflow_run_id,
	status,
	objective,
	task_type,
	deliverable,
	rubric_json,
	budget_json,
	trigger_json,
	stop_policy_json,
	metadata_json,
	created_at,
	updated_at,
	started_at,
	finished_at
FROM agent_loop_goals
WHERE user_id = $1
  AND (sqlc.arg('session_id')::text = '' OR session_id = sqlc.arg('session_id')::text)
  AND (sqlc.arg('status')::text = '' OR status = sqlc.arg('status')::text)
ORDER BY updated_at DESC
LIMIT NULLIF(sqlc.arg('limit_count')::int, 0);

-- name: UpdateLoopGoalRun :exec
UPDATE agent_loop_goals
SET job_id = COALESCE(NULLIF($1, ''), job_id),
	workflow_run_id = COALESCE(NULLIF($2, ''), workflow_run_id),
	status = $3,
	updated_at = $4,
	started_at = COALESCE(started_at, $5),
	finished_at = COALESCE($6, finished_at)
WHERE user_id = $7
  AND id = $8;

-- name: UpdateLoopGoalStatus :exec
UPDATE agent_loop_goals
SET status = $1,
	updated_at = $2,
	started_at = COALESCE(started_at, $3),
	finished_at = COALESCE($4, finished_at)
WHERE user_id = $5
  AND id = $6;
