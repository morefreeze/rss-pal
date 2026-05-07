# Podcast Audio Playback Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a global, persistent podcast audio player to RSS Pal so users can play, pause, scrub, speed up, skip, and resume `<enclosure>` audio across page navigations and devices.

**Architecture:** Worker parses RSS `<enclosure>` and `itunes:duration` into three new `articles` columns (idempotent backfill via `ON CONFLICT … WHERE media_url IS NULL`). New `playback_progress` table stores per-user resume position. New `PUT /api/articles/:id/playback` upserts progress and writes a `completed_listen` user_preferences signal on first completion. Frontend mounts a single `<audio>` element + `PlayerContext` in `Layout.tsx`; a fixed-position `MiniPlayer` reads/writes the context. ▶ buttons on `ArticleListPage` rows and an `ArticlePlayerCard` on `ArticlePage` both call `playArticle()` from the context.

**Tech Stack:** Go 1.22 / Gin / `database/sql` + `lib/pq` (positional params) / PostgreSQL 15 / `gofeed` for RSS / React 18 + React Router 6 + TypeScript / native HTML5 `<audio>` + `MediaSession` API. No new dependencies.

**Spec:** `docs/superpowers/specs/2026-05-07-podcast-audio-playback-design.md`

---

## File Map

**New files**
- `backend/migrations/011_audio_video.sql` — schema migration
- `backend/internal/rss/media.go` — `ExtractMedia(item *gofeed.Item) *MediaInfo` + duration parser
- `backend/internal/rss/media_test.go` — unit tests for media extraction
- `backend/internal/repository/playback_progress.go` — `PlaybackProgressRepository`
- `backend/internal/api/playback.go` — `PlaybackHandler` (GET, PUT)
- `backend/internal/api/playback_test.go` — handler integration tests
- `frontend/src/player/PlayerContext.tsx` — provider + hook
- `frontend/src/components/MiniPlayer.tsx` — fixed-bottom player UI
- `frontend/src/components/ArticlePlayerCard.tsx` — large play button on article detail

**Modified files**
- `backend/internal/model/model.go` — `Article` gets `MediaURL`, `MediaType`, `MediaDurationSeconds`; new `PlaybackProgress` struct
- `backend/internal/repository/article.go` — extend `Create` INSERT, add `UpdateMediaIfNull`, extend `scanArticle`/`scanArticleNoFeedTitle` and every SELECT that goes through them, extend recommendation scoring CASE statements
- `backend/internal/repository/preference.go` — extend `GetArticleScore` CASE
- `backend/internal/api/signalweight.go` — extend `StrengthFromSignal`
- `backend/cmd/worker/main.go` — call `rss.ExtractMedia` in both `processFeed` and `processHTMLFeed`; populate `article.Media*` on insert; call `UpdateMediaIfNull` on dedup hit
- `backend/cmd/server/main.go` — wire `PlaybackProgressRepository`, register `PlaybackHandler` routes
- `frontend/src/api/client.ts` — `Article` type gets media fields; new `getPlayback`/`putPlayback`
- `frontend/src/components/Layout.tsx` — wrap `<Outlet/>` in `<PlayerProvider>`, render `<MiniPlayer />`
- `frontend/src/pages/ArticleListPage.tsx` — render ▶ button when `article.media_url` is set
- `frontend/src/pages/ArticlePage.tsx` — render `<ArticlePlayerCard />` above content

---

## Pre-flight

- [ ] **Read the spec end-to-end** so the rationale for each task is in your head before you start.
  - Open `docs/superpowers/specs/2026-05-07-podcast-audio-playback-design.md` and read sections 4–9.

- [ ] **Confirm starting branch.**
  ```bash
  git -C /Users/bytedance/mygit/rss-pal status -sb
  ```
  Expected: `## feature/podcast-audio` and a clean tree (the spec was already committed as `63ebb39`).

- [ ] **Verify Docker stack is reachable** (used for manual verification later).
  ```bash
  docker-compose ps
  ```
  Expected: `postgres`, `api`, `worker`, `frontend` all `Up`. If not, `docker-compose up -d`.

---

## Task 1: Database migration

**Files:**
- Create: `backend/migrations/011_audio_video.sql`

- [ ] **Step 1: Write the migration**

```sql
-- 011_audio_video.sql
-- Adds podcast/video media metadata to articles and playback progress
-- tracking. Idempotent (uses IF NOT EXISTS).

ALTER TABLE articles
    ADD COLUMN IF NOT EXISTS media_url VARCHAR(2048),
    ADD COLUMN IF NOT EXISTS media_type VARCHAR(64),
    ADD COLUMN IF NOT EXISTS media_duration_seconds INT;

CREATE TABLE IF NOT EXISTS playback_progress (
    id SERIAL PRIMARY KEY,
    user_id INT REFERENCES users(id) ON DELETE CASCADE,
    article_id INT REFERENCES articles(id) ON DELETE CASCADE,
    position_seconds INT DEFAULT 0,
    last_played_at TIMESTAMP DEFAULT NOW(),
    is_completed BOOLEAN DEFAULT false,
    UNIQUE(user_id, article_id)
);

CREATE INDEX IF NOT EXISTS idx_playback_progress_user ON playback_progress(user_id);
```

- [ ] **Step 2: Apply the migration manually against the running container**

```bash
docker-compose exec -T postgres psql -U postgres -d rsspal -f - < backend/migrations/011_audio_video.sql
docker-compose exec -T postgres psql -U postgres -d rsspal -c "\d articles"
docker-compose exec -T postgres psql -U postgres -d rsspal -c "\d playback_progress"
```

Expected: `articles` table now lists `media_url`, `media_type`, `media_duration_seconds`. `playback_progress` table exists with the listed columns and a unique index on `(user_id, article_id)`.

- [ ] **Step 3: Commit**

```bash
git add backend/migrations/011_audio_video.sql
git commit -m "feat(podcast): add media columns + playback_progress table"
```

---

## Task 2: Article model fields + PlaybackProgress model

**Files:**
- Modify: `backend/internal/model/model.go:21-35` (Article struct)
- Modify: `backend/internal/model/model.go` (add new struct at the end)

- [ ] **Step 1: Add three fields to `Article`**

Replace lines 21–35 (the `Article` struct) with:

```go
type Article struct {
	ID                   int        `json:"id" db:"id"`
	FeedID               int        `json:"feed_id" db:"feed_id"`
	FeedTitle            string     `json:"feed_title,omitempty" db:"feed_title"`
	Title                string     `json:"title" db:"title"`
	URL                  string     `json:"url" db:"url"`
	Content              string     `json:"content" db:"content"`
	PublishedAt          *time.Time `json:"published_at" db:"published_at"`
	SummaryBrief         string     `json:"summary_brief" db:"summary_brief"`
	SummaryDetailed      string     `json:"summary_detailed" db:"summary_detailed"`
	FetchedAt            time.Time  `json:"fetched_at" db:"fetched_at"`
	WordCount            int        `json:"word_count" db:"word_count"`
	ReadingMinutes       int        `json:"reading_minutes" db:"reading_minutes"`
	IsRead               bool       `json:"is_read" db:"is_read"`
	MediaURL             string     `json:"media_url,omitempty" db:"media_url"`
	MediaType            string     `json:"media_type,omitempty" db:"media_type"`
	MediaDurationSeconds int        `json:"media_duration_seconds,omitempty" db:"media_duration_seconds"`
}
```

- [ ] **Step 2: Add `PlaybackProgress` struct at the end of the file**

