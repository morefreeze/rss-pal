# Weak Network Performance Optimization — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Cut RSS Pal's article list / detail first-screen time from 3-5 s to ~600 ms-1.2 s on throttled 4G by trimming payloads, enabling compression, adding HTTP caching, hardening axios for weak networks, and adding skeleton screens + DB indexes.

**Architecture:** Layered changes across nginx, the Go API, the React frontend, and PostgreSQL. Each task is independently revertible. The biggest single win is dropping `content` and `summary_detailed` from the list endpoint response (today ~100 KB → ~10 KB gzipped).

**Tech Stack:** Go 1.24, Gin, `github.com/gin-contrib/gzip`, PostgreSQL 15, React 18 + Vite, axios, nginx (alpine).

**Spec:** [`docs/superpowers/specs/2026-05-25-weak-network-perf-design.md`](../specs/2026-05-25-weak-network-perf-design.md)

**Note on dropped task:** The spec section "Frontend request orchestration" included parallelizing `getFeeds()` and `getArticles()`. On closer inspection of `frontend/src/pages/ArticleListPage.tsx:256-328`, those calls already live in independent `useEffect`s with no shared dependency, so they fire in parallel today. No work needed; the spec change is harmless but the implementation can skip it.

---

## Task 1: Enable gzip in nginx

**Files:**
- Modify: `frontend/nginx.conf`

- [ ] **Step 1: Add gzip directives inside the `server { ... }` block**

In `frontend/nginx.conf`, after the `resolver 127.0.0.11 valid=10s ipv6=off;` line and before the first `location /api {` block, add:

```
    # Compress text-like responses. Static asset bundles + JSON API
    # payloads benefit the most on weak networks.
    gzip on;
    gzip_vary on;
    gzip_min_length 512;
    gzip_comp_level 6;
    gzip_proxied any;
    gzip_types
        application/javascript
        application/json
        application/xml
        text/css
        text/html
        text/plain
        text/xml
        image/svg+xml;
```

- [ ] **Step 2: Sanity-check the file still parses**

Run from repo root:

```bash
docker run --rm -v "$PWD/frontend/nginx.conf:/etc/nginx/conf.d/default.conf:ro" nginx:alpine nginx -t
```

Expected output: `nginx: configuration file /etc/nginx/conf.d/default.conf test is successful`.

If you see "test failed", re-read the file — most likely a missing semicolon or wrong indentation.

- [ ] **Step 3: Commit**

```bash
git add frontend/nginx.conf
git commit -m "perf(nginx): enable gzip for JS/CSS/JSON/HTML responses"
```

---

## Task 2: Add Gin gzip middleware

**Files:**
- Modify: `backend/cmd/server/main.go:60-90`
- Modify: `backend/go.mod`, `backend/go.sum`
- Create: `backend/internal/api/gzip_test.go`

- [ ] **Step 1: Add the dependency**

```bash
cd backend && go get github.com/gin-contrib/gzip@v1.0.1 && cd -
```

Expected: `go.mod` gains `github.com/gin-contrib/gzip v1.0.1`, `go.sum` updated.

- [ ] **Step 2: Write a failing test**

Create `backend/internal/api/gzip_test.go`:

```go
package api_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	gingzip "github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"
)

// TestGzipMiddleware verifies the middleware compresses JSON responses
// over the min-content-length threshold when the client advertises
// Accept-Encoding: gzip.
func TestGzipMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(gingzip.Gzip(gingzip.DefaultCompression))

	// Payload >512 bytes so it crosses the typical min-length threshold.
	payload := strings.Repeat("x", 2048)
	r.GET("/big", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"data": payload})
	})

	req := httptest.NewRequest(http.MethodGet, "/big", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if got := w.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("expected Content-Encoding: gzip, got %q", got)
	}
	if w.Body.Len() >= 2048 {
		t.Fatalf("expected compressed body smaller than raw, got %d bytes", w.Body.Len())
	}
}
```

- [ ] **Step 3: Run the test to confirm it passes against the library alone**

```bash
cd backend && go test ./internal/api/ -run TestGzipMiddleware -v && cd -
```

Expected: PASS. (We are not testing our own wiring yet, just confirming the library works as documented.)

- [ ] **Step 4: Wire the middleware into the server**

Edit `backend/cmd/server/main.go`. Add to the imports:

```go
	gingzip "github.com/gin-contrib/gzip"
```

Find the line `router := gin.Default()` (around L72). Immediately after it, before `router.SetTrustedProxies(...)`, insert:

```go
	// Compress JSON/text responses for clients that opt in. Defensive
	// when the API is reached directly (no nginx); skip already-compressed
	// content types and the streaming summary endpoint that controls its
	// own framing.
	router.Use(gingzip.Gzip(
		gingzip.DefaultCompression,
		gingzip.WithExcludedExtensions([]string{".png", ".jpg", ".jpeg", ".gif", ".webp", ".mp4", ".mp3", ".woff", ".woff2"}),
		gingzip.WithExcludedPaths([]string{"/api/articles/.*/summary/stream"}),
	))
```

