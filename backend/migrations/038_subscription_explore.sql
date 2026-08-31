-- Subscription Explore: globally refreshed source/article cache plus
-- user-isolated batches, feedback, and article events. Safe for both initdb
-- and an existing database where the migration runner records no history.

-- A user may subscribe to a URL already subscribed by another user. NULL
-- owner_id represents one shared feed and is included in this key via 0.
ALTER TABLE feeds DROP CONSTRAINT IF EXISTS feeds_url_key;
CREATE UNIQUE INDEX IF NOT EXISTS idx_feeds_owner_url
    ON feeds ((COALESCE(owner_id, 0)), url);

-- The curated registry acquires operational metadata without changing its
-- existing catalog semantics. Existing rows remain pending until verified.
ALTER TABLE recommended_feeds ADD COLUMN IF NOT EXISTS site_url VARCHAR(2048);
ALTER TABLE recommended_feeds ADD COLUMN IF NOT EXISTS normalized_url VARCHAR(2048);
ALTER TABLE recommended_feeds ADD COLUMN IF NOT EXISTS validation_status VARCHAR(16) NOT NULL DEFAULT 'pending';
ALTER TABLE recommended_feeds ADD COLUMN IF NOT EXISTS verified_at TIMESTAMP;
ALTER TABLE recommended_feeds ADD COLUMN IF NOT EXISTS last_checked_at TIMESTAMP;
ALTER TABLE recommended_feeds ADD COLUMN IF NOT EXISTS last_fetched_at TIMESTAMP;
ALTER TABLE recommended_feeds ADD COLUMN IF NOT EXISTS etag VARCHAR(500);
ALTER TABLE recommended_feeds ADD COLUMN IF NOT EXISTS last_modified VARCHAR(500);
ALTER TABLE recommended_feeds ADD COLUMN IF NOT EXISTS health_score DOUBLE PRECISION;
ALTER TABLE recommended_feeds ADD COLUMN IF NOT EXISTS last_error TEXT;
ALTER TABLE recommended_feeds ADD COLUMN IF NOT EXISTS first_discovered_at TIMESTAMP;
ALTER TABLE recommended_feeds ADD COLUMN IF NOT EXISTS last_observed_at TIMESTAMP;

UPDATE recommended_feeds
   SET normalized_url = lower(trim(url))
 WHERE normalized_url IS NULL;
UPDATE recommended_feeds
   SET validation_status = 'pending'
 WHERE validation_status IS NULL;

ALTER TABLE recommended_feeds DROP CONSTRAINT IF EXISTS recommended_feeds_validation_status_check;
ALTER TABLE recommended_feeds ADD CONSTRAINT recommended_feeds_validation_status_check
    CHECK (validation_status IN ('pending', 'valid', 'invalid'));
CREATE INDEX IF NOT EXISTS idx_recommended_feeds_normalized_url
    ON recommended_feeds (normalized_url);

-- Global registry/cache tables. They contain no user profile state.
CREATE TABLE IF NOT EXISTS explore_registry_providers (
    id SERIAL PRIMARY KEY,
    provider_key VARCHAR(100) NOT NULL UNIQUE,
    endpoint VARCHAR(2048) NOT NULL,
    kind VARCHAR(32) NOT NULL,
    topic VARCHAR(100) NOT NULL,
    default_interval_minutes INTEGER NOT NULL DEFAULT 360 CHECK (default_interval_minutes > 0),
    enabled BOOLEAN NOT NULL DEFAULT true,
    last_sync_started_at TIMESTAMP,
    last_synced_at TIMESTAMP,
    last_sync_error TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS explore_source_observations (
    id SERIAL PRIMARY KEY,
    provider_id INTEGER NOT NULL REFERENCES explore_registry_providers(id) ON DELETE CASCADE,
    source_url VARCHAR(2048) NOT NULL,
    normalized_url VARCHAR(2048) NOT NULL,
    title VARCHAR(500) NOT NULL DEFAULT '',
    description TEXT,
    topic VARCHAR(100),
    feed_type VARCHAR(32) NOT NULL DEFAULT 'rss',
    observed_at TIMESTAMP NOT NULL DEFAULT NOW(),
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    UNIQUE (provider_id, normalized_url)
);
CREATE INDEX IF NOT EXISTS idx_explore_source_observations_provider_normalized_url
    ON explore_source_observations (provider_id, normalized_url);

CREATE TABLE IF NOT EXISTS explore_fetch_runs (
    id SERIAL PRIMARY KEY,
    provider_id INTEGER NOT NULL REFERENCES explore_registry_providers(id) ON DELETE CASCADE,
    status VARCHAR(16) NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'running', 'succeeded', 'failed')),
    claimed_count INTEGER NOT NULL DEFAULT 0 CHECK (claimed_count >= 0 AND claimed_count <= 500),
    started_at TIMESTAMP,
    finished_at TIMESTAMP,
    error_message TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_explore_fetch_runs_provider_created_at
    ON explore_fetch_runs (provider_id, created_at DESC);