```go
// PlaybackProgress is the per-user resume position for an audio article.
type PlaybackProgress struct {
	ID              int       `json:"id" db:"id"`
	UserID          int       `json:"user_id" db:"user_id"`
	ArticleID       int       `json:"article_id" db:"article_id"`
	PositionSeconds int       `json:"position_seconds" db:"position_seconds"`
	LastPlayedAt    time.Time `json:"last_played_at" db:"last_played_at"`
	IsCompleted     bool      `json:"is_completed" db:"is_completed"`
}
```

- [ ] **Step 3: Verify it compiles**

```bash
docker-compose exec -T api go build ./internal/model/...
# or if running outside docker:
cd backend && go build ./internal/model/...
```

Expected: no output (build succeeds). Compile errors elsewhere will be addressed in later tasks.

- [ ] **Step 4: Commit**

```bash
git add backend/internal/model/model.go
git commit -m "feat(podcast): add media fields to Article + PlaybackProgress model"
```

---

## Task 3: `rss.ExtractMedia` (TDD)

**Files:**
- Create: `backend/internal/rss/media_test.go`
- Create: `backend/internal/rss/media.go`

- [ ] **Step 1: Write the failing tests**

`backend/internal/rss/media_test.go`:

```go
package rss

import (
	"testing"

	"github.com/mmcdole/gofeed"
)

func TestExtractMedia_AudioEnclosure(t *testing.T) {
	item := &gofeed.Item{
		Enclosures: []*gofeed.Enclosure{
			{URL: "https://cdn.example.com/ep1.mp3", Type: "audio/mpeg", Length: "31415926"},
		},
	}
	got := ExtractMedia(item)
	if got == nil {
		t.Fatal("expected MediaInfo, got nil")
	}
	if got.URL != "https://cdn.example.com/ep1.mp3" {
		t.Errorf("URL = %q", got.URL)
	}
	if got.Type != "audio/mpeg" {
		t.Errorf("Type = %q", got.Type)
	}
	if got.Duration != 0 {
		t.Errorf("Duration without itunes ext should be 0, got %d", got.Duration)
	}
}

func TestExtractMedia_PrefersAudioOverImage(t *testing.T) {
	item := &gofeed.Item{
		Enclosures: []*gofeed.Enclosure{
			{URL: "https://cdn.example.com/cover.jpg", Type: "image/jpeg"},
			{URL: "https://cdn.example.com/ep1.mp3", Type: "audio/mpeg"},
		},
	}
	got := ExtractMedia(item)
	if got == nil || got.URL != "https://cdn.example.com/ep1.mp3" {
		t.Fatalf("expected audio URL, got %+v", got)
	}
}

func TestExtractMedia_VideoEnclosure(t *testing.T) {
	item := &gofeed.Item{
		Enclosures: []*gofeed.Enclosure{
			{URL: "https://cdn.example.com/ep1.mp4", Type: "video/mp4"},
		},
	}
	got := ExtractMedia(item)
	if got == nil || got.Type != "video/mp4" {
		t.Fatalf("expected video, got %+v", got)
	}
}

func TestExtractMedia_NoEnclosure(t *testing.T) {
	if got := ExtractMedia(&gofeed.Item{}); got != nil {
		t.Fatalf("expected nil, got %+v", got)
	}
}

func TestExtractMedia_OnlyImageEnclosure(t *testing.T) {
	item := &gofeed.Item{
		Enclosures: []*gofeed.Enclosure{
			{URL: "https://cdn.example.com/cover.jpg", Type: "image/jpeg"},
		},
	}
	if got := ExtractMedia(item); got != nil {
		t.Fatalf("expected nil for image-only enclosures, got %+v", got)
	}
}

func TestExtractMedia_DurationFromITunes(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want int
	}{
		{"hh:mm:ss", "01:02:03", 3723},
		{"mm:ss", "42:13", 2533},
		{"raw seconds", "2530", 2530},
		{"empty", "", 0},
		{"invalid", "garbage", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			item := &gofeed.Item{
				Enclosures: []*gofeed.Enclosure{
					{URL: "https://x/ep.mp3", Type: "audio/mpeg"},
				},
				ITunesExt: &gofeed.ITunesItemExtension{Duration: tc.raw},
			}
			got := ExtractMedia(item)
			if got == nil {
				t.Fatal("expected non-nil")
			}
			if got.Duration != tc.want {
				t.Errorf("Duration = %d, want %d (raw=%q)", got.Duration, tc.want, tc.raw)
			}
		})
	}
}
```

- [ ] **Step 2: Run the tests; confirm they fail with "undefined: ExtractMedia"**

```bash
cd backend && go test ./internal/rss/ -run TestExtractMedia -v
```

Expected: `# github.com/bytedance/rss-pal/internal/rss [build failed]` with `undefined: ExtractMedia` (and `undefined: MediaInfo`).

- [ ] **Step 3: Implement `media.go`**

`backend/internal/rss/media.go`:

```go
package rss

import (
	"strconv"
	"strings"

	"github.com/mmcdole/gofeed"
)

// MediaInfo describes a single audio/video enclosure attached to a feed item.
type MediaInfo struct {
	URL      string
	Type     string
	Duration int // seconds; 0 means unknown
}

// ExtractMedia returns the first audio or video enclosure on item, or nil.
// enclosure.Length is bytes per the RSS spec — not seconds — so we ignore it
// and read item.ITunesExt.Duration when available.
func ExtractMedia(item *gofeed.Item) *MediaInfo {
	if item == nil {
		return nil
	}
	for _, e := range item.Enclosures {
		if e == nil || e.URL == "" {
			continue
		}
		t := strings.ToLower(e.Type)
		if !strings.HasPrefix(t, "audio/") && !strings.HasPrefix(t, "video/") {
			continue
		}
		mi := &MediaInfo{URL: e.URL, Type: e.Type}
		if item.ITunesExt != nil {
			mi.Duration = parseITunesDuration(item.ITunesExt.Duration)
		}
		return mi
	}
	return nil
}

// parseITunesDuration accepts "hh:mm:ss", "mm:ss", or raw integer seconds.
// Returns 0 on any parse failure (including empty input).
func parseITunesDuration(raw string) int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	if !strings.Contains(raw, ":") {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			return 0
		}
		return n
	}
	parts := strings.Split(raw, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return 0
	}
	var nums []int
	for _, p := range parts {
		n, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil || n < 0 {
			return 0
		}
		nums = append(nums, n)
	}
	if len(nums) == 2 {
		return nums[0]*60 + nums[1]
	}
	return nums[0]*3600 + nums[1]*60 + nums[2]
}
```

- [ ] **Step 4: Run the tests; confirm they pass**

```bash
cd backend && go test ./internal/rss/ -run TestExtractMedia -v
```

