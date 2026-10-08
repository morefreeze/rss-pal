# Explore Markdown Implementation Plan

**Goal:** Convert new and cached Explore HTML to readable Markdown and repair exact imported copies.
**Architecture:** Shared RSS normalizer; versioned Explore storage; guarded, backed-up one-shot repair.
**Tech Stack:** Go 1.25, existing html-to-markdown converter, PostgreSQL, ReactMarkdown.

- [ ] Add failing regressions to RSS normalization, feed ingestion and subscription promotion. Run with Go 1.25 and TEST_DB_URL on isolated port 55438.
- [ ] Implement `rss.NormalizeFeedContent`, normalize feed output, add `content_version` migration/model/storage and import fallback. Preserve content/version on stale and null upserts.
- [ ] Add and run failing database repair tests, then implement `internal/explore.RepairContent` and `cmd/repair_explore_content` with dry-run, exclusive backup, per-row transactions, compare-before-write and exact-copy matching.
- [ ] Exercise full article 3013 fixture locally, checking headings, image/link counts, ruby text and no literal layout tags. Run affected package tests against PostgreSQL and build server/worker/repair binary.
- [ ] Review diff with code-reviewer, address findings and re-run relevant checks. Integrate latest origin/master and push. Verify Deploy Tencent SHA and health.
- [ ] Dry-run repair on Tencent, inspect results, apply with backup, repeat dry-run for zero legacy rows and verify article 3013 plus Explore reader. Report counts, SHA and limitations.
