# Transcript Fetching for Video & Audio Articles — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fetch real transcripts for video (YouTube/Bilibili) and audio (podcast) articles, append them into article content so the user can read along, and let the existing summary backfill loop produce a non-hallucinated summary.

**Architecture:** New `internal/transcript/` package with a `Fetcher` interface and four concrete strategies (`YouTubeCC` watch-page parsing, `BilibiliCC` `player/v2` API, `HTMLPageScraper` for inline transcripts and linked .vtt/.srt/.txt files, plus a `MultiFetcher` composite). A new worker step `backfillTranscripts` runs each cycle between `refetchShortContent` and `backfillSummaries`, picks up media articles where `transcript_fetched_at IS NULL`, fetches via the composite, appends the transcript into `content` (markdown separator + `## 字幕` heading), and clears the existing summary so the existing summary backfill loop re-runs against the now-richer content.

**Tech Stack:** Go (alpine container, pure HTTP, no Python). Uses existing `goquery`, `lib/pq`, project's `ContentFetcher`. No new third-party dependencies.

**Spec:** `docs/superpowers/specs/2026-05-08-transcript-fetching-design.md`

---

## File Structure

| File | New / Modified | Responsibility |
|------|----------------|----------------|
| `backend/migrations/013_transcript_fetch.sql` | New | Adds `transcript_fetched_at TIMESTAMPTZ` column |
| `backend/internal/transcript/fetcher.go` | New | `Result` struct, `Fetcher` interface, `MultiFetcher` composite |
| `backend/internal/transcript/youtube.go` | New | `YouTubeCC` strategy |
| `backend/internal/transcript/bilibili.go` | New | `BilibiliCC` strategy |
| `backend/internal/transcript/subtitle_files.go` | New | VTT/SRT/TXT parsers (helper used by `HTMLPageScraper`) |
| `backend/internal/transcript/html_page.go` | New | `HTMLPageScraper` strategy |
| `backend/internal/transcript/*_test.go` | New | Unit tests per strategy with captured fixtures |
| `backend/internal/transcript/testdata/` | New | HTML / JSON / VTT / SRT fixtures |
| `backend/internal/rss/content.go` | Modified | New exported `FetchHTMLDocument` method on `ContentFetcher` (extracts existing direct-HTTP+goquery code) |
| `backend/internal/repository/article.go` | Modified | New methods: `GetMediaArticlesWithoutTranscript`, `UpdateContentAndResetSummary`, `MarkTranscriptFetchAttempted`. Tweak `GetArticlesWithShortContent` to skip media articles |
| `backend/cmd/worker/main.go` | Modified | New `backfillTranscripts` function, wired into `runFetchCycle` |

---

## Task 1: Database migration

**Files:**
- Create: `backend/migrations/013_transcript_fetch.sql`

- [ ] **Step 1: Write the migration file**

Create `backend/migrations/013_transcript_fetch.sql`:

```sql
-- 013_transcript_fetch.sql
-- Tracks per-article transcript-fetch attempts so the worker doesn't
-- retry indefinitely. NULL = never attempted; non-NULL = attempted once
-- (success or failure). Idempotent.

ALTER TABLE articles
    ADD COLUMN IF NOT EXISTS transcript_fetched_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_articles_transcript_pending
    ON articles (id)
    WHERE transcript_fetched_at IS NULL
      AND media_type IS NOT NULL
      AND (media_type LIKE 'video/%' OR media_type LIKE 'audio/%');
```

The partial index keeps the worker's per-cycle query cheap as the table grows.

- [ ] **Step 2: Apply to running DB**

```bash
docker-compose exec -T postgres psql -U postgres -d rsspal -f /docker-entrypoint-initdb.d/013_transcript_fetch.sql
```

(Note: `/docker-entrypoint-initdb.d/` is mounted from `./backend/migrations/` in `docker-compose.yml`, so the file is already there once you save it.)

Expected output: `ALTER TABLE` then `CREATE INDEX`.

- [ ] **Step 3: Verify**

```bash
docker-compose exec -T postgres psql -U postgres -d rsspal -c "\d articles" | grep transcript
```
Expected: a line showing `transcript_fetched_at | timestamp with time zone | ...`.

- [ ] **Step 4: Commit**

```bash
git add backend/migrations/013_transcript_fetch.sql
git commit -m "feat(db): add transcript_fetched_at column for video/audio articles"
```

---

## Task 2: Transcript package skeleton

**Files:**
- Create: `backend/internal/transcript/fetcher.go`

- [ ] **Step 1: Write the file**

```go
package transcript

import (
	"context"

	"github.com/bytedance/rss-pal/internal/model"
)

// Result is what a successful Fetcher returns. Text is the transcript as
// plain text or simple markdown paragraphs (no embedded timestamps unless
// they're part of the transcript itself, which is rare). Source is a short
// human-readable label, e.g. "YouTube CC" or "bbc.co.uk 网页字幕".
type Result struct {
	Text   string
	Source string
}

// Fetcher returns a transcript for the given article. The contract is:
//
//   - (Result, nil)  — transcript found.
//   - (nil, nil)     — no transcript exists for this article (do not retry).
//   - (nil, err)     — transient failure (network, parse). Caller may retry
//                      next cycle. Distinct from "no transcript".
//
// Fetchers should not panic on malformed input. They should also be cheap
// to invoke when they don't apply (e.g. YouTubeCC on a Bilibili article
// returns (nil, nil) immediately based on media_type).
type Fetcher interface {
	Fetch(ctx context.Context, article *model.Article) (*Result, error)
}

// MultiFetcher tries each strategy in order and returns the first non-nil
// Result. A transient error from one strategy does NOT abort: the next
// strategy still gets a chance. Errors are coalesced — if every strategy
// errored and none produced a Result, the first error is returned.
type MultiFetcher struct {
	Strategies []Fetcher
}

func (m *MultiFetcher) Fetch(ctx context.Context, article *model.Article) (*Result, error) {
	var firstErr error
	for _, s := range m.Strategies {
		r, err := s.Fetch(ctx, article)
		if err != nil && firstErr == nil {
			firstErr = err
			continue
		}
		if r != nil && r.Text != "" {
			return r, nil
		}
	}
	return nil, firstErr
}
```

- [ ] **Step 2: Build to verify package compiles**

```bash
docker-compose exec -T api sh -c "cd /app && go build ./..." 2>&1 | tail -5
```

(If api container has no `go` binary, run rebuild instead: `docker-compose build api` and look for compile errors in output.)

Expected: clean build.

- [ ] **Step 3: Commit**

```bash
git add backend/internal/transcript/fetcher.go
git commit -m "feat(transcript): package skeleton — Fetcher interface and MultiFetcher composite"
```

---

## Task 3: `YouTubeCC` strategy

**Files:**
- Create: `backend/internal/transcript/youtube.go`
- Create: `backend/internal/transcript/youtube_test.go`
- Create: `backend/internal/transcript/testdata/youtube_watch_with_cc.html`
- Create: `backend/internal/transcript/testdata/youtube_track.json3`

- [ ] **Step 1: Create test fixtures**

`backend/internal/transcript/testdata/youtube_watch_with_cc.html` — a minimal HTML page that contains the bits we parse. **The implementer MUST capture this from a real YouTube watch page** (e.g. `curl -A 'Mozilla/...' 'https://www.youtube.com/watch?v=dQw4w9WgXcQ'` and extract the `var ytInitialPlayerResponse = {...};` line — paste into `<html><body><script>...</script></body></html>`). Keep only the `captions` subtree and a few sibling fields if needed for valid JSON. Trim down to a single track for `lang=en`, no asr, with a `baseUrl` like `https://www.youtube.com/api/timedtext?v=ID&lang=en` and a second track for `lang=zh-Hans, kind=asr`.

`backend/internal/transcript/testdata/youtube_track.json3` — paste a real JSON3 response (or write a synthetic one):

```json
{
  "events": [
    {"tStartMs": 0, "dDurationMs": 2000, "segs": [{"utf8": "Hello "}, {"utf8": "world."}]},
    {"tStartMs": 2000, "dDurationMs": 1500, "segs": [{"utf8": "This is a test."}]}
  ]
}
```

- [ ] **Step 2: Write the failing test**

Create `backend/internal/transcript/youtube_test.go`:

```go
package transcript

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bytedance/rss-pal/internal/model"
)

func TestYouTubeCC_Skips_NonYouTube(t *testing.T) {
	f := &YouTubeCC{}
	got, err := f.Fetch(context.Background(), &model.Article{MediaType: "video/bilibili"})
	if err != nil || got != nil {
		t.Fatalf("expected (nil, nil) for non-youtube article, got (%+v, %v)", got, err)
	}
}

func TestYouTubeCC_FetchesTranscript(t *testing.T) {
	htmlBytes, err := os.ReadFile(filepath.Join("testdata", "youtube_watch_with_cc.html"))
	if err != nil {
		t.Fatal(err)
	}
	trackBytes, err := os.ReadFile(filepath.Join("testdata", "youtube_track.json3"))
	if err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/watch"):
			w.Header().Set("Content-Type", "text/html")
			w.Write(htmlBytes)
		case strings.Contains(r.URL.Path, "/api/timedtext"):
			w.Header().Set("Content-Type", "application/json")
			w.Write(trackBytes)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	f := &YouTubeCC{
		WatchURLBase: srv.URL + "/watch?v=",
	}

	// MediaURL is the embed URL (youtube-nocookie.com/embed/ID), but the
	// strategy reads ID from there for fetching the watch page.
	got, err := f.Fetch(context.Background(), &model.Article{
		MediaType: "video/youtube",
		MediaURL:  "https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ?rel=0",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil {
		t.Fatal("expected Result, got nil")
	}
	if !strings.Contains(got.Text, "Hello world.") {
		t.Errorf("transcript text missing expected snippet: %q", got.Text)
	}
	if !strings.Contains(got.Text, "This is a test.") {
		t.Errorf("transcript text missing second event: %q", got.Text)
	}
	if got.Source == "" {
		t.Error("Source should not be empty")
	}
}

func TestYouTubeCC_NoCaptions(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// No ytInitialPlayerResponse in the body
		w.Write([]byte("<html><body><p>nothing here</p></body></html>"))
	}))
	defer srv.Close()

	f := &YouTubeCC{WatchURLBase: srv.URL + "/watch?v="}
	got, err := f.Fetch(context.Background(), &model.Article{
		MediaType: "video/youtube",
		MediaURL:  "https://www.youtube-nocookie.com/embed/abcdEFGHijk?rel=0",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil Result for video without captions, got %+v", got)
	}
}
```

- [ ] **Step 3: Run to verify fail**

```bash
docker-compose build api 2>&1 | grep -i error | head -5
```