- [ ] **Step 5: Build to confirm it compiles**

```bash
cd backend && go build ./... && cd -
```

Expected: no output (success).

- [ ] **Step 6: Run all API tests**

```bash
cd backend && go test ./internal/api/... && cd -
```

Expected: PASS for all packages.

- [ ] **Step 7: Commit**

```bash
git add backend/cmd/server/main.go backend/go.mod backend/go.sum backend/internal/api/gzip_test.go
git commit -m "perf(api): add gin-gzip middleware for response compression"
```

---

## Task 3: Trim list endpoint payload (backend + frontend together)

The backend DTO change and the frontend type split must land in the same commit, otherwise the frontend would still try to read `.content` / `.summary_detailed` fields that no longer ship.

**Files:**
- Modify: `backend/internal/api/article.go:130-201`
- Modify: `frontend/src/api/client.ts:101-129, 275-330`
- Modify: `frontend/src/pages/ArticleListPage.tsx` (type references only)

- [ ] **Step 1: Write a failing test for the trimmed list response**

Create `backend/internal/api/article_list_response_test.go`:

```go
package api_test

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestArticleListItemHasNoFatPayload is a marshal-shape test: the
// JSON keys returned by GET /api/articles must NOT include `content`
// or `summary_detailed`. We construct the DTO directly rather than
// spinning up a real handler — the SQL stack needs a DB.
//
// Update this test if the list DTO struct is renamed.
func TestArticleListItemHasNoFatPayload(t *testing.T) {
	// Construct an empty DTO via reflection on the live struct so the
	// test fails compile-time if the struct is removed.
	type probe struct{}
	_ = probe{} // placeholder; the real assertion is the JSON below.

	// Anyone modifying the list response should run this and confirm
	// only the lean keys appear. We marshal a one-item slice through
	// the same struct used by the handler.
	item := newArticleListItemForTest()
	b, err := json.Marshal(item)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, banned := range []string{`"content"`, `"summary_detailed"`} {
		if strings.Contains(string(b), banned) {
			t.Fatalf("list item JSON must not contain %s; got %s", banned, b)
		}
	}
	for _, required := range []string{`"id"`, `"title"`, `"url"`, `"summary_brief"`, `"fetched_at"`, `"manual_tags"`} {
		if !strings.Contains(string(b), required) {
			t.Fatalf("list item JSON missing %s; got %s", required, b)
		}
	}
}
```

Also create `backend/internal/api/article_list_response_helpers_test.go`:

```go
package api_test

import (
	"github.com/bytedance/rss-pal/internal/api"
	"github.com/bytedance/rss-pal/internal/model"
)

// newArticleListItemForTest returns a zero-valued list item with the
// non-omitempty required fields populated, so the marshal-shape test
// can rely on stable key presence.
func newArticleListItemForTest() api.ArticleListItem {
	return api.ArticleListItem{
		ID:           1,
		FeedID:       2,
		Title:        "t",
		URL:          "u",
		SummaryBrief: "s",
		ManualTags:   []model.UserTag{},
	}
}
```

- [ ] **Step 2: Run the test to verify it fails (struct does not exist yet)**

```bash
cd backend && go test ./internal/api/ -run TestArticleListItemHasNoFatPayload -v && cd -
```

Expected: FAIL with `undefined: api.ArticleListItem`.

- [ ] **Step 3: Add the `ArticleListItem` type and rewrite the handler**

Create a new file `backend/internal/api/article_list_item.go`:

