-- Short-lived, one-use snapshots for bulk read actions. Never stores article content.
CREATE TABLE IF NOT EXISTS reading_read_undo (
 token TEXT PRIMARY KEY,
 user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 snapshot JSONB NOT NULL,
 marked_at TIMESTAMP NOT NULL,
 expires_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS reading_read_undo_expiry ON reading_read_undo(user_id, expires_at);
ALTER TABLE reading_read_undo ENABLE ROW LEVEL SECURITY;
ALTER TABLE reading_read_undo FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS reading_read_undo_user ON reading_read_undo;
CREATE POLICY reading_read_undo_user ON reading_read_undo
 USING (app_rls_bypass() OR user_id = app_current_user_id())
 WITH CHECK (app_rls_bypass() OR user_id = app_current_user_id());
DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'rsspal_app') THEN
  GRANT SELECT, INSERT, UPDATE, DELETE ON reading_read_undo TO rsspal_app;
 END IF;
END $$;
