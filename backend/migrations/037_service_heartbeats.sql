CREATE TABLE IF NOT EXISTS service_heartbeats (
    component TEXT PRIMARY KEY,
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

GRANT SELECT, INSERT, UPDATE ON service_heartbeats TO rsspal_app;
