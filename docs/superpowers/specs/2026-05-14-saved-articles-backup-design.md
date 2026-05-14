# 网摘 (Saved Articles) Backup & Restore — Design

**Date**: 2026-05-14
**Status**: Approved, ready for implementation plan
**Author**: brainstorm session with user

## Problem

The existing backup module (`backend/internal/backup/`) snapshots subscription-side state — feeds, tags, interest signals — but explicitly excludes articles, reasoning that articles can be re-fetched from RSS by the worker.

This reasoning **does not hold** for "网摘" (the user's saved collection), which is defined as:

```
saved_predicate =
  EXISTS (user_preferences.signal_type='save' for this article)
  OR
  (feeds.feed_type='saved' AND article is in that feed)
```

Articles in `feed_type='saved'` are bookmarklet-captured from arbitrary URLs and **cannot be re-fetched** — a wipe + restore loses them permanently. Articles with a `signal_type='save'` preference are RSS-side articles the user explicitly elected to keep; even if the RSS source republishes them, the saved relationship and the content snapshot at save-time are user-meaningful state worth preserving.

Additionally, the current `Restore` already collects `ArticleUserTags` and `UserPreferences` into the snapshot but **skips them on apply** (counted under `SkippedArticleLink`) because article IDs don't survive a wipe. With saved articles now part of the backup, the rows referencing those articles can be restored via an export-ID map.

## Scope

**In scope**
- Backup saved articles (matching the predicate above, excluding link_set children).
- Backup reading_progress rows belonging to those saved articles.
- Restore saved articles into the `articles` table.
- During restore, build `old article_id → new article_id` map; use it to also restore the existing `ArticleUserTags` and `UserPreferences` entries that reference saved articles. Non-saved relations remain skipped (matching current behavior).
- Trigger a debounced backup after a successful bookmarklet capture.

**Out of scope**
- Backing up RSS-side article content (worker re-fetches).
- Backing up link_set children or `link_set_candidates` (re-extractable from parent HTML).
- Backing up `interest_tags` (separate concern; not requested).
- Per-user scoped restore. Like the current implementation, restore is admin-only and global.
- A separate UI / API for "export my 网摘" — restore is the only consumer.

## File Layout

Each backup timestamp produces **two files**, paired by the shared timestamp prefix:

```
${BACKUP_DIR}/
  rss-pal-backup-20260514-093015.json           # ① metadata (unchanged, plain JSON)
  rss-pal-backup-20260514-093015.saved.json.gz  # ② saved archive (new, gzipped JSON)
```

- ① keeps the existing `Snapshot` struct and `SnapshotVersion = 1`. **Zero schema change.**
- ② is a new `SavedSnapshot` with `SavedSnapshotVersion = 1`, gzip-compressed because saved-article content is HTML/markdown (highly compressible) and individual files would otherwise reach 10–25 MB for a moderately heavy user.

Filename convention: the saved file's name is exactly the metadata filename with `.json` replaced by `.saved.json.gz`. This is computed in code, not embedded in either file's payload, so the on-disk pairing is canonical.

### Write order

**Write ② first, then ①.** Rationale:

- `List(dir)` enumerates by ① filename pattern — ① is the "commit pointer."
- If the process crashes between writes, we may leave an orphan ② with no ①. `List()` ignores it (filename pattern doesn't match). Next `Prune` sweeps it.
- We never produce a visible-but-incomplete backup (① present, ② missing in a way the loader can't detect).

Both files use the existing `tmp + rename` atomic-write idiom.

### Read

- `Load(metadataPath)` — unchanged, loads ①.
- `LoadSaved(metadataPath)` — new. Derives sibling path from `metadataPath`, opens it through `gzip.NewReader`, parses. **Sibling missing → returns `(nil, nil)`.** This is the legacy-compat path (old backups have no ②) and also the "saved write succeeded but metadata didn't, so this backup isn't visible anyway" path.

### Prune

When `Prune` removes ①, it also removes the sibling ② (best-effort: log on failure, don't fail the prune).

Before computing the prune set, `Prune` also sweeps orphan `*.saved.json.gz` files (no matching ①). These can result from a crash between writes or from a partially-applied delete.

## SavedSnapshot Schema

New file `backend/internal/backup/snapshot_saved.go`:

```go
const SavedSnapshotVersion = 1

// SavedArticleRow is one saved article in serializable form.
// ExportID is the DB id at backup time. After restore upserts the article,
// we record old→new id mapping so other tables can be reconnected.
type SavedArticleRow struct {
    ExportID             int        `json:"export_id"`     // articles.id at backup time
    FeedURL              string     `json:"feed_url"`      // for resolving feed_id on restore
    Title                string     `json:"title"`
    URL                  string     `json:"url"`
    Content              string     `json:"content"`
    PublishedAt          *time.Time `json:"published_at,omitempty"`
    SummaryBrief         string     `json:"summary_brief,omitempty"`
    SummaryDetailed      string     `json:"summary_detailed,omitempty"`
    FetchedAt            time.Time  `json:"fetched_at"`
    WordCount            int        `json:"word_count"`
    ReadingMinutes       int        `json:"reading_minutes"`
    IsRead               bool       `json:"is_read"`
    EditorNote           string     `json:"editor_note,omitempty"`
    MediaURL             string     `json:"media_url,omitempty"`
    MediaType            string     `json:"media_type,omitempty"`
    MediaDurationSeconds int        `json:"media_duration_seconds,omitempty"`
}

// ReadingProgressRow attaches to a SavedArticleRow via ArticleExportID.
type ReadingProgressRow struct {
    UserID          int       `json:"user_id"`
    ArticleExportID int       `json:"article_export_id"`
    ScrollPosition  float64   `json:"scroll_position"`
    LastReadAt      time.Time `json:"last_read_at"`
    IsCompleted     bool      `json:"is_completed"`
}

type SavedSnapshot struct {
    Version         int                  `json:"version"`
    CreatedAt       time.Time            `json:"created_at"`
    SavedArticles   []SavedArticleRow    `json:"saved_articles"`
    ReadingProgress []ReadingProgressRow `json:"reading_progress"`
}
```

The `CreatedAt` value in ② must equal the `CreatedAt` in ① (same `time.Time` propagated through `Build`). Pairing is by filename; `CreatedAt` equality is a sanity check, not a load gate.

### Saved predicate (DB → SavedSnapshot)

Inside the same read-only transaction `Build` already uses:

```sql
SELECT a.id, f.url AS feed_url, a.title, a.url, a.content,
       a.published_at, a.summary_brief, a.summary_detailed,
       a.fetched_at, a.word_count, a.reading_minutes, a.is_read,
       a.editor_note, a.media_url, a.media_type, a.media_duration_seconds
FROM articles a
JOIN feeds f ON a.feed_id = f.id
WHERE a.parent_article_id IS NULL
  AND (
    EXISTS (
      SELECT 1 FROM user_preferences p
      WHERE p.article_id = a.id AND p.signal_type = 'save'
    )
    OR f.feed_type = 'saved'
  )
ORDER BY a.id
```

`parent_article_id IS NULL` excludes link_set children, which are out of scope.

`ReadingProgress` extraction filters to `article_id IN (saved_set)`:

```sql
SELECT user_id, article_id, scroll_position, last_read_at, is_completed
FROM reading_progress
WHERE article_id = ANY($1)
```

## Build Flow

`Build(ctx, db)` is extended to:

1. Load `Snapshot` fields as today (no behavior change for existing tables).
2. **New:** Run the saved-article query inside the same TX, fill `SavedArticles`.
3. **New:** Run reading_progress filtered by saved-article IDs.
4. Return a `*Snapshot` (metadata, ①) and a `*SavedSnapshot` (saved, ②).

Signature change: `Build` returns `(*Snapshot, *SavedSnapshot, error)`. Existing callers (`Runner.RunNow`) updated.

## Write Flow

New function `WriteFiles(s *Snapshot, ss *SavedSnapshot, dir string) (metadataPath, savedPath string, err error)`:

1. Compute filenames from `s.CreatedAt` (existing logic).
2. Marshal `ss` → JSON, wrap in gzip, atomic write to `savedPath`.
3. Marshal `s` → JSON (existing `WriteFile` logic factored out), atomic write to `metadataPath`.
4. If ② succeeds but ① fails: leave ② on disk as an orphan, return error. Next prune will sweep it.

`WriteFile` (legacy) is retained as a thin wrapper around `WriteFiles` for any test that needs it, or deleted if no callers remain.

## Restore Flow

`Restore(ctx, db, s, ss)` — signature extended to take both snapshots. `ss` may be nil (legacy backup), in which case behavior matches today exactly.

Inside a single TX:

1. Upsert feeds (existing).
2. Upsert user_tags, interest_categories, interest_topics (existing).
3. **New:** For each `SavedArticleRow ar`:
   - Lookup `feed_id` by `feeds.url = ar.FeedURL`. If missing, skip (count under a new stat `SkippedMissingFeed`); this can only happen if `ss` references a feed somehow excluded from `s.Feeds`, which shouldn't occur but we don't trust input.
   - `INSERT INTO articles (feed_id, title, url, content, published_at, summary_brief, summary_detailed, fetched_at, word_count, reading_minutes, is_read, editor_note, media_url, media_type, media_duration_seconds) VALUES (...) ON CONFLICT (feed_id, url) WHERE parent_article_id IS NULL DO NOTHING RETURNING id`.
   - If RETURNING is empty (conflict, row existed), `SELECT id FROM articles WHERE feed_id=$1 AND url=$2 AND parent_article_id IS NULL`.
   - Record `idMap[ar.ExportID] = newID`.
4. **New:** For each `ReadingProgressRow rp`:
   - Translate `rp.ArticleExportID` via `idMap`. If absent, skip.
   - Upsert `INSERT ... ON CONFLICT (user_id, article_id) DO NOTHING`. (DB-wins: a fresher local progress beats the backup.)
5. For each `ArticleUserTag aut` in `s` (existing field, previously skipped):
   - If `aut.ArticleID` ∈ `idMap`: translate and `INSERT ... ON CONFLICT DO NOTHING`. Count under `stats.ArticleUserTags`.
   - Else: count under `stats.SkippedArticleLink` (existing behavior).
6. For each `UserPreference up` in `s`:
   - Same pattern. Count under `stats.UserPreferences` or `stats.SkippedArticleLink`.
7. COMMIT.

`RestoreStats` gains:

```go
SavedArticles      int  // upserted (including no-op on conflict)
ReadingProgress    int
ArticleUserTags    int  // newly restorable thanks to idMap
UserPreferences    int  // newly restorable thanks to idMap
SkippedArticleLink int  // remains: relations whose article_id is not saved
SkippedMissingFeed int  // new: saved rows whose feed couldn't be resolved
```

### Conflict semantics

- **Saved article exists in DB at same `(feed_id, url)`** → `DO NOTHING`. Local content wins because re-captures (bookmarklet, see `bookmarklet.go:177`) explicitly overwrite content with the freshest scrape.
- **ArticleUserTag / UserPreference / ReadingProgress row already exists** → `DO NOTHING`. Local state wins for the same reason.
- **Saved feed (`bookmarklet://user/<id>`) missing or owner changed** → feeds upsert by URL already handles this; the URL itself is the per-user identity, so a target DB with a different `users.id` could orphan saved feeds. Out of scope for this change — single-user personal tool, user IDs are stable in practice.

## Trigger Points

Add `runner.TriggerAsync()` in `bookmarklet.Capture` (success path, both update and create branches in `bookmarklet.go`). The 5-min debounce coalesces bursts.

Wiring: pass the existing `*backup.Runner` to `BookmarkletHandler` via a `WithBackupRunner(r)` setter, mirroring `FeedHandler.WithBackupRunner`. Handler tolerates nil runner (when `BACKUP_DIR` unconfigured).

## Versioning / Backward Compat

- Metadata file format (`Snapshot`, version 1): **unchanged**. Old backups load and restore identically — `LoadSaved` returns nil sibling, restore degrades to current behavior.
- New saved file (`SavedSnapshot`, version 1): the first version. Reader rejects unknown versions with a clear error so a future-version saved file paired with an old build fails loudly rather than partial-restoring.

## Tests

Add `backend/internal/backup/snapshot_saved_test.go`:

1. **Build_SavedArticles**: seed two feeds (`saved`, `rss`), articles in each, signals; verify `SavedSnapshot.SavedArticles` matches the predicate (includes bookmarklet-feed articles, includes RSS articles with `signal_type='save'`, excludes link_set children, excludes RSS articles without save signal).
2. **Build_ReadingProgress**: progress rows on saved articles included, on non-saved articles excluded.
3. **WriteFiles_Atomicity**: simulate failure between ② and ① write; verify orphan ② present, no ①; `List()` does not return the orphan; `Prune` removes it.
4. **LoadSaved_MissingSibling**: legacy-style metadata-only file → `LoadSaved` returns `(nil, nil)`, restore behaves as before.
5. **Restore_IDMapping**: build a snapshot, wipe articles + saved feed + tags, restore, verify (a) saved articles re-inserted with new IDs, (b) ArticleUserTags reattached to new IDs, (c) UserPreferences reattached, (d) ReadingProgress reattached, (e) non-saved RSS article's tags counted under `SkippedArticleLink`.
6. **Restore_Idempotent**: restore the same backup twice; second run is a no-op (counts non-zero, but DB state unchanged).
7. **Restore_ConflictDBWins**: pre-seed an article at `(feed_id, url)` with different content; restore; verify content unchanged.
8. **Prune_RemovesSibling**: prune deletes both metadata and `.saved.json.gz`.
9. **Prune_SweepsOrphans**: orphan `.saved.json.gz` with no matching metadata gets deleted.

Existing `snapshot_test.go` and `retention_test.go` tests must continue to pass unchanged.

## Migration / Rollout

No DB migration required — all needed columns and unique indexes already exist:

- `articles` unique on `(feed_id, url) WHERE parent_article_id IS NULL` (`uniq_articles_feed_url_no_child`, from migration 020).
- `feeds` unique on `url`.
- `article_user_tags` and `user_preferences` unique constraints already in place.

First post-deploy backup will include saved articles. Old backups already on disk continue to restore via the legacy code path (no ② sibling).

## Open Questions

None.

## Non-Goals (Reminders)

- Compressing the metadata file. It stays plain JSON for hand-debuggability; saved file is the only one that grows linearly with user data.
- Streaming I/O. Snapshot/restore are in-memory; even at 50 MB raw / ~5 MB gzipped this is fine for the personal-tool use case.
- Cross-user restore protection. Single-user tool, admin-gated endpoint.
