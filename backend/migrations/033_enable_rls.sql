-- backend/migrations/033_enable_rls.sql
-- Row-Level Security: defense-in-depth multi-tenant isolation.
--
-- Conventions:
--   * Two custom GUCs drive every policy:
--       app.user_id    — the authenticated user's id, set by RLSTxMiddleware
--                        via set_config(..., true) inside the per-request tx.
--       app.bypass_rls — 'true' on worker / migration / admin contexts that
--                        legitimately need cross-user access.
--   * current_setting(name, true) returns '' (not error) when unset, so a
--     missing app.user_id casts to NULL and the USING clause fails closed.
--   * Policies are PERMISSIVE (default). One policy per table.
--   * Tables NOT enabled for RLS: users, invite_codes (auth-layer concerns,
--     authorisation is at the API not the row), share_tokens (intentionally
--     public via token), feed_health_metrics, recommended_feeds,
--     link_set_candidates (global / admin-only views).
--   * The users table needs no RLS because (a) Register has no app.user_id
--     in context (pre-auth) and (b) the username/password is itself the
--     auth boundary.

-- Helper functions inlined into every policy USING/WITH CHECK.
CREATE OR REPLACE FUNCTION app_current_user_id() RETURNS INT AS $$
    SELECT NULLIF(current_setting('app.user_id', true), '')::int;
$$ LANGUAGE sql STABLE;

CREATE OR REPLACE FUNCTION app_rls_bypass() RETURNS BOOLEAN AS $$
    SELECT COALESCE(current_setting('app.bypass_rls', true), '') = 'true';
$$ LANGUAGE sql STABLE;

-- ============================================================
-- Private tables: scoped by user_id
-- ============================================================

ALTER TABLE reading_progress ENABLE ROW LEVEL SECURITY;
ALTER TABLE reading_progress FORCE ROW LEVEL SECURITY;
CREATE POLICY reading_progress_user_isolation ON reading_progress
    USING (app_rls_bypass() OR user_id = app_current_user_id())
    WITH CHECK (app_rls_bypass() OR user_id = app_current_user_id());

ALTER TABLE playback_progress ENABLE ROW LEVEL SECURITY;
ALTER TABLE playback_progress FORCE ROW LEVEL SECURITY;
CREATE POLICY playback_progress_user_isolation ON playback_progress
    USING (app_rls_bypass() OR user_id = app_current_user_id())
    WITH CHECK (app_rls_bypass() OR user_id = app_current_user_id());

ALTER TABLE user_preferences ENABLE ROW LEVEL SECURITY;
ALTER TABLE user_preferences FORCE ROW LEVEL SECURITY;
CREATE POLICY user_preferences_user_isolation ON user_preferences
    USING (app_rls_bypass() OR user_id = app_current_user_id())
    WITH CHECK (app_rls_bypass() OR user_id = app_current_user_id());

ALTER TABLE user_tags ENABLE ROW LEVEL SECURITY;
ALTER TABLE user_tags FORCE ROW LEVEL SECURITY;
CREATE POLICY user_tags_user_isolation ON user_tags
    USING (app_rls_bypass() OR user_id = app_current_user_id())
    WITH CHECK (app_rls_bypass() OR user_id = app_current_user_id());

ALTER TABLE article_user_tags ENABLE ROW LEVEL SECURITY;
ALTER TABLE article_user_tags FORCE ROW LEVEL SECURITY;
CREATE POLICY article_user_tags_user_isolation ON article_user_tags
    USING (app_rls_bypass() OR user_id = app_current_user_id())
    WITH CHECK (app_rls_bypass() OR user_id = app_current_user_id());

ALTER TABLE tag_suggestion_dismissals ENABLE ROW LEVEL SECURITY;
ALTER TABLE tag_suggestion_dismissals FORCE ROW LEVEL SECURITY;
CREATE POLICY tag_suggestion_dismissals_user_isolation ON tag_suggestion_dismissals
    USING (app_rls_bypass() OR user_id = app_current_user_id())
    WITH CHECK (app_rls_bypass() OR user_id = app_current_user_id());

ALTER TABLE interest_topics ENABLE ROW LEVEL SECURITY;
ALTER TABLE interest_topics FORCE ROW LEVEL SECURITY;
CREATE POLICY interest_topics_user_isolation ON interest_topics
    USING (app_rls_bypass() OR user_id = app_current_user_id())
    WITH CHECK (app_rls_bypass() OR user_id = app_current_user_id());

ALTER TABLE interest_tags ENABLE ROW LEVEL SECURITY;
ALTER TABLE interest_tags FORCE ROW LEVEL SECURITY;
CREATE POLICY interest_tags_user_isolation ON interest_tags
    USING (app_rls_bypass() OR user_id = app_current_user_id())
    WITH CHECK (app_rls_bypass() OR user_id = app_current_user_id());

