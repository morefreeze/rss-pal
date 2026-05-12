-- 020_link_set.sql
-- link_set feature: opt-in flag on feeds, plus child-of-parent expansion on articles.

ALTER TABLE feeds
  ADD COLUMN IF NOT EXISTS expand_links BOOLEAN NOT NULL DEFAULT false;

ALTER TABLE articles
  ADD COLUMN IF NOT EXISTS is_link_set        BOOLEAN NOT NULL DEFAULT false,
  ADD COLUMN IF NOT EXISTS parent_article_id  INT REFERENCES articles(id) ON DELETE CASCADE,
  ADD COLUMN IF NOT EXISTS processing_state   VARCHAR(16) NOT NULL DEFAULT 'ready',
  ADD COLUMN IF NOT EXISTS prerank_score      FLOAT,
  ADD COLUMN IF NOT EXISTS editor_note        TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_articles_parent
  ON articles(parent_article_id) WHERE parent_article_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_articles_link_set
  ON articles(feed_id, fetched_at DESC) WHERE is_link_set = true;

CREATE INDEX IF NOT EXISTS idx_articles_stubs
  ON articles(id) WHERE processing_state = 'stub';

CREATE INDEX IF NOT EXISTS idx_articles_processing
  ON articles(id) WHERE processing_state = 'processing';

CREATE UNIQUE INDEX IF NOT EXISTS uniq_link_set_child_url
  ON articles(parent_article_id, url) WHERE parent_article_id IS NOT NULL;

-- Replace global articles_feed_id_url_key with a partial that excludes children
-- (children are governed by uniq_link_set_child_url above).
ALTER TABLE articles DROP CONSTRAINT IF EXISTS articles_feed_id_url_key;
CREATE UNIQUE INDEX IF NOT EXISTS uniq_articles_feed_url_no_child
  ON articles(feed_id, url) WHERE parent_article_id IS NULL;
