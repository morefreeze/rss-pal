-- 029_articles_kind.sql
-- Adds `kind` discriminator to articles. Drives frontend rendering
-- (tweet → TweetCard, default 'article' → existing renderer).
-- Backfills existing twitter status URLs created via the bookmarklet
-- twitter capture path to kind='tweet'.

ALTER TABLE articles
  ADD COLUMN IF NOT EXISTS kind TEXT NOT NULL DEFAULT 'article';

CREATE INDEX IF NOT EXISTS idx_articles_kind
  ON articles(kind)
  WHERE kind <> 'article';

UPDATE articles
  SET kind = 'tweet'
  WHERE kind = 'article'
    AND url ~ '^https://x\.com/[^/]+/status/[0-9]+$';
