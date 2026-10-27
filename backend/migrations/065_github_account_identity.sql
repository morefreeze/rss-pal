-- Production replays this migration chain. Never narrow a constraint that a
-- newer migration has already widened, or existing newer identities would fail.
BEGIN;
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = 'account_identities'::regclass
          AND conname = 'account_identities_provider_check'
          AND position('''github''' in pg_get_constraintdef(oid)) > 0
    ) THEN
        ALTER TABLE account_identities DROP CONSTRAINT IF EXISTS account_identities_provider_check;
        ALTER TABLE account_identities ADD CONSTRAINT account_identities_provider_check
            CHECK (provider IN ('email','google','apple','github'));
    END IF;
END $$;
COMMIT;
