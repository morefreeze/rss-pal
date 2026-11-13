BEGIN;

SET LOCAL app.bypass_rls = 'true';

CREATE TABLE IF NOT EXISTS site_traffic_collection (
    id BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (id),
    started_at TIMESTAMPTZ NOT NULL DEFAULT statement_timestamp()
);
INSERT INTO site_traffic_collection (id) VALUES (TRUE) ON CONFLICT DO NOTHING;

CREATE TABLE IF NOT EXISTS site_page_views (
    event_id UUID PRIMARY KEY,
    visitor_id UUID NOT NULL,
    at TIMESTAMPTZ NOT NULL DEFAULT statement_timestamp(),
    source TEXT NOT NULL CHECK (source IN ('direct', 'internal', 'external', 'campaign'))
);
CREATE INDEX IF NOT EXISTS site_page_views_at ON site_page_views (at);

ALTER TABLE site_traffic_collection ENABLE ROW LEVEL SECURITY;
ALTER TABLE site_traffic_collection FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS site_traffic_collection_admin ON site_traffic_collection;
CREATE POLICY site_traffic_collection_admin ON site_traffic_collection
    USING (app_rls_bypass()) WITH CHECK (app_rls_bypass());

ALTER TABLE site_page_views ENABLE ROW LEVEL SECURITY;
ALTER TABLE site_page_views FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS site_page_views_admin ON site_page_views;
CREATE POLICY site_page_views_admin ON site_page_views
    USING (app_rls_bypass()) WITH CHECK (app_rls_bypass());

COMMIT;
