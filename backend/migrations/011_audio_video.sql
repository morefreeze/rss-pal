-- 011_audio_video.sql
-- Adds podcast/video media metadata to articles and playback progress
-- tracking. Idempotent (uses IF NOT EXISTS).

ALTER TABLE articles
    ADD COLUMN IF NOT EXISTS media_url VARCHAR(2048),
    ADD COLUMN IF NOT EXISTS media_type VARCHAR(64),
    ADD COLUMN IF NOT EXISTS media_duration_seconds INT;

CREATE TABLE IF NOT EXISTS playback_progress (
    id SERIAL PRIMARY KEY,
    user_id INT REFERENCES users(id) ON DELETE CASCADE,
    article_id INT REFERENCES articles(id) ON DELETE CASCADE,
    position_seconds INT DEFAULT 0,
    last_played_at TIMESTAMP DEFAULT NOW(),
    is_completed BOOLEAN DEFAULT false,
    UNIQUE(user_id, article_id)
);

CREATE INDEX IF NOT EXISTS idx_playback_progress_user ON playback_progress(user_id);
