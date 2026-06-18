CREATE TABLE agent_loop_triggers (
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
