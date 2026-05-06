# Bookmarklet Capture Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a Settings-page bookmarklet that captures the current browser page's HTML, sends it to a CORS+token-authenticated capture endpoint, and either updates an existing article (matched by normalized URL across the user's feeds) or creates a new one in an auto-provisioned "📑 收藏" feed.

**Architecture:** New token-authenticated POST endpoint `/api/bookmarklet/capture` (registered as a public route in `main.go`, does its own bearer-token check via `users.bookmarklet_token`). New JWT settings endpoints `GET/POST /api/settings/bookmarklet-token` for showing/rotating the token. Saved feed is `feeds(feed_type='saved', url='bookmarklet://user/<id>', is_active=true, owner_id=<id>)`; worker excludes `feed_type='saved'` from polling. Frontend Settings page renders a draggable `<a href="javascript:...">` with the user's token baked in.

**Tech Stack:** Go 1.x + Gin + lib/pq (no ORM). PostgreSQL. React + axios. goquery (already used). No new dependencies.

**Spec:** `docs/superpowers/specs/2026-05-06-bookmarklet-capture-design.md`

---

## File Structure

| File | Action | Responsibility |
|------|--------|----------------|
| `backend/migrations/007_bookmarklet.sql` | create | Add `users.bookmarklet_token` column + partial unique index |
| `backend/internal/util/urlnorm.go` | create | Pure `NormalizeURL(string) string` |
| `backend/internal/util/urlnorm_test.go` | create | Table-driven tests for `NormalizeURL` |
| `backend/internal/repository/user.go` | modify | Add `GetByBookmarkletToken`, `SetBookmarkletToken` |
| `backend/internal/repository/feed.go` | modify | Add `GetOrCreateSavedFeed`; filter `feed_type='saved'` in `GetAllActive` |
| `backend/internal/repository/article.go` | modify | Add `FindByOwnerAndURL` |
| `backend/internal/api/bookmarklet.go` | create | `BookmarkletHandler` with `Capture` endpoint and bearer-token middleware |
| `backend/internal/api/settings.go` | modify | Add `GetBookmarkletToken`, `RegenerateBookmarkletToken` methods on existing `SettingsHandler` (so they share the JWT-protected handler) |
| `backend/cmd/server/main.go` | modify | Register new public route + 2 settings routes; thread userRepo into SettingsHandler |
| `frontend/src/api/client.ts` | modify | Add `getBookmarkletToken`, `regenerateBookmarkletToken` |
| `frontend/src/pages/SettingsPage.tsx` | modify | Add "📌 浏览器抓取" section |

The bookmarklet *handler* is a new file because it's the only path that does **not** go through JWT middleware — keeping it physically separate avoids accidentally importing JWT helpers like `getUserID(c)`. The settings GET/regenerate methods belong on `SettingsHandler` because they reuse JWT auth and `getUserID(c)` like every other settings endpoint.

`SettingsHandler` currently doesn't take `userRepo`. We thread it in as a constructor arg rather than pulling user-token logic into a third handler — there's only two endpoints and they're conceptually settings.

---

## Task 1: Migration + URL Normalization Utility

**Files:**
- Create: `backend/migrations/007_bookmarklet.sql`
- Create: `backend/internal/util/urlnorm.go`
- Create: `backend/internal/util/urlnorm_test.go`

- [ ] **Step 1.1: Create migration file**

Write `backend/migrations/007_bookmarklet.sql`:

```sql
-- Add per-user long-lived token used to authenticate the browser bookmarklet
-- against POST /api/bookmarklet/capture. Nullable so existing users keep working;
-- token is generated lazily when the user first visits the Settings page.
ALTER TABLE users ADD COLUMN IF NOT EXISTS bookmarklet_token VARCHAR(64);

CREATE UNIQUE INDEX IF NOT EXISTS idx_users_bookmarklet_token
    ON users(bookmarklet_token)
    WHERE bookmarklet_token IS NOT NULL;
```

- [ ] **Step 1.2: Apply migration to running DB**

The migration files are auto-run on first container start, but the running database already exists. Apply manually:

```bash
docker exec -i rss-pal-postgres-1 psql -U postgres -d rsspal < backend/migrations/007_bookmarklet.sql
```

Verify:

```bash
docker exec rss-pal-postgres-1 psql -U postgres -d rsspal -c "\d users" | grep bookmarklet_token
```

Expected output contains: `bookmarklet_token | character varying(64)`

- [ ] **Step 1.3: Write `NormalizeURL` failing tests**

Create `backend/internal/util/urlnorm_test.go`:

