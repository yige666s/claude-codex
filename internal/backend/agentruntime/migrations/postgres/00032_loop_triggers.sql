-- +goose Up
CREATE TABLE IF NOT EXISTS agent_loop_triggers (
	id TEXT PRIMARY KEY,
	user_id TEXT NOT NULL,
	session_id TEXT NOT NULL DEFAULT '',
	dedupe_key TEXT NOT NULL UNIQUE,
	trigger_type TEXT NOT NULL,
	source TEXT NOT NULL DEFAULT '',
	payload_json JSONB NOT NULL DEFAULT '{}',
	job_id TEXT NOT NULL DEFAULT '',
	loop_goal_id TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL DEFAULT 'started',
	created_at TIMESTAMPTZ NOT NULL,
	expires_at TIMESTAMPTZ NOT NULL
);

ALTER TABLE agent_loop_triggers ADD COLUMN IF NOT EXISTS id TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_loop_triggers ADD COLUMN IF NOT EXISTS user_id TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_loop_triggers ADD COLUMN IF NOT EXISTS session_id TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_loop_triggers ADD COLUMN IF NOT EXISTS dedupe_key TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_loop_triggers ADD COLUMN IF NOT EXISTS trigger_type TEXT NOT NULL DEFAULT 'manual';
ALTER TABLE agent_loop_triggers ADD COLUMN IF NOT EXISTS source TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_loop_triggers ADD COLUMN IF NOT EXISTS payload_json JSONB NOT NULL DEFAULT '{}';
ALTER TABLE agent_loop_triggers ADD COLUMN IF NOT EXISTS job_id TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_loop_triggers ADD COLUMN IF NOT EXISTS loop_goal_id TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_loop_triggers ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'started';
ALTER TABLE agent_loop_triggers ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ NOT NULL DEFAULT now();
ALTER TABLE agent_loop_triggers ADD COLUMN IF NOT EXISTS expires_at TIMESTAMPTZ NOT NULL DEFAULT now();

CREATE UNIQUE INDEX IF NOT EXISTS idx_agent_loop_triggers_dedupe
	ON agent_loop_triggers (dedupe_key);

CREATE INDEX IF NOT EXISTS idx_agent_loop_triggers_user_created
	ON agent_loop_triggers (user_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_agent_loop_triggers_expires
	ON agent_loop_triggers (expires_at);

-- +goose Down
SELECT 1;
