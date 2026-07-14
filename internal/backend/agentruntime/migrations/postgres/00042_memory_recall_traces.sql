-- +goose Up
CREATE TABLE IF NOT EXISTS agent_memory_recall_events (
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

CREATE INDEX IF NOT EXISTS idx_agent_memory_recall_events_user_created
	ON agent_memory_recall_events (user_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_agent_memory_recall_events_user_session_created
	ON agent_memory_recall_events (user_id, session_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_agent_memory_recall_events_query_hash
	ON agent_memory_recall_events (query_hash);

-- +goose Down
DROP TABLE IF EXISTS agent_memory_recall_events;
