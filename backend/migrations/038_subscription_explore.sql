-- Subscription Explore: a shared source/article cache and per-user snapshots.
-- This migration is safe to re-run after a partially completed initdb.

ALTER TABLE feeds DROP CONSTRAINT IF EXISTS feeds_url_key;
CREATE UNIQUE INDEX IF NOT EXISTS idx_feeds_owner_url
    ON feeds ((COALESCE(owner_id, 0)), url);

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
   SET normalized_url = lower(btrim(url))
 WHERE normalized_url IS NULL;
UPDATE recommended_feeds
   SET validation_status = 'pending'
 WHERE validation_status IS NULL;

-- Older catalogs can contain case/whitespace URL duplicates. recommended_feeds
-- has no user-owned state; retain the deterministic lowest id and merge useful
-- catalog metadata before removing only its duplicate catalog rows.
WITH ranked AS (
    SELECT id, normalized_url, min(id) OVER (PARTITION BY normalized_url) AS canonical_id
      FROM recommended_feeds
     WHERE normalized_url IS NOT NULL
),
duplicates AS (
    SELECT r.id, r.canonical_id
      FROM ranked r
     WHERE r.id <> r.canonical_id
)
UPDATE recommended_feeds canonical
   SET site_url = COALESCE(canonical.site_url, duplicate.site_url),
       description = COALESCE(canonical.description, duplicate.description),
       first_discovered_at = NULLIF(
           LEAST(
               COALESCE(canonical.first_discovered_at, 'infinity'::timestamp),
               COALESCE(duplicate.first_discovered_at, 'infinity'::timestamp)
           ),
           'infinity'::timestamp
       ),
       last_observed_at = NULLIF(
           GREATEST(
               COALESCE(canonical.last_observed_at, '-infinity'::timestamp),
               COALESCE(duplicate.last_observed_at, '-infinity'::timestamp)
           ),
           '-infinity'::timestamp
       )
  FROM duplicates d
  JOIN recommended_feeds duplicate ON duplicate.id = d.id
 WHERE canonical.id = d.canonical_id;

DELETE FROM recommended_feeds duplicate
 USING (
    SELECT id
      FROM (
          SELECT id, row_number() OVER (PARTITION BY normalized_url ORDER BY id) AS ordinal
            FROM recommended_feeds
           WHERE normalized_url IS NOT NULL
      ) ranked
     WHERE ordinal > 1
 ) duplicate_ids
 WHERE duplicate.id = duplicate_ids.id;

ALTER TABLE recommended_feeds DROP CONSTRAINT IF EXISTS recommended_feeds_validation_status_check;
ALTER TABLE recommended_feeds ADD CONSTRAINT recommended_feeds_validation_status_check
    CHECK (validation_status IN ('pending', 'valid', 'invalid'));
ALTER TABLE recommended_feeds DROP CONSTRAINT IF EXISTS recommended_feeds_health_score_check;
ALTER TABLE recommended_feeds ADD CONSTRAINT recommended_feeds_health_score_check
    CHECK (health_score IS NULL OR health_score BETWEEN 0 AND 1);
CREATE UNIQUE INDEX IF NOT EXISTS idx_recommended_feeds_normalized_url
    ON recommended_feeds (normalized_url);

