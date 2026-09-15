-- Pre-authentication abuse budgets are intentionally not user-scoped/RLS:
-- authentication has no established user ID. Keys are HMACs of identifiers.
CREATE TABLE IF NOT EXISTS auth_rate_limits (
    bucket_key TEXT PRIMARY KEY,
    attempts INTEGER NOT NULL CHECK (attempts > 0),
    expires_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS auth_rate_limits_expiry_idx ON auth_rate_limits(expires_at);
