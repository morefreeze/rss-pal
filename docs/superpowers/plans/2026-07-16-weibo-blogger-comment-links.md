# Weibo Blogger Comment Links Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Preserve the first same-author Weibo comment and its direct resource links in RSS Pal article content, including article 3784.

**Architecture:** Resolve Weibo profile feeds through RSSHub with comments enabled, then normalize the RSS item HTML inside the existing `rss` package. The normalizer keeps only the first top-level comment authored by the profile UID, unwraps Weibo redirect URLs, converts the result to Markdown, and signals callers when an existing article should be refreshed. Worker and manual-feed ingestion share the same content/deep-fetch policy, while the repository performs an idempotent content-and-summary update.

**Tech Stack:** Go 1.25, RSSHub, gofeed, goquery, html-to-markdown v2, PostgreSQL repository tests, Docker Compose, React frontend acceptance check.

---

## File map

- `backend/internal/rss/resolver_social.go`: add the RSSHub comments route and derive its no-comments fallback URL.
- `backend/internal/rss/resolver_social_test.go`: assert all supported Weibo profile forms enable comments.
- `backend/internal/rss/fetcher.go`: retry the base Weibo RSSHub route when the comments-enabled route returns an error response.
- `backend/internal/rss/resolver_fetcher_test.go`: prove the availability fallback request sequence.
- `backend/internal/rss/weibo_content.go`: isolate Weibo status detection, blogger-comment selection, redirect unwrapping, Markdown conversion, and deep-fetch policy.
- `backend/internal/rss/weibo_content_test.go`: fixture-based tests using the article 3784 comment shape.
- `backend/internal/repository/article.go`: idempotently update enriched content and invalidate stale summaries.
- `backend/internal/repository/article_weibo_content_test.go`: exercise the update against the migrated PostgreSQL test schema.
- `backend/cmd/worker/main.go`: use the shared normalizer for scheduled ingestion and update existing Weibo articles.
- `backend/internal/api/feed.go`: mirror the same behavior for manual “fetch now”.

### Task 1: Enable RSSHub comments with an availability fallback

**Files:**
- Modify: `backend/internal/rss/resolver_social_test.go`
- Modify: `backend/internal/rss/resolver_fetcher_test.go`
- Modify: `backend/internal/rss/resolver_social.go`
- Modify: `backend/internal/rss/fetcher.go`

- [ ] **Step 1: Write failing resolver tests**

Change the three Weibo profile expectations in `TestResolveFeedURLSocialPlatforms` to include the RSSHub route parameter:

```go
{name: "weibo_desktop", input: "https://weibo.com/u/1195230310", rsshubBase: base, want: base + "/weibo/user/1195230310/displayComments=1"},
{name: "weibo_mobile_u", input: "https://m.weibo.cn/u/1195230310?jumpfrom=weibocom", rsshubBase: base, want: base + "/weibo/user/1195230310/displayComments=1"},
{name: "weibo_mobile_profile", input: "https://m.weibo.cn/profile/1195230310", rsshubBase: base, want: base + "/weibo/user/1195230310/displayComments=1"},
```

- [ ] **Step 2: Write the failing fallback test**

Add this test to `backend/internal/rss/resolver_fetcher_test.go`:

```go
func TestFetcherFetchFallsBackWhenWeiboCommentsRouteFails(t *testing.T) {
	fetcher := NewFetcher("http://rsshub.test:1200")
	var paths []string
	fetcher.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		paths = append(paths, req.URL.Path)
		if strings.HasSuffix(req.URL.Path, "/displayComments=1") {
			return &http.Response{
				StatusCode: http.StatusBadGateway,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader("comments unavailable")),
			}, nil
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/rss+xml"}},
			Body: io.NopCloser(strings.NewReader(`<?xml version="1.0"?><rss version="2.0"><channel><title>Weibo</title><link>https://weibo.com/u/1195230310</link><description>feed</description><item><title>post</title><link>https://weibo.com/1195230310/P123</link><guid>P123</guid></item></channel></rss>`)),
		}, nil
	})}

	result, err := fetcher.Fetch(context.Background(), "https://weibo.com/u/1195230310", "", "")
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	want := []string{
		"/weibo/user/1195230310/displayComments=1",
		"/weibo/user/1195230310",
	}
	if !reflect.DeepEqual(paths, want) {
		t.Fatalf("paths = %#v, want %#v", paths, want)
	}
	if result == nil || result.Feed == nil || result.Feed.Title != "Weibo" {
		t.Fatalf("unexpected result: %#v", result)
	}
}
```

Add `reflect` to that test file's imports.

- [ ] **Step 3: Run the focused tests and verify they fail**

Run:

```bash
cd backend
go test ./internal/rss -run 'TestResolveFeedURLSocialPlatforms|TestFetcherFetchFallsBackWhenWeiboCommentsRouteFails' -count=1
```

Expected: the resolver cases report the old routes, and the fallback test receives a 502 error after only one request.

- [ ] **Step 4: Implement the comments route and fallback URL parser**

Change both successful branches in `resolveWeibo` to return:

```go
return "/weibo/user/" + parts[1] + "/displayComments=1", true
```

Add this helper to `backend/internal/rss/resolver_social.go`:

```go
func weiboCommentsFallbackURL(raw string) (string, bool) {
	u, err := url.Parse(raw)
	if err != nil || !strings.HasSuffix(u.Path, "/displayComments=1") {
		return "", false
	}
	u.Path = strings.TrimSuffix(u.Path, "/displayComments=1")
	return u.String(), true
}
```

Expand the file import block to include `strings`.

- [ ] **Step 5: Add a shared HTTP response fallback**

Add the following method to `backend/internal/rss/fetcher.go`:

```go
func (f *Fetcher) getFeedResponse(
	ctx context.Context,
	target string,
	configure func(*http.Request),
) (*http.Response, error) {
	get := func(target string) (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
		if err != nil {
			return nil, err
		}
		configure(req)
		return f.client.Do(req)
	}

	resp, err := get(target)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNotModified {
		return resp, nil
	}
	fallback, ok := weiboCommentsFallbackURL(target)
	if !ok {
		return resp, nil
	}
	_ = resp.Body.Close()
	return get(fallback)
}
```

In `Fetcher.Fetch`, replace direct request creation and `f.client.Do(req)` with:

```go
resp, err := f.getFeedResponse(ctx, feedURL, func(req *http.Request) {
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	if lastModified != "" {
		req.Header.Set("If-Modified-Since", lastModified)
	}
	req.Header.Set("User-Agent", userAgent)
})
```

In `Fetcher.Preview`, use the same helper and set its existing `User-Agent`, `Accept`, and `Accept-Language` headers inside the callback. Keep the existing status handling and body closing after the helper returns.

- [ ] **Step 6: Run tests and commit**

Run:

```bash
cd backend
gofmt -w internal/rss/resolver_social.go internal/rss/resolver_social_test.go internal/rss/resolver_fetcher_test.go internal/rss/fetcher.go
go test ./internal/rss -run 'TestResolveFeedURLSocialPlatforms|TestFetcherFetchFallsBackWhenWeiboCommentsRouteFails|TestFetcherFetchUsesResolvedFeedURL|TestFetcherFetchHTMLUsesResolvedFeedURL' -count=1
```

Expected: `ok github.com/bytedance/rss-pal/internal/rss`.

Commit:

```bash
git add backend/internal/rss/resolver_social.go backend/internal/rss/resolver_social_test.go backend/internal/rss/resolver_fetcher_test.go backend/internal/rss/fetcher.go
git commit -m "feat: request Weibo comments with fallback"
```

### Task 2: Extract only the first blogger comment and preserve links

**Files:**
- Create: `backend/internal/rss/weibo_content.go`
- Create: `backend/internal/rss/weibo_content_test.go`

- [ ] **Step 1: Write the failing article-3784-shaped tests**

Create `backend/internal/rss/weibo_content_test.go` with:

```go
package rss

import (
	"strings"
	"testing"
)

const weiboResourceDescription = `恋爱演算法 首播第一集 资源<br>夸克+度<br>WP平
<img src="https://tvax4.sinaimg.cn/poster.jpg">
<div><h3>热门评论</h3>
<p><a href="https://weibo.com/2904546111">美剧日剧韩剧泰剧英剧推荐</a>:
<a href="https://weibo.cn/sinaurl?u=https%3A%2F%2Fpan.quark.cn%2Fs%2Fc140bc08bbfa">网页链接</a> D
<a href="https://weibo.cn/sinaurl?u=https%3A%2F%2Fpan.baidu.com%2Fs%2F1tg0ec1MDYlS8B0Ph7fVDsA%3Fpwd%3Dir22">网页链接</a>
<blockquote><a href="https://weibo.com/2904546111">美剧日剧韩剧泰剧英剧推荐</a>: 拿走吱吱吱</blockquote></p>
<p><a href="https://weibo.com/7204115674">桜吹雪Freedom</a>: [兔子]</p>
</div>`

