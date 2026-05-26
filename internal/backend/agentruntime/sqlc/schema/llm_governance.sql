CREATE TABLE agent_llm_usage (
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
	estimated_cost_usd DOUBLE PRECISION NOT NULL,
	attempt INTEGER NOT NULL,
	status TEXT NOT NULL,
	error TEXT,
	latency_ms BIGINT NOT NULL,
	ttft_ms BIGINT NOT NULL DEFAULT 0,
	created_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE agent_llm_quota_adjustments (
	id TEXT PRIMARY KEY,
	user_id TEXT NOT NULL,
	actor_user_id TEXT,
	reason TEXT,
	request_delta INTEGER NOT NULL,
	input_token_delta INTEGER NOT NULL,
	output_token_delta INTEGER NOT NULL,
	total_token_delta INTEGER NOT NULL,
	estimated_cost_delta_usd DOUBLE PRECISION NOT NULL,
	created_at TIMESTAMPTZ NOT NULL
);
