-- 012_completed_listen_unique.sql
-- Make the completed_listen signal at-most-one per (user, article). Defends
-- against a TOCTOU race in PlaybackProgressRepository.Upsert where concurrent
-- PUT /playback requests with is_completed=true could both observe
-- wasCompleted=false and both write the signal — doubling the recommendation
-- boost for one podcast.

CREATE UNIQUE INDEX IF NOT EXISTS user_preferences_completed_listen_unique
    ON user_preferences (user_id, article_id)
    WHERE signal_type = 'completed_listen';
