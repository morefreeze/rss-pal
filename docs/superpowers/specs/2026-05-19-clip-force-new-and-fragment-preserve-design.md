# Clip force-new + fragment-preserving URL normalization

Date: 2026-05-19

## Motivation

When the user clips a page through the browser extension and the URL is already in their clip bin, the backend returns `status: "duplicate"` and the popup offers a binary 覆盖 / 取消 prompt. Two related problems:

1. **No escape hatch for true positives.** If the user knows the new capture is a genuinely different article that happens to share the URL, their only options are overwrite (destroys the previous) or cancel (loses the new one). They want a 新建 button that stores both.

2. **False-positive duplicates on hash-route SPAs.** `util.NormalizeURL` strips the URL fragment before dedup. That's correct for `#section1` anchors, but wrong for hash-route apps like Gmail, where `https://mail.google.com/.../#inbox/abc` and `.../#inbox/def` are different emails. Every Gmail clip after the first triggers the duplicate prompt with confusing length/image numbers that compare unrelated emails.

Fix both together: preserve fragments for the clip path so hash-route URLs naturally don't collide (treats the bug at its root), and add a 新建 button for the residual cases where same URL + same fragment really do back different content.

## Scope

In:
- Extension popup gains a 新建 button in the duplicate prompt; clicking it POSTs `force_new: true`.
- `POST /api/bookmarklet/capture` honors `force_new`: skip the dedup branch entirely, create a new article unconditionally.
- The DB unique index `uniq_articles_feed_url_no_child` is narrowed to exclude clip articles, via a new `articles.is_clip` boolean column.
- `bookmarklet.Capture` uses a new `util.NormalizeURLKeepFragment` helper that preserves `Fragment` / `RawFragment` while still stripping tracking params, lowercasing host, normalizing trailing slash.
- `FindByOwnerAndURL` adds explicit `ORDER BY a.fetched_at DESC LIMIT 1` so the dedup prompt always compares against the most recent capture when multiple rows share a URL.
- Extension `manifest.json` version bumped (per project memory).

Out:
- RSS and HTML scrape paths continue using the global `NormalizeURL` (fragment-stripped). No change.
- Article rendering, sharing, summary generation: no change. Articles with fragments work the same as articles without.
- No heuristic to auto-detect "hash-route" vs "anchor-fragment" — we just always preserve the fragment on the clip path.
- The dedup prompt's content-length / image-count heuristics stay unchanged. The 新建 button is independent of them.

## Design

### Part B — Preserve fragment on clip path

**`backend/internal/util/urlnorm.go`** — new sibling helper:

```go
// NormalizeURLKeepFragment is like NormalizeURL but keeps the URL fragment
// intact. Used by the clip-capture path where hash-route SPAs (Gmail,
// Bilibili, ...) encode the real page identity after `#`. Stripping the
// fragment there causes false-positive duplicates across distinct emails /
// videos that share a stable base URL.
func NormalizeURLKeepFragment(raw string) string { ... }
```

Implementation: factor the common normalization into an internal helper that takes a `stripFragment bool` flag (or restructure the existing function). The two public functions both call it. Either approach is fine; the simplest is to copy the function body and remove the two fragment-clearing lines — less code-churn, no risk of regressing existing call sites.

**`backend/internal/api/bookmarklet.go`** — replace the single `util.NormalizeURL(req.URL)` call (currently line 124) with `util.NormalizeURLKeepFragment(req.URL)`. No other call site changes.

**Side effect to accept:** Two clips of `https://example.com/article#section1` and `#section2` now become two distinct DB rows instead of being deduped. Users can manually delete extras. The reverse failure (Gmail-style false-positive dedup) is the bigger pain and gets fixed.

**Tests** in `backend/internal/util/urlnorm_test.go` — add cases:
- `NormalizeURLKeepFragment("https://example.com/a#sec")` returns `https://example.com/a#sec`
- `NormalizeURLKeepFragment("https://mail.google.com/mail/u/0/#inbox/abc")` returns `https://mail.google.com/mail/u/0/#inbox/abc`
- `NormalizeURLKeepFragment` still strips utm_/gclid/fbclid params and lowercases the host (parity check with `NormalizeURL` minus the fragment behavior).

### Part A — `force_new` flow + DB constraint relaxation

**Migration** — `backend/migrations/025_articles_is_clip.sql`:

```sql
-- 025_articles_is_clip.sql
-- Add is_clip boolean to articles so the dedup unique index can exclude
-- clip-bin captures. Clip captures need to allow multiple rows with the
-- same (feed_id, url) — the user explicitly clicked 新建 to keep both.

ALTER TABLE articles
  ADD COLUMN IF NOT EXISTS is_clip BOOLEAN NOT NULL DEFAULT false;

UPDATE articles SET is_clip = true
  WHERE feed_id IN (SELECT id FROM feeds WHERE feed_type = 'clip');

DROP INDEX IF EXISTS uniq_articles_feed_url_no_child;
CREATE UNIQUE INDEX uniq_articles_feed_url_no_child
  ON articles(feed_id, url)
  WHERE parent_article_id IS NULL AND NOT is_clip;
```

Per project convention, the auto-init only runs on a fresh volume — operator must `psql -f` this on the existing dev DB after deploy. The implementation plan will spell this out as a non-skippable step bracketed by backup + verification (matching the T7 dance from the previous spec).

