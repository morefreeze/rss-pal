CREATE TABLE IF NOT EXISTS reading_history (
    id BIGSERIAL PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    article_id INTEGER REFERENCES articles(id) ON DELETE CASCADE,
    explore_article_id INTEGER REFERENCES explore_articles(id) ON DELETE SET NULL,
    explore_article_key TEXT,
    explore_title TEXT NOT NULL DEFAULT '',
    explore_source_title TEXT NOT NULL DEFAULT '',
    opened_at TIMESTAMPTZ NOT NULL DEFAULT statement_timestamp(),
    CHECK (num_nonnulls(article_id, explore_article_key) = 1),
    CHECK (explore_article_id IS NULL OR explore_article_key IS NOT NULL)
);

CREATE UNIQUE INDEX IF NOT EXISTS reading_history_article_user
    ON reading_history (article_id, user_id) WHERE article_id IS NOT NULL;
-- Explore articles are cached. Preserve what the user opened even if the
-- cached row expires, and reuse the record when the same URL is opened again.
CREATE UNIQUE INDEX IF NOT EXISTS reading_history_explore_key_user
    ON reading_history (explore_article_key, user_id) WHERE explore_article_key IS NOT NULL;
CREATE INDEX IF NOT EXISTS reading_history_explore_article_id
    ON reading_history (explore_article_id) WHERE explore_article_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS reading_history_user_recency
    ON reading_history (user_id, opened_at DESC, id DESC);

ALTER TABLE reading_history ENABLE ROW LEVEL SECURITY;
ALTER TABLE reading_history FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS reading_history_user_isolation ON reading_history;
CREATE POLICY reading_history_user_isolation ON reading_history
    USING (app_rls_bypass() OR user_id = app_current_user_id())
    WITH CHECK (app_rls_bypass() OR user_id = app_current_user_id());
