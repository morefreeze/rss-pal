BEGIN;
CREATE TABLE IF NOT EXISTS englife_connection (
 id BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK(id), secret BYTEA NOT NULL,
 account_id TEXT NOT NULL, account TEXT NOT NULL, state TEXT NOT NULL, connection_id TEXT NOT NULL,
 checked_at TIMESTAMPTZ NOT NULL, remaining INTEGER
);
CREATE TABLE IF NOT EXISTS englife_pairing (
 id BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK(id), token_hash TEXT NOT NULL,
 admin_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 expires_at TIMESTAMPTZ NOT NULL
);
CREATE TABLE IF NOT EXISTS englife_jobs (
 account_id TEXT NOT NULL, video_id VARCHAR(11) NOT NULL, language TEXT NOT NULL,
 remote_id TEXT NOT NULL DEFAULT '', status TEXT NOT NULL, output TEXT NOT NULL DEFAULT '',
 next_check_at TIMESTAMPTZ NOT NULL, updated_at TIMESTAMPTZ NOT NULL,
 PRIMARY KEY(account_id,video_id,language)
);
ALTER TABLE englife_connection ENABLE ROW LEVEL SECURITY;
ALTER TABLE englife_connection FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS englife_connection_internal ON englife_connection;
CREATE POLICY englife_connection_internal ON englife_connection USING(app_rls_bypass()) WITH CHECK(app_rls_bypass());
ALTER TABLE englife_pairing ENABLE ROW LEVEL SECURITY;
ALTER TABLE englife_pairing FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS englife_pairing_internal ON englife_pairing;
CREATE POLICY englife_pairing_internal ON englife_pairing USING(app_rls_bypass()) WITH CHECK(app_rls_bypass());
ALTER TABLE englife_jobs ENABLE ROW LEVEL SECURITY;
ALTER TABLE englife_jobs FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS englife_jobs_internal ON englife_jobs;
CREATE POLICY englife_jobs_internal ON englife_jobs USING(app_rls_bypass()) WITH CHECK(app_rls_bypass());
ALTER TABLE articles ADD COLUMN IF NOT EXISTS transcript_next_attempt_at TIMESTAMPTZ;
COMMIT;
