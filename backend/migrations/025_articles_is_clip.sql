-- 025_articles_is_clip.sql
-- Add is_clip boolean to articles so the dedup unique index can exclude
-- clip-bin captures. The clip path needs to allow multiple rows sharing
-- (feed_id, url) — the user explicitly clicks 新建 to keep both when two
-- captures legitimately have the same URL but different content.

ALTER TABLE articles
  ADD COLUMN IF NOT EXISTS is_clip BOOLEAN NOT NULL DEFAULT false;

UPDATE articles SET is_clip = true
  WHERE feed_id IN (SELECT id FROM feeds WHERE feed_type = 'clip');

DROP INDEX IF EXISTS uniq_articles_feed_url_no_child;
CREATE UNIQUE INDEX uniq_articles_feed_url_no_child
  ON articles(feed_id, url)
  WHERE parent_article_id IS NULL AND NOT is_clip;
