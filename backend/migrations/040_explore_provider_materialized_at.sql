-- Track the exact provider generation represented by current observations.
ALTER TABLE explore_registry_providers
    ADD COLUMN IF NOT EXISTS last_materialized_at TIMESTAMP;

WITH latest_generation AS (
    SELECT provider_id, MAX(last_seen_at) AS observed_at
    FROM explore_source_observations
    GROUP BY provider_id
)
UPDATE explore_registry_providers AS provider
SET last_materialized_at = latest_generation.observed_at
FROM latest_generation
WHERE provider.id = latest_generation.provider_id
  AND provider.last_materialized_at IS NULL
  AND provider.last_success_at = latest_generation.observed_at;

-- Older workers advanced last_success_at on 304 without advancing observations.
-- A mismatch is indistinguishable from a genuinely empty final 200 response, so
-- fail closed: discard conditional validators and make the provider immediately
-- due. The next request must materialize a full 200 before any 304 can renew it.
UPDATE explore_registry_providers
SET etag = NULL,
    last_modified = NULL,
    last_sync_at = NULL
WHERE last_materialized_at IS NULL
  AND last_success_at IS NOT NULL;
