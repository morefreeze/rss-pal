-- backend/migrations/008_insights.sql
-- Adds the data backbone for the insights feature: per-article classification cache,
-- per-user fine-grained tag interests, and persisted insight generations.

ALTER TABLE articles
  ADD COLUMN IF NOT EXISTS topic TEXT,
  ADD COLUMN IF NOT EXISTS tags  TEXT[];

CREATE INDEX IF NOT EXISTS idx_articles_no_topic
  ON articles (id) WHERE topic IS NULL;

CREATE TABLE IF NOT EXISTS interest_tags (
  id                 SERIAL PRIMARY KEY,
  user_id            INT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  tag                TEXT NOT NULL,
  weight             FLOAT NOT NULL DEFAULT 0,
  last_reinforced_at TIMESTAMP NOT NULL DEFAULT NOW(),
  UNIQUE (user_id, tag)
);

CREATE INDEX IF NOT EXISTS idx_interest_tags_user_weight
  ON interest_tags (user_id, weight DESC);

CREATE TABLE IF NOT EXISTS user_insights (
  id           SERIAL PRIMARY KEY,
  user_id      INT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  content      TEXT NOT NULL,
  triggered_by VARCHAR(16) NOT NULL CHECK (triggered_by IN ('auto','manual')),
  model        VARCHAR(64),
  generated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_user_insights_user_latest
  ON user_insights (user_id, generated_at DESC);

CREATE INDEX IF NOT EXISTS idx_user_insights_quota
  ON user_insights (user_id, triggered_by, generated_at);
