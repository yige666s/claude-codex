-- +goose Up
ALTER TABLE agent_message_attachments ADD COLUMN IF NOT EXISTS extracted_text_key TEXT NOT NULL DEFAULT '';

-- +goose Down
SELECT 1;