Expected: every `TestExtractMedia*` case prints `--- PASS`.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/rss/media.go backend/internal/rss/media_test.go
git commit -m "feat(podcast): rss.ExtractMedia + itunes:duration parser"
```

---

## Task 4: Worker writes media on insert + repository `Create`

**Files:**
- Modify: `backend/internal/repository/article.go:21-39` (`scanArticle`)
- Modify: `backend/internal/repository/article.go:41-56` (`scanArticleNoFeedTitle`)
- Modify: `backend/internal/repository/article.go:152-155` (`Create`)
- Modify: `backend/internal/repository/article.go` — every SELECT that feeds the two `scanArticle*` helpers must add the three new columns
- Modify: `backend/internal/repository/article.go:107-124` (`GetByID`) — adds three columns + scan vars
- Modify: `backend/internal/repository/article.go:129-149` (`GetByIDWithFeedType`) — same
- Modify: `backend/cmd/worker/main.go:259-275` (RSS feed insert path)
- Modify: `backend/cmd/worker/main.go:319-336` (HTML feed insert path)

- [ ] **Step 1: Update `scanArticle` to read the three new columns**

Replace lines 21–39 with:

```go
func (r *ArticleRepository) scanArticle(rows *sql.Rows) ([]model.Article, error) {
	var articles []model.Article
	for rows.Next() {
		var a model.Article
		var content, summaryBrief, summaryDetailed, feedTitle, mediaURL, mediaType sql.NullString
		var mediaDuration sql.NullInt64
		var isRead sql.NullBool
		err := rows.Scan(&a.ID, &a.FeedID, &a.Title, &a.URL, &content, &a.PublishedAt, &summaryBrief, &summaryDetailed, &a.FetchedAt, &a.WordCount, &a.ReadingMinutes, &mediaURL, &mediaType, &mediaDuration, &feedTitle, &isRead)
		if err != nil {
			return nil, err
		}
		a.Content = content.String
		a.SummaryBrief = summaryBrief.String
		a.SummaryDetailed = summaryDetailed.String
		a.FeedTitle = feedTitle.String
		a.IsRead = isRead.Bool
		a.MediaURL = mediaURL.String
		a.MediaType = mediaType.String
		a.MediaDurationSeconds = int(mediaDuration.Int64)
		articles = append(articles, a)
	}
	return articles, nil
}
```

The new column order is: `… reading_minutes, media_url, media_type, media_duration_seconds, feed_title, is_read`.

- [ ] **Step 2: Update `scanArticleNoFeedTitle` similarly**

Replace lines 41–56 with:

```go
func (r *ArticleRepository) scanArticleNoFeedTitle(rows *sql.Rows) ([]model.Article, error) {
	var articles []model.Article
	for rows.Next() {
		var a model.Article
		var content, summaryBrief, summaryDetailed, mediaURL, mediaType sql.NullString
		var mediaDuration sql.NullInt64
		err := rows.Scan(&a.ID, &a.FeedID, &a.Title, &a.URL, &content, &a.PublishedAt, &summaryBrief, &summaryDetailed, &a.FetchedAt, &a.WordCount, &a.ReadingMinutes, &mediaURL, &mediaType, &mediaDuration)
		if err != nil {
			return nil, err
		}
		a.Content = content.String
		a.SummaryBrief = summaryBrief.String
		a.SummaryDetailed = summaryDetailed.String
		a.MediaURL = mediaURL.String
		a.MediaType = mediaType.String
		a.MediaDurationSeconds = int(mediaDuration.Int64)
		articles = append(articles, a)
	}
	return articles, nil
}
```

The new column order: `… reading_minutes, media_url, media_type, media_duration_seconds`.

- [ ] **Step 3: Update every SELECT statement that goes through these helpers**

Run this to enumerate them:

```bash
grep -n "FROM articles\b\|FROM articles a\b" backend/internal/repository/article.go
```

For each query string that ends up calling `r.scanArticle(rows)` or `r.scanArticleNoFeedTitle(rows)`, insert `a.media_url, a.media_type, a.media_duration_seconds` (or `articles.media_url, ...` if the table is unaliased) **immediately after `reading_minutes` and before `feed_title` / before the closing of the column list**.

Concretely, the queries to update are at lines: **59, 110, 132, 171, 223, 257, 276, 293, 314, 330, 359, 380, 415, 462, 502, 528**. After editing, run:

```bash
cd backend && go build ./internal/repository/...
```

Expected: build succeeds. If it fails, the most likely cause is an extra/missing column in some `Scan` call vs. its `SELECT` — line up the column lists.

- [ ] **Step 4: Update `GetByID` (lines 107–124) and `GetByIDWithFeedType` (lines 129–149) — they don't go through the helpers**

`GetByID`:

```go
func (r *ArticleRepository) GetByID(id, userID int) (*model.Article, error) {
	query := `
		SELECT a.id, a.feed_id, a.title, a.url, a.content, a.published_at, a.summary_brief, a.summary_detailed, a.fetched_at, a.word_count, a.reading_minutes, a.media_url, a.media_type, a.media_duration_seconds, f.title as feed_title
		FROM articles a
		JOIN feeds f ON a.feed_id = f.id
		WHERE a.id = $1 AND (f.owner_id IS NULL OR f.owner_id = $2)`
	var a model.Article
	var content, summaryBrief, summaryDetailed, feedTitle, mediaURL, mediaType sql.NullString
	var mediaDuration sql.NullInt64
	err := r.db.QueryRow(query, id, userID).Scan(&a.ID, &a.FeedID, &a.Title, &a.URL, &content, &a.PublishedAt, &summaryBrief, &summaryDetailed, &a.FetchedAt, &a.WordCount, &a.ReadingMinutes, &mediaURL, &mediaType, &mediaDuration, &feedTitle)
	if err != nil {
		return nil, err
	}
	a.Content = content.String
	a.SummaryBrief = summaryBrief.String
	a.SummaryDetailed = summaryDetailed.String
	a.FeedTitle = feedTitle.String
	a.MediaURL = mediaURL.String
	a.MediaType = mediaType.String
	a.MediaDurationSeconds = int(mediaDuration.Int64)
	return &a, nil
}
```

Apply the same surgery to `GetByIDWithFeedType` (insert the three columns into the SELECT list and scan vars in the same place).

- [ ] **Step 5: Extend `Create` to write the three new columns**

Replace lines 152–155 with:

```go
func (r *ArticleRepository) Create(article *model.Article) error {
	query := `INSERT INTO articles (feed_id, title, url, content, published_at, word_count, reading_minutes, media_url, media_type, media_duration_seconds) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) RETURNING id, fetched_at`
	mediaURL := nullableString(article.MediaURL)
	mediaType := nullableString(article.MediaType)
	mediaDuration := nullableInt(article.MediaDurationSeconds)
	return r.db.QueryRow(query, article.FeedID, article.Title, article.URL, article.Content, article.PublishedAt, article.WordCount, article.ReadingMinutes, mediaURL, mediaType, mediaDuration).Scan(&article.ID, &article.FetchedAt)
}

// nullableString returns a sql.NullString that's NULL when s is empty.
func nullableString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

