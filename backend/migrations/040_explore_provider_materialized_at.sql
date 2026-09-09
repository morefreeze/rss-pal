-- Track the exact provider generation represented by current observations.
-- Older workers advanced last_success_at on 304 without advancing observations,
-- so bootstrap from the latest persisted observation rather than provider state.
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
  AND provider.last_materialized_at IS NULL;
