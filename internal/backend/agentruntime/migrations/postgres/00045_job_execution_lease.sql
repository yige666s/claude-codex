-- +goose Up
ALTER TABLE agent_jobs ADD COLUMN IF NOT EXISTS execution_owner TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_jobs ADD COLUMN IF NOT EXISTS execution_epoch BIGINT NOT NULL DEFAULT 0;
ALTER TABLE agent_jobs ADD COLUMN IF NOT EXISTS execution_lease_expires_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_agent_jobs_execution_lease
	ON agent_jobs (status, execution_lease_expires_at)
	WHERE status = 'running';

-- +goose Down
SELECT 1;
