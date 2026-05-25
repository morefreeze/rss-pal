-- 028_articles_processing_error.sql
-- PDF clip: store a human-readable error when PDF processing fails so the
-- frontend can surface it on the article card.

ALTER TABLE articles
  ADD COLUMN IF NOT EXISTS processing_error TEXT NOT NULL DEFAULT '';
