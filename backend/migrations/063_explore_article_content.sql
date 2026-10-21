-- Shared public-page extraction is independent of periodic feed-body refreshes.
CREATE TABLE IF NOT EXISTS explore_article_content (
 article_id INTEGER PRIMARY KEY REFERENCES explore_articles(id) ON DELETE CASCADE,
 source_url TEXT NOT NULL,
 content TEXT NOT NULL DEFAULT '',
 status TEXT NOT NULL CHECK(status IN ('fetching','fetched','failed')),
 lease_token TEXT NOT NULL,
 lease_expires_at TIMESTAMPTZ,
 retry_at TIMESTAMPTZ,
 attempts INTEGER NOT NULL DEFAULT 1,
 last_error TEXT NOT NULL DEFAULT '',
 updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()
);
ALTER TABLE explore_article_content ENABLE ROW LEVEL SECURITY;
ALTER TABLE explore_article_content FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS explore_article_content_admin ON explore_article_content;
CREATE POLICY explore_article_content_admin ON explore_article_content USING (app_rls_bypass()) WITH CHECK (app_rls_bypass());
DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname='rsspal_app') THEN
  GRANT SELECT,INSERT,UPDATE,DELETE ON explore_article_content TO rsspal_app;
 END IF;
END $$;
