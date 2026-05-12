-- 022_link_set_candidates.sql
-- Cache the candidates extracted during worker detection so the
-- batch-fetch modal opens instantly instead of re-fetching HTML.

CREATE TABLE IF NOT EXISTS link_set_candidates (
  id                SERIAL PRIMARY KEY,
  parent_article_id INT NOT NULL REFERENCES articles(id) ON DELETE CASCADE,
  url               TEXT NOT NULL,
  title             TEXT NOT NULL,
  editor_note       TEXT NOT NULL DEFAULT '',
  position          INT NOT NULL DEFAULT 0,
  created_at        TIMESTAMP NOT NULL DEFAULT NOW(),
  UNIQUE (parent_article_id, url)
);

CREATE INDEX IF NOT EXISTS idx_link_set_candidates_parent_position
  ON link_set_candidates(parent_article_id, position);