// nullableInt returns a sql.NullInt64 that's NULL when n is zero.
func nullableInt(n int) sql.NullInt64 {
	if n == 0 {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: int64(n), Valid: true}
}
```

(If `nullableString`/`nullableInt` already exist in the package under another name, reuse those instead.)

- [ ] **Step 6: Add `UpdateMediaIfNull` next to `Create`**

Append in `article.go` (just after `Create`):

```go
// UpdateMediaIfNull fills the three media columns for an existing row, but
// only when media_url is currently NULL. Idempotent: subsequent calls on a
// row that already has media are no-ops. Used by the worker to backfill
// historical podcast episodes the first time we see them after this feature
// ships, without overwriting hand-edited or richer data.
func (r *ArticleRepository) UpdateMediaIfNull(feedID int, url, mediaURL, mediaType string, durationSeconds int) error {
	if mediaURL == "" {
		return nil
	}
	_, err := r.db.Exec(`
		UPDATE articles
		SET media_url = $3, media_type = $4, media_duration_seconds = $5
		WHERE feed_id = $1 AND url = $2 AND media_url IS NULL
	`, feedID, url, mediaURL, nullableString(mediaType), nullableInt(durationSeconds))
	return err
}
```

- [ ] **Step 7: Wire the worker (RSS path)**

In `backend/cmd/worker/main.go`, change the existing block at lines 228–275 to:

```go
		exists, _ := articleRepo.Exists(feed.ID, item.Link)
		mediaInfo := rss.ExtractMedia(item)
		if exists {
			articleRepo.UpdatePublishedAtIfNull(feed.ID, item.Link, parsePublishedTime(item.PublishedParsed, item.UpdatedParsed))
			if mediaInfo != nil {
				if err := articleRepo.UpdateMediaIfNull(feed.ID, item.Link, mediaInfo.URL, mediaInfo.Type, mediaInfo.Duration); err != nil {
					log.Printf("Failed to backfill media for %s: %v", item.Link, err)
				}
			}
			continue
		}

		queuedCount++

		wg.Add(1)
		go func(item *gofeed.Item, mediaInfo *rss.MediaInfo) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

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

			article := &model.Article{
				FeedID:      feed.ID,
				Title:       item.Title,
				URL:         item.Link,
				Content:     content,
				PublishedAt: parsePublishedTime(item.PublishedParsed, item.UpdatedParsed),
			}
			article.WordCount, article.ReadingMinutes = rss.ComputeMetrics(content)
			if mediaInfo != nil {
				article.MediaURL = mediaInfo.URL
				article.MediaType = mediaInfo.Type
				article.MediaDurationSeconds = mediaInfo.Duration
			}

			if err := articleRepo.Create(article); err != nil {
				log.Printf("Failed to create article: %v", err)
			} else {
				atomic.AddInt64(&newCount, 1)
				if summarizer != nil {
					asyncSummarize(summarizer, articleRepo, article.ID, article.Title, article.Content)
				}
			}
		}(item, mediaInfo)
```

The HTML feed path (`processHTMLFeed`, lines 304–337) does **not** need changes — HTML-scraped items have no enclosures.

- [ ] **Step 8: Build and run the existing test suite to catch regressions**

```bash
cd backend && go build ./... && go test ./internal/rss/... ./internal/repository/...
```

Expected: build succeeds; existing tests pass. New `media_test` cases keep passing.

- [ ] **Step 9: Verify backfill end-to-end**

```bash
docker-compose up -d --build api worker
docker-compose logs --tail=50 worker
# wait one full poll cycle (~60s)
docker-compose exec -T postgres psql -U postgres -d rsspal -c "SELECT id, title, media_url, media_duration_seconds FROM articles WHERE media_url IS NOT NULL LIMIT 5;"
```

Expected: at least a few rows have non-NULL `media_url`. If your subscriptions don't include any podcast feed yet, add one (e.g., the BBC Daily Global News feed: `https://podcasts.files.bbci.co.uk/p02nq0gn.rss`) via the UI, wait a poll, and re-query.

- [ ] **Step 10: Commit**

```bash
git add backend/internal/repository/article.go backend/cmd/worker/main.go
git commit -m "feat(podcast): worker captures media on insert + backfills existing rows"
```

---

## Task 5: Playback progress repository

**Files:**
- Create: `backend/internal/repository/playback_progress.go`

- [ ] **Step 1: Implement the repository**

```go
package repository

import (
	"database/sql"
	"time"

	"github.com/bytedance/rss-pal/internal/model"
)

type PlaybackProgressRepository struct {
	db *sql.DB
}

func NewPlaybackProgressRepository(db *sql.DB) *PlaybackProgressRepository {
	return &PlaybackProgressRepository{db: db}
}

// Get returns the current progress row for (user, article), or nil if absent.
func (r *PlaybackProgressRepository) Get(userID, articleID int) (*model.PlaybackProgress, error) {
	query := `
		SELECT id, user_id, article_id, position_seconds, last_played_at, is_completed
		FROM playback_progress
		WHERE user_id = $1 AND article_id = $2
	`
	var p model.PlaybackProgress
	err := r.db.QueryRow(query, userID, articleID).Scan(&p.ID, &p.UserID, &p.ArticleID, &p.PositionSeconds, &p.LastPlayedAt, &p.IsCompleted)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// UpsertResult tells the caller whether is_completed flipped from false→true on this call.
type UpsertResult struct {
	NewlyCompleted bool
}

// Upsert writes the latest position. Returns NewlyCompleted=true exactly once
// (on the call that flips is_completed false→true), so the handler knows when
// to record the completed_listen signal.
func (r *PlaybackProgressRepository) Upsert(userID, articleID, positionSeconds int, isCompleted bool) (UpsertResult, error) {
	prev, err := r.Get(userID, articleID)
	if err != nil {
		return UpsertResult{}, err
	}
	wasCompleted := prev != nil && prev.IsCompleted

	_, err = r.db.Exec(`
		INSERT INTO playback_progress (user_id, article_id, position_seconds, last_played_at, is_completed)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (user_id, article_id) DO UPDATE SET
			position_seconds = EXCLUDED.position_seconds,
			last_played_at = EXCLUDED.last_played_at,
			is_completed = playback_progress.is_completed OR EXCLUDED.is_completed
	`, userID, articleID, positionSeconds, time.Now(), isCompleted)
	if err != nil {
		return UpsertResult{}, err
	}

	return UpsertResult{NewlyCompleted: !wasCompleted && isCompleted}, nil
}
```

Note: `is_completed = playback_progress.is_completed OR EXCLUDED.is_completed` makes completion **sticky** — once true, it can't be flipped back by a stale write from another tab.

- [ ] **Step 2: Verify it compiles**

```bash
cd backend && go build ./internal/repository/...
```

Expected: success.

- [ ] **Step 3: Commit**

```bash
git add backend/internal/repository/playback_progress.go
git commit -m "feat(podcast): PlaybackProgressRepository with sticky completion"
```

---

## Task 6: Playback API + tests (TDD)

**Files:**
- Create: `backend/internal/api/playback_test.go`
- Create: `backend/internal/api/playback.go`
- Modify: `backend/cmd/server/main.go` (wire repo + register routes)

- [ ] **Step 1: Look at an existing handler test for the testing pattern**

```bash
ls backend/internal/api/*_test.go
sed -n '1,60p' backend/internal/api/bookmarklet_test.go
```

Use the same `httptest.NewRecorder()` + `gin.New()` setup, the same fake DB or in-memory `sql.DB` if used, and the same `getUserID` shim. **Do not invent a new test scaffold.**

- [ ] **Step 2: Write the failing test**

`backend/internal/api/playback_test.go`:

```go
package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// Reuses the test scaffolding pattern from bookmarklet_test.go — see that file
// for the helpers (newTestDB, seedUser, etc.). If those helpers live elsewhere,
// adapt this test to call them.

func TestPlayback_GetReturnsZeroWhenNoRow(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, cleanup := newTestDB(t)
	defer cleanup()
	userID := seedUser(t, db, "alice")
	articleID := seedArticle(t, db, "https://x/ep1.mp3", userID)

	h := NewPlaybackHandler(repository.NewPlaybackProgressRepository(db), repository.NewPreferenceRepository(db))
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("user_id", userID); c.Next() })
	r.GET("/api/articles/:id/playback", h.Get)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/articles/"+itoa(articleID)+"/playback", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	var body struct {
		Position    int  `json:"position_seconds"`
		IsCompleted bool `json:"is_completed"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body.Position != 0 || body.IsCompleted != false {
		t.Errorf("expected zero values, got %+v", body)
	}
}

