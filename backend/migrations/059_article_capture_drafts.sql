-- Shortcut shares wait in the original article's private confirmation list.
CREATE TABLE IF NOT EXISTS article_capture_drafts (
 user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 article_id INTEGER NOT NULL REFERENCES articles(id) ON DELETE CASCADE,
 url TEXT NOT NULL CHECK (char_length(url) BETWEEN 1 AND 4096),
 title TEXT NOT NULL DEFAULT '',
 added_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY (user_id, article_id, url)
);
ALTER TABLE article_capture_drafts ENABLE ROW LEVEL SECURITY;
ALTER TABLE article_capture_drafts FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS article_capture_drafts_owner ON article_capture_drafts;
CREATE POLICY article_capture_drafts_owner ON article_capture_drafts
 USING (app_rls_bypass() OR (user_id=app_current_user_id() AND EXISTS (
  SELECT 1 FROM articles a JOIN feeds f ON f.id=a.feed_id WHERE a.id=article_id AND f.owner_id=user_id
 )))
 WITH CHECK (app_rls_bypass() OR (user_id=app_current_user_id() AND EXISTS (
  SELECT 1 FROM articles a JOIN feeds f ON f.id=a.feed_id WHERE a.id=article_id AND f.owner_id=user_id
 )));
CREATE INDEX IF NOT EXISTS article_capture_drafts_article ON article_capture_drafts(article_id);
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='rsspal_app') THEN
  GRANT SELECT,INSERT,UPDATE,DELETE ON article_capture_drafts TO rsspal_app;
 END IF;
END $$;
