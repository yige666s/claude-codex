-- +goose Up
ALTER TABLE agent_connector_oauth_states
  ADD COLUMN IF NOT EXISTS metadata_json JSONB NOT NULL DEFAULT '{}'::jsonb;

-- +goose Down
SELECT 1;