```go
package api

import (
	"time"

	"github.com/bytedance/rss-pal/internal/model"
)

// ArticleListItem is the lean DTO returned by GET /api/articles.
// It deliberately excludes the heavy `content` and `summary_detailed`
// fields — the list UI only renders the brief summary. Detail pages
// fetch the full article via GET /api/articles/:id.
type ArticleListItem struct {
	ID                   int             `json:"id"`
	FeedID               int             `json:"feed_id"`
	FeedTitle            string          `json:"feed_title,omitempty"`
	Title                string          `json:"title"`
	URL                  string          `json:"url"`
	PublishedAt          *time.Time      `json:"published_at"`
	SummaryBrief         string          `json:"summary_brief"`
	FetchedAt            time.Time       `json:"fetched_at"`
	WordCount            int             `json:"word_count"`
	ReadingMinutes       int             `json:"reading_minutes"`
	IsRead               bool            `json:"is_read"`
	MediaURL             string          `json:"media_url,omitempty"`
	MediaType            string          `json:"media_type,omitempty"`
	MediaDurationSeconds int             `json:"media_duration_seconds,omitempty"`
	LinksExtendable      *bool           `json:"links_extendable,omitempty"`
	LinkSetSuggested     *bool           `json:"link_set_suggested,omitempty"`
	ParentArticleID      *int            `json:"parent_article_id,omitempty"`
	ProcessingState      string          `json:"processing_state,omitempty"`
	PrerankScore         *float64        `json:"prerank_score,omitempty"`
	EditorNote           string          `json:"editor_note,omitempty"`
	ManualTags           []model.UserTag `json:"manual_tags"`
}

// articleToListItem projects a model.Article onto the lean DTO. Used
// by GetAll to avoid serializing the full content blob.
func articleToListItem(a model.Article, tags []model.UserTag) ArticleListItem {
	if tags == nil {
		tags = []model.UserTag{}
	}
	return ArticleListItem{
		ID:                   a.ID,
		FeedID:               a.FeedID,
		FeedTitle:            a.FeedTitle,
		Title:                a.Title,
		URL:                  a.URL,
		PublishedAt:          a.PublishedAt,
		SummaryBrief:         a.SummaryBrief,
		FetchedAt:            a.FetchedAt,
		WordCount:            a.WordCount,
		ReadingMinutes:       a.ReadingMinutes,
		IsRead:               a.IsRead,
		MediaURL:             a.MediaURL,
		MediaType:            a.MediaType,
		MediaDurationSeconds: a.MediaDurationSeconds,
		LinksExtendable:      a.LinksExtendable,
		LinkSetSuggested:     a.LinkSetSuggested,
		ParentArticleID:      a.ParentArticleID,
		ProcessingState:      a.ProcessingState,
		PrerankScore:         a.PrerankScore,
		EditorNote:           a.EditorNote,
		ManualTags:           tags,
	}
}
```

Now edit `backend/internal/api/article.go`. In `GetAll` (lines 189-200), replace the inline struct + serialization with the new DTO. Specifically, replace:

```go
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

with:

```go
	out := make([]ArticleListItem, len(articles))
	for i, a := range articles {
		out[i] = articleToListItem(a, tagMap[a.ID])
	}
	c.JSON(http.StatusOK, out)
```

- [ ] **Step 4: Run the shape test to verify it now passes**

```bash
cd backend && go test ./internal/api/ -run TestArticleListItemHasNoFatPayload -v && cd -
```

Expected: PASS.

- [ ] **Step 5: Update the frontend `Article` / `ArticleListItem` types**

Edit `frontend/src/api/client.ts`. Find the existing `Article` interface (around line 101). Immediately above it, add the lean type:

```ts
export interface ArticleListItem {
  id: number
  feed_id: number
  feed_title?: string
  title: string
  url: string
  published_at: string | null
  summary_brief: string
  fetched_at: string
  word_count?: number
  reading_minutes?: number
  is_read?: boolean
  media_url?: string
  media_type?: string
  media_duration_seconds?: number
  links_extendable?: boolean | null
  link_set_suggested?: boolean | null
  parent_article_id?: number | null
  processing_state?: 'ready' | 'stub' | 'processing' | 'failed'
  prerank_score?: number | null
  editor_note?: string
  manual_tags: UserTag[]
  // Transient — surfaced only by some recommendation responses.
  parent_title?: string
  is_fallback?: boolean
  // True for items returned by the link-set endpoint when the worker
  // tagged a parent as a link list but the user hasn't extracted it.
  is_link_set?: boolean
}
```

Keep `Article` as-is (it remains the detail-endpoint shape with `content` and `summary_detailed`).

- [ ] **Step 6: Retype the list-returning client functions**

In the same file, find the function `getArticles` (search for `export const getArticles`) and the related `getRecommended`, `getLinkSetRecommended`, `searchArticles` functions. Change their return type from `Promise<Article[]>` to `Promise<ArticleListItem[]>`.

Specifically:

```ts
export const getArticles = (params?: { ... }) =>
  api.get<ArticleListItem[]>('/articles', { params }).then(res => res.data)

export const searchArticles = (q: string, limit = 20) =>
  api.get<ArticleListItem[]>('/articles/search', { params: { q, limit } }).then(res => res.data)
```

For `getRecommended` and `getLinkSetRecommended`, check the current signature and apply the same `Article[]` → `ArticleListItem[]` swap **only if** they call the trimmed `GET /api/articles` family. (Recommended uses its own endpoint that still returns full Articles — leave those alone unless TypeScript errors say otherwise.)

- [ ] **Step 7: Update `ArticleListPage.tsx` type imports**

In `frontend/src/pages/ArticleListPage.tsx`, change:

```ts
import { ..., Article, ... } from '../api/client'
```

to:

```ts
import { ..., Article, ArticleListItem, ... } from '../api/client'
```

Then find every place that has `useState<Article[]>` or `Article[]` annotation referring to the list data — replace with `ArticleListItem[]`. (The page-level `articles`, `setArticles`, `loadArticles` callback locals, anything used by infinite scroll.) Leave references to `Article` alone if they are about a single article fetched via `getArticle()`.

- [ ] **Step 8: Run frontend type check**

```bash
cd frontend && npm run build 2>&1 | tail -40 && cd -
```

Expected: build succeeds. If TypeScript flags `.content` or `.summary_detailed` accesses on the list items, those are real bugs — fix by either (a) changing the call site to fetch the full article via `getArticle`, or (b) reverting that one field if it's safely available (it shouldn't be).

- [ ] **Step 9: Run backend tests**

```bash
cd backend && go test ./... && cd -
```

Expected: PASS.

- [ ] **Step 10: Commit**

```bash
git add backend/internal/api/article.go backend/internal/api/article_list_item.go \
        backend/internal/api/article_list_response_test.go backend/internal/api/article_list_response_helpers_test.go \
        frontend/src/api/client.ts frontend/src/pages/ArticleListPage.tsx