```go
package util

import "testing"

func TestNormalizeURL(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"strips fragment", "https://example.com/a#section", "https://example.com/a"},
		{"strips utm_source", "https://example.com/a?utm_source=x", "https://example.com/a"},
		{"strips multiple utm_*", "https://example.com/a?utm_source=x&utm_medium=y", "https://example.com/a"},
		{"strips gclid", "https://example.com/a?gclid=xx", "https://example.com/a"},
		{"strips fbclid", "https://example.com/a?fbclid=xx", "https://example.com/a"},
		{"keeps non-tracking params", "https://example.com/a?id=1&utm_medium=y", "https://example.com/a?id=1"},
		{"keeps multiple non-tracking params in order", "https://example.com/a?id=1&utm_medium=y&id2=2", "https://example.com/a?id=1&id2=2"},
		{"strips fragment and tracking", "https://Example.com/a?utm_source=x&id=1#sec", "https://example.com/a?id=1"},
		{"lowercases host", "https://EXAMPLE.com/path", "https://example.com/path"},
		{"preserves path case", "https://example.com/Article/Foo", "https://example.com/Article/Foo"},
		{"preserves trailing slash", "https://example.com/a/", "https://example.com/a/"},
		{"preserves http scheme", "http://example.com/a", "http://example.com/a"},
		{"clean url unchanged", "https://example.com/a", "https://example.com/a"},
		{"unparseable returned as-is", "not a url", "not a url"},
		{"empty string returned as-is", "", ""},
		{"strips ref and ref_src", "https://example.com/a?ref=foo&ref_src=bar&id=1", "https://example.com/a?id=1"},
		{"strips msclkid yclid igshid", "https://example.com/a?msclkid=1&yclid=2&igshid=3", "https://example.com/a"},
		{"strips _hsenc _hsmi mc_cid mc_eid", "https://example.com/a?_hsenc=1&_hsmi=2&mc_cid=3&mc_eid=4", "https://example.com/a"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeURL(tt.in)
			if got != tt.want {
				t.Errorf("NormalizeURL(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
```

- [ ] **Step 1.4: Run test to verify it fails**

```bash
cd backend && go test ./internal/util/... 2>&1 | tail -10
```

Expected: build failure (`undefined: NormalizeURL`) — that is the failing state.

- [ ] **Step 1.5: Implement `NormalizeURL`**

Create `backend/internal/util/urlnorm.go`:

```go
// Package util holds small pure helpers shared across the codebase.
package util

import (
	"net/url"
	"strings"
)

// trackingParamsExact is the set of query parameter names removed wholesale
// during URL normalization. utm_* is handled separately by prefix.
var trackingParamsExact = map[string]struct{}{
	"gclid":    {},
	"fbclid":   {},
	"msclkid":  {},
	"yclid":    {},
	"igshid":   {},
	"mc_cid":   {},
	"mc_eid":   {},
	"_hsenc":   {},
	"_hsmi":    {},
	"ref":      {},
	"ref_src":  {},
}

// NormalizeURL returns a canonical form of raw used for article deduplication.
// Strips #fragment, removes tracking parameters (utm_*, gclid, fbclid, ...),
// lowercases the host. Preserves scheme, path case, and trailing slash so it
// stays compatible with already-stored URLs from RSS feeds.
//
// Inputs that fail to parse are returned unchanged so the caller can still
// match exotic URLs by exact-string equality.
func NormalizeURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return raw
	}

	u.Fragment = ""
	u.RawFragment = ""

	if u.RawQuery != "" {
		q := u.Query()
		for k := range q {
			if _, drop := trackingParamsExact[k]; drop || strings.HasPrefix(k, "utm_") {
				q.Del(k)
			}
		}
		u.RawQuery = q.Encode()
	}

	u.Host = strings.ToLower(u.Host)
	return u.String()
}
```

- [ ] **Step 1.6: Run tests to verify they pass**

```bash
cd backend && go test ./internal/util/... -v 2>&1 | tail -25
```

Expected: all 18 sub-tests `PASS`.

Note: `url.Values.Encode()` sorts keys alphabetically, which means the "keeps multiple non-tracking params in order" test must use input keys that survive in alphabetical order (`id` before `id2`). Already arranged that way.

- [ ] **Step 1.7: Commit**

