-- 031_daily_briefing.sql
-- Daily briefing cache (mirrors 007_bestblogs_features.sql weekly_digests).
CREATE TABLE IF NOT EXISTS daily_digests (
    id SERIAL PRIMARY KEY,
    user_id INT REFERENCES users(id) ON DELETE CASCADE,
    day_start DATE NOT NULL,
    intro_text TEXT NOT NULL,
    article_ids INTEGER[] NOT NULL,
    generated_at TIMESTAMP DEFAULT NOW(),
    UNIQUE(user_id, day_start)
);
CREATE INDEX IF NOT EXISTS idx_daily_digests_user_day
    ON daily_digests(user_id, day_start DESC);

ALTER TABLE users
    ADD COLUMN IF NOT EXISTS briefing_last_tab VARCHAR(10) DEFAULT 'daily';
