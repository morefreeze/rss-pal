BEGIN;
SET LOCAL app.bypass_rls = 'true';
CREATE TABLE IF NOT EXISTS platform_ai_config (
 id BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK(id),
 revision BIGINT NOT NULL DEFAULT 0 CHECK(revision >= 0),
 config JSONB,
 updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()
);
INSERT INTO platform_ai_config(id) VALUES(TRUE) ON CONFLICT DO NOTHING;
ALTER TABLE platform_ai_config ENABLE ROW LEVEL SECURITY;
ALTER TABLE platform_ai_config FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS platform_ai_config_admin ON platform_ai_config;
CREATE POLICY platform_ai_config_admin ON platform_ai_config USING(app_rls_bypass()) WITH CHECK(app_rls_bypass());
COMMIT;