git commit -m "perf(api): drop content/summary_detailed from list response

Splits the list DTO from the detail DTO. Frontend list only renders
summary_brief; full article body is fetched lazily on detail navigation.
Saves ~80-90% of list response bytes on a typical 20-item page."
```

---

## Task 4: Add ETag + 304 to the list endpoint

**Files:**
- Create: `backend/internal/api/etag.go`
- Modify: `backend/internal/api/article.go:130-201` (GetAll)
- Create: `backend/internal/api/etag_test.go`

- [ ] **Step 1: Write a failing test for the ETag helper**

Create `backend/internal/api/etag_test.go`:

```go
package api_test

import (
	"strings"
	"testing"
	"time"

	"github.com/bytedance/rss-pal/internal/api"
	"github.com/bytedance/rss-pal/internal/model"
)

func TestComputeListETagStable(t *testing.T) {
	items := []api.ArticleListItem{
		{ID: 1, FetchedAt: time.Unix(100, 0)},
		{ID: 2, FetchedAt: time.Unix(200, 0)},
	}
	a := api.ComputeListETag("k1", items)
	b := api.ComputeListETag("k1", items)
	if a != b {
		t.Fatalf("same input must produce same etag: %q vs %q", a, b)
	}
	if !strings.HasPrefix(a, `W/"`) || !strings.HasSuffix(a, `"`) {
		t.Fatalf("expected weak etag format W/\"...\", got %q", a)
	}
}

func TestComputeListETagChangesOnContent(t *testing.T) {
	base := []api.ArticleListItem{{ID: 1, FetchedAt: time.Unix(100, 0)}}
	tag1 := api.ComputeListETag("k1", base)

	updated := []api.ArticleListItem{{ID: 1, FetchedAt: time.Unix(999, 0)}}
	if tag1 == api.ComputeListETag("k1", updated) {
		t.Fatalf("etag must change when fetched_at changes")
	}

	if tag1 == api.ComputeListETag("k2", base) {
		t.Fatalf("etag must change when query signature changes")
	}

	base2 := []api.ArticleListItem{
		{ID: 1, FetchedAt: time.Unix(100, 0)},
		{ID: 2, FetchedAt: time.Unix(100, 0)},
	}
	if tag1 == api.ComputeListETag("k1", base2) {
		t.Fatalf("etag must change when item count changes")
	}
	_ = model.UserTag{} // keep import
}
```

- [ ] **Step 2: Run the test to confirm it fails (function does not exist)**

```bash
cd backend && go test ./internal/api/ -run TestComputeListETag -v && cd -
```

Expected: FAIL with `undefined: api.ComputeListETag`.

- [ ] **Step 3: Implement `ComputeListETag`**

Create `backend/internal/api/etag.go`:

```go
package api

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"
)

// ComputeListETag builds a weak ETag for an article-list response.
// Inputs combine a per-request query signature (so different filter
// combinations get distinct ETags) with content fingerprints — count,
// first/last id, and the max fetched_at across items. This is cheap
// (no extra DB round trip) and changes whenever the worker writes
// new articles for the query.
//
// Format: W/"<hex sha256>"
func ComputeListETag(querySignature string, items []ArticleListItem) string {
	h := sha256.New()
	fmt.Fprintf(h, "v1|%s|count=%d|", querySignature, len(items))
	if len(items) > 0 {
		var maxFetched time.Time
		for _, it := range items {
			if it.FetchedAt.After(maxFetched) {
				maxFetched = it.FetchedAt
			}
		}
		fmt.Fprintf(h, "first=%d|last=%d|max_fetched=%d",
			items[0].ID, items[len(items)-1].ID, maxFetched.UnixNano())
	}
	return `W/"` + hex.EncodeToString(h.Sum(nil)[:16]) + `"`
}
```

- [ ] **Step 4: Run the test to verify it passes**

```bash
cd backend && go test ./internal/api/ -run TestComputeListETag -v && cd -
```

Expected: PASS.

- [ ] **Step 5: Wire ETag into the `GetAll` handler**

In `backend/internal/api/article.go`, modify `GetAll`. After building `out` (the `[]ArticleListItem`) and before `c.JSON(...)`, insert:

```go
	// Cache-Control: must revalidate every time, but a 304 short-circuit
	// lets us skip serializing the body when nothing changed.
	c.Header("Cache-Control", "private, no-cache")

	signature := fmt.Sprintf("u=%d|f=%v|unread=%v|saved=%v|tag=%v|untagged=%v|sort=%s|dir=%s|limit=%d|offset=%d",
		userID, derefIntPtr(feedID), unreadOnly, savedOnly, derefIntPtr(tagID), untagged, sort, dir, limit, offset)
	etag := ComputeListETag(signature, out)
	c.Header("ETag", etag)
	if match := c.GetHeader("If-None-Match"); match != "" && match == etag {
		c.Status(http.StatusNotModified)
		return
	}

	c.JSON(http.StatusOK, out)