```bash
git add backend/migrations/007_bookmarklet.sql backend/internal/util/urlnorm.go backend/internal/util/urlnorm_test.go
git commit -m "$(cat <<'EOF'
feat(bookmarklet): migration 007 + URL normalization util

Adds users.bookmarklet_token column with partial unique index, and a
pure NormalizeURL helper used by the upcoming capture endpoint to
deduplicate articles across utm_* / fragment variations.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 2: User Repository — Bookmarklet Token Methods

**Files:**
- Modify: `backend/internal/repository/user.go`

- [ ] **Step 2.1: Add bookmarklet token methods**

Append to `backend/internal/repository/user.go` (just before `func generateCode(...)` at the bottom):

```go
// GetByBookmarkletToken returns the user that owns the given bookmarklet
// token, or (nil, nil) if no row matches. Used by the capture endpoint
// to authenticate cross-origin bookmarklet requests.
func (r *UserRepository) GetByBookmarkletToken(token string) (*model.User, error) {
	if token == "" {
		return nil, nil
	}
	user := &model.User{}
	err := r.db.QueryRow(
		`SELECT id, username, password_hash, is_admin, created_at FROM users WHERE bookmarklet_token = $1`,
		token,
	).Scan(&user.ID, &user.Username, &user.PasswordHash, &user.IsAdmin, &user.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return user, err
}

// SetBookmarkletToken writes (or rotates) the user's long-lived bookmarklet
// token. Pass an empty string to clear it.
func (r *UserRepository) SetBookmarkletToken(userID int, token string) error {
	var t interface{} = token
	if token == "" {
		t = nil
	}
	_, err := r.db.Exec(`UPDATE users SET bookmarklet_token = $1 WHERE id = $2`, t, userID)
	return err
}

// GetBookmarkletToken returns the user's current bookmarklet token, or "" if
// none has been generated yet.
func (r *UserRepository) GetBookmarkletToken(userID int) (string, error) {
	var token sql.NullString
	err := r.db.QueryRow(`SELECT bookmarklet_token FROM users WHERE id = $1`, userID).Scan(&token)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return token.String, err
}
```

- [ ] **Step 2.2: Verify the file compiles**

```bash
cd backend && go build ./internal/repository/...
```

Expected: no output (success).

- [ ] **Step 2.3: Commit**

```bash
git add backend/internal/repository/user.go
git commit -m "$(cat <<'EOF'
feat(bookmarklet): user repo bookmarklet token methods

GetByBookmarkletToken / GetBookmarkletToken / SetBookmarkletToken — used
by the capture endpoint and the Settings get/regenerate handlers.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 3: Feed Repository — Saved Feed + Worker Filter

**Files:**
- Modify: `backend/internal/repository/feed.go`

- [ ] **Step 3.1: Add `GetOrCreateSavedFeed` method**

Append to `backend/internal/repository/feed.go`:

```go
// GetOrCreateSavedFeed returns the user's "📑 收藏" feed, creating it if it
// doesn't exist. Saved feeds are used as the destination for articles
// captured via the browser bookmarklet when no existing article matches the
// captured URL. The url column has a global UNIQUE constraint, so we use a
// per-user sentinel of `bookmarklet://user/<id>`.
func (r *FeedRepository) GetOrCreateSavedFeed(ownerID int) (*model.Feed, error) {
	var f model.Feed
	var title sql.NullString
	err := r.db.QueryRow(
		`SELECT id, url, title, last_fetched_at, fetch_interval_minutes, etag, last_modified, is_active, owner_id, feed_type, created_at
		 FROM feeds WHERE owner_id = $1 AND feed_type = 'saved'`,
		ownerID,
	).Scan(&f.ID, &f.URL, &title, &f.LastFetchedAt, &f.FetchIntervalMin, new(sql.NullString), new(sql.NullString), &f.IsActive, &f.OwnerID, &f.FeedType, &f.CreatedAt)
	if err == nil {
		f.Title = title.String
		return &f, nil
	}
	if err != sql.ErrNoRows {
		return nil, err
	}

	sentinelURL := fmt.Sprintf("bookmarklet://user/%d", ownerID)
	owner := ownerID
	newFeed := &model.Feed{
		URL:              sentinelURL,
		Title:            "📑 收藏",
		FetchIntervalMin: 60,
		IsActive:         true,
		OwnerID:          &owner,
		FeedType:         "saved",
	}
	insertErr := r.db.QueryRow(
		`INSERT INTO feeds (url, title, fetch_interval_minutes, is_active, owner_id, feed_type)
		 VALUES ($1, $2, $3, $4, $5, $6) RETURNING id, created_at`,
		newFeed.URL, newFeed.Title, newFeed.FetchIntervalMin, newFeed.IsActive, newFeed.OwnerID, newFeed.FeedType,
	).Scan(&newFeed.ID, &newFeed.CreatedAt)
	if insertErr != nil {
		return nil, insertErr
	}
	return newFeed, nil
}
```

- [ ] **Step 3.2: Add the `fmt` import**

The new code uses `fmt.Sprintf`. Ensure `backend/internal/repository/feed.go` imports `fmt`. Current imports are `database/sql`, `time`, and the model package. Add `fmt`:

```go
import (
	"database/sql"
	"fmt"
	"time"

	"github.com/bytedance/rss-pal/internal/model"
)
```

- [ ] **Step 3.3: Filter saved feeds out of the worker's poll list**

Modify `GetAllActive` in `backend/internal/repository/feed.go`. Change:

```go
func (r *FeedRepository) GetAllActive() ([]model.Feed, error) {
	query := `SELECT id, url, title, last_fetched_at, fetch_interval_minutes, etag, last_modified, is_active, owner_id, feed_type, created_at FROM feeds WHERE is_active = true`
```

To:

```go
func (r *FeedRepository) GetAllActive() ([]model.Feed, error) {
	query := `SELECT id, url, title, last_fetched_at, fetch_interval_minutes, etag, last_modified, is_active, owner_id, feed_type, created_at FROM feeds WHERE is_active = true AND feed_type IN ('rss', 'html')`
```

This is the query the worker calls every minute (`cmd/worker/main.go:141`). Without this filter the worker would try to `gofeed.ParseURL("bookmarklet://user/...")` and log an error per cycle.

- [ ] **Step 3.4: Verify compilation**

```bash
cd backend && go build ./...
```

Expected: no output.

- [ ] **Step 3.5: Commit**

```bash
git add backend/internal/repository/feed.go
git commit -m "$(cat <<'EOF'
feat(bookmarklet): feed repo GetOrCreateSavedFeed + worker filter

Lazy-create the per-user saved feed on first capture. Worker excludes
feed_type='saved' from polling so the bookmarklet:// sentinel URL never
hits gofeed.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 4: Article Repository — `FindByOwnerAndURL`

**Files:**
- Modify: `backend/internal/repository/article.go`

- [ ] **Step 4.1: Add the lookup method**

Insert after the existing `Exists` method (around line 135 in `backend/internal/repository/article.go`):

```go
// FindByOwnerAndURL returns the article matching exactURL within any feed
// owned by ownerID, or (nil, nil) if no match. Caller is responsible for
// passing a normalized URL (see util.NormalizeURL).
func (r *ArticleRepository) FindByOwnerAndURL(ownerID int, exactURL string) (*model.Article, error) {
	query := `
		SELECT a.id, a.feed_id, a.title, a.url, a.content, a.published_at, a.summary_brief, a.summary_detailed, a.fetched_at
		FROM articles a
		JOIN feeds f ON a.feed_id = f.id
		WHERE f.owner_id = $1 AND a.url = $2
		LIMIT 1
	`
	var a model.Article
	var content, summaryBrief, summaryDetailed sql.NullString
	err := r.db.QueryRow(query, ownerID, exactURL).Scan(
		&a.ID, &a.FeedID, &a.Title, &a.URL, &content, &a.PublishedAt,
		&summaryBrief, &summaryDetailed, &a.FetchedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	a.Content = content.String
	a.SummaryBrief = summaryBrief.String
	a.SummaryDetailed = summaryDetailed.String
	return &a, nil
}
```

- [ ] **Step 4.2: Verify compilation**

```bash
cd backend && go build ./...
```

Expected: no output.

- [ ] **Step 4.3: Commit**

```bash
git add backend/internal/repository/article.go
git commit -m "$(cat <<'EOF'
feat(bookmarklet): article repo FindByOwnerAndURL

Cross-feed exact-URL lookup scoped to a single user. Used by the
bookmarklet capture endpoint to decide between update vs. create.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 5: Bookmarklet Capture Handler

**Files:**
- Create: `backend/internal/api/bookmarklet.go`

- [ ] **Step 5.1: Create handler file**

Create `backend/internal/api/bookmarklet.go`:

```go
package api

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/bytedance/rss-pal/internal/model"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/bytedance/rss-pal/internal/rss"
	"github.com/bytedance/rss-pal/internal/util"
	"github.com/gin-gonic/gin"
)

// captureMaxBodyBytes caps the JSON body the bookmarklet can send. 1 MiB is
// generous for outerHTML on a typical article page; abusive payloads are
// truncated by gin and produce a 413.
const captureMaxBodyBytes = 1 << 20 // 1 MiB

type BookmarkletHandler struct {
	userRepo    *repository.UserRepository
	feedRepo    *repository.FeedRepository
	articleRepo *repository.ArticleRepository
}

func NewBookmarkletHandler(
	userRepo *repository.UserRepository,
	feedRepo *repository.FeedRepository,
	articleRepo *repository.ArticleRepository,
) *BookmarkletHandler {
	return &BookmarkletHandler{
		userRepo:    userRepo,
		feedRepo:    feedRepo,
		articleRepo: articleRepo,
	}
}

// Capture is the POST /api/bookmarklet/capture handler. It does its own
// bearer-token authentication against users.bookmarklet_token (no JWT) so it
// can be invoked from any third-party origin.
func (h *BookmarkletHandler) Capture(c *gin.Context) {
	user, err := h.authenticate(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "无效的 bookmarklet token"})
		return
	}

	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, captureMaxBodyBytes)
	var req struct {
		URL   string `json:"url"`
		Title string `json:"title"`
		HTML  string `json:"html"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "内容过大"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式错误"})
		return
	}
	if req.URL == "" || req.HTML == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "url 和 html 必填"})
		return
	}

	normalized := util.NormalizeURL(req.URL)
	content, err := extractContentFromHTML(req.HTML)
	if err != nil || strings.TrimSpace(content) == "" {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "无法从页面提取正文"})
		return
	}

	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = normalized
	}

	existing, err := h.articleRepo.FindByOwnerAndURL(user.ID, normalized)
	if err != nil {
		log.Printf("bookmarklet: lookup failed for user=%d url=%s: %v", user.ID, normalized, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询文章失败"})
		return
	}

	if existing != nil {
		if len(content) <= len(existing.Content) {
			c.JSON(http.StatusOK, gin.H{
				"status":     "unchanged",
				"article_id": existing.ID,
				"message":    "已有内容更完整,未覆盖",
			})
			return
		}
		if err := h.articleRepo.UpdateContent(existing.ID, content); err != nil {
			log.Printf("bookmarklet: UpdateContent failed for article=%d: %v", existing.ID, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "更新文章失败"})
			return
		}
		// Clearing summaries forces the worker's backfillSummaries loop to
		// regenerate them from the new content on its next pass.
		if err := h.articleRepo.UpdateSummary(existing.ID, "", ""); err != nil {
			log.Printf("bookmarklet: clear summary failed for article=%d: %v", existing.ID, err)
		}
		log.Printf("bookmarklet: updated article=%d user=%d url=%s len=%d", existing.ID, user.ID, normalized, len(content))
		c.JSON(http.StatusOK, gin.H{
			"status":     "updated",
			"article_id": existing.ID,
			"message":    "已更新文章: " + existing.Title,
		})
		return
	}

	feed, err := h.feedRepo.GetOrCreateSavedFeed(user.ID)
	if err != nil {
		log.Printf("bookmarklet: GetOrCreateSavedFeed failed for user=%d: %v", user.ID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建收藏 feed 失败"})
		return
	}

	article := &model.Article{
		FeedID:  feed.ID,
		Title:   title,
		URL:     normalized,
		Content: content,
	}
	if err := h.articleRepo.Create(article); err != nil {
		log.Printf("bookmarklet: Create article failed for user=%d url=%s: %v", user.ID, normalized, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "新建文章失败"})
		return
	}
	log.Printf("bookmarklet: created article=%d user=%d url=%s len=%d", article.ID, user.ID, normalized, len(content))
	c.JSON(http.StatusCreated, gin.H{
		"status":     "created",
		"article_id": article.ID,
		"message":    "已收藏: " + title,
	})
}

func (h *BookmarkletHandler) authenticate(c *gin.Context) (*model.User, error) {
	authHeader := c.GetHeader("Authorization")
	if authHeader == "" {
		return nil, errors.New("missing token")
	}
	token := authHeader
	if strings.HasPrefix(authHeader, "Bearer ") {
		token = authHeader[7:]
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, errors.New("empty token")
	}
	user, err := h.userRepo.GetByBookmarkletToken(token)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, errors.New("token not found")
	}
	return user, nil
}

// extractContentFromHTML parses the captured outerHTML through goquery and
// reuses the existing scraper extraction logic.
func extractContentFromHTML(html string) (string, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return "", err
	}
	doc.Find("script, style, nav, header, footer, aside, .sidebar, .comments, .advertisement, .ad, .social-share, .related-posts, .tags, [class*=share], [class*=comment], [class*=recommend]").Remove()

	var content string
	doc.Find("article, main, [role='main'], .post-content, .article-content, .article-body, .entry-content, .content, .post").EachWithBreak(func(i int, s *goquery.Selection) bool {
		c := strings.TrimSpace(s.Text())
		if len(c) > len(content) {
			content = c
		}
		return true
	})

	if content == "" {
		var b strings.Builder
		doc.Find("p").Each(func(i int, s *goquery.Selection) {
			t := strings.TrimSpace(s.Text())
			if len(t) > 30 {
				b.WriteString(t)
				b.WriteString("\n\n")
			}
		})
		content = b.String()
	}

	if len(content) > 50000 {
		content = content[:50000] + "..."
	}
	return strings.TrimSpace(content), nil
}

// GenerateBookmarkletToken returns a 32-byte random hex string suitable for
// users.bookmarklet_token. Used by the Settings regenerate endpoint.
func GenerateBookmarkletToken() (string, error) {
	b := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// Compile-time check the rss package is used (avoids unused-import drift if
// the extraction logic is later refactored to call rss.FetchContentFromReader
// directly). Remove once the package is referenced from real code.
var _ = rss.NewContentFetcher
```

Note on the `extractContentFromHTML` function: it duplicates the selector logic from `internal/rss/content.go::fetchDirect` rather than calling `FetchContentFromReader` because that method's selector list is much shorter and would miss many sites. The full-fat extraction here mirrors the production `fetchDirect` selectors. The trailing `var _ = rss.NewContentFetcher` is a placeholder to keep the rss import (used for future refactor); remove if not desired.

**Cleaner alternative if you want to skip the placeholder:** delete the `rss` import line and the `var _ =` line — the function is fully self-contained.

- [ ] **Step 5.2: Verify compilation (handler not yet wired)**

```bash
cd backend && go build ./internal/api/...
```

Expected: no output. If goquery is not in `go.mod`, it already is (used by `internal/rss/content.go`).

- [ ] **Step 5.3: Commit**

```bash
git add backend/internal/api/bookmarklet.go
git commit -m "$(cat <<'EOF'
feat(bookmarklet): capture handler with bearer-token auth

POST /api/bookmarklet/capture: validates Authorization header against
users.bookmarklet_token, normalizes URL, extracts content from posted
HTML, then updates an existing article (clearing summaries so the
worker regenerates them) or creates a new one in the user's saved feed.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 6: Settings Endpoints — Get/Regenerate Token

**Files:**
- Modify: `backend/internal/api/settings.go`

- [ ] **Step 6.1: Thread `userRepo` into `SettingsHandler`**

Edit `backend/internal/api/settings.go`. Change the struct and constructor:

```go
type SettingsHandler struct {
	templateRepo *repository.TemplateRepository
	userRepo     *repository.UserRepository
	cfg          *config.Config
	summarizer   *ai.Summarizer
}

func NewSettingsHandler(cfg *config.Config, templateRepo *repository.TemplateRepository, userRepo *repository.UserRepository) *SettingsHandler {
	var summarizer *ai.Summarizer
	if cfg.Claude.APIKey != "" {
		summarizer = ai.NewSummarizer(cfg.Claude.APIKey, cfg.Claude.BaseURL)
	}
	return &SettingsHandler{cfg: cfg, templateRepo: templateRepo, userRepo: userRepo, summarizer: summarizer}
}
```

- [ ] **Step 6.2: Add the two new handler methods**

Append to `backend/internal/api/settings.go`:

```go
// GetBookmarkletToken GET /api/settings/bookmarklet-token — 返回当前用户的
// bookmarklet token；未生成过返回 token=null。
func (h *SettingsHandler) GetBookmarkletToken(c *gin.Context) {
	userID := getUserID(c)
	token, err := h.userRepo.GetBookmarkletToken(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if token == "" {
		c.JSON(http.StatusOK, gin.H{"token": nil})
		return
	}
	c.JSON(http.StatusOK, gin.H{"token": token})
}

// RegenerateBookmarkletToken POST /api/settings/bookmarklet-token/regenerate
// — 生成新的 token，旧 token 立即失效。
func (h *SettingsHandler) RegenerateBookmarkletToken(c *gin.Context) {
	userID := getUserID(c)
	token, err := GenerateBookmarkletToken()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "生成 token 失败"})
		return
	}
	if err := h.userRepo.SetBookmarkletToken(userID, token); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "保存 token 失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"token": token})
}
```

- [ ] **Step 6.3: Verify compilation**

```bash
cd backend && go build ./internal/api/...
```

Expected: build error in `cmd/server/main.go` because `NewSettingsHandler` signature changed. That's expected — fixed in Task 7.

```bash
cd backend && go build ./internal/api/... 2>&1 | head -5
```

Expected: clean — `internal/api` itself compiles. The error will be in `cmd/server`.

- [ ] **Step 6.4: Commit**

```bash
git add backend/internal/api/settings.go
git commit -m "$(cat <<'EOF'
feat(bookmarklet): settings endpoints to view/rotate token

