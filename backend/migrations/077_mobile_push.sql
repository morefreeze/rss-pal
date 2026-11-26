BEGIN;

-- Record the inserting transaction for future articles without rewriting existing rows.
ALTER TABLE articles ADD COLUMN IF NOT EXISTS mobile_push_arrival_xid XID8;
ALTER TABLE articles ALTER COLUMN mobile_push_arrival_xid SET DEFAULT pg_current_xact_id();

CREATE TABLE IF NOT EXISTS mobile_push_keys (
    singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (singleton),
    private_key TEXT NOT NULL,
    public_key TEXT NOT NULL
);

ALTER TABLE mobile_push_keys ENABLE ROW LEVEL SECURITY;
ALTER TABLE mobile_push_keys FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS mobile_push_keys_worker ON mobile_push_keys;
CREATE POLICY mobile_push_keys_worker ON mobile_push_keys USING (app_rls_bypass());

CREATE TABLE IF NOT EXISTS mobile_push_subscriptions (
    endpoint TEXT PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    auth TEXT NOT NULL,
    p256dh TEXT NOT NULL,
    checked_snapshot PG_SNAPSHOT NOT NULL DEFAULT pg_current_snapshot(),
    due_at TIMESTAMPTZ NOT NULL DEFAULT (NOW() + INTERVAL '3 hours')
);

CREATE INDEX IF NOT EXISTS mobile_push_due ON mobile_push_subscriptions(due_at);

ALTER TABLE mobile_push_subscriptions ENABLE ROW LEVEL SECURITY;
ALTER TABLE mobile_push_subscriptions FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS mobile_push_subscription_owner ON mobile_push_subscriptions;
CREATE POLICY mobile_push_subscription_owner ON mobile_push_subscriptions
    USING (app_rls_bypass() OR user_id = app_current_user_id());

COMMIT;
