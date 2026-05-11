# bestblogs-inspired features Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add WeChat-source support via self-hosted RSSHub, word count / reading time on every article, a weekly digest page (`/weekly`) with AI-generated theme intro, and a recommended feeds library (`/recommended`) seeded with 12 high-quality sources curated from bestblogs.dev Issue #93.

**Architecture:** New `rsshub` container provides RSS for sources without native feeds (WeChat, podcasts). One DB migration adds `articles.word_count` / `reading_minutes`, plus `recommended_feeds` (catalog) and `weekly_digests` (AI intro cache) tables. Worker computes metrics inline; new `/api/recommended-feeds` and `/api/weekly-digest` endpoints; two new React pages.

**Tech Stack:** Go 1.21+ (Gin, lib/pq, gofeed, goquery), PostgreSQL 15, React 18 + Vite + Axios, Docker Compose, RSSHub (`diygod/rsshub`).

**Spec reference:** `docs/superpowers/specs/2026-05-06-bestblogs-inspired-features-design.md`

**Spec correction noted:** spec mentions `feed_type='webpage'` but the existing codebase uses `"html"` (see `model/model.go` and `cmd/worker/main.go:174`). This plan uses the actual codebase value `"html"` everywhere; new values added are `"youtube"` and `"podcast"`.

**Working directory for all commands:** `/Users/bytedance/mygit/rss-pal` (project root). Use `cd backend` or `cd frontend` only when explicitly noted.

---

## Task 1: Migration 007 — Schema additions

**Files:**
- Create: `backend/migrations/007_bestblogs_features.sql`

- [x] **Step 1: Create migration file**

```sql
-- 007_bestblogs_features.sql

-- Article reading metrics (word count + estimated reading minutes)
ALTER TABLE articles ADD COLUMN IF NOT EXISTS word_count INT DEFAULT 0;
ALTER TABLE articles ADD COLUMN IF NOT EXISTS reading_minutes INT DEFAULT 0;

-- Recommended feeds library (catalog only; subscription state lives in `feeds`)
CREATE TABLE IF NOT EXISTS recommended_feeds (
    id SERIAL PRIMARY KEY,
    url VARCHAR(2048) NOT NULL UNIQUE,
    title VARCHAR(500) NOT NULL,
    description TEXT,
    category VARCHAR(100) NOT NULL,        -- 'ai_eng' | 'cn_tech' | 'enterprise' | 'podcast' | 'youtube'
    language VARCHAR(10) NOT NULL,         -- 'zh' | 'en'
    feed_type VARCHAR(20) DEFAULT 'rss',   -- 'rss' | 'html' | 'youtube' | 'podcast'
    is_broken BOOLEAN DEFAULT false,       -- true if seed-time probe failed; UI shows ⚠ badge
    sort_order INT DEFAULT 0,
    created_at TIMESTAMP DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_recommended_feeds_category ON recommended_feeds(category, sort_order);

-- Weekly digest AI intro cache
CREATE TABLE IF NOT EXISTS weekly_digests (
    id SERIAL PRIMARY KEY,
    user_id INT REFERENCES users(id) ON DELETE CASCADE,
    week_start DATE NOT NULL,              -- Monday in Asia/Shanghai
    intro_text TEXT NOT NULL,
    article_ids INTEGER[] NOT NULL,        -- snapshot at generation time
    generated_at TIMESTAMP DEFAULT NOW(),
    UNIQUE(user_id, week_start)
);
```

- [x] **Step 2: Apply the migration**

Compose mounts `./backend/migrations` to `/docker-entrypoint-initdb.d`, but that only runs on **fresh** db init. For an existing db, apply manually:

```bash
docker-compose up -d postgres
docker exec -i $(docker-compose ps -q postgres) psql -U postgres -d rsspal < backend/migrations/007_bestblogs_features.sql
```

Expected output: `ALTER TABLE` × 2, `CREATE TABLE` × 2, `CREATE INDEX` × 1.

- [x] **Step 3: Verify schema**

```bash
docker exec -i $(docker-compose ps -q postgres) psql -U postgres -d rsspal -c "\d articles" -c "\d recommended_feeds" -c "\d weekly_digests"
```

Expected: `articles` shows `word_count` and `reading_minutes` columns (`integer`, default `0`); both new tables exist with the columns above.

- [x] **Step 4: Commit**

```bash
git add backend/migrations/007_bestblogs_features.sql
git commit -m "feat(db): migration 007 — article metrics, recommended_feeds, weekly_digests"
```

---

## Task 2: Reading metrics utility (Go, TDD)

**Files:**
- Create: `backend/internal/rss/metrics.go`
- Test:   `backend/internal/rss/metrics_test.go`

- [x] **Step 1: Write the failing test**

`backend/internal/rss/metrics_test.go`:

```go
package rss

import "testing"

func TestComputeMetrics_PureChinese(t *testing.T) {
	// 600 Han chars -> word_count=600, reading_minutes=2 (600/300=2)
	text := ""
	for i := 0; i < 600; i++ {
		text += "中"
	}
	wc, rm := ComputeMetrics(text)
	if wc != 600 {
		t.Errorf("word_count = %d, want 600", wc)
	}
	if rm != 2 {
		t.Errorf("reading_minutes = %d, want 2", rm)
	}
}

func TestComputeMetrics_PureEnglish(t *testing.T) {
	// 500 English words -> wc=500, rm=2 (500/250=2)
	text := ""
	for i := 0; i < 500; i++ {
		text += "word "
	}
	wc, rm := ComputeMetrics(text)
	if wc != 500 {
		t.Errorf("word_count = %d, want 500", wc)
	}
	if rm != 2 {
		t.Errorf("reading_minutes = %d, want 2", rm)
	}
}

func TestComputeMetrics_Mixed(t *testing.T) {
	// 300 zh chars + 250 en words -> wc=550, rm=max(1, round(300/300 + 250/250))=2
	text := ""
	for i := 0; i < 300; i++ {
		text += "中"
	}
	for i := 0; i < 250; i++ {
		text += " word"
	}
	wc, rm := ComputeMetrics(text)
	if wc != 550 {
		t.Errorf("word_count = %d, want 550", wc)
	}
	if rm != 2 {
		t.Errorf("reading_minutes = %d, want 2", rm)
	}
}

func TestComputeMetrics_Empty(t *testing.T) {
	wc, rm := ComputeMetrics("")
	if wc != 0 {
		t.Errorf("word_count = %d, want 0", wc)
	}
	if rm != 0 {
		t.Errorf("reading_minutes = %d, want 0", rm)
	}
}

func TestComputeMetrics_VeryShort(t *testing.T) {
	// 50 zh chars -> rm = max(1, round(50/300)) = max(1, 0) = 1 (only when wc>0)
	text := ""
	for i := 0; i < 50; i++ {
		text += "中"
	}
	wc, rm := ComputeMetrics(text)
	if wc != 50 {
		t.Errorf("word_count = %d, want 50", wc)
	}
	if rm != 1 {
		t.Errorf("reading_minutes = %d, want 1 (floor minimum)", rm)
	}
}

func TestComputeMetrics_StripsHTMLTags(t *testing.T) {
	// HTML tags should be ignored; only visible text counted.
	text := "<p>Hello <strong>world</strong></p>"
	wc, _ := ComputeMetrics(text)
	if wc != 2 {
		t.Errorf("word_count = %d, want 2 (Hello + world)", wc)
	}
}
```

- [x] **Step 2: Run test to verify it fails**

```bash
cd backend && go test ./internal/rss/ -run TestComputeMetrics -v
```

Expected: FAIL with "undefined: ComputeMetrics".

- [x] **Step 3: Implement**

`backend/internal/rss/metrics.go`:

```go
package rss

import (
	"math"
	"strings"
	"unicode"
)

// ComputeMetrics returns word_count and reading_minutes for an article body.
// Heuristic:
//   - count Han characters (CJK Unified Ideographs) as 1 each
//   - strip Han, then count whitespace-separated tokens as English words
//   - reading speed: 300 zh chars/min, 250 en words/min
//   - reading_minutes = max(1, round(zh/300 + en/250)) when word_count > 0
//
// HTML tags are stripped before counting so the same input as the AI summarizer
// (raw HTML) produces sensible numbers.
func ComputeMetrics(content string) (wordCount, readingMinutes int) {
	text := stripHTMLForMetrics(content)
	if text == "" {
		return 0, 0
	}

	zhChars := 0
	var nonHan strings.Builder
	for _, r := range text {
		if unicode.Is(unicode.Han, r) {
			zhChars++
		} else {
			nonHan.WriteRune(r)
		}
	}

	enWords := 0
	for _, w := range strings.Fields(nonHan.String()) {
		if w != "" {
			enWords++
		}
	}

	wordCount = zhChars + enWords
	if wordCount == 0 {
		return 0, 0
	}

	mins := float64(zhChars)/300.0 + float64(enWords)/250.0
	readingMinutes = int(math.Round(mins))
	if readingMinutes < 1 {
		readingMinutes = 1
	}
	return wordCount, readingMinutes
}

// stripHTMLForMetrics removes tags and collapses whitespace.
// We do not try to be perfect — this is just for character counting.
func stripHTMLForMetrics(s string) string {
	out := make([]rune, 0, len(s))
	inTag := false
	for _, r := range s {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
		case !inTag:
			out = append(out, r)
		}
	}
	return strings.TrimSpace(string(out))
}
```

