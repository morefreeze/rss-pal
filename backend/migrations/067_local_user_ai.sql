-- User-supplied model credentials now live in the browser. Keep the legacy
-- table for upgrade/restore compatibility, but never allow an active key again.
BEGIN;
UPDATE user_ai_configs SET api_key = '', updated_at = NOW() WHERE api_key <> '';
DO $$ BEGIN
 IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname='user_ai_configs_no_server_key' AND conrelid='user_ai_configs'::regclass) THEN
  ALTER TABLE user_ai_configs ADD CONSTRAINT user_ai_configs_no_server_key CHECK (api_key = '');
 END IF;
END $$;
COMMIT;
