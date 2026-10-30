BEGIN;
-- Keep prompt history separate from nickname_setup_required: migration 066
-- reclassifies external accounts whenever the deployment migrations replay.
ALTER TABLE users ADD COLUMN IF NOT EXISTS nickname_prompted_at TIMESTAMPTZ;
COMMIT;