- [x] **Step 4: Run test to verify it passes**

```bash
cd backend && go test ./internal/rss/ -run TestComputeMetrics -v
```

Expected: all 6 tests PASS.

- [x] **Step 5: Commit**

```bash
git add backend/internal/rss/metrics.go backend/internal/rss/metrics_test.go
git commit -m "feat(rss): add ComputeMetrics for word count and reading minutes"
```

---

## Task 3: Wire metrics into worker, repository, model, and API

**Files:**
- Modify: `backend/internal/model/model.go` (add fields to `Article`)
- Modify: `backend/internal/repository/article.go` (write/read metrics)
- Modify: `backend/cmd/worker/main.go` (compute on insert + on refetch)
- Create: `backend/cmd/backfill_metrics/main.go` (one-shot backfill)

- [x] **Step 1: Add fields to model**

In `backend/internal/model/model.go`, change the `Article` struct (find the existing block, add two fields after `FetchedAt`):

```go
type Article struct {
	ID              int        `json:"id" db:"id"`
	FeedID          int        `json:"feed_id" db:"feed_id"`
	FeedTitle       string     `json:"feed_title,omitempty" db:"feed_title"`
	Title           string     `json:"title" db:"title"`
	URL             string     `json:"url" db:"url"`
	Content         string     `json:"content" db:"content"`
	PublishedAt     *time.Time `json:"published_at" db:"published_at"`
	SummaryBrief    string     `json:"summary_brief" db:"summary_brief"`
	SummaryDetailed string     `json:"summary_detailed" db:"summary_detailed"`
	FetchedAt       time.Time  `json:"fetched_at" db:"fetched_at"`
	WordCount       int        `json:"word_count" db:"word_count"`
	ReadingMinutes  int        `json:"reading_minutes" db:"reading_minutes"`
	IsRead          bool       `json:"is_read" db:"is_read"`
}
```

- [x] **Step 2: Update repository to read/write metrics**

In `backend/internal/repository/article.go`:

a) Update `scanArticle` to read the two new columns:

```go
func (r *ArticleRepository) scanArticle(rows *sql.Rows) ([]model.Article, error) {
	var articles []model.Article
	for rows.Next() {
		var a model.Article
		var content, summaryBrief, summaryDetailed, feedTitle sql.NullString
		var isRead sql.NullBool
		err := rows.Scan(&a.ID, &a.FeedID, &a.Title, &a.URL, &content, &a.PublishedAt, &summaryBrief, &summaryDetailed, &a.FetchedAt, &a.WordCount, &a.ReadingMinutes, &feedTitle, &isRead)
		if err != nil {
			return nil, err
		}
		a.Content = content.String
		a.SummaryBrief = summaryBrief.String
		a.SummaryDetailed = summaryDetailed.String
		a.FeedTitle = feedTitle.String
		a.IsRead = isRead.Bool
		articles = append(articles, a)
	}
	return articles, nil
}
```

b) Update `scanArticleNoFeedTitle` similarly:

```go
func (r *ArticleRepository) scanArticleNoFeedTitle(rows *sql.Rows) ([]model.Article, error) {
	var articles []model.Article
	for rows.Next() {
		var a model.Article
		var content, summaryBrief, summaryDetailed sql.NullString
		err := rows.Scan(&a.ID, &a.FeedID, &a.Title, &a.URL, &content, &a.PublishedAt, &summaryBrief, &summaryDetailed, &a.FetchedAt, &a.WordCount, &a.ReadingMinutes)
		if err != nil {
			return nil, err
		}
		a.Content = content.String
		a.SummaryBrief = summaryBrief.String
		a.SummaryDetailed = summaryDetailed.String
		articles = append(articles, a)
	}
	return articles, nil
}
```

c) Add `word_count, reading_minutes` to **every** `SELECT` statement that uses these scanners. Affected queries (search the file for `articles.summary_detailed, articles.fetched_at`, `a.summary_detailed, a.fetched_at`, `summary_detailed, fetched_at`):

- `GetAll`: SELECT list — append `articles.word_count, articles.reading_minutes` after `articles.fetched_at` and **before** `feeds.title as feed_title`.
- `GetByID`: SELECT `a.word_count, a.reading_minutes` after `a.fetched_at`. Update the `Scan(...)` call to read into `&a.WordCount, &a.ReadingMinutes` between `&a.FetchedAt` and `&feedTitle`.
- `GetRecommended`: SELECT `a.word_count, a.reading_minutes` after `a.fetched_at`.
- `GetArticlesForTopicExtraction`: same.
- `GetArticlesWithoutSummary`: same (using `articles` table alias `articles` or unqualified — match existing).
- `GetArticlesWithShortContent`: same.
- `Search`: SELECT `a.word_count, a.reading_minutes` after `a.fetched_at`.

Concrete example for `GetAll`:

```go
query := `SELECT articles.id, articles.feed_id, articles.title, articles.url, articles.content, articles.published_at, articles.summary_brief, articles.summary_detailed, articles.fetched_at, articles.word_count, articles.reading_minutes, feeds.title as feed_title, COALESCE(rp.is_completed, false) as is_read
FROM articles
JOIN feeds ON articles.feed_id = feeds.id
LEFT JOIN reading_progress rp ON articles.id = rp.article_id AND rp.user_id = $1`
```

Concrete example for `GetByID`:

```go
query := `
    SELECT a.id, a.feed_id, a.title, a.url, a.content, a.published_at, a.summary_brief, a.summary_detailed, a.fetched_at, a.word_count, a.reading_minutes, f.title as feed_title
    FROM articles a
    JOIN feeds f ON a.feed_id = f.id
    WHERE a.id = $1 AND (f.owner_id IS NULL OR f.owner_id = $2)`
var a model.Article
var content, summaryBrief, summaryDetailed, feedTitle sql.NullString
err := r.db.QueryRow(query, id, userID).Scan(&a.ID, &a.FeedID, &a.Title, &a.URL, &content, &a.PublishedAt, &summaryBrief, &summaryDetailed, &a.FetchedAt, &a.WordCount, &a.ReadingMinutes, &feedTitle)
```

d) Add a new repo method to write metrics:

```go
func (r *ArticleRepository) UpdateMetrics(id, wordCount, readingMinutes int) error {
	_, err := r.db.Exec(`UPDATE articles SET word_count = $1, reading_minutes = $2 WHERE id = $3`, wordCount, readingMinutes, id)
	return err
}
```

e) Update `Create` to insert metrics computed by the caller:

```go
func (r *ArticleRepository) Create(article *model.Article) error {
	query := `INSERT INTO articles (feed_id, title, url, content, published_at, word_count, reading_minutes) VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id, fetched_at`
	return r.db.QueryRow(query, article.FeedID, article.Title, article.URL, article.Content, article.PublishedAt, article.WordCount, article.ReadingMinutes).Scan(&article.ID, &article.FetchedAt)
}
```

f) Update `UpdateContent` to also recompute metrics. Replace the existing function body:

```go
func (r *ArticleRepository) UpdateContent(id int, content string) error {
	wc, rm := computeMetricsExternal(content)
	_, err := r.db.Exec(`UPDATE articles SET content = $1, word_count = $2, reading_minutes = $3, refetch_attempts = 0 WHERE id = $4`, content, wc, rm, id)
	return err
}
```

