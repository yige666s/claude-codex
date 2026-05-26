-- +goose Up
ALTER TABLE agent_messages ADD COLUMN IF NOT EXISTS archive_uri TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_messages ADD COLUMN IF NOT EXISTS archive_checksum TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_messages ADD COLUMN IF NOT EXISTS archived_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_agent_messages_archive_due ON agent_messages (created_at, archived_at);
CREATE INDEX IF NOT EXISTS idx_agent_messages_archive_uri ON agent_messages (archive_uri);

-- +goose Down
SELECT 1;
