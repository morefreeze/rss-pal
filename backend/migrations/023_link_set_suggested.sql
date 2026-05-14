-- 023_link_set_suggested.sql
-- Track auto-detected "potential link_set" status for rss-typed articles.
-- The worker runs a stricter rule (>=11 candidate links forming a continuous
-- one-per-line block with at most 2 gap segments) on rss articles and writes
-- the result here. The frontend surfaces a confirm button; only after the
-- user confirms do we flip links_extendable=true and enable batch fetch.
--
--   NULL  = not yet checked (or not applicable, e.g. feed_type != 'rss')
--   true  = worker detected a link-list pattern, awaiting user confirmation
--   false = worker checked and did not detect a list pattern

ALTER TABLE articles ADD COLUMN IF NOT EXISTS link_set_suggested BOOLEAN;

-- Re-check rss articles that the legacy worker may have marked false:
-- the old worker only scanned link_set/saved feeds so this is defensive,
-- but we want to be sure rss rows are eligible under the new rule.
UPDATE articles a
SET links_extendable = NULL
FROM feeds f
WHERE a.feed_id = f.id
  AND f.feed_type = 'rss'
  AND a.links_extendable IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_articles_link_set_suggested
  ON articles(feed_id, fetched_at DESC) WHERE link_set_suggested = true;