g) At the **top of the file** (or in any new helper) wire the metrics function. Since `repository` should not import `rss` (would create a cycle if rss starts importing repository — currently it doesn't; verify), prefer keeping the SQL update in the repo and computing in the caller. **Cleaner alternative:** delete the helper call and instead change the `UpdateContent` signature:

```go
func (r *ArticleRepository) UpdateContent(id int, content string, wordCount, readingMinutes int) error {
	_, err := r.db.Exec(`UPDATE articles SET content = $1, word_count = $2, reading_minutes = $3, refetch_attempts = 0 WHERE id = $4`, content, wordCount, readingMinutes, id)
	return err
}
```

Then update **every caller** of `UpdateContent` (search the codebase) to pass metrics.

Run to find callers:

```bash
grep -rn "UpdateContent" backend/
```

Update each call site to compute metrics first and pass them:

```go
wc, rm := rss.ComputeMetrics(newContent)
articleRepo.UpdateContent(articleID, newContent, wc, rm)
```

- [x] **Step 3: Update worker to compute metrics on new article insert**

In `backend/cmd/worker/main.go`, in `processFeed` (around line 240) where the article is built before `articleRepo.Create(article)`:

```go
article := &model.Article{
    FeedID:      feed.ID,
    Title:       item.Title,
    URL:         item.Link,
    Content:     content,
    PublishedAt: parsePublishedTime(item.PublishedParsed, item.UpdatedParsed),
}
article.WordCount, article.ReadingMinutes = rss.ComputeMetrics(content)
```

Apply the same to `processHTMLFeed` (around line 295+). Search for all `articleRepo.Create` call sites and add the metrics line just before each.

- [x] **Step 4: Update worker `refetchShortContent` path to recompute metrics**

Find `refetchShortContent` (search for it in `cmd/worker/main.go`). At each `articleRepo.UpdateContent(...)` call, the new signature requires the metrics — they're already computed by you when you pass them in the new caller-computed pattern from Step 2g.

- [x] **Step 5: Build to confirm it compiles**

```bash
cd backend && go build ./...
```

Expected: no errors.

- [x] **Step 6: Create one-shot backfill cmd**

`backend/cmd/backfill_metrics/main.go`:

```go
package main

import (
	"database/sql"
	"log"

	"github.com/bytedance/rss-pal/internal/config"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/bytedance/rss-pal/internal/rss"
)

// Recomputes word_count / reading_minutes for every article whose word_count is 0
// and content is non-empty. Safe to re-run (idempotent on already-set rows).
func main() {
	cfg := config.Load()
	db, err := repository.NewDB(&cfg.Database)
	if err != nil {
		log.Fatalf("db connect: %v", err)
	}
	defer db.Close()

	rows, err := db.Query(`SELECT id, content FROM articles WHERE word_count = 0 AND content IS NOT NULL AND content != ''`)
	if err != nil {
		log.Fatalf("query: %v", err)
	}
	defer rows.Close()

	updated := 0
	for rows.Next() {
		var id int
		var content sql.NullString
		if err := rows.Scan(&id, &content); err != nil {
			log.Printf("scan: %v", err)
			continue
		}
		wc, rm := rss.ComputeMetrics(content.String)
		if _, err := db.Exec(`UPDATE articles SET word_count = $1, reading_minutes = $2 WHERE id = $3`, wc, rm, id); err != nil {
			log.Printf("update id=%d: %v", id, err)
			continue
		}
		updated++
		if updated%100 == 0 {
			log.Printf("backfilled %d articles…", updated)
		}
	}
	log.Printf("done; backfilled %d articles", updated)
}
```

- [x] **Step 7: Build the backfill cmd**

```bash
cd backend && go build -o /tmp/backfill_metrics ./cmd/backfill_metrics
```

Expected: no errors.

- [x] **Step 8: Rebuild and start api+worker, run backfill**

```bash
docker-compose up -d --build api worker
sleep 5
docker-compose exec worker /app/backfill_metrics 2>/dev/null || \
  docker-compose run --rm worker sh -c "go run ./cmd/backfill_metrics"
```

(If your worker image doesn't have the `backfill_metrics` binary baked in, the `docker-compose run` form rebuilds and runs it from source. Either way, expect logs ending with `done; backfilled N articles`.)

Alternatively run it on the host:

```bash
cd backend && DB_HOST=localhost go run ./cmd/backfill_metrics
```

- [x] **Step 9: Verify a few rows have non-zero metrics**

```bash
docker exec -i $(docker-compose ps -q postgres) psql -U postgres -d rsspal -c "SELECT id, word_count, reading_minutes FROM articles WHERE word_count > 0 LIMIT 5;"
```

Expected: 5 rows with non-zero values.

- [x] **Step 10: Commit**

```bash
git add backend/internal/model/model.go backend/internal/repository/article.go backend/cmd/worker/main.go backend/cmd/backfill_metrics/main.go
git commit -m "feat: persist word_count and reading_minutes on articles

- repository updates Create / UpdateContent / scan paths
- worker computes metrics on insert and on refetch
- one-shot backfill cmd at backend/cmd/backfill_metrics"
```

---

## Task 4: Frontend `<ReadingMeta>` and integration

**Files:**
- Modify: `frontend/src/api/client.ts` (add fields to `Article` type)
- Create: `frontend/src/components/ReadingMeta.tsx`
- Modify: `frontend/src/pages/ArticleListPage.tsx` (render meta in card)
- Modify: `frontend/src/pages/ArticlePage.tsx` (render meta in header)

- [x] **Step 1: Add fields to `Article` type**

In `frontend/src/api/client.ts`, find `export interface Article` and add two fields:

```ts
export interface Article {
  id: number
  feed_id: number
  feed_title?: string
  title: string
  url: string
  content: string
  published_at: string | null
  summary_brief: string
  summary_detailed: string
  fetched_at: string
  word_count?: number
  reading_minutes?: number
  is_read?: boolean
}
```

- [x] **Step 2: Create the component**

`frontend/src/components/ReadingMeta.tsx`:

```tsx
interface Props {
  wordCount?: number
  readingMinutes?: number
  className?: string
}

// Renders "📖 1,234 字 · 5 分钟" — silent if word_count is missing or 0.
export default function ReadingMeta({ wordCount, readingMinutes, className }: Props) {
  if (!wordCount || wordCount <= 0) return null
  const formatted = wordCount.toLocaleString('zh-CN')
  const mins = readingMinutes && readingMinutes > 0 ? readingMinutes : 1
  return (
    <span className={className} style={{ color: '#888', fontSize: 12 }}>
      📖 {formatted} 字 · {mins} 分钟
    </span>
  )
}
```

- [x] **Step 3: Render in article list cards**

In `frontend/src/pages/ArticleListPage.tsx`, locate where each article card renders its meta line (search for existing display of `feed_title` or `published_at` near the card). Add an import and a `<ReadingMeta>` next to the existing meta text.

Add the import near the top:

```tsx
import ReadingMeta from '../components/ReadingMeta'
```

In the article card JSX, find a spot in the meta row (where source / date / unread badge live) and add:

```tsx
<ReadingMeta wordCount={article.word_count} readingMinutes={article.reading_minutes} />
```

(Pick the meta row that already has `gap` styling so it renders inline.)

- [x] **Step 4: Render in article detail header**

In `frontend/src/pages/ArticlePage.tsx`, near the top of the rendered article (where title and date display), add the same component:

```tsx
import ReadingMeta from '../components/ReadingMeta'
// ...
<ReadingMeta wordCount={article.word_count} readingMinutes={article.reading_minutes} />
```

- [x] **Step 5: Type-check the frontend**

```bash
cd frontend && npm run build
```

Expected: build succeeds with no TypeScript errors.

- [x] **Step 6: Manual browser verification**

```bash
cd frontend && npm run dev
```

Open `http://localhost:5173/articles`, log in, scroll the list. Expected: each article card shows `📖 N 字 · M 分钟`. Articles without computed metrics (none, since backfill ran) show no meta.

- [x] **Step 7: Commit**

```bash
git add frontend/src/api/client.ts frontend/src/components/ReadingMeta.tsx frontend/src/pages/ArticleListPage.tsx frontend/src/pages/ArticlePage.tsx
git commit -m "feat(frontend): show word count and reading minutes on article cards"
```

---

## Task 5: Add RSSHub container to docker-compose

**Files:**
- Modify: `docker-compose.yml`

- [x] **Step 1: Append the rsshub service**

In `docker-compose.yml`, add this service block (between `worker` and `frontend`, or anywhere within `services:`):

```yaml
  rsshub:
    image: diygod/rsshub:latest
    restart: unless-stopped
    environment:
      NODE_ENV: production
      CACHE_TYPE: memory
      CACHE_EXPIRE: 3600
      REQUEST_TIMEOUT: 15000
    # No port exposure — only api/worker reach it via http://rsshub:1200 on the compose network
```

- [x] **Step 2: Bring it up**

```bash
docker-compose up -d rsshub
```

Expected: `rsshub` shows healthy in `docker-compose ps`.

- [x] **Step 3: Verify reachability from worker**

```bash
docker-compose exec worker sh -c "wget -qO- http://rsshub:1200/test/1 | head -c 500"
```

Expected: a small RSS XML response starting with `<?xml`. If `wget` isn't in the worker image, try the api container:

```bash
docker-compose exec api sh -c "wget -qO- http://rsshub:1200/test/1 | head -c 500"
```

(Both alpine-based images normally have busybox `wget`.)

- [x] **Step 4: Commit**

```bash
git add docker-compose.yml
git commit -m "infra: add self-hosted rsshub container for non-RSS sources"
```

---

## Task 6: Worker handles `youtube` / `podcast` feed_type

**Why:** YouTube channel RSS and podcast RSS only carry video/episode metadata — no article body. The existing "short content reFetch" loop would otherwise try repeatedly to scrape these and fail.

**Files:**
- Modify: `backend/cmd/worker/main.go`
- Modify: `backend/internal/repository/article.go` (skip `youtube`/`podcast` in `GetArticlesWithShortContent`)

- [x] **Step 1: Skip deep content fetch in `processFeed`**

In `cmd/worker/main.go`, inside `processFeed`, where today the code calls `contentFetcher.FetchContent(ctx, item.Link)` (around line 230), wrap it in a feed-type guard:

```go
content := rss.StripHTML(item.Description)
if content == "" {
    content = rss.StripHTML(item.Content)
}

skipDeepFetch := feed.FeedType == "youtube" || feed.FeedType == "podcast"
if !skipDeepFetch && item.Link != "" {
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

(Keep the existing else-branch behavior; just guard the deep fetch.)

- [x] **Step 2: Skip refetch loop for these feed types**

In `internal/repository/article.go`, update `GetArticlesWithShortContent` so it joins `feeds` and excludes `youtube`/`podcast`:

```go
func (r *ArticleRepository) GetArticlesWithShortContent(minLength int) ([]model.Article, error) {
	query := `
		SELECT a.id, a.feed_id, a.title, a.url, a.content, a.published_at, a.summary_brief, a.summary_detailed, a.fetched_at, a.word_count, a.reading_minutes
		FROM articles a
		JOIN feeds f ON a.feed_id = f.id
		WHERE a.url != '' AND a.refetch_attempts < 5
		  AND f.feed_type NOT IN ('youtube', 'podcast')
		  AND ((LENGTH(a.content) < $1 OR a.content IS NULL AND a.fetched_at > NOW() - INTERVAL '7 days')
		       OR (a.content LIKE '%<%>%' AND a.fetched_at > NOW() - INTERVAL '30 days'))
		ORDER BY a.fetched_at DESC
		LIMIT 50
	`
	rows, err := r.db.Query(query, minLength)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return r.scanArticleNoFeedTitle(rows)
}
```

- [x] **Step 3: Build**

```bash
cd backend && go build ./...
```

Expected: no errors.

- [x] **Step 4: Commit**

```bash
git add backend/cmd/worker/main.go backend/internal/repository/article.go
git commit -m "feat(worker): skip deep content fetch for youtube/podcast feed types"
```

---

## Task 7: Recommended feeds repository + API

**Files:**
- Create: `backend/internal/model/recommended.go`
- Create: `backend/internal/repository/recommended.go`
- Create: `backend/internal/api/recommended.go`
- Modify: `backend/cmd/server/main.go` (wire the handler)

- [x] **Step 1: Model**

`backend/internal/model/recommended.go`:

```go
package model

import "time"

type RecommendedFeed struct {
	ID          int       `json:"id" db:"id"`
	URL         string    `json:"url" db:"url"`
	Title       string    `json:"title" db:"title"`
	Description string    `json:"description" db:"description"`
	Category    string    `json:"category" db:"category"`     // ai_eng | cn_tech | enterprise | podcast | youtube
	Language    string    `json:"language" db:"language"`     // zh | en
	FeedType    string    `json:"feed_type" db:"feed_type"`   // rss | html | youtube | podcast
	IsBroken    bool      `json:"is_broken" db:"is_broken"`
	SortOrder   int       `json:"sort_order" db:"sort_order"`
	Subscribed  bool      `json:"subscribed"`                 // computed at read time
	CreatedAt   time.Time `json:"created_at" db:"created_at"`
}
```

- [x] **Step 2: Repository**

`backend/internal/repository/recommended.go`:

```go
package repository

import (
	"database/sql"

	"github.com/bytedance/rss-pal/internal/model"
)

type RecommendedFeedRepository struct {
	db *sql.DB
}

func NewRecommendedFeedRepository(db *sql.DB) *RecommendedFeedRepository {
	return &RecommendedFeedRepository{db: db}
}

// ListWithSubscriptionStatus returns the catalog with `subscribed = true` when
// the URL already exists in `feeds` (regardless of owner), so the UI can show
// a "✓ 已订阅" badge for shared seeds and the user's own feeds alike.
func (r *RecommendedFeedRepository) ListWithSubscriptionStatus(userID int) ([]model.RecommendedFeed, error) {
	rows, err := r.db.Query(`
		SELECT rf.id, rf.url, rf.title, rf.description, rf.category, rf.language, rf.feed_type, rf.is_broken, rf.sort_order, rf.created_at,
		       (f.id IS NOT NULL) AS subscribed
		FROM recommended_feeds rf
		LEFT JOIN feeds f ON f.url = rf.url AND (f.owner_id IS NULL OR f.owner_id = $1)
		ORDER BY rf.category, rf.sort_order, rf.id
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []model.RecommendedFeed
	for rows.Next() {
		var rf model.RecommendedFeed
		var description sql.NullString
		if err := rows.Scan(&rf.ID, &rf.URL, &rf.Title, &description, &rf.Category, &rf.Language, &rf.FeedType, &rf.IsBroken, &rf.SortOrder, &rf.CreatedAt, &rf.Subscribed); err != nil {
			return nil, err
		}
		rf.Description = description.String
		out = append(out, rf)
	}
	return out, nil
}

func (r *RecommendedFeedRepository) GetByID(id int) (*model.RecommendedFeed, error) {
	var rf model.RecommendedFeed
	var description sql.NullString
	err := r.db.QueryRow(`
		SELECT id, url, title, description, category, language, feed_type, is_broken, sort_order, created_at
		FROM recommended_feeds WHERE id = $1
	`, id).Scan(&rf.ID, &rf.URL, &rf.Title, &description, &rf.Category, &rf.Language, &rf.FeedType, &rf.IsBroken, &rf.SortOrder, &rf.CreatedAt)
	if err != nil {
		return nil, err
	}
	rf.Description = description.String
	return &rf, nil
}
```

- [x] **Step 3: API handler**

`backend/internal/api/recommended.go`:

```go
package api

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"

	"github.com/bytedance/rss-pal/internal/model"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/gin-gonic/gin"
)

type RecommendedHandler struct {
	repo     *repository.RecommendedFeedRepository
	feedRepo *repository.FeedRepository
}

func NewRecommendedHandler(repo *repository.RecommendedFeedRepository, feedRepo *repository.FeedRepository) *RecommendedHandler {
	return &RecommendedHandler{repo: repo, feedRepo: feedRepo}
}

func (h *RecommendedHandler) List(c *gin.Context) {
	userID := getUserID(c)
	items, err := h.repo.ListWithSubscriptionStatus(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, items)
}

// Subscribe inserts the recommended feed into `feeds` with the current user as
// owner. If the URL already exists (someone else's, or admin's shared seed),
// returns 200 idempotent so the UI can stay simple.
func (h *RecommendedHandler) Subscribe(c *gin.Context) {
	userID := getUserID(c)
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	rf, err := h.repo.GetByID(id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, gin.H{"error": "推荐源不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if rf.IsBroken {
		c.JSON(http.StatusBadRequest, gin.H{"error": "该来源当前不可用"})
		return
	}

	uid := userID
	feed := &model.Feed{
		URL:              rf.URL,
		Title:            rf.Title,
		FetchIntervalMin: 60,
		OwnerID:          &uid,
		FeedType:         rf.FeedType,
	}
	if err := h.feedRepo.Create(feed); err != nil {
		// Likely UNIQUE conflict on url. Treat as idempotent.
		c.JSON(http.StatusOK, gin.H{"status": "already_subscribed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "subscribed", "feed_id": feed.ID})
}
```

- [x] **Step 4: Wire into the server**

In `backend/cmd/server/main.go`:

a) Below the existing `templateRepo := repository.NewTemplateRepository(db)` line, add:

```go
recommendedRepo := repository.NewRecommendedFeedRepository(db)
```

b) Below the existing `insightsHandler := api.NewInsightsHandler(...)` line, add:

```go
recommendedHandler := api.NewRecommendedHandler(recommendedRepo, feedRepo)
```

c) Inside the protected route group (after `apiGroup.POST("/insights/generate", insightsHandler.Generate)`), add:

```go
// Recommended feeds (catalog)
apiGroup.GET("/recommended-feeds", recommendedHandler.List)
apiGroup.POST("/recommended-feeds/:id/subscribe", recommendedHandler.Subscribe)
```

- [x] **Step 5: Build**

```bash
cd backend && go build ./...
```

Expected: no errors.

- [x] **Step 6: Smoke test (empty list returns `[]` or `null`)**

```bash
docker-compose up -d --build api
sleep 5
TOKEN=$(curl -s -X POST localhost:8080/api/auth/login -H 'Content-Type: application/json' -d '{"username":"admin","password":"admin"}' | grep -o '"token":"[^"]*' | cut -d'"' -f4)
curl -s -H "Authorization: Bearer $TOKEN" localhost:8080/api/recommended-feeds
```

Expected: `null` or `[]` (catalog still empty until Task 9 seeds it).

- [x] **Step 7: Commit**

```bash
git add backend/internal/model/recommended.go backend/internal/repository/recommended.go backend/internal/api/recommended.go backend/cmd/server/main.go
git commit -m "feat(api): recommended feeds catalog and subscribe endpoint"
```

---

## Task 8: Recommended feeds page (frontend)

**Files:**
- Modify: `frontend/src/api/client.ts` (add types and calls)
- Create: `frontend/src/pages/RecommendedPage.tsx`
- Modify: `frontend/src/App.tsx` (route)

- [x] **Step 1: Add types and API calls in `client.ts`**

Append to `frontend/src/api/client.ts`:

```ts
export interface RecommendedFeed {
  id: number
  url: string
  title: string
  description: string
  category: string                // 'ai_eng' | 'cn_tech' | 'enterprise' | 'podcast' | 'youtube'
  language: string                // 'zh' | 'en'
  feed_type: string
  is_broken: boolean
  sort_order: number
  subscribed: boolean
  created_at: string
}

export const getRecommendedFeeds = () =>
  api.get<RecommendedFeed[]>('/recommended-feeds').then(res => res.data || [])

export const subscribeRecommendedFeed = (id: number) =>
  api.post<{ status: string; feed_id?: number }>(`/recommended-feeds/${id}/subscribe`).then(res => res.data)
```

- [x] **Step 2: Build the page**

`frontend/src/pages/RecommendedPage.tsx`:

```tsx
import { useEffect, useState } from 'react'
import { getRecommendedFeeds, subscribeRecommendedFeed, RecommendedFeed } from '../api/client'

const CATEGORY_LABELS: Record<string, string> = {
  ai_eng: 'AI 工程',
  cn_tech: '中文科技',
  enterprise: '企业基建',
  podcast: '播客',
  youtube: '视频',
}
const CATEGORY_ORDER = ['ai_eng', 'cn_tech', 'enterprise', 'youtube', 'podcast']

export default function RecommendedPage() {
  const [items, setItems] = useState<RecommendedFeed[]>([])
  const [loading, setLoading] = useState(true)
  const [busyId, setBusyId] = useState<number | null>(null)

  useEffect(() => { load() }, [])

  const load = async () => {
    setLoading(true)
    try {
      const data = await getRecommendedFeeds()
      setItems(data)
    } finally {
      setLoading(false)
    }
  }

  const handleSubscribe = async (id: number) => {
    setBusyId(id)
    try {
      await subscribeRecommendedFeed(id)
      await load()
    } catch (e: any) {
      alert(e?.response?.data?.error || '订阅失败')
    } finally {
      setBusyId(null)
    }
  }

  if (loading) return <div className="card">加载中…</div>

  const grouped: Record<string, RecommendedFeed[]> = {}
  for (const it of items) {
    if (!grouped[it.category]) grouped[it.category] = []
    grouped[it.category].push(it)
  }

  return (
    <div>
      <h2 style={{ marginBottom: 16 }}>推荐订阅</h2>
      <p className="text-muted text-sm" style={{ marginBottom: 16 }}>
        以下是从 bestblogs.dev 精选的高质量来源。点击「订阅」即可添加到你的订阅列表。
      </p>
      {CATEGORY_ORDER.filter(c => grouped[c]?.length).map(cat => (
        <section key={cat} style={{ marginBottom: 24 }}>
          <h3 style={{ fontSize: 16, fontWeight: 600, marginBottom: 8 }}>
            {CATEGORY_LABELS[cat] || cat}
          </h3>
          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(280px, 1fr))', gap: 12 }}>
            {grouped[cat].map(rf => (
              <div key={rf.id} className="card" style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
                <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', gap: 8 }}>
                  <strong style={{ fontSize: 14 }}>{rf.title}</strong>
                  <span style={{ fontSize: 11, padding: '2px 6px', background: '#f0f0f0', borderRadius: 4 }}>
                    {rf.language === 'zh' ? '中文' : 'English'}
                  </span>
                </div>
                {rf.description && (
                  <div className="text-muted text-sm" style={{ fontSize: 12 }}>{rf.description}</div>
                )}
                <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginTop: 'auto' }}>
                  {rf.is_broken ? (
                    <span style={{ fontSize: 12, color: '#c33' }}>⚠ 当前路由不可用</span>
                  ) : rf.subscribed ? (
                    <span style={{ fontSize: 12, color: '#28a745' }}>✓ 已订阅</span>
                  ) : (
                    <button
                      onClick={() => handleSubscribe(rf.id)}
                      disabled={busyId === rf.id}
                      style={{ padding: '4px 10px', fontSize: 12 }}
                    >
                      {busyId === rf.id ? '订阅中…' : '订阅'}
                    </button>
                  )}
                </div>
              </div>
            ))}
          </div>
        </section>
      ))}
      {items.length === 0 && (
        <div className="card text-muted">暂无推荐源,请等待管理员配置。</div>
      )}
    </div>
  )
}
```

- [x] **Step 3: Add route in `App.tsx`**

In `frontend/src/App.tsx`:

a) Add the import:

```tsx
import RecommendedPage from './pages/RecommendedPage'
```

b) Inside the `<Route element={<RequireAuth ...>}>` block, add a new route:

```tsx
<Route path="recommended" element={<RecommendedPage />} />
```

- [x] **Step 4: Type-check**

```bash
cd frontend && npm run build
```

Expected: no TypeScript errors.

- [x] **Step 5: Commit**

```bash
git add frontend/src/api/client.ts frontend/src/pages/RecommendedPage.tsx frontend/src/App.tsx
git commit -m "feat(frontend): recommended feeds catalog page"
```

---

## Task 9: Seed cmd — populate `recommended_feeds` and admin's shared `feeds`

**Goal:** One-shot script that:
1. Probes each seed URL (HTTP GET, follow redirects, 10s timeout)
2. Inserts the row into `recommended_feeds` regardless (sets `is_broken=true` if probe failed)
3. For probes that succeed: also inserts into `feeds` with `owner_id=NULL` (shared/visible to all users) using `ON CONFLICT (url) DO NOTHING`
4. Prints a JSON report and a per-source pass/fail summary

**Files:**
- Create: `backend/cmd/seed/main.go`

- [x] **Step 1: Write the seed cmd**

`backend/cmd/seed/main.go`:

```go
package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/bytedance/rss-pal/internal/config"
	"github.com/bytedance/rss-pal/internal/repository"
)

type seedFeed struct {
	URL         string `json:"url"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Category    string `json:"category"`
	Language    string `json:"language"`
	FeedType    string `json:"feed_type"`
	SortOrder   int    `json:"sort_order"`
}

