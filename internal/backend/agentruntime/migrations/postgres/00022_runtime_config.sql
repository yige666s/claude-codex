-- +goose Up
CREATE TABLE IF NOT EXISTS agent_runtime_config (
	config_key TEXT PRIMARY KEY,
	payload TEXT NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL
);

-- +goose Down
SELECT 1;
