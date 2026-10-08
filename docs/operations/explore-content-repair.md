# Explore HTML to Markdown repair

`051_explore_content_version.sql` marks existing cache bodies as legacy (0).
New fetches store Markdown with version 1. Subscription promotion converts
legacy cache before copying, but preserves already normalized Markdown exactly.

Deploy the new API and worker before repairing data. The API image includes
`/app/repair_explore_content`; it uses the existing DB environment and internal
maintenance connection. It defaults to a read-only dry-run:

```sh
docker compose exec -T api /app/repair_explore_content
```

For apply, use a **new** backup path on a persistent, private host-mounted
location. The command rejects existing files and creates mode 0600 JSONL:

```sh
docker compose exec -T api /app/repair_explore_content --apply --backup /backups/explore-content-before-20261008.jsonl
```

Verify the mount for the selected path before executing. The command snapshots
complete old cache/imported rows plus intended new content, and fsyncs before
each transaction writes. A backup entry can describe an aborted transaction;
it is a write-ahead recovery record, not proof that a change committed.

Only ordinary articles from the same feed URL and article URL whose body
exactly matches the old cache (or its retained legacy MD5 provenance) are repaired. Independent full-page fetches,
clips, child articles and user-state tables are untouched. This includes raw
imports whose cache was already normalized by a concurrent fetch. Updated
imports have reading metrics recomputed and summaries/image dimensions cleared
for regeneration. Each cache row is locked, and writes additionally compare
old content/version. Errors stop the run without undoing prior successful rows;
rerun with a new backup path to resume safely.

A follow-up dry-run should report `Legacy:0 Changed:0 Articles:0` (Scanned is
the total visited). Verify known article 3013 and its Explore reader, check
heading/image counts, and inspect any remaining HTML outside fenced code.

Recovery: read backup JSONL in reverse order; for each row, first check its ID,
URL, feed/source identity and that current content equals cache `new_content.String`
(or SQL NULL when `Valid` is false), or imported entry `new_content`. Restore only changed fields from the old
row: cache `content/content_version/legacy_content_hash`; imported `content/word_count/reading_minutes/
summary_brief/summary_detailed/image_dimensions`. Never replace entire rows or
user-state data, and never overwrite subsequent content edits. Perform guarded
recovery in transactions; no automatic rollback is attempted by this tool.