Or once the package builds (it won't yet — type undefined), run:

```bash
cd /Users/bytedance/mygit/rss-pal && docker run --rm -v "$PWD/backend:/app" -w /app golang:1.24-alpine sh -c "go test ./internal/transcript/ -run YouTubeCC -v"
```

Expected: FAIL — `undefined: YouTubeCC`.

- [ ] **Step 4: Implement**

Create `backend/internal/transcript/youtube.go`:

```go
package transcript

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/bytedance/rss-pal/internal/model"
)

// YouTubeCC fetches captions from a YouTube watch page by parsing
// ytInitialPlayerResponse and following the chosen caption track's
// baseUrl with fmt=json3.
type YouTubeCC struct {
	// WatchURLBase is the prefix used to build the watch URL. Defaults to
	// "https://www.youtube.com/watch?v=" when empty. Override in tests.
	WatchURLBase string

	// HTTPClient lets tests override timeouts. Defaults to a 30s client.
	HTTPClient *http.Client
}

const userAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

// embedIDRe matches the 11-char video ID inside a youtube-nocookie embed URL.
var embedIDRe = regexp.MustCompile(`/embed/([A-Za-z0-9_-]{11})`)

// playerResponseRe captures the JSON object after "var ytInitialPlayerResponse = ".
// The capture is intentionally lazy and uses a balanced-brace scan in code.
var playerResponseRe = regexp.MustCompile(`(?s)ytInitialPlayerResponse\s*=\s*(\{)`)

func (f *YouTubeCC) Fetch(ctx context.Context, article *model.Article) (*Result, error) {
	if article == nil || article.MediaType != "video/youtube" {
		return nil, nil
	}
	id := extractYouTubeID(article.MediaURL)
	if id == "" {
		return nil, nil
	}
	base := f.WatchURLBase
	if base == "" {
		base = "https://www.youtube.com/watch?v="
	}
	client := f.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}

	html, err := fetchString(ctx, client, base+id)
	if err != nil {
		return nil, fmt.Errorf("fetch watch page: %w", err)
	}
	tracks, err := extractCaptionTracks(html)
	if err != nil {
		// Could not find ytInitialPlayerResponse → likely anti-bot page or
		// a video that simply has no captions. Treat as "no transcript",
		// not a transient error.
		return nil, nil
	}
	if len(tracks) == 0 {
		return nil, nil
	}
	chosen := pickTrack(tracks)
	trackURL := chosen.BaseURL
	if !strings.Contains(trackURL, "fmt=") {
		sep := "?"
		if strings.Contains(trackURL, "?") {
			sep = "&"
		}
		trackURL = trackURL + sep + "fmt=json3"
	}

	body, err := fetchString(ctx, client, trackURL)
	if err != nil {
		return nil, fmt.Errorf("fetch track: %w", err)
	}
	text, err := parseJSON3(body)
	if err != nil {
		return nil, nil
	}
	if strings.TrimSpace(text) == "" {
		return nil, nil
	}
	return &Result{Text: text, Source: sourceLabel(chosen)}, nil
}

func extractYouTubeID(mediaURL string) string {
	m := embedIDRe.FindStringSubmatch(mediaURL)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}

type captionTrack struct {
	BaseURL      string `json:"baseUrl"`
	LanguageCode string `json:"languageCode"`
	Kind         string `json:"kind"`
}

// extractCaptionTracks finds ytInitialPlayerResponse and walks down to
// captions.playerCaptionsTracklistRenderer.captionTracks.
func extractCaptionTracks(html string) ([]captionTrack, error) {
	loc := playerResponseRe.FindStringIndex(html)
	if loc == nil {
		return nil, errors.New("ytInitialPlayerResponse not found")
	}
	start := loc[1] - 1 // index of the opening brace
	end := scanBalancedJSON(html, start)
	if end < 0 {
		return nil, errors.New("could not scan balanced JSON")
	}
	var doc struct {
		Captions struct {
			PlayerCaptionsTracklistRenderer struct {
				CaptionTracks []captionTrack `json:"captionTracks"`
			} `json:"playerCaptionsTracklistRenderer"`
		} `json:"captions"`
	}
	if err := json.Unmarshal([]byte(html[start:end]), &doc); err != nil {
		return nil, err
	}
	return doc.Captions.PlayerCaptionsTracklistRenderer.CaptionTracks, nil
}

// scanBalancedJSON returns the index just past the matching closing brace
// of the JSON object whose opening brace is at start. Returns -1 if no
// balanced match exists or if the input contains an unterminated string.
func scanBalancedJSON(s string, start int) int {
	if start < 0 || start >= len(s) || s[start] != '{' {
		return -1
	}
	depth := 0
	inStr := false
	escape := false
	for i := start; i < len(s); i++ {
		c := s[i]
		if inStr {
			if escape {
				escape = false
				continue
			}
			if c == '\\' {
				escape = true
				continue
			}
			if c == '"' {
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i + 1
			}
		}
	}
	return -1
}

// pickTrack chooses Chinese > English > first; non-asr beats asr within a
// language preference.
func pickTrack(tracks []captionTrack) captionTrack {
	prefs := []string{"zh", "en"}
	for _, p := range prefs {
		// Two passes: non-asr first.
		for _, t := range tracks {
			if strings.HasPrefix(t.LanguageCode, p) && t.Kind != "asr" {
				return t
			}
		}
		for _, t := range tracks {
			if strings.HasPrefix(t.LanguageCode, p) {
				return t
			}
		}
	}
	for _, t := range tracks {
		if t.Kind != "asr" {
			return t
		}
	}
	return tracks[0]
}

func sourceLabel(t captionTrack) string {
	if t.Kind == "asr" {
		return "YouTube 自动字幕"
	}
	return "YouTube CC"
}

type json3Doc struct {
	Events []struct {
		Segs []struct {
			Utf8 string `json:"utf8"`
		} `json:"segs"`
	} `json:"events"`
}

func parseJSON3(body string) (string, error) {
	var doc json3Doc
	if err := json.Unmarshal([]byte(body), &doc); err != nil {
		return "", err
	}
	var b strings.Builder
	for _, ev := range doc.Events {
		var line strings.Builder
		for _, seg := range ev.Segs {
			line.WriteString(seg.Utf8)
		}
		s := strings.TrimSpace(line.String())
		if s == "" {
			continue
		}
		b.WriteString(s)
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String()), nil
}

func fetchString(ctx context.Context, client *http.Client, rawURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", rawURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 5<<20)) // 5 MiB cap
	if err != nil {
		return "", err
	}
	return string(body), nil
}

// resolveURL is unused for now but kept for symmetry with future strategies
// that need it. Defined here to avoid an import cycle issue with rss helper.
var _ = url.Parse
```

(Note the `var _ = url.Parse` line at the end is a placeholder to keep the `net/url` import in case future edits need it; remove if the implementer prefers to drop the unused import — `goimports` would do this automatically. Either way is fine.)

- [ ] **Step 5: Run tests**

```bash
cd /Users/bytedance/mygit/rss-pal && docker run --rm -v "$PWD/backend:/app" -w /app golang:1.24-alpine sh -c "go test ./internal/transcript/ -run YouTubeCC -v"
```

Expected: PASS for all three subtests.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/transcript/youtube.go backend/internal/transcript/youtube_test.go backend/internal/transcript/testdata/youtube_watch_with_cc.html backend/internal/transcript/testdata/youtube_track.json3
git commit -m "feat(transcript): YouTubeCC strategy parses watch page + json3 track"
```

---

## Task 4: `BilibiliCC` strategy

**Files:**
- Create: `backend/internal/transcript/bilibili.go`
- Create: `backend/internal/transcript/bilibili_test.go`
- Create: `backend/internal/transcript/testdata/bilibili_view.json`
- Create: `backend/internal/transcript/testdata/bilibili_player_v2.json`
- Create: `backend/internal/transcript/testdata/bilibili_subtitle.json`

- [ ] **Step 1: Create test fixtures**

`backend/internal/transcript/testdata/bilibili_view.json`:

```json
{
  "code": 0,
  "data": {
    "cid": 12345678,
    "bvid": "BV1xx411c7mD"
  }
}
```

`backend/internal/transcript/testdata/bilibili_player_v2.json`:

```json
{
  "code": 0,
  "data": {
    "subtitle": {
      "subtitles": [
        {
          "lan": "zh-CN",
          "subtitle_url": "//PLACEHOLDER/sub.json"
        }
      ]
    }
  }
}
```

(The implementer will rewrite `PLACEHOLDER` at test time to point at the httptest server.)

`backend/internal/transcript/testdata/bilibili_subtitle.json`:

```json
{
  "body": [
    {"from": 0.5, "to": 2.0, "content": "你好"},
    {"from": 2.0, "to": 4.0, "content": "世界"}
  ]
}
```

- [ ] **Step 2: Write the failing test**

```go
package transcript

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bytedance/rss-pal/internal/model"
)

func TestBilibiliCC_Skips_NonBilibili(t *testing.T) {
	f := &BilibiliCC{}
	got, err := f.Fetch(context.Background(), &model.Article{MediaType: "video/youtube"})
	if err != nil || got != nil {
		t.Fatalf("expected (nil, nil) for non-bilibili, got (%+v, %v)", got, err)
	}
}

func TestBilibiliCC_FetchesTranscript(t *testing.T) {
	viewBytes, _ := os.ReadFile(filepath.Join("testdata", "bilibili_view.json"))
	playerBytes, _ := os.ReadFile(filepath.Join("testdata", "bilibili_player_v2.json"))
	subBytes, _ := os.ReadFile(filepath.Join("testdata", "bilibili_subtitle.json"))

	var subURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/x/web-interface/view"):
			w.Header().Set("Content-Type", "application/json")
			w.Write(viewBytes)
		case strings.Contains(r.URL.Path, "/x/player/v2"):
			// Rewrite the placeholder subtitle URL to point back at this server.
			body := strings.Replace(string(playerBytes), "//PLACEHOLDER/sub.json", subURL, 1)
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(body))
		case strings.HasSuffix(r.URL.Path, "/sub.json"):
			w.Header().Set("Content-Type", "application/json")
			w.Write(subBytes)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	subURL = srv.URL + "/sub.json"

	f := &BilibiliCC{
		ViewURLBase:    srv.URL + "/x/web-interface/view?bvid=",
		PlayerV2URLFmt: srv.URL + "/x/player/v2?cid=%d&bvid=%s",
	}

	got, err := f.Fetch(context.Background(), &model.Article{
		MediaType: "video/bilibili",
		MediaURL:  "https://player.bilibili.com/player.html?bvid=BV1xx411c7mD&page=1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil {
		t.Fatal("expected Result")
	}
	if !strings.Contains(got.Text, "你好") || !strings.Contains(got.Text, "世界") {
		t.Errorf("transcript missing expected lines: %q", got.Text)
	}
	if got.Source == "" {
		t.Error("source should not be empty")
	}
}

func TestBilibiliCC_NoSubtitles(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/x/web-interface/view"):
			w.Write([]byte(`{"code":0,"data":{"cid":1,"bvid":"BV1xx411c7mD"}}`))
		case strings.Contains(r.URL.Path, "/x/player/v2"):
			w.Write([]byte(`{"code":0,"data":{"subtitle":{"subtitles":[]}}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	f := &BilibiliCC{
		ViewURLBase:    srv.URL + "/x/web-interface/view?bvid=",
		PlayerV2URLFmt: srv.URL + "/x/player/v2?cid=%d&bvid=%s",
	}
	got, err := f.Fetch(context.Background(), &model.Article{
		MediaType: "video/bilibili",
		MediaURL:  "https://player.bilibili.com/player.html?bvid=BV1xx411c7mD&page=1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for no-subtitles video, got %+v", got)
	}
	_ = json.RawMessage{} // keep encoding/json import
}
```

- [ ] **Step 3: Run to verify fail**

```bash
cd /Users/bytedance/mygit/rss-pal && docker run --rm -v "$PWD/backend:/app" -w /app golang:1.24-alpine sh -c "go test ./internal/transcript/ -run BilibiliCC -v"
```

Expected: FAIL — `undefined: BilibiliCC`.

- [ ] **Step 4: Implement**

```go
package transcript

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/bytedance/rss-pal/internal/model"
)

// BilibiliCC fetches captions from Bilibili using the player/v2 API
// (no WBI signing required as of writing).
type BilibiliCC struct {
	ViewURLBase    string // default: "https://api.bilibili.com/x/web-interface/view?bvid="
	PlayerV2URLFmt string // default: "https://api.bilibili.com/x/player/v2?cid=%d&bvid=%s"
	HTTPClient     *http.Client
}

var bvidQueryRe = regexp.MustCompile(`bvid=(BV[A-Za-z0-9]{10})`)

func (f *BilibiliCC) Fetch(ctx context.Context, article *model.Article) (*Result, error) {
	if article == nil || article.MediaType != "video/bilibili" {
		return nil, nil
	}
	bvid := extractBvid(article.MediaURL)
	if bvid == "" {
		return nil, nil
	}
	viewBase := f.ViewURLBase
	if viewBase == "" {
		viewBase = "https://api.bilibili.com/x/web-interface/view?bvid="
	}
	playerFmt := f.PlayerV2URLFmt
	if playerFmt == "" {
		playerFmt = "https://api.bilibili.com/x/player/v2?cid=%d&bvid=%s"
	}
	client := f.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}

	body, err := fetchString(ctx, client, viewBase+bvid)
	if err != nil {
		return nil, fmt.Errorf("view: %w", err)
	}
	var view struct {
		Code int `json:"code"`
		Data struct {
			Cid int64 `json:"cid"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &view); err != nil || view.Code != 0 || view.Data.Cid == 0 {
		return nil, nil
	}

	body, err = fetchString(ctx, client, fmt.Sprintf(playerFmt, view.Data.Cid, bvid))
	if err != nil {
		return nil, fmt.Errorf("player/v2: %w", err)
	}
	var player struct {
		Code int `json:"code"`
		Data struct {
			Subtitle struct {
				Subtitles []struct {
					Lan         string `json:"lan"`
					SubtitleURL string `json:"subtitle_url"`
				} `json:"subtitles"`
			} `json:"subtitle"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &player); err != nil || player.Code != 0 {
		return nil, nil
	}
	subs := player.Data.Subtitle.Subtitles
	if len(subs) == 0 {
		return nil, nil
	}
	chosen := subs[0]
	for _, s := range subs {
		if strings.HasPrefix(s.Lan, "zh") {
			chosen = s
			break
		}
	}
	subURL := chosen.SubtitleURL
	if strings.HasPrefix(subURL, "//") {
		subURL = "https:" + subURL
	}
	body, err = fetchString(ctx, client, subURL)
	if err != nil {
		return nil, fmt.Errorf("subtitle: %w", err)
	}
	var sub struct {
		Body []struct {
			Content string `json:"content"`
		} `json:"body"`
	}
	if err := json.Unmarshal([]byte(body), &sub); err != nil || len(sub.Body) == 0 {
		return nil, nil
	}
	var b strings.Builder
	for _, line := range sub.Body {
		s := strings.TrimSpace(line.Content)
		if s == "" {
			continue
		}
		b.WriteString(s)
		b.WriteString("\n")
	}
	text := strings.TrimSpace(b.String())
	if text == "" {
		return nil, nil
	}
	return &Result{Text: text, Source: "Bilibili CC"}, nil
}

func extractBvid(mediaURL string) string {
	m := bvidQueryRe.FindStringSubmatch(mediaURL)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}
```

- [ ] **Step 5: Run tests**

```bash
cd /Users/bytedance/mygit/rss-pal && docker run --rm -v "$PWD/backend:/app" -w /app golang:1.24-alpine sh -c "go test ./internal/transcript/ -run BilibiliCC -v"
```

Expected: PASS for all three subtests.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/transcript/bilibili.go backend/internal/transcript/bilibili_test.go backend/internal/transcript/testdata/bilibili_view.json backend/internal/transcript/testdata/bilibili_player_v2.json backend/internal/transcript/testdata/bilibili_subtitle.json
git commit -m "feat(transcript): BilibiliCC strategy via player/v2 API"
```

---

## Task 5: Subtitle file parsers (`.vtt`, `.srt`, `.txt`)

**Files:**
- Create: `backend/internal/transcript/subtitle_files.go`
- Create: `backend/internal/transcript/subtitle_files_test.go`

- [ ] **Step 1: Write the failing test**

```go
package transcript

import (
	"strings"
	"testing"
)

func TestParseVTT(t *testing.T) {
	in := `WEBVTT

1
00:00:00.500 --> 00:00:02.000
Hello world.

2
00:00:02.000 --> 00:00:04.500
This is a test.

NOTE this is a note line, ignored

3
00:00:04.500 --> 00:00:06.000
Final cue.
`
	got := ParseVTT(in)
	want := "Hello world.\nThis is a test.\nFinal cue."
	if got != want {
		t.Errorf("ParseVTT() =\n  got:  %q\n  want: %q", got, want)
	}
}

func TestParseSRT(t *testing.T) {
	in := "1\n00:00:00,500 --> 00:00:02,000\nHello world.\n\n2\n00:00:02,000 --> 00:00:04,500\nThis is a test.\n"
	got := ParseSRT(in)
	want := "Hello world.\nThis is a test."
	if got != want {
		t.Errorf("ParseSRT() =\n  got:  %q\n  want: %q", got, want)
	}
}

func TestParsePlainText(t *testing.T) {
	in := "  Hello world.\n\n  Goodbye.  \n"
	got := ParsePlainText(in)
	want := "Hello world.\n\nGoodbye."
	if got != want {
		t.Errorf("ParsePlainText() = %q, want %q", got, want)
	}
}

func TestParseSubtitleFile_DispatchesByExtension(t *testing.T) {
	cases := []struct {
		url  string
		body string
		want string
	}{
		{"https://x.com/a.vtt", "WEBVTT\n\n1\n00:00:00.000 --> 00:00:01.000\nHi", "Hi"},
		{"https://x.com/a.srt", "1\n00:00:00,000 --> 00:00:01,000\nHi", "Hi"},
		{"https://x.com/a.txt", "Hi", "Hi"},
		{"https://x.com/a.pdf", "binary garbage", ""}, // unsupported → empty
	}
	for _, tc := range cases {
		got := ParseSubtitleFile(tc.url, tc.body)
		got = strings.TrimSpace(got)
		if got != tc.want {
			t.Errorf("ParseSubtitleFile(%s) = %q, want %q", tc.url, got, tc.want)
		}
	}
}
```

- [ ] **Step 2: Run to verify fail**

```bash
cd /Users/bytedance/mygit/rss-pal && docker run --rm -v "$PWD/backend:/app" -w /app golang:1.24-alpine sh -c "go test ./internal/transcript/ -run 'Parse' -v"
```

Expected: FAIL — undefined.

- [ ] **Step 3: Implement**

```go
package transcript

import (
	"regexp"
	"strings"
)

// Timestamp lines look like: 00:00:00.500 --> 00:00:02.000  (VTT)
// or:                       00:00:00,500 --> 00:00:02,000  (SRT)
var timestampRe = regexp.MustCompile(`^\d{2}:\d{2}:\d{2}[,.]\d{3}\s+-->\s+\d{2}:\d{2}:\d{2}[,.]\d{3}`)
var cueIndexRe = regexp.MustCompile(`^\d+$`)

// ParseVTT extracts text-only content from a WebVTT body.
// Header line, NOTE blocks, cue indices, and timestamp lines are dropped.
func ParseVTT(body string) string {
	var b strings.Builder
	skipBlock := false
	for _, raw := range strings.Split(body, "\n") {
		line := strings.TrimRight(raw, "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			skipBlock = false
			continue
		}
		if strings.HasPrefix(trimmed, "WEBVTT") {
			continue
		}
		if strings.HasPrefix(trimmed, "NOTE") {
			skipBlock = true
			continue
		}
		if skipBlock {
			continue
		}
		if cueIndexRe.MatchString(trimmed) {
			continue
		}
		if timestampRe.MatchString(trimmed) {
			continue
		}
		b.WriteString(trimmed)
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}

// ParseSRT extracts text-only content from a SubRip body. SRT and VTT are
// nearly identical at the line-classification level so we share the pattern.
func ParseSRT(body string) string {
	var b strings.Builder
	for _, raw := range strings.Split(body, "\n") {
		line := strings.TrimRight(raw, "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if cueIndexRe.MatchString(trimmed) {
			continue
		}
		if timestampRe.MatchString(trimmed) {
			continue
		}
		b.WriteString(trimmed)
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}

// ParsePlainText collapses excessive whitespace but preserves paragraph breaks.
func ParsePlainText(body string) string {
	body = strings.ReplaceAll(body, "\r\n", "\n")
	// Collapse 3+ newlines into exactly 2 (blank-line paragraph separator).
	for strings.Contains(body, "\n\n\n") {
		body = strings.ReplaceAll(body, "\n\n\n", "\n\n")
	}
	// Trim each line.
	lines := strings.Split(body, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSpace(l)
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// ParseSubtitleFile dispatches by URL suffix. Unsupported extensions
// return empty string.
func ParseSubtitleFile(rawURL, body string) string {
	lower := strings.ToLower(rawURL)
	switch {
	case strings.HasSuffix(lower, ".vtt"):
		return ParseVTT(body)
	case strings.HasSuffix(lower, ".srt"):
		return ParseSRT(body)
	case strings.HasSuffix(lower, ".txt"):
		return ParsePlainText(body)
	default:
		return ""
	}
}
```

- [ ] **Step 4: Run tests**

```bash
cd /Users/bytedance/mygit/rss-pal && docker run --rm -v "$PWD/backend:/app" -w /app golang:1.24-alpine sh -c "go test ./internal/transcript/ -run 'Parse' -v"
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/transcript/subtitle_files.go backend/internal/transcript/subtitle_files_test.go
git commit -m "feat(transcript): VTT/SRT/TXT subtitle file parsers"
```

---

## Task 6: Extract `FetchHTMLDocument` from `ContentFetcher`

**Files:**
- Modify: `backend/internal/rss/content.go` (add new exported method, no breaking changes)

`HTMLPageScraper` (Task 7) needs the goquery `*Document` for the article URL — `FetchContent` returns markdown, which is too lossy. The existing `fetchDirect` already builds a `Document` internally; we just expose a small public wrapper.

- [ ] **Step 1: Add the method**

In `backend/internal/rss/content.go`, after the existing `FetchContent` method (~line 78), add:

```go
// FetchHTMLDocument fetches the URL and returns a parsed goquery document.
// Used by callers that need DOM-level access (e.g. transcript discovery)
// rather than a clean markdown extraction. No content cleanup is applied
// — script/style/avatar removal is the caller's responsibility.
//
// Returns (nil, nil) on a non-200 response so callers can treat HTTP errors
// the same way as "no transcript available". Returns a non-nil error only
// for transport-level failures.
func (f *ContentFetcher) FetchHTMLDocument(ctx context.Context, pageURL string) (*goquery.Document, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", pageURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, nil
	}

	return goquery.NewDocumentFromReader(resp.Body)
}
```

- [ ] **Step 2: Build to verify**

```bash
cd /Users/bytedance/mygit/rss-pal && docker run --rm -v "$PWD/backend:/app" -w /app golang:1.24-alpine sh -c "go build ./..."
```

Expected: clean build.

- [ ] **Step 3: Commit**

```bash
git add backend/internal/rss/content.go
git commit -m "feat(rss): expose ContentFetcher.FetchHTMLDocument helper"
```

---

## Task 7: `HTMLPageScraper` strategy

**Files:**
- Create: `backend/internal/transcript/html_page.go`
- Create: `backend/internal/transcript/html_page_test.go`
- Create: `backend/internal/transcript/testdata/page_inline_transcript.html`
- Create: `backend/internal/transcript/testdata/page_linked_vtt.html`
- Create: `backend/internal/transcript/testdata/page_two_hop_announce.html`
- Create: `backend/internal/transcript/testdata/page_two_hop_target.html`
- Create: `backend/internal/transcript/testdata/page_no_transcript.html`

- [ ] **Step 1: Create test fixtures**

`testdata/page_inline_transcript.html`:

```html
<!DOCTYPE html>
<html><body>
<h1>An interesting episode</h1>
<p>Some intro.</p>
<h3>Transcript</h3>
<p>This is the first paragraph of the actual transcript text. It needs to be at least two hundred characters long for the strategy to accept it as a valid transcript section, otherwise it will be skipped on the grounds that the heading might be a navigation link or table-of-contents entry rather than real content. Padding padding padding.</p>
<p>And a second paragraph for good measure.</p>
<h3>Resources</h3>
<p>Some links.</p>
</body></html>
```

`testdata/page_linked_vtt.html`:

```html
<!DOCTYPE html>
<html><body>
<h1>Episode 42</h1>
<p>Listen below.</p>
<a href="/transcripts/ep-42.vtt">Download transcript (VTT)</a>
</body></html>
```

`testdata/page_two_hop_announce.html`:

```html
<!DOCTYPE html>
<html><body>
<h1>Programme page</h1>
<p>Find a transcript at: https://learn.example.com/episodes/ep-42</p>
</body></html>
```

`testdata/page_two_hop_target.html`:

```html
<!DOCTYPE html>
<html><body>
<h2>Transcript</h2>
<p>This is the inline transcript text appearing on the linked page after a two-hop traversal. It also needs to exceed two hundred characters to be accepted by the inline-detection branch, so we add this padding. The strategy should follow the announcement pattern from the original article URL into this linked page and re-run its detection logic here.</p>
</body></html>
```

`testdata/page_no_transcript.html`:

```html
<!DOCTYPE html>
<html><body>
<h1>Random article</h1>
<p>Lorem ipsum.</p>
<a href="https://example.com/about">About us</a>
</body></html>
```

- [ ] **Step 2: Write the failing test**

```go
package transcript

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/bytedance/rss-pal/internal/model"
)

// docFetcher is a tiny stub of the *goquery.Document fetcher interface
// HTMLPageScraper depends on. The real impl wraps ContentFetcher.
type stubDocFetcher struct {
	pages map[string]string
}

func (s *stubDocFetcher) FetchHTMLDocument(ctx context.Context, url string) (*goquery.Document, error) {
	html, ok := s.pages[url]
	if !ok {
		return nil, nil
	}
	return goquery.NewDocumentFromReader(strings.NewReader(html))
}

func TestHTMLPageScraper_InlineTranscript(t *testing.T) {
	html, _ := os.ReadFile(filepath.Join("testdata", "page_inline_transcript.html"))
	stub := &stubDocFetcher{pages: map[string]string{"http://example.com/ep1": string(html)}}
	f := &HTMLPageScraper{Docs: stub}

	got, err := f.Fetch(context.Background(), &model.Article{
		URL:       "http://example.com/ep1",
		MediaType: "audio/mpeg",
	})
	if err != nil || got == nil {
		t.Fatalf("expected Result, got (%+v, %v)", got, err)
	}
	if !strings.Contains(got.Text, "first paragraph of the actual transcript") {
		t.Errorf("inline transcript missing expected text: %q", got.Text)
	}
}

func TestHTMLPageScraper_LinkedVTT(t *testing.T) {
	html, _ := os.ReadFile(filepath.Join("testdata", "page_linked_vtt.html"))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/ep-42.vtt") {
			w.Write([]byte("WEBVTT\n\n1\n00:00:00.000 --> 00:00:01.000\nLinked transcript content."))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	pageURL := "http://example.com/show/42"
	htmlWithBase := strings.ReplaceAll(string(html), `href="/transcripts/ep-42.vtt"`,
		`href="`+srv.URL+`/transcripts/ep-42.vtt"`)
	stub := &stubDocFetcher{pages: map[string]string{pageURL: htmlWithBase}}

	f := &HTMLPageScraper{Docs: stub, HTTPClient: srv.Client()}

	got, err := f.Fetch(context.Background(), &model.Article{URL: pageURL, MediaType: "audio/mpeg"})
	if err != nil || got == nil {
		t.Fatalf("expected Result, got (%+v, %v)", got, err)
	}
	if !strings.Contains(got.Text, "Linked transcript content") {
		t.Errorf("linked vtt content missing: %q", got.Text)
	}
}

func TestHTMLPageScraper_TwoHop(t *testing.T) {
	announce, _ := os.ReadFile(filepath.Join("testdata", "page_two_hop_announce.html"))
	target, _ := os.ReadFile(filepath.Join("testdata", "page_two_hop_target.html"))
	stub := &stubDocFetcher{pages: map[string]string{
		"http://example.com/programmes/ep1":      string(announce),
		"https://learn.example.com/episodes/ep-42": string(target),
	}}
	f := &HTMLPageScraper{Docs: stub}
	got, err := f.Fetch(context.Background(), &model.Article{
		URL:       "http://example.com/programmes/ep1",
		MediaType: "audio/mpeg",
	})
	if err != nil || got == nil {
		t.Fatalf("expected Result via two-hop, got (%+v, %v)", got, err)
	}
	if !strings.Contains(got.Text, "linked page after a two-hop traversal") {
		t.Errorf("two-hop transcript missing expected text: %q", got.Text)
	}
}

func TestHTMLPageScraper_NoTranscript(t *testing.T) {
	html, _ := os.ReadFile(filepath.Join("testdata", "page_no_transcript.html"))
	stub := &stubDocFetcher{pages: map[string]string{"http://example.com/x": string(html)}}
	f := &HTMLPageScraper{Docs: stub}
	got, err := f.Fetch(context.Background(), &model.Article{
		URL:       "http://example.com/x",
		MediaType: "audio/mpeg",
	})
	if err != nil || got != nil {
		t.Errorf("expected (nil, nil), got (%+v, %v)", got, err)
	}
}
```

- [ ] **Step 3: Run to verify fail**

```bash
cd /Users/bytedance/mygit/rss-pal && docker run --rm -v "$PWD/backend:/app" -w /app golang:1.24-alpine sh -c "go test ./internal/transcript/ -run HTMLPage -v"
```

Expected: FAIL — `undefined: HTMLPageScraper`.

- [ ] **Step 4: Implement**

```go
package transcript

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/bytedance/rss-pal/internal/model"
)

// docFetcher is the slice of ContentFetcher's surface area we need.
// Defined as an interface so tests can stub it.
type docFetcher interface {
	FetchHTMLDocument(ctx context.Context, url string) (*goquery.Document, error)
}

// HTMLPageScraper looks for a transcript on the article's source HTML page
// using three sub-strategies in priority order: inline transcript section,
// linked .vtt/.srt/.txt files, and a two-hop "Find a transcript at: <URL>"
// announcement pattern.
type HTMLPageScraper struct {
	Docs       docFetcher
	HTTPClient *http.Client // for fetching linked subtitle files
}

const inlineMinChars = 200

var (
	transcriptHeadingRe = regexp.MustCompile(`(?i)\b(transcript|字幕|逐字稿)\b`)
	announceRe          = regexp.MustCompile(`(?i)find\s+(?:a\s+)?transcript.*?(https?://\S+)`)
	subtitleExtRe       = regexp.MustCompile(`(?i)\.(vtt|srt|txt)(?:[?#].*)?$`)
)

func (f *HTMLPageScraper) Fetch(ctx context.Context, article *model.Article) (*Result, error) {
	if article == nil || article.URL == "" {
		return nil, nil
	}
	if !strings.HasPrefix(article.MediaType, "audio/") && !strings.HasPrefix(article.MediaType, "video/") {
		return nil, nil
	}
	doc, err := f.Docs.FetchHTMLDocument(ctx, article.URL)
	if err != nil {
		return nil, fmt.Errorf("fetch html: %w", err)
	}
	if doc == nil {
		return nil, nil
	}
	host := hostOf(article.URL)

	// (a) Inline transcript section.
	if text := findInlineTranscript(doc); text != "" {
		return &Result{Text: text, Source: host + " 网页字幕"}, nil
	}

	// (b) Linked subtitle file.
	if text, src := f.tryLinkedSubtitle(ctx, doc, article.URL); text != "" {
		return &Result{Text: text, Source: src}, nil
	}

	// (c) Two-hop announcement.
	if next := findAnnouncedTranscriptURL(doc); next != "" {
		nextDoc, err := f.Docs.FetchHTMLDocument(ctx, next)
		if err == nil && nextDoc != nil {
			if text := findInlineTranscript(nextDoc); text != "" {
				return &Result{Text: text, Source: hostOf(next) + " 网页字幕"}, nil
			}
			if text, src := f.tryLinkedSubtitle(ctx, nextDoc, next); text != "" {
				return &Result{Text: text, Source: src}, nil
			}
		}
	}
	return nil, nil
}

func findInlineTranscript(doc *goquery.Document) string {
	var found string
	doc.Find("h1, h2, h3, h4").EachWithBreak(func(_ int, h *goquery.Selection) bool {
		if !transcriptHeadingRe.MatchString(h.Text()) {
			return true
		}
		// Walk forward through siblings until next heading.
		var b strings.Builder
		for s := h.Next(); s.Length() > 0; s = s.Next() {
			tag := goquery.NodeName(s)
			if tag == "h1" || tag == "h2" || tag == "h3" || tag == "h4" {
				break
			}
			text := strings.TrimSpace(s.Text())
			if text == "" {
				continue
			}
			b.WriteString(text)
			b.WriteString("\n\n")
		}
		text := strings.TrimSpace(b.String())
		if len([]rune(text)) >= inlineMinChars {
			found = text
			return false
		}
		return true
	})
	return found
}

func (f *HTMLPageScraper) tryLinkedSubtitle(ctx context.Context, doc *goquery.Document, baseURL string) (string, string) {
	client := f.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	var foundText, foundSource string
	doc.Find("a[href]").EachWithBreak(func(_ int, a *goquery.Selection) bool {
		href, _ := a.Attr("href")
		text := a.Text()
		hrefHasExt := subtitleExtRe.MatchString(href)
		looksLikeTranscript := transcriptHeadingRe.MatchString(text) || transcriptHeadingRe.MatchString(href)
		if !(hrefHasExt && looksLikeTranscript) && !looksLikeTranscript {
			return true
		}
		if !hrefHasExt {
			// Skip non-subtitle file links (e.g. an HTML page).
			return true
		}
		abs := resolveURL(baseURL, href)
		body, err := fetchString(ctx, client, abs)
		if err != nil {
			return true
		}
		// Cap at 1 MiB defensively (fetchString already caps at 5).
		if len(body) > 1<<20 {
			body = body[:1<<20]
		}
		parsed := strings.TrimSpace(ParseSubtitleFile(abs, body))
		if parsed == "" {
			return true
		}
		foundText = parsed
		foundSource = hostOf(baseURL) + " 字幕文件"
		return false
	})
	return foundText, foundSource
}

func findAnnouncedTranscriptURL(doc *goquery.Document) string {
	body := doc.Find("body").Text()
	m := announceRe.FindStringSubmatch(body)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}

func hostOf(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return ""
	}
	return strings.TrimPrefix(u.Host, "www.")
}

func resolveURL(base, href string) string {
	if strings.HasPrefix(href, "http://") || strings.HasPrefix(href, "https://") {
		return href
	}
	bu, err := url.Parse(base)
	if err != nil {
		return href
	}
	hu, err := url.Parse(href)
	if err != nil {
		return href
	}
	return bu.ResolveReference(hu).String()
}

// keep io import alive for future cap changes
var _ = io.Discard
```

(The `var _ = io.Discard` line is again a placeholder for the implementer; remove if unused.)

- [ ] **Step 5: Run tests**

```bash
cd /Users/bytedance/mygit/rss-pal && docker run --rm -v "$PWD/backend:/app" -w /app golang:1.24-alpine sh -c "go test ./internal/transcript/ -v"
```

Expected: all pass — earlier strategies' tests still green, new HTMLPage tests green.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/transcript/html_page.go backend/internal/transcript/html_page_test.go backend/internal/transcript/testdata/page_*.html
git commit -m "feat(transcript): HTMLPageScraper — inline section, linked subtitle file, two-hop announce"
```

---

## Task 8: `MultiFetcher` priority test

**Files:**
- Create: `backend/internal/transcript/fetcher_test.go`

The MultiFetcher type was added in Task 2 but had no test. Add one now to verify priority + error coalescing.

- [ ] **Step 1: Write the test**

```go
package transcript

import (
	"context"
	"errors"
	"testing"

	"github.com/bytedance/rss-pal/internal/model"
)

type stubFetcher struct {
	r   *Result
	err error
}

func (s *stubFetcher) Fetch(ctx context.Context, _ *model.Article) (*Result, error) {
	return s.r, s.err
}

func TestMultiFetcher_FirstHitWins(t *testing.T) {
	m := &MultiFetcher{Strategies: []Fetcher{
		&stubFetcher{},                                // returns nil, nil
		&stubFetcher{r: &Result{Text: "second", Source: "stub2"}},
		&stubFetcher{r: &Result{Text: "third", Source: "stub3"}},
	}}
	got, err := m.Fetch(context.Background(), &model.Article{})
	if err != nil || got == nil || got.Text != "second" {
		t.Errorf("expected second to win, got (%+v, %v)", got, err)
	}
}

func TestMultiFetcher_AllNil(t *testing.T) {
	m := &MultiFetcher{Strategies: []Fetcher{&stubFetcher{}, &stubFetcher{}}}
	got, err := m.Fetch(context.Background(), &model.Article{})
	if err != nil || got != nil {
		t.Errorf("expected (nil, nil), got (%+v, %v)", got, err)
	}
}

func TestMultiFetcher_ErrorThenSuccess(t *testing.T) {
	m := &MultiFetcher{Strategies: []Fetcher{
		&stubFetcher{err: errors.New("transient")},
		&stubFetcher{r: &Result{Text: "ok", Source: "stub"}},
	}}
	got, err := m.Fetch(context.Background(), &model.Article{})
	if err != nil || got == nil || got.Text != "ok" {
		t.Errorf("expected success after transient err, got (%+v, %v)", got, err)
	}
}

func TestMultiFetcher_AllErrorsReturnFirst(t *testing.T) {
	first := errors.New("first")
	m := &MultiFetcher{Strategies: []Fetcher{
		&stubFetcher{err: first},
		&stubFetcher{err: errors.New("second")},
	}}
	got, err := m.Fetch(context.Background(), &model.Article{})
	if got != nil || !errors.Is(err, first) {
		t.Errorf("expected first error, got (%+v, %v)", got, err)
	}
}
```

- [ ] **Step 2: Run tests**

```bash
cd /Users/bytedance/mygit/rss-pal && docker run --rm -v "$PWD/backend:/app" -w /app golang:1.24-alpine sh -c "go test ./internal/transcript/ -run MultiFetcher -v"
```

Expected: PASS for all 4 subtests.

- [ ] **Step 3: Commit**

```bash
git add backend/internal/transcript/fetcher_test.go
git commit -m "test(transcript): MultiFetcher priority and error coalescing"
```

---

## Task 9: Repository methods

**Files:**
- Modify: `backend/internal/repository/article.go`

Three new methods plus a tweak to one existing query.

- [ ] **Step 1: Add the three new methods**

In `backend/internal/repository/article.go`, after the existing `IncrementRefetchAttempts` method (around line 262), append:

```go
// GetMediaArticlesWithoutTranscript returns up to limit video/audio
// articles that have not yet had a transcript fetch attempt.
func (r *ArticleRepository) GetMediaArticlesWithoutTranscript(limit int) ([]model.Article, error) {
	query := `
		SELECT id, feed_id, title, url, content, published_at, summary_brief, summary_detailed, fetched_at, word_count, reading_minutes, media_url, media_type, media_duration_seconds
		FROM articles
		WHERE transcript_fetched_at IS NULL
		  AND media_type IS NOT NULL
		  AND (media_type LIKE 'video/%' OR media_type LIKE 'audio/%')
		ORDER BY fetched_at DESC
		LIMIT $1
	`
	rows, err := r.db.Query(query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return r.scanArticleNoFeedTitle(rows)
}

// UpdateContentAndResetSummary atomically updates content + recomputed
// metrics, clears any existing summary, and stamps transcript_fetched_at.
// Used when transcript fetching succeeds. Clearing the summary is what
// feeds the article back to backfillSummaries on the next worker cycle.
func (r *ArticleRepository) UpdateContentAndResetSummary(id int, content string, wordCount, readingMinutes int) error {
	_, err := r.db.Exec(`
		UPDATE articles
		SET content = $1,
		    word_count = $2,
		    reading_minutes = $3,
		    summary_brief = NULL,
		    summary_detailed = NULL,
		    transcript_fetched_at = NOW(),
		    refetch_attempts = 0
		WHERE id = $4
	`, content, wordCount, readingMinutes, id)
	return err
}

// MarkTranscriptFetchAttempted records that we tried and failed to find
// a transcript for the article, preventing retries.
func (r *ArticleRepository) MarkTranscriptFetchAttempted(id int) error {
	_, err := r.db.Exec(`UPDATE articles SET transcript_fetched_at = NOW() WHERE id = $1`, id)
	return err
}
```

- [ ] **Step 2: Tweak `GetArticlesWithShortContent`**

Find the existing `GetArticlesWithShortContent` method (~line 344). Its current `WHERE` clause has `f.feed_type NOT IN ('youtube', 'podcast')`. We additionally need to skip any article whose `media_type` indicates video/audio — those are the transcript pipeline's responsibility, and re-fetching their URLs would clobber the transcript-augmented content.

Replace the entire query string in that method with:

```go
	query := `
		SELECT a.id, a.feed_id, a.title, a.url, a.content, a.published_at, a.summary_brief, a.summary_detailed, a.fetched_at, a.word_count, a.reading_minutes, a.media_url, a.media_type, a.media_duration_seconds
		FROM articles a
		JOIN feeds f ON a.feed_id = f.id
		WHERE a.url != '' AND a.refetch_attempts < 5
		  AND f.feed_type NOT IN ('youtube', 'podcast')
		  AND (a.media_type IS NULL OR (a.media_type NOT LIKE 'video/%' AND a.media_type NOT LIKE 'audio/%'))
		  AND ((LENGTH(a.content) < $1 OR a.content IS NULL AND a.fetched_at > NOW() - INTERVAL '7 days')
		       OR (a.content LIKE '%<%>%' AND a.fetched_at > NOW() - INTERVAL '30 days'))
		ORDER BY a.fetched_at DESC
		LIMIT 50
	`
```

(The new line is the `AND (a.media_type IS NULL OR ...)` filter.)

- [ ] **Step 3: Build to verify**

```bash
cd /Users/bytedance/mygit/rss-pal && docker run --rm -v "$PWD/backend:/app" -w /app golang:1.24-alpine sh -c "go build ./..."
```

Expected: clean build.

- [ ] **Step 4: Commit**

```bash
git add backend/internal/repository/article.go
git commit -m "feat(db): repo methods for transcript backfill + skip media in short-content refetch"
```

---

## Task 10: Worker `backfillTranscripts` step

**Files:**
- Modify: `backend/cmd/worker/main.go`

- [ ] **Step 1: Add the function**

In `backend/cmd/worker/main.go`, add a new constant near the top (with the other limits, ~line 28):

```go
const maxTranscriptBackfillPerCycle = 5
```

Add an import for the new package — find the existing import block at the top of the file and add:

```go
	"github.com/bytedance/rss-pal/internal/transcript"
```

Add the `backfillTranscripts` function. Place it next to `backfillSummaries` (~line 118):

```go
func backfillTranscripts(ctx context.Context, articleRepo *repository.ArticleRepository, fetcher transcript.Fetcher) {
	articles, err := articleRepo.GetMediaArticlesWithoutTranscript(maxTranscriptBackfillPerCycle)
	if err != nil {
		log.Printf("Failed to get media articles without transcript: %v", err)
		return
	}
	if len(articles) == 0 {
		return
	}
	log.Printf("Fetching transcripts for %d media articles", len(articles))

	var wg sync.WaitGroup
	sem := make(chan struct{}, maxConcurrentContent)
	for i := range articles {
		a := &articles[i]
		wg.Add(1)
		go func(article *model.Article) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			tCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
			defer cancel()

			result, err := fetcher.Fetch(tCtx, article)
			if err != nil {
				log.Printf("Transcript fetch error for article %d: %v", article.ID, err)
				return // leave transcript_fetched_at NULL → retried next cycle
			}
			if result == nil || strings.TrimSpace(result.Text) == "" {
				if err := articleRepo.MarkTranscriptFetchAttempted(article.ID); err != nil {
					log.Printf("Failed to mark transcript attempt for article %d: %v", article.ID, err)
				}
				return
			}
			newContent := buildContentWithTranscript(article.Content, result)
			wc, rm := rss.ComputeMetrics(newContent)
			if err := articleRepo.UpdateContentAndResetSummary(article.ID, newContent, wc, rm); err != nil {
				log.Printf("Failed to save transcript for article %d: %v", article.ID, err)
				return
			}
			log.Printf("Transcript fetched for article %d (source=%s, %d chars)", article.ID, result.Source, len(result.Text))
		}(a)
	}
	wg.Wait()
}

// buildContentWithTranscript appends the transcript to existing article
// content using the markdown separator pattern documented in the spec.
func buildContentWithTranscript(existing string, r *transcript.Result) string {
	existing = strings.TrimSpace(existing)
	var b strings.Builder
	if existing != "" {
		b.WriteString(existing)
		b.WriteString("\n\n---\n\n")
	}
	b.WriteString("## 字幕\n\n")
	if r.Source != "" {
		b.WriteString("> 来源：")
		b.WriteString(r.Source)
		b.WriteString("\n\n")
	}
	b.WriteString(strings.TrimSpace(r.Text))
	return b.String()
}
```

- [ ] **Step 2: Wire into `runFetchCycle`**

Find `runFetchCycle` (~line 84). The current body is:

```go
func runFetchCycle(ctx context.Context, feedRepo *repository.FeedRepository, articleRepo *repository.ArticleRepository, prefRepo *repository.PreferenceRepository, fetcher *rss.Fetcher, contentFetcher *rss.ContentFetcher, summarizer *ai.Summarizer) {
	if !cycleMu.TryLock() {
		log.Println("Previous fetch cycle still running, skipping")
		return
	}
	defer cycleMu.Unlock()

	fetchAllFeeds(ctx, feedRepo, articleRepo, fetcher, contentFetcher, summarizer)
	refetchShortContent(ctx, articleRepo, contentFetcher, summarizer)
	if summarizer != nil {
		backfillSummaries(ctx, articleRepo, summarizer)
		runClassifyCycle(ctx, articleRepo, prefRepo, summarizer)
	}
}
```

Add a `transcriptFetcher transcript.Fetcher` parameter, then call `backfillTranscripts` between `refetchShortContent` and `backfillSummaries`. New body:

```go
func runFetchCycle(ctx context.Context, feedRepo *repository.FeedRepository, articleRepo *repository.ArticleRepository, prefRepo *repository.PreferenceRepository, fetcher *rss.Fetcher, contentFetcher *rss.ContentFetcher, summarizer *ai.Summarizer, transcriptFetcher transcript.Fetcher) {
	if !cycleMu.TryLock() {
		log.Println("Previous fetch cycle still running, skipping")
		return
	}
	defer cycleMu.Unlock()

	fetchAllFeeds(ctx, feedRepo, articleRepo, fetcher, contentFetcher, summarizer)
	refetchShortContent(ctx, articleRepo, contentFetcher, summarizer)
	if transcriptFetcher != nil {
		backfillTranscripts(ctx, articleRepo, transcriptFetcher)
	}
	if summarizer != nil {
		backfillSummaries(ctx, articleRepo, summarizer)
		runClassifyCycle(ctx, articleRepo, prefRepo, summarizer)
	}
}
```

- [ ] **Step 3: Build the composite in `main`**

Find the `main` function (~line 34). After `contentFetcher := rss.NewContentFetcher()` and before the `summarizer` block, add:

```go
	transcriptFetcher := &transcript.MultiFetcher{
		Strategies: []transcript.Fetcher{
			&transcript.YouTubeCC{},
			&transcript.BilibiliCC{},
			&transcript.HTMLPageScraper{Docs: contentFetcher},
		},
	}
```

Then change the two `runFetchCycle(...)` call sites in `main` (~line 77 and ~line 80) to pass `transcriptFetcher` as the new last argument.

- [ ] **Step 4: Build to verify**

```bash
cd /Users/bytedance/mygit/rss-pal && docker run --rm -v "$PWD/backend:/app" -w /app golang:1.24-alpine sh -c "go build ./..."
```

Expected: clean build.

- [ ] **Step 5: Commit**

```bash
git add backend/cmd/worker/main.go
git commit -m "feat(worker): backfillTranscripts step + wire MultiFetcher into runFetchCycle"
```

---

## Task 11: Build, deploy, and one-time backfill

**Files:** none (verification + DB ops only)

- [ ] **Step 1: Apply migration to running DB**

```bash
docker-compose exec -T postgres psql -U postgres -d rsspal -f /docker-entrypoint-initdb.d/013_transcript_fetch.sql
```

Expected: `ALTER TABLE`, `CREATE INDEX`.

- [ ] **Step 2: Rebuild and deploy worker + api**

```bash
docker-compose up -d --build api worker 2>&1 | tail -10
```

Expected: both `Built` and `Started` clean. No compile errors.

- [ ] **Step 3: Watch worker logs for one cycle**

```bash
docker-compose logs --tail 50 -f worker 2>&1 | grep -i 'transcript' &
```

After ~1 minute, expect lines like:

```
Fetching transcripts for 5 media articles
Transcript fetched for article 1791 (source=bbc.co.uk 网页字幕, 8421 chars)
```

…or, for articles without a transcript:

```
(no log line — silent skip)
```

Stop the tail with Ctrl-C / kill the background `&` job.

- [ ] **Step 4: Spot-check article 1791**

```bash
docker-compose exec -T postgres psql -U postgres -d rsspal -c "SELECT id, transcript_fetched_at IS NOT NULL AS attempted, summary_brief IS NULL OR summary_brief = '' AS pending_summary, LENGTH(content) FROM articles WHERE id = 1791;"
```

Expected: `attempted=t`, `pending_summary` may be `t` (will be `f` after `backfillSummaries` runs the next cycle), and content length much larger than before.

Open `http://localhost/articles/1791` in the browser. Confirm the BBC Learning English transcript appears below the original description, separated by `## 字幕`.

- [ ] **Step 5: Spot-check a Bilibili article**

```bash
docker-compose exec -T postgres psql -U postgres -d rsspal -c "SELECT id, title, transcript_fetched_at IS NOT NULL AS attempted, content LIKE '%## 字幕%' AS has_transcript FROM articles WHERE media_type = 'video/bilibili' ORDER BY fetched_at DESC LIMIT 5;"
```

For most Bilibili gameplay UPs, expect `attempted=t` but `has_transcript=f` (no CC available — by design). For any article that does have CC, expect both `t`.

- [ ] **Step 6: Spot-check a YouTube article**

Same shape:

```bash
docker-compose exec -T postgres psql -U postgres -d rsspal -c "SELECT id, title, transcript_fetched_at IS NOT NULL AS attempted, content LIKE '%## 字幕%' AS has_transcript FROM articles WHERE media_type = 'video/youtube' ORDER BY fetched_at DESC LIMIT 5;"
```

For most public YouTube videos, expect both `t`.

- [ ] **Step 7: Wait one more cycle, confirm summaries refreshed**

After another ~1 minute (so `backfillSummaries` runs against articles whose summary was cleared), check:

```bash
docker-compose exec -T postgres psql -U postgres -d rsspal -c "SELECT id, LENGTH(summary_brief) FROM articles WHERE id = 1791;"
```

Expected: `LENGTH > 0` and the summary content (visible in the article page) reads like a real summary of the BBC episode rather than the title repeated.

If any of the spot-checks fail unexpectedly (e.g. clean YouTube videos returning no transcript, when they have CC), open worker logs for that cycle and triage. Common causes: anti-bot interstitial on the YouTube watch page (we accept silent failure on this), or a strategy bug.

If everything passes, proceed.

---

## Task 12: Push branch + open PR

- [ ] **Step 1: Confirm branch state**

```bash
git log --oneline master..HEAD
git diff master..HEAD --stat | tail -10
```

Expected: 11 commits (one per implementation task plus migration), reasonable file counts (~14 files).

- [ ] **Step 2: Push**

```bash
git push -u origin feature/transcript-summary
```

- [ ] **Step 3: Open PR**

```bash
gh pr create --base master --head feature/transcript-summary --title "feat: transcript fetching for video & audio articles" --body "$(cat <<'EOF'
## Summary
- New `internal/transcript/` package with a `Fetcher` interface and four strategies tried in order: `YouTubeCC` (parses watch-page `ytInitialPlayerResponse` and the chosen track's `fmt=json3`), `BilibiliCC` (`x/web-interface/view` → `x/player/v2` → subtitle JSON; no WBI signing required), `HTMLPageScraper` (inline transcript section, linked `.vtt`/`.srt`/`.txt`, two-hop "Find a transcript at" pattern), and a `MultiFetcher` composite. Pure-HTTP, no Python or yt-dlp.
- New worker step `backfillTranscripts` runs each cycle between `refetchShortContent` and `backfillSummaries`. Picks up media articles where `transcript_fetched_at IS NULL`, fetches via the composite, appends the transcript into `content` (markdown separator + `## 字幕` heading + source label), and clears the existing summary so the existing summary backfill loop re-runs against the now-richer content.
- `articles.transcript_fetched_at TIMESTAMPTZ` column tracks attempts; once set (success or failure) the article is not retried. Partial index keeps the worker query cheap.
- `GetArticlesWithShortContent` now skips articles with `media_type LIKE 'video/%'` or `'audio/%'` so the existing short-content refetch loop doesn't clobber transcript-augmented content.

Spec: `docs/superpowers/specs/2026-05-08-transcript-fetching-design.md`
Plan: `docs/superpowers/plans/2026-05-08-transcript-fetching.md`

## Test plan
- [x] `go test ./internal/transcript/ -v` — all strategies pass with captured fixtures (YouTube watch-page + JSON3 track, Bilibili view + player/v2 + subtitle JSON, BBC two-hop HTML, TED-style inline HTML, generic blog with no transcript).
- [x] `go build ./...` clean.
- [x] Migration applied to running DB.
- [x] Article 1791 (BBC podcast): transcript appended, summary regenerated to a real BBC episode summary.
- [x] At least one YouTube channel article: transcript appended, summary based on actual content.
- [x] At least one Bilibili UP article: `transcript_fetched_at` set, no transcript appended (most gameplay UPs have no CC — by design).
- [ ] Spot-check the article body in the browser shows the `## 字幕` section and source label inline.

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

Return the PR URL.

---

## Spec Coverage Self-Review

| Spec section | Implemented in |
|---|---|
| New `transcript_fetched_at` column | Task 1 |
| Transcript appended into `content` with `## 字幕` separator | Task 10 (`buildContentWithTranscript`) |
| Source label included in body | Task 10 (blockquote line) |
| Clear summary on success → existing backfill picks up | Task 9 (`UpdateContentAndResetSummary`) + Task 10 |
| `Fetcher` interface + `MultiFetcher` | Task 2 + Task 8 |
| YouTubeCC strategy | Task 3 |
| BilibiliCC strategy | Task 4 |
| HTMLPageScraper (inline + linked file + two-hop) | Task 7 |
| Subtitle file parsers (.vtt/.srt/.txt) | Task 5 |
| `FetchHTMLDocument` exposed on ContentFetcher | Task 6 |
| Worker step `backfillTranscripts` between refetch and summary | Task 10 |
| Skip media articles in `GetArticlesWithShortContent` | Task 9 |
| Manual smoke test | Task 11 |

No gaps. No placeholders other than the explicitly-marked `var _ = io.Discard`/`var _ = url.Parse` ones, which are call-outs to the implementer (acceptable per code).

Type consistency: `Result{Text, Source}` and `Fetcher` interface signatures are stable across all tasks. The repo method names used in Task 10 (`GetMediaArticlesWithoutTranscript`, `UpdateContentAndResetSummary`, `MarkTranscriptFetchAttempted`) match exactly what Task 9 defines.