ALTER TABLE interest_categories ENABLE ROW LEVEL SECURITY;
ALTER TABLE interest_categories FORCE ROW LEVEL SECURITY;
CREATE POLICY interest_categories_user_isolation ON interest_categories
    USING (app_rls_bypass() OR user_id = app_current_user_id())
    WITH CHECK (app_rls_bypass() OR user_id = app_current_user_id());

ALTER TABLE user_insights ENABLE ROW LEVEL SECURITY;
ALTER TABLE user_insights FORCE ROW LEVEL SECURITY;
CREATE POLICY user_insights_user_isolation ON user_insights
    USING (app_rls_bypass() OR user_id = app_current_user_id())
    WITH CHECK (app_rls_bypass() OR user_id = app_current_user_id());

ALTER TABLE article_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE article_events FORCE ROW LEVEL SECURITY;
CREATE POLICY article_events_user_isolation ON article_events
    USING (app_rls_bypass() OR user_id = app_current_user_id())
    WITH CHECK (app_rls_bypass() OR user_id = app_current_user_id());

ALTER TABLE weekly_digests ENABLE ROW LEVEL SECURITY;
ALTER TABLE weekly_digests FORCE ROW LEVEL SECURITY;
CREATE POLICY weekly_digests_user_isolation ON weekly_digests
    USING (app_rls_bypass() OR user_id = app_current_user_id())
    WITH CHECK (app_rls_bypass() OR user_id = app_current_user_id());

ALTER TABLE daily_digests ENABLE ROW LEVEL SECURITY;
ALTER TABLE daily_digests FORCE ROW LEVEL SECURITY;
CREATE POLICY daily_digests_user_isolation ON daily_digests
    USING (app_rls_bypass() OR user_id = app_current_user_id())
    WITH CHECK (app_rls_bypass() OR user_id = app_current_user_id());

ALTER TABLE hidden_articles ENABLE ROW LEVEL SECURITY;
ALTER TABLE hidden_articles FORCE ROW LEVEL SECURITY;
CREATE POLICY hidden_articles_user_isolation ON hidden_articles
    USING (app_rls_bypass() OR user_id = app_current_user_id())
    WITH CHECK (app_rls_bypass() OR user_id = app_current_user_id());

ALTER TABLE user_ai_configs ENABLE ROW LEVEL SECURITY;
ALTER TABLE user_ai_configs FORCE ROW LEVEL SECURITY;
CREATE POLICY user_ai_configs_user_isolation ON user_ai_configs
    USING (app_rls_bypass() OR user_id = app_current_user_id())
    WITH CHECK (app_rls_bypass() OR user_id = app_current_user_id());

-- ============================================================
-- Shared-but-owned tables
-- ============================================================

-- feeds: shared rows (owner_id IS NULL) visible to all; owned rows only to owner.
ALTER TABLE feeds ENABLE ROW LEVEL SECURITY;
ALTER TABLE feeds FORCE ROW LEVEL SECURITY;
CREATE POLICY feeds_owner_isolation ON feeds
    USING (
        app_rls_bypass()
        OR owner_id IS NULL
        OR owner_id = app_current_user_id()
    )
    WITH CHECK (
        app_rls_bypass()
        OR owner_id IS NULL
        OR owner_id = app_current_user_id()
    );

-- articles: visibility via parent feed. Articles are shared content, so
-- multiple users may legitimately see the same article row.
ALTER TABLE articles ENABLE ROW LEVEL SECURITY;
ALTER TABLE articles FORCE ROW LEVEL SECURITY;
CREATE POLICY articles_via_feed ON articles
    USING (
        app_rls_bypass()
        OR EXISTS (
            SELECT 1 FROM feeds f
            WHERE f.id = articles.feed_id
              AND (f.owner_id IS NULL OR f.owner_id = app_current_user_id())
        )
    )
    WITH CHECK (
        app_rls_bypass()
        OR EXISTS (
            SELECT 1 FROM feeds f
            WHERE f.id = articles.feed_id
              AND (f.owner_id IS NULL OR f.owner_id = app_current_user_id())
        )
    );

-- summary_templates: system templates (user_id IS NULL, is_system=true)
-- visible to all; per-user templates only to owner.
ALTER TABLE summary_templates ENABLE ROW LEVEL SECURITY;
ALTER TABLE summary_templates FORCE ROW LEVEL SECURITY;
CREATE POLICY summary_templates_isolation ON summary_templates
    USING (
        app_rls_bypass()
        OR (is_system = true AND user_id IS NULL)
        OR user_id = app_current_user_id()
    )
    WITH CHECK (
        app_rls_bypass()
        OR user_id = app_current_user_id()
    );