func TestBuildItemContentKeepsOnlyFirstBloggerComment(t *testing.T) {
	got, enriched := BuildItemContent(weiboResourceDescription, "", "https://weibo.com/2904546111/R8PkkgPKd")
	if !enriched {
		t.Fatal("expected blogger comment enrichment")
	}
	for _, want := range []string{
		"### 博主首评",
		"https://pan.quark.cn/s/c140bc08bbfa",
		"https://pan.baidu.com/s/1tg0ec1MDYlS8B0Ph7fVDsA?pwd=ir22",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("content missing %q:\n%s", want, got)
		}
	}
	for _, unwanted := range []string{"桜吹雪Freedom", "拿走吱吱吱", "weibo.cn/sinaurl", "热门评论"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("content unexpectedly contains %q:\n%s", unwanted, got)
		}
	}
}

func TestBuildItemContentWithoutBloggerCommentKeepsPostBody(t *testing.T) {
	raw := `正文<br><div><h3>热门评论</h3><p><a href="https://weibo.com/7204115674">其他人</a>: hello</p></div>`
	got, enriched := BuildItemContent(raw, "", "https://weibo.com/2904546111/R8PkkgPKd")
	if enriched {
		t.Fatal("unexpected blogger comment enrichment")
	}
	if !strings.Contains(got, "正文") || strings.Contains(got, "其他人") {
		t.Fatalf("unexpected content: %s", got)
	}
}