```

Add this helper to the bottom of `article.go` (or to `etag.go` if it isn't already used elsewhere — check first with `grep -n derefIntPtr backend/internal/`):

```go
func derefIntPtr(p *int) any {
	if p == nil {
		return "nil"
	}
	return *p
}
```

Make sure `"fmt"` and `"net/http"` are imported in `article.go` (they likely already are).

- [ ] **Step 6: Write a handler-level test for the 304 round-trip**

Append to `backend/internal/api/etag_test.go`:

```go
func TestListETagHeaderIsPresent(t *testing.T) {
	// Construct a representative response and verify ComputeListETag
	// returns a value that the handler would emit. (Full handler test
	// would need a DB; we cover the integration manually with curl
	// per the plan's verification steps.)
	items := []api.ArticleListItem{{ID: 1, FetchedAt: time.Unix(100, 0)}}
	got := api.ComputeListETag("u=1", items)
	if got == "" {
		t.Fatalf("etag must not be empty")
	}
}
```

- [ ] **Step 7: Run all api tests**

```bash
cd backend && go test ./internal/api/... && cd -
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add backend/internal/api/article.go backend/internal/api/etag.go backend/internal/api/etag_test.go
git commit -m "perf(api): add ETag + 304 short-circuit on article list endpoint"
```

---

## Task 5: Add ETag + Cache-Control to the detail endpoint

**Files:**
- Modify: `backend/internal/api/article.go:221-255` (GetByID)
- Modify: `backend/internal/api/etag_test.go`

- [ ] **Step 1: Write a failing test for the detail ETag helper**

Append to `backend/internal/api/etag_test.go`:

```go
func TestComputeDetailETagStable(t *testing.T) {
	art := model.Article{
		ID:              7,
		FetchedAt:       time.Unix(500, 0),
		SummaryDetailed: "abc",
		Content:         "hello world",
	}
	a := api.ComputeDetailETag(art)
	b := api.ComputeDetailETag(art)
	if a != b {
		t.Fatalf("detail etag must be stable: %q vs %q", a, b)
	}
}

func TestComputeDetailETagChangesOnUpdate(t *testing.T) {
	art := model.Article{ID: 7, FetchedAt: time.Unix(500, 0), Content: "v1", SummaryDetailed: "s1"}
	tag1 := api.ComputeDetailETag(art)
	art.Content = "v2"
	if tag1 == api.ComputeDetailETag(art) {
		t.Fatalf("etag must change when content changes")
	}
	art.Content = "v1"
	art.SummaryDetailed = "s2"
	if tag1 == api.ComputeDetailETag(art) {
		t.Fatalf("etag must change when summary_detailed changes")
	}
	art.SummaryDetailed = "s1"
	art.FetchedAt = time.Unix(999, 0)
	if tag1 == api.ComputeDetailETag(art) {
		t.Fatalf("etag must change when fetched_at changes")
	}
}
```

- [ ] **Step 2: Run the test to confirm it fails**

```bash
cd backend && go test ./internal/api/ -run TestComputeDetailETag -v && cd -
```

Expected: FAIL with `undefined: api.ComputeDetailETag`.

- [ ] **Step 3: Implement `ComputeDetailETag`**

Append to `backend/internal/api/etag.go`:

```go
// ComputeDetailETag builds a weak ETag for a single-article response.
// Sensitive to fetched_at, content length, and summary lengths — any
// of which change when the worker re-fetches or re-summarises the
// article.
func ComputeDetailETag(a model.Article) string {
	h := sha256.New()
	fmt.Fprintf(h, "v1|id=%d|fetched=%d|content=%d|brief=%d|detailed=%d|state=%s",
		a.ID, a.FetchedAt.UnixNano(),
		len(a.Content), len(a.SummaryBrief), len(a.SummaryDetailed),
		a.ProcessingState)
	return `W/"` + hex.EncodeToString(h.Sum(nil)[:16]) + `"`
}
```

Add to imports of `etag.go`:

```go
	"github.com/bytedance/rss-pal/internal/model"
