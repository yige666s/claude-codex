CREATE TABLE agent_eval_runs (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	status TEXT NOT NULL,
	trigger TEXT NOT NULL DEFAULT '',
	scope TEXT NOT NULL DEFAULT '{}',
	started_at TIMESTAMPTZ NOT NULL,
	completed_at TIMESTAMPTZ,
	total BIGINT NOT NULL DEFAULT 0,
	passed BIGINT NOT NULL DEFAULT 0,
	failed BIGINT NOT NULL DEFAULT 0,
	warning BIGINT NOT NULL DEFAULT 0,
	metrics TEXT NOT NULL DEFAULT '{}',
	threshold_status TEXT NOT NULL DEFAULT '',
	summary TEXT NOT NULL DEFAULT ''
);

CREATE TABLE agent_eval_results (
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
	metrics TEXT NOT NULL DEFAULT '{}',
	findings TEXT NOT NULL DEFAULT '[]',
	created_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE agent_eval_reviews (
	id TEXT PRIMARY KEY,
	result_id TEXT NOT NULL,
	status TEXT NOT NULL,
	reviewer TEXT NOT NULL DEFAULT '',
	note TEXT NOT NULL DEFAULT '',
	created_at TIMESTAMPTZ NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE agent_eval_golden_sets (
	id TEXT NOT NULL,
	version TEXT NOT NULL DEFAULT 'v1',
	name TEXT NOT NULL,
	description TEXT NOT NULL DEFAULT '',
	metadata TEXT NOT NULL DEFAULT '{}',
	created_at TIMESTAMPTZ NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL,
	PRIMARY KEY (id, version)
);

CREATE TABLE agent_eval_golden_cases (
	id TEXT NOT NULL,
	set_id TEXT NOT NULL,
	set_version TEXT NOT NULL DEFAULT 'v1',
	position BIGINT NOT NULL DEFAULT 0,
	query TEXT NOT NULL,
	expected_answer TEXT NOT NULL DEFAULT '',
	expected_facts TEXT NOT NULL DEFAULT '[]',
	gold_evidence TEXT NOT NULL DEFAULT '[]',
	tags TEXT NOT NULL DEFAULT '[]',
	metadata TEXT NOT NULL DEFAULT '{}',
	created_at TIMESTAMPTZ NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL,
	PRIMARY KEY (set_id, set_version, id)
);
