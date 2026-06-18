-- +goose Up
CREATE TABLE IF NOT EXISTS agent_loop_goals (
	id TEXT PRIMARY KEY,
	user_id TEXT NOT NULL,
	session_id TEXT NOT NULL DEFAULT '',
	job_id TEXT NOT NULL DEFAULT '',
	workflow_run_id TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL,
	objective TEXT NOT NULL,
	task_type TEXT NOT NULL DEFAULT '',
	deliverable TEXT NOT NULL DEFAULT '',
	rubric_json JSONB NOT NULL DEFAULT '{}',
	budget_json JSONB NOT NULL DEFAULT '{}',
	trigger_json JSONB NOT NULL DEFAULT '{}',
	stop_policy_json JSONB NOT NULL DEFAULT '{}',
	metadata_json JSONB NOT NULL DEFAULT '{}',
	created_at TIMESTAMPTZ NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL,
	started_at TIMESTAMPTZ,
	finished_at TIMESTAMPTZ
);

ALTER TABLE agent_loop_goals ADD COLUMN IF NOT EXISTS id TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_loop_goals ADD COLUMN IF NOT EXISTS user_id TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_loop_goals ADD COLUMN IF NOT EXISTS session_id TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_loop_goals ADD COLUMN IF NOT EXISTS job_id TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_loop_goals ADD COLUMN IF NOT EXISTS workflow_run_id TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_loop_goals ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'pending';
ALTER TABLE agent_loop_goals ADD COLUMN IF NOT EXISTS objective TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_loop_goals ADD COLUMN IF NOT EXISTS task_type TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_loop_goals ADD COLUMN IF NOT EXISTS deliverable TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_loop_goals ADD COLUMN IF NOT EXISTS rubric_json JSONB NOT NULL DEFAULT '{}';
ALTER TABLE agent_loop_goals ADD COLUMN IF NOT EXISTS budget_json JSONB NOT NULL DEFAULT '{}';
ALTER TABLE agent_loop_goals ADD COLUMN IF NOT EXISTS trigger_json JSONB NOT NULL DEFAULT '{}';
ALTER TABLE agent_loop_goals ADD COLUMN IF NOT EXISTS stop_policy_json JSONB NOT NULL DEFAULT '{}';
ALTER TABLE agent_loop_goals ADD COLUMN IF NOT EXISTS metadata_json JSONB NOT NULL DEFAULT '{}';
ALTER TABLE agent_loop_goals ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ NOT NULL DEFAULT now();
ALTER TABLE agent_loop_goals ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT now();
ALTER TABLE agent_loop_goals ADD COLUMN IF NOT EXISTS started_at TIMESTAMPTZ;
ALTER TABLE agent_loop_goals ADD COLUMN IF NOT EXISTS finished_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_agent_loop_goals_user_updated
	ON agent_loop_goals (user_id, updated_at DESC);

CREATE INDEX IF NOT EXISTS idx_agent_loop_goals_session_updated
	ON agent_loop_goals (session_id, updated_at DESC);

CREATE INDEX IF NOT EXISTS idx_agent_loop_goals_status_updated
	ON agent_loop_goals (status, updated_at DESC);

CREATE INDEX IF NOT EXISTS idx_agent_loop_goals_workflow_run
	ON agent_loop_goals (workflow_run_id);

-- +goose Down
SELECT 1;
