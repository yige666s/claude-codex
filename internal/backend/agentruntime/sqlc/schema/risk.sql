CREATE TABLE agent_risk_events (
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

CREATE TABLE agent_risk_scores (
	subject_type TEXT NOT NULL,
	subject_id TEXT NOT NULL,
	score INTEGER NOT NULL,
	risk_level TEXT NOT NULL,
	event_count INTEGER NOT NULL,
	last_event_at TIMESTAMPTZ NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL,
	PRIMARY KEY (subject_type, subject_id)
);

CREATE TABLE agent_risk_reviews (
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
