ALTER TABLE recommended_feeds
 ADD COLUMN fetch_state TEXT NOT NULL DEFAULT 'active' CHECK (fetch_state IN ('active','retry_wait','retry_exhausted','unavailable','ineligible')),
 ADD COLUMN fetch_failures INTEGER NOT NULL DEFAULT 0 CHECK (fetch_failures >= 0),
 ADD COLUMN next_retry_at TIMESTAMP;

-- Preserve historical task attempts; seed a single source-wide budget.
UPDATE recommended_feeds s SET fetch_failures=COALESCE((SELECT max(q.attempts) FROM explore_fetch_queue q WHERE q.source_id=s.id AND q.status IN ('pending','leased')),0);
UPDATE recommended_feeds SET fetch_state=CASE
 WHEN COALESCE(last_error,'') ~* 'insufficient public observation|no (recent article|article published|parseable articles)|at least two parseable|no supported feed alternate|no discovered alternate|Failed to detect feed type|document is not a feed' THEN 'ineligible'
 WHEN merged_into_source_id IS NOT NULL OR COALESCE(last_error,'') ~* 'HTTP status 410|blocked address|blocked port|invalid port|credentials are not allowed|unsupported scheme|unsafe .*URL|missing host|empty url|redirect rejected|response exceeds configured limit|Superseded by original-domain' THEN 'unavailable'
 WHEN fetch_failures >= 7 THEN 'retry_exhausted'
 ELSE 'retry_wait' END
WHERE validation_status='invalid' OR is_broken OR COALESCE(last_error,'')<>'' OR fetch_failures>0;
UPDATE recommended_feeds SET fetch_failures=GREATEST(fetch_failures,1),
 next_retry_at=CURRENT_TIMESTAMP + ((ARRAY[3600,14400,57600,172800,691200,2764800])[LEAST(GREATEST(fetch_failures,1),6)] * (0.9+random()*0.2))*INTERVAL '1 second'
WHERE fetch_state='retry_wait';
UPDATE recommended_feeds SET is_broken=true,health_score=0 WHERE fetch_state IN ('retry_exhausted','unavailable','ineligible');
UPDATE explore_fetch_queue q SET status='invalid',completed_at=CURRENT_TIMESTAMP,
 lease_owner=NULL,lease_token=NULL,lease_expires_at=NULL,
 last_error=COALESCE(NULLIF(q.last_error,''),s.last_error,'source stopped by failure policy'),updated_at=CURRENT_TIMESTAMP
FROM recommended_feeds s WHERE s.id=q.source_id AND s.fetch_state IN ('retry_exhausted','unavailable','ineligible') AND q.status='pending';
UPDATE explore_fetch_queue q SET not_before=GREATEST(q.not_before,s.next_retry_at)
FROM recommended_feeds s WHERE s.id=q.source_id AND s.fetch_state='retry_wait' AND q.status='pending';
CREATE INDEX recommended_feeds_retry_due ON recommended_feeds(next_retry_at) WHERE fetch_state='retry_wait';
