-- +goose Up
CREATE TABLE IF NOT EXISTS agent_prompt_environment_pins (
	prompt_id TEXT NOT NULL,
	environment TEXT NOT NULL,
	version TEXT NOT NULL,
	pinned_by TEXT NOT NULL DEFAULT '',
	changelog TEXT NOT NULL DEFAULT '',
	eval_run_id TEXT NOT NULL DEFAULT '',
	created_at TIMESTAMPTZ NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL,
	PRIMARY KEY (prompt_id, environment)
);

CREATE INDEX IF NOT EXISTS idx_agent_prompt_environment_pins_environment ON agent_prompt_environment_pins (environment, updated_at DESC);

-- +goose Down
DROP TABLE IF EXISTS agent_prompt_environment_pins;
