-- +goose Up
-- +goose StatementBegin
DO $$
BEGIN
	IF EXISTS (
		SELECT 1
		FROM information_schema.columns
		WHERE table_schema = current_schema()
		  AND table_name = 'agent_sessions'
		  AND column_name = 'payload'
	) AND NOT EXISTS (
		SELECT 1
		FROM information_schema.columns
		WHERE table_schema = current_schema()
		  AND table_name = 'agent_sessions'
		  AND column_name = 'status'
	) THEN
		DROP TABLE IF EXISTS agent_sessions_legacy_pre_message_module;
		ALTER TABLE agent_sessions RENAME TO agent_sessions_legacy_pre_message_module;
	END IF;
END $$;
-- +goose StatementEnd

CREATE TABLE IF NOT EXISTS agent_sessions (
	user_id TEXT NOT NULL,
	session_id TEXT NOT NULL,
	agent_id TEXT NOT NULL DEFAULT '',
	title TEXT NOT NULL DEFAULT '',
	status INTEGER NOT NULL DEFAULT 1,
	message_count INTEGER NOT NULL DEFAULT 0,
	total_tokens BIGINT NOT NULL DEFAULT 0,
	working_dir TEXT NOT NULL DEFAULT '',
	tags JSONB NOT NULL DEFAULT '[]',
	description TEXT NOT NULL DEFAULT '',
	parent_id TEXT NOT NULL DEFAULT '',
	branch_point INTEGER NOT NULL DEFAULT 0,
	metadata JSONB NOT NULL DEFAULT '{}',
	archived INTEGER NOT NULL DEFAULT 0,
	created_at TIMESTAMPTZ NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL,
	last_message_at TIMESTAMPTZ,
	PRIMARY KEY (user_id, session_id)
);

CREATE INDEX IF NOT EXISTS idx_agent_sessions_user_status_last_message ON agent_sessions (user_id, status, last_message_at);
CREATE INDEX IF NOT EXISTS idx_agent_sessions_agent_status ON agent_sessions (agent_id, status);

-- +goose Down
SELECT 1;
