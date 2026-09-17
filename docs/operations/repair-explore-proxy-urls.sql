BEGIN;
SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='30s';
CREATE TEMP TABLE affected_proxy_sources ON COMMIT DROP AS
SELECT f.id FROM recommended_feeds f
WHERE (f.url ~ '^https?://([0-9]+\.){3}[0-9]+(:[0-9]+)?/' OR f.url ~ '^https?://\[')
AND EXISTS(SELECT 1 FROM explore_source_observations o WHERE o.source_id=f.id
 AND o.external_key !~ '^(https?://)?([0-9]+\.){3}[0-9]+' AND o.external_key !~ '^https?://\[');
CREATE TEMP TABLE original_proxy_aliases ON COMMIT DROP AS
SELECT f.id, EXISTS(SELECT 1 FROM explore_batch_sources s WHERE s.source_id=f.merged_into_source_id
 AND s.batch_id=(SELECT id FROM explore_batches WHERE user_id=1 AND status='done' ORDER BY slot_at DESC LIMIT 1)) AS previously_recommended
FROM recommended_feeds f WHERE f.merged_into_source_id IN(SELECT id FROM affected_proxy_sources)
AND f.url ~ '^https?://' AND f.url !~ '^https?://([0-9]+\.){3}[0-9]+' AND f.url !~ '^https?://\[';
UPDATE recommended_feeds SET validation_status='invalid',is_broken=true,health_score=0,
 last_error='Legacy proxy connection IP persisted as source URL; original domain must be revalidated'
WHERE id IN(SELECT id FROM affected_proxy_sources);
UPDATE recommended_feeds SET merged_into_source_id=NULL,validation_status='pending',is_broken=false,health_score=NULL,
 last_fetched_at=NULL,last_checked_at=NULL,verified_at=NULL,last_error=NULL
WHERE id IN(SELECT id FROM original_proxy_aliases);
UPDATE explore_fetch_queue SET status='invalid',completed_at=CURRENT_TIMESTAMP,updated_at=CURRENT_TIMESTAMP,
 last_error='Superseded by original-domain validation after proxy URL repair'
WHERE status='pending' AND source_id IN(SELECT id FROM affected_proxy_sources UNION SELECT id FROM original_proxy_aliases);
INSERT INTO explore_fetch_queue(source_id,task_type,priority)
SELECT id,'validate_source',1000 FROM original_proxy_aliases ORDER BY previously_recommended DESC,id LIMIT 60
ON CONFLICT (source_id,task_type) WHERE status IN ('pending','leased') DO UPDATE SET priority=GREATEST(explore_fetch_queue.priority,EXCLUDED.priority);
-- Require a real new directory response, without pretending old cache was freshly fetched.
UPDATE explore_registry_providers SET etag=NULL,last_modified=NULL,last_sync_at=NULL
WHERE enabled AND provider_kind IN('opml','directory','github_awesome');
SELECT (SELECT count(*) FROM affected_proxy_sources) AS quarantined_ip_sources,
 (SELECT count(*) FROM original_proxy_aliases) AS restored_original_domain_candidates;
COMMIT;
