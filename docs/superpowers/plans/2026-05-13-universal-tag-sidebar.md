# Universal Tag Sidebar Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a collapsible left tag sidebar to all article-list views, surface untagged articles, and display manual tags on every article card.

**Architecture:** Backend extends `/api/articles` with `tag_id` / `untagged` filters and adds `manual_tags` to each item in the response (anonymous wrapper struct around `model.Article`, matching the existing `/api/saved` pattern). A new `/api/tags/sidebar` endpoint returns dynamic per-tag counts that respect the current feed/unread/saved filter, plus `total_count` and `untagged_count`. Frontend reuses the saved-page sidebar pattern, gated by a new top-left toggle button; tag filter state in `sessionStorage`, sidebar open state in `localStorage`.

**Tech Stack:** Go (Gin, `database/sql`, `lib/pq`), React 18 + TypeScript + Vite, axios. No new dependencies.

**Test approach:** The repository layer has no existing test infrastructure (only `util`, `transcript`, `rss` packages are tested). The plan verifies by `go build` + `curl` smoke tests against the running stack, plus manual UI verification. Each task ends with a runnable check.

**Reference spec:** `docs/superpowers/specs/2026-05-13-universal-tag-sidebar-design.md`

**Operational notes:**
- Frontend changes require `docker-compose up -d --build frontend` to take effect (nginx serves a pre-built bundle).
- Backend changes require `docker-compose up -d --build api` (or restart the dev `go run` process).
- No DB migrations.

---

## Task 1: Add `manual_tags` to `/api/articles` response

**Goal:** Every article returned by `GET /api/articles` carries the calling user's manual tags on that article (empty array if none). No filter changes yet.

**Files:**
- Modify: `backend/internal/api/article.go` — wrap response items with `manual_tags`
- Modify: `backend/cmd/server/main.go` — inject `ArticleUserTagRepository` into `ArticleHandler` if not already wired

**Pattern to follow:** `backend/internal/api/saved.go:74-105` already does this for `/api/saved`. Mirror it.

- [ ] **Step 1: Locate the existing `/api/articles` handler**

```bash
grep -n "func.*GetAll\b" backend/internal/api/article.go
```

Expected: a line like `func (h *ArticleHandler) GetAll(c *gin.Context) {`.

- [ ] **Step 2: Check whether `ArticleHandler` already has a tag-bind repo dependency**

```bash
grep -n "bindRepo\|ArticleUserTagRepository\|UserTagRepository" backend/internal/api/article.go backend/cmd/server/main.go
```

If `ArticleHandler` does not already receive an `*repository.ArticleUserTagRepository`, add it.

- [ ] **Step 3: Wire the repo into `ArticleHandler`**

Edit `backend/internal/api/article.go`. Add a `bindRepo *repository.ArticleUserTagRepository` field to the handler struct and update its constructor. Example shape (adapt to the existing constructor signature):

```go
type ArticleHandler struct {
    repo     *repository.ArticleRepository
    bindRepo *repository.ArticleUserTagRepository
    // ...existing fields...
}

func NewArticleHandler(
    repo *repository.ArticleRepository,
    bindRepo *repository.ArticleUserTagRepository,
    // ...existing args...
) *ArticleHandler {
    return &ArticleHandler{
        repo:     repo,
        bindRepo: bindRepo,
        // ...
    }
}
```

Update the constructor call site in `backend/cmd/server/main.go` to pass the existing `articleUserTagRepo` (or whatever variable name is used there — `grep "ArticleUserTag" backend/cmd/server/main.go` to find).

- [ ] **Step 4: Build to confirm the wiring compiles**

```bash
cd backend && go build ./...
```

Expected: exits 0, no errors.

- [ ] **Step 5: Wrap the JSON response in `GetAll` with `manual_tags`**

In `backend/internal/api/article.go::GetAll`, after the call that yields `articles []model.Article`, add:

```go
// Batch-load manual tags so every list item shows the chips that
// the per-article TagBar already lets users add.
userID := getUserID(c)
ids := make([]int, len(articles))
for i, a := range articles {
    ids[i] = a.ID
}
tagMap, err := h.bindRepo.GetManualForArticles(ids, userID)
if err != nil {
    c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
    return
}

type item struct {
    model.Article
    ManualTags []model.UserTag `json:"manual_tags"`
}
out := make([]item, len(articles))
for i, a := range articles {
    out[i] = item{Article: a, ManualTags: tagMap[a.ID]}
    if out[i].ManualTags == nil {
        out[i].ManualTags = []model.UserTag{}
    }
}
c.JSON(http.StatusOK, out)
```

Replace the previous `c.JSON(http.StatusOK, articles)` (or equivalent) call. The handler currently returns `[]model.Article` directly; the wrapping changes the shape from `Article[]` to `Article[]` with an extra field — fully backward-compatible for clients that ignore the new field.

- [ ] **Step 6: Build and verify**

```bash
cd backend && go build ./...
```

Expected: exits 0.

- [ ] **Step 7: Smoke test with curl**

Start the stack: `docker-compose up -d --build api`. Then:

```bash
COOKIE='auth_token=authenticated'   # default dev auth
curl -s -b "$COOKIE" 'http://localhost:8080/api/articles?limit=1' | jq '.[0] | keys'
```

Expected: output includes `"manual_tags"` alongside the existing keys.

- [ ] **Step 8: Commit**

