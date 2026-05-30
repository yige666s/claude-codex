CREATE TABLE agent_asset_insights (
	insight_id TEXT PRIMARY KEY,
	asset_id TEXT NOT NULL,
	kind TEXT NOT NULL,
	user_id TEXT NOT NULL,
	session_id TEXT NOT NULL DEFAULT '',
	job_id TEXT NOT NULL DEFAULT '',
	filename TEXT NOT NULL,
	content_type TEXT NOT NULL,
	status TEXT NOT NULL,
	summary TEXT NOT NULL DEFAULT '',
	ocr_text JSONB NOT NULL DEFAULT '[]',
	tags JSONB NOT NULL DEFAULT '[]',
	entities JSONB NOT NULL DEFAULT '[]',
	relationships JSONB NOT NULL DEFAULT '[]',
	style JSONB NOT NULL DEFAULT '{}',
	candidate_memories JSONB NOT NULL DEFAULT '[]',
	extractor TEXT NOT NULL DEFAULT '',
	confidence REAL NOT NULL DEFAULT 0,
	error TEXT NOT NULL DEFAULT '',
	created_at TIMESTAMPTZ NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL,
	completed_at TIMESTAMPTZ,
	UNIQUE (asset_id)
);

CREATE INDEX idx_agent_asset_insights_user_updated ON agent_asset_insights (user_id, updated_at DESC);
CREATE INDEX idx_agent_asset_insights_session ON agent_asset_insights (user_id, session_id, updated_at DESC);
CREATE INDEX idx_agent_asset_insights_status ON agent_asset_insights (status, updated_at);
