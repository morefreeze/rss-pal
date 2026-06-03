-- Per-user "shared backlog" cutoff. New users joining a team-shared deployment
-- shouldn't be flooded with years of accumulated articles from public feeds —
-- they see only the last N days by default (7), but can pull the slider back.
-- Owner-owned feeds are unaffected (you always see your own private feed in
-- full). Idempotent.

ALTER TABLE users ADD COLUMN IF NOT EXISTS shared_visible_from TIMESTAMP;

-- Backfill existing rows to epoch so current users keep seeing everything.
UPDATE users SET shared_visible_from = '1970-01-01 00:00:00'::TIMESTAMP
 WHERE shared_visible_from IS NULL;

-- Future users: 7-day backlog from registration time.
ALTER TABLE users ALTER COLUMN shared_visible_from SET DEFAULT (NOW() - INTERVAL '7 days');

-- Helper: returns the calling user's floor (or epoch if unset, fail-open for
-- the user — RLS still hides cross-tenant data via the other clauses). Stable
-- so the planner evaluates it once per query.
CREATE OR REPLACE FUNCTION app_user_shared_floor() RETURNS TIMESTAMP AS $$
    SELECT COALESCE(shared_visible_from, '1970-01-01'::TIMESTAMP)
      FROM users WHERE id = app_current_user_id();
$$ LANGUAGE sql STABLE;

GRANT EXECUTE ON FUNCTION app_user_shared_floor() TO rsspal_app;

-- Rebuild the articles policy: shared feeds (owner_id IS NULL) only return
-- articles published at or after the caller's floor. Private feeds owned by
-- the caller return everything regardless.
DROP POLICY IF EXISTS articles_via_feed ON articles;
CREATE POLICY articles_via_feed ON articles
    USING (
        app_rls_bypass()
        OR EXISTS (
            SELECT 1 FROM feeds f
            WHERE f.id = articles.feed_id
              AND (
                  f.owner_id = app_current_user_id()
                  OR (f.owner_id IS NULL
                      AND articles.published_at >= app_user_shared_floor())
              )
        )
    )
    WITH CHECK (
        app_rls_bypass()
        OR EXISTS (
            SELECT 1 FROM feeds f
            WHERE f.id = articles.feed_id
              AND (
                  f.owner_id = app_current_user_id()
                  OR (f.owner_id IS NULL
                      AND articles.published_at >= app_user_shared_floor())
              )
        )
    );
