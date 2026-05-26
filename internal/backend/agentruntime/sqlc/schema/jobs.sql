CREATE TABLE agent_jobs (
	job_id TEXT PRIMARY KEY,
	user_id TEXT NOT NULL,
	session_id TEXT NOT NULL,
	type TEXT NOT NULL,
	status TEXT NOT NULL,
	content TEXT,
	attachments TEXT NOT NULL DEFAULT '',
	error TEXT,
	created_at TIMESTAMPTZ NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL,
	started_at TIMESTAMPTZ,
	finished_at TIMESTAMPTZ
);

CREATE TABLE agent_job_events (
	event_id TEXT PRIMARY KEY,
	job_id TEXT NOT NULL,
	user_id TEXT NOT NULL,
	session_id TEXT NOT NULL,
	type TEXT NOT NULL,
	payload TEXT NOT NULL,
	created_at TIMESTAMPTZ NOT NULL
);
