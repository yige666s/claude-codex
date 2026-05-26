-- +goose Up
-- +goose StatementBegin
DO $$
BEGIN
	IF EXISTS (
		SELECT 1
		FROM information_schema.columns
		WHERE table_schema = current_schema()
		  AND table_name = 'agent_messages'
		  AND column_name = 'message_index'
	) AND NOT EXISTS (
		SELECT 1
		FROM information_schema.columns
		WHERE table_schema = current_schema()
		  AND table_name = 'agent_messages'
		  AND column_name = 'message_id'
	) THEN
		DROP TABLE IF EXISTS agent_message_embedding_meta;
		DROP TABLE IF EXISTS agent_message_attachments;
		DROP TABLE IF EXISTS agent_messages_legacy_pre_message_module;
		ALTER TABLE agent_messages RENAME TO agent_messages_legacy_pre_message_module;
	END IF;
END $$;
-- +goose StatementEnd

CREATE TABLE IF NOT EXISTS agent_messages (
	message_id TEXT PRIMARY KEY,
	session_id TEXT NOT NULL,
	user_id TEXT NOT NULL,
	seq_no BIGINT NOT NULL,
	parent_id TEXT NOT NULL DEFAULT '',
	role TEXT NOT NULL,
	content_type TEXT NOT NULL DEFAULT 'text',
	content TEXT NOT NULL DEFAULT '',
	content_parts JSONB NOT NULL DEFAULT '[]',
	tool_call_id TEXT NOT NULL DEFAULT '',
	tool_name TEXT NOT NULL DEFAULT '',
	tool_input JSONB NOT NULL DEFAULT '{}',
	tool_output TEXT NOT NULL DEFAULT '',
	tool_calls JSONB NOT NULL DEFAULT '[]',
	prompt_tokens INTEGER NOT NULL DEFAULT 0,
	completion_tokens INTEGER NOT NULL DEFAULT 0,
	status INTEGER NOT NULL DEFAULT 1,
	is_context_used INTEGER NOT NULL DEFAULT 1,
	model_id TEXT NOT NULL DEFAULT '',
	run_id TEXT NOT NULL DEFAULT '',
	hidden INTEGER NOT NULL DEFAULT 0,
	created_at TIMESTAMPTZ NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_agent_messages_session_active_seq ON agent_messages (user_id, session_id, seq_no) WHERE status <> 2;
CREATE INDEX IF NOT EXISTS idx_agent_messages_session_created ON agent_messages (user_id, session_id, created_at);
CREATE INDEX IF NOT EXISTS idx_agent_messages_run_id ON agent_messages (run_id);
CREATE INDEX IF NOT EXISTS idx_agent_messages_user_created ON agent_messages (user_id, created_at);
CREATE INDEX IF NOT EXISTS idx_agent_messages_role_created ON agent_messages (user_id, role, created_at);

CREATE TABLE IF NOT EXISTS agent_message_attachments (
	attachment_id TEXT NOT NULL,
	message_id TEXT NOT NULL,
	session_id TEXT NOT NULL,
	user_id TEXT NOT NULL,
	file_type TEXT NOT NULL,
	mime_type TEXT NOT NULL,
	file_name TEXT NOT NULL DEFAULT '',
	file_size BIGINT NOT NULL DEFAULT 0,
	storage_key TEXT NOT NULL,
	thumbnail_key TEXT NOT NULL DEFAULT '',
	embedding_status INTEGER NOT NULL DEFAULT 0,
	extracted_text_key TEXT NOT NULL DEFAULT '',
	created_at TIMESTAMPTZ NOT NULL,
	PRIMARY KEY (message_id, attachment_id)
);

CREATE INDEX IF NOT EXISTS idx_agent_message_attachments_message ON agent_message_attachments (message_id);
CREATE INDEX IF NOT EXISTS idx_agent_message_attachments_session ON agent_message_attachments (session_id);
CREATE INDEX IF NOT EXISTS idx_agent_message_attachments_user_status ON agent_message_attachments (user_id, embedding_status, created_at);

CREATE TABLE IF NOT EXISTS agent_message_embedding_meta (
	embedding_id TEXT PRIMARY KEY,
	message_id TEXT NOT NULL,
	session_id TEXT NOT NULL,
	user_id TEXT NOT NULL,
	chunk_index INTEGER NOT NULL DEFAULT 0,
	vector_id TEXT NOT NULL,
	model_version TEXT NOT NULL DEFAULT '',
	created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_agent_message_embedding_message ON agent_message_embedding_meta (message_id);
CREATE INDEX IF NOT EXISTS idx_agent_message_embedding_user ON agent_message_embedding_meta (user_id);

DROP INDEX IF EXISTS idx_agent_messages_session_seq;
CREATE UNIQUE INDEX IF NOT EXISTS idx_agent_messages_session_active_seq ON agent_messages (user_id, session_id, seq_no) WHERE status <> 2;

-- +goose Down
SELECT 1;