Adds GET /api/settings/bookmarklet-token and POST .../regenerate, both
under the JWT-protected settings group. Threads userRepo into
SettingsHandler.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 7: Wire Routes in `main.go`

**Files:**
- Modify: `backend/cmd/server/main.go`

- [ ] **Step 7.1: Construct the new handler and update SettingsHandler call**

Edit `backend/cmd/server/main.go`. Find the handler construction block (around lines 35-45) and update `NewSettingsHandler` plus add the bookmarklet handler:

```go
authHandler := api.NewAuthHandler(cfg, userRepo)
feedHandler := api.NewFeedHandler(feedRepo, articleRepo)
articleHandler := api.NewArticleHandler(articleRepo, progressRepo, prefRepo, summarizerService)
articleHandler.SetTemplateRepo(templateRepo, cfg)
prefHandler := api.NewPreferenceHandler(prefRepo)
progressHandler := api.NewProgressHandler(progressRepo)
contentHandler := api.NewContentHandler(articleRepo)
statsHandler := api.NewStatsHandler(statsRepo)
settingsHandler := api.NewSettingsHandler(cfg, templateRepo, userRepo)
shareHandler := api.NewShareHandler(shareRepo, articleRepo)
insightsHandler := api.NewInsightsHandler(prefRepo, templateRepo, summarizer, cfg)
bookmarkletHandler := api.NewBookmarkletHandler(userRepo, feedRepo, articleRepo)
```

