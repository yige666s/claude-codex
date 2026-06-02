CREATE TABLE agent_workflow_runs (
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

CREATE TABLE agent_workflow_steps (
	id TEXT PRIMARY KEY,
	run_id TEXT NOT NULL REFERENCES agent_workflow_runs(id) ON DELETE CASCADE,
	step_name TEXT NOT NULL,
	status TEXT NOT NULL,
	input_json JSONB NOT NULL DEFAULT '{}',
	output_json JSONB NOT NULL DEFAULT '{}',
	error TEXT NOT NULL DEFAULT '',
	started_at TIMESTAMPTZ NOT NULL,
	finished_at TIMESTAMPTZ
);
