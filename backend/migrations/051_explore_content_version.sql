-- Zero identifies legacy feed bodies, which may still be raw HTML. Keep the
-- marker separate from the body: Markdown code examples can contain HTML too.
ALTER TABLE explore_articles ADD COLUMN IF NOT EXISTS content_version INTEGER NOT NULL DEFAULT 0;
-- Preserve provenance across a fresh fetch racing with the historical repair.
ALTER TABLE explore_articles ADD COLUMN IF NOT EXISTS legacy_content_hash TEXT;