- [ ] **Step 7.2: Register the public capture route**

Add the bookmarklet capture route in the **public routes** section (above the `apiGroup` block). Insert after the existing public share route (around line 69):

```go
// Public bookmarklet capture (CORS + per-user token auth, no JWT)
router.POST("/api/bookmarklet/capture", bookmarkletHandler.Capture)
```

The CORS middleware applied at the router level (lines 51-61) already sets the right headers and handles `OPTIONS`, so no extra OPTIONS route is needed.

- [ ] **Step 7.3: Register the two settings token routes**

Inside the `apiGroup` (JWT-protected) block, add to the **Settings** subsection (after line 132 `apiGroup.POST("/settings/polish-prompt", ...)`):

```go
apiGroup.GET("/settings/bookmarklet-token", settingsHandler.GetBookmarkletToken)
apiGroup.POST("/settings/bookmarklet-token/regenerate", settingsHandler.RegenerateBookmarkletToken)
```

- [ ] **Step 7.4: Verify the server builds**

```bash
cd backend && go build ./...
```

Expected: no output.

- [ ] **Step 7.5: Rebuild and restart api + worker containers**

```bash
docker-compose up -d --build api worker
```

- [ ] **Step 7.6: Smoke test the public endpoint exists**

Without a token, the endpoint should return 401 (proves it's wired and reachable):

```bash
curl -i -X POST http://localhost/api/bookmarklet/capture \
  -H "Content-Type: application/json" \
  -d '{"url":"https://example.com","title":"x","html":"<p>x</p>"}' 2>&1 | head -10
```

Expected: `HTTP/1.1 401` with body `{"error":"无效的 bookmarklet token"}`.

- [ ] **Step 7.7: Commit**

```bash
git add backend/cmd/server/main.go
git commit -m "$(cat <<'EOF'
feat(bookmarklet): wire capture + token routes

POST /api/bookmarklet/capture (public, token-auth) and
GET/POST /api/settings/bookmarklet-token[/regenerate] (JWT).

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 8: Frontend — API Client + Settings Section

**Files:**
- Modify: `frontend/src/api/client.ts`
- Modify: `frontend/src/pages/SettingsPage.tsx`

- [ ] **Step 8.1: Add API client functions**

Append to `frontend/src/api/client.ts` (near the other settings endpoints around line 295):

```ts
export const getBookmarkletToken = () =>
  api.get<{ token: string | null }>('/settings/bookmarklet-token').then(res => res.data.token)

export const regenerateBookmarkletToken = () =>
  api.post<{ token: string }>('/settings/bookmarklet-token/regenerate').then(res => res.data.token)
```

- [ ] **Step 8.2: Add bookmarklet section to Settings page**

Edit `frontend/src/pages/SettingsPage.tsx`.

First, update the import line near the top (line 2):

```ts
import { getTemplates, createTemplate, deleteTemplate, getAIConfig, saveAIConfig, setDefaultTemplate, createInviteCode, getInviteCodes, changePassword, polishPrompt, getBookmarkletToken, regenerateBookmarkletToken, SummaryTemplate, UserAIConfig, InviteCode } from '../api/client'
```

Then add a `BookmarkletSection` component near the top of the file (after the existing `PromptField` component definition):

```tsx
function BookmarkletSection() {
  const [token, setToken] = useState<string | null>(null)
  const [revealed, setRevealed] = useState(false)
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    getBookmarkletToken().then(setToken).catch(() => setToken(null))
  }, [])

  const handleRegenerate = async () => {
    if (!confirm('重新生成会让旧书签失效，确认?')) return
    setBusy(true)
    try {
      const t = await regenerateBookmarkletToken()
      setToken(t)
      setRevealed(true)
      toast.success('Token 已重新生成，请重新拖动书签')
    } catch {
      toast.error('生成失败，请重试')
    } finally {
      setBusy(false)
    }
  }

  const apiBase = window.location.origin
  const bookmarkletJS = token
    ? buildBookmarkletJS(apiBase, token)
    : ''

  const masked = token ? token.slice(0, 6) + '…' + token.slice(-4) : '尚未生成'

  return (
    <section className="card" style={{ marginBottom: 20 }}>
      <h2 style={{ marginTop: 0 }}>📌 浏览器抓取</h2>
      <p className="text-sm" style={{ color: '#666', marginBottom: 12 }}>
        把下方按钮拖到浏览器书签栏。在任何网页点一下，就把当前页发回 RSS Pal —
        匹配到已有文章则更新内容，否则保存到「📑 收藏」feed。
      </p>

      {token ? (
        <div style={{ marginBottom: 12 }}>
          <a
            href={bookmarkletJS}
            draggable
            onClick={e => e.preventDefault()}
            style={{
              display: 'inline-block',
              padding: '8px 16px',
              background: '#222',
              color: '#fff',
              borderRadius: 6,
              textDecoration: 'none',
              fontSize: 14,
              cursor: 'grab',
            }}
          >
            📑 发送到 RSS Pal
          </a>
          <span className="text-sm" style={{ marginLeft: 12, color: '#888' }}>
            ← 拖到书签栏
          </span>
        </div>
      ) : (
        <div className="text-sm" style={{ color: '#888', marginBottom: 12 }}>
          点「重新生成」获取你的第一个 token。
        </div>
      )}

      <div className="flex gap-2" style={{ alignItems: 'center', flexWrap: 'wrap' }}>
        <span className="text-sm">Token:</span>
        <code style={{ background: '#f4f4f4', padding: '3px 8px', borderRadius: 4, fontSize: 12 }}>
          {revealed && token ? token : masked}
        </code>
        {token && (
          <button
            type="button"
            className="secondary"
            style={{ fontSize: 12, padding: '3px 10px' }}
            onClick={() => setRevealed(v => !v)}
          >
            {revealed ? '隐藏' : '显示'}
          </button>
        )}
        <button
          type="button"
          style={{ fontSize: 12, padding: '3px 10px' }}
          onClick={handleRegenerate}
          disabled={busy}
        >
          {busy ? '...' : token ? '🔄 重新生成' : '生成 Token'}
        </button>
      </div>
      {token && (
        <p className="text-sm" style={{ color: '#999', marginTop: 8, marginBottom: 0 }}>
          ⚠️ 重新生成后旧书签失效，需要重新拖一次。
        </p>
      )}
    </section>
  )
}