CREATE TABLE IF NOT EXISTS explore_registry_providers (
    id SERIAL PRIMARY KEY,
    provider_key VARCHAR(100) NOT NULL UNIQUE,
    provider_kind VARCHAR(32) NOT NULL CHECK (provider_kind IN ('opml', 'directory', 'reddit_stream', 'github_awesome', 'related_site')),
    endpoint VARCHAR(2048) NOT NULL,
    topic VARCHAR(100),
    sync_interval_minutes INTEGER NOT NULL DEFAULT 360 CHECK (sync_interval_minutes > 0),
    enabled BOOLEAN NOT NULL DEFAULT true,
    etag VARCHAR(500),
    last_modified VARCHAR(500),
    last_sync_at TIMESTAMP,
    last_success_at TIMESTAMP,
    consecutive_failures INTEGER NOT NULL DEFAULT 0 CHECK (consecutive_failures >= 0),
    last_error TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS explore_source_observations (
    id SERIAL PRIMARY KEY,
    provider_id INTEGER NOT NULL REFERENCES explore_registry_providers(id) ON DELETE CASCADE,
    source_id INTEGER NOT NULL REFERENCES recommended_feeds(id) ON DELETE CASCADE,
    external_key VARCHAR(500) NOT NULL,
    provider_tags TEXT[] NOT NULL DEFAULT '{}',
    first_seen_at TIMESTAMP NOT NULL DEFAULT NOW(),
    last_seen_at TIMESTAMP NOT NULL DEFAULT NOW(),
    occurrence_count INTEGER NOT NULL DEFAULT 1 CHECK (occurrence_count > 0),
    UNIQUE (provider_id, external_key, source_id)
);
CREATE INDEX IF NOT EXISTS idx_explore_source_observations_source
    ON explore_source_observations (source_id, last_seen_at DESC);

CREATE TABLE IF NOT EXISTS explore_fetch_runs (
    id SERIAL PRIMARY KEY,
    window_at TIMESTAMP NOT NULL UNIQUE,
    status VARCHAR(16) NOT NULL DEFAULT 'running' CHECK (status IN ('running', 'done', 'failed')),
    claimed_count INTEGER NOT NULL DEFAULT 0 CHECK (claimed_count >= 0 AND claimed_count <= 500),
    started_at TIMESTAMP,
    completed_at TIMESTAMP,
    worker_id VARCHAR(200),
    error_message TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS explore_fetch_queue (
    id SERIAL PRIMARY KEY,
    source_id INTEGER NOT NULL REFERENCES recommended_feeds(id) ON DELETE CASCADE,
    task_type VARCHAR(32) NOT NULL CHECK (task_type IN ('validate_source', 'refresh_articles')),
    status VARCHAR(16) NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'leased', 'done', 'invalid')),
    priority INTEGER NOT NULL DEFAULT 0,
    not_before TIMESTAMP NOT NULL DEFAULT NOW(),
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    run_id INTEGER REFERENCES explore_fetch_runs(id) ON DELETE SET NULL,
    lease_owner VARCHAR(200),
    lease_expires_at TIMESTAMP,
    last_error TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMP
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_explore_fetch_queue_active_source_task
    ON explore_fetch_queue (source_id, task_type)
    WHERE status IN ('pending', 'leased');
CREATE INDEX IF NOT EXISTS idx_explore_fetch_queue_claimable
    ON explore_fetch_queue (not_before, priority DESC, id)
    WHERE status IN ('pending', 'leased');

CREATE TABLE IF NOT EXISTS explore_articles (
    id SERIAL PRIMARY KEY,
    source_id INTEGER NOT NULL REFERENCES recommended_feeds(id) ON DELETE CASCADE,
    url VARCHAR(2048) NOT NULL,
    normalized_url VARCHAR(2048) NOT NULL,
    title VARCHAR(500) NOT NULL,
    content TEXT,
    excerpt TEXT,
    published_at TIMESTAMP,
    fetched_at TIMESTAMP NOT NULL DEFAULT NOW(),
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    UNIQUE (source_id, normalized_url)
);
CREATE INDEX IF NOT EXISTS idx_explore_articles_source_published_at
    ON explore_articles (source_id, published_at DESC);

CREATE TABLE IF NOT EXISTS explore_batches (
    id SERIAL PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    slot_at TIMESTAMP NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'done', 'failed')),
    source_count INTEGER NOT NULL DEFAULT 0 CHECK (source_count >= 0),
    error_message TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMP,
    CONSTRAINT explore_batches_id_user_id_key UNIQUE (id, user_id),
    UNIQUE (user_id, slot_at)
);
CREATE INDEX IF NOT EXISTS idx_explore_batches_user_created_at
    ON explore_batches (user_id, created_at DESC);

