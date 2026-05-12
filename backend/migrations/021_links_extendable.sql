-- 021_links_extendable.sql
-- Switch from eager-expand to opt-in batch fetch:
--  - rename is_link_set -> links_extendable (semantics shift: "detected as
--    having extractable outbound links" rather than "should be expanded")
--  - make nullable: NULL = not yet checked, true = extendable, false = checked-no
--  - reset all values to NULL so the worker re-detects under the new threshold

ALTER TABLE articles RENAME COLUMN is_link_set TO links_extendable;
ALTER TABLE articles ALTER COLUMN links_extendable DROP NOT NULL;
ALTER TABLE articles ALTER COLUMN links_extendable DROP DEFAULT;
UPDATE articles SET links_extendable = NULL;

DROP INDEX IF EXISTS idx_articles_link_set;
CREATE INDEX IF NOT EXISTS idx_articles_links_extendable
  ON articles(feed_id, fetched_at DESC) WHERE links_extendable = true;
CREATE INDEX IF NOT EXISTS idx_articles_links_unchecked
  ON articles(id) WHERE links_extendable IS NULL;

-- Clean up old eager-model children (stubs, processing, ready) — the user
-- said the eager expansion was "完全不好"; we're starting clean.
DELETE FROM articles WHERE parent_article_id IS NOT NULL;
