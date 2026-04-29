-- Add user_id to interest_topics for per-user interest tracking
ALTER TABLE interest_topics ADD COLUMN IF NOT EXISTS user_id INT REFERENCES users(id) ON DELETE CASCADE;

-- Drop the old global unique constraint on topic
ALTER TABLE interest_topics DROP CONSTRAINT IF EXISTS interest_topics_topic_key;

-- Add new unique constraint per user
ALTER TABLE interest_topics ADD CONSTRAINT interest_topics_user_topic_key UNIQUE (user_id, topic);
