-- +goose Up
CREATE TABLE IF NOT EXISTS agent_memory_episodes (
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

ALTER TABLE agent_memory_episodes ADD COLUMN IF NOT EXISTS id TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_memory_episodes ADD COLUMN IF NOT EXISTS user_id TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_memory_episodes ADD COLUMN IF NOT EXISTS session_id TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_memory_episodes ADD COLUMN IF NOT EXISTS title TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_memory_episodes ADD COLUMN IF NOT EXISTS summary TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_memory_episodes ADD COLUMN IF NOT EXISTS l0_abstract TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_memory_episodes ADD COLUMN IF NOT EXISTS key_topics TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_memory_episodes ADD COLUMN IF NOT EXISTS source_type TEXT NOT NULL DEFAULT 'session';
ALTER TABLE agent_memory_episodes ADD COLUMN IF NOT EXISTS source_id TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_memory_episodes ADD COLUMN IF NOT EXISTS source_refs TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_memory_episodes ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'active';
ALTER TABLE agent_memory_episodes ADD COLUMN IF NOT EXISTS visibility TEXT NOT NULL DEFAULT 'user';
ALTER TABLE agent_memory_episodes ADD COLUMN IF NOT EXISTS turn_count BIGINT NOT NULL DEFAULT 0;
ALTER TABLE agent_memory_episodes ADD COLUMN IF NOT EXISTS token_count BIGINT NOT NULL DEFAULT 0;
ALTER TABLE agent_memory_episodes ADD COLUMN IF NOT EXISTS confidence DOUBLE PRECISION NOT NULL DEFAULT 0.7;
ALTER TABLE agent_memory_episodes ADD COLUMN IF NOT EXISTS weight DOUBLE PRECISION NOT NULL DEFAULT 0.65;
ALTER TABLE agent_memory_episodes ADD COLUMN IF NOT EXISTS recall_count BIGINT NOT NULL DEFAULT 0;
ALTER TABLE agent_memory_episodes ADD COLUMN IF NOT EXISTS use_count BIGINT NOT NULL DEFAULT 0;
ALTER TABLE agent_memory_episodes ADD COLUMN IF NOT EXISTS recall_score DOUBLE PRECISION NOT NULL DEFAULT 0;
ALTER TABLE agent_memory_episodes ADD COLUMN IF NOT EXISTS last_recalled_at TIMESTAMPTZ;
ALTER TABLE agent_memory_episodes ADD COLUMN IF NOT EXISTS last_used_at TIMESTAMPTZ;
ALTER TABLE agent_memory_episodes ADD COLUMN IF NOT EXISTS promoted_at TIMESTAMPTZ;
ALTER TABLE agent_memory_episodes ADD COLUMN IF NOT EXISTS metadata TEXT NOT NULL DEFAULT '{}';
ALTER TABLE agent_memory_episodes ADD COLUMN IF NOT EXISTS expires_at TIMESTAMPTZ;
ALTER TABLE agent_memory_episodes ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ NOT NULL DEFAULT now();
ALTER TABLE agent_memory_episodes ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT now();

CREATE UNIQUE INDEX IF NOT EXISTS idx_agent_memory_episodes_source
	ON agent_memory_episodes (user_id, source_type, source_id)
	WHERE source_id <> '';

CREATE INDEX IF NOT EXISTS idx_agent_memory_episodes_user_created
	ON agent_memory_episodes (user_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_agent_memory_episodes_user_session
	ON agent_memory_episodes (user_id, session_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_agent_memory_episodes_user_status_weight
	ON agent_memory_episodes (user_id, status, visibility, weight DESC);

CREATE INDEX IF NOT EXISTS idx_agent_memory_episodes_user_promoted
	ON agent_memory_episodes (user_id, promoted_at, recall_score DESC);

CREATE INDEX IF NOT EXISTS idx_agent_memory_episodes_expires
	ON agent_memory_episodes (expires_at)
	WHERE expires_at IS NOT NULL;

-- +goose Down
SELECT 1;
