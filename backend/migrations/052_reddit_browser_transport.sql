-- Browser sessions collect these listings; server workers still validate RSS.
ALTER TABLE explore_registry_providers ADD COLUMN IF NOT EXISTS browser_only BOOLEAN NOT NULL DEFAULT false;
UPDATE explore_registry_providers SET browser_only=true
WHERE provider_kind='reddit_top' AND provider_key IN (
 'reddit-programming-top-week','reddit-programming-top-month',
 'reddit-machinelearning-top-week','reddit-machinelearning-top-month',
 'reddit-localllama-top-week','reddit-localllama-top-month',
 'reddit-artificial-top-week','reddit-artificial-top-month');