```bash
git add backend/internal/api/article.go backend/cmd/server/main.go
git commit -m "feat(api): include manual_tags in /api/articles response

Mirror the /api/saved batch-load pattern so list cards can render
the same tag chips users already attach via the article-detail TagBar.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 2: Refactor article filter SQL into a shared helper

**Goal:** Extract the WHERE/JOIN building from `repository/article.go::GetAll` into a helper so the new sidebar endpoint can apply the exact same filter and counts will match the article list.

**Files:**
- Modify: `backend/internal/repository/article.go`

- [ ] **Step 1: Read the current GetAll filter logic**

```bash
sed -n '102,150p' backend/internal/repository/article.go
```

Note the three current dimensions: `feedID *int`, `unreadOnly bool`, `savedOnly bool`. They build:
- A `LEFT JOIN reading_progress rp ON articles.id = rp.article_id AND rp.user_id = $1` (always-present)
- An optional `LEFT JOIN user_preferences up_save ...` when `savedOnly` is true
- WHERE fragments: `articles.feed_id = $N`, `COALESCE(rp.is_completed, false) = false`, `up_save.signal_value = $N`

- [ ] **Step 2: Add an `ArticleFilter` type and helper**

Add at the top of `backend/internal/repository/article.go` (after imports, before `ArticleRepository`):

```go
// ArticleFilter captures the dimensions used by both the article list
// query and the sidebar counts. Keeping these together guarantees the
// sidebar tag counts equal what the list returns under the same filter.
type ArticleFilter struct {
    UserID     int
    FeedID     *int
    UnreadOnly bool
    SavedOnly  bool
}

