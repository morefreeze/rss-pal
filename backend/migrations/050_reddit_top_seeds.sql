-- Reddit RSSHub no longer provides this route. Scored listings have their own
-- kind so one qualifying post supplies evidence without faking repeat sightings.
ALTER TABLE explore_registry_providers
    DROP CONSTRAINT explore_registry_providers_provider_kind_check;
ALTER TABLE explore_registry_providers ADD CONSTRAINT explore_registry_providers_provider_kind_check
    CHECK (provider_kind IN ('opml', 'directory', 'reddit_stream', 'reddit_top', 'github_awesome', 'related_site'));

UPDATE explore_registry_providers SET enabled=false, updated_at=NOW()
WHERE provider_key='reddit-programming' AND provider_kind='reddit_stream';

INSERT INTO explore_registry_providers(provider_key,provider_kind,endpoint,topic,sync_interval_minutes)
SELECT 'reddit-' || lower(subreddit) || '-top-' || period,
       'reddit_top',
       'https://www.reddit.com/r/' || subreddit || '/top.json?t=' || period || '&limit=100&raw_json=1#min_score=100',
       topic,360
FROM (VALUES ('programming','programming'),('MachineLearning','machine-learning'),('LocalLLaMA','artificial-intelligence'),('artificial','artificial-intelligence')) AS boards(subreddit,topic)
CROSS JOIN (VALUES ('week'),('month')) AS windows(period)
ON CONFLICT(provider_key) DO NOTHING;
