-- +goose Up
CREATE TABLE IF NOT EXISTS agent_workflow_runs (
	id TEXT PRIMARY KEY,
	user_id TEXT NOT NULL DEFAULT '',
	session_id TEXT NOT NULL DEFAULT '',
	job_id TEXT NOT NULL DEFAULT '',
	name TEXT NOT NULL,
	version TEXT NOT NULL DEFAULT 'v1',
	status TEXT NOT NULL,
	state_json JSONB NOT NULL DEFAULT '{}',
	error TEXT NOT NULL DEFAULT '',
	created_at TIMESTAMPTZ NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL,
	started_at TIMESTAMPTZ,
	finished_at TIMESTAMPTZ
);

ALTER TABLE agent_workflow_runs ADD COLUMN IF NOT EXISTS id TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_workflow_runs ADD COLUMN IF NOT EXISTS user_id TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_workflow_runs ADD COLUMN IF NOT EXISTS session_id TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_workflow_runs ADD COLUMN IF NOT EXISTS job_id TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_workflow_runs ADD COLUMN IF NOT EXISTS name TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_workflow_runs ADD COLUMN IF NOT EXISTS version TEXT NOT NULL DEFAULT 'v1';
ALTER TABLE agent_workflow_runs ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_workflow_runs ADD COLUMN IF NOT EXISTS state_json JSONB NOT NULL DEFAULT '{}';
ALTER TABLE agent_workflow_runs ADD COLUMN IF NOT EXISTS error TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_workflow_runs ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ NOT NULL DEFAULT now();
ALTER TABLE agent_workflow_runs ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT now();
ALTER TABLE agent_workflow_runs ADD COLUMN IF NOT EXISTS started_at TIMESTAMPTZ;
ALTER TABLE agent_workflow_runs ADD COLUMN IF NOT EXISTS finished_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_agent_workflow_runs_user_time ON agent_workflow_runs (user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_agent_workflow_runs_session_time ON agent_workflow_runs (session_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_agent_workflow_runs_job_time ON agent_workflow_runs (job_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_agent_workflow_runs_name_time ON agent_workflow_runs (name, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_agent_workflow_runs_status_time ON agent_workflow_runs (status, created_at DESC);

CREATE TABLE IF NOT EXISTS agent_workflow_steps (
	id TEXT PRIMARY KEY,
	run_id TEXT NOT NULL,
	step_name TEXT NOT NULL,
	status TEXT NOT NULL,
	input_json JSONB NOT NULL DEFAULT '{}',
	output_json JSONB NOT NULL DEFAULT '{}',
	error TEXT NOT NULL DEFAULT '',
	started_at TIMESTAMPTZ NOT NULL,
	finished_at TIMESTAMPTZ
);

ALTER TABLE agent_workflow_steps ADD COLUMN IF NOT EXISTS id TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_workflow_steps ADD COLUMN IF NOT EXISTS run_id TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_workflow_steps ADD COLUMN IF NOT EXISTS step_name TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_workflow_steps ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_workflow_steps ADD COLUMN IF NOT EXISTS input_json JSONB NOT NULL DEFAULT '{}';
ALTER TABLE agent_workflow_steps ADD COLUMN IF NOT EXISTS output_json JSONB NOT NULL DEFAULT '{}';
ALTER TABLE agent_workflow_steps ADD COLUMN IF NOT EXISTS error TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_workflow_steps ADD COLUMN IF NOT EXISTS started_at TIMESTAMPTZ NOT NULL DEFAULT now();
ALTER TABLE agent_workflow_steps ADD COLUMN IF NOT EXISTS finished_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_agent_workflow_steps_run_time ON agent_workflow_steps (run_id, started_at, id);

-- +goose StatementBegin
DO $$
BEGIN
	IF NOT EXISTS (
		SELECT 1
		FROM pg_constraint
		WHERE conname = 'fk_agent_workflow_steps_run'
	) THEN
		ALTER TABLE agent_workflow_steps ADD CONSTRAINT fk_agent_workflow_steps_run FOREIGN KEY (run_id) REFERENCES agent_workflow_runs(id) ON DELETE CASCADE;
	END IF;
END $$;
-- +goose StatementEnd

-- +goose Down
SELECT 1;