// buildArticleFilterSQL returns SQL fragments that callers splice into a
// query operating on the `articles` table (must be aliased as `articles`
// or `a` — the helper emits both forms via the alias arg).
// nextArg is the next positional placeholder index to start at, given
// that callers may have placed other args before the filter args.
func buildArticleFilterSQL(f ArticleFilter, alias string, nextArg int) (
    joinSQL string,
    whereFragments []string,
    args []any,
    finalArg int,
) {
    args = []any{}
    // Reading-progress join is always emitted because unread filter and
    // is_read field both need it. Caller already aliases reading-progress
    // result as `rp`.
    joinSQL = fmt.Sprintf(`
LEFT JOIN reading_progress rp ON %s.id = rp.article_id AND rp.user_id = $%d`, alias, nextArg)
    args = append(args, f.UserID)
    nextArg++

    if f.FeedID != nil {
        whereFragments = append(whereFragments, fmt.Sprintf("%s.feed_id = $%d", alias, nextArg))
        args = append(args, *f.FeedID)
        nextArg++
    }
    if f.UnreadOnly {
        whereFragments = append(whereFragments, "COALESCE(rp.is_completed, false) = false")
    }
    if f.SavedOnly {
        joinSQL += fmt.Sprintf(`
LEFT JOIN user_preferences up_save ON %s.id = up_save.article_id AND up_save.user_id = $%d AND up_save.signal_type = 'save'`, alias, nextArg-1) // reuse user_id arg
        // Note: we don't append user_id again because we already pushed it
        // for the rp join. But we DO need a literal 1.0 arg.
        whereFragments = append(whereFragments, fmt.Sprintf("up_save.signal_value = $%d", nextArg))
        args = append(args, 1.0)
        nextArg++
    }
    finalArg = nextArg
    return
}
```

**Important nuance:** the current `GetAll` reuses `$1` for the reading-progress `user_id` and then for the saved-feed join's `user_id`. The helper here threads `nextArg` carefully — it consumes one positional for the `reading_progress.user_id`, then if `SavedOnly` is true reuses the same value in the SQL string (no extra arg push) by writing `$<nextArg-1>`. This preserves the current arg layout. Tests below confirm correctness.

- [ ] **Step 3: Rewrite `GetAll` to use the helper**

Replace the body of `GetAll` so it calls the helper. The end result is:

```go
func (r *ArticleRepository) GetAll(limit, offset int, feedID *int, unreadOnly bool, savedOnly bool, userID int) ([]model.Article, error) {
    filter := ArticleFilter{
        UserID:     userID,
        FeedID:     feedID,
        UnreadOnly: unreadOnly,
        SavedOnly:  savedOnly,
    }
    joins, whereFrags, args, nextArg := buildArticleFilterSQL(filter, "articles", 1)

    query := `SELECT articles.id, articles.feed_id, articles.title, articles.url, articles.content, articles.published_at, articles.summary_brief, articles.summary_detailed, articles.fetched_at, articles.word_count, articles.reading_minutes, articles.media_url, articles.media_type, articles.media_duration_seconds, feeds.title as feed_title, COALESCE(rp.is_completed, false) as is_read, articles.links_extendable, articles.parent_article_id, articles.processing_state, articles.prerank_score, articles.editor_note
FROM articles
JOIN feeds ON articles.feed_id = feeds.id` + joins

    if len(whereFrags) > 0 {
        query += " WHERE " + whereFrags[0]
        for i := 1; i < len(whereFrags); i++ {
            query += " AND " + whereFrags[i]
        }
    }

    query += fmt.Sprintf(" ORDER BY DATE_TRUNC('day', GREATEST(COALESCE(articles.published_at, articles.fetched_at), articles.fetched_at - INTERVAL '7 days')) DESC, COALESCE(articles.published_at, articles.fetched_at) DESC LIMIT $%d OFFSET $%d", nextArg, nextArg+1)
    args = append(args, limit, offset)

    rows, err := r.db.Query(query, args...)
    if err != nil {
        return nil, err
    }
    defer rows.Close()
    return r.scanArticle(rows)
}
```

- [ ] **Step 4: Build to confirm refactor compiles**

```bash
cd backend && go build ./...
```

Expected: exits 0.

- [ ] **Step 5: Smoke test — list endpoint unchanged**

```bash
docker-compose up -d --build api
COOKIE='auth_token=authenticated'
curl -s -b "$COOKIE" 'http://localhost:8080/api/articles?limit=3' | jq 'length'
curl -s -b "$COOKIE" 'http://localhost:8080/api/articles?unread=true&limit=3' | jq 'length'
curl -s -b "$COOKIE" 'http://localhost:8080/api/articles?saved=true&limit=3' | jq 'length'
```

Expected: each command returns an integer (0 ≤ n ≤ 3). Combined filters return the same articles as before the refactor.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/repository/article.go
git commit -m "refactor(repo): extract article filter SQL into shared helper

Prep for the sidebar endpoint, which must apply the same filter as
the list endpoint to keep counts in sync.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 3: Add `tag_id` and `untagged` filters to `GetAll`

**Goal:** Two new optional dimensions on the article query, exposed via `/api/articles?tag_id=N` and `/api/articles?untagged=true`. Mutually exclusive.

**Files:**
- Modify: `backend/internal/repository/article.go` — extend `ArticleFilter` + `buildArticleFilterSQL`, and `GetAll` signature
- Modify: `backend/internal/api/article.go::GetAll` — parse query, validate exclusivity, pass through

- [ ] **Step 1: Extend `ArticleFilter` and `buildArticleFilterSQL`**

In `backend/internal/repository/article.go`, modify the `ArticleFilter` struct:

```go
type ArticleFilter struct {
    UserID     int
    FeedID     *int
    UnreadOnly bool
    SavedOnly  bool
    TagID      *int   // when non-nil, only articles bound to this tag by UserID
    Untagged   bool   // when true, only articles with zero manual tags by UserID
}
```

Inside `buildArticleFilterSQL`, after the existing branches and before `finalArg = nextArg`, add:

```go
if f.TagID != nil {
    whereFragments = append(whereFragments, fmt.Sprintf(
        `EXISTS (SELECT 1 FROM article_user_tags aut
                 WHERE aut.article_id = %s.id
                   AND aut.user_id = $%d
                   AND aut.tag_id = $%d)`, alias, nextArg, nextArg+1))
    args = append(args, f.UserID, *f.TagID)
    nextArg += 2
}
if f.Untagged {
    whereFragments = append(whereFragments, fmt.Sprintf(
        `NOT EXISTS (SELECT 1 FROM article_user_tags aut
                     WHERE aut.article_id = %s.id
                       AND aut.user_id = $%d)`, alias, nextArg))
    args = append(args, f.UserID)
    nextArg++
}
```

- [ ] **Step 2: Update `GetAll` signature**

Change the signature to accept the new fields. Replace:

```go
func (r *ArticleRepository) GetAll(limit, offset int, feedID *int, unreadOnly bool, savedOnly bool, userID int) ([]model.Article, error) {
```

with:

```go
func (r *ArticleRepository) GetAll(limit, offset int, feedID *int, unreadOnly bool, savedOnly bool, userID int, tagID *int, untagged bool) ([]model.Article, error) {
```

And populate the filter:

```go
filter := ArticleFilter{
    UserID:     userID,
    FeedID:     feedID,
    UnreadOnly: unreadOnly,
    SavedOnly:  savedOnly,
    TagID:      tagID,
    Untagged:   untagged,
}
```

- [ ] **Step 3: Find existing `GetAll` callers and update them**

```bash
grep -rn "\.GetAll(" backend/internal/ backend/cmd/
```

Each call site that doesn't filter by tag must pass `nil, false` as the new trailing args. Update them.

- [ ] **Step 4: Update the article handler to parse + validate the query**

In `backend/internal/api/article.go::GetAll` (before the call into the repo):

```go
var tagID *int
if s := c.Query("tag_id"); s != "" {
    n, err := strconv.Atoi(s)
    if err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": "tag_id must be an integer"})
        return
    }
    tagID = &n
}
untagged := c.Query("untagged") == "true"
if tagID != nil && untagged {
    c.JSON(http.StatusBadRequest, gin.H{"error": "tag_id and untagged cannot be combined"})
    return
}
```

Then thread `tagID, untagged` into the `repo.GetAll(...)` call.

If `strconv` is not already imported, add it.

- [ ] **Step 5: Build**

```bash
cd backend && go build ./...
```

Expected: exits 0.

- [ ] **Step 6: Smoke test the new filters**

```bash
docker-compose up -d --build api
COOKIE='auth_token=authenticated'
# Pick an existing tag id from the DB:
TAGID=$(curl -s -b "$COOKIE" 'http://localhost:8080/api/tags' | jq '.[0].id // empty')
echo "tag id: $TAGID"
# tag_id only:
curl -s -b "$COOKIE" "http://localhost:8080/api/articles?tag_id=${TAGID}&limit=5" | jq 'length, .[0].manual_tags'
# untagged only:
curl -s -b "$COOKIE" 'http://localhost:8080/api/articles?untagged=true&limit=5' | jq '.[0].manual_tags // []'
# Conflict:
curl -s -b "$COOKIE" 'http://localhost:8080/api/articles?tag_id=1&untagged=true' -o /dev/null -w '%{http_code}\n'
```

Expected:
- `tag_id` query: returned articles all have `manual_tags` containing the requested tag id
- `untagged=true` query: returned articles all have `manual_tags = []`
- Conflict query: HTTP `400`

If no tags exist yet, create one via `POST /api/tags` or via the UI, attach it to one article via the `TagBar`, then re-test.

- [ ] **Step 7: Commit**

```bash
git add backend/internal/repository/article.go backend/internal/api/article.go
git commit -m "feat(api): add tag_id and untagged filters to /api/articles

Mutually exclusive — both passed returns 400. Reuses the shared
ArticleFilter helper so future filter dimensions land in one place.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 4: Add `GetTagsForSidebar` repository method

**Goal:** A single repository method that returns `{tags:[{id,name,article_count}], total_count, untagged_count}` under the given filter, with parity to the list query.

**Files:**
- Modify: `backend/internal/repository/user_tag.go`

- [ ] **Step 1: Define return type at the top of `user_tag.go`**

Add (near the existing `EffectiveSource` block):

```go
// TagSidebarData is the response shape for GET /api/tags/sidebar.
type TagSidebarData struct {
    Tags           []model.UserTag `json:"tags"`            // article_count populated
    TotalCount     int             `json:"total_count"`     // articles under the filter (no tag scoping)
    UntaggedCount  int             `json:"untagged_count"`  // articles with zero manual tags
}
```

- [ ] **Step 2: Add the method**

Add to `UserTagRepository` in `backend/internal/repository/user_tag.go`:

```go
// GetTagsForSidebar returns tags with dynamic counts under the article
// filter, plus the matching total and untagged counts. Filter shape
// matches what /api/articles accepts (without TagID/Untagged — those
// would only filter on themselves).
func (r *UserTagRepository) GetTagsForSidebar(filter ArticleFilter) (*TagSidebarData, error) {
    // Tags + per-tag count
    joins, whereFrags, args, _ := buildArticleFilterSQL(filter, "a", 2) // $1 reserved for t.user_id
    tagsQuery := `
        SELECT t.id, t.user_id, t.name, t.created_at,
               COUNT(DISTINCT aut.article_id) AS article_count
        FROM user_tags t
        JOIN article_user_tags aut ON aut.tag_id = t.id AND aut.user_id = t.user_id
        JOIN articles a ON a.id = aut.article_id` + joins + `
        WHERE t.user_id = $1`
    for _, w := range whereFrags {
        tagsQuery += " AND " + w
    }
    tagsQuery += `
        GROUP BY t.id, t.user_id, t.name, t.created_at
        HAVING COUNT(DISTINCT aut.article_id) > 0
        ORDER BY t.name ASC`
    qargs := append([]any{filter.UserID}, args...)
    rows, err := r.db.Query(tagsQuery, qargs...)
    if err != nil {
        return nil, err
    }
    defer rows.Close()
    tags := []model.UserTag{}
    for rows.Next() {
        var t model.UserTag
        if err := rows.Scan(&t.ID, &t.UserID, &t.Name, &t.CreatedAt, &t.ArticleCount); err != nil {
            return nil, err
        }
        tags = append(tags, t)
    }
    if err := rows.Err(); err != nil {
        return nil, err
    }

    // total + untagged counts — use the same buildArticleFilterSQL output
    // but applied directly to a COUNT(*) on articles.
    joins2, where2, args2, _ := buildArticleFilterSQL(filter, "articles", 1)
    totalQuery := `SELECT COUNT(*) FROM articles` + joins2
    untaggedFrag := fmt.Sprintf(
        `NOT EXISTS (SELECT 1 FROM article_user_tags aut WHERE aut.article_id = articles.id AND aut.user_id = $%d)`,
        len(args2)+1)
    untaggedArgs := append([]any{}, args2...)
    untaggedArgs = append(untaggedArgs, filter.UserID)
    untaggedQuery := `SELECT COUNT(*) FROM articles` + joins2

    if len(where2) > 0 {
        clause := " WHERE " + where2[0]
        for i := 1; i < len(where2); i++ {
            clause += " AND " + where2[i]
        }
        totalQuery += clause
        untaggedQuery += clause + " AND " + untaggedFrag
    } else {
        untaggedQuery += " WHERE " + untaggedFrag
    }

    var total, untagged int
    if err := r.db.QueryRow(totalQuery, args2...).Scan(&total); err != nil {
        return nil, err
    }
    if err := r.db.QueryRow(untaggedQuery, untaggedArgs...).Scan(&untagged); err != nil {
        return nil, err
    }

    return &TagSidebarData{
        Tags:          tags,
        TotalCount:    total,
        UntaggedCount: untagged,
    }, nil
}
```

**Verify model.UserTag has an `ArticleCount` field** with `db` and `json` tags. If it doesn't, find its declaration (`grep "type UserTag struct" backend/internal/model/`) and add:

```go
ArticleCount int `json:"article_count" db:"article_count"`
```

The existing `GetTagsForUser` (line 62-…) already scans into this field, so it should be present. Double-check.

- [ ] **Step 3: Build**

```bash
cd backend && go build ./...
```

Expected: exits 0.

- [ ] **Step 4: Commit**

```bash
git add backend/internal/repository/user_tag.go backend/internal/model/*.go
git commit -m "feat(repo): add GetTagsForSidebar with dynamic per-tag counts

Reuses the shared article filter so sidebar counts agree with
/api/articles under the same scope. Returns tags, total_count, and
untagged_count in one call.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 5: Add `/api/tags/sidebar` HTTP endpoint

**Goal:** A new authenticated GET route that wraps `GetTagsForSidebar` and parses query params identical to `/api/articles` (minus `tag_id`/`untagged`).

**Files:**
- Modify: `backend/internal/api/user_tag.go` — add handler
- Modify: `backend/cmd/server/main.go` — register route

- [ ] **Step 1: Add the handler**

In `backend/internal/api/user_tag.go`, add (the file already has access to a `UserTagRepository`; if the handler struct field is called something else, adapt):

```go
// GetTagSidebar returns the user's tags + counts under the current
// article-list filter. Mirrors the query params of GET /api/articles
// minus the tag scoping itself.
func (h *UserTagHandler) GetTagSidebar(c *gin.Context) {
    userID := getUserID(c)

    var feedID *int
    if s := c.Query("feed_id"); s != "" {
        n, err := strconv.Atoi(s)
        if err != nil {
            c.JSON(http.StatusBadRequest, gin.H{"error": "feed_id must be an integer"})
            return
        }
        feedID = &n
    }
    filter := repository.ArticleFilter{
        UserID:     userID,
        FeedID:     feedID,
        UnreadOnly: c.Query("unread") == "true",
        SavedOnly:  c.Query("saved") == "true",
    }
    data, err := h.tagRepo.GetTagsForSidebar(filter)
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
        return
    }
    c.JSON(http.StatusOK, data)
}
```

If the handler struct's tag-repo field is not named `tagRepo`, find it: `grep "UserTagRepository" backend/internal/api/user_tag.go` — and use the existing name.

Ensure `strconv` and the `repository` package are imported.

- [ ] **Step 2: Register the route**

In `backend/cmd/server/main.go`, near the existing `/api/tags` routes (around line 128-131), add:

```go
apiGroup.GET("/tags/sidebar", userTagHandler.GetTagSidebar)
```

**Position matters in Gin**: `/tags/sidebar` must be registered before any `/tags/:id` wildcard pattern, otherwise Gin matches `:id=sidebar`. Check existing order — `/tags/:id` PATCH/DELETE may exist. Insert `/tags/sidebar` GET above them.

- [ ] **Step 3: Build**

```bash
cd backend && go build ./...
```

Expected: exits 0.

- [ ] **Step 4: Smoke test**

```bash
docker-compose up -d --build api
COOKIE='auth_token=authenticated'
curl -s -b "$COOKIE" 'http://localhost:8080/api/tags/sidebar' | jq '{n_tags: (.tags|length), total: .total_count, untagged: .untagged_count}'
# Scope to one feed:
FEED_ID=$(curl -s -b "$COOKIE" 'http://localhost:8080/api/feeds' | jq '.[0].id')
curl -s -b "$COOKIE" "http://localhost:8080/api/tags/sidebar?feed_id=${FEED_ID}" | jq '{n_tags: (.tags|length), total: .total_count, untagged: .untagged_count}'
# Counts cross-check:
LIST_COUNT=$(curl -s -b "$COOKIE" "http://localhost:8080/api/articles?feed_id=${FEED_ID}&limit=10000" | jq 'length')
TOTAL=$(curl -s -b "$COOKIE" "http://localhost:8080/api/tags/sidebar?feed_id=${FEED_ID}" | jq '.total_count')
echo "list=$LIST_COUNT sidebar.total=$TOTAL  (should match)"
```

Expected: the `total_count` from the sidebar equals the length of the unfiltered list under the same feed.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/api/user_tag.go backend/cmd/server/main.go
git commit -m "feat(api): GET /api/tags/sidebar with dynamic counts

Mirrors the article-list filter params so the new universal tag
sidebar can render counts that match the list it filters.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 6: Frontend API client — `manual_tags`, `tag_id`/`untagged`, sidebar endpoint

**Goal:** TypeScript types and helpers for the new server contract.

**Files:**
- Modify: `frontend/src/api/client.ts`

- [ ] **Step 1: Add `manual_tags` to the `Article` interface**

```bash
grep -n "export interface Article" frontend/src/api/client.ts
```

Add a field:

```ts
export interface Article {
  // ...existing fields...
  manual_tags: UserTag[]
}
```

Place the field near the bottom of the interface. The `UserTag` type is already declared further down — TypeScript hoists declarations within a file, so order doesn't matter; if the linter complains about forward reference, move `UserTag` up above `Article` (it's a leaf type with no dependencies).

- [ ] **Step 2: Extend `getArticles` params**

Replace the existing `getArticles` declaration (around `client.ts:263`):

```ts
export const getArticles = (params?: {
  feed_id?: number
  unread?: boolean
  saved?: boolean
  tag_id?: number
  untagged?: boolean
  limit?: number
  offset?: number
}) => api.get<Article[]>('/articles', { params }).then(res => res.data)
```

- [ ] **Step 3: Add the sidebar endpoint helper**

Append to the `=== Tags ===` section (near `getArticleTags`):

```ts
export interface TagSidebarData {
  tags: UserTag[]
  total_count: number
  untagged_count: number
}

