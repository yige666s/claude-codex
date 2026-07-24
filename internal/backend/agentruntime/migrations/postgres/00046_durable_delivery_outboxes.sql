-- +goose Up
CREATE TABLE IF NOT EXISTS agent_message_event_outbox (
	event_id TEXT PRIMARY KEY,
	message_id TEXT NOT NULL REFERENCES agent_messages(message_id) ON DELETE CASCADE,
	user_id TEXT NOT NULL,
	session_id TEXT NOT NULL,
	event_type TEXT NOT NULL,
	payload JSONB NOT NULL,
	attempts INTEGER NOT NULL DEFAULT 0,
	available_at TIMESTAMPTZ NOT NULL DEFAULT now(),
	claimed_by TEXT NOT NULL DEFAULT '',
	claimed_until TIMESTAMPTZ,
	last_error TEXT NOT NULL DEFAULT '',
	published_at TIMESTAMPTZ,
	created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
	updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_agent_message_event_outbox_pending
	ON agent_message_event_outbox (available_at, claimed_until, created_at)
	WHERE published_at IS NULL;

CREATE TABLE IF NOT EXISTS agent_job_queue_outbox (
	job_id TEXT PRIMARY KEY REFERENCES agent_jobs(job_id) ON DELETE CASCADE,
	user_id TEXT NOT NULL,
	request_id TEXT NOT NULL DEFAULT '',
	hide_user_message BOOLEAN NOT NULL DEFAULT false,
	attempts INTEGER NOT NULL DEFAULT 0,
	available_at TIMESTAMPTZ NOT NULL DEFAULT now(),
	claimed_by TEXT NOT NULL DEFAULT '',
	claimed_until TIMESTAMPTZ,
	last_error TEXT NOT NULL DEFAULT '',
	published_at TIMESTAMPTZ,
	created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
	updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_agent_job_queue_outbox_pending
	ON agent_job_queue_outbox (available_at, claimed_until, created_at)
	WHERE published_at IS NULL;

-- +goose Down
DROP TABLE IF EXISTS agent_job_queue_outbox;
DROP TABLE IF EXISTS agent_message_event_outbox;