// 12 sources, curated from bestblogs.dev Issue #93.
// RSSHub-routed URLs assume the rsshub container is reachable at http://rsshub:1200.
// When running this seed from the host machine, override RSSHUB_BASE env var,
// e.g. RSSHUB_BASE=http://localhost:1200 (after exposing the port temporarily)
// or simply trust the current values (they'll be probed-and-recorded; broken
// ones get is_broken=true and can be retried later).
var seeds = []seedFeed{
	{URL: "https://openai.com/news/rss.xml", Title: "OpenAI Blog", Description: "OpenAI 官方博客", Category: "ai_eng", Language: "en", FeedType: "rss", SortOrder: 1},
	{URL: "https://www.anthropic.com/news/feed.xml", Title: "Anthropic News", Description: "Anthropic / Claude 官方更新", Category: "ai_eng", Language: "en", FeedType: "rss", SortOrder: 2},
	{URL: "https://blog.cloudflare.com/rss/", Title: "Cloudflare Blog", Description: "Cloudflare 工程博客", Category: "enterprise", Language: "en", FeedType: "rss", SortOrder: 1},
	{URL: "https://feed.infoq.com/", Title: "InfoQ", Description: "Software architecture and engineering", Category: "enterprise", Language: "en", FeedType: "rss", SortOrder: 2},
	{URL: "https://baoyu.io/feed.xml", Title: "宝玉的分享", Description: "宝玉的 AI / 工程笔记", Category: "ai_eng", Language: "zh", FeedType: "rss", SortOrder: 10},
	{URL: "https://www.qbitai.com/feed", Title: "量子位", Description: "AI 行业新闻与解读", Category: "cn_tech", Language: "zh", FeedType: "rss", SortOrder: 1},
	{URL: "https://www.youtube.com/feeds/videos.xml?user=SequoiaCapital", Title: "Sequoia Capital (YouTube)", Description: "红杉资本访谈与分享", Category: "ai_eng", Language: "en", FeedType: "youtube", SortOrder: 20},
	{URL: "https://www.youtube.com/feeds/videos.xml?user=ycombinator", Title: "Y Combinator (YouTube)", Description: "YC 创业者访谈", Category: "ai_eng", Language: "en", FeedType: "youtube", SortOrder: 21},
	{URL: "https://www.youtube.com/feeds/videos.xml?channel_id=UCFzCkTM2OmkSHcrZftJa9-w", Title: "AI Engineer (YouTube)", Description: "AI Engineer summit & talks", Category: "ai_eng", Language: "en", FeedType: "youtube", SortOrder: 22},
	{URL: "https://addyo.substack.com/feed", Title: "Addy Osmani", Description: "Software engineering essays", Category: "ai_eng", Language: "en", FeedType: "rss", SortOrder: 30},
	{URL: rsshubURL("/wechat/ce/MzI5MDA1MDU4MA=="), Title: "腾讯技术工程", Description: "腾讯技术工程 公众号(via RSSHub)", Category: "cn_tech", Language: "zh", FeedType: "rss", SortOrder: 50},
	{URL: rsshubURL("/xiaoyuzhou/podcast/6388ea5cb164f9c40c2cdb40"), Title: "小宇宙播客", Description: "中文 AI / 工程主题播客(via RSSHub)", Category: "podcast", Language: "zh", FeedType: "podcast", SortOrder: 60},
}