func TestPlayback_PutUpsertsAndWritesCompletedListenSignal(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, cleanup := newTestDB(t)
	defer cleanup()
	userID := seedUser(t, db, "alice")
	articleID := seedArticle(t, db, "https://x/ep1.mp3", userID)

	h := NewPlaybackHandler(repository.NewPlaybackProgressRepository(db), repository.NewPreferenceRepository(db))
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("user_id", userID); c.Next() })
	r.PUT("/api/articles/:id/playback", h.Put)

	// First PUT: incomplete → no signal
	putBody, _ := json.Marshal(map[string]any{"position_seconds": 60, "is_completed": false})
	doPut(t, r, articleID, putBody)
	assertSignalCount(t, db, userID, articleID, "completed_listen", 0)

	// Second PUT: completes → exactly one signal
	putBody, _ = json.Marshal(map[string]any{"position_seconds": 2400, "is_completed": true})
	doPut(t, r, articleID, putBody)
	assertSignalCount(t, db, userID, articleID, "completed_listen", 1)

	// Third PUT: still completed → still exactly one signal (no double-write)
	putBody, _ = json.Marshal(map[string]any{"position_seconds": 2410, "is_completed": true})
	doPut(t, r, articleID, putBody)
	assertSignalCount(t, db, userID, articleID, "completed_listen", 1)
}

// helpers — implement using the patterns from bookmarklet_test.go
func doPut(t *testing.T, r *gin.Engine, articleID int, body []byte) {
	t.Helper()
	w := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/articles/"+itoa(articleID)+"/playback", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK && w.Code != http.StatusNoContent {
		t.Fatalf("PUT status = %d, body=%s", w.Code, w.Body.String())
	}
}
```

> If `newTestDB`, `seedUser`, `seedArticle`, `assertSignalCount`, or `itoa` don't exist in the package's existing test files, add them to a new `testhelpers_test.go` (modeled after whatever `bookmarklet_test.go` does). **Don't invent a new mocking framework — match the existing style.**

- [ ] **Step 3: Run the tests; confirm they fail with `undefined: NewPlaybackHandler`**

```bash
cd backend && go test ./internal/api/ -run TestPlayback -v
```

- [ ] **Step 4: Implement the handler**

`backend/internal/api/playback.go`:

```go
package api

import (
	"net/http"
	"strconv"

	"github.com/bytedance/rss-pal/internal/model"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/gin-gonic/gin"
)

type PlaybackHandler struct {
	repo     *repository.PlaybackProgressRepository
	prefRepo *repository.PreferenceRepository
}

func NewPlaybackHandler(repo *repository.PlaybackProgressRepository, prefRepo *repository.PreferenceRepository) *PlaybackHandler {
	return &PlaybackHandler{repo: repo, prefRepo: prefRepo}
}

// Get returns the user's saved position for an article. Missing row → zero values.
// Response: { "position_seconds": int, "is_completed": bool }
func (h *PlaybackHandler) Get(c *gin.Context) {
	articleID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid article id"})
		return
	}
	userID := getUserID(c)
	p, err := h.repo.Get(userID, articleID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if p == nil {
		c.JSON(http.StatusOK, gin.H{"position_seconds": 0, "is_completed": false})
		return
	}
	c.JSON(http.StatusOK, gin.H{"position_seconds": p.PositionSeconds, "is_completed": p.IsCompleted})
}

