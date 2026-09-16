BEGIN;
SET LOCAL app.bypass_rls = 'true';
CREATE TABLE IF NOT EXISTS operations_collection (id BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK(id), started_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(), dropped BIGINT NOT NULL DEFAULT 0);
INSERT INTO operations_collection(id) VALUES(TRUE) ON CONFLICT DO NOTHING;
CREATE TABLE IF NOT EXISTS operations_events (id BIGSERIAL PRIMARY KEY,at TIMESTAMPTZ NOT NULL,kind VARCHAR(24) NOT NULL,reason VARCHAR(40) NOT NULL,task_type VARCHAR(24) NOT NULL DEFAULT '',user_id INT NOT NULL DEFAULT 0 CHECK(user_id>=0),count INT NOT NULL CHECK(count>0),retry_at TIMESTAMPTZ);
CREATE INDEX IF NOT EXISTS operations_events_time ON operations_events(at DESC,id DESC);
CREATE INDEX IF NOT EXISTS operations_events_kind_time ON operations_events(kind,at DESC);
ALTER TABLE operations_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE operations_events FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS operations_events_admin ON operations_events;
CREATE POLICY operations_events_admin ON operations_events USING(app_rls_bypass()) WITH CHECK(app_rls_bypass());
ALTER TABLE operations_collection ENABLE ROW LEVEL SECURITY;
ALTER TABLE operations_collection FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS operations_collection_admin ON operations_collection;
CREATE POLICY operations_collection_admin ON operations_collection USING(app_rls_bypass()) WITH CHECK(app_rls_bypass());

COMMIT;
