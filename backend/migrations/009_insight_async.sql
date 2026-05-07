-- Async insight generation: pending/done/failed lifecycle.
-- Existing rows default to 'done' so they keep counting against quota retroactively.

ALTER TABLE user_insights
  ADD COLUMN IF NOT EXISTS status    VARCHAR(16) NOT NULL DEFAULT 'done'
    CHECK (status IN ('pending','done','failed')),
  ADD COLUMN IF NOT EXISTS error_msg TEXT;

-- DB-enforced "at most one pending row per user" — prevents the click-twice race.
CREATE UNIQUE INDEX IF NOT EXISTS idx_one_pending_per_user
  ON user_insights (user_id) WHERE status = 'pending';

-- Make content nullable so pending rows can exist before AI returns.
ALTER TABLE user_insights ALTER COLUMN content DROP NOT NULL;
