-- 030_feeds_provider_source_id.sql
-- Adds provider_source_id to feeds so non-RSS sources (twitter list,
-- twitter user, twitter bookmarks) can be uniquely identified per owner.
-- For rss/html/clip feeds this column stays NULL.
--
-- The existing feed_type column already discriminates 'rss' | 'html' | 'clip';
-- this migration extends the usable value set to include 'twitter:list',
-- 'twitter:user', 'twitter:bookmarks' without a schema change (feed_type is TEXT).

ALTER TABLE feeds
  ADD COLUMN IF NOT EXISTS provider_source_id TEXT;

CREATE UNIQUE INDEX IF NOT EXISTS idx_feeds_owner_type_source
  ON feeds(owner_id, feed_type, provider_source_id)
  WHERE provider_source_id IS NOT NULL;
