# Clip rename + in-page tab + sort direction — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stop the 网摘 bottom tab from ejecting users from `/articles`, give the sort button asc/desc per field, and rename the 网摘-bin code path saved → clip (DB + backend + frontend) without disturbing the unrelated `signal_type='save'` star signal.

**Architecture:** Three coupled changes share one deploy: a DB migration flips `feeds.feed_type='saved'` to `'clip'`; backend renames `SavedHandler`/`SavedRepository`/`/api/saved` to the clip equivalent and narrows the result set to clip-bin only; frontend renames its `/saved` route, `SavedPage` component, and api types, then wires the 网摘 tab to `/articles?view=clip` and ArticleListPage reconciles the dropdown to the URL. Sort gets a `SortDir` axis plus an `order` query param, surfaced as two narrow toggleable buttons.

**Tech Stack:** Go 1.24 (Gin, `database/sql` + `lib/pq`), PostgreSQL 15, React 18 + React Router 6 + Vite + axios, Docker Compose.

**Key spec reference:** `docs/superpowers/specs/2026-05-19-clip-rename-and-sort-direction-design.md`

**Deviation note (vs spec):** CSS class names `saved-row` / `saved-row-label` / `saved-row-count` / `saved-section-title` are **also used by `components/TagSidebar.tsx`** (the non-clip universal tag sidebar). Renaming them would force changes to that unrelated component. We keep these CSS class names as-is. The user-facing rename remains: DB feed_type, backend handler/repo/endpoint, frontend route, page/component file names, and TS types.

---

## File map

**Backend — create**
- `backend/migrations/024_rename_feed_type_saved_to_clip.sql`
- `backend/internal/repository/clip.go` (extracted from `user_tag.go`)
- `backend/internal/api/clip.go` (renamed from `saved.go`)

**Backend — delete**
- `backend/internal/api/saved.go` (after content moves to clip.go)

**Backend — modify**
- `backend/internal/repository/article.go` (SortMode → +SortDir, `feed_type IN ('link_set','saved')` literal, comment around line 332)
- `backend/internal/repository/user_tag.go` (remove the relocated Saved* types; update `effectiveSourceFor` + `GetSourceForArticle` to compare `'clip'`; update comments)
- `backend/internal/repository/feed.go` (GetOrCreateSavedFeed → GetOrCreateClipFeed, `'saved'` → `'clip'` in INSERT + WHERE)
- `backend/internal/api/article.go` (parse `order`, `from_bookmarklet` derivation uses `'clip'`)
- `backend/internal/api/feed.go` (caller of GetOrCreateSavedFeed)
- `backend/internal/api/bookmarklet.go` (caller of GetOrCreateSavedFeed)
- `backend/cmd/server/main.go` (handler/repo wiring + route)

**Frontend — create**
- `frontend/src/pages/ClipPage.tsx` (renamed from SavedPage.tsx)
- `frontend/src/components/ClipTagSidebar.tsx` (renamed from SavedTagSidebar.tsx)
- `frontend/src/components/ClipTagChipBar.tsx` (renamed from SavedTagChipBar.tsx)

**Frontend — delete**
- `frontend/src/pages/SavedPage.tsx`
- `frontend/src/components/SavedTagSidebar.tsx`
- `frontend/src/components/SavedTagChipBar.tsx`

**Frontend — modify**
- `frontend/src/api/client.ts` (types + `getClip` + `order` param + `ArticleOrder`)
- `frontend/src/App.tsx` (route swap, import)
- `frontend/src/components/MobileTabBar.tsx` (`to` + custom isActive)
- `frontend/src/pages/ArticleListPage.tsx` (isClippingMode literal, SavedPage import, `useSearchParams` reconciliation, empty hint, sort field/dir state + UI)

---

## Tasks

### Task 1: Add the rename migration file

**Files:**
- Create: `backend/migrations/024_rename_feed_type_saved_to_clip.sql`

- [ ] **Step 1: Write the migration**

```sql
-- 024_rename_feed_type_saved_to_clip.sql
-- Rename the 网摘 pseudo-feed type from 'saved' to 'clip'. The "saved" name
-- collided with user_preferences.signal_type='save' (per-article star/bookmark),
-- which is a different concept. After this migration, 'saved' must no longer
-- appear as a feed_type value.

UPDATE feeds SET feed_type = 'clip' WHERE feed_type = 'saved';
```

- [ ] **Step 2: Commit**

```bash
git add backend/migrations/024_rename_feed_type_saved_to_clip.sql
git commit -m "feat(migration): 024 rename feed_type saved -> clip"
```

> **Do not apply yet.** Backend code still queries `'saved'`. The migration is applied in Task 7 after the backend is updated.

---

### Task 2: Extract Saved* types to a new `clip.go` repo file and rename to Clip*

**Files:**
- Create: `backend/internal/repository/clip.go`
- Modify: `backend/internal/repository/user_tag.go:364-513` (cut the SavedRepository block — `SavedRepository`, `NewSavedRepository`, `SavedQuery`, `SavedRow`, `ListSaved`)

- [ ] **Step 1: Create `backend/internal/repository/clip.go`**