```

- [ ] **Step 4: Run the test to verify it passes**

```bash
cd backend && go test ./internal/api/ -run TestComputeDetailETag -v && cd -
```

Expected: PASS.

- [ ] **Step 5: Wire ETag + Cache-Control into `GetByID`**

In `backend/internal/api/article.go`, modify `GetByID`. After loading the article (around line 232) and before constructing the `response` map, insert:

```go
	// 5 min fresh, then SWR for another 10 min — articles change
	// rarely. The streaming-summary endpoint controls its own caching.
	c.Header("Cache-Control", "private, max-age=300, stale-while-revalidate=600")

	etag := ComputeDetailETag(*article)
	c.Header("ETag", etag)
	if match := c.GetHeader("If-None-Match"); match != "" && match == etag {
		c.Status(http.StatusNotModified)
		return
	}
```

Make sure `article` is dereferenced safely — check the existing nil-check above the insertion point. If `article` is a value (not a pointer), drop the `*`.

- [ ] **Step 6: Run all api tests**

```bash
cd backend && go test ./internal/api/... && cd -
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add backend/internal/api/article.go backend/internal/api/etag.go backend/internal/api/etag_test.go
git commit -m "perf(api): add ETag and Cache-Control on article detail endpoint"
```

---

## Task 6: axios timeout + GET-only retry

**Files:**
- Modify: `frontend/src/api/client.ts:1-27`

- [ ] **Step 1: Replace the axios setup with a timeout + retry interceptor**

In `frontend/src/api/client.ts`, change the top of the file (lines 1-27) to:

```ts
import axios, { AxiosError, InternalAxiosRequestConfig } from 'axios'
import { clearAllFabCollapsed } from '../components/CollapsibleFab'

// Augment the axios request config with a per-request retry counter
// so the response interceptor can avoid retrying the same request
// more than once.
interface RetriableConfig extends InternalAxiosRequestConfig {
  __retryCount?: number
}

export const api = axios.create({
  baseURL: '/api',
  // Weak networks can stall single packets for many seconds. 10s is
  // generous enough for legitimate slow responses while still giving
  // the interceptor a chance to retry.
  timeout: 10000,
})

// JWT interceptor
api.interceptors.request.use(config => {
  const token = localStorage.getItem('token')
  if (token) {
    config.headers.Authorization = `Bearer ${token}`
  }
  return config
})

