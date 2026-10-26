BEGIN;
ALTER TABLE users ADD COLUMN IF NOT EXISTS nickname TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN IF NOT EXISTS nickname_setup_required BOOLEAN NOT NULL DEFAULT false;
-- Existing externally registered accounts have generated login usernames.
UPDATE users SET nickname_setup_required = true WHERE password_hash = '!external' AND nickname = '';
COMMIT;
