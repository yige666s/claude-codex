-- +goose Up
CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE INDEX IF NOT EXISTS idx_agent_messages_content_trgm ON agent_messages USING GIN (content gin_trgm_ops);
CREATE INDEX IF NOT EXISTS idx_agent_messages_tool_output_trgm ON agent_messages USING GIN (tool_output gin_trgm_ops);

-- +goose Down
SELECT 1;
