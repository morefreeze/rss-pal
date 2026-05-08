-- 016_user_tags.sql

CREATE TABLE IF NOT EXISTS user_tags (
    id          SERIAL PRIMARY KEY,
    user_id     INT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name        VARCHAR(64) NOT NULL,
    created_at  TIMESTAMP DEFAULT NOW(),
    UNIQUE (user_id, name)
);

CREATE TABLE IF NOT EXISTS article_user_tags (
    article_id  INT NOT NULL REFERENCES articles(id) ON DELETE CASCADE,
    tag_id      INT NOT NULL REFERENCES user_tags(id) ON DELETE CASCADE,
    user_id     INT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at  TIMESTAMP DEFAULT NOW(),
    PRIMARY KEY (article_id, tag_id)
);

CREATE INDEX IF NOT EXISTS idx_article_user_tags_user_tag
  ON article_user_tags(user_id, tag_id);
CREATE INDEX IF NOT EXISTS idx_article_user_tags_user_article
  ON article_user_tags(user_id, article_id);

CREATE TABLE IF NOT EXISTS tag_suggestion_dismissals (
    article_id INT NOT NULL REFERENCES articles(id) ON DELETE CASCADE,
    user_id    INT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name       VARCHAR(64) NOT NULL,
    created_at TIMESTAMP DEFAULT NOW(),
    PRIMARY KEY (article_id, user_id, name)
);