**`model.Article`** — add `IsClip bool`. JSON tag if exposed (probably not needed — internal denormalization). All existing scanner code that fetches articles by column list does NOT need to read this; only the `Create` SQL needs to write it.

**`ArticleRepository.Create`** — extend the INSERT to include `is_clip`. Most callers default it to false; the clip-bin path sets it true.

**`FindByOwnerAndURL`** — change the trailing `LIMIT 1` to `ORDER BY a.fetched_at DESC LIMIT 1`. When multiple clip articles share a URL, the dedup prompt compares against the most recent. Behavior is identical for the dominant single-row case.

**`bookmarklet.Capture`** — accept `force_new` in the request body:

```go
var req struct {
    URL      string `json:"url"`
    Title    string `json:"title"`
    HTML     string `json:"html"`
    Force    bool   `json:"force"`
    ForceNew bool   `json:"force_new"`
}
```

Flow change: if `req.ForceNew == true`, skip the entire `existing != nil` branch (no dedup check, no UpdateContent path) and fall through to the Create path. The Create path sets `article.IsClip = true` for clip-bin inserts (it does this unconditionally for the bookmarklet handler — every article it creates is a clip).

`Force` and `ForceNew` are independent. `Force=true` keeps existing semantics (bypass prompt but UPDATE in place). `ForceNew=true` is the new "always insert" mode. If both are sent, `ForceNew` wins (gate on `ForceNew` first); the extension never sends both at once.

### Part A — extension UI

**`extension/popup.html`** — current duplicate prompt has 覆盖 / 取消. Insert a 新建 button between them so the order reads 覆盖 / 新建 / 取消:

```html
<div class="duplicate-actions">
  <button class="btn btn-danger" id="overwriteBtn">覆盖</button>
  <button class="btn btn-primary" id="newBtn">新建</button>
  <button class="btn btn-secondary" id="cancelBtn">取消</button>
</div>
```

**`extension/popup.js`**:
- `sendToServer` signature stays compatible; add an optional 7th parameter `forceNew = false` that sets `force_new` in the body. Calls pass either `force` OR `force_new` (not both).
- Bind a click handler on `newBtn` that reuses `lastCapture` and calls `sendToServer(..., false /*force*/, true /*forceNew*/)`. On success, show the same "✅ 已加入网摘" + article link the regular create path shows.
- Update `hideDuplicate` / `setLoading` references to cover `newBtn` as well so the spinner displays correctly.

**`extension/manifest.json`** — bump `version` (project memory: every change under `extension/` requires a version bump so the reload check is visible).

### Tests

- `backend/internal/util/urlnorm_test.go` — three cases for `NormalizeURLKeepFragment` (see Part B).
- `backend/internal/api/bookmarklet_test.go` (exists) — add a `force_new=true` case asserting:
  - existing article remains untouched (content, title intact)
  - a new row is inserted with the same URL, `is_clip=true`, and the new content
  - the response is `status: "created"` with the new article ID
- Manual smoke: clip a Gmail thread twice (different `#inbox/...` fragments) → both land as separate articles without a duplicate prompt. Clip the same Gmail URL twice (refresh same email) → duplicate prompt appears → click 新建 → second row appears in the clip bin.

## Out of scope / explicitly deferred

- A UI in the article list view to merge or compare clip articles that share a URL.
- Server-side cleanup of clip articles that legitimately duplicated (e.g., a "merge into the latest" admin tool).
- Heuristic detection of hash-route SPAs to selectively preserve fragment per-site — we keep the simple "clip path always preserves" rule.
- Forward-port of the existing `Force` (in-place overwrite) flag — its semantics are unchanged.

## Risks

- **Migration ordering.** If the backend deploys before `025` is applied, `force_new` inserts will trip the existing unique index and error out. Mitigation: implementation plan applies the migration before deploying the new backend, with a `SELECT COUNT(*) FROM articles WHERE is_clip` check.
- **Fragment-bearing legacy URLs.** Pre-existing clip articles in the DB have fragment-stripped URLs. After this change, re-clipping the same Gmail thread captures a URL with fragment, so the new and old rows don't dedup against each other. The user sees both. Acceptable — old captures are dated, new captures take over visually.
- **Hash-route normalization across sites.** Some sites use `#` for non-route purposes (back-to-top anchors). Preserving them creates more rows than before. We accept this trade-off; mitigation in the future could be a per-site fragment-policy file but not now.

## Verification

1. Migration applied: `SELECT is_clip, COUNT(*) FROM articles GROUP BY is_clip` shows clip-bin rows as `true`, others as `false`. The new `uniq_articles_feed_url_no_child` index exists with the new predicate.
2. Extension version bumped in `manifest.json`; user reloads extension and confirms the version label.
3. Clip a Gmail thread → page A → backend stores URL with `#inbox/abc`. Clip a different email at the same Gmail base URL → backend stores URL with `#inbox/def`. Both visible in clip bin, no duplicate prompt fired for the second.
4. Clip the same Gmail URL twice (no fragment change) → duplicate prompt appears with 覆盖 / 新建 / 取消. Click 新建 → success toast + new row in clip bin (old row unchanged).
5. Click 覆盖 still works (existing behavior preserved).
6. `urlnorm_test.go` passes; `bookmarklet_test.go` `force_new` case passes.
