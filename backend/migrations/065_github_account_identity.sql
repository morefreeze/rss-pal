BEGIN;
ALTER TABLE account_identities DROP CONSTRAINT account_identities_provider_check;
ALTER TABLE account_identities ADD CONSTRAINT account_identities_provider_check
 CHECK (provider IN ('email','google','apple','github'));
COMMIT;
