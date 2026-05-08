-- 015_feed_governance.sql
-- Phase 1 feed governance: behavioral events + feed status/weight
-- Idempotent: safe to re-run on fresh or partially-migrated databases.

CREATE TABLE IF NOT EXISTS article_events (
    id           BIGSERIAL PRIMARY KEY,
    user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    article_id   INTEGER NOT NULL REFERENCES articles(id) ON DELETE CASCADE,
    event_type   VARCHAR(32) NOT NULL,
    occurred_at  TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_article_events_user_time ON article_events (user_id, occurred_at DESC);
CREATE INDEX IF NOT EXISTS idx_article_events_article ON article_events (article_id);
CREATE INDEX IF NOT EXISTS idx_article_events_type ON article_events (event_type);

-- Feeds: status state machine + priority weight
ALTER TABLE feeds ADD COLUMN IF NOT EXISTS status VARCHAR(16) NOT NULL DEFAULT 'active';
ALTER TABLE feeds ADD COLUMN IF NOT EXISTS priority_weight DOUBLE PRECISION NOT NULL DEFAULT 1.0;

-- Conservative migration: existing inactive feeds → paused, active → active
UPDATE feeds SET status = 'paused' WHERE is_active = false;
UPDATE feeds SET status = 'active' WHERE is_active = true;
-- Catch-all: ensure no row is left with NULL status (e.g. rows where is_active IS NULL)
UPDATE feeds SET status = 'active' WHERE status IS NULL;

CREATE INDEX IF NOT EXISTS idx_feeds_status ON feeds (status);
