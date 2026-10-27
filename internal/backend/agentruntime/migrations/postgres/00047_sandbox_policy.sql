-- +goose Up
CREATE TABLE IF NOT EXISTS agent_sandbox_policy_revisions (
    revision BIGSERIAL PRIMARY KEY,
    payload JSONB NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('draft', 'active', 'superseded')),
    actor TEXT NOT NULL,
    source_revision BIGINT REFERENCES agent_sandbox_policy_revisions(revision),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS agent_sandbox_policy_active (
    singleton SMALLINT PRIMARY KEY CHECK (singleton = 1),
    revision BIGINT NOT NULL REFERENCES agent_sandbox_policy_revisions(revision),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS agent_sandbox_policy_audit (
    event_id BIGSERIAL PRIMARY KEY,
    revision BIGINT NOT NULL REFERENCES agent_sandbox_policy_revisions(revision),
    previous_revision BIGINT,
    actor TEXT NOT NULL,
    operation TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
INSERT INTO agent_sandbox_policy_revisions (payload, state, actor)
SELECT '{"enabled":false,"default_profile":"","profiles":{},"routes":[],"node_pools":{}}'::jsonb, 'active', 'bootstrap'
WHERE NOT EXISTS (SELECT 1 FROM agent_sandbox_policy_revisions);
INSERT INTO agent_sandbox_policy_active (singleton, revision)
SELECT 1, revision FROM agent_sandbox_policy_revisions WHERE state = 'active' ORDER BY revision LIMIT 1
ON CONFLICT (singleton) DO NOTHING;

-- +goose Down
DROP TABLE IF EXISTS agent_sandbox_policy_audit;
DROP TABLE IF EXISTS agent_sandbox_policy_active;
DROP TABLE IF EXISTS agent_sandbox_policy_revisions;
