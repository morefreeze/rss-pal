# Clip publication date implementation plan

**Goal:** Preserve verified publication dates throughout web clipping and repair the authorized last-month records.

**Architecture:** Extract metadata before HTML cleanup with one Go parser. Preserve the source metadata in browser captures. Use NULL-only persistence and a scoped, preview-first operator command for historical repair.

**Tech stack:** Go/goquery/PostgreSQL, browser JavaScript, React bookmarklet generator.

## Tasks

- [x] Write failing parser and capture tests: meta published_time, typed JSON-LD, explicit publication time, WeChat date, absence/modified-only, recapture preserving existing dates.
- [x] Add `internal/rss/publication.go`; expose `ContentResult.PublishedAt` and fresh metadata fetching. Wire `api/bookmarklet.go`, `api/content.go` and NULL-only repository update. Run `go test ./internal/rss ./internal/api ./internal/repository` with PostgreSQL enabled.
- [x] Preserve date-bearing metadata in extension/content.js, extension/popup.js, and SettingsPage bookmarklet. Add executable DOM capture regressions and verify existing extension/frontend suites. Review delegated capture task for scope then quality.
- [x] Add `cmd/backfill_clip_dates`: required owner and RFC3339 since, default dry run, bounded fetch, per-record status, atomic guarded article/share fill. Test scope, empty dates, existing timestamps, other owners, older articles, immutable non-date snapshot fields and idempotency.
- [ ] Review the full diff, fix findings, run backend tests, extension checks, frontend build and relevant capture tests. Merge/push actual mainline while preserving unrelated working-tree changes.
- [ ] Verify Tencent deployment revision and health. Back up only selected records, preview then apply for owner 1 since 2026-09-13T16:00:00Z. Re-query dates/snapshot fields and open the original public link. Record unresolved sources precisely.

Execution: delegate the independent browser capture task using subagent-driven-development; keep backend extraction/persistence/backfill together. User approved implementation and last-month repair; no further workflow approval gate.
