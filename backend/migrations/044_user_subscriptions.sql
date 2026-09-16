-- Formal subscriptions belong to one user. The public recommendation catalog
-- and immutable article-share snapshots are independent of subscription rows.
BEGIN;
LOCK TABLE feeds IN SHARE ROW EXCLUSIVE MODE;
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM feeds WHERE owner_id IS NULL) THEN
        IF NOT EXISTS (SELECT 1 FROM users WHERE id = 1 AND is_admin) THEN
            RAISE EXCEPTION 'legacy subscriptions require administrator user 1';
        END IF;
        IF EXISTS (
            SELECT 1 FROM feeds legacy JOIN feeds owned ON owned.url = legacy.url
            WHERE legacy.owner_id IS NULL AND owned.owner_id = 1
        ) THEN
            RAISE EXCEPTION 'legacy subscriptions conflict with administrator URLs; resolve explicitly before migration';
        END IF;
        UPDATE feeds SET owner_id = 1 WHERE owner_id IS NULL;
    END IF;
END $$;
DROP POLICY IF EXISTS feeds_visibility ON feeds;
DROP POLICY IF EXISTS feeds_owner_isolation ON feeds;
CREATE POLICY feeds_owner_isolation ON feeds
    USING (app_rls_bypass() OR owner_id = app_current_user_id())
    WITH CHECK (app_rls_bypass() OR owner_id = app_current_user_id());
DROP POLICY IF EXISTS articles_via_feed ON articles;
CREATE POLICY articles_via_feed ON articles
    USING (app_rls_bypass() OR EXISTS (
        SELECT 1 FROM feeds f WHERE f.id = articles.feed_id AND f.owner_id = app_current_user_id()
    ))
    WITH CHECK (app_rls_bypass() OR EXISTS (
        SELECT 1 FROM feeds f WHERE f.id = articles.feed_id AND f.owner_id = app_current_user_id()
    ));
COMMIT;
