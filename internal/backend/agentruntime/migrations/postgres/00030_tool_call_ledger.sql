-- +goose Up
CREATE TABLE IF NOT EXISTS agent_tool_call_ledger (
	id TEXT PRIMARY KEY,
	user_id TEXT NOT NULL DEFAULT '',
	session_id TEXT NOT NULL DEFAULT '',
	job_id TEXT NOT NULL DEFAULT '',
	workflow_run_id TEXT NOT NULL DEFAULT '',
	workflow_step_id TEXT NOT NULL DEFAULT '',
	workflow_step_index INTEGER NOT NULL DEFAULT 0,
	tool_call_id TEXT NOT NULL DEFAULT '',
	tool_name TEXT NOT NULL DEFAULT '',
	args_hash TEXT NOT NULL DEFAULT '',
	idempotency_key TEXT NOT NULL,
	status TEXT NOT NULL DEFAULT '',
	input_json JSONB NOT NULL DEFAULT '{}',
	output_text TEXT NOT NULL DEFAULT '',
	error TEXT NOT NULL DEFAULT '',
	external_idempotency_key TEXT NOT NULL DEFAULT '',
	attempt INTEGER NOT NULL DEFAULT 1,
	metadata_json JSONB NOT NULL DEFAULT '{}',
	started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
	completed_at TIMESTAMPTZ
);

ALTER TABLE agent_tool_call_ledger ADD COLUMN IF NOT EXISTS id TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_tool_call_ledger ADD COLUMN IF NOT EXISTS user_id TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_tool_call_ledger ADD COLUMN IF NOT EXISTS session_id TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_tool_call_ledger ADD COLUMN IF NOT EXISTS job_id TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_tool_call_ledger ADD COLUMN IF NOT EXISTS workflow_run_id TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_tool_call_ledger ADD COLUMN IF NOT EXISTS workflow_step_id TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_tool_call_ledger ADD COLUMN IF NOT EXISTS workflow_step_index INTEGER NOT NULL DEFAULT 0;
ALTER TABLE agent_tool_call_ledger ADD COLUMN IF NOT EXISTS tool_call_id TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_tool_call_ledger ADD COLUMN IF NOT EXISTS tool_name TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_tool_call_ledger ADD COLUMN IF NOT EXISTS args_hash TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_tool_call_ledger ADD COLUMN IF NOT EXISTS idempotency_key TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_tool_call_ledger ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_tool_call_ledger ADD COLUMN IF NOT EXISTS input_json JSONB NOT NULL DEFAULT '{}';
ALTER TABLE agent_tool_call_ledger ADD COLUMN IF NOT EXISTS output_text TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_tool_call_ledger ADD COLUMN IF NOT EXISTS error TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_tool_call_ledger ADD COLUMN IF NOT EXISTS external_idempotency_key TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_tool_call_ledger ADD COLUMN IF NOT EXISTS attempt INTEGER NOT NULL DEFAULT 1;
ALTER TABLE agent_tool_call_ledger ADD COLUMN IF NOT EXISTS metadata_json JSONB NOT NULL DEFAULT '{}';
ALTER TABLE agent_tool_call_ledger ADD COLUMN IF NOT EXISTS started_at TIMESTAMPTZ NOT NULL DEFAULT now();
ALTER TABLE agent_tool_call_ledger ADD COLUMN IF NOT EXISTS completed_at TIMESTAMPTZ;

CREATE UNIQUE INDEX IF NOT EXISTS uniq_agent_tool_call_ledger_idempotency
	ON agent_tool_call_ledger (idempotency_key)
	WHERE idempotency_key <> '';

CREATE INDEX IF NOT EXISTS idx_agent_tool_call_ledger_user_time
	ON agent_tool_call_ledger (user_id, started_at DESC);

CREATE INDEX IF NOT EXISTS idx_agent_tool_call_ledger_session_time
	ON agent_tool_call_ledger (session_id, started_at DESC);

CREATE INDEX IF NOT EXISTS idx_agent_tool_call_ledger_job_time
	ON agent_tool_call_ledger (job_id, started_at DESC);

CREATE INDEX IF NOT EXISTS idx_agent_tool_call_ledger_workflow_time
	ON agent_tool_call_ledger (workflow_run_id, workflow_step_id, started_at DESC);

CREATE INDEX IF NOT EXISTS idx_agent_tool_call_ledger_status_time
	ON agent_tool_call_ledger (status, started_at DESC);

-- +goose Down
SELECT 1;
