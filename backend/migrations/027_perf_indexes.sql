-- 027_perf_indexes.sql
--
-- Composite indexes for the article list query, which filters by
-- feed_id and sorts by published_at DESC or fetched_at DESC. On a
-- table that grows over time these become necessary to keep list
-- TTFB flat.
--
-- All CREATE INDEX statements use CONCURRENTLY so they do not block
-- writes from the worker. CONCURRENTLY cannot run inside a
-- transaction — psql must be invoked without a wrapping BEGIN/COMMIT.
--
-- For an existing deployment, apply with:
--   docker-compose exec -T postgres \
--     psql -U postgres -d rsspal < backend/migrations/027_perf_indexes.sql

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_articles_feed_published
    ON articles (feed_id, published_at DESC NULLS LAST);

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_articles_feed_fetched
    ON articles (feed_id, fetched_at DESC);

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_articles_published
    ON articles (published_at DESC NULLS LAST);
