-- +goose Up
CREATE TABLE IF NOT EXISTS agent_skills (
	name TEXT PRIMARY KEY,
	display_name TEXT NOT NULL DEFAULT '',
	description TEXT NOT NULL DEFAULT '',
	category TEXT NOT NULL DEFAULT '',
	icon TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL DEFAULT 'unpublished',
	version TEXT NOT NULL DEFAULT '',
	source TEXT NOT NULL DEFAULT '',
	skill_root TEXT NOT NULL DEFAULT '',
	metadata TEXT NOT NULL DEFAULT '{}',
	content_hash TEXT NOT NULL DEFAULT '',
	created_at TIMESTAMPTZ NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL,
	published_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_agent_skills_status ON agent_skills (status, updated_at);
CREATE INDEX IF NOT EXISTS idx_agent_skills_category ON agent_skills (category, status);

CREATE TABLE IF NOT EXISTS agent_skill_versions (
	skill_name TEXT NOT NULL,
	version TEXT NOT NULL DEFAULT '',
	content_hash TEXT NOT NULL DEFAULT '',
	changelog TEXT NOT NULL DEFAULT '',
	metadata TEXT NOT NULL DEFAULT '{}',
	created_at TIMESTAMPTZ NOT NULL,
	published_at TIMESTAMPTZ,
	PRIMARY KEY (skill_name, version, content_hash)
);

CREATE INDEX IF NOT EXISTS idx_agent_skill_versions_skill_created ON agent_skill_versions (skill_name, created_at);

CREATE TABLE IF NOT EXISTS agent_skill_releases (
	id TEXT PRIMARY KEY,
	skill_name TEXT NOT NULL,
	version TEXT NOT NULL DEFAULT '',
	content_hash TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL DEFAULT '',
	changelog TEXT NOT NULL DEFAULT '',
	actor TEXT NOT NULL DEFAULT '',
	metadata TEXT NOT NULL DEFAULT '{}',
	created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_agent_skill_releases_skill_created ON agent_skill_releases (skill_name, created_at);

DELETE FROM agent_skill_versions v WHERE NOT EXISTS (
	SELECT 1 FROM agent_skills s WHERE s.name = v.skill_name
);

-- +goose StatementBegin
DO $$
BEGIN
	IF NOT EXISTS (
		SELECT 1
		FROM pg_constraint
		WHERE conname = 'fk_agent_skill_versions_skill'
	) THEN
		ALTER TABLE agent_skill_versions ADD CONSTRAINT fk_agent_skill_versions_skill FOREIGN KEY (skill_name) REFERENCES agent_skills(name) ON DELETE CASCADE;
	END IF;
END $$;
-- +goose StatementEnd

-- +goose StatementBegin
DO $$
BEGIN
	IF NOT EXISTS (
		SELECT 1
		FROM pg_constraint
		WHERE conname = 'fk_agent_skill_releases_skill'
	) THEN
		ALTER TABLE agent_skill_releases ADD CONSTRAINT fk_agent_skill_releases_skill FOREIGN KEY (skill_name) REFERENCES agent_skills(name) ON DELETE CASCADE;
	END IF;
END $$;
-- +goose StatementEnd

-- +goose Down
SELECT 1;
