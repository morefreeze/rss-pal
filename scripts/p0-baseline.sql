-- Read-only production baseline; timestamps are UTC, RSS success includes 304.
BEGIN READ ONLY;
SELECT NOW() AS observed_at;
SELECT feed_type, status, COUNT(*) AS feeds FROM feeds GROUP BY 1,2 ORDER BY 1,2;
SELECT id, title, last_fetched_at AS last_successful_check, last_attempt_at,
       next_attempt_at, fetch_failures, fetch_error
FROM feeds WHERE status='active' AND feed_type IN ('rss','html','youtube','podcast')
ORDER BY fetch_failures DESC, last_fetched_at NULLS FIRST;
SELECT owner_id,bucket,used FROM task_budget_daily
WHERE day=(NOW() AT TIME ZONE 'UTC')::date AND bucket IN ('subscription_fetch','background_fetch')
ORDER BY 1,2;
SELECT task_type,reason,SUM(count) AS events FROM operations_events
WHERE at>NOW()-interval '2 hours' GROUP BY 1,2 ORDER BY events DESC;
SELECT COUNT(*) AS subscription_articles_24h,
       COUNT(*) FILTER(WHERE length(a.content)<300) AS short_candidates,
       COUNT(*) FILTER(WHERE COALESCE(a.summary_brief,'')='') AS missing_summary
FROM articles a JOIN feeds f ON f.id=a.feed_id
WHERE f.feed_type IN ('rss','html','youtube','podcast') AND NOT a.is_clip
AND a.fetched_at>(NOW() AT TIME ZONE 'UTC')-interval '24 hours';
SELECT feed_id,url,COUNT(*) FROM articles GROUP BY 1,2 HAVING COUNT(*)>1;
COMMIT;
