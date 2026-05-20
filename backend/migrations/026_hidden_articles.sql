-- Per-user soft delete ("hide") for articles. The article row itself is
-- untouched; this table is a pure per-user visibility overlay used by every
-- user-facing list endpoint.
CREATE TABLE IF NOT EXISTS hidden_articles (
    id         SERIAL PRIMARY KEY,
    user_id    INT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    article_id INT NOT NULL REFERENCES articles(id) ON DELETE CASCADE,
    hidden_at  TIMESTAMP NOT NULL DEFAULT NOW(),
    UNIQUE (user_id, article_id)
);

CREATE INDEX IF NOT EXISTS idx_hidden_articles_user
    ON hidden_articles(user_id);
