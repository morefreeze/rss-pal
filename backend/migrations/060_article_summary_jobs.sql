-- Durable summary leases coordinate the API and worker across processes.
CREATE TABLE IF NOT EXISTS article_summary_jobs (
 article_id INTEGER PRIMARY KEY REFERENCES articles(id) ON DELETE CASCADE,
 state TEXT NOT NULL CHECK(state IN ('running','failed','done')),
 lease_token TEXT NOT NULL,
 lease_expires_at TIMESTAMPTZ,
 attempts INTEGER NOT NULL DEFAULT 1,
 last_error TEXT NOT NULL DEFAULT '',
 retry_at TIMESTAMPTZ,
 updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()
);
ALTER TABLE article_summary_jobs ENABLE ROW LEVEL SECURITY;
ALTER TABLE article_summary_jobs FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS article_summary_jobs_visible ON article_summary_jobs;
CREATE POLICY article_summary_jobs_visible ON article_summary_jobs
 USING (app_rls_bypass() OR EXISTS (SELECT 1 FROM articles a WHERE a.id=article_id))
 WITH CHECK (app_rls_bypass() OR EXISTS (SELECT 1 FROM articles a WHERE a.id=article_id));
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='rsspal_app') THEN
  GRANT SELECT,INSERT,UPDATE,DELETE ON article_summary_jobs TO rsspal_app;
 END IF;
END $$;