func TestShouldDeepFetchArticle(t *testing.T) {
	cases := []struct {
		name, feedType, itemURL, mediaType string
		want                               bool
	}{
		{name: "weibo", feedType: "rss", itemURL: "https://weibo.com/2904546111/R8PkkgPKd", want: false},
		{name: "youtube_feed", feedType: "youtube", itemURL: "https://example.com/post", want: false},
		{name: "video", feedType: "rss", itemURL: "https://example.com/video", mediaType: "video/mp4", want: false},
		{name: "normal_article", feedType: "rss", itemURL: "https://example.com/post", want: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ShouldDeepFetchArticle(tc.feedType, tc.itemURL, tc.mediaType); got != tc.want {
				t.Fatalf("ShouldDeepFetchArticle() = %v, want %v", got, tc.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run tests and verify they fail**

Run:

```bash
cd backend
go test ./internal/rss -run 'TestBuildItemContent|TestShouldDeepFetchArticle' -count=1
```

Expected: compile failure because `BuildItemContent` and `ShouldDeepFetchArticle` do not exist.

- [ ] **Step 3: Implement the focused Weibo normalizer**

Create `backend/internal/rss/weibo_content.go`:

```go
package rss

import (
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

const bloggerCommentHeading = "博主首评"

func BuildItemContent(description, fallback, itemURL string) (string, bool) {
	raw := description
	if strings.TrimSpace(raw) == "" {
		raw = fallback
	}
	uid, ok := weiboStatusUID(itemURL)
	if !ok {
		return StripHTML(raw), false
	}
	return buildWeiboItemContent(raw, uid)
}

func ShouldDeepFetchArticle(feedType, itemURL, mediaType string) bool {
	if feedType == "youtube" || feedType == "podcast" || strings.HasPrefix(mediaType, "video/") {
		return false
	}
	return !isWeiboStatusURL(itemURL)
}

func isWeiboStatusURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	host := canonicalHost(u)
	parts := pathSegments(u)
	return (host == "weibo.com" && len(parts) == 2 && isDigits(parts[0])) ||
		(host == "m.weibo.cn" && len(parts) == 2 && parts[0] == "status")
}

func weiboStatusUID(raw string) (string, bool) {
	u, err := url.Parse(raw)
	if err != nil || canonicalHost(u) != "weibo.com" {
		return "", false
	}
	parts := pathSegments(u)
	if len(parts) != 2 || !isDigits(parts[0]) {
		return "", false
	}
	return parts[0], true
}

func buildWeiboItemContent(raw, uid string) (string, bool) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(raw))
	if err != nil {
		return StripHTML(raw), false
	}

	enriched := false
	doc.Find("h3").EachWithBreak(func(_ int, heading *goquery.Selection) bool {
		if strings.TrimSpace(heading.Text()) != "热门评论" {
			return true
		}
		wrapper := heading.Parent()
		var bloggerHTML string
		wrapper.ChildrenFiltered("p").EachWithBreak(func(_ int, comment *goquery.Selection) bool {
			author := comment.ChildrenFiltered("a").First()
			if !weiboAuthorMatches(author.AttrOr("href", ""), uid) {
				return true
			}
			comment.Find("blockquote").Remove()
			bloggerHTML, _ = goquery.OuterHtml(comment)
			return false
		})
		if bloggerHTML == "" {
			wrapper.Remove()
			return false
		}
		wrapper.SetHtml("<h3>" + bloggerCommentHeading + "</h3>" + bloggerHTML)
		enriched = true
		return false
	})

	doc.Find("a[href]").Each(func(_ int, anchor *goquery.Selection) {
		if direct, ok := unwrapWeiboRedirect(anchor.AttrOr("href", "")); ok {
			anchor.SetAttr("href", direct)
		}
	})
	markdown := ExtractMarkdown(doc.Find("body").First())
	return cleanContent(markdown), enriched
}

func weiboAuthorMatches(raw, uid string) bool {
	u, err := url.Parse(raw)
	if err != nil || canonicalHost(u) != "weibo.com" {
		return false
	}
	parts := pathSegments(u)
	return len(parts) == 1 && parts[0] == uid
}

func unwrapWeiboRedirect(raw string) (string, bool) {
	u, err := url.Parse(raw)
	if err != nil || canonicalHost(u) != "weibo.cn" || u.Path != "/sinaurl" {
		return "", false
	}
	target, err := url.Parse(u.Query().Get("u"))
	if err != nil || (target.Scheme != "http" && target.Scheme != "https") || target.Host == "" {
		return "", false
	}
	return target.String(), true
}
```

- [ ] **Step 4: Run tests and commit**

Run:

```bash
cd backend
gofmt -w internal/rss/weibo_content.go internal/rss/weibo_content_test.go
go test ./internal/rss -run 'TestBuildItemContent|TestShouldDeepFetchArticle' -count=1
```

Expected: `ok github.com/bytedance/rss-pal/internal/rss`.

Commit:

```bash
git add backend/internal/rss/weibo_content.go backend/internal/rss/weibo_content_test.go
git commit -m "feat: extract Weibo blogger comment links"
```

### Task 3: Add an idempotent existing-article refresh

**Files:**
- Modify: `backend/internal/repository/article.go`
- Create: `backend/internal/repository/article_weibo_content_test.go`

- [ ] **Step 1: Write the failing repository test**

Create `backend/internal/repository/article_weibo_content_test.go`:

```go
package repository

import (
	"database/sql"
	"testing"

	"github.com/bytedance/rss-pal/internal/repository/testdb"
)

func TestUpdateEnrichedContentIfChanged(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()

	var feedID int
	if err := db.QueryRow(`INSERT INTO feeds (url, title) VALUES ('https://weibo.com/u/2904546111', 'Weibo') RETURNING id`).Scan(&feedID); err != nil {
		t.Fatalf("insert feed: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO articles (feed_id, title, url, content, summary_brief, summary_detailed, word_count, reading_minutes) VALUES ($1, 'post', 'https://weibo.com/2904546111/R8PkkgPKd', 'old login page', 'brief', 'detailed', 3, 1)`, feedID); err != nil {
		t.Fatalf("insert article: %v", err)
	}

	repo := NewArticleRepository(db)
	content := "正文\n\n### 博主首评\n\n[网页链接](https://pan.quark.cn/s/example)"
	updated, err := repo.UpdateEnrichedContentIfChanged(feedID, "https://weibo.com/2904546111/R8PkkgPKd", content, 8, 1)
	if err != nil {
		t.Fatalf("UpdateEnrichedContentIfChanged: %v", err)
	}
	if !updated {
		t.Fatal("expected article update")
	}

	var gotContent string
	var wordCount, readingMinutes int
	var brief, detailed sql.NullString
	if err := db.QueryRow(`SELECT content, word_count, reading_minutes, summary_brief, summary_detailed FROM articles WHERE feed_id = $1`, feedID).Scan(&gotContent, &wordCount, &readingMinutes, &brief, &detailed); err != nil {
		t.Fatalf("read article: %v", err)
	}
	if gotContent != content || wordCount != 8 || readingMinutes != 1 || brief.Valid || detailed.Valid {
		t.Fatalf("unexpected row: content=%q words=%d minutes=%d brief=%v detailed=%v", gotContent, wordCount, readingMinutes, brief, detailed)
	}

	updated, err = repo.UpdateEnrichedContentIfChanged(feedID, "https://weibo.com/2904546111/R8PkkgPKd", content, 8, 1)
	if err != nil || updated {
		t.Fatalf("second update = %v, %v; want false, nil", updated, err)
	}
}
```

- [ ] **Step 2: Run the test and verify it fails**

Run:

```bash
cd backend
go test ./internal/repository -run TestUpdateEnrichedContentIfChanged -count=1
```

Expected: compile failure because `UpdateEnrichedContentIfChanged` does not exist. If PostgreSQL is unavailable, the test may skip after compilation; the compile failure must still be observed first.

- [ ] **Step 3: Implement the repository update**

Add to `backend/internal/repository/article.go` after `UpdateContent`:

```go
func (r *ArticleRepository) UpdateEnrichedContentIfChanged(feedID int, articleURL, content string, wordCount, readingMinutes int) (bool, error) {
	res, err := r.db.Exec(`
		UPDATE articles
		SET content = $3,
		    word_count = $4,
		    reading_minutes = $5,
		    summary_brief = NULL,
		    summary_detailed = NULL,
		    refetch_attempts = 0
		WHERE feed_id = $1
		  AND url = $2
		  AND content IS DISTINCT FROM $3
	`, feedID, articleURL, content, wordCount, readingMinutes)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}
```

- [ ] **Step 4: Run tests and commit**

Run:

```bash
cd backend
gofmt -w internal/repository/article.go internal/repository/article_weibo_content_test.go
go test ./internal/repository -run TestUpdateEnrichedContentIfChanged -count=1
```

Expected: PASS when PostgreSQL is available, or SKIP only for the existing test database availability guard.

Commit:

```bash
git add backend/internal/repository/article.go backend/internal/repository/article_weibo_content_test.go
git commit -m "feat: refresh enriched article content"
```

### Task 4: Wire scheduled and manual feed ingestion

**Files:**
- Modify: `backend/cmd/worker/main.go`
- Modify: `backend/internal/api/feed.go`

- [ ] **Step 1: Add enriched updates to the worker's existing-item branch**

Immediately after `if exists {` in `backend/cmd/worker/main.go`, insert:

```go
content, enriched := rss.BuildItemContent(item.Description, item.Content, item.Link)
if enriched {
	wc, rm := rss.ComputeMetrics(content)
	updated, err := articleRepo.UpdateEnrichedContentIfChanged(feed.ID, item.Link, content, wc, rm)
	if err != nil {
		log.Printf("Failed to enrich existing Weibo article %s: %v", item.Link, err)
	} else if updated {
		log.Printf("Enriched existing Weibo article: %s", item.Link)
	}
}
```

Inside the new-item goroutine, replace the two `StripHTML` calls with:

```go
content, _ := rss.BuildItemContent(item.Description, item.Content, item.Link)
```

Replace the local `skipDeepFetch` construction with:

```go
mediaType := ""
if mediaInfo != nil {
	mediaType = mediaInfo.Type
}
if rss.ShouldDeepFetchArticle(feed.FeedType, item.Link, mediaType) && item.Link != "" {
	log.Printf("Fetching full content for: %s", item.Link)
	fullContent, err := contentFetcher.FetchContent(ctx, item.Link)
	if err != nil {
		log.Printf("Failed to fetch content from %s: %v", item.Link, err)
	} else if len(fullContent) > len(content) {
		content = fullContent
		log.Printf("Got full content: %d chars", len(content))
	}
}
```

- [ ] **Step 2: Mirror the same behavior in manual feed fetch**

Immediately after `if exists {` in the item loop in `backend/internal/api/feed.go`, add:

```go
content, enriched := rss.BuildItemContent(item.Description, item.Content, item.Link)
if enriched {
	wc, rm := rss.ComputeMetrics(content)
	updated, err := articleRepo.UpdateEnrichedContentIfChanged(feed.ID, item.Link, content, wc, rm)
	if err != nil {
		log.Printf("Failed to enrich existing Weibo article %s: %v", item.Link, err)
	} else if updated {
		log.Printf("Enriched existing Weibo article: %s", item.Link)
	}
}
```

For new items, replace the two `StripHTML` calls with:

```go
content, _ := rss.BuildItemContent(item.Description, item.Content, item.Link)
```

Replace the local deep-fetch condition with:

```go
mediaType := ""
if mediaInfo != nil {
	mediaType = mediaInfo.Type
}
if rss.ShouldDeepFetchArticle(feed.FeedType, item.Link, mediaType) && item.Link != "" {
	fullContent, err := h.contentFetcher.FetchContent(c.Request.Context(), item.Link)
	if err == nil && len(fullContent) > len(content) {
		content = fullContent
	}
}
```

- [ ] **Step 3: Format and run package tests**

Run:

```bash
cd backend
gofmt -w cmd/worker/main.go internal/api/feed.go
go test ./cmd/worker ./internal/api ./internal/rss ./internal/repository -count=1
```

Expected: all packages print `ok` (repository integration tests may report skipped tests only when the test database is unavailable).

- [ ] **Step 4: Run static checks and commit**

Run:

```bash
cd backend
go vet ./...
```

Expected: exit status 0 with no findings.

Commit:

```bash
git add backend/cmd/worker/main.go backend/internal/api/feed.go
git commit -m "feat: ingest Weibo blogger comment links"
```

### Task 5: Verify article 3784 end to end

**Files:**
- No source files expected.

- [ ] **Step 1: Run the full backend and frontend verification suites**

Run:

```bash
cd backend
go test ./... -count=1
go vet ./...
cd ../frontend
npm test -- --run
npm run build
```

Expected: all Go tests and vet pass, Vitest passes, and Vite produces a successful production build.

- [ ] **Step 2: Rebuild the local services**

Run from the repository root:

```bash
docker compose up -d --build api worker frontend
docker compose ps
curl -fsS http://127.0.0.1:8080/health
```

Expected: `api`, `worker`, and `frontend` are Up; health returns success.

- [ ] **Step 3: Force the local Weibo feed through the new route**

Find the local feed ID and clear only its HTTP cache metadata:

```bash
docker compose exec -T postgres psql -U postgres -d rsspal -Atc "SELECT id, url FROM feeds WHERE url = 'https://weibo.com/u/2904546111';"
docker compose exec -T postgres psql -U postgres -d rsspal -c "UPDATE feeds SET etag = '', last_modified = '', last_fetched_at = NULL WHERE url = 'https://weibo.com/u/2904546111';"
docker compose restart worker
```

Expected: the first command returns the one target feed; the update affects exactly one row.

- [ ] **Step 4: Verify the stored article content**

Poll for up to 60 seconds and print the final row:

```bash
for i in {1..12}; do
  content=$(docker compose exec -T postgres psql -U postgres -d rsspal -Atc "SELECT content FROM articles WHERE url = 'https://weibo.com/2904546111/R8PkkgPKd';")
  if printf '%s' "$content" | grep -q '博主首评'; then break; fi
  sleep 5
done
docker compose exec -T postgres psql -U postgres -d rsspal -x -c "SELECT id, url, content FROM articles WHERE url = 'https://weibo.com/2904546111/R8PkkgPKd';"
```

Expected content contains:

```text
### 博主首评
https://pan.quark.cn/s/c140bc08bbfa
https://pan.baidu.com/s/1tg0ec1MDYlS8B0Ph7fVDsA?pwd=ir22
```

Expected content does not contain `登录`, `微博热搜`, `桜吹雪Freedom`, `拿走吱吱吱`, or `weibo.cn/sinaurl`.

- [ ] **Step 5: Verify rendering in the local browser**

Open `http://localhost/articles/3784` using the available signed-in browser session. Confirm the `博主首评` section has two clickable links that open the direct Quark and Baidu targets in a new tab, and that no login/search boilerplate is visible.

- [ ] **Step 6: Check the worktree and make a verification-only commit only if needed**

Run:

```bash
git status --short
git log --oneline --decorate -6
```

Expected: only the user's pre-existing untracked backup files and `rss-pal-course/` remain; all feature source changes are committed on `feat/weibo-blogger-comment-links`.