export const getTagSidebar = (params?: {
  feed_id?: number
  unread?: boolean
  saved?: boolean
}) => api.get<TagSidebarData>('/tags/sidebar', { params }).then(r => r.data)
```

- [ ] **Step 4: Type-check + build**

```bash
cd frontend && npm run build
```

Expected: clean build. If there are pre-existing TS errors unrelated to this change, do not fix them — they're out of scope.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/api/client.ts
git commit -m "feat(api-client): types and helpers for tag sidebar + filters

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 7: New `TagSidebar` component (non-saved feeds)

**Goal:** A compact sidebar listing 全部 / 无 tag / per-tag rows. Single-select. No Sources section.

**Files:**
- Create: `frontend/src/components/TagSidebar.tsx`

- [ ] **Step 1: Read the existing `SavedTagSidebar` for style + structure**

```bash
sed -n '1,114p' frontend/src/components/SavedTagSidebar.tsx
```

Note the use of `.saved-row`, `.saved-section-title`, `.saved-row-label`, `.saved-row-count`, `.active` classes. Reuse them so the new sidebar inherits the same visuals.

- [ ] **Step 2: Create the file**

`frontend/src/components/TagSidebar.tsx`:

```tsx
import { TagSidebarData } from '../api/client'

export type TagFilter =
  | { kind: 'all' }
  | { kind: 'untagged' }
  | { kind: 'tag'; id: number }

