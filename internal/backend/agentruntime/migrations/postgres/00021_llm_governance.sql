-- +goose Up
CREATE TABLE IF NOT EXISTS agent_llm_usage (
	id TEXT PRIMARY KEY,
	user_id TEXT NOT NULL,
	session_id TEXT NOT NULL,
	request_id TEXT,
	skill_name TEXT,
	provider TEXT NOT NULL,
	model TEXT NOT NULL,
	input_tokens INTEGER NOT NULL,
	output_tokens INTEGER NOT NULL,
	total_tokens INTEGER NOT NULL,
	estimated_cost_usd REAL NOT NULL,
	attempt INTEGER NOT NULL,
	status TEXT NOT NULL,
	error TEXT,
	latency_ms BIGINT NOT NULL,
	ttft_ms BIGINT NOT NULL DEFAULT 0,
	created_at TIMESTAMPTZ NOT NULL
);

ALTER TABLE agent_llm_usage ADD COLUMN IF NOT EXISTS ttft_ms BIGINT NOT NULL DEFAULT 0;

CREATE TABLE IF NOT EXISTS agent_llm_quota_adjustments (
	id TEXT PRIMARY KEY,
	user_id TEXT NOT NULL,
	actor_user_id TEXT,
	reason TEXT,
	request_delta INTEGER NOT NULL,
	input_token_delta INTEGER NOT NULL,
	output_token_delta INTEGER NOT NULL,
	total_token_delta INTEGER NOT NULL,
	estimated_cost_delta_usd REAL NOT NULL,
	created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_agent_llm_usage_user_created ON agent_llm_usage (user_id, created_at);
CREATE INDEX IF NOT EXISTS idx_agent_llm_usage_session_created ON agent_llm_usage (session_id, created_at);
CREATE INDEX IF NOT EXISTS idx_agent_llm_quota_adjustments_user_created ON agent_llm_quota_adjustments (user_id, created_at);

-- +goose Down
SELECT 1;
