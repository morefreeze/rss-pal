-- 013_transcript_fetch.sql
-- Tracks per-article transcript-fetch attempts so the worker doesn't
-- retry indefinitely. NULL = never attempted; non-NULL = attempted once
-- (success or failure). Idempotent.

ALTER TABLE articles
    ADD COLUMN IF NOT EXISTS transcript_fetched_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_articles_transcript_pending
    ON articles (id)
    WHERE transcript_fetched_at IS NULL
      AND media_type IS NOT NULL
      AND (media_type LIKE 'video/%' OR media_type LIKE 'audio/%');
