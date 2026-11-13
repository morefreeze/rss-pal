BEGIN;

ALTER TABLE articles
    ADD COLUMN IF NOT EXISTS refetch_retry_started_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS refetch_next_attempt_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS transcript_retry_started_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS transcript_retry_attempts INTEGER NOT NULL DEFAULT 0;

CREATE INDEX IF NOT EXISTS articles_expiring_link_fetches
    ON articles(refetch_retry_started_at)
    WHERE parent_article_id IS NOT NULL AND processing_state = 'processing'
        AND refetch_retry_started_at IS NOT NULL;

COMMIT;
