-- One-time operator repair for the seven confirmed owner-1 legacy rows.
-- Back up feeds first. Execute with psql -v ON_ERROR_STOP=1 as an admin.
-- A single DO statement is atomic, including guard failures. Re-runs are safe.
DO $$
DECLARE
    source RECORD;
BEGIN
    LOCK TABLE feeds, articles IN SHARE ROW EXCLUSIVE MODE;
    IF NOT EXISTS (SELECT 1 FROM feeds WHERE id=27 AND owner_id=1
        AND url='bookmarklet://user/1' AND feed_type='clip') THEN
        RAISE EXCEPTION 'Canonical clip feed 27 has changed; manual review required';
    END IF;
    IF EXISTS (SELECT 1 FROM feeds WHERE id=61) THEN
        IF NOT EXISTS (SELECT 1 FROM feeds WHERE id=61 AND owner_id=1
            AND url='https://bookmarklet://user/1' AND feed_type='rss'
            AND COALESCE(BTRIM(title),'')='' AND provider_source_id IS NULL)
            OR EXISTS (SELECT 1 FROM articles WHERE feed_id=61) THEN
            RAISE EXCEPTION 'Duplicate clip feed 61 has changed or has articles; refusing deletion';
        END IF;
    END IF;

    FOR source IN SELECT * FROM (VALUES
        (45, 'https://extension://twitter:user/1/alibaba_qwen', 'extension://twitter:user/1/alibaba_qwen', 'twitter:user', 'alibaba_qwen', 'X 用户 · @alibaba_qwen'),
        (46, 'https://extension://twitter:user/1/fred17304866704', 'extension://twitter:user/1/fred17304866704', 'twitter:user', 'fred17304866704', 'X 用户 · @fred17304866704'),
        (47, 'https://extension://twitter:user/1/zarazhangrui', 'extension://twitter:user/1/zarazhangrui', 'twitter:user', 'zarazhangrui', 'X 用户 · @zarazhangrui'),
        (48, 'https://extension://twitter:user/1/zhang_benita', 'extension://twitter:user/1/zhang_benita', 'twitter:user', 'zhang_benita', 'X 用户 · @zhang_benita'),
        (49, 'https://extension://twitter:bookmarks/1/self', 'extension://twitter:bookmarks/1/self', 'twitter:bookmarks', 'self', 'X 书签'),
        (59, 'https://buttondown.com/hacker-newsletter/archive/793/', 'https://buttondown.com/hacker-newsletter/archive/793/', 'html', NULL, 'Hacker Newsletter · 第 793 期')
    ) AS s(id, old_url, new_url, kind, provider_id, label)
    LOOP
        IF EXISTS (SELECT 1 FROM feeds WHERE id=source.id AND owner_id=1
            AND url=source.old_url AND feed_type='rss'
            AND COALESCE(BTRIM(title),'')='' AND provider_source_id IS NULL) THEN
            UPDATE feeds SET url=source.new_url, feed_type=source.kind,
                title=source.label, provider_source_id=source.provider_id,
                etag=NULL, last_modified=NULL, last_attempt_at=NULL,
                next_attempt_at=NULL, fetch_error='', fetch_failures=0
            WHERE id=source.id;
        ELSIF NOT EXISTS (SELECT 1 FROM feeds WHERE id=source.id AND owner_id=1
            AND url=source.new_url AND feed_type=source.kind
            AND provider_source_id IS NOT DISTINCT FROM source.provider_id
            AND title=source.label) THEN
            RAISE EXCEPTION 'Feed % has changed; manual review required', source.id;
        END IF;
    END LOOP;

    DELETE FROM feeds WHERE id=61;
END $$;
