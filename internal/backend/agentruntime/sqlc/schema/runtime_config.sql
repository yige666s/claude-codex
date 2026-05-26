CREATE TABLE agent_runtime_config (
	config_key TEXT PRIMARY KEY,
	payload TEXT NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL
);
