CREATE TABLE agent_prompt_templates (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	description TEXT NOT NULL DEFAULT '',
	scope TEXT NOT NULL DEFAULT '',
	owner TEXT NOT NULL DEFAULT '',
	metadata TEXT NOT NULL DEFAULT '{}',
	created_at TIMESTAMPTZ NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE agent_prompt_versions (
	prompt_id TEXT NOT NULL,
	version TEXT NOT NULL,
	status TEXT NOT NULL DEFAULT 'draft',
	content TEXT NOT NULL,
	variables_schema TEXT NOT NULL DEFAULT '{}',
	render_config TEXT NOT NULL DEFAULT '{}',
	content_hash TEXT NOT NULL,
	base_version TEXT NOT NULL DEFAULT '',
	changelog TEXT NOT NULL DEFAULT '',
	created_by TEXT NOT NULL DEFAULT '',
	reviewed_by TEXT NOT NULL DEFAULT '',
	created_at TIMESTAMPTZ NOT NULL,
	published_at TIMESTAMPTZ,
	PRIMARY KEY (prompt_id, version)
);

CREATE TABLE agent_prompt_experiments (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	prompt_id TEXT NOT NULL,
	status TEXT NOT NULL DEFAULT 'draft',
	traffic_scope TEXT NOT NULL DEFAULT 'user',
	allocation TEXT NOT NULL DEFAULT '{}',
	guardrails TEXT NOT NULL DEFAULT '{}',
	winner_variant_id TEXT NOT NULL DEFAULT '',
	created_by TEXT NOT NULL DEFAULT '',
	updated_by TEXT NOT NULL DEFAULT '',
	started_at TIMESTAMPTZ,
	ended_at TIMESTAMPTZ,
	created_at TIMESTAMPTZ NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE agent_prompt_experiment_variants (
	experiment_id TEXT NOT NULL,
	variant_id TEXT NOT NULL,
	prompt_version TEXT NOT NULL,
	weight INTEGER NOT NULL DEFAULT 0,
	metadata TEXT NOT NULL DEFAULT '{}',
	created_at TIMESTAMPTZ NOT NULL,
	PRIMARY KEY (experiment_id, variant_id)
);

CREATE TABLE agent_prompt_environment_pins (
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
