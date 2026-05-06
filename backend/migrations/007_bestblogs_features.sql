-- 007_bestblogs_features.sql

-- Article reading metrics (word count + estimated reading minutes)
ALTER TABLE articles ADD COLUMN IF NOT EXISTS word_count INT DEFAULT 0;
ALTER TABLE articles ADD COLUMN IF NOT EXISTS reading_minutes INT DEFAULT 0;

-- Recommended feeds library (catalog only; subscription state lives in `feeds`)
CREATE TABLE IF NOT EXISTS recommended_feeds (
    id SERIAL PRIMARY KEY,
    url VARCHAR(2048) NOT NULL UNIQUE,
    title VARCHAR(500) NOT NULL,
    description TEXT,
    category VARCHAR(100) NOT NULL,        -- 'ai_eng' | 'cn_tech' | 'enterprise' | 'podcast' | 'youtube'
    language VARCHAR(10) NOT NULL,         -- 'zh' | 'en'
    feed_type VARCHAR(20) DEFAULT 'rss',   -- 'rss' | 'html' | 'youtube' | 'podcast'
    is_broken BOOLEAN DEFAULT false,       -- true if seed-time probe failed; UI shows ⚠ badge
    sort_order INT DEFAULT 0,
    created_at TIMESTAMP DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_recommended_feeds_category ON recommended_feeds(category, sort_order);

-- Weekly digest AI intro cache
CREATE TABLE IF NOT EXISTS weekly_digests (
    id SERIAL PRIMARY KEY,
    user_id INT REFERENCES users(id) ON DELETE CASCADE,
    week_start DATE NOT NULL,              -- Monday in Asia/Shanghai
    intro_text TEXT NOT NULL,
    article_ids INTEGER[] NOT NULL,        -- snapshot at generation time
    generated_at TIMESTAMP DEFAULT NOW(),
    UNIQUE(user_id, week_start)
);