function buildBookmarkletJS(apiBase: string, token: string): string {
  const code = `(function(){
fetch('${apiBase}/api/bookmarklet/capture',{method:'POST',headers:{'Content-Type':'application/json','Authorization':'Bearer ${token}'},body:JSON.stringify({url:location.href,title:document.title,html:document.documentElement.outerHTML})}).then(function(r){return r.json().then(function(j){return{ok:r.ok,j:j}})}).then(function(x){toast(x.ok?x.j.message:'错误: '+(x.j.error||'未知错误'))}).catch(function(e){toast('错误: '+e.message)});
function toast(m){var d=document.createElement('div');d.style.cssText='position:fixed;top:20px;right:20px;z-index:2147483647;padding:12px 16px;background:#222;color:#fff;border-radius:8px;font:14px -apple-system,sans-serif;box-shadow:0 4px 12px rgba(0,0,0,.3);max-width:320px;';d.textContent='RSS Pal: '+m;document.body.appendChild(d);setTimeout(function(){d.remove()},3000);}
})();`
  return 'javascript:' + encodeURIComponent(code)
}
```

Then mount the section inside the page's main render. Find the JSX root return of the `SettingsPage` component (search for the top-level container element) and add `<BookmarkletSection />` near the AI config section.

**Concrete placement instruction:** open `SettingsPage.tsx`, find the line that renders the AI config card (look for the heading containing "AI" or "API Key"). Insert `<BookmarkletSection />` immediately above that card. If unclear, append it just before the inviteCodes / closing wrapper so it appears at the bottom of the page — both placements are acceptable for this iteration.

- [ ] **Step 8.3: Build the frontend**

```bash
docker-compose up -d --build frontend
```

- [ ] **Step 8.4: Smoke test in the browser**

1. Open http://localhost in browser, log in.
2. Navigate to Settings.
3. Confirm the "📌 浏览器抓取" section renders with a "生成 Token" button.
4. Click it; the dark "📑 发送到 RSS Pal" pill should appear, plus a masked token + 显示/重新生成 buttons.
5. Click "显示" — the full hex token should appear.
6. Drag the "📑 发送到 RSS Pal" pill to the browser bookmarks bar.

- [ ] **Step 8.5: Commit**

```bash
git add frontend/src/api/client.ts frontend/src/pages/SettingsPage.tsx
git commit -m "$(cat <<'EOF'
feat(bookmarklet): Settings UI for browser capture

