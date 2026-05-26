-- +goose Up
-- +goose StatementBegin
DO $$
BEGIN
	IF to_regclass('agent_memory') IS NULL AND to_regclass('agent_memory_items') IS NOT NULL THEN
		ALTER TABLE agent_memory_items RENAME TO agent_memory;
	END IF;
END $$;
-- +goose StatementEnd

DROP TABLE IF EXISTS agent_memories;

CREATE TABLE IF NOT EXISTS agent_memory (
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

ALTER TABLE agent_memory ADD COLUMN IF NOT EXISTS level TEXT NOT NULL DEFAULT 'atomic';
ALTER TABLE agent_memory ADD COLUMN IF NOT EXISTS namespace TEXT NOT NULL DEFAULT 'default';
ALTER TABLE agent_memory ADD COLUMN IF NOT EXISTS category TEXT NOT NULL DEFAULT 'fact';
ALTER TABLE agent_memory ADD COLUMN IF NOT EXISTS tags TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_memory ADD COLUMN IF NOT EXISTS source_refs TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_memory ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'active';
ALTER TABLE agent_memory ADD COLUMN IF NOT EXISTS raw_hash TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_memory ADD COLUMN IF NOT EXISTS confidence DOUBLE PRECISION NOT NULL DEFAULT 0.7;
ALTER TABLE agent_memory ADD COLUMN IF NOT EXISTS weight DOUBLE PRECISION NOT NULL DEFAULT 0.65;
ALTER TABLE agent_memory ADD COLUMN IF NOT EXISTS access_count BIGINT NOT NULL DEFAULT 0;
ALTER TABLE agent_memory ADD COLUMN IF NOT EXISTS parent_id TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_memory ADD COLUMN IF NOT EXISTS related_ids TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_memory ADD COLUMN IF NOT EXISTS conflict_ids TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_memory ADD COLUMN IF NOT EXISTS supersedes_id TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_memory ADD COLUMN IF NOT EXISTS superseded_by_id TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_memory ADD COLUMN IF NOT EXISTS last_injected_at TIMESTAMPTZ;
ALTER TABLE agent_memory ADD COLUMN IF NOT EXISTS metadata TEXT NOT NULL DEFAULT '{}';
ALTER TABLE agent_memory ADD COLUMN IF NOT EXISTS expires_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_agent_memory_user_created ON agent_memory (user_id, created_at);
CREATE INDEX IF NOT EXISTS idx_agent_memory_user_session ON agent_memory (user_id, session_id, created_at);
CREATE INDEX IF NOT EXISTS idx_agent_memory_user_weight ON agent_memory (user_id, status, visibility, weight);
CREATE INDEX IF NOT EXISTS idx_agent_memory_user_hash ON agent_memory (user_id, raw_hash);
CREATE INDEX IF NOT EXISTS idx_agent_memory_user_level ON agent_memory (user_id, level, status);
CREATE INDEX IF NOT EXISTS idx_agent_memory_user_namespace ON agent_memory (user_id, namespace, status);

CREATE TABLE IF NOT EXISTS agent_memory_settings (
	user_id TEXT PRIMARY KEY,
	payload TEXT NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS agent_personalization_settings (
	user_id TEXT PRIMARY KEY,
	payload TEXT NOT NULL,
	version BIGINT NOT NULL DEFAULT 1,
	updated_at TIMESTAMPTZ NOT NULL
);

-- +goose Down
SELECT 1;
