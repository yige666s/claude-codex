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

CREATE TABLE agent_memory_episodes (
	id TEXT PRIMARY KEY,
	user_id TEXT NOT NULL,
	session_id TEXT NOT NULL DEFAULT '',
	title TEXT NOT NULL DEFAULT '',
	summary TEXT NOT NULL,
	l0_abstract TEXT NOT NULL DEFAULT '',
	key_topics TEXT NOT NULL DEFAULT '',
	source_type TEXT NOT NULL DEFAULT 'session',
	source_id TEXT NOT NULL DEFAULT '',
	source_refs TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL DEFAULT 'active',
	visibility TEXT NOT NULL DEFAULT 'user',
	turn_count BIGINT NOT NULL DEFAULT 0,
	token_count BIGINT NOT NULL DEFAULT 0,
	confidence DOUBLE PRECISION NOT NULL DEFAULT 0.7,
	weight DOUBLE PRECISION NOT NULL DEFAULT 0.65,
	recall_count BIGINT NOT NULL DEFAULT 0,
	use_count BIGINT NOT NULL DEFAULT 0,
	recall_score DOUBLE PRECISION NOT NULL DEFAULT 0,
	last_recalled_at TIMESTAMPTZ,
	last_used_at TIMESTAMPTZ,
	promoted_at TIMESTAMPTZ,
	metadata TEXT NOT NULL DEFAULT '{}',
	expires_at TIMESTAMPTZ,
	created_at TIMESTAMPTZ NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE agent_memory_recall_events (
	id TEXT PRIMARY KEY,
	user_id TEXT NOT NULL,
	session_id TEXT NOT NULL DEFAULT '',
	trigger_reason TEXT NOT NULL DEFAULT '',
	query_hash TEXT NOT NULL DEFAULT '',
	query TEXT NOT NULL DEFAULT '',
	original_query TEXT NOT NULL DEFAULT '',
	rewritten_query TEXT NOT NULL DEFAULT '',
	query_rewrite_used BOOLEAN NOT NULL DEFAULT FALSE,
	query_rewrite_reason TEXT NOT NULL DEFAULT '',
	query_rewrite_degraded BOOLEAN NOT NULL DEFAULT FALSE,
	memory_item_ids TEXT NOT NULL DEFAULT '[]',
	episode_ids TEXT NOT NULL DEFAULT '[]',
	source_refs TEXT NOT NULL DEFAULT '[]',
	memory_chars BIGINT NOT NULL DEFAULT 0,
	episode_chars BIGINT NOT NULL DEFAULT 0,
	injected BOOLEAN NOT NULL DEFAULT FALSE,
	degraded BOOLEAN NOT NULL DEFAULT FALSE,
	degraded_reason TEXT NOT NULL DEFAULT '',
	latency_ms BIGINT NOT NULL DEFAULT 0,
	metadata TEXT NOT NULL DEFAULT '{}',
	created_at TIMESTAMPTZ NOT NULL
);
