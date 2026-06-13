-- 036_refresh_tokens.sql
--
-- Long-lived refresh tokens for the "记住此设备" login flow.
--
-- The access JWT TTL stays at 7 days with sliding renewal; refresh tokens kick
-- in only when a device has been idle past the JWT lifetime. We DO NOT enable
-- RLS on this table — same precedent as share_tokens (033_enable_rls.sql:14):
-- the token_hash itself is the secret, and the refresh endpoint is hit by
-- unauthenticated clients (no JWT yet), so RLS would require a bypass pool
-- detour for the lookup. App code enforces ownership via WHERE user_id=$1.

CREATE TABLE IF NOT EXISTS refresh_tokens (
    id SERIAL PRIMARY KEY,
    user_id INT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL UNIQUE,
    expires_at TIMESTAMP NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    last_used_at TIMESTAMP,
    user_agent TEXT NOT NULL DEFAULT '',
    revoked_at TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_refresh_tokens_user ON refresh_tokens(user_id);
CREATE INDEX IF NOT EXISTS idx_refresh_tokens_expires ON refresh_tokens(expires_at)
    WHERE revoked_at IS NULL;

-- rsspal_app needs DML on this table; ALTER DEFAULT PRIVILEGES from
-- 034_app_role_and_grants.sql covers tables created in the public schema after
-- that migration, but be explicit for clarity.
GRANT SELECT, INSERT, UPDATE, DELETE ON refresh_tokens TO rsspal_app;
GRANT USAGE, SELECT ON SEQUENCE refresh_tokens_id_seq TO rsspal_app;