Adds a draggable bookmarklet pill plus token reveal/regenerate controls
in the Settings page. The bookmarklet posts the current page's HTML to
/api/bookmarklet/capture with the user's bearer token.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 9: End-to-End Verification

**Files:**
- (no code changes; verification only)

- [ ] **Step 9.1: Reset article 993's content to a stub so the "updated" path is exercised**

```bash
docker exec rss-pal-postgres-1 psql -U postgres -d rsspal -c \
  "UPDATE articles SET content = 'short stub' WHERE id = 993;"
```

- [ ] **Step 9.2: Verify "updated" path with the bookmarklet**

1. Visit http://localhost/settings, generate or reveal the token, drag the bookmarklet pill to the bookmarks bar.
2. Open https://productonboarding.com/articles/why-product-tours-get-skipped in a new tab.
3. Click the bookmarklet in the bookmarks bar.
4. Right-side toast should show: "RSS Pal: 已更新文章: Why Most Product Tours Get Skipped".
5. Verify in DB:
   ```bash
   docker exec rss-pal-postgres-1 psql -U postgres -d rsspal -c \
     "SELECT id, length(content), summary_brief FROM articles WHERE id = 993;"
   ```
   Expected: `length(content)` is several thousand (browser-rendered DOM has the real article text), `summary_brief` is empty or null (cleared so worker regenerates).

