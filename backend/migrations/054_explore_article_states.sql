-- Neutral per-account article decisions, independent of ranking feedback.
-- URL keys intentionally survive shared article-cache pruning/reingestion.
CREATE TABLE IF NOT EXISTS explore_article_states (
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    normalized_url VARCHAR(2048) NOT NULL,
    saved BOOLEAN NOT NULL DEFAULT false,
    skipped BOOLEAN NOT NULL DEFAULT false,
    saved_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, normalized_url)
);
CREATE INDEX IF NOT EXISTS idx_explore_article_states_saved_url
    ON explore_article_states (normalized_url) WHERE saved;
CREATE INDEX IF NOT EXISTS idx_explore_articles_normalized_url ON explore_articles (normalized_url);
ALTER TABLE explore_article_states ENABLE ROW LEVEL SECURITY;
ALTER TABLE explore_article_states FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS explore_article_states_user_isolation ON explore_article_states;
CREATE POLICY explore_article_states_user_isolation ON explore_article_states
    USING (app_rls_bypass() OR user_id=app_current_user_id())
    WITH CHECK (app_rls_bypass() OR user_id=app_current_user_id());
GRANT SELECT, INSERT, UPDATE, DELETE ON explore_article_states TO rsspal_app;
