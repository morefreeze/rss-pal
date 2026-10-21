-- Successful checks and failed attempts have different meanings.
ALTER TABLE feeds ADD COLUMN IF NOT EXISTS last_attempt_at TIMESTAMPTZ;
ALTER TABLE feeds ADD COLUMN IF NOT EXISTS next_attempt_at TIMESTAMPTZ;
ALTER TABLE feeds ADD COLUMN IF NOT EXISTS fetch_error TEXT NOT NULL DEFAULT '';
ALTER TABLE feeds ADD COLUMN IF NOT EXISTS fetch_failures INTEGER NOT NULL DEFAULT 0;
