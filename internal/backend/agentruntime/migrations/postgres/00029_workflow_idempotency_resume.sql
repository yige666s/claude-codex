-- +goose Up
ALTER TABLE agent_workflow_runs ADD COLUMN IF NOT EXISTS request_id TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_workflow_runs ADD COLUMN IF NOT EXISTS idempotency_key TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_workflow_runs ADD COLUMN IF NOT EXISTS lease_owner TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_workflow_runs ADD COLUMN IF NOT EXISTS lease_expires_at TIMESTAMPTZ;
ALTER TABLE agent_workflow_runs ADD COLUMN IF NOT EXISTS recoverable BOOLEAN NOT NULL DEFAULT false;

ALTER TABLE agent_workflow_steps ADD COLUMN IF NOT EXISTS step_index INTEGER NOT NULL DEFAULT 0;
ALTER TABLE agent_workflow_steps ADD COLUMN IF NOT EXISTS idempotency_key TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_workflow_steps ADD COLUMN IF NOT EXISTS attempt INTEGER NOT NULL DEFAULT 1;
ALTER TABLE agent_workflow_steps ADD COLUMN IF NOT EXISTS metadata_json JSONB NOT NULL DEFAULT '{}';

-- +goose StatementBegin
WITH ordered AS (
	SELECT
		id,
		ROW_NUMBER() OVER (PARTITION BY run_id ORDER BY started_at ASC, id ASC) - 1 AS new_step_index
	FROM agent_workflow_steps
)
UPDATE agent_workflow_steps steps
SET step_index = ordered.new_step_index
FROM ordered
WHERE steps.id = ordered.id;
-- +goose StatementEnd

UPDATE agent_workflow_steps
SET idempotency_key = run_id || ':' || step_index::TEXT || ':' || step_name
WHERE idempotency_key = '';

CREATE INDEX IF NOT EXISTS idx_agent_workflow_runs_idempotency
	ON agent_workflow_runs (user_id, name, idempotency_key)
	WHERE idempotency_key <> '';

CREATE UNIQUE INDEX IF NOT EXISTS uniq_agent_workflow_runs_idempotency
	ON agent_workflow_runs (user_id, name, idempotency_key)
	WHERE idempotency_key <> '';

CREATE UNIQUE INDEX IF NOT EXISTS uniq_agent_workflow_steps_run_index
	ON agent_workflow_steps (run_id, step_index);

CREATE UNIQUE INDEX IF NOT EXISTS uniq_agent_workflow_steps_idempotency
	ON agent_workflow_steps (run_id, idempotency_key)
	WHERE idempotency_key <> '';

CREATE INDEX IF NOT EXISTS idx_agent_workflow_steps_run_index
	ON agent_workflow_steps (run_id, step_index);

-- +goose Down
SELECT 1;
