-- +goose Up
CREATE TABLE IF NOT EXISTS agent_eval_runs (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	status TEXT NOT NULL,
	trigger TEXT NOT NULL DEFAULT '',
	scope JSONB NOT NULL DEFAULT '{}',
	started_at TIMESTAMPTZ NOT NULL,
	completed_at TIMESTAMPTZ,
	total BIGINT NOT NULL DEFAULT 0,
	passed BIGINT NOT NULL DEFAULT 0,
	failed BIGINT NOT NULL DEFAULT 0,
	warning BIGINT NOT NULL DEFAULT 0,
	metrics JSONB NOT NULL DEFAULT '{}',
	threshold_status TEXT NOT NULL DEFAULT '',
	summary TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_agent_eval_runs_started ON agent_eval_runs (started_at);
CREATE INDEX IF NOT EXISTS idx_agent_eval_runs_status_started ON agent_eval_runs (status, started_at);

CREATE TABLE IF NOT EXISTS agent_eval_results (
	id TEXT PRIMARY KEY,
	run_id TEXT NOT NULL,
	subject_type TEXT NOT NULL,
	subject_id TEXT NOT NULL,
	user_id TEXT NOT NULL DEFAULT '',
	session_id TEXT NOT NULL DEFAULT '',
	job_id TEXT NOT NULL DEFAULT '',
	skill_name TEXT NOT NULL DEFAULT '',
	provider TEXT NOT NULL DEFAULT '',
	model TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL,
	score DOUBLE PRECISION NOT NULL DEFAULT 0,
	input TEXT NOT NULL DEFAULT '',
	output TEXT NOT NULL DEFAULT '',
	metrics JSONB NOT NULL DEFAULT '{}',
	findings JSONB NOT NULL DEFAULT '[]',
	created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_agent_eval_results_run_status ON agent_eval_results (run_id, status);
CREATE INDEX IF NOT EXISTS idx_agent_eval_results_subject ON agent_eval_results (subject_type, subject_id);
CREATE INDEX IF NOT EXISTS idx_agent_eval_results_user_created ON agent_eval_results (user_id, created_at);

CREATE TABLE IF NOT EXISTS agent_eval_reviews (
	id TEXT PRIMARY KEY,
	result_id TEXT NOT NULL,
	status TEXT NOT NULL,
	reviewer TEXT NOT NULL DEFAULT '',
	note TEXT NOT NULL DEFAULT '',
	created_at TIMESTAMPTZ NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_agent_eval_reviews_result ON agent_eval_reviews (result_id);
CREATE INDEX IF NOT EXISTS idx_agent_eval_reviews_status_updated ON agent_eval_reviews (status, updated_at);

DELETE FROM agent_eval_reviews rv WHERE NOT EXISTS (
	SELECT 1 FROM agent_eval_results r WHERE r.id = rv.result_id
) OR EXISTS (
	SELECT 1 FROM agent_eval_results r
	WHERE r.id = rv.result_id
	  AND NOT EXISTS (SELECT 1 FROM agent_eval_runs er WHERE er.id = r.run_id)
);

DELETE FROM agent_eval_results r WHERE NOT EXISTS (
	SELECT 1 FROM agent_eval_runs er WHERE er.id = r.run_id
);

-- +goose StatementBegin
DO $$
BEGIN
	IF NOT EXISTS (
		SELECT 1
		FROM pg_constraint
		WHERE conname = 'fk_agent_eval_results_run'
	) THEN
		ALTER TABLE agent_eval_results ADD CONSTRAINT fk_agent_eval_results_run FOREIGN KEY (run_id) REFERENCES agent_eval_runs(id) ON DELETE CASCADE;
	END IF;
END $$;
-- +goose StatementEnd

-- +goose StatementBegin
DO $$
BEGIN
	IF NOT EXISTS (
		SELECT 1
		FROM pg_constraint
		WHERE conname = 'fk_agent_eval_reviews_result'
	) THEN
		ALTER TABLE agent_eval_reviews ADD CONSTRAINT fk_agent_eval_reviews_result FOREIGN KEY (result_id) REFERENCES agent_eval_results(id) ON DELETE CASCADE;
	END IF;
END $$;
-- +goose StatementEnd

-- +goose Down
SELECT 1;
