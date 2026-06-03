CREATE TABLE agent_workflow_runs (
	id TEXT PRIMARY KEY,
	user_id TEXT NOT NULL DEFAULT '',
	session_id TEXT NOT NULL DEFAULT '',
	job_id TEXT NOT NULL DEFAULT '',
	request_id TEXT NOT NULL DEFAULT '',
	idempotency_key TEXT NOT NULL DEFAULT '',
	name TEXT NOT NULL,
	version TEXT NOT NULL DEFAULT 'v1',
	status TEXT NOT NULL,
	state_json JSONB NOT NULL DEFAULT '{}',
	error TEXT NOT NULL DEFAULT '',
	lease_owner TEXT NOT NULL DEFAULT '',
	lease_expires_at TIMESTAMPTZ,
	recoverable BOOLEAN NOT NULL DEFAULT false,
	created_at TIMESTAMPTZ NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL,
	started_at TIMESTAMPTZ,
	finished_at TIMESTAMPTZ
);

CREATE TABLE agent_workflow_steps (
	id TEXT PRIMARY KEY,
	run_id TEXT NOT NULL REFERENCES agent_workflow_runs(id) ON DELETE CASCADE,
	step_index INTEGER NOT NULL DEFAULT 0,
	step_name TEXT NOT NULL,
	idempotency_key TEXT NOT NULL DEFAULT '',
	attempt INTEGER NOT NULL DEFAULT 1,
	status TEXT NOT NULL,
	input_json JSONB NOT NULL DEFAULT '{}',
	output_json JSONB NOT NULL DEFAULT '{}',
	error TEXT NOT NULL DEFAULT '',
	metadata_json JSONB NOT NULL DEFAULT '{}',
	started_at TIMESTAMPTZ NOT NULL,
	finished_at TIMESTAMPTZ
);
