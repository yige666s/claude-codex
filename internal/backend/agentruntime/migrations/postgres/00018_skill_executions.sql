-- +goose Up
CREATE TABLE IF NOT EXISTS agent_skill_executions (
	id TEXT PRIMARY KEY,
	skill_name TEXT NOT NULL,
	user_id TEXT NOT NULL DEFAULT '',
	session_id TEXT NOT NULL DEFAULT '',
	job_id TEXT NOT NULL DEFAULT '',
	request_id TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL,
	error TEXT NOT NULL DEFAULT '',
	error_kind TEXT NOT NULL DEFAULT '',
	provider TEXT NOT NULL DEFAULT '',
	model TEXT NOT NULL DEFAULT '',
	input_summary TEXT NOT NULL DEFAULT '',
	artifact_count BIGINT NOT NULL DEFAULT 0,
	duration_ms BIGINT NOT NULL DEFAULT 0,
	diagnostic_json TEXT NOT NULL DEFAULT '{}',
	metadata TEXT NOT NULL DEFAULT '{}',
	started_at TIMESTAMPTZ NOT NULL,
	completed_at TIMESTAMPTZ NOT NULL
);

ALTER TABLE agent_skill_executions ADD COLUMN IF NOT EXISTS id TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_skill_executions ADD COLUMN IF NOT EXISTS skill_name TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_skill_executions ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_skill_executions ADD COLUMN IF NOT EXISTS user_id TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_skill_executions ADD COLUMN IF NOT EXISTS session_id TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_skill_executions ADD COLUMN IF NOT EXISTS job_id TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_skill_executions ADD COLUMN IF NOT EXISTS request_id TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_skill_executions ADD COLUMN IF NOT EXISTS error TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_skill_executions ADD COLUMN IF NOT EXISTS error_kind TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_skill_executions ADD COLUMN IF NOT EXISTS provider TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_skill_executions ADD COLUMN IF NOT EXISTS model TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_skill_executions ADD COLUMN IF NOT EXISTS input_summary TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_skill_executions ADD COLUMN IF NOT EXISTS artifact_count BIGINT NOT NULL DEFAULT 0;
ALTER TABLE agent_skill_executions ADD COLUMN IF NOT EXISTS duration_ms BIGINT NOT NULL DEFAULT 0;
ALTER TABLE agent_skill_executions ADD COLUMN IF NOT EXISTS diagnostic_json TEXT NOT NULL DEFAULT '{}';
ALTER TABLE agent_skill_executions ADD COLUMN IF NOT EXISTS metadata TEXT NOT NULL DEFAULT '{}';
ALTER TABLE agent_skill_executions ADD COLUMN IF NOT EXISTS started_at TIMESTAMPTZ NOT NULL DEFAULT now();
ALTER TABLE agent_skill_executions ADD COLUMN IF NOT EXISTS completed_at TIMESTAMPTZ NOT NULL DEFAULT now();

CREATE INDEX IF NOT EXISTS idx_agent_skill_executions_skill_time ON agent_skill_executions (skill_name, completed_at);
CREATE INDEX IF NOT EXISTS idx_agent_skill_executions_status_time ON agent_skill_executions (status, completed_at);
CREATE INDEX IF NOT EXISTS idx_agent_skill_executions_user_time ON agent_skill_executions (user_id, completed_at);

-- +goose Down
SELECT 1;
