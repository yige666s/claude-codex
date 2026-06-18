-- +goose Up
CREATE TABLE IF NOT EXISTS agent_deep_agent_evidence (
	id TEXT PRIMARY KEY,
	run_id TEXT NOT NULL,
	user_id TEXT NOT NULL DEFAULT '',
	session_id TEXT NOT NULL DEFAULT '',
	job_id TEXT NOT NULL DEFAULT '',
	loop_goal_id TEXT NOT NULL DEFAULT '',
	step_id TEXT NOT NULL DEFAULT '',
	action_id TEXT NOT NULL DEFAULT '',
	template_id TEXT NOT NULL DEFAULT '',
	task_type TEXT NOT NULL DEFAULT '',
	trigger_type TEXT NOT NULL DEFAULT '',
	route_json JSONB NOT NULL DEFAULT '{}',
	evidence_json JSONB NOT NULL DEFAULT '{}',
	artifact_count INTEGER NOT NULL DEFAULT 0,
	source_count INTEGER NOT NULL DEFAULT 0,
	tool_call_count INTEGER NOT NULL DEFAULT 0,
	child_job_count INTEGER NOT NULL DEFAULT 0,
	error_class TEXT NOT NULL DEFAULT '',
	side_effect_level TEXT NOT NULL DEFAULT '',
	created_at TIMESTAMPTZ NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL
);

ALTER TABLE agent_deep_agent_evidence ADD COLUMN IF NOT EXISTS id TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_deep_agent_evidence ADD COLUMN IF NOT EXISTS run_id TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_deep_agent_evidence ADD COLUMN IF NOT EXISTS user_id TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_deep_agent_evidence ADD COLUMN IF NOT EXISTS session_id TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_deep_agent_evidence ADD COLUMN IF NOT EXISTS job_id TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_deep_agent_evidence ADD COLUMN IF NOT EXISTS loop_goal_id TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_deep_agent_evidence ADD COLUMN IF NOT EXISTS step_id TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_deep_agent_evidence ADD COLUMN IF NOT EXISTS action_id TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_deep_agent_evidence ADD COLUMN IF NOT EXISTS template_id TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_deep_agent_evidence ADD COLUMN IF NOT EXISTS task_type TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_deep_agent_evidence ADD COLUMN IF NOT EXISTS trigger_type TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_deep_agent_evidence ADD COLUMN IF NOT EXISTS route_json JSONB NOT NULL DEFAULT '{}';
ALTER TABLE agent_deep_agent_evidence ADD COLUMN IF NOT EXISTS evidence_json JSONB NOT NULL DEFAULT '{}';
ALTER TABLE agent_deep_agent_evidence ADD COLUMN IF NOT EXISTS artifact_count INTEGER NOT NULL DEFAULT 0;
ALTER TABLE agent_deep_agent_evidence ADD COLUMN IF NOT EXISTS source_count INTEGER NOT NULL DEFAULT 0;
ALTER TABLE agent_deep_agent_evidence ADD COLUMN IF NOT EXISTS tool_call_count INTEGER NOT NULL DEFAULT 0;
ALTER TABLE agent_deep_agent_evidence ADD COLUMN IF NOT EXISTS child_job_count INTEGER NOT NULL DEFAULT 0;
ALTER TABLE agent_deep_agent_evidence ADD COLUMN IF NOT EXISTS error_class TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_deep_agent_evidence ADD COLUMN IF NOT EXISTS side_effect_level TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_deep_agent_evidence ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ NOT NULL DEFAULT now();
ALTER TABLE agent_deep_agent_evidence ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT now();

CREATE UNIQUE INDEX IF NOT EXISTS idx_agent_deep_agent_evidence_run_step_action
	ON agent_deep_agent_evidence (run_id, step_id, action_id);

CREATE INDEX IF NOT EXISTS idx_agent_deep_agent_evidence_user_session
	ON agent_deep_agent_evidence (user_id, session_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_agent_deep_agent_evidence_run
	ON agent_deep_agent_evidence (run_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_agent_deep_agent_evidence_task_template
	ON agent_deep_agent_evidence (task_type, template_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_agent_deep_agent_evidence_loop_goal
	ON agent_deep_agent_evidence (loop_goal_id, created_at DESC);

-- +goose Down
SELECT 1;
