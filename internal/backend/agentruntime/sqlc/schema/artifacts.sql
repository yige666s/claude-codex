CREATE TABLE agent_artifacts (
	artifact_id TEXT PRIMARY KEY,
	kind TEXT NOT NULL DEFAULT 'artifact',
	user_id TEXT NOT NULL,
	session_id TEXT NOT NULL DEFAULT '',
	job_id TEXT NOT NULL DEFAULT '',
	object_key TEXT NOT NULL,
	filename TEXT NOT NULL,
	content_type TEXT NOT NULL,
	size_bytes BIGINT NOT NULL,
	created_at TIMESTAMPTZ NOT NULL,
	deleted_at TIMESTAMPTZ
);

CREATE INDEX idx_agent_artifacts_user_created ON agent_artifacts (user_id, created_at);
CREATE INDEX idx_agent_artifacts_kind_user_created ON agent_artifacts (kind, user_id, created_at);
CREATE INDEX idx_agent_artifacts_session ON agent_artifacts (user_id, session_id, created_at);
CREATE INDEX idx_agent_artifacts_job ON agent_artifacts (user_id, job_id, created_at);