CREATE TABLE IF NOT EXISTS explore_batch_sources (
    id SERIAL PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    batch_id INTEGER NOT NULL,
    source_id INTEGER NOT NULL REFERENCES recommended_feeds(id) ON DELETE CASCADE,
    rank INTEGER NOT NULL CHECK (rank > 0),
    score DOUBLE PRECISION NOT NULL DEFAULT 0,
    topic VARCHAR(100),
    reason TEXT,
    UNIQUE (batch_id, source_id),
    CONSTRAINT explore_batch_sources_batch_id_user_id_fkey
        FOREIGN KEY (batch_id, user_id)
        REFERENCES explore_batches(id, user_id)
        ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_explore_batch_sources_batch_rank
    ON explore_batch_sources (batch_id, rank);

CREATE TABLE IF NOT EXISTS explore_feedback (
    id SERIAL PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    source_id INTEGER REFERENCES recommended_feeds(id) ON DELETE CASCADE,
    topic VARCHAR(100),
    feedback_type VARCHAR(32) NOT NULL CHECK (feedback_type IN ('hide_source', 'dampen_topic', 'boost_topic')),
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    CHECK (
        (feedback_type = 'hide_source' AND source_id IS NOT NULL AND topic IS NULL)
        OR (feedback_type IN ('dampen_topic', 'boost_topic') AND source_id IS NULL AND topic IS NOT NULL)
    )
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_explore_feedback_user_source_type
    ON explore_feedback (user_id, source_id, feedback_type)
    WHERE source_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_explore_feedback_user_topic_type
    ON explore_feedback (user_id, topic, feedback_type)
    WHERE topic IS NOT NULL;

CREATE TABLE IF NOT EXISTS explore_article_events (
    id BIGSERIAL PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    explore_article_id INTEGER NOT NULL REFERENCES explore_articles(id) ON DELETE CASCADE,
    event_type VARCHAR(32) NOT NULL CHECK (event_type IN ('exposure', 'click', 'completed_read')),
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
    USING (app_rls_bypass() OR user_id = app_current_user_id())
    WITH CHECK (app_rls_bypass() OR user_id = app_current_user_id());

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

INSERT INTO explore_registry_providers (provider_key, provider_kind, endpoint, topic, sync_interval_minutes)
VALUES
    ('plenary-programming-opml', 'opml', 'https://raw.githubusercontent.com/spians/awesome-RSS-feeds/master/recommended/with_category/Programming.opml', 'programming', 360),
    ('plenary-tech-opml', 'opml', 'https://raw.githubusercontent.com/spians/awesome-RSS-feeds/master/recommended/with_category/Tech.opml', 'technology', 360),
    ('plenary-webdev-opml', 'opml', 'https://raw.githubusercontent.com/spians/awesome-RSS-feeds/master/recommended/with_category/Web%20Development.opml', 'web-development', 360),
    ('chinese-independent', 'opml', 'https://raw.githubusercontent.com/timqian/chinese-independent-blogs/master/feed.opml', 'chinese-independent', 360),
    ('ooh-recently-added', 'directory', 'https://ooh.directory/feeds/recently-added.xml', 'recently-added', 360),
    ('reddit-programming', 'reddit_stream', '/reddit/subreddit/programming', 'programming', 360),
    ('awesome-selfhosted', 'github_awesome', 'https://raw.githubusercontent.com/awesome-selfhosted/awesome-selfhosted/master/README.md', 'self-hosted', 360)
ON CONFLICT (provider_key) DO UPDATE
    SET provider_kind = EXCLUDED.provider_kind,
        endpoint = EXCLUDED.endpoint,
        topic = EXCLUDED.topic,
        sync_interval_minutes = EXCLUDED.sync_interval_minutes;
