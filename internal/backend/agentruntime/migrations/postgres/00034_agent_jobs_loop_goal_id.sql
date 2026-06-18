-- +goose Up
ALTER TABLE agent_jobs ADD COLUMN IF NOT EXISTS loop_goal_id TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_agent_jobs_loop_goal_updated ON agent_jobs (loop_goal_id, updated_at);

-- +goose Down
SELECT 1;