api.interceptors.response.use(
  res => res,
  async (err: AxiosError) => {
    const config = err.config as RetriableConfig | undefined

    // Retry once on network failure / timeout, ONLY for GET requests.
    // Non-GET requests could create duplicate side-effects.
    if (config) {
      const method = (config.method ?? 'get').toLowerCase()
      const isNetworkFailure = !err.response || err.code === 'ECONNABORTED' || err.code === 'ERR_NETWORK'
      if (method === 'get' && isNetworkFailure && !config.__retryCount) {
        config.__retryCount = 1
        await new Promise(r => setTimeout(r, 500))
        return api(config)
      }
    }

    if (err.response?.status === 401) {
      localStorage.removeItem('token')
      localStorage.removeItem('user')
      window.location.href = '/login'
    }
    return Promise.reject(err)
  }
)
```

(The `clearAllFabCollapsed` import is kept because the original file used it elsewhere — leave the rest of the file untouched.)

- [ ] **Step 2: Verify the rest of the file still compiles**

```bash
cd frontend && npm run build 2>&1 | tail -20 && cd -
```

Expected: build succeeds. If TypeScript complains about `InternalAxiosRequestConfig` not being exported, the installed axios version is older — fall back to `AxiosRequestConfig` and add `// eslint-disable-next-line @typescript-eslint/no-explicit-any` if needed.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/api/client.ts
git commit -m "perf(frontend): axios 10s timeout + single GET retry on network failure"
```

---

## Task 7: Skeleton screens for list and detail

**Files:**
- Create: `frontend/src/components/ArticleListSkeleton.tsx`
- Create: `frontend/src/components/ArticleDetailSkeleton.tsx`
- Create: `frontend/src/components/Skeleton.css`
- Modify: `frontend/src/pages/ArticleListPage.tsx` (render skeleton on first load)
- Modify: `frontend/src/pages/ArticlePage.tsx` (render skeleton on first load)

- [ ] **Step 1: Create the shared skeleton CSS**

Create `frontend/src/components/Skeleton.css`:

```css
.skeleton-pulse {
  background: linear-gradient(
    90deg,
    var(--color-skeleton-base, #e9e9ee) 0%,
    var(--color-skeleton-shine, #f4f4f8) 50%,
    var(--color-skeleton-base, #e9e9ee) 100%
  );
  background-size: 200% 100%;
  animation: skeleton-shimmer 1.4s ease-in-out infinite;
  border-radius: 4px;
  display: inline-block;
}

@keyframes skeleton-shimmer {
  0%   { background-position: 200% 0; }
  100% { background-position: -200% 0; }
}

.skeleton-row {
  padding: 12px 0;
  border-bottom: 1px solid var(--color-border-subtle, #eee);
}

.skeleton-row + .skeleton-row {
  margin-top: 0;
}

.skeleton-bar { height: 14px; }
.skeleton-bar-tall { height: 18px; }

@media (prefers-color-scheme: dark) {
  .skeleton-pulse {
    background: linear-gradient(
      90deg,
      var(--color-skeleton-base, #2a2a30) 0%,
      var(--color-skeleton-shine, #3a3a42) 50%,
      var(--color-skeleton-base, #2a2a30) 100%
    );
    background-size: 200% 100%;
  }
}
```

- [ ] **Step 2: Create the list skeleton component**

Create `frontend/src/components/ArticleListSkeleton.tsx`:

```tsx
import './Skeleton.css'

interface Props {
  rows?: number
}

// ArticleListSkeleton renders placeholder rows that mimic the layout
// of ArticleRow (title bar + brief summary + metadata strip). Used
// on the first article-list paint so the user sees structure rather
// than a blank page on weak networks.
export function ArticleListSkeleton({ rows = 8 }: Props) {
  return (
    <div role="status" aria-label="Loading articles" aria-live="polite">
      {Array.from({ length: rows }).map((_, i) => (
        <div key={i} className="skeleton-row">
          <div className="skeleton-pulse skeleton-bar-tall" style={{ width: `${60 + (i % 4) * 8}%` }} />
          <div style={{ height: 6 }} />
          <div className="skeleton-pulse skeleton-bar" style={{ width: '92%' }} />
          <div style={{ height: 6 }} />
          <div className="skeleton-pulse skeleton-bar" style={{ width: '40%' }} />
        </div>
      ))}
    </div>
  )
}
```

- [ ] **Step 3: Create the detail skeleton component**

Create `frontend/src/components/ArticleDetailSkeleton.tsx`:

```tsx
import './Skeleton.css'

// ArticleDetailSkeleton renders a title bar and a few paragraph bars,
// shown while the detail endpoint is in flight on first navigation.
export function ArticleDetailSkeleton() {
  return (
    <div role="status" aria-label="Loading article" aria-live="polite" style={{ padding: '16px 0' }}>
      <div className="skeleton-pulse skeleton-bar-tall" style={{ width: '70%', height: 28 }} />
      <div style={{ height: 12 }} />
      <div className="skeleton-pulse skeleton-bar" style={{ width: '30%' }} />
      <div style={{ height: 24 }} />
      {Array.from({ length: 5 }).map((_, i) => (
        <div key={i}>
          <div className="skeleton-pulse skeleton-bar" style={{ width: i === 4 ? '60%' : '100%' }} />
          <div style={{ height: 10 }} />
        </div>
      ))}
    </div>
  )
}
```

- [ ] **Step 4: Render the list skeleton on first-load**

In `frontend/src/pages/ArticleListPage.tsx`, find the place that renders the articles list (search for `articles.map` near where rows are rendered). Above it, identify where the current loading branch lives.

Find the section that currently renders "Loading" or empty state on first load (look around the JSX where `loading && articles.length === 0` would make sense — typically inside the main content column). Replace the blank/loading-spinner branch with the skeleton:

```tsx
import { ArticleListSkeleton } from '../components/ArticleListSkeleton'
// ... in the JSX where the list body is rendered:
{loading && articles.length === 0 ? (
  <ArticleListSkeleton rows={8} />
) : (
  // existing rendering of rows + load-more spinner
)}
```

Keep the existing "loading more" pagination spinner — the skeleton only replaces the first-paint blank state.

- [ ] **Step 5: Render the detail skeleton on first-load**

In `frontend/src/pages/ArticlePage.tsx`, find the JSX where the article body would render. Above the main return (or in the early-return path for "no article yet"), add:

```tsx
import { ArticleDetailSkeleton } from '../components/ArticleDetailSkeleton'
// ...
if (loading && !article) {
  return (
    <div className="article-page">
      <ArticleDetailSkeleton />
    </div>
  )
}
```

Use whichever wrapper class the page already uses; if there is no `article-page` class, omit the wrapper.

- [ ] **Step 6: Build the frontend**

```bash
cd frontend && npm run build 2>&1 | tail -20 && cd -
```

Expected: build succeeds.

- [ ] **Step 7: Commit**

```bash
git add frontend/src/components/Skeleton.css \
        frontend/src/components/ArticleListSkeleton.tsx \
        frontend/src/components/ArticleDetailSkeleton.tsx \
        frontend/src/pages/ArticleListPage.tsx \
        frontend/src/pages/ArticlePage.tsx
git commit -m "perf(ui): skeleton placeholders on first list and detail paint"
```

---

## Task 8: DB composite indexes (migration only — manual apply on existing DB)

**Files:**
- Create: `backend/migrations/027_perf_indexes.sql`

- [ ] **Step 1: Confirm the next migration number**

```bash
ls /Users/bytedance/mygit/rss-pal/backend/migrations/ | sort | tail -5
```

Expected: highest number is `026_hidden_articles.sql`. Use `027` as the next number. If anything higher already exists, increment.

- [ ] **Step 2: Create the migration file**

Create `backend/migrations/027_perf_indexes.sql`:

```sql
-- 027_perf_indexes.sql
--
-- Composite indexes for the article list query, which filters by
-- feed_id and sorts by published_at DESC or fetched_at DESC. On a
-- table that grows over time these become necessary to keep list
-- TTFB flat.
--
-- All CREATE INDEX statements use CONCURRENTLY so they do not block
-- writes from the worker. CONCURRENTLY cannot run inside a
-- transaction — psql must be invoked without a wrapping BEGIN/COMMIT.
--
-- For an existing deployment, apply with:
--   docker-compose exec -T postgres \
--     psql -U postgres -d rsspal < backend/migrations/027_perf_indexes.sql

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_articles_feed_published
    ON articles (feed_id, published_at DESC NULLS LAST);

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_articles_feed_fetched
    ON articles (feed_id, fetched_at DESC);

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_articles_published
    ON articles (published_at DESC NULLS LAST);
```

- [ ] **Step 3: Commit**

```bash
git add backend/migrations/027_perf_indexes.sql
git commit -m "perf(db): add composite indexes for article list query

idx_articles_feed_published, idx_articles_feed_fetched, idx_articles_published
all use CREATE INDEX CONCURRENTLY so they do not block worker writes.

Existing deployments must apply manually:
  docker-compose exec -T postgres \\
    psql -U postgres -d rsspal < backend/migrations/027_perf_indexes.sql"
```

---

## Task 9: Final verification + push

- [ ] **Step 1: Run the full test suite**

```bash
cd backend && go test ./... && cd -
cd frontend && npm run build && cd -
```

Both must pass.

- [ ] **Step 2: Bring the stack up with rebuilt images**

```bash
docker-compose up -d --build
docker-compose logs --tail=50 api
docker-compose logs --tail=50 frontend
```

Expected: no startup errors. The API should bind on :8080; nginx should accept connections.

- [ ] **Step 3: Smoke-test gzip is on**

```bash
curl -s -H 'Accept-Encoding: gzip' -I https://localhost/assets/$(curl -s https://localhost/ -k | grep -o 'assets/[a-zA-Z0-9._-]*\.js' | head -1) -k | grep -i 'content-encoding'
```

Expected: `Content-Encoding: gzip` (case may vary). If empty, re-check the gzip block placement and `gzip_min_length`.

- [ ] **Step 4: Smoke-test ETag**

```bash
# Replace YOUR_JWT with a valid token from localStorage (DevTools → Application).
TOKEN="YOUR_JWT"
ETAG=$(curl -sk -H "Authorization: Bearer $TOKEN" -D - 'https://localhost/api/articles?limit=20' -o /dev/null | awk -F': ' 'tolower($1)=="etag"{print $2}' | tr -d '\r')
echo "ETag: $ETAG"
curl -sk -H "Authorization: Bearer $TOKEN" -H "If-None-Match: $ETAG" -o /dev/null -w '%{http_code}\n' 'https://localhost/api/articles?limit=20'
```

Expected: first command prints a `W/"..."` ETag; second prints `304`.

- [ ] **Step 5: Apply the migration on the live DB**

```bash
docker-compose exec -T postgres psql -U postgres -d rsspal < backend/migrations/027_perf_indexes.sql
docker-compose exec -T postgres psql -U postgres -d rsspal -c "\d articles" | grep -E 'idx_articles_feed_(published|fetched)|idx_articles_published'
```

Expected: the three new indexes appear in `\d articles` output. **Pause here and ask the user to confirm** before continuing — DB changes are reversible only via `DROP INDEX`, but the user's `Never destroy or lose the database` preference means we surface this step explicitly.

- [ ] **Step 6: Push and update the PR**

```bash
git push
gh pr view --json url -q .url
gh pr ready  # mark PR ready for review
```

- [ ] **Step 7: Confirm the PR URL**

Print the PR URL one more time so the operator can hand it to the user:

```bash
gh pr view --json url -q .url
```

---

## Spec coverage check

| Spec section | Implemented in |
|---|---|
| A. Nginx gzip | Task 1 |
| B. Gin gzip middleware | Task 2 |
| C. List API payload trim | Task 3 |
| D. ETag + Cache-Control (list) | Task 4 |
| D. ETag + Cache-Control (detail) | Task 5 |
| E. DB indexes | Task 8 |
| F. axios timeout + GET retry | Task 6 |
| F. Parallel feeds/articles fetch | Already parallel — skipped (see top note) |
| G. Skeleton screens | Task 7 |
| Implementation order (8 commits) | Tasks 1, 2, 3, 4, 5, 6, 7, 8 (8 commits, matches spec) |
