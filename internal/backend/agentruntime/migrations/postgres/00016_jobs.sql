-- +goose Up
CREATE TABLE IF NOT EXISTS agent_jobs (
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

ALTER TABLE agent_jobs ADD COLUMN IF NOT EXISTS attachments TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_agent_jobs_user_updated ON agent_jobs (user_id, updated_at);
CREATE INDEX IF NOT EXISTS idx_agent_jobs_session_updated ON agent_jobs (session_id, updated_at);

CREATE TABLE IF NOT EXISTS agent_job_events (
	event_id TEXT PRIMARY KEY,
	job_id TEXT NOT NULL,
	user_id TEXT NOT NULL,
	session_id TEXT NOT NULL,
	type TEXT NOT NULL,
	payload TEXT NOT NULL,
	created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_agent_job_events_job_created ON agent_job_events (job_id, created_at);

DELETE FROM agent_job_events e WHERE NOT EXISTS (
	SELECT 1 FROM agent_jobs j WHERE j.job_id = e.job_id
);

-- +goose StatementBegin
DO $$
BEGIN
	IF NOT EXISTS (
		SELECT 1
		FROM pg_constraint
		WHERE conname = 'fk_agent_job_events_job'
	) THEN
		ALTER TABLE agent_job_events ADD CONSTRAINT fk_agent_job_events_job FOREIGN KEY (job_id) REFERENCES agent_jobs(job_id) ON DELETE CASCADE;
	END IF;
END $$;
-- +goose StatementEnd

-- +goose Down
SELECT 1;
