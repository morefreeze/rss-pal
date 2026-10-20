-- Authentication metadata is deliberately outside RLS, like users and refresh
-- tokens: a digest must resolve its owner before PublicTokenMiddleware can set
-- app.user_id. All account management queries also constrain user_id explicitly.
CREATE TABLE IF NOT EXISTS shortcut_pairings (
 user_id INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
 code_hash TEXT UNIQUE NOT NULL CHECK(length(code_hash)=64),
 expires_at TIMESTAMPTZ NOT NULL
);
CREATE TABLE IF NOT EXISTS shortcut_connections (
 token_hash TEXT PRIMARY KEY CHECK(length(token_hash)=64),
 user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS shortcut_connections_user ON shortcut_connections(user_id);
