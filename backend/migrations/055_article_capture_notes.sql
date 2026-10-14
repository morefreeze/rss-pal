-- Private reader-authored capture notes never enter article content or shares.
CREATE TABLE IF NOT EXISTS article_capture_notes (
    user_id INT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    article_id INT NOT NULL REFERENCES articles(id) ON DELETE CASCADE,
    capture_note TEXT NOT NULL DEFAULT '' CHECK (char_length(capture_note) <= 10000),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, article_id)
);
ALTER TABLE article_capture_notes ENABLE ROW LEVEL SECURITY;
ALTER TABLE article_capture_notes FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS article_capture_notes_user_isolation ON article_capture_notes;
CREATE POLICY article_capture_notes_user_isolation ON article_capture_notes
    USING (app_rls_bypass() OR user_id = app_current_user_id())
    WITH CHECK (app_rls_bypass() OR user_id = app_current_user_id());
CREATE INDEX IF NOT EXISTS idx_article_capture_notes_article ON article_capture_notes(article_id);
