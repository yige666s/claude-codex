-- +goose Up
ALTER TABLE agent_loop_triggers
	ADD COLUMN IF NOT EXISTS failure_reason TEXT NOT NULL DEFAULT '';

-- +goose Down
SELECT 1;
