-- Authentication tables intentionally operate before user RLS is established.
-- They contain only hashed short-lived credentials; app-facing reads are owner constrained.
CREATE TABLE IF NOT EXISTS account_identities (
 provider TEXT NOT NULL CHECK(provider IN ('email','google','apple')),
 subject TEXT NOT NULL,
 user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 email TEXT NOT NULL DEFAULT '',
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 PRIMARY KEY(provider,subject),
 UNIQUE(user_id,provider)
);
CREATE TABLE IF NOT EXISTS account_email_challenges (
 id TEXT PRIMARY KEY,
 email TEXT NOT NULL,
 code_hash TEXT NOT NULL,
 ip_hash TEXT NOT NULL,
 bind_user_id INTEGER NOT NULL DEFAULT 0,
 attempts INTEGER NOT NULL DEFAULT 0,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 expires_at TIMESTAMPTZ NOT NULL DEFAULT now()+interval '10 minutes',
 consumed BOOLEAN NOT NULL DEFAULT false
);
CREATE INDEX IF NOT EXISTS account_email_challenges_created_idx ON account_email_challenges(created_at);
CREATE INDEX IF NOT EXISTS account_email_challenges_email_idx ON account_email_challenges(email,created_at);
CREATE TABLE IF NOT EXISTS account_auth_flows (
 token_hash TEXT PRIMARY KEY,
 kind TEXT NOT NULL,
 browser_hash TEXT NOT NULL,
 payload JSONB NOT NULL,
 expires_at TIMESTAMPTZ NOT NULL DEFAULT now()+interval '10 minutes'
);
CREATE INDEX IF NOT EXISTS account_auth_flows_expiry_idx ON account_auth_flows(expires_at);
