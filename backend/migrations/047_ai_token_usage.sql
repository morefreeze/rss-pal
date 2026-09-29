BEGIN;
SET LOCAL app.bypass_rls = 'true';
CREATE TABLE IF NOT EXISTS ai_token_collection (
 id BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK(id),
 started_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()
);
INSERT INTO ai_token_collection(id) VALUES(TRUE) ON CONFLICT DO NOTHING;
CREATE TABLE IF NOT EXISTS ai_token_usage (
 id BIGSERIAL PRIMARY KEY,
 at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 provider TEXT NOT NULL,
 model TEXT NOT NULL,
 user_id INTEGER NOT NULL DEFAULT 0 CHECK(user_id >= 0),
 input_tokens BIGINT CHECK(input_tokens >= 0),
 cached_tokens BIGINT CHECK(cached_tokens >= 0 AND cached_tokens <= input_tokens),
 output_tokens BIGINT CHECK(output_tokens >= 0),
 input_price NUMERIC(16,8),
 cached_price NUMERIC(16,8),
 output_price NUMERIC(16,8),
 cost_usd NUMERIC(24,12)
);
CREATE INDEX IF NOT EXISTS ai_token_usage_at ON ai_token_usage(at);
ALTER TABLE ai_token_usage ENABLE ROW LEVEL SECURITY;
ALTER TABLE ai_token_usage FORCE ROW LEVEL SECURITY;
ALTER TABLE ai_token_collection ENABLE ROW LEVEL SECURITY;
ALTER TABLE ai_token_collection FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS ai_token_usage_admin ON ai_token_usage;
DROP POLICY IF EXISTS ai_token_collection_admin ON ai_token_collection;
CREATE POLICY ai_token_usage_admin ON ai_token_usage USING(app_rls_bypass()) WITH CHECK(app_rls_bypass());
CREATE POLICY ai_token_collection_admin ON ai_token_collection USING(app_rls_bypass()) WITH CHECK(app_rls_bypass());
COMMIT;