func rsshubURL(route string) string {
	base := os.Getenv("RSSHUB_BASE")
	if base == "" {
		base = "http://rsshub:1200"
	}
	return base + route
}

type report struct {
	URL          string `json:"url"`
	Title        string `json:"title"`
	HTTPStatus   int    `json:"http_status"`
	ProbeError   string `json:"probe_error,omitempty"`
	IsBroken     bool   `json:"is_broken"`
	WroteFeeds   bool   `json:"wrote_feeds"`
	WroteCatalog bool   `json:"wrote_catalog"`
}

func probe(url string) (int, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", "rss-pal-seed/1.0")
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	return resp.StatusCode, nil
}

func main() {
	cfg := config.Load()
	db, err := repository.NewDB(&cfg.Database)
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer db.Close()

	reports := make([]report, 0, len(seeds))

	for _, s := range seeds {
		r := report{URL: s.URL, Title: s.Title}

		status, err := probe(s.URL)
		r.HTTPStatus = status
		if err != nil {
			r.ProbeError = err.Error()
			r.IsBroken = true
		} else if status < 200 || status >= 400 {
			r.IsBroken = true
		}

		// Always upsert into recommended_feeds catalog.
		_, errC := db.Exec(`
			INSERT INTO recommended_feeds (url, title, description, category, language, feed_type, is_broken, sort_order)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			ON CONFLICT (url) DO UPDATE SET
				title = EXCLUDED.title,
				description = EXCLUDED.description,
				category = EXCLUDED.category,
				language = EXCLUDED.language,
				feed_type = EXCLUDED.feed_type,
				is_broken = EXCLUDED.is_broken,
				sort_order = EXCLUDED.sort_order
		`, s.URL, s.Title, s.Description, s.Category, s.Language, s.FeedType, r.IsBroken, s.SortOrder)
		if errC != nil {
			log.Printf("catalog upsert failed for %s: %v", s.URL, errC)
		} else {
			r.WroteCatalog = true
		}

		// If healthy, also seed into feeds (shared, owner_id=NULL).
		if !r.IsBroken {
			_, errF := db.Exec(`
				INSERT INTO feeds (url, title, fetch_interval_minutes, owner_id, feed_type, is_active)
				VALUES ($1, $2, 60, NULL, $3, true)
				ON CONFLICT (url) DO NOTHING
			`, s.URL, s.Title, s.FeedType)
			if errF != nil {
				log.Printf("feeds insert failed for %s: %v", s.URL, errF)
			} else {
				r.WroteFeeds = true
			}
		}

		reports = append(reports, r)
		log.Printf("[%s] status=%d broken=%v feed=%v",
			s.URL, r.HTTPStatus, r.IsBroken, r.WroteFeeds)
	}

	// Print JSON report at the end so it's easy to grep.
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(reports)

	// Sanity: count what we wrote.
	var ok, broken int
	_ = db.QueryRow(`SELECT COUNT(*) FROM recommended_feeds WHERE is_broken=false`).Scan(&ok)
	_ = db.QueryRow(`SELECT COUNT(*) FROM recommended_feeds WHERE is_broken=true`).Scan(&broken)
	log.Printf("seed complete: %d healthy, %d broken", ok, broken)
}
```

- [x] **Step 2: Build**

```bash
cd backend && go build ./...
```

Expected: no errors.

- [x] **Step 3: Run the seed**

Easiest: from inside the worker container so it's on the rsshub network:

```bash
docker-compose up -d rsshub postgres
docker-compose run --rm worker sh -c "cd /app && go run ./cmd/seed"
```

If the worker image lacks Go (production image), run on the host with overrides:

```bash
cd backend && DB_HOST=localhost RSSHUB_BASE=http://localhost:1200 go run ./cmd/seed
```

(Temporarily expose RSSHub port: add `ports: ["1200:1200"]` to the rsshub service if running this way, then remove afterward.)

Expected output: a JSON array of 12 reports, then a final `seed complete: X healthy, Y broken` line. WeChat (#11) and the podcast (#12) are most likely the broken ones; YouTube (#7-9) and Anthropic (#2) may also fail depending on current routes — that's fine, they get `is_broken=true` and are visible only in the catalog.

- [x] **Step 4: Verify rows landed**

```bash
docker exec -i $(docker-compose ps -q postgres) psql -U postgres -d rsspal -c "SELECT category, count(*) FROM recommended_feeds GROUP BY category;"
docker exec -i $(docker-compose ps -q postgres) psql -U postgres -d rsspal -c "SELECT url, is_broken FROM recommended_feeds ORDER BY id;"
docker exec -i $(docker-compose ps -q postgres) psql -U postgres -d rsspal -c "SELECT count(*) FROM feeds WHERE owner_id IS NULL;"
```

Expected: 12 rows in `recommended_feeds` across the 5 categories; >=8 healthy rows in `feeds` with `owner_id IS NULL`.

- [x] **Step 5: Investigate any unexpected breakage**

For any source you expected to work but came back `is_broken=true`, do a manual probe:

```bash
docker-compose exec worker sh -c "wget -qO- --timeout=10 '<the-url>' | head -c 500"
```

If RSS source is genuinely down, leave as broken. If it's a transient network issue, re-run the seed (it's idempotent). For RSSHub microsoft/wechat routes that fail, search for alternatives at `https://docs.rsshub.app/` and update the `seeds` slice; re-run.

- [x] **Step 6: Commit**

```bash
git add backend/cmd/seed/main.go
git commit -m "feat: seed cmd populates recommended_feeds catalog and shared feeds

- probes each URL; broken sources stay in catalog with is_broken=true
- successful ones are inserted into feeds with owner_id=NULL (shared)
- ON CONFLICT idempotent; safe to re-run after fixing bad routes"
```

- [x] **Step 7: Verify worker picks up seeded feeds**

```bash
docker-compose restart worker
sleep 90  # one fetch cycle
docker-compose logs --tail=50 worker | grep -E "Fetching feed|new articles"
```

Expected: logs show `Fetching feed: https://openai.com/...` etc., and `Feed ... fetched, N new articles` lines.

---

## Task 10: Weekly digest API + AI prompt

**Files:**
- Create: `backend/internal/ai/weekly_digest.go`
- Create: `backend/internal/repository/weekly_digest.go`
- Create: `backend/internal/api/weekly.go`
- Modify: `backend/cmd/server/main.go`

- [x] **Step 1: AI prompt for the weekly intro**

`backend/internal/ai/weekly_digest.go`:

```go
package ai

import (
	"context"
	"fmt"
	"strings"
)

// GenerateWeeklyIntro produces a Chinese 150-200 word intro that frames the
// theme of the week given the titles + brief summaries of the top articles.
// Returns "" + nil when articles is empty.
func (s *Summarizer) GenerateWeeklyIntro(ctx context.Context, articles []WeeklyDigestItem) (string, error) {
	if len(articles) == 0 {
		return "", nil
	}

	var b strings.Builder
	for i, a := range articles {
		fmt.Fprintf(&b, "%d. 《%s》\n   摘要：%s\n\n", i+1, a.Title, truncateContent(a.SummaryBrief))
	}

	prompt := `以下是本周精选的若干篇文章的标题和摘要：

` + b.String() + `请用 150-200 字的中文写一段「本周主题导语」，回答这个问题：
「为什么这一周值得读者关注？这些文章共同指向什么趋势或思考？」