interface Props {
  data: TagSidebarData
  selection: TagFilter
  onSelect: (sel: TagFilter) => void
}

export default function TagSidebar({ data, selection, onSelect }: Props) {
  return (
    <aside
      style={{
        width: 220,
        flexShrink: 0,
        borderRight: '1px solid var(--border)',
        padding: 12,
        overflowY: 'auto',
      }}
    >
      <div>
        <button
          type="button"
          className={'saved-row' + (selection.kind === 'all' ? ' active' : '')}
          onClick={() => onSelect({ kind: 'all' })}
        >
          <span className="saved-row-label">全部</span>
          <span className="saved-row-count">{data.total_count}</span>
        </button>
        <button
          type="button"
          className={'saved-row' + (selection.kind === 'untagged' ? ' active' : '')}
          onClick={() => onSelect({ kind: 'untagged' })}
        >
          <span className="saved-row-label">(无 tag)</span>
          <span className="saved-row-count">{data.untagged_count}</span>
        </button>
      </div>

      <div style={{ marginTop: 12 }}>
        <div className="saved-section-title">Tags</div>
        {data.tags.length === 0 ? (
          <div className="text-muted text-sm" style={{ padding: '4px 8px' }}>
            暂无 tag
          </div>
        ) : (
          data.tags.map(t => {
            const active = selection.kind === 'tag' && selection.id === t.id
            return (
              <button
                key={t.id}
                type="button"
                className={'saved-row' + (active ? ' active' : '')}
                onClick={() => onSelect({ kind: 'tag', id: t.id })}
              >
                <span className="saved-row-label">{t.name}</span>
                <span className="saved-row-count">{t.article_count}</span>
              </button>
            )
          })
        )}
      </div>
    </aside>
  )
}
```

- [ ] **Step 3: Type-check + build**

```bash
cd frontend && npm run build
```

Expected: clean build.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/components/TagSidebar.tsx
git commit -m "feat(components): add TagSidebar (tags-only, no Sources)

Sibling to SavedTagSidebar for use outside 网摘. Reuses .saved-row
styling so the visuals stay consistent.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 8: New `SidebarToggleButton` component

**Goal:** A small button in the page header that flips the sidebar open/closed state. Icon: a square-with-a-bar SVG (sidebar shape). Keyboard shortcut `t` (when no input is focused).

**Files:**
- Create: `frontend/src/components/SidebarToggleButton.tsx`

- [ ] **Step 1: Create the file**

`frontend/src/components/SidebarToggleButton.tsx`:

```tsx
interface Props {
  open: boolean
  onToggle: () => void
}

