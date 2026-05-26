CREATE TABLE agent_audit_logs (
	id TEXT PRIMARY KEY,
	event TEXT NOT NULL,
	user_id TEXT,
	session_id TEXT,
	job_id TEXT,
	asset_id TEXT,
	request_id TEXT,
	ip_address TEXT,
	user_agent TEXT,
	metadata TEXT NOT NULL,
	created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX idx_agent_audit_logs_user_created ON agent_audit_logs (user_id, created_at);
CREATE INDEX idx_agent_audit_logs_event_created ON agent_audit_logs (event, created_at);