CREATE TABLE IF NOT EXISTS explore_fetch_queue (
    id SERIAL PRIMARY KEY,
    run_id INTEGER NOT NULL REFERENCES explore_fetch_runs(id) ON DELETE CASCADE,
    source_observation_id INTEGER REFERENCES explore_source_observations(id) ON DELETE CASCADE,
    status VARCHAR(16) NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'claimed', 'done', 'failed')),
    attempt_count INTEGER NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
    claimed_at TIMESTAMP,
    completed_at TIMESTAMP,
    last_error TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_explore_fetch_queue_claimable
    ON explore_fetch_queue (status, created_at)
    WHERE status IN ('pending', 'claimed');

CREATE TABLE IF NOT EXISTS explore_articles (
    id SERIAL PRIMARY KEY,
    source_observation_id INTEGER NOT NULL REFERENCES explore_source_observations(id) ON DELETE CASCADE,
    title VARCHAR(500) NOT NULL,
    url VARCHAR(2048) NOT NULL,
    normalized_url VARCHAR(2048) NOT NULL,
    content TEXT,
    summary_brief TEXT,
    published_at TIMESTAMP,
    fetched_at TIMESTAMP NOT NULL DEFAULT NOW(),
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    UNIQUE (source_observation_id, normalized_url)
);
CREATE INDEX IF NOT EXISTS idx_explore_articles_source_published_at
    ON explore_articles (source_observation_id, published_at DESC);

