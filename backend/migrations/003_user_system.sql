-- Users table
CREATE TABLE IF NOT EXISTS users (
    id SERIAL PRIMARY KEY,
    username VARCHAR(50) UNIQUE NOT NULL,
    password_hash VARCHAR(255) NOT NULL,
    is_admin BOOLEAN DEFAULT false,
    created_at TIMESTAMP DEFAULT NOW()
);

-- Invite codes
CREATE TABLE IF NOT EXISTS invite_codes (
    id SERIAL PRIMARY KEY,
    code VARCHAR(20) UNIQUE NOT NULL,
    created_by INT REFERENCES users(id),
    used_by INT REFERENCES users(id),
    expires_at TIMESTAMP,
    created_at TIMESTAMP DEFAULT NOW()
);

-- Feed ownership: NULL = shared (admin-created), user_id = private
ALTER TABLE feeds ADD COLUMN IF NOT EXISTS owner_id INT REFERENCES users(id);

-- User-scoped preferences
ALTER TABLE user_preferences ADD COLUMN IF NOT EXISTS user_id INT REFERENCES users(id);

-- User-scoped reading progress
ALTER TABLE reading_progress ADD COLUMN IF NOT EXISTS user_id INT REFERENCES users(id);

-- Change reading_progress unique constraint from (article_id) to (article_id, user_id)
ALTER TABLE reading_progress DROP CONSTRAINT IF EXISTS reading_progress_article_id_key;
CREATE UNIQUE INDEX IF NOT EXISTS reading_progress_article_user_unique ON reading_progress(article_id, user_id);

