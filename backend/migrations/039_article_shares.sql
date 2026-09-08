-- Immutable public article snapshots. This table intentionally does not use
-- RLS because public-token resolution happens before app.user_id is set.
-- Owner-facing endpoints must always filter created_by, and creation must use
-- an article row already authorized by the request's RLS transaction.

CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE IF NOT EXISTS article_shares (
    public_id VARCHAR(32) PRIMARY KEY,
    article_id INTEGER NOT NULL REFERENCES articles(id) ON DELETE CASCADE,
    created_by INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    snapshot_version SMALLINT NOT NULL DEFAULT 1 CHECK (snapshot_version = 1),
    snapshot JSONB NOT NULL,
    expires_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ,
    legacy_token_digest CHAR(64),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (expires_at IS NULL OR expires_at > created_at)
);

CREATE INDEX IF NOT EXISTS idx_article_shares_owner_article_created
    ON article_shares (created_by, article_id, created_at DESC);

CREATE UNIQUE INDEX IF NOT EXISTS idx_article_shares_legacy_digest
    ON article_shares (legacy_token_digest)
    WHERE legacy_token_digest IS NOT NULL;

DO $$
BEGIN
    IF to_regclass('share_tokens') IS NOT NULL THEN
        IF EXISTS (
            SELECT 1
              FROM share_tokens st
              LEFT JOIN articles a ON a.id = st.article_id
              LEFT JOIN feeds f ON f.id = a.feed_id
             WHERE st.created_by IS NULL OR a.id IS NULL OR f.id IS NULL
        ) THEN
            RAISE EXCEPTION 'share_tokens contains rows that cannot be migrated safely';
        END IF;

        -- Dynamic SQL is required: after a successful run share_tokens is
        -- dropped, and a later idempotent run must not parse a stale relation.
        EXECUTE $copy$
            INSERT INTO article_shares (
                public_id, article_id, created_by, snapshot_version, snapshot,
                expires_at, legacy_token_digest, created_at
            )
            SELECT encode(gen_random_bytes(16), 'hex'), st.article_id, st.created_by, 1,
                   jsonb_build_object(
                       'title', a.title,
                       'url', a.url,
                       'feed_title', COALESCE(f.title, ''),
                       'published_at', a.published_at AT TIME ZONE current_setting('TIMEZONE'),
                       'word_count', COALESCE(a.word_count, 0),
                       'reading_minutes', COALESCE(a.reading_minutes, 0),
                       'summary_brief', COALESCE(a.summary_brief, ''),
                       'summary_detailed', COALESCE(a.summary_detailed, ''),
                       'content', COALESCE(a.content, ''),
                       'media_url', COALESCE(a.media_url, ''),
                       'media_type', COALESCE(a.media_type, ''),
                       'media_duration_seconds', COALESCE(a.media_duration_seconds, 0),
                       'image_dimensions', COALESCE(a.image_dimensions, '{}'::jsonb),
                       'snapshotted_at', NOW()
                   ),
                   NOW() + INTERVAL '30 days',
                   encode(digest(st.token, 'sha256'), 'hex'),
                   st.created_at AT TIME ZONE current_setting('TIMEZONE')
              FROM share_tokens st
              JOIN articles a ON a.id = st.article_id
              JOIN feeds f ON f.id = a.feed_id
            ON CONFLICT (legacy_token_digest)
                WHERE legacy_token_digest IS NOT NULL DO NOTHING
        $copy$;

        DROP TABLE share_tokens;
    END IF;
END
$$;
