-- Add per-user long-lived token used to authenticate the browser bookmarklet
-- against POST /api/bookmarklet/capture. Nullable so existing users keep working;
-- token is generated lazily when the user first visits the Settings page.
ALTER TABLE users ADD COLUMN IF NOT EXISTS bookmarklet_token VARCHAR(64);

CREATE UNIQUE INDEX IF NOT EXISTS idx_users_bookmarklet_token
    ON users(bookmarklet_token)
    WHERE bookmarklet_token IS NOT NULL;