要求：
- 不要逐篇复述；要从中提炼出共同主题、张力或对比。
- 给读者一个清晰的「为什么这周值得关注」视角。
- 语气专业、克制，避免营销化措辞。
- 直接输出导语正文，不要标题、不要 Markdown、不要分点列表。`

	return s.call(ctx, prompt, 600)
}

// WeeklyDigestItem is the minimum payload the prompt needs.
type WeeklyDigestItem struct {
	Title        string
	SummaryBrief string
}
```

- [x] **Step 2: Repository for digest cache**

`backend/internal/repository/weekly_digest.go`:

```go
package repository

import (
	"database/sql"
	"errors"
	"time"

	"github.com/lib/pq"
)

type WeeklyDigest struct {
	UserID      int
	WeekStart   time.Time
	IntroText   string
	ArticleIDs  []int64
	GeneratedAt time.Time
}

type WeeklyDigestRepository struct {
	db *sql.DB
}

func NewWeeklyDigestRepository(db *sql.DB) *WeeklyDigestRepository {
	return &WeeklyDigestRepository{db: db}
}

func (r *WeeklyDigestRepository) Get(userID int, weekStart time.Time) (*WeeklyDigest, error) {
	var d WeeklyDigest
	var ids pq.Int64Array
	err := r.db.QueryRow(`
		SELECT user_id, week_start, intro_text, article_ids, generated_at
		FROM weekly_digests WHERE user_id = $1 AND week_start = $2
	`, userID, weekStart).Scan(&d.UserID, &d.WeekStart, &d.IntroText, &ids, &d.GeneratedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	d.ArticleIDs = ids
	return &d, nil
}

func (r *WeeklyDigestRepository) Upsert(userID int, weekStart time.Time, intro string, articleIDs []int) error {
	ids := make(pq.Int64Array, len(articleIDs))
	for i, id := range articleIDs {
		ids[i] = int64(id)
	}
	_, err := r.db.Exec(`
		INSERT INTO weekly_digests (user_id, week_start, intro_text, article_ids)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (user_id, week_start) DO UPDATE SET
			intro_text = EXCLUDED.intro_text,
			article_ids = EXCLUDED.article_ids,
			generated_at = NOW()
	`, userID, weekStart, intro, ids)
	return err
}
```

- [x] **Step 3: Top-articles-of-the-week query**

Add this method to `backend/internal/repository/article.go`. **Also add `"github.com/lib/pq"` to the file's import block** if it's not already there (needed for `pq.Int64Array`).

```go
// GetByIDsForUser fetches the given article IDs in the order they appear in
// `ids`. Used by the weekly digest to honor the "frozen snapshot" semantic:
// once a digest is generated for a week, the article set is locked.
func (r *ArticleRepository) GetByIDsForUser(userID int, ids []int) ([]model.Article, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	int64s := make(pq.Int64Array, len(ids))
	for i, id := range ids {
		int64s[i] = int64(id)
	}
	query := `
		SELECT a.id, a.feed_id, a.title, a.url, a.content, a.published_at, a.summary_brief, a.summary_detailed, a.fetched_at, a.word_count, a.reading_minutes, f.title as feed_title, COALESCE(rp.is_completed, false) as is_read
		FROM articles a
		JOIN feeds f ON a.feed_id = f.id
		LEFT JOIN reading_progress rp ON a.id = rp.article_id AND rp.user_id = $1
		WHERE a.id = ANY($2) AND (f.owner_id IS NULL OR f.owner_id = $1)
		ORDER BY array_position($2, a.id)
	`
	rows, err := r.db.Query(query, userID, int64s)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return r.scanArticle(rows)
}

// GetTopArticlesInRange returns up to `limit` articles from feeds visible to
// `userID` whose published_at falls in [start, end). Ranks by personalization
// score (mirrors GetRecommended), tie-breaking by published_at desc. Falls
// back to recency for users with no preference signals.
func (r *ArticleRepository) GetTopArticlesInRange(userID int, start, end time.Time, limit int) ([]model.Article, error) {
	query := `
		SELECT a.id, a.feed_id, a.title, a.url, a.content, a.published_at, a.summary_brief, a.summary_detailed, a.fetched_at, a.word_count, a.reading_minutes, f.title as feed_title, COALESCE(rp.is_completed, false) as is_read
		FROM articles a
		JOIN feeds f ON a.feed_id = f.id
		LEFT JOIN reading_progress rp ON a.id = rp.article_id AND rp.user_id = $1
		LEFT JOIN (
			SELECT article_id, SUM(
				CASE signal_type
					WHEN 'like' THEN 5.0 * signal_value
					WHEN 'dislike' THEN -10.0 * signal_value
					WHEN 'save' THEN 3.0 * signal_value
					WHEN 'read_duration' THEN signal_value / 60.0
					ELSE 1.0 * signal_value
				END
			) as score
			FROM user_preferences
			WHERE user_id = $1
			GROUP BY article_id
		) p ON a.id = p.article_id
		WHERE (f.owner_id IS NULL OR f.owner_id = $1)
		  AND a.published_at >= $2 AND a.published_at < $3
		ORDER BY COALESCE(p.score, 0) DESC, a.published_at DESC
		LIMIT $4
	`
	rows, err := r.db.Query(query, userID, start, end, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return r.scanArticle(rows)
}
```

- [x] **Step 4: Weekly digest API handler**

`backend/internal/api/weekly.go`:

```go
package api

import (
	"net/http"
	"time"

	"github.com/bytedance/rss-pal/internal/ai"
	"github.com/bytedance/rss-pal/internal/model"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/gin-gonic/gin"
)

type WeeklyHandler struct {
	articleRepo *repository.ArticleRepository
	digestRepo  *repository.WeeklyDigestRepository
	summarizer  *ai.Summarizer
}

func NewWeeklyHandler(articleRepo *repository.ArticleRepository, digestRepo *repository.WeeklyDigestRepository, summarizer *ai.Summarizer) *WeeklyHandler {
	return &WeeklyHandler{articleRepo: articleRepo, digestRepo: digestRepo, summarizer: summarizer}
}

// shanghai is fixed (UTC+8, no DST). Hardcoded so we don't depend on the
// container's tzdata being present.
var shanghai = time.FixedZone("Asia/Shanghai", 8*3600)

// startOfWeek returns the Monday 00:00 in Asia/Shanghai for the calendar week
// containing `t`.
func startOfWeek(t time.Time) time.Time {
	t = t.In(shanghai)
	// Monday is weekday 1 in Go's iso convention via int(time.Weekday())+6%7
	weekday := int(t.Weekday())
	if weekday == 0 {
		weekday = 7
	}
	monday := t.AddDate(0, 0, -(weekday - 1))
	return time.Date(monday.Year(), monday.Month(), monday.Day(), 0, 0, 0, 0, shanghai)
}

func (h *WeeklyHandler) Get(c *gin.Context) {
	userID := getUserID(c)

	weekStart := startOfWeek(time.Now())
	if w := c.Query("week"); w != "" {
		parsed, err := time.ParseInLocation("2006-01-02", w, shanghai)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "week 必须是 YYYY-MM-DD 格式"})
			return
		}
		weekStart = startOfWeek(parsed)
	}
	weekEnd := weekStart.AddDate(0, 0, 7)

	// Cache lookup first — if present, honor the frozen article snapshot.
	cached, _ := h.digestRepo.Get(userID, weekStart)

	var (
		articles []model.Article
		intro    string
	)
	if cached != nil {
		ids := make([]int, len(cached.ArticleIDs))
		for i, id := range cached.ArticleIDs {
			ids[i] = int(id)
		}
		articles, _ = h.articleRepo.GetByIDsForUser(userID, ids)
		intro = cached.IntroText
	} else {
		var err error
		articles, err = h.articleRepo.GetTopArticlesInRange(userID, weekStart, weekEnd, 10)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}

	if cached == nil && len(articles) > 0 && h.summarizer != nil {
		items := make([]ai.WeeklyDigestItem, 0, len(articles))
		for _, a := range articles {
			items = append(items, ai.WeeklyDigestItem{Title: a.Title, SummaryBrief: a.SummaryBrief})
		}
		generated, gerr := h.summarizer.GenerateWeeklyIntro(c.Request.Context(), items)
		if gerr == nil && generated != "" {
			intro = generated
			ids := make([]int, 0, len(articles))
			for _, a := range articles {
				ids = append(ids, a.ID)
			}
			if uerr := h.digestRepo.Upsert(userID, weekStart, intro, ids); uerr != nil {
				// Non-fatal: just log via response header
				c.Writer.Header().Set("X-Digest-Cache-Error", uerr.Error())
			}
		} else if gerr != nil {
			// AI failed; spec says respond with empty intro + articles.
			c.Writer.Header().Set("X-Digest-AI-Error", gerr.Error())
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"week_start": weekStart.Format("2006-01-02"),
		"intro_text": intro,
		"articles":   articles,
	})
}
```

