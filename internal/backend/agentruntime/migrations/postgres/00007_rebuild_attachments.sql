-- +goose Up
DROP TABLE IF EXISTS agent_message_attachments;

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
	created_at TIMESTAMPTZ NOT NULL,
	PRIMARY KEY (message_id, attachment_id)
);

CREATE INDEX IF NOT EXISTS idx_agent_message_attachments_message ON agent_message_attachments (message_id);
CREATE INDEX IF NOT EXISTS idx_agent_message_attachments_session ON agent_message_attachments (session_id);
CREATE INDEX IF NOT EXISTS idx_agent_message_attachments_user_status ON agent_message_attachments (user_id, embedding_status, created_at);

-- +goose Down
SELECT 1;