```go
package repository

import (
	"database/sql"
	"strconv"
	"strings"

	"github.com/bytedance/rss-pal/internal/model"
	"github.com/lib/pq"
)

type ClipRepository struct {
	db *sql.DB
}

func NewClipRepository(db *sql.DB) *ClipRepository {
	return &ClipRepository{db: db}
}

// ClipQuery describes a /api/clip request.
//
// SourceKind / SourceValue:
//   - kind="feed", value="<id>"   → filter a.feed_id = id
//   - kind="host", value="<host>" → filter on host extracted from a.url
//     (lower-cased, "www." stripped) — used to drill into a single clip source.
type ClipQuery struct {
	UserID      int
	TagIDs      []int  // empty = "all"
	Mode        string // "and" | "or"; only honored when len(TagIDs)>1
	Untagged    bool   // overrides TagIDs when true
	SourceKind  string // "" | "feed" | "host"
	SourceValue string
	Limit       int
	Offset      int
}

// ClipRow pairs an Article with the EffectiveSource the UI should render.
type ClipRow struct {
	Article         model.Article
	EffectiveSource EffectiveSource
}

// ListClipped returns articles in the user's clip pseudo-feed (feed_type='clip').
// Star-saved articles (user_preferences.signal_type='save') are NOT included;
// they're reached via the 已保存 checkbox on /articles instead.
func (r *ClipRepository) ListClipped(q ClipQuery) ([]ClipRow, int, error) {
	args := []interface{}{q.UserID}
	where := []string{`f.feed_type = 'clip' AND f.owner_id = $1`}
	// Tenancy guard kept for symmetry with other queries in this codebase.
	where = append(where, `(f.owner_id IS NULL OR f.owner_id = $1)`)

	if q.Untagged {
		where = append(where, `NOT EXISTS (
			SELECT 1 FROM article_user_tags aut
			WHERE aut.article_id = a.id AND aut.user_id = $1
		)`)
	} else if len(q.TagIDs) > 0 {
		args = append(args, pq.Array(q.TagIDs))
		idsParam := "$" + strconv.Itoa(len(args))
		if q.Mode == "and" && len(q.TagIDs) > 1 {
			args = append(args, len(q.TagIDs))
			countParam := "$" + strconv.Itoa(len(args))
			where = append(where, `(
				SELECT COUNT(DISTINCT aut.tag_id) FROM article_user_tags aut
				WHERE aut.article_id = a.id AND aut.user_id = $1
				  AND aut.tag_id = ANY(`+idsParam+`::int[])
			) = `+countParam)
		} else {
			where = append(where, `EXISTS (
				SELECT 1 FROM article_user_tags aut
				WHERE aut.article_id = a.id AND aut.user_id = $1
				  AND aut.tag_id = ANY(`+idsParam+`::int[])
			)`)
		}
	}

	switch q.SourceKind {
	case "feed":
		if q.SourceValue != "" {
			args = append(args, q.SourceValue)
			where = append(where, `a.feed_id::text = $`+strconv.Itoa(len(args)))
		}
	case "host":
		if q.SourceValue != "" {
			args = append(args, q.SourceValue)
			where = append(where, `lower(regexp_replace(a.url, '^https?://(?:www\.)?([^/]+).*$', '\1')) = lower($`+strconv.Itoa(len(args))+`)`)
		}
	}

	whereSQL := strings.Join(where, " AND ")

	var total int
	if err := r.db.QueryRow(`
		SELECT COUNT(*) FROM articles a
		JOIN feeds f ON f.id = a.feed_id
		WHERE `+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	args = append(args, q.Limit, q.Offset)
	limitParam := "$" + strconv.Itoa(len(args)-1)
	offsetParam := "$" + strconv.Itoa(len(args))
	rows, err := r.db.Query(`
		SELECT a.id, a.feed_id, f.title AS feed_title, COALESCE(f.feed_type, 'rss') AS feed_type,
		       a.title, a.url,
		       a.published_at, a.summary_brief, a.fetched_at,
		       COALESCE(a.word_count, 0), COALESCE(a.reading_minutes, 0),
		       COALESCE(a.media_type, ''),
		       COALESCE(rp.is_completed, false) AS is_read
		FROM articles a
		JOIN feeds f ON f.id = a.feed_id
		LEFT JOIN reading_progress rp ON rp.article_id = a.id AND rp.user_id = $1
		WHERE `+whereSQL+`
		ORDER BY a.published_at DESC NULLS LAST, a.id DESC
		LIMIT `+limitParam+` OFFSET `+offsetParam, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var out []ClipRow
	for rows.Next() {
		var a model.Article
		var summary, mediaType sql.NullString
		var feedTitle sql.NullString
		var feedType string
		if err := rows.Scan(
			&a.ID, &a.FeedID, &feedTitle, &feedType, &a.Title, &a.URL,
			&a.PublishedAt, &summary, &a.FetchedAt,
			&a.WordCount, &a.ReadingMinutes, &mediaType,
			&a.IsRead,
		); err != nil {
			return nil, 0, err
		}
		a.FeedTitle = feedTitle.String
		a.SummaryBrief = summary.String
		a.MediaType = mediaType.String
		out = append(out, ClipRow{
			Article:         a,
			EffectiveSource: effectiveSourceFor(a.FeedID, feedTitle.String, feedType, a.URL),
		})
	}
	return out, total, rows.Err()
}
```

- [ ] **Step 2: Delete the old block in `user_tag.go`**