- [x] **Step 5: Wire into the server**

In `backend/cmd/server/main.go`:

a) Below the existing repo creation lines, add:

```go
weeklyDigestRepo := repository.NewWeeklyDigestRepository(db)
```

b) Below the existing handler creation lines (alongside `recommendedHandler := ...`), add:

```go
weeklyHandler := api.NewWeeklyHandler(articleRepo, weeklyDigestRepo, summarizer)
```

c) Inside the protected route group (alongside the recommended-feeds routes), add:

```go
apiGroup.GET("/weekly-digest", weeklyHandler.Get)
```

- [x] **Step 6: Build**

```bash
cd backend && go build ./...
```

Expected: no errors.

- [x] **Step 7: Smoke test the endpoint**

```bash
docker-compose up -d --build api
sleep 5
TOKEN=$(curl -s -X POST localhost:8080/api/auth/login -H 'Content-Type: application/json' -d '{"username":"admin","password":"admin"}' | grep -o '"token":"[^"]*' | cut -d'"' -f4)
curl -s -H "Authorization: Bearer $TOKEN" 'localhost:8080/api/weekly-digest' | head -c 800
```

Expected: JSON with `week_start` (a Monday), `intro_text` (possibly empty if no articles this week, or AI-generated otherwise), and an `articles` array.

- [x] **Step 8: Commit**

```bash
git add backend/internal/ai/weekly_digest.go backend/internal/repository/weekly_digest.go backend/internal/api/weekly.go backend/internal/repository/article.go backend/cmd/server/main.go
git commit -m "feat(api): weekly digest with AI-generated theme intro and cache"
```

---

## Task 11: Weekly digest page (frontend)

**Files:**
- Modify: `frontend/src/api/client.ts`
- Create: `frontend/src/pages/WeeklyPage.tsx`
- Modify: `frontend/src/App.tsx`

- [x] **Step 1: Add types and API call**

Append to `frontend/src/api/client.ts`:

```ts
export interface WeeklyDigest {
  week_start: string
  intro_text: string
  articles: Article[]
}

export const getWeeklyDigest = (week?: string) =>
  api.get<WeeklyDigest>('/weekly-digest', { params: week ? { week } : {} }).then(res => res.data)
```

- [x] **Step 2: Build the page**

`frontend/src/pages/WeeklyPage.tsx`:

```tsx
import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { getWeeklyDigest, WeeklyDigest } from '../api/client'
import ReadingMeta from '../components/ReadingMeta'

function shiftWeek(weekStart: string, days: number): string {
  const d = new Date(weekStart + 'T00:00:00+08:00')
  d.setDate(d.getDate() + days)
  return d.toISOString().slice(0, 10)
}

export default function WeeklyPage() {
  const [digest, setDigest] = useState<WeeklyDigest | null>(null)
  const [loading, setLoading] = useState(true)
  const [week, setWeek] = useState<string | undefined>(undefined)

  useEffect(() => { load(week) }, [week])

  const load = async (w?: string) => {
    setLoading(true)
    try {
      const data = await getWeeklyDigest(w)
      setDigest(data)
    } finally {
      setLoading(false)
    }
  }

  if (loading) return <div className="card">加载中…</div>
  if (!digest) return <div className="card">暂无数据</div>

  return (
    <div>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 16 }}>
        <h2>本周精选 · {digest.week_start}</h2>
        <div style={{ display: 'flex', gap: 8 }}>
          <button className="secondary" onClick={() => setWeek(shiftWeek(digest.week_start, -7))}>‹ 上一周</button>
          <button className="secondary" onClick={() => setWeek(shiftWeek(digest.week_start, 7))}>下一周 ›</button>
          {week !== undefined && (
            <button className="secondary" onClick={() => setWeek(undefined)}>本周</button>
          )}
        </div>
      </div>

      {digest.intro_text ? (
        <div className="card" style={{ marginBottom: 16, lineHeight: 1.7 }}>
          {digest.intro_text}
        </div>
      ) : (
        <div className="card text-muted" style={{ marginBottom: 16, fontSize: 13 }}>
          {digest.articles.length === 0
            ? '本周暂无入选文章。'
            : '本周导语生成失败或暂未生成,以下是入选文章:'}
        </div>
      )}

      {digest.articles.length === 0 ? null : (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
          {digest.articles.map(a => (
            <Link key={a.id} to={`/articles/${a.id}`} className="card" style={{ display: 'block', textDecoration: 'none', color: 'inherit' }}>
              <div style={{ fontSize: 15, fontWeight: 600, marginBottom: 4 }}>{a.title}</div>
              <div style={{ display: 'flex', gap: 12, alignItems: 'center', flexWrap: 'wrap', marginBottom: 6 }}>
                {a.feed_title && <span className="text-muted text-sm">{a.feed_title}</span>}
                {a.published_at && <span className="text-muted text-sm">{new Date(a.published_at).toLocaleDateString('zh-CN')}</span>}
                <ReadingMeta wordCount={a.word_count} readingMinutes={a.reading_minutes} />
              </div>
              {a.summary_brief && <div className="text-muted" style={{ fontSize: 13, lineHeight: 1.5 }}>{a.summary_brief}</div>}
            </Link>
          ))}
        </div>
      )}
    </div>
  )
}
```

- [x] **Step 3: Add route**

In `frontend/src/App.tsx`:

a) Import:

```tsx
import WeeklyPage from './pages/WeeklyPage'
```

b) Inside the protected `<Route>` block, add:

```tsx
<Route path="weekly" element={<WeeklyPage />} />
```

- [x] **Step 4: Type-check**

```bash
cd frontend && npm run build
```

Expected: no TypeScript errors.

- [x] **Step 5: Commit**

```bash
git add frontend/src/api/client.ts frontend/src/pages/WeeklyPage.tsx frontend/src/App.tsx
git commit -m "feat(frontend): weekly digest page with prev/next navigation"
```

---

## Task 12: Navigation links + final E2E verification

**Files:**
- Modify: `frontend/src/components/Layout.tsx`

- [x] **Step 1: Add nav entries**

In `frontend/src/components/Layout.tsx`, find the desktop nav (the `<nav className="flex gap-2 desktop-nav" ...>` block) and add two `NavLink` entries between `订阅` and `洞察`:

```tsx
<NavLink to="/articles" className={navLinkClass}>{articlesLabel}</NavLink>
<NavLink to="/weekly" className={navLinkClass}>周刊</NavLink>
<NavLink to="/feeds" className={navLinkClass}>订阅</NavLink>
<NavLink to="/recommended" className={navLinkClass}>推荐</NavLink>
<NavLink to="/insights" className={navLinkClass}>洞察</NavLink>
```

Also update the mobile dropdown nav array — add the same two entries:

```tsx
{[
  { to: '/articles', label: articlesLabel },
  { to: '/weekly', label: '周刊' },
  { to: '/feeds', label: '订阅' },
  { to: '/recommended', label: '推荐' },
  { to: '/insights', label: '洞察' },
  { to: '/stats', label: '统计' },
  { to: '/settings', label: '设置' },
].map(...)}
```

- [x] **Step 2: Type-check + build**

```bash
cd frontend && npm run build
```

Expected: no errors.

- [x] **Step 3: End-to-end verification**

```bash
docker-compose up -d --build
sleep 10
docker-compose ps
```

Expected: postgres / api / worker / frontend / rsshub all `Up` and healthy.

In a browser at `http://localhost`, log in as admin and verify:

- [x] `/feeds` shows 12 seeded sources (or fewer if some were broken)
- [x] `/articles` lists articles from at least: OpenAI, Cloudflare, baoyu.io, 量子位, Substack, 1+ YouTube channel
- [x] Article cards show `📖 N 字 · M 分钟`
- [x] `/recommended` shows 12 cards across categories (AI 工程 / 中文科技 / 企业基建 / 视频 / 播客). Healthy ones show `✓ 已订阅`. Broken ones show `⚠ 当前路由不可用`
- [x] `/weekly` shows the AI-generated intro paragraph + up to 10 article cards. Click `‹ 上一周` and confirm articles change.
- [x] Mobile menu (resize browser narrow, click `☰`) shows the new `周刊` and `推荐` entries
- [x] No console errors (`F12` → Console)

- [x] **Step 4: Commit + push**

```bash
git add frontend/src/components/Layout.tsx
git commit -m "feat(frontend): navigation links for weekly digest and recommended"
```

---

## Out of Scope (per spec § YAGNI)

These features are **not** part of this plan:
- Email push of the weekly digest (SMTP)
- Topic clustering inside the weekly digest
- Article-level fixed taxonomy
- Recommendation algorithm rewrite
- Pro / paid tiers
- X / Twitter integration
