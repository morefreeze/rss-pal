CREATE TABLE IF NOT EXISTS service_heartbeats (
    component TEXT PRIMARY KEY,
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'rsspal_app') THEN
        GRANT SELECT, INSERT, UPDATE ON service_heartbeats TO rsspal_app;
    END IF;
END
$$;