export default function SidebarToggleButton({ open, onToggle }: Props) {
  return (
    <button
      type="button"
      className="btn-ghost"
      onClick={onToggle}
      title={open ? '收起侧栏 (T)' : '展开侧栏 (T)'}
      aria-label={open ? '收起侧栏' : '展开侧栏'}
      aria-pressed={open}
      style={{ padding: '4px 8px' }}
    >
      <svg
        width="18"
        height="18"
        viewBox="0 0 18 18"
        fill="none"
        stroke="currentColor"
        strokeWidth="1.5"
      >
        <rect x="2" y="2.5" width="14" height="13" rx="2" />
        <line x1="7" y1="2.5" x2="7" y2="15.5" />
        {open && <line x1="3.5" y1="6" x2="5.5" y2="6" />}
        {open && <line x1="3.5" y1="9" x2="5.5" y2="9" />}
      </svg>
    </button>
  )
}
```

- [ ] **Step 2: Type-check + build**

```bash
cd frontend && npm run build
```

Expected: clean build.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/SidebarToggleButton.tsx
git commit -m "feat(components): SidebarToggleButton for tag sidebar

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 9: Wire sidebar into `ArticleListPage` (state + data fetch + filter)

**Goal:** Add the open/closed state, the tag filter state, the sidebar-data fetch, and pass the new filter into `getArticles`. Mount the sidebar conditionally. No layout changes to the main area yet (next task) — but the cards now show `manual_tags`.

**Files:**
- Modify: `frontend/src/pages/ArticleListPage.tsx`

- [ ] **Step 1: Add imports at the top of the file**

```ts
import TagSidebar, { TagFilter } from '../components/TagSidebar'
import SidebarToggleButton from '../components/SidebarToggleButton'
import { getTagSidebar, TagSidebarData } from '../api/client'
```

- [ ] **Step 2: Add the new state hooks**

Place near the other `useState` calls (after `sessionReadIds` is fine):

```ts
const [sidebarOpen, setSidebarOpen] = useState<boolean>(() => {
  try { return localStorage.getItem('tagSidebarOpen') === 'true' } catch { return false }
})
const [tagFilter, setTagFilter] = useState<TagFilter>(() => {
  try {
    const raw = sessionStorage.getItem('articleTagFilter')
    return raw ? JSON.parse(raw) as TagFilter : { kind: 'all' }
  } catch { return { kind: 'all' } }
})
const [tagSidebarData, setTagSidebarData] = useState<TagSidebarData | null>(null)
```

Add a small helper for sidebar toggle:

```ts
const toggleSidebar = () => {
  setSidebarOpen(o => {
    const next = !o
    try { localStorage.setItem('tagSidebarOpen', String(next)) } catch {}
    return next
  })
}
```

And a helper for tag selection (handles grouped exclusion — Q10):

```ts
const selectTag = (sel: TagFilter) => {
  if (sel.kind !== 'all' && grouped) setGrouped(false)
  setTagFilter(sel)
  try { sessionStorage.setItem('articleTagFilter', JSON.stringify(sel)) } catch {}
}
```

- [ ] **Step 3: Add the sidebar data fetch effect**

After the existing `loadArticles` effect block, add:

```ts
useEffect(() => {
  if (!sidebarOpen || isClippingMode) return
  getTagSidebar({
    feed_id: selectedFeed || undefined,
    unread: unreadOnly || undefined,
    saved: savedOnly || undefined,
  }).then(setTagSidebarData).catch(() => setTagSidebarData(null))
}, [sidebarOpen, isClippingMode, selectedFeed, unreadOnly, savedOnly])
```

- [ ] **Step 4: Thread the tag filter into `loadArticles`**

Locate the existing call (around line 236):

```ts
const raw = await getArticles({
  feed_id: selectedFeed || undefined,
  unread: unreadOnly || undefined,
  saved: savedOnly || undefined,
  limit: PAGE_SIZE,
  offset: off,
})
```

Replace with:

```ts
const raw = await getArticles({
  feed_id: selectedFeed || undefined,
  unread: unreadOnly || undefined,
  saved: savedOnly || undefined,
  tag_id: tagFilter.kind === 'tag' ? tagFilter.id : undefined,
  untagged: tagFilter.kind === 'untagged' || undefined,
  limit: PAGE_SIZE,
  offset: off,
})
```

Add `tagFilter` to the `useCallback` dependency array of `loadArticles` (it's likely on the same line as `selectedFeed, unreadOnly, savedOnly, isClippingMode, grouped`):

```ts
}, [selectedFeed, unreadOnly, savedOnly, isClippingMode, grouped, tagFilter])
```

- [ ] **Step 5: Update the `ArticleCard` `manualTags` prop**

Find the existing line (around `ArticleListPage.tsx:673`):

```tsx
manualTags={[]}
```

Replace with:

```tsx
manualTags={article.manual_tags || []}
```

- [ ] **Step 6: Hide the 📚 分组 button when a tag filter is active (Q10)**

Locate the existing `📚 分组` button (around line 506). Its conditional currently looks like `{!isClippingMode && !searchQuery && (...)}`. Update:

```tsx
{!isClippingMode && !searchQuery && tagFilter.kind === 'all' && (
  <button
    type="button"
    className={grouped ? '' : 'btn-ghost'}
    onClick={() => {
      const next = !grouped
      setGrouped(next)
      try { sessionStorage.setItem('articlesGrouped', String(next)) } catch {}
    }}
    title={grouped ? '回到列表视图' : '按主题分组查看'}
  >
    📚 分组
  </button>
)}
```

Also update the grouped-view branch (around line 629) to fall through to flat list when a tag is selected. Find:

```tsx
) : !isClippingMode && !searchQuery && !grouped && (
```

Earlier (around 644 `!isClippingMode && !searchQuery && loading`) the branch order is fine; the safety net is `selectTag` already sets grouped=false. Just ensure the grouped branch (`!isClippingMode && !searchQuery && grouped`) also gates on `tagFilter.kind === 'all'`:

```tsx
{!isClippingMode && !searchQuery && grouped && tagFilter.kind === 'all' ? (
```

- [ ] **Step 7: Type-check + build**

```bash
cd frontend && npm run build
```

Expected: clean build.

- [ ] **Step 8: Commit**

```bash
git add frontend/src/pages/ArticleListPage.tsx
git commit -m "feat(articles): wire tag sidebar state and filters in list page

State: sidebarOpen (localStorage), tagFilter (sessionStorage).
loadArticles passes tag_id/untagged; cards display manual_tags; 📚
grouping is hidden while a tag filter is active.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 10: Mount the sidebar and toggle button in the page layout

**Goal:** Actually render the `TagSidebar` and `SidebarToggleButton` so the user sees them. Adjust the outer layout to be a flex row when the sidebar is open.

**Files:**
- Modify: `frontend/src/pages/ArticleListPage.tsx`

- [ ] **Step 1: Locate the page header and outer container**

```bash
grep -n "<h2>{isClippingMode\|return (" frontend/src/pages/ArticleListPage.tsx | head
```

Identify the JSX return root and the toolbar `<div>` containing the `<h2>`.

- [ ] **Step 2: Add the toggle button before the `<h2>`**

Find the line:

```tsx
<h2>{isClippingMode ? '网摘' : '文章列表'}</h2>
```

Wrap it with a flex container that also holds the toggle button:

```tsx
<div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
  <SidebarToggleButton open={sidebarOpen} onToggle={toggleSidebar} />
  <h2 style={{ margin: 0 }}>{isClippingMode ? '网摘' : '文章列表'}</h2>
</div>
```

- [ ] **Step 3: Wrap the whole page in a flex row when sidebar is open and non-clipping**

The clipping-mode branch already renders `<SavedPage>` which owns its own layout — handled in Task 11. Here we add the sidebar mount for the non-clipping case.

Find the JSX return root. It looks roughly like:

```tsx
return (
  <div>
    <div className="page-header">...</div>
    {/* toolbar */}
    {/* main content / cards */}
  </div>
)
```

Restructure to:

```tsx
return (
  <div style={{ display: 'flex', minHeight: '100vh' }}>
    {sidebarOpen && !isClippingMode && tagSidebarData && (
      <TagSidebar data={tagSidebarData} selection={tagFilter} onSelect={selectTag} />
    )}
    <div style={{ flex: 1, minWidth: 0 }}>
      {/* everything that was previously the page body — header, toolbar, cards */}
    </div>
  </div>
)
```

The wrapper `<div style={{ flex: 1, minWidth: 0 }}>` preserves the current main column behavior (the `minWidth: 0` is necessary so flex children don't blow out horizontally).

- [ ] **Step 4: Type-check + build**

```bash
cd frontend && npm run build
```

Expected: clean build.

- [ ] **Step 5: Rebuild Docker frontend and smoke-test**

```bash
docker-compose up -d --build frontend
```

Open `http://localhost:8080/articles` in a browser. Verify:

- Toggle button appears top-left next to "文章列表"
- Click toggle → sidebar slides in on the left with 全部 / 无 tag / Tags
- Counts in sidebar match the article list under the same feed/unread/saved filter
- Click "无 tag" → list filters to articles with no manual chips on their cards
- Click a tag → list filters to that tag, cards show the chip
- Card chips render `manual_tags` (try on a regular feed where you've added a tag via the article-detail page)
- Refresh the page: sidebar state persists; tag filter clears (sessionStorage scoped)

- [ ] **Step 6: Commit**

```bash
git add frontend/src/pages/ArticleListPage.tsx
git commit -m "feat(articles): render tag sidebar and toggle button

Flex-row layout when sidebar is open; toggle persists to
localStorage. Saved mode handled separately.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 11: Plumb `sidebarOpen` into `SavedPage` for 网摘 consistency

**Goal:** When the user is on the 网摘 feed (clipping mode), the **same** toggle button hides/shows the existing `SavedTagSidebar`. `SavedPage` standalone behavior on `/saved` is unchanged.

**Files:**
- Modify: `frontend/src/pages/SavedPage.tsx`
- Modify: `frontend/src/pages/ArticleListPage.tsx`

- [ ] **Step 1: Add an optional prop to `SavedPage`**

```bash
grep -n "interface SavedPageProps\|export default function SavedPage\|restrictToFeedId" frontend/src/pages/SavedPage.tsx
```

Locate the props interface and the `SavedTagSidebar` mount (around line 223). Add a new optional prop:

```ts
interface SavedPageProps {
  restrictToFeedId?: number
  entryPath?: string
  sidebarOpen?: boolean   // default undefined → behaves as always-on (standalone /saved)
}
```

In the function body, gate the sidebar mount on the prop:

```tsx
{(props.sidebarOpen ?? true) && (
  <SavedTagSidebar
    tags={tags}
    sources={sources}
    selection={selection}
    onSelect={onSelect}
  />
)}
```

(Adapt to the existing prop destructuring style — if the component already destructures props at the top, add `sidebarOpen` there and reference it directly.)

- [ ] **Step 2: Pass `sidebarOpen` from `ArticleListPage`**

Locate the `<SavedPage>` embed (around `ArticleListPage.tsx:531`):

```tsx
{isClippingMode && selectedFeed != null && (
  <SavedPage restrictToFeedId={selectedFeed} entryPath="/articles" />
)}
```

Update to:

```tsx
{isClippingMode && selectedFeed != null && (
  <SavedPage
    restrictToFeedId={selectedFeed}
    entryPath="/articles"
    sidebarOpen={sidebarOpen}
  />
)}
```

- [ ] **Step 3: Type-check + build**

```bash
cd frontend && npm run build
```

Expected: clean build.

- [ ] **Step 4: Rebuild and smoke-test 网摘**

```bash
docker-compose up -d --build frontend
```

- Navigate to `/articles`, pick the ⭐ 网摘 feed
- Toggle button hides/shows the SavedTagSidebar
- Standalone `/saved` route still shows the sidebar by default (unchanged)

- [ ] **Step 5: Commit**

```bash
git add frontend/src/pages/SavedPage.tsx frontend/src/pages/ArticleListPage.tsx
git commit -m "feat(saved): respect parent sidebarOpen when embedded

Standalone /saved keeps always-on sidebar; embedded clipping mode
follows the universal toggle.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 12: Optional keyboard shortcut and final polish

**Goal:** Bind `t` to toggle the sidebar (when no input is focused), and verify the spec's test plan end-to-end.

**Files:**
- Modify: `frontend/src/pages/ArticleListPage.tsx`

- [ ] **Step 1: Add the `t` shortcut**

Find the existing global key handler (around line 377 — it already early-returns for INPUT/TEXTAREA/SELECT focus):

```bash
grep -n "tagName)?.toUpperCase()\|case 'j'\|case 'k'" frontend/src/pages/ArticleListPage.tsx
```

Locate the switch/if branch. Add a case for `t`:

```ts
case 't':
case 'T':
  e.preventDefault()
  toggleSidebar()
  return
```

If the handler is inside a `useEffect`, ensure `toggleSidebar` is in the dep array, or factor it out so the closure stays valid.

- [ ] **Step 2: Type-check + build**

```bash
cd frontend && npm run build
```

Expected: clean build.

- [ ] **Step 3: Final end-to-end smoke**

```bash
docker-compose up -d --build frontend
```

Walk through the full test plan from the spec (`docs/superpowers/specs/2026-05-13-universal-tag-sidebar-design.md` § Testing → Frontend):

1. Toggle button flips sidebar; persists across reload
2. "无 tag" filters to untagged articles; chips absent on those cards
3. Click a tag row → list filters; chip appears on cards
4. Change feed dropdown while tag selected → AND combination, counts update
5. Open 📚 grouping, then click a tag → grouping turns off; 📚 button hidden while tag active
6. 网摘 feed → Sources section still visible in its sidebar; same toggle hides it
7. Add a tag via TagBar on an article, return to list → chip on its card

If anything fails, fix and re-test before committing.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/pages/ArticleListPage.tsx
git commit -m "feat(articles): keyboard shortcut T to toggle tag sidebar

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 13: Push and update PR

**Goal:** Push all implementation commits to the existing feature branch so PR #23 reflects the completed work.

- [ ] **Step 1: Verify clean state**

```bash
git status
git log --oneline origin/feature/universal-tag-sidebar..HEAD
```

Expected: working tree clean; the local branch is ahead of origin by ~12 commits (one per task).

- [ ] **Step 2: Push**

```bash
git push origin feature/universal-tag-sidebar
```

- [ ] **Step 3: Verify PR**

```bash
gh pr view 23
```

Expected: status shows updated commit count. Confirm the test plan checkboxes in the PR body still apply.

---

## Self-Review Notes

**Spec coverage check:**
- Q1 collapsible sidebar with toggle button → Tasks 8, 10
- Q2 Tags-only sidebar on non-saved → Task 7 (no Sources branch)
- Q3 AND combination → Task 9 (loadArticles passes both feed and tag)
- Q4 single-select → Task 7 (`TagFilter` union type, no array)
- Q5 网摘 sidebar collapsible too → Task 11
- Q6 cards display manual_tags → Tasks 1 (backend), 9 (frontend)
- Q7 dynamic counts → Task 4 (sidebar repo) + Task 9 (refetch on filter change)
- Q8 new `/api/tags/sidebar` endpoint → Tasks 4, 5
- Q9 sessionStorage for tag filter → Task 9
- Q10 grouped ⊥ tag → Task 9 (Step 6) + Task 7 (selectTag auto-disable)

**Type consistency:** `ArticleFilter` is shared between repo packages — confirmed defined once in `article.go` and consumed by `user_tag.go` via the package-level type. `TagFilter` (frontend) is exported from `TagSidebar.tsx` and imported in `ArticleListPage.tsx`.

**Risks / things to watch during execution:**
- `buildArticleFilterSQL` arg threading in Task 2 — the `nextArg` accounting around the saved-feed branch reuses an existing positional. The smoke test (Task 2 Step 5) exercises all three filter combinations; if any returns different rows than before, the helper has a bug.
- Gin route order in Task 5 — `/tags/sidebar` must be registered before any `/tags/:id` matcher.
- The grouped-view conditional in Task 9 Step 6 has several branches in the existing code (`!isClippingMode && !searchQuery && grouped`); ensure the `tagFilter.kind === 'all'` gate is added consistently or grouping reappears when it shouldn't.