-- User scoped state. The RLS policies below are deliberately the same
-- fail-closed policy shape as migration 033's other private tables.
CREATE TABLE IF NOT EXISTS explore_batches (
    id SERIAL PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMP,
    generated_at TIMESTAMP NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_explore_batches_user_created_at
    ON explore_batches (user_id, created_at DESC);

CREATE TABLE IF NOT EXISTS explore_batch_sources (
    id SERIAL PRIMARY KEY,
    batch_id INTEGER NOT NULL REFERENCES explore_batches(id) ON DELETE CASCADE,
    source_observation_id INTEGER NOT NULL REFERENCES explore_source_observations(id) ON DELETE CASCADE,
    rank INTEGER NOT NULL CHECK (rank > 0),
    score DOUBLE PRECISION NOT NULL DEFAULT 0,
    reason TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    UNIQUE (batch_id, source_observation_id),
    UNIQUE (batch_id, rank)
);
CREATE INDEX IF NOT EXISTS idx_explore_batch_sources_batch_rank
    ON explore_batch_sources (batch_id, rank);

CREATE TABLE IF NOT EXISTS explore_feedback (
    id SERIAL PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    source_observation_id INTEGER NOT NULL REFERENCES explore_source_observations(id) ON DELETE CASCADE,
    feedback_type VARCHAR(32) NOT NULL CHECK (feedback_type IN ('hide_source', 'less_like_this')),
    revoked_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_explore_feedback_user_source_active
    ON explore_feedback (user_id, source_observation_id, feedback_type)
    WHERE revoked_at IS NULL;

CREATE TABLE IF NOT EXISTS explore_article_events (
    id BIGSERIAL PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    explore_article_id INTEGER NOT NULL REFERENCES explore_articles(id) ON DELETE CASCADE,
    batch_id INTEGER REFERENCES explore_batches(id) ON DELETE SET NULL,
    event_type VARCHAR(32) NOT NULL CHECK (event_type IN ('exposure', 'click', 'read')),
    occurred_at TIMESTAMP NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_explore_article_events_user_article
    ON explore_article_events (user_id, explore_article_id, occurred_at DESC);

ALTER TABLE explore_batches ENABLE ROW LEVEL SECURITY;
ALTER TABLE explore_batches FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS explore_batches_user_isolation ON explore_batches;
CREATE POLICY explore_batches_user_isolation ON explore_batches
    USING (app_rls_bypass() OR user_id = app_current_user_id())
    WITH CHECK (app_rls_bypass() OR user_id = app_current_user_id());

ALTER TABLE explore_batch_sources ENABLE ROW LEVEL SECURITY;
ALTER TABLE explore_batch_sources FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS explore_batch_sources_user_isolation ON explore_batch_sources;
CREATE POLICY explore_batch_sources_user_isolation ON explore_batch_sources
    USING (app_rls_bypass() OR EXISTS (
        SELECT 1 FROM explore_batches b
         WHERE b.id = explore_batch_sources.batch_id
           AND b.user_id = app_current_user_id()
    ))
    WITH CHECK (app_rls_bypass() OR EXISTS (
        SELECT 1 FROM explore_batches b
         WHERE b.id = explore_batch_sources.batch_id
           AND b.user_id = app_current_user_id()
    ));

ALTER TABLE explore_feedback ENABLE ROW LEVEL SECURITY;
ALTER TABLE explore_feedback FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS explore_feedback_user_isolation ON explore_feedback;
CREATE POLICY explore_feedback_user_isolation ON explore_feedback
    USING (app_rls_bypass() OR user_id = app_current_user_id())
    WITH CHECK (app_rls_bypass() OR user_id = app_current_user_id());

ALTER TABLE explore_article_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE explore_article_events FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS explore_article_events_user_isolation ON explore_article_events;
CREATE POLICY explore_article_events_user_isolation ON explore_article_events
    USING (app_rls_bypass() OR user_id = app_current_user_id())
    WITH CHECK (app_rls_bypass() OR user_id = app_current_user_id());

-- Keep operator-controlled enabled state and runtime synchronisation metadata
-- on conflict; release revisions may only refresh stable registry fields.
INSERT INTO explore_registry_providers (provider_key, endpoint, kind, topic, default_interval_minutes)
VALUES
    ('plenary-programming-opml', 'https://raw.githubusercontent.com/spians/awesome-RSS-feeds/master/recommended/with_category/Programming.opml', 'opml', 'programming', 360),
    ('plenary-tech-opml', 'https://raw.githubusercontent.com/spians/awesome-RSS-feeds/master/recommended/with_category/Tech.opml', 'opml', 'technology', 360),
    ('plenary-webdev-opml', 'https://raw.githubusercontent.com/spians/awesome-RSS-feeds/master/recommended/with_category/Web%20Development.opml', 'opml', 'web-development', 360),
    ('chinese-independent', 'https://raw.githubusercontent.com/timqian/chinese-independent-blogs/master/feed.opml', 'opml', 'chinese-independent', 360),
    ('ooh-recently-added', 'https://ooh.directory/feeds/recently-added.xml', 'rss', 'recently-added', 360),
    ('reddit-programming', '/reddit/subreddit/programming', 'rsshub', 'programming', 360),
    ('awesome-selfhosted', 'https://raw.githubusercontent.com/awesome-selfhosted/awesome-selfhosted/master/README.md', 'markdown', 'self-hosted', 360)
ON CONFLICT (provider_key) DO UPDATE
    SET endpoint = EXCLUDED.endpoint,
        kind = EXCLUDED.kind,
        topic = EXCLUDED.topic,
        default_interval_minutes = EXCLUDED.default_interval_minutes;
