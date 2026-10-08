# Explore article Markdown repair

User approved fixing new and existing Explore articles, including article 3013. Normalize feed HTML at ingestion using the existing RSS converter. Keep plain text and Markdown unchanged when no actual HTML is present; resolve relative links/images against the article URL and preserve code, tables, images, ruby text and quotations. Do not render arbitrary raw HTML in the reader.

Persist content_version (0 legacy, 1 normalized) so imports and historical repair do not reconvert Markdown. The catalog upsert must preserve the version with the content when an older or null update arrives. Subscription promotion converts only legacy cache entries.

A one-shot command scans legacy entries in bounded batches, defaults to dry-run and requires a new mode-0600 JSONL backup file for apply. Each entry is rechecked in a transaction. Before writes, back up full old rows plus new content and fsync. Repair the cache and only ordinary articles whose URL and content still exactly match it. Recompute article reading metrics and clear content-derived summaries/image dimensions. Preserve user state and differently fetched articles. Roll back on errors; reruns skip version 1 entries. Deploy before applying so fresh fetches cannot reintroduce raw HTML.

Validate through feed parsing, real PostgreSQL promotion/upsert/repair regressions, concurrency and repeat-run checks, then review, integrate origin/master, push and verify Tencent deployment and real UI content. No unrelated workspace changes or service restarts.
