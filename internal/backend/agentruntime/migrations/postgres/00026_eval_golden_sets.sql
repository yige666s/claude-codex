-- +goose Up
CREATE TABLE IF NOT EXISTS agent_eval_golden_sets (
	id TEXT NOT NULL,
	version TEXT NOT NULL DEFAULT 'v1',
	name TEXT NOT NULL,
	description TEXT NOT NULL DEFAULT '',
	metadata JSONB NOT NULL DEFAULT '{}',
	created_at TIMESTAMPTZ NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL,
	PRIMARY KEY (id, version)
);

CREATE INDEX IF NOT EXISTS idx_agent_eval_golden_sets_updated ON agent_eval_golden_sets (updated_at);

CREATE TABLE IF NOT EXISTS agent_eval_golden_cases (
	id TEXT NOT NULL,
	set_id TEXT NOT NULL,
	set_version TEXT NOT NULL DEFAULT 'v1',
	position BIGINT NOT NULL DEFAULT 0,
	query TEXT NOT NULL,
	expected_answer TEXT NOT NULL DEFAULT '',
	expected_facts JSONB NOT NULL DEFAULT '[]',
	gold_evidence JSONB NOT NULL DEFAULT '[]',
	tags JSONB NOT NULL DEFAULT '[]',
	metadata JSONB NOT NULL DEFAULT '{}',
	created_at TIMESTAMPTZ NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL,
	PRIMARY KEY (set_id, set_version, id)
);

CREATE INDEX IF NOT EXISTS idx_agent_eval_golden_cases_set_position ON agent_eval_golden_cases (set_id, set_version, position);

-- +goose StatementBegin
DO $$
BEGIN
	IF NOT EXISTS (
		SELECT 1
		FROM pg_constraint
		WHERE conname = 'fk_agent_eval_golden_cases_set'
	) THEN
		ALTER TABLE agent_eval_golden_cases ADD CONSTRAINT fk_agent_eval_golden_cases_set FOREIGN KEY (set_id, set_version) REFERENCES agent_eval_golden_sets(id, version) ON DELETE CASCADE;
	END IF;
END $$;
-- +goose StatementEnd

-- +goose Down
SELECT 1;