Open `backend/internal/repository/user_tag.go`. Delete lines 364–513 (the `SavedRepository` struct through end of `ListSaved`). Keep the comment-only header on `EffectiveSource`/`effectiveSourceFor`/`extractHost` (those stay in user_tag.go because they're shared).

Update the comment on `effectiveSourceFor` (line ~40-42) and on `EffectiveSource` (line ~22-24) and on `GetSourceForArticle` (line ~282-284): replace `bookmarklet (feed_type='saved')` with `clip-bin (feed_type='clip')`, and update the conditional check at lines 44 and 299 from `if feedType == "saved"` to `if feedType == "clip"`. Update comment at line 335 from `/api/saved` to `/api/clip`.

- [ ] **Step 3: Build to confirm no leftover users of old types**

Run: `cd backend && go build ./...`

Expected: compilation errors in `cmd/server/main.go` and `internal/api/saved.go` referencing the now-deleted `SavedRepository`/`NewSavedRepository`/etc. That's expected — fixed in later tasks. Don't commit yet.

> Leave the build broken at this point. The next task brings it back.

---

### Task 3: Rename the handler — `saved.go` → `clip.go`

**Files:**
- Create: `backend/internal/api/clip.go`
- Delete: `backend/internal/api/saved.go`

- [ ] **Step 1: Create `backend/internal/api/clip.go`**

```go
package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/bytedance/rss-pal/internal/model"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/gin-gonic/gin"
)

type ClipHandler struct {
	clip     *repository.ClipRepository
	bindRepo *repository.ArticleUserTagRepository
}

func NewClipHandler(clip *repository.ClipRepository, bindRepo *repository.ArticleUserTagRepository) *ClipHandler {
	return &ClipHandler{clip: clip, bindRepo: bindRepo}
}

// GET /api/clip
func (h *ClipHandler) List(c *gin.Context) {
	userID := getUserID(c)

	q := repository.ClipQuery{
		UserID: userID,
		Mode:   strings.ToLower(c.DefaultQuery("mode", "and")),
		Limit:  20,
		Offset: 0,
	}

	if v := c.Query("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 100 {
			q.Limit = n
		}
	}
	if v := c.Query("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			q.Offset = n
		}
	}
	if c.Query("untagged") == "true" {
		q.Untagged = true
	} else if v := c.Query("tag_ids"); v != "" {
		for _, s := range strings.Split(v, ",") {
			if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil && n > 0 {
				q.TagIDs = append(q.TagIDs, n)
			}
		}
	}
	if v := c.Query("source"); v != "" {
		if i := strings.Index(v, ":"); i > 0 {
			kind := v[:i]
			value := v[i+1:]
			if kind == "feed" || kind == "host" {
				q.SourceKind = kind
				q.SourceValue = value
			}
		}
	}

	rows, total, err := h.clip.ListClipped(q)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if rows == nil {
		rows = []repository.ClipRow{}
	}

	ids := make([]int, len(rows))
	for i, r := range rows {
		ids[i] = r.Article.ID
	}
	tagMap, err := h.bindRepo.GetManualForArticles(ids, userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	type item struct {
		model.Article
		ManualTags      []model.UserTag            `json:"manual_tags"`
		EffectiveSource repository.EffectiveSource `json:"effective_source"`
	}
	out := make([]item, len(rows))
	for i, r := range rows {
		out[i] = item{
			Article:         r.Article,
			ManualTags:      tagMap[r.Article.ID],
			EffectiveSource: r.EffectiveSource,
		}
		if out[i].ManualTags == nil {
			out[i].ManualTags = []model.UserTag{}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"items": out,
		"total": total,
	})
}
```

- [ ] **Step 2: Delete `backend/internal/api/saved.go`**

```bash
rm backend/internal/api/saved.go
```

- [ ] **Step 3: Don't build yet — wiring still missing**

Wiring happens in Task 4.

---

### Task 4: Wire the renamed handler/repo into `main.go`

**Files:**
- Modify: `backend/cmd/server/main.go:40,69,163`

- [ ] **Step 1: Edit `cmd/server/main.go`**

Find line ~40:
```go
savedRepo := repository.NewSavedRepository(db)
```
Replace with:
```go
clipRepo := repository.NewClipRepository(db)
```

Find line ~69:
```go
savedHandler := api.NewSavedHandler(savedRepo, articleUserTagRepo)
```
Replace with:
```go
clipHandler := api.NewClipHandler(clipRepo, articleUserTagRepo)
```

Find line ~163:
```go
apiGroup.GET("/saved", savedHandler.List)
```
Replace with:
```go
apiGroup.GET("/clip", clipHandler.List)
```

- [ ] **Step 2: Build**

Run: `cd backend && go build ./...`

Expected: SUCCESS. If errors remain, grep for `SavedRepository\|SavedHandler\|/saved\b` under `backend/cmd backend/internal` and fix.

- [ ] **Step 3: Commit (backend rename so far — endpoint is `/api/clip` returning ONLY clip-bin items, but DB still has 'saved' literals)**

```bash
git add backend/internal/repository/clip.go backend/internal/repository/user_tag.go \
        backend/internal/api/clip.go backend/internal/api/saved.go \
        backend/cmd/server/main.go
git commit -m "refactor(backend): rename Saved -> Clip handler/repo; /api/clip returns only clip-bin items"
```

> Note: at this commit, `/api/clip` will return zero rows because the DB still stores `feed_type='saved'` and the new code queries `'clip'`. That's fine — Task 7 applies the migration. Do not deploy this commit alone.

---

### Task 5: Replace remaining `'saved'` feed_type literals across backend

**Files:**
- Modify: `backend/internal/repository/feed.go:222-269` (`GetOrCreateSavedFeed`)
- Modify: `backend/internal/repository/article.go:332,1499`
- Modify: `backend/internal/api/article.go:210`
- Modify: `backend/internal/api/feed.go:496,575`
- Modify: `backend/internal/api/bookmarklet.go:215,217`

- [ ] **Step 1: Rename `GetOrCreateSavedFeed` → `GetOrCreateClipFeed` in `repository/feed.go`**

Edit `feed.go`:
- Function name: `GetOrCreateSavedFeed` → `GetOrCreateClipFeed`
- Doc comment above it: update prose referring to "saved" pseudo-feed → "clip" pseudo-feed
- Line 229 SQL: `feed_type = 'saved'` → `feed_type = 'clip'`
- Line 259: `FeedType: "saved"` → `FeedType: "clip"`

- [ ] **Step 2: Update callers**

In `backend/internal/api/feed.go`:
- Line ~496 comment: `GetOrCreateSavedFeed` → `GetOrCreateClipFeed`
- Line ~575: `h.repo.GetOrCreateSavedFeed(*ownerID)` → `h.repo.GetOrCreateClipFeed(*ownerID)`

In `backend/internal/api/bookmarklet.go`:
- Line ~215: `h.feedRepo.GetOrCreateSavedFeed(user.ID)` → `h.feedRepo.GetOrCreateClipFeed(user.ID)`
- Line ~217 log message: `"bookmarklet: GetOrCreateSavedFeed failed for user=%d: %v"` → `"bookmarklet: GetOrCreateClipFeed failed for user=%d: %v"`

- [ ] **Step 3: Update SQL literals in `repository/article.go`**

Line ~332 comment: change `(e.g., "rss" / "saved" / "youtube")` → `(e.g., "rss" / "clip" / "youtube")`.

Line ~1499:
```go
AND f.feed_type IN ('link_set', 'saved')
```
→
```go
AND f.feed_type IN ('link_set', 'clip')
```

- [ ] **Step 4: Update derivation in `api/article.go`**

Line ~210:
```go
"from_bookmarklet": feedType == "saved",
```
→
```go
"from_bookmarklet": feedType == "clip",
```

- [ ] **Step 5: Build + sanity grep**

Run: `cd backend && go build ./...` → SUCCESS.

Run:
```bash
grep -rn "feed_type.*saved\|'saved'\|\"saved\"" backend/internal backend/cmd \
  | grep -v _test.go \
  | grep -v migrations \
  | grep -v snapshot_saved.go \
  | grep -v signal_type
```
Expected: zero output. (The `snapshot_saved.go` and `signal_type` lines are intentionally excluded — they're the non-renamed neighbors.)

If hits remain, fix them and re-grep until clean.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/repository/feed.go backend/internal/repository/article.go \
        backend/internal/api/article.go backend/internal/api/feed.go \
        backend/internal/api/bookmarklet.go
git commit -m "refactor(backend): replace 'saved' feed_type literals with 'clip'"
```

---

### Task 6: Backend sort direction — add `SortDir`, accept `order` param

**Files:**
- Modify: `backend/internal/repository/article.go:230-284` (SortMode + GetAll signature + ORDER BY)
- Modify: `backend/internal/api/article.go:133-139` (parse `order`, pass to GetAll)

- [ ] **Step 1: Add `SortDir` next to `SortMode` in `repository/article.go`**

Locate the `SortMode` block (around line 230–235) and append:

```go
// SortDir selects ascending vs descending for the chosen SortMode. Defaults
// to SortDesc; SortAsc is exposed so the UI can flip per field independently.
type SortDir int

const (
	SortDesc SortDir = iota
	SortAsc
)

func (d SortDir) sql() string {
	if d == SortAsc {
		return "ASC"
	}
	return "DESC"
}
```

- [ ] **Step 2: Update `GetAll` signature and ORDER BY**

Change the signature on line 237:
```go
func (r *ArticleRepository) GetAll(limit, offset int, feedID *int, unreadOnly bool, savedOnly bool, userID int, tagID *int, untagged bool, sort SortMode, dir SortDir) ([]model.Article, error) {
```

Update the switch around line 263:
```go
switch sort {
case SortCaptured:
	query += fmt.Sprintf(" ORDER BY articles.fetched_at %s LIMIT $%d OFFSET $%d", dir.sql(), nextArg, nextArg+1)
default:
	d := dir.sql()
	query += fmt.Sprintf(" ORDER BY DATE_TRUNC('day', GREATEST(COALESCE(articles.published_at, articles.fetched_at), articles.fetched_at - INTERVAL '7 days')) %s, COALESCE(articles.published_at, articles.fetched_at) %s LIMIT $%d OFFSET $%d", d, d, nextArg, nextArg+1)
}
```

- [ ] **Step 3: Update the single caller in `api/article.go`**

Around line 133–139, after the existing `sort := repository.SortPublished` block, add:
```go
dir := repository.SortDesc
if c.Query("order") == "asc" {
	dir = repository.SortAsc
}
```

Change the GetAll call (line 139):
```go
articles, err := h.articleRepo.GetAll(limit, offset, feedID, unreadOnly, savedOnly, userID, tagID, untagged, sort, dir)
```

- [ ] **Step 4: Build**

Run: `cd backend && go build ./...` → SUCCESS.

If other callers of `GetAll` exist that I missed, fix them by passing `repository.SortDesc` (preserve current behavior). Grep first: `grep -rn "articleRepo.GetAll\|\.GetAll(limit" backend/ | grep -v _test`.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/repository/article.go backend/internal/api/article.go
git commit -m "feat(backend): add SortDir asc/desc to /api/articles via order param"
```

---

### Task 7: Apply migration + restart services

> **DB safety note:** This is a DB-touching step. Per `CLAUDE.md`, take a backup before applying and verify counts after.

**Files:**
- None (operations only)

- [ ] **Step 1: Take a backup**

```bash
docker-compose exec -T postgres pg_dump -U postgres rsspal \
  > backups.pre-migrate-$(date +%Y%m%d-%H%M%S).sql
```

Expected: a non-empty `.sql` file in the repo root.

- [ ] **Step 2: Dry-run inspect — confirm rows exist with old value**

```bash
docker-compose exec -T postgres psql -U postgres -d rsspal -c \
  "SELECT feed_type, COUNT(*) FROM feeds GROUP BY feed_type ORDER BY feed_type;"
```

Expected: a row with `feed_type = 'saved'` and a positive count (assuming the user has used the bookmarklet/extension at least once). Note the count `N`.

- [ ] **Step 3: Apply migration**

```bash
docker-compose exec -T postgres psql -U postgres -d rsspal \
  -f /docker-entrypoint-initdb.d/024_rename_feed_type_saved_to_clip.sql
```

If the file isn't mounted under that path, cat-pipe instead:
```bash
docker-compose exec -T postgres psql -U postgres -d rsspal \
  < backend/migrations/024_rename_feed_type_saved_to_clip.sql
```

Expected output: `UPDATE N` matching the count from Step 2.

- [ ] **Step 4: Verify**

```bash
docker-compose exec -T postgres psql -U postgres -d rsspal -c \
  "SELECT feed_type, COUNT(*) FROM feeds GROUP BY feed_type ORDER BY feed_type;"
```

Expected: zero rows for `'saved'`, a row for `'clip'` with count `N` (matches Step 2).

- [ ] **Step 5: Rebuild + restart backend**

```bash
docker-compose up -d --build api worker
docker-compose logs -f api | head -50
```

Expected: API starts cleanly, no errors about missing columns or stuck feeds.

- [ ] **Step 6: Smoke-test `/api/clip`**

Use the existing auth cookie in your browser session (or curl with a JWT). Hit:
```
GET http://localhost:8080/api/clip?limit=5
```
Expected: 200 OK with `{"items": [...], "total": N}` where items are from the clip bin (`from_bookmarklet`-eligible). `/api/saved` should now 404.

- [ ] **Step 7: Note in commit log**

This is an operational step, no commit required.

---

### Task 8: Frontend — rename Saved → Clip in `api/client.ts`

**Files:**
- Modify: `frontend/src/api/client.ts:273-284, 297, 765-808`

- [ ] **Step 1: Add `ArticleOrder` and extend `getArticles` params**

Around line 273:
```ts
export type ArticleSort = 'published' | 'captured'
export type ArticleOrder = 'asc' | 'desc'

export const getArticles = (params?: {
  feed_id?: number
  unread?: boolean
  saved?: boolean
  tag_id?: number
  untagged?: boolean
  limit?: number
  offset?: number
  sort?: ArticleSort
  order?: ArticleOrder
}) => api.get<Article[]>('/articles', { params }).then(res => res.data)
```

`saved?` on this params object stays — it's the `signal_type='save'` filter (已保存 checkbox), unrelated to the clip rename.

- [ ] **Step 2: Rename the Saved* block at lines 765-808**

Replace the entire `// === Saved (Phase 2) ===` block (`EffectiveSource`, `SavedItem`, `SavedListResponse`, `GetSavedParams`, `getSaved`) with:

```ts
// === Clip (网摘) ===

// EffectiveSource: what the source-tag chip on a clip article should say.
// `key` is "feed:<id>" or "host:<host>" — opaque to the UI, passed back as
// the `source` filter on /api/clip.
export interface EffectiveSource {
  key: string
  title: string
}

export type ClipItem = Article & {
  manual_tags: UserTag[]
  effective_source: EffectiveSource
}

export interface ClipListResponse {
  items: ClipItem[]
  total: number
}

export interface GetClipParams {
  tag_ids?: number[]
  mode?: 'and' | 'or'
  untagged?: boolean
  source?: string // EffectiveSource.key, e.g. "feed:8" or "host:github.com"
  limit?: number
  offset?: number
}

export const getClip = (params: GetClipParams = {}) => {
  const query: Record<string, string | number | boolean> = {}
  if (params.untagged) {
    query.untagged = 'true'
  } else if (params.tag_ids && params.tag_ids.length > 0) {
    query.tag_ids = params.tag_ids.join(',')
    if (params.tag_ids.length > 1 && params.mode) {
      query.mode = params.mode
    }
  }
  if (params.source) query.source = params.source
  if (params.limit !== undefined) query.limit = params.limit
  if (params.offset !== undefined) query.offset = params.offset
  return api.get<ClipListResponse>('/clip', { params: query }).then(r => r.data)
}
```

- [ ] **Step 3: Don't build yet — frontend consumers still reference old names**

The next two tasks rename them. Build runs at Task 11 Step 4.

---

### Task 9: Frontend — rename SavedPage.tsx → ClipPage.tsx

**Files:**
- Create: `frontend/src/pages/ClipPage.tsx`
- Delete: `frontend/src/pages/SavedPage.tsx`

- [ ] **Step 1: Copy the file**

```bash
git mv frontend/src/pages/SavedPage.tsx frontend/src/pages/ClipPage.tsx
```

- [ ] **Step 2: Rename inside the file**

Open `ClipPage.tsx` and do these edits:

1. Imports: replace
```ts
import {
  GetSavedParams,
  SavedItem,
  SavedListResponse,
  UserTag,
  getSaved,
  listTags,
} from '../api/client'
import SavedTagSidebar, {
  SavedSelection,
  SavedSourceRow,
} from '../components/SavedTagSidebar'
import SavedTagChipBar from '../components/SavedTagChipBar'
```
with
```ts
import {
  GetClipParams,
  ClipItem,
  ClipListResponse,
  UserTag,
  getClip,
  listTags,
} from '../api/client'
import ClipTagSidebar, {
  ClipSelection,
  ClipSourceRow,
} from '../components/ClipTagSidebar'
import ClipTagChipBar from '../components/ClipTagChipBar'
```

2. Rename every occurrence (in-file):
- `SavedSelection` → `ClipSelection`
- `SavedSourceRow` → `ClipSourceRow`
- `SavedItem` → `ClipItem`
- `SavedListResponse` → `ClipListResponse`
- `GetSavedParams` → `GetClipParams`
- `getSaved(` → `getClip(`
- `<SavedTagSidebar` → `<ClipTagSidebar`
- `</SavedTagSidebar>` → `</ClipTagSidebar>`
- `<SavedTagChipBar` → `<ClipTagChipBar`
- `</SavedTagChipBar>` → `</ClipTagChipBar>`
- Function/component name: `export default function SavedPage` → `export default function ClipPage`
- `interface SavedPageProps` → `interface ClipPageProps`
- Default for `entryPath` prop: change `entryPath = '/saved'` → `entryPath = '/clip'`
- Comments: any reference to "网摘 (clipping)" or "saved-bin" in prose may stay since the Chinese label 网摘 is unchanged; do replace any remaining English use of `saved` referring to the bin with `clip`.

3. Save.

- [ ] **Step 3: Don't build yet**

Components still need to be renamed in Task 10.

---

### Task 10: Frontend — rename SavedTagSidebar / SavedTagChipBar

**Files:**
- Create: `frontend/src/components/ClipTagSidebar.tsx`
- Create: `frontend/src/components/ClipTagChipBar.tsx`
- Delete: `frontend/src/components/SavedTagSidebar.tsx`
- Delete: `frontend/src/components/SavedTagChipBar.tsx`

- [ ] **Step 1: Rename the files**

```bash
git mv frontend/src/components/SavedTagSidebar.tsx frontend/src/components/ClipTagSidebar.tsx
git mv frontend/src/components/SavedTagChipBar.tsx frontend/src/components/ClipTagChipBar.tsx
```

- [ ] **Step 2: Rename symbols in `ClipTagSidebar.tsx`**

Open the file and do:
- `SavedTagSidebar` → `ClipTagSidebar` (default-export function + any internal refs)
- Exported types: `SavedSelection` → `ClipSelection`, `SavedSourceRow` → `ClipSourceRow`
- Any imports of these types from `api/client` (likely none — they're locally defined)
- If the component references `SavedItem` from `api/client`, change to `ClipItem`
- **Leave CSS class names** `saved-row`, `saved-row-label`, `saved-row-count`, `saved-section-title` **unchanged** (shared with TagSidebar.tsx — see plan header deviation note).
- Comments: replace prose use of "saved" referring to the bin with "clip"; keep the Chinese "网摘" UI strings as-is.

- [ ] **Step 3: Rename symbols in `ClipTagChipBar.tsx`**

Same pattern:
- `SavedTagChipBar` → `ClipTagChipBar`
- Any internal type references named `Saved*` → `Clip*`
- CSS classes unchanged.

- [ ] **Step 4: Don't build yet — App.tsx + ArticleListPage still import old names**

---

### Task 11: Frontend — App.tsx route + ArticleListPage import / literal updates

**Files:**
- Modify: `frontend/src/App.tsx:70` (route + import)
- Modify: `frontend/src/pages/ArticleListPage.tsx:7, 204, 233, 599`

- [ ] **Step 1: Update `App.tsx`**

Line 16 — change
```ts
import SavedPage from './pages/SavedPage'
```
to
```ts
import ClipPage from './pages/ClipPage'
```

Line 70 — change
```tsx
<Route path="saved" element={<SavedPage />} />
```
to
```tsx
<Route path="clip" element={<ClipPage />} />
```

- [ ] **Step 2: Update `ArticleListPage.tsx`**

Line 7:
```ts
import SavedPage from './SavedPage'
```
→
```ts
import ClipPage from './ClipPage'
```

Line 204:
```ts
const isClippingMode = selectedFeedObj?.feed_type === 'saved'
```
→
```ts
const isClippingMode = selectedFeedObj?.feed_type === 'clip'
```

Line 233 comment: replace `SavedPage component owns its own data fetching` → `ClipPage component owns its own data fetching`.

Line 599 JSX:
```tsx
<SavedPage
  restrictToFeedId={selectedFeed}
  entryPath="/articles"
  sidebarOpen={sidebarOpen}
/>
```
→
```tsx
<ClipPage
  restrictToFeedId={selectedFeed}
  entryPath="/articles"
  sidebarOpen={sidebarOpen}
/>
```

- [ ] **Step 3: Sanity grep**

```bash
grep -rn "SavedPage\|SavedTagSidebar\|SavedTagChipBar\|SavedItem\|SavedListResponse\|GetSavedParams\|getSaved\b\|SavedSelection\|SavedSourceRow" frontend/src/
```
Expected: zero output.

```bash
grep -rn "feed_type.*saved\|'saved'\|\"saved\"" frontend/src/
```
Expected: only the `?saved=true` query string (already filtered by `savedOnly`) lines in `api/client.ts:618` (`form.append('saved', input.saved)`) and `ArticleListPage.tsx`'s `savedOnly` calls — those refer to the unrelated star signal and stay.

- [ ] **Step 4: Build frontend**

```bash
cd frontend && npm run build
```

Expected: SUCCESS, no TS errors.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/api/client.ts \
        frontend/src/pages/ClipPage.tsx frontend/src/pages/SavedPage.tsx \
        frontend/src/components/ClipTagSidebar.tsx frontend/src/components/SavedTagSidebar.tsx \
        frontend/src/components/ClipTagChipBar.tsx frontend/src/components/SavedTagChipBar.tsx \
        frontend/src/App.tsx frontend/src/pages/ArticleListPage.tsx
git commit -m "refactor(frontend): rename Saved -> Clip (page, components, types, route)"
```

Per `CLAUDE.md` memory: rebuild the docker frontend container next.

```bash
docker-compose up -d --build frontend
```

---

### Task 12: MobileTabBar — point 网摘 to `/articles?view=clip` with custom isActive

**Files:**
- Modify: `frontend/src/components/MobileTabBar.tsx`

- [ ] **Step 1: Replace the file**

Open `MobileTabBar.tsx`. Change the imports header to include `useLocation`:

```ts
import { useState } from 'react'
import { NavLink, useLocation } from 'react-router-dom'
import MoreSheet from './MoreSheet'
```

Change the `TABS` array:
```ts
type Tab = { to: string; icon: string; label: string; showUnread?: boolean; matchClip?: boolean }

const TABS: Tab[] = [
  { to: '/articles',            icon: '📰', label: '文章', showUnread: true, matchClip: false },
  { to: '/articles?view=clip',  icon: '⭐', label: '网摘',                   matchClip: true  },
  { to: '/feeds',               icon: '📡', label: '订阅' },
]
```

Inside the component body, before `return`, compute a helper:
```ts
const location = useLocation()
const isClipView = location.pathname === '/articles'
  && new URLSearchParams(location.search).get('view') === 'clip'

const tabIsActive = (t: Tab) => {
  if (t.to === '/articles') {
    return location.pathname === '/articles' && !isClipView
  }
  if (t.matchClip) {
    return isClipView
  }
  // Other tabs (/feeds): plain pathname prefix match.
  return location.pathname === t.to || location.pathname.startsWith(t.to + '/')
}
```

In the `TABS.map(...)` render, switch from NavLink's built-in `isActive` callback to the helper:
```tsx
{TABS.map(tab => {
  const active = tabIsActive(tab)
  return (
    <NavLink
      key={tab.to}
      to={tab.to}
      end={false}
      className="mobile-tab-link"
      style={tabStyle(active)}
    >
      <span style={{ fontSize: 22, lineHeight: 1, position: 'relative' }}>
        {tab.icon}
        {tab.showUnread && unreadCount > 0 && (
          <span
            className="unread-badge"
            style={{ position: 'absolute', top: -4, right: -10 }}
          >
            {unreadCount > 99 ? '99+' : unreadCount}
          </span>
        )}
      </span>
      <span>{tab.label}</span>
    </NavLink>
  )
})}
```

(Style helper `tabStyle(active)` already exists in the file and doesn't change.)

- [ ] **Step 2: Build**

```bash
cd frontend && npm run build
```

Expected: SUCCESS.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/MobileTabBar.tsx
git commit -m "feat(frontend): MobileTabBar 网摘 stays on /articles via ?view=clip"
```

---

### Task 13: ArticleListPage — reconcile `?view=clip` to clip feed selection (and back)

**Files:**
- Modify: `frontend/src/pages/ArticleListPage.tsx` (imports + new effect + dropdown change handler + empty-hint render)

- [ ] **Step 1: Update imports**

Top of the file (around line 2):
```ts
import { Link, useNavigate, useSearchParams } from 'react-router-dom'
```

- [ ] **Step 2: Add the searchParams hook + reconcile effect**

Inside the component, near the other state declarations (right after `const navigate = useNavigate()`), add:

```ts
const [searchParams, setSearchParams] = useSearchParams()
const wantsClip = searchParams.get('view') === 'clip'
```

Add a new `useEffect` that runs after feeds load. Place it directly after the existing `useEffect` that calls `loadFeeds()` (around line 220–230):

```ts
// Reconcile the ?view=clip URL param with the dropdown selection.
// - view=clip + non-clip selection → switch to the clip feed
// - view=clip + no clip feed exists → leave selection null and show hint
// - no view param + currently on clip feed → clear selection back to "all"
useEffect(() => {
  if (feeds.length === 0) return
  const clipFeed = feeds.find(f => f.feed_type === 'clip')
  const selectedIsClip = selectedFeedObj?.feed_type === 'clip'
  if (wantsClip) {
    if (clipFeed && selectedFeed !== clipFeed.id) {
      setSelectedFeed(clipFeed.id)
      try { sessionStorage.setItem('selectedFeed', JSON.stringify(clipFeed.id)) } catch {}
    } else if (!clipFeed && selectedFeed !== null) {
      setSelectedFeed(null)
      try { sessionStorage.setItem('selectedFeed', 'null') } catch {}
    }
  } else {
    if (selectedIsClip) {
      setSelectedFeed(null)
      try { sessionStorage.setItem('selectedFeed', 'null') } catch {}
    }
  }
}, [feeds, wantsClip])
```

> `selectedFeedObj` is already declared above on line ~203 — make sure this new effect lives *after* that derived value definition. If lexical ordering bites you, inline the lookup: `const selectedIsClip = feeds.find(f => f.id === selectedFeed)?.feed_type === 'clip'`.

- [ ] **Step 3: Sync URL when user manually changes the dropdown**

`ArticleListPage.tsx` lines 513–519 — replace
```tsx
<select
  value={selectedFeed || ''}
  onChange={e => {
    const val = e.target.value ? Number(e.target.value) : null
    setSelectedFeed(val)
    try { sessionStorage.setItem('selectedFeed', JSON.stringify(val)) } catch {}
  }}
```
with
```tsx
<select
  value={selectedFeed || ''}
  onChange={e => {
    const val = e.target.value ? Number(e.target.value) : null
    setSelectedFeed(val)
    try { sessionStorage.setItem('selectedFeed', JSON.stringify(val)) } catch {}
    const pickedClip = val != null && feeds.find(f => f.id === val)?.feed_type === 'clip'
    if (pickedClip && !wantsClip) {
      setSearchParams({ view: 'clip' })
    } else if (!pickedClip && wantsClip) {
      setSearchParams({})
    }
  }}
```

- [ ] **Step 4: Empty hint when wantsClip + no clip feed exists**

Just above the existing `{isClippingMode && selectedFeed != null && (<ClipPage ... />)}` block (line ~598), add:

```tsx
{wantsClip && !isClippingMode && !feeds.find(f => f.feed_type === 'clip') && (
  <div className="text-muted" style={{ padding: 24, textAlign: 'center' }}>
    还没有网摘 — 安装浏览器扩展或书签后再来收藏文章。
  </div>
)}
```

- [ ] **Step 5: Build**

```bash
cd frontend && npm run build
```

Expected: SUCCESS.

- [ ] **Step 6: Manual verify in browser**

```bash
docker-compose up -d --build frontend
```

Then in browser:
1. Visit `http://localhost/articles` — URL stays `/articles`, 文章 tab highlighted.
2. Tap 网摘 — URL becomes `/articles?view=clip`, clip view renders, 网摘 tab highlighted.
3. Tap 文章 — URL returns to `/articles`, clip view goes away.
4. From clip view, pick a different feed in the dropdown — URL drops `?view=clip`.
5. Old URL `http://localhost/saved` — catch-all sends you to `/articles` (per `App.tsx` `path="*"`).

- [ ] **Step 7: Commit**

```bash
git add frontend/src/pages/ArticleListPage.tsx
git commit -m "feat(frontend): ArticleListPage reconciles ?view=clip with clip feed selection"
```

---

### Task 14: Frontend — sort field × direction (two narrow buttons)

**Files:**
- Modify: `frontend/src/pages/ArticleListPage.tsx` (state, persistence, button block at lines 558–571, getArticles call site)

- [ ] **Step 1: Replace the single `sortMode` state with split `sortField` + `sortDir`**

Find (around line 161–163):
```ts
const [sortMode, setSortMode] = useState<ArticleSort>(() => {
  try { return (sessionStorage.getItem('articlesSort') as ArticleSort) === 'captured' ? 'captured' : 'published' } catch { return 'published' }
})
```

Replace with:
```ts
const [sortField, setSortField] = useState<ArticleSort>(() => {
  try {
    const v = sessionStorage.getItem('articlesSortField')
      ?? sessionStorage.getItem('articlesSort') // legacy fallback
    return v === 'captured' ? 'captured' : 'published'
  } catch { return 'published' }
})
const [sortDir, setSortDir] = useState<ArticleOrder>(() => {
  try { return sessionStorage.getItem('articlesSortDir') === 'asc' ? 'asc' : 'desc' } catch { return 'desc' }
})
```

Add `ArticleOrder` to the import at the top:
```ts
import { /* ...existing... */, ArticleOrder } from '../api/client'
```

- [ ] **Step 2: Replace remaining `sortMode` references**

Search the file for `sortMode` (also appears in the deps arrays of two `useEffect`s, and inside `loadArticles`'s `getArticles({ ... sort: sortMode })`).

Updates:
- In `loadArticles`'s call (around line 282):
  ```ts
  sort: sortField,
  order: sortDir,
  ```
- In the two effect deps arrays (lines 253 and 304): replace `sortMode` with `sortField, sortDir`.
- Anywhere else `sortMode` is referenced, swap to `sortField` (field check) — context will tell you which is meant.

- [ ] **Step 3: Replace the sort button block**

Find (around line 558–571):
```tsx
{!isClippingMode && !searchQuery && !grouped && (
  <button
    type="button"
    className="btn-ghost"
    onClick={() => {
      const next: ArticleSort = sortMode === 'captured' ? 'published' : 'captured'
      setSortMode(next)
      try { sessionStorage.setItem('articlesSort', next) } catch {}
    }}
    title={sortMode === 'captured' ? '当前按抓取时间排序,点击切换为发布时间' : '当前按发布时间排序,点击切换为抓取时间'}
  >
    ⏱ {sortMode === 'captured' ? '抓取时间' : '发布时间'}
  </button>
)}
```

Replace with two narrow toggles:

```tsx
{!isClippingMode && !searchQuery && !grouped && (() => {
  const pick = (field: ArticleSort) => {
    if (field === sortField) {
      const next: ArticleOrder = sortDir === 'desc' ? 'asc' : 'desc'
      setSortDir(next)
      try { sessionStorage.setItem('articlesSortDir', next) } catch {}
    } else {
      setSortField(field)
      try { sessionStorage.setItem('articlesSortField', field) } catch {}
    }
  }
  const arrow = sortDir === 'asc' ? '↑' : '↓'
  const btn = (field: ArticleSort, label: string) => {
    const active = sortField === field
    return (
      <button
        type="button"
        className={active ? '' : 'btn-ghost'}
        onClick={() => pick(field)}
        style={{ padding: '4px 8px', minWidth: 0 }}
        title={
          active
            ? '再点切换升序/降序'
            : `点击按${label}排序`
        }
      >
        {label}{active ? ` ${arrow}` : ''}
      </button>
    )
  }
  return (
    <div style={{ display: 'inline-flex', gap: 4 }}>
      {btn('published', '发布')}
      {btn('captured', '抓取')}
    </div>
  )
})()}
```

> Labels shortened to `发布` / `抓取` so the two buttons fit narrow per user direction ("上下按钮可以窄"). The arrow only renders on the active button.

- [ ] **Step 4: Build**

```bash
cd frontend && npm run build
```

Expected: SUCCESS.

- [ ] **Step 5: Rebuild frontend container + smoke test**

```bash
docker-compose up -d --build frontend
```

In browser at `/articles`:
1. Default state: `发布 ↓` active, `抓取` ghost. List newest-published first.
2. Tap `发布` → label becomes `发布 ↑`, list flips to oldest-first.
3. Tap `抓取` → `抓取 ↑` active (direction preserved); list reorders by `fetched_at` asc.
4. Tap `抓取` again → `抓取 ↓`.
5. Reload page → state restored from sessionStorage.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/pages/ArticleListPage.tsx
git commit -m "feat(frontend): sort field x direction (asc/desc) on /articles"
```

---

### Task 15: Final verification

**Files:** None (verification only)

- [ ] **Step 1: Run the full sanity grep one more time**

```bash
grep -rn "feed_type.*saved\|'saved'\|\"saved\"\|SavedPage\|SavedHandler\|SavedRepository\|GetOrCreateSavedFeed\|getSaved\b\|/api/saved\|SavedTagSidebar\|SavedTagChipBar" \
  backend/internal backend/cmd frontend/src \
  | grep -v _test.go \
  | grep -v migrations \
  | grep -v snapshot_saved.go \
  | grep -v signal_type
```
Expected: zero output.

- [ ] **Step 2: DB state**

```bash
docker-compose exec -T postgres psql -U postgres -d rsspal -c \
  "SELECT feed_type, COUNT(*) FROM feeds GROUP BY feed_type ORDER BY feed_type;"
```
Expected: zero rows for `'saved'`, expected count for `'clip'`.

- [ ] **Step 3: End-to-end walk-through (golden path)**

1. `/articles` — list loads, 文章 tab active, `发布 ↓` button active.
2. Click `发布` → `发布 ↑`, list reverses (oldest at top).
3. Click `抓取` → `抓取 ↑` active.
4. Tap 网摘 tab → URL becomes `/articles?view=clip`, clip layout renders (tag sidebar + clip articles).
5. From clip view, pick a non-clip feed in the dropdown → URL loses `?view=clip`, sort buttons reappear.
6. Visit `/clip` directly → ClipPage renders standalone.
7. Visit `/saved` → catch-all redirects to `/articles`.
8. Star an RSS article via the existing 已保存 toggle → it shows up under `/articles` with 已保存 checkbox checked, **does not** appear in `/clip` or under the 网摘 tab. (This is the spec's behavior change — confirm it.)

- [ ] **Step 4: Backup retention check**

The pre-migration backup file from Task 7 Step 1 is in the repo root (`backups.pre-migrate-<ts>.sql`). Per `CLAUDE.md` DB-safety memory, leave it in place; user can move/delete after the change settles.

---

## Notes for the executing engineer

- **Don't skip Task 7.** Several tasks before it leave the build technically working but the DB out of sync. Until the migration runs, `/api/clip` returns zero rows. Don't conclude "it's broken" from that state — verify the DB state first.
- **`signal_type='save'` stays as-is everywhere.** If you find yourself editing a line that compares against the SQL string `'save'` (singular, no `d`) or a `signal_type` column, stop — that's the unrelated star signal. Same for `savedOnly` Go variables and the `?saved=true` query param.
- **CSS classes** `saved-row` / `saved-section-title` / `saved-row-label` / `saved-row-count` are intentionally untouched — they're shared with the universal `TagSidebar.tsx` and aren't part of the rename.
- **Per project memory:** after any `frontend/src/` change, rebuild the frontend container (`docker-compose up -d --build frontend`) — there's no hot reload through nginx.
- **Commits:** the plan splits work into small, working commits. Don't batch them — each commit should leave the tree buildable (with the exception explicitly called out before Task 4's commit, where `/api/clip` returns no rows until Task 7 applies the migration).
