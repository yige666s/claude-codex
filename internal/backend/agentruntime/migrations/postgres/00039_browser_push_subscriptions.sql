-- +goose Up
CREATE TABLE IF NOT EXISTS agent_browser_push_subscriptions (
    subscription_id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL,
    endpoint TEXT NOT NULL,
    endpoint_hash TEXT NOT NULL,
    p256dh TEXT NOT NULL,
    auth_secret TEXT NOT NULL,
    user_agent TEXT NOT NULL DEFAULT '',
    platform TEXT NOT NULL DEFAULT 'web',
    enabled BOOLEAN NOT NULL DEFAULT true,
    disabled_reason TEXT NOT NULL DEFAULT '',
    expires_at TIMESTAMPTZ,
    last_sent_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, endpoint_hash)
);

CREATE INDEX IF NOT EXISTS idx_agent_browser_push_subscriptions_user_enabled
    ON agent_browser_push_subscriptions (user_id, enabled, updated_at DESC);

-- +goose Down
DROP TABLE IF EXISTS agent_browser_push_subscriptions;