// Put upserts the user's position. On the first transition false→true, also
// writes a completed_listen user_preferences row so the recommender treats
// "listened all the way through" as a strong positive signal (value=8).
//
// Body: { "position_seconds": int, "is_completed": bool }
func (h *PlaybackHandler) Put(c *gin.Context) {
	articleID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid article id"})
		return
	}
	var req struct {
		PositionSeconds int  `json:"position_seconds"`
		IsCompleted     bool `json:"is_completed"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.PositionSeconds < 0 {
		req.PositionSeconds = 0
	}
	userID := getUserID(c)

	result, err := h.repo.Upsert(userID, articleID, req.PositionSeconds, req.IsCompleted)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if result.NewlyCompleted {
		_ = h.prefRepo.Add(&model.UserPreference{
			UserID:      userID,
			ArticleID:   articleID,
			SignalType:  "completed_listen",
			SignalValue: 1.0, // weight is applied in the scoring CASE; value is the count
		})
	}
	c.Status(http.StatusOK)
}
```

- [ ] **Step 5: Wire the handler in `backend/cmd/server/main.go`**

After line 25 (where `prefRepo` is created), add:

```go
playbackRepo := repository.NewPlaybackProgressRepository(db)
```

After line 39 (where `articleHandler` is created), add:

```go
playbackHandler := api.NewPlaybackHandler(playbackRepo, prefRepo)
```

In the auth-protected `apiGroup` block (after line 109 where `articles/:id` is registered), add:

```go
		apiGroup.GET("/articles/:id/playback", playbackHandler.Get)
		apiGroup.PUT("/articles/:id/playback", playbackHandler.Put)
```

- [ ] **Step 6: Run the tests; confirm they pass**

```bash
cd backend && go test ./internal/api/ -run TestPlayback -v
```

Expected: all pass.

- [ ] **Step 7: Manual smoke check**

```bash
docker-compose up -d --build api
# Log in via the UI to obtain the auth_token cookie, copy it, then:
curl -s -b "auth_token=authenticated; user_id=1" http://localhost:8080/api/articles/1/playback
# Expected: {"position_seconds":0,"is_completed":false}
curl -s -b "auth_token=authenticated; user_id=1" -X PUT http://localhost:8080/api/articles/1/playback \
    -H "Content-Type: application/json" \
    -d '{"position_seconds": 42, "is_completed": false}'
# Expected: 200 OK, empty body
docker-compose exec -T postgres psql -U postgres -d rsspal -c "SELECT * FROM playback_progress;"
# Expected: one row (user_id=1, article_id=1, position_seconds=42)
```

- [ ] **Step 8: Commit**

```bash
git add backend/internal/api/playback.go backend/internal/api/playback_test.go backend/cmd/server/main.go
git commit -m "feat(podcast): GET/PUT /api/articles/:id/playback"
```

---

## Task 7: Recommendation scoring picks up `completed_listen`

**Files:**
- Modify: `backend/internal/repository/preference.go:137-143`
- Modify: `backend/internal/repository/article.go:226-231, 385-390, 420-421, 507-512, 533-540`
- Modify: `backend/internal/api/signalweight.go:18-31`

The spec sets `completed_listen` weight at 8 (per-article score) and topic-strength 2.0 (parity with `save`).

- [ ] **Step 1: Extend `GetArticleScore` (preference.go)**

Change lines 137–143 from:

```go
		CASE signal_type
			WHEN 'like' THEN 5.0 * signal_value
			WHEN 'dislike' THEN -10.0 * signal_value
			WHEN 'save' THEN 3.0 * signal_value
			WHEN 'read_duration' THEN signal_value / 60.0
			ELSE 1.0 * signal_value
		END
```

to:

```go
		CASE signal_type
			WHEN 'like' THEN 5.0 * signal_value
			WHEN 'dislike' THEN -10.0 * signal_value
			WHEN 'save' THEN 3.0 * signal_value
			WHEN 'read_duration' THEN signal_value / 60.0
			WHEN 'completed_listen' THEN 8.0 * signal_value
			ELSE 1.0 * signal_value
		END
```

- [ ] **Step 2: Apply the same change to article.go at lines 226–231, 385–390, and 507–512**

Three identical CASE blocks, all four-line bodies. Add the new `WHEN 'completed_listen' THEN 8.0 * signal_value` line in each.

- [ ] **Step 3: Extend the strong-signal filter at article.go:420-421**

Change:

```go
		WHERE up.signal_type IN ('like','save')
		    OR (up.signal_type = 'read_duration' AND up.signal_value >= 60)
```

to:

```go
		WHERE up.signal_type IN ('like','save','completed_listen')
		    OR (up.signal_type = 'read_duration' AND up.signal_value >= 60)
```

- [ ] **Step 4: Extend the topic-strength HAVING/MAX block at article.go:533–540**

Look at lines 533–540 (the `CASE signal_type … WHEN 'like' THEN … WHEN 'save' THEN …` block followed by `WHERE user_id = $1 AND signal_type IN ('like','save')`). Add a `WHEN 'completed_listen' THEN 2.0 * signal_value` branch and add `'completed_listen'` to the `IN (…)` list.

- [ ] **Step 5: Extend `StrengthFromSignal` (signalweight.go)**

After the `case "read_duration":` branch (lines 24–28), add before `}`:

```go
	case "completed_listen":
		return 2.0
```

- [ ] **Step 6: Build and run all backend tests**

```bash
cd backend && go build ./... && go test ./...
```

Expected: build + tests pass.

- [ ] **Step 7: Commit**

```bash
git add backend/internal/repository/preference.go backend/internal/repository/article.go backend/internal/api/signalweight.go
git commit -m "feat(podcast): completed_listen signal feeds recommendation scoring"
```

---

## Task 8: Frontend client types + functions

**Files:**
- Modify: `frontend/src/api/client.ts`

- [ ] **Step 1: Locate the `Article` interface**

```bash
grep -n "interface Article\|export type Article\b\|^type Article\b" frontend/src/api/client.ts
```

- [ ] **Step 2: Add three optional fields to the `Article` interface**

Add these inside the `Article` interface (anywhere is fine, but match existing snake_case convention):

```ts
  media_url?: string
  media_type?: string
  media_duration_seconds?: number
```

- [ ] **Step 3: Add two new API functions next to existing `getArticle`/related calls**

```ts
export interface PlaybackProgress {
  position_seconds: number
  is_completed: boolean
}

export function getPlayback(articleId: number): Promise<PlaybackProgress> {
  return client.get(`/api/articles/${articleId}/playback`).then(r => r.data)
}

export function putPlayback(articleId: number, body: PlaybackProgress): Promise<void> {
  return client.put(`/api/articles/${articleId}/playback`, body).then(() => undefined)
}
```

(`client` is the existing axios instance — match the file's existing import patterns.)

- [ ] **Step 4: Type-check**

```bash
cd frontend && npx tsc --noEmit
```

Expected: no new errors.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/api/client.ts
git commit -m "feat(podcast): client types + getPlayback/putPlayback"
```

---

## Task 9: PlayerContext + Provider

**Files:**
- Create: `frontend/src/player/PlayerContext.tsx`

- [ ] **Step 1: Implement the provider**

```tsx
import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState } from 'react'
import { Article, getPlayback, putPlayback } from '../api/client'

type Speed = 1 | 1.25 | 1.5 | 1.75 | 2

interface PlayerState {
  articleId: number | null
  title: string
  feedTitle: string
  src: string
  duration: number
  position: number
  playing: boolean
  speed: Speed
  loading: boolean
  error: string | null
}

interface PlayerActions {
  playArticle(article: Article): Promise<void>
  toggle(): void
  seek(sec: number): void
  skip(deltaSec: number): void
  setSpeed(s: Speed): void
  close(): void
}

type PlayerContextValue = PlayerState & PlayerActions & { audioRef: React.RefObject<HTMLAudioElement> }

const PlayerContext = createContext<PlayerContextValue | null>(null)

export function usePlayer(): PlayerContextValue {
  const ctx = useContext(PlayerContext)
  if (!ctx) throw new Error('usePlayer must be used inside <PlayerProvider>')
  return ctx
}

const initial: PlayerState = {
  articleId: null,
  title: '',
  feedTitle: '',
  src: '',
  duration: 0,
  position: 0,
  playing: false,
  speed: 1,
  loading: false,
  error: null,
}

export function PlayerProvider({ children }: { children: React.ReactNode }) {
  const audioRef = useRef<HTMLAudioElement>(null)
  const [state, setState] = useState<PlayerState>(initial)
  const stateRef = useRef(state)
  stateRef.current = state

  // Flush latest position to backend. Safe to call any time; no-ops when no article.
  const flush = useCallback(async (overrides?: Partial<{ position: number; isCompleted: boolean }>) => {
    const s = stateRef.current
    if (!s.articleId) return
    const position = overrides?.position ?? s.position
    const isCompleted = overrides?.isCompleted ?? false
    try {
      await putPlayback(s.articleId, { position_seconds: Math.floor(position), is_completed: isCompleted })
    } catch (e) {
      // ignore — next tick will retry
    }
  }, [])

  // Periodic flush every 10s while playing.
  useEffect(() => {
    if (!state.playing) return
    const id = window.setInterval(() => { flush() }, 10000)
    return () => window.clearInterval(id)
  }, [state.playing, flush])

  // Bind <audio> events.
  useEffect(() => {
    const el = audioRef.current
    if (!el) return
    const onLoaded = () => setState(s => ({ ...s, duration: el.duration || s.duration, loading: false }))
    const onTime = () => setState(s => ({ ...s, position: el.currentTime }))
    const onPlay = () => setState(s => ({ ...s, playing: true }))
    const onPause = () => { setState(s => ({ ...s, playing: false })); flush() }
    const onEnded = () => { setState(s => ({ ...s, playing: false })); flush({ position: stateRef.current.duration, isCompleted: true }) }
    const onError = () => setState(s => ({ ...s, error: '无法加载音频', loading: false, playing: false }))
    el.addEventListener('loadedmetadata', onLoaded)
    el.addEventListener('timeupdate', onTime)
    el.addEventListener('play', onPlay)
    el.addEventListener('pause', onPause)
    el.addEventListener('ended', onEnded)
    el.addEventListener('error', onError)
    return () => {
      el.removeEventListener('loadedmetadata', onLoaded)
      el.removeEventListener('timeupdate', onTime)
      el.removeEventListener('play', onPlay)
      el.removeEventListener('pause', onPause)
      el.removeEventListener('ended', onEnded)
      el.removeEventListener('error', onError)
    }
  }, [flush])

  const playArticle = useCallback(async (article: Article) => {
    if (!article.media_url) return
    const el = audioRef.current
    if (!el) return

    // If switching to a different article, flush the old one first.
    if (stateRef.current.articleId && stateRef.current.articleId !== article.id) {
      await flush()
    }

    let resumeAt = 0
    try {
      const p = await getPlayback(article.id)
      resumeAt = p.is_completed ? 0 : p.position_seconds
    } catch {
      // ok, start from 0
    }

    setState({
      articleId: article.id,
      title: article.title,
      feedTitle: article.feed_title || '',
      src: article.media_url,
      duration: article.media_duration_seconds || 0,
      position: resumeAt,
      playing: false,
      speed: stateRef.current.speed,
      loading: true,
      error: null,
    })

    el.src = article.media_url
    el.playbackRate = stateRef.current.speed
    // Wait for the metadata before seeking — otherwise the seek is dropped.
    const playFromResume = () => {
      el.currentTime = resumeAt
      el.play().catch(() => {})
      el.removeEventListener('loadedmetadata', playFromResume)
    }
    el.addEventListener('loadedmetadata', playFromResume)
    el.load()
  }, [flush])

  const toggle = useCallback(() => {
    const el = audioRef.current
    if (!el || !stateRef.current.articleId) return
    if (el.paused) el.play().catch(() => {})
    else el.pause()
  }, [])

  const seek = useCallback((sec: number) => {
    const el = audioRef.current
    if (!el) return
    el.currentTime = Math.max(0, sec)
  }, [])

  const skip = useCallback((delta: number) => {
    const el = audioRef.current
    if (!el) return
    el.currentTime = Math.max(0, Math.min(el.duration || Infinity, el.currentTime + delta))
  }, [])

  const setSpeed = useCallback((s: Speed) => {
    const el = audioRef.current
    if (el) el.playbackRate = s
    setState(prev => ({ ...prev, speed: s }))
  }, [])

  const close = useCallback(() => {
    const el = audioRef.current
    if (el) {
      el.pause()
      el.removeAttribute('src')
      el.load()
    }
    flush()
    setState(initial)
  }, [flush])

  // MediaSession (lock-screen / hardware keys).
  useEffect(() => {
    if (!('mediaSession' in navigator)) return
    if (!state.articleId) {
      navigator.mediaSession.metadata = null
      return
    }
    navigator.mediaSession.metadata = new MediaMetadata({
      title: state.title,
      artist: state.feedTitle,
    })
    navigator.mediaSession.setActionHandler('play', toggle)
    navigator.mediaSession.setActionHandler('pause', toggle)
    navigator.mediaSession.setActionHandler('seekforward', () => skip(10))
    navigator.mediaSession.setActionHandler('seekbackward', () => skip(-5))
  }, [state.articleId, state.title, state.feedTitle, toggle, skip])

  // Final flush on unmount.
  useEffect(() => () => { flush() }, [flush])

  const value = useMemo<PlayerContextValue>(() => ({
    ...state,
    audioRef,
    playArticle, toggle, seek, skip, setSpeed, close,
  }), [state, playArticle, toggle, seek, skip, setSpeed, close])

  return (
    <PlayerContext.Provider value={value}>
      <audio ref={audioRef} preload="metadata" style={{ display: 'none' }} />
      {children}
    </PlayerContext.Provider>
  )
}
```

- [ ] **Step 2: Type-check**

```bash
cd frontend && npx tsc --noEmit
```

Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/player/PlayerContext.tsx
git commit -m "feat(podcast): PlayerProvider with resume + 10s flush + MediaSession"
```

---

## Task 10: MiniPlayer + mount in Layout

**Files:**
- Create: `frontend/src/components/MiniPlayer.tsx`
- Modify: `frontend/src/components/Layout.tsx`

- [ ] **Step 1: Implement `MiniPlayer.tsx`**

```tsx
import { usePlayer } from '../player/PlayerContext'

const SPEEDS = [1, 1.25, 1.5, 1.75, 2] as const

function fmt(sec: number): string {
  if (!isFinite(sec) || sec < 0) return '--:--'
  const total = Math.floor(sec)
  const m = Math.floor(total / 60)
  const s = total % 60
  return `${m.toString().padStart(2, '0')}:${s.toString().padStart(2, '0')}`
}

export default function MiniPlayer() {
  const p = usePlayer()
  if (p.articleId === null) return null

  return (
    <div
      role="region"
      aria-label="Podcast player"
      style={{
        position: 'fixed',
        bottom: 0,
        left: 0,
        right: 0,
        height: 64,
        background: '#fff',
        borderTop: '1px solid #ddd',
        display: 'flex',
        alignItems: 'center',
        gap: 12,
        padding: '0 12px',
        boxShadow: '0 -2px 8px rgba(0,0,0,0.08)',
        zIndex: 1000,
      }}
    >
      <button onClick={p.toggle} aria-label={p.playing ? '暂停' : '播放'} style={{ fontSize: 20, padding: '4px 10px' }}>
        {p.playing ? '⏸' : '▶'}
      </button>
      <button onClick={() => p.skip(-5)} aria-label="后退5秒" style={{ padding: '4px 8px' }}>⏪5</button>
      <button onClick={() => p.skip(10)} aria-label="前进10秒" style={{ padding: '4px 8px' }}>⏩10</button>

      <div style={{ flex: 1, display: 'flex', flexDirection: 'column', minWidth: 0 }}>
        <div style={{ fontSize: 13, fontWeight: 600, whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>
          {p.title}
          {p.feedTitle && <span style={{ color: '#888', fontWeight: 400 }}> · {p.feedTitle}</span>}
        </div>
        <input
          type="range"
          min={0}
          max={p.duration || 0}
          value={p.position}
          onChange={e => p.seek(Number(e.target.value))}
          style={{ width: '100%' }}
          aria-label="播放进度"
        />
      </div>

      <span style={{ fontSize: 12, color: '#666', whiteSpace: 'nowrap' }}>
        {fmt(p.position)} / {fmt(p.duration)}
      </span>

      <select
        value={p.speed}
        onChange={e => p.setSpeed(Number(e.target.value) as 1 | 1.25 | 1.5 | 1.75 | 2)}
        aria-label="播放速度"
        style={{ fontSize: 13 }}
      >
        {SPEEDS.map(s => <option key={s} value={s}>{s}x</option>)}
      </select>

      <button onClick={p.close} aria-label="关闭播放器" style={{ padding: '4px 8px' }}>✕</button>

      {p.error && <span style={{ color: '#c00', fontSize: 12 }}>{p.error}</span>}
    </div>
  )
}
```

- [ ] **Step 2: Mount provider + mini-player in `Layout.tsx`**

In `frontend/src/components/Layout.tsx`:

1. Add imports at the top:
   ```tsx
   import { PlayerProvider } from '../player/PlayerContext'
   import MiniPlayer from './MiniPlayer'
   ```

2. Wrap the existing JSX. The current top-level return is `<div> <header>…</header> <main><Outlet/></main> <Toaster/> </div>`. Wrap the entire content in `<PlayerProvider>` and add `<MiniPlayer/>` next to `<Toaster/>`. Add bottom padding to `<main>` so the fixed player doesn't overlap content:

   ```tsx
   return (
     <PlayerProvider>
       <div>
         <header style={{ marginBottom: 16 }}>
           {/* …existing header… */}
         </header>
         <main style={{ paddingBottom: 80 }}>
           <Outlet />
         </main>
         <Toaster />
         <MiniPlayer />
       </div>
     </PlayerProvider>
   )
   ```

- [ ] **Step 3: Rebuild the frontend container and verify it loads**

```bash
docker-compose up -d --build frontend
```

Then load `http://localhost` in a browser and confirm:
- Page renders (no whitescreen)
- DevTools console: no errors
- The `<audio>` element exists in DOM (search "audio" in the elements panel — it will be hidden)
- The mini-player is **not** visible yet (no article is playing)

- [ ] **Step 4: Commit**

```bash
git add frontend/src/components/MiniPlayer.tsx frontend/src/components/Layout.tsx
git commit -m "feat(podcast): MiniPlayer + mount PlayerProvider in Layout"
```

---

## Task 11: ▶ button on `ArticleListPage` rows

**Files:**
- Modify: `frontend/src/pages/ArticleListPage.tsx`

- [ ] **Step 1: Open the file and find the row template**

```bash
grep -n "article.title\|article\\.id\|map(\\(article" frontend/src/pages/ArticleListPage.tsx | head -20
```

Identify the JSX block that renders one row (likely an `<a>`, `<Link>`, or `<div onClick={…}>` per article).

- [ ] **Step 2: Add a play button next to the title**

Import the player hook:

```tsx
import { usePlayer } from '../player/PlayerContext'
```

Inside the component, add:

```tsx
const player = usePlayer()
```

Inside the row template, just before the title element, add:

```tsx
{article.media_url && (
  <button
    aria-label="播放"
    title="播放"
    onClick={(e) => {
      e.preventDefault()
      e.stopPropagation()
      player.playArticle(article)
    }}
    style={{
      marginRight: 8,
      padding: '2px 8px',
      borderRadius: 999,
      border: '1px solid #0066cc',
      background: '#fff',
      color: '#0066cc',
      fontSize: 12,
      cursor: 'pointer',
    }}
  >
    ▶
  </button>
)}
```

`e.preventDefault() + e.stopPropagation()` is essential — otherwise the surrounding `<Link>` / row click will navigate to the article detail page, which is **not** what the user wants when clicking ▶.

- [ ] **Step 3: Rebuild & verify**

```bash
docker-compose up -d --build frontend
```

Load the article list. Filter to a podcast feed. Confirm:
1. Items with media show a ▶ button.
2. Clicking ▶ does **not** navigate to the article page.
3. The mini-player appears at the bottom and starts playing.
4. Clicking elsewhere on the row still navigates to the detail page.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/pages/ArticleListPage.tsx
git commit -m "feat(podcast): ▶ play button on list rows with media"
```

---

## Task 12: `ArticlePlayerCard` on `ArticlePage`

**Files:**
- Create: `frontend/src/components/ArticlePlayerCard.tsx`
- Modify: `frontend/src/pages/ArticlePage.tsx`

- [ ] **Step 1: Implement the card**

```tsx
import { Article } from '../api/client'
import { usePlayer } from '../player/PlayerContext'

function fmtMinSec(sec: number): string {
  if (!sec || sec <= 0) return ''
  const m = Math.floor(sec / 60)
  const s = sec % 60
  return `${m}分${s.toString().padStart(2, '0')}秒`
}

export default function ArticlePlayerCard({ article }: { article: Article }) {
  const p = usePlayer()
  if (!article.media_url) return null

  const isCurrent = p.articleId === article.id
  const playing = isCurrent && p.playing

  return (
    <div
      style={{
        margin: '12px 0 20px',
        padding: 16,
        border: '1px solid #ddd',
        borderRadius: 8,
        background: '#fafafa',
        display: 'flex',
        alignItems: 'center',
        gap: 16,
      }}
    >
      <button
        onClick={() => (isCurrent ? p.toggle() : p.playArticle(article))}
        aria-label={playing ? '暂停' : '播放'}
        style={{
          width: 56,
          height: 56,
          borderRadius: 999,
          background: '#0066cc',
          color: '#fff',
          border: 'none',
          fontSize: 24,
          cursor: 'pointer',
        }}
      >
        {playing ? '⏸' : '▶'}
      </button>
      <div style={{ flex: 1 }}>
        <div style={{ fontWeight: 600, fontSize: 15 }}>音频节目</div>
        <div style={{ fontSize: 13, color: '#666' }}>
          {fmtMinSec(article.media_duration_seconds || 0) || '时长未知'}
        </div>
      </div>
    </div>
  )
}
```

- [ ] **Step 2: Insert it into `ArticlePage.tsx` above the article body**

```bash
grep -n "MarkdownArticle\|<MarkdownArticle\|<article\|article.content" frontend/src/pages/ArticlePage.tsx | head -10
```

Find the spot where the title is rendered followed by the article body (`<MarkdownArticle source={…} />` or similar). Add an import:

```tsx
import ArticlePlayerCard from '../components/ArticlePlayerCard'
```

And just before the body component:

```tsx
<ArticlePlayerCard article={article} />
```

(The exact name of the local article variable depends on `ArticlePage.tsx`; use whatever holds the fetched `Article`.)

- [ ] **Step 3: Rebuild & verify**

```bash
docker-compose up -d --build frontend
```

Open a podcast article page in the browser and confirm:
1. The card appears above the body (or in place of it, if `content` is empty for podcast articles).
2. Clicking ▶ on the card starts playback in the mini-player.
3. The card's ▶ icon flips to ⏸ while playing.
4. Navigating away (e.g., to `/feeds`) keeps audio playing; the mini-player stays visible.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/components/ArticlePlayerCard.tsx frontend/src/pages/ArticlePage.tsx
git commit -m "feat(podcast): ArticlePlayerCard on article detail page"
```

---

## Task 13: End-to-end manual verification

This task contains no code changes — only proof that the feature works. Run through every check.

- [ ] **Step 1: Rebuild the full stack from a clean state**

```bash
docker-compose down
docker-compose up -d --build
docker-compose logs --tail=50 worker
```

Wait ~2 minutes for one full RSS poll cycle.

- [ ] **Step 2: Confirm media backfill on existing rows**

```bash
docker-compose exec -T postgres psql -U postgres -d rsspal -c "SELECT COUNT(*) FROM articles WHERE media_url IS NOT NULL;"
```

Expected: a positive number if any podcast feed is subscribed; 0 otherwise. If 0, subscribe to a podcast feed via the UI (e.g., `https://feeds.simplecast.com/54nAGcIl` for The Daily) and wait one more poll cycle.

- [ ] **Step 3: Walk through the user flows**

In a browser at `http://localhost`:

1. **Play from list:** open Articles, find a podcast item, click ▶ inline. Mini-player appears at the bottom and starts playing. The list does **not** navigate to the article page.
2. **Cross-page continuity:** while playing, click "订阅" (Feeds) in the nav. Audio continues. Mini-player stays.
3. **Speed:** change the speed selector to 1.5x. Audio audibly speeds up.
4. **Skip:** click ⏪5 and ⏩10. Position changes by –5s and +10s respectively (visible on the time display).
5. **Resume:** click ✕ on the mini-player. Reload the page (`Cmd-R`). Open the same article — the article page card shows the ▶ button. Click it; playback resumes near where you left off (within ±10s, since flushes happen on a 10s timer).
6. **Cross-device resume:** confirm by querying the DB:
   ```bash
   docker-compose exec -T postgres psql -U postgres -d rsspal -c "SELECT * FROM playback_progress ORDER BY last_played_at DESC LIMIT 5;"
   ```
   Expected: row(s) with current `position_seconds`.
7. **Completion signal:** seek near the end (within 5 seconds of `media_duration_seconds`) and let it play out. Then:
   ```bash
   docker-compose exec -T postgres psql -U postgres -d rsspal -c "SELECT * FROM user_preferences WHERE signal_type = 'completed_listen';"
   ```
   Expected: one new row for that article.
8. **Mobile lock screen** (optional, only if testing on a phone): start playback in mobile Safari/Chrome, lock the device. Lock screen shows the episode metadata and play/pause buttons.

- [ ] **Step 4: Update memory**

If any flow above behaves unexpectedly and you have to fix it, capture the fix's cause in a memory note (`feedback_*` if it's a coding-pattern lesson; `project_*` if it's a status fact). Skip this step if everything worked.

- [ ] **Step 5: Final commit (only if any cleanup was needed)**

```bash
git status
# If anything was changed during verification, commit it:
git add -p
git commit -m "fix(podcast): <specific issue>"
```

---

## Wrap-up

- [ ] **Confirm branch state**

```bash
git -C /Users/bytedance/mygit/rss-pal log --oneline origin/master..HEAD
```

Expected: ~14 commits, one per Task, all on `feature/podcast-audio`.

- [ ] **Hand off**

The next person to touch this branch should run through Task 13 (Step 3, items 1–7) on a fresh terminal+browser to confirm parity. After that, push and open a PR against `master`.
