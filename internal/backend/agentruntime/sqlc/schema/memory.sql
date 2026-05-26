CREATE TABLE agent_memory (
	id TEXT PRIMARY KEY,
	user_id TEXT NOT NULL,
	session_id TEXT,
	namespace TEXT NOT NULL DEFAULT 'default',
	kind TEXT NOT NULL,
	level TEXT NOT NULL DEFAULT 'atomic',
	category TEXT NOT NULL DEFAULT 'fact',
	tags TEXT NOT NULL DEFAULT '',
	source TEXT NOT NULL,
	source_refs TEXT NOT NULL DEFAULT '',
	visibility TEXT NOT NULL,
	status TEXT NOT NULL DEFAULT 'active',
	content TEXT NOT NULL,
	raw_hash TEXT NOT NULL DEFAULT '',
	confidence DOUBLE PRECISION NOT NULL DEFAULT 0.7,
	weight DOUBLE PRECISION NOT NULL DEFAULT 0.65,
	access_count BIGINT NOT NULL DEFAULT 0,
	parent_id TEXT NOT NULL DEFAULT '',
	related_ids TEXT NOT NULL DEFAULT '',
	conflict_ids TEXT NOT NULL DEFAULT '',
	supersedes_id TEXT NOT NULL DEFAULT '',
	superseded_by_id TEXT NOT NULL DEFAULT '',
	last_injected_at TIMESTAMPTZ,
	metadata TEXT NOT NULL DEFAULT '{}',
	expires_at TIMESTAMPTZ,
	created_at TIMESTAMPTZ NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE agent_memory_settings (
	user_id TEXT PRIMARY KEY,
	payload TEXT NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE agent_personalization_settings (
	user_id TEXT PRIMARY KEY,
	payload TEXT NOT NULL,
	version BIGINT NOT NULL DEFAULT 1,
	updated_at TIMESTAMPTZ NOT NULL
);
