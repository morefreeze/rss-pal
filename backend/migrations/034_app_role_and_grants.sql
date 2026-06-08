-- backend/migrations/034_app_role_and_grants.sql
-- Non-superuser, no-bypass role so RLS policies from migration 033 actually
-- enforce. The .env switch (DB_USER → rsspal_app) is in Task 6.1.
-- Idempotent.

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'rsspal_app') THEN
        CREATE ROLE rsspal_app
            WITH LOGIN
                 NOSUPERUSER
                 NOBYPASSRLS
                 NOCREATEROLE
                 NOCREATEDB
                 NOREPLICATION
                 PASSWORD 'rsspal_app_placeholder_change_me';
    END IF;
END
$$;

DO $$
BEGIN
    EXECUTE format('GRANT CONNECT ON DATABASE %I TO rsspal_app', current_database());
END
$$;

-- current_schema() makes this work for both production `public` and the
-- per-test `test_<name>` schemas the testdb fixture creates.
DO $$
DECLARE
    s text := current_schema();
BEGIN
    EXECUTE format('GRANT USAGE ON SCHEMA %I TO rsspal_app', s);
    EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA %I TO rsspal_app', s);
    EXECUTE format('GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA %I TO rsspal_app', s);
    EXECUTE format('ALTER DEFAULT PRIVILEGES IN SCHEMA %I GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO rsspal_app', s);
    EXECUTE format('ALTER DEFAULT PRIVILEGES IN SCHEMA %I GRANT USAGE, SELECT ON SEQUENCES TO rsspal_app', s);
END
$$;

GRANT EXECUTE ON FUNCTION app_current_user_id() TO rsspal_app;
GRANT EXECUTE ON FUNCTION app_rls_bypass() TO rsspal_app;
