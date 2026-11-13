-- The previous worker kept successfully fetched HTML children in processing
-- while waiting for a summary. Their stored content is already complete.
UPDATE articles
SET processing_state = 'ready', processing_error = ''
WHERE parent_article_id IS NOT NULL
  AND processing_state = 'processing'
  AND COALESCE(content, '') <> '';
