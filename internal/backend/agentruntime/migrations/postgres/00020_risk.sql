-- +goose Up
CREATE TABLE IF NOT EXISTS agent_risk_events (
	id TEXT PRIMARY KEY,
	user_id TEXT,
	session_id TEXT,
	job_id TEXT,
	asset_id TEXT,
	request_id TEXT,
	ip_address TEXT,
	operation TEXT NOT NULL,
	reason TEXT NOT NULL,
	risk_level TEXT NOT NULL,
	score_delta INTEGER NOT NULL,
	metadata TEXT NOT NULL,
	created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_agent_risk_events_user_created ON agent_risk_events (user_id, created_at);
CREATE INDEX IF NOT EXISTS idx_agent_risk_events_operation_created ON agent_risk_events (operation, created_at);
CREATE INDEX IF NOT EXISTS idx_agent_risk_events_ip_created ON agent_risk_events (ip_address, created_at);

CREATE TABLE IF NOT EXISTS agent_risk_scores (
	subject_type TEXT NOT NULL,
	subject_id TEXT NOT NULL,
	score INTEGER NOT NULL,
	risk_level TEXT NOT NULL,
	event_count INTEGER NOT NULL,
	last_event_at TIMESTAMPTZ NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL,
	PRIMARY KEY (subject_type, subject_id)
);

CREATE INDEX IF NOT EXISTS idx_agent_risk_scores_score ON agent_risk_scores (score);

CREATE TABLE IF NOT EXISTS agent_risk_reviews (
	id TEXT PRIMARY KEY,
	risk_event_id TEXT NOT NULL UNIQUE,
	user_id TEXT,
	session_id TEXT,
	job_id TEXT,
	asset_id TEXT,
	request_id TEXT,
	ip_address TEXT,
	operation TEXT NOT NULL,
	reason TEXT NOT NULL,
	risk_level TEXT NOT NULL,
	priority TEXT NOT NULL,
	status TEXT NOT NULL,
	assigned_to TEXT,
	resolution TEXT,
	note TEXT,
	metadata TEXT NOT NULL,
	created_at TIMESTAMPTZ NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL,
	resolved_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_agent_risk_reviews_status_updated ON agent_risk_reviews (status, updated_at);
CREATE INDEX IF NOT EXISTS idx_agent_risk_reviews_user_created ON agent_risk_reviews (user_id, created_at);

DELETE FROM agent_risk_reviews rv WHERE NOT EXISTS (
	SELECT 1 FROM agent_risk_events e WHERE e.id = rv.risk_event_id
);

-- +goose StatementBegin
DO $$
BEGIN
	IF NOT EXISTS (
		SELECT 1
		FROM pg_constraint
		WHERE conname = 'fk_agent_risk_reviews_event'
	) THEN
		ALTER TABLE agent_risk_reviews ADD CONSTRAINT fk_agent_risk_reviews_event FOREIGN KEY (risk_event_id) REFERENCES agent_risk_events(id) ON DELETE CASCADE;
	END IF;
END $$;
-- +goose StatementEnd

-- +goose Down
SELECT 1;