- [ ] **Step 9.3: Verify "created" path on a new URL**

1. Open any blog post not in your subscriptions (e.g., a Medium article).
2. Click the bookmarklet.
3. Toast: "RSS Pal: 已收藏: <article title>".
4. In the RSS Pal feed list, a new feed "📑 收藏" should appear; opening it shows the new article.
5. Verify only one saved feed was created:
   ```bash
   docker exec rss-pal-postgres-1 psql -U postgres -d rsspal -c \
     "SELECT id, title, url, feed_type FROM feeds WHERE feed_type='saved';"
   ```
   Expected: one row per user; URL is `bookmarklet://user/<id>`.

- [ ] **Step 9.4: Verify "unchanged" path (don't downgrade content)**

1. With article 993 already populated from Step 9.2, modify a non-article URL (or use a page where the rendered DOM is shorter than the existing article content).
2. Easier: temporarily set article 993's content to 100K chars:
   ```bash
   docker exec rss-pal-postgres-1 psql -U postgres -d rsspal -c \
     "UPDATE articles SET content = repeat('x', 100000) WHERE id = 993;"
   ```
3. Open the productonboarding URL and click the bookmarklet.
4. Toast: "RSS Pal: 已有内容更完整,未覆盖".
5. Verify content unchanged:
   ```bash
   docker exec rss-pal-postgres-1 psql -U postgres -d rsspal -c \
     "SELECT length(content) FROM articles WHERE id = 993;"
   ```
   Expected: `100000`.
6. Restore real content:
   ```bash
   docker exec rss-pal-postgres-1 psql -U postgres -d rsspal -c \
     "UPDATE articles SET content = '', refetch_attempts = 0 WHERE id = 993;"
   ```
   Worker's next cycle will repopulate via direct + Jina; or click the bookmarklet again to force the browser version.

- [ ] **Step 9.5: Verify token rotation invalidates the old bookmarklet**

1. In Settings, click "🔄 重新生成". Confirm the prompt.
2. The pill's `href` should update; drag it to the bookmarks bar (replacing the old one).
3. The OLD bookmark (still in the bookmarks bar from step 9.2) when clicked should now toast: "RSS Pal: 错误: 无效的 bookmarklet token".
4. The new bookmark works as before.

- [ ] **Step 9.6: Verify worker doesn't try to fetch the saved feed**

```bash
docker-compose logs --since 2m worker | grep -E "bookmarklet://" || echo "no errors — saved feed correctly excluded"
```

Expected: "no errors — saved feed correctly excluded".

- [ ] **Step 9.7: Verify summary backfill regenerates after overwrite**

Wait ~1-2 minutes after Step 9.2 (the worker's `backfillSummaries` runs each cycle), then:

```bash
docker exec rss-pal-postgres-1 psql -U postgres -d rsspal -c \
  "SELECT id, length(content), length(summary_brief) FROM articles WHERE id = 993;"
```

Expected: `summary_brief` length > 0 (regenerated automatically).

- [ ] **Step 9.8: Final commit (verification notes only, no code)**

If any of the above steps reveal issues, fix and re-commit before proceeding. Otherwise no commit needed for this task — it's pure verification.

---

## Self-Review Checklist (post-write)

Run these before declaring the plan ready:

1. **Spec coverage:**
   - migration 007 → Task 1 ✓
   - URL normalization rules → Task 1 ✓
   - saved feed schema (title/url/feed_type/owner_id/is_active) → Task 3 ✓
   - worker filter → Task 3 ✓
   - article cross-feed lookup → Task 4 ✓
   - capture endpoint contract (status codes, body cap, CORS, token auth) → Task 5 ✓
   - overwrite-only-if-longer + clear summaries → Task 5 ✓
   - GET / regenerate token → Task 6 ✓
   - bookmarklet JS template → Task 8 ✓
   - Settings UI → Task 8 ✓
   - manual verification (5 scenarios) → Task 9 ✓

2. **Placeholder scan:** No `TBD`, `TODO`, or "implement appropriately" steps. Every code-modifying step has actual code.

3. **Type/identifier consistency:**
   - `BookmarkletHandler` constructor takes `userRepo, feedRepo, articleRepo` (Task 5) and is wired with same args in Task 7. ✓
   - `SettingsHandler` gets `userRepo` field added in Task 6 and is constructed with it in Task 7. ✓
   - `GetOrCreateSavedFeed(ownerID int)` matches handler call in Task 5. ✓
   - `FindByOwnerAndURL(ownerID int, exactURL string)` matches handler call in Task 5. ✓
   - `GenerateBookmarkletToken()` defined in `bookmarklet.go` and called from `settings.go` (same package, no import needed). ✓
