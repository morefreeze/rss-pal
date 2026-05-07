# Video Embed Support (YouTube + Bilibili) — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Render YouTube and Bilibili videos inline inside articles (top-of-article card for video-first feeds; in-place embed inside blog posts), reusing the existing `media_url` / `media_type` columns introduced for podcast audio.

**Architecture:** Detection runs at fetch time. A new `internal/rss/video.go` package exposes pure-function extractors (`ExtractVideo`, `RewriteVideoIframes`, `RewriteVideoLinks`, `StripDuplicateVideo`). Existing call sites that already invoke `rss.ExtractMedia` pick up a new sibling `rss.ExtractVideoMedia` that produces the same `*MediaInfo` shape so video can be plugged into the audio-shaped pipeline. The HTML→Markdown step gains an iframe pre-pass and a URL post-pass that emit `[[video:platform:id]]` placeholders; the frontend's `MarkdownArticle` intercepts those placeholders via a paragraph-component override and renders a single `<iframe>` via a new `VideoEmbed` component.

**Tech Stack:** Go 1.x + `gofeed` + `goquery` (backend); React 18 + `react-markdown` (frontend). No new dependencies.

**Spec:** `docs/superpowers/specs/2026-05-07-video-embed-support-design.md`

---

## File Structure

| File | New / Modified | Responsibility |
|------|----------------|----------------|
| `backend/internal/rss/video.go` | New | URL detection, placeholder emission, iframe rewrite, URL rewrite, dedup |
| `backend/internal/rss/video_test.go` | New | Unit tests for all of the above |
| `backend/internal/rss/content.go` | Modified | Call iframe rewriter pre-conversion; call URL rewriter post-conversion |
| `backend/cmd/worker/main.go` | Modified | Prefer video over audio media; apply dedup |
| `backend/internal/api/feed.go` | Modified | Same prefer-video at refresh-feed entry |
| `backend/internal/api/content.go` | Modified | Same prefer-video at article re-fetch entry |
| `frontend/src/components/VideoEmbed.tsx` | New | Presentational iframe wrapper |
| `frontend/src/components/parseVideoPlaceholder.ts` | New | Pure-function placeholder parser shared by both call sites |
| `frontend/src/components/ArticlePlayerCard.tsx` | Modified | Branch to `<VideoEmbed>` when `media_type` starts with `video/` |
| `frontend/src/components/MarkdownArticle.tsx` | Modified | Paragraph override matches placeholder, renders `<VideoEmbed>` |

**Note on testing:** the frontend has no test framework configured (no vitest/jest in `package.json`). Backend tasks use full TDD (Go's `testing` package). Frontend correctness is verified by `tsc` (already runs as part of `npm run build`) plus a manual smoke test in Task 14.

---

## Task 1: `ExtractVideo` — YouTube URL detection

**Files:**
- Create: `backend/internal/rss/video.go`
- Test: `backend/internal/rss/video_test.go`

- [ ] **Step 1: Write the failing test**

Create `backend/internal/rss/video_test.go`:

```go
package rss

import "testing"

func TestExtractVideo_YouTube(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		wantID  string
		wantStart int
	}{
		{"watch", "https://www.youtube.com/watch?v=dQw4w9WgXcQ", "dQw4w9WgXcQ", 0},
		{"watch_with_t", "https://www.youtube.com/watch?v=dQw4w9WgXcQ&t=42s", "dQw4w9WgXcQ", 42},
		{"watch_with_t_no_s", "https://www.youtube.com/watch?v=dQw4w9WgXcQ&t=90", "dQw4w9WgXcQ", 90},
		{"short", "https://youtu.be/dQw4w9WgXcQ", "dQw4w9WgXcQ", 0},
		{"short_with_hash_t", "https://youtu.be/dQw4w9WgXcQ?t=15", "dQw4w9WgXcQ", 15},
		{"embed", "https://www.youtube.com/embed/dQw4w9WgXcQ", "dQw4w9WgXcQ", 0},
		{"shorts", "https://www.youtube.com/shorts/dQw4w9WgXcQ", "dQw4w9WgXcQ", 0},
		{"watch_with_list_ignored", "https://www.youtube.com/watch?v=dQw4w9WgXcQ&list=PLabc", "dQw4w9WgXcQ", 0},
		{"nocookie_embed", "https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ", "dQw4w9WgXcQ", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ExtractVideo(tc.in)
			if !ok {
				t.Fatalf("ExtractVideo(%q) returned ok=false", tc.in)
			}
			if got.Platform != "youtube" {
				t.Errorf("Platform = %q, want youtube", got.Platform)
			}
			if got.ID != tc.wantID {
				t.Errorf("ID = %q, want %q", got.ID, tc.wantID)
			}
			if got.Start != tc.wantStart {
				t.Errorf("Start = %d, want %d", got.Start, tc.wantStart)
			}
		})
	}
}

func TestExtractVideo_NotAVideo(t *testing.T) {
	cases := []string{
		"",
		"https://example.com/post/123",
		"https://vimeo.com/123456",
		"https://www.youtube.com/",
		"https://www.youtube.com/watch",                     // no v=
		"https://www.youtube.com/watch?v=tooShort",          // wrong length
		"https://www.youtube.com/feed/subscriptions",
		"https://b23.tv/abc123",                             // out of scope
		"https://www.bilibili.com/video/av12345",            // legacy AV out of scope
	}
	for _, in := range cases {
		t.Run(in, func(t *testing.T) {
			if got, ok := ExtractVideo(in); ok {
				t.Errorf("ExtractVideo(%q) = %+v, want ok=false", in, got)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/rss/ -run TestExtractVideo -v`
Expected: FAIL with `undefined: ExtractVideo`.

- [ ] **Step 3: Write minimal implementation**

Create `backend/internal/rss/video.go`:

```go
package rss

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// VideoEmbed describes a recognized embeddable video reference.
type VideoEmbed struct {
	Platform string // "youtube" or "bilibili"
	ID       string // video ID (e.g. "dQw4w9WgXcQ" or "BV1xx411c7mD")
	Start    int    // seconds, 0 if unset
	Page     int    // bilibili-only, 0 means default (page 1)
	EmbedURL string // canonical iframe src
}

// youTubeIDPattern matches the canonical YouTube video ID character set/length.
var youTubeIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

// ExtractVideo parses a single URL and returns a VideoEmbed if it matches
// a supported YouTube or Bilibili form. Pure: no network I/O.
func ExtractVideo(rawURL string) (*VideoEmbed, bool) {
	if rawURL == "" {
		return nil, false
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return nil, false
	}
	host := strings.ToLower(strings.TrimPrefix(u.Host, "www."))

	if v, ok := extractYouTube(u, host); ok {
		return v, true
	}
	return nil, false
}

func extractYouTube(u *url.URL, host string) (*VideoEmbed, bool) {
	var id string
	switch host {
	case "youtu.be":
		id = strings.Trim(u.Path, "/")
	case "youtube.com", "m.youtube.com", "music.youtube.com",
		"youtube-nocookie.com", "www.youtube-nocookie.com":
		switch {
		case u.Path == "/watch":
			id = u.Query().Get("v")
		case strings.HasPrefix(u.Path, "/embed/"):
			id = strings.TrimPrefix(u.Path, "/embed/")
		case strings.HasPrefix(u.Path, "/shorts/"):
			id = strings.TrimPrefix(u.Path, "/shorts/")
		case strings.HasPrefix(u.Path, "/v/"):
			id = strings.TrimPrefix(u.Path, "/v/")
		}
		// Strip any trailing path segments (e.g. /embed/ID/extra)
		if i := strings.Index(id, "/"); i >= 0 {
			id = id[:i]
		}
	default:
		return nil, false
	}
	if !youTubeIDPattern.MatchString(id) {
		return nil, false
	}
	v := &VideoEmbed{Platform: "youtube", ID: id}
	v.Start = parseStartParam(u)
	v.EmbedURL = v.buildEmbedURL()
	return v, true
}

// parseStartParam reads ?t= or ?start= or #t= and returns seconds.
// Accepts plain integers ("90"), trailing 's' ("90s"), and "1m30s" form.
func parseStartParam(u *url.URL) int {
	q := u.Query()
	for _, key := range []string{"t", "start"} {
		if v := q.Get(key); v != "" {
			if n := parseDurationSpec(v); n > 0 {
				return n
			}
		}
	}
	if u.Fragment != "" && strings.HasPrefix(u.Fragment, "t=") {
		if n := parseDurationSpec(strings.TrimPrefix(u.Fragment, "t=")); n > 0 {
			return n
		}
	}
	return 0
}

// parseDurationSpec accepts "90", "90s", "1m30s", "1h2m3s".
func parseDurationSpec(s string) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	if n, err := strconv.Atoi(s); err == nil && n >= 0 {
		return n
	}
	// 1h2m3s form
	var total, cur int
	for _, c := range s {
		switch {
		case c >= '0' && c <= '9':
			cur = cur*10 + int(c-'0')
		case c == 'h':
			total += cur * 3600
			cur = 0
		case c == 'm':
			total += cur * 60
			cur = 0
		case c == 's':
			total += cur
			cur = 0
		default:
			return 0
		}
	}
	return total + cur
}

// buildEmbedURL constructs the canonical iframe src for the embed.
func (v *VideoEmbed) buildEmbedURL() string {
	switch v.Platform {
	case "youtube":
		s := "https://www.youtube-nocookie.com/embed/" + v.ID + "?rel=0"
		if v.Start > 0 {
			s += "&start=" + strconv.Itoa(v.Start)
		}
		return s
	case "bilibili":
		page := v.Page
		if page <= 0 {
			page = 1
		}
		s := "https://player.bilibili.com/player.html?bvid=" + v.ID +
			"&high_quality=1&autoplay=0&page=" + strconv.Itoa(page)
		if v.Start > 0 {
			s += "&t=" + strconv.Itoa(v.Start)
		}
		return s
	}
	return ""
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/rss/ -run TestExtractVideo -v`
Expected: PASS for all subtests.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/rss/video.go backend/internal/rss/video_test.go
git commit -m "feat(rss): ExtractVideo parses YouTube URLs into VideoEmbed"
```

---

## Task 2: `ExtractVideo` — Bilibili URL detection

**Files:**
- Modify: `backend/internal/rss/video.go`
- Modify: `backend/internal/rss/video_test.go`

- [ ] **Step 1: Add the failing test**

Append to `backend/internal/rss/video_test.go`:

```go
func TestExtractVideo_Bilibili(t *testing.T) {
	cases := []struct {
		name      string
		in        string
		wantID    string
		wantPage  int
		wantStart int
	}{
		{"plain_bv", "https://www.bilibili.com/video/BV1xx411c7mD", "BV1xx411c7mD", 0, 0},
		{"trailing_slash", "https://www.bilibili.com/video/BV1xx411c7mD/", "BV1xx411c7mD", 0, 0},
		{"with_page", "https://www.bilibili.com/video/BV1xx411c7mD/?p=2", "BV1xx411c7mD", 2, 0},
		{"with_t", "https://www.bilibili.com/video/BV1xx411c7mD?t=15", "BV1xx411c7mD", 0, 15},
		{"with_page_and_t", "https://www.bilibili.com/video/BV1xx411c7mD?p=3&t=42", "BV1xx411c7mD", 3, 42},
		{"m_subdomain", "https://m.bilibili.com/video/BV1xx411c7mD", "BV1xx411c7mD", 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ExtractVideo(tc.in)
			if !ok {
				t.Fatalf("ExtractVideo(%q) returned ok=false", tc.in)
			}
			if got.Platform != "bilibili" {
				t.Errorf("Platform = %q, want bilibili", got.Platform)
			}
			if got.ID != tc.wantID {
				t.Errorf("ID = %q, want %q", got.ID, tc.wantID)
			}
			if got.Page != tc.wantPage {
				t.Errorf("Page = %d, want %d", got.Page, tc.wantPage)
			}
			if got.Start != tc.wantStart {
				t.Errorf("Start = %d, want %d", got.Start, tc.wantStart)
			}
		})
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `cd backend && go test ./internal/rss/ -run TestExtractVideo_Bilibili -v`
Expected: FAIL — Bilibili branch not yet implemented; current `ExtractVideo` returns `ok=false`.

- [ ] **Step 3: Add Bilibili extractor**

In `backend/internal/rss/video.go`, add at package scope (above `ExtractVideo`):

```go
// bilibiliBVPattern matches BV-form Bilibili IDs.
var bilibiliBVPattern = regexp.MustCompile(`^BV[0-9A-Za-z]{10}$`)
```

Replace the body of `ExtractVideo` to also try Bilibili:

```go
func ExtractVideo(rawURL string) (*VideoEmbed, bool) {
	if rawURL == "" {
		return nil, false
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return nil, false
	}
	host := strings.ToLower(strings.TrimPrefix(u.Host, "www."))

	if v, ok := extractYouTube(u, host); ok {
		return v, true
	}
	if v, ok := extractBilibili(u, host); ok {
		return v, true
	}
	return nil, false
}

func extractBilibili(u *url.URL, host string) (*VideoEmbed, bool) {
	if host != "bilibili.com" && host != "m.bilibili.com" {
		return nil, false
	}
	if !strings.HasPrefix(u.Path, "/video/") {
		return nil, false
	}
	id := strings.TrimPrefix(u.Path, "/video/")
	id = strings.TrimRight(id, "/")
	if i := strings.Index(id, "/"); i >= 0 {
		id = id[:i]
	}
	if !bilibiliBVPattern.MatchString(id) {
		return nil, false
	}
	v := &VideoEmbed{Platform: "bilibili", ID: id}
	v.Start = parseStartParam(u)
	if p := u.Query().Get("p"); p != "" {
		if n, err := strconv.Atoi(p); err == nil && n > 0 {
			v.Page = n
		}
	}
	v.EmbedURL = v.buildEmbedURL()
	return v, true
}
```

- [ ] **Step 4: Run to verify pass**

Run: `cd backend && go test ./internal/rss/ -run TestExtractVideo -v`
Expected: PASS for all subtests including AV-form negative case (already in `TestExtractVideo_NotAVideo`).

- [ ] **Step 5: Commit**

```bash
git add backend/internal/rss/video.go backend/internal/rss/video_test.go
git commit -m "feat(rss): ExtractVideo recognizes Bilibili BV-form URLs"
```

---

## Task 3: `VideoEmbed.Placeholder()` and embed-URL parser

**Files:**
- Modify: `backend/internal/rss/video.go`
- Modify: `backend/internal/rss/video_test.go`

This task adds the placeholder serializer (used by case-B rewriters in Tasks 6–8) and a small parser used by case-A dedup (Task 8).

- [ ] **Step 1: Write the failing test**

Append to `video_test.go`:

```go
func TestVideoEmbed_Placeholder(t *testing.T) {
	cases := []struct {
		name string
		v    VideoEmbed
		want string
	}{
		{"yt_basic", VideoEmbed{Platform: "youtube", ID: "dQw4w9WgXcQ"}, "[[video:youtube:dQw4w9WgXcQ]]"},
		{"yt_start", VideoEmbed{Platform: "youtube", ID: "dQw4w9WgXcQ", Start: 42}, "[[video:youtube:dQw4w9WgXcQ?start=42]]"},
		{"bili_basic", VideoEmbed{Platform: "bilibili", ID: "BV1xx411c7mD"}, "[[video:bilibili:BV1xx411c7mD]]"},
		{"bili_page", VideoEmbed{Platform: "bilibili", ID: "BV1xx411c7mD", Page: 2}, "[[video:bilibili:BV1xx411c7mD?page=2]]"},
		{"bili_page_start", VideoEmbed{Platform: "bilibili", ID: "BV1xx411c7mD", Page: 2, Start: 15}, "[[video:bilibili:BV1xx411c7mD?page=2&start=15]]"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.v.Placeholder(); got != tc.want {
				t.Errorf("Placeholder() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestParseEmbedURL(t *testing.T) {
	cases := []struct {
		in   string
		want VideoEmbed // EmbedURL ignored
	}{
		{
			"https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ?rel=0",
			VideoEmbed{Platform: "youtube", ID: "dQw4w9WgXcQ"},
		},
		{
			"https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ?rel=0&start=42",
			VideoEmbed{Platform: "youtube", ID: "dQw4w9WgXcQ", Start: 42},
		},
		{
			"https://player.bilibili.com/player.html?bvid=BV1xx411c7mD&high_quality=1&autoplay=0&page=1",
			VideoEmbed{Platform: "bilibili", ID: "BV1xx411c7mD", Page: 1},
		},
		{
			"https://player.bilibili.com/player.html?bvid=BV1xx411c7mD&high_quality=1&autoplay=0&page=2&t=15",
			VideoEmbed{Platform: "bilibili", ID: "BV1xx411c7mD", Page: 2, Start: 15},
		},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got, ok := ParseEmbedURL(tc.in)
			if !ok {
				t.Fatalf("ParseEmbedURL(%q) ok=false", tc.in)
			}
			if got.Platform != tc.want.Platform || got.ID != tc.want.ID ||
				got.Start != tc.want.Start || got.Page != tc.want.Page {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `cd backend && go test ./internal/rss/ -run "TestVideoEmbed_Placeholder|TestParseEmbedURL" -v`
Expected: FAIL — `Placeholder` and `ParseEmbedURL` not yet defined.

- [ ] **Step 3: Implement**

Append to `backend/internal/rss/video.go`:

```go
// Placeholder returns the markdown-safe inline placeholder used to represent
// this embed inside article content. Form: [[video:<platform>:<id>(?query)?]]
// where the optional query carries `page` (bilibili) and `start` keys.
func (v *VideoEmbed) Placeholder() string {
	if v == nil || v.Platform == "" || v.ID == "" {
		return ""
	}
	var params []string
	if v.Platform == "bilibili" && v.Page > 0 {
		params = append(params, "page="+strconv.Itoa(v.Page))
	}
	if v.Start > 0 {
		params = append(params, "start="+strconv.Itoa(v.Start))
	}
	if len(params) == 0 {
		return "[[video:" + v.Platform + ":" + v.ID + "]]"
	}
	return "[[video:" + v.Platform + ":" + v.ID + "?" + strings.Join(params, "&") + "]]"
}

// ParseEmbedURL is the inverse of buildEmbedURL: given a stored embed URL
// (as written into media_url), return the VideoEmbed components.
func ParseEmbedURL(rawURL string) (*VideoEmbed, bool) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return nil, false
	}
	host := strings.ToLower(strings.TrimPrefix(u.Host, "www."))
	switch host {
	case "youtube-nocookie.com", "youtube.com":
		if !strings.HasPrefix(u.Path, "/embed/") {
			return nil, false
		}
		id := strings.TrimPrefix(u.Path, "/embed/")
		if i := strings.Index(id, "/"); i >= 0 {
			id = id[:i]
		}
		if !youTubeIDPattern.MatchString(id) {
			return nil, false
		}
		v := &VideoEmbed{Platform: "youtube", ID: id, EmbedURL: rawURL}
		if s := u.Query().Get("start"); s != "" {
			if n, err := strconv.Atoi(s); err == nil && n > 0 {
				v.Start = n
			}
		}
		return v, true
	case "player.bilibili.com":
		id := u.Query().Get("bvid")
		if !bilibiliBVPattern.MatchString(id) {
			return nil, false
		}
		v := &VideoEmbed{Platform: "bilibili", ID: id, EmbedURL: rawURL}
		if p := u.Query().Get("page"); p != "" {
			if n, err := strconv.Atoi(p); err == nil && n > 0 {
				v.Page = n
			}
		}
		if s := u.Query().Get("t"); s != "" {
			if n, err := strconv.Atoi(s); err == nil && n > 0 {
				v.Start = n
			}
		}
		return v, true
	}
	return nil, false
}
```

- [ ] **Step 4: Run to verify pass**

Run: `cd backend && go test ./internal/rss/ -run "TestVideoEmbed_Placeholder|TestParseEmbedURL" -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/rss/video.go backend/internal/rss/video_test.go
git commit -m "feat(rss): VideoEmbed.Placeholder serializer + ParseEmbedURL inverse"
```

---

## Task 4: `ExtractVideoMedia` adapter — plug video into MediaInfo-shaped pipeline

**Files:**
- Modify: `backend/internal/rss/video.go`
- Modify: `backend/internal/rss/video_test.go`

The worker, `api/feed.go`, and `api/content.go` already work in terms of `*MediaInfo`. This adapter lets video plug in without changing those signatures.

- [ ] **Step 1: Write the failing test**

Append to `video_test.go`:

```go
func TestExtractVideoMedia(t *testing.T) {
	cases := []struct {
		in       string
		wantNil  bool
		wantType string
		wantHost string // substring assertion on URL
	}{
		{"https://www.youtube.com/watch?v=dQw4w9WgXcQ", false, "video/youtube", "youtube-nocookie.com"},
		{"https://www.bilibili.com/video/BV1xx411c7mD?p=2", false, "video/bilibili", "player.bilibili.com"},
		{"https://example.com/post/abc", true, "", ""},
		{"", true, "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got := ExtractVideoMedia(tc.in)
			if tc.wantNil {
				if got != nil {
					t.Fatalf("expected nil, got %+v", got)
				}
				return
			}
			if got == nil {
				t.Fatalf("expected MediaInfo, got nil")
			}
			if got.Type != tc.wantType {
				t.Errorf("Type = %q, want %q", got.Type, tc.wantType)
			}
			if !strings.Contains(got.URL, tc.wantHost) {
				t.Errorf("URL = %q does not contain %q", got.URL, tc.wantHost)
			}
		})
	}
}
```

(The test file already imports `testing`. Add `"strings"` to its imports.)

- [ ] **Step 2: Run to verify failure**

Run: `cd backend && go test ./internal/rss/ -run TestExtractVideoMedia -v`
Expected: FAIL — `ExtractVideoMedia` undefined.

- [ ] **Step 3: Implement**

Append to `video.go`:

```go
// ExtractVideoMedia is a convenience wrapper that returns a *MediaInfo
// when rawURL is a recognized video. Returns nil otherwise. Callers that
// already work in terms of MediaInfo (worker, feed/content APIs) can fall
// back to ExtractMedia(item) when this returns nil. Video wins over audio
// because video-bearing feeds occasionally also carry an audio enclosure
// (rare; we choose visibility over completeness).
func ExtractVideoMedia(rawURL string) *MediaInfo {
	v, ok := ExtractVideo(rawURL)
	if !ok {
		return nil
	}
	return &MediaInfo{
		URL:  v.EmbedURL,
		Type: "video/" + v.Platform,
	}
}
```

- [ ] **Step 4: Run to verify pass**

Run: `cd backend && go test ./internal/rss/ -v`
Expected: PASS for all rss tests (existing audio tests must still pass).

- [ ] **Step 5: Commit**

```bash
git add backend/internal/rss/video.go backend/internal/rss/video_test.go
git commit -m "feat(rss): ExtractVideoMedia adapts ExtractVideo to MediaInfo"
```

---

## Task 5: Wire `ExtractVideoMedia` into worker and API entry points

**Files:**
- Modify: `backend/cmd/worker/main.go:229` (and surrounding `if mediaInfo != nil` blocks)
- Modify: `backend/internal/api/feed.go:283`
- Modify: `backend/internal/api/content.go:99` (and the `FindMediaInHTML` fallback at :110)

Pattern: prefer video; fall back to existing audio path.

- [ ] **Step 1: Update `cmd/worker/main.go`**

Find the call site at line ~229 (`mediaInfo := rss.ExtractMedia(item)`). Replace that single line with:

```go
mediaInfo := rss.ExtractVideoMedia(item.Link)
if mediaInfo == nil {
	mediaInfo = rss.ExtractMedia(item)
}
```

No other changes needed in the worker — the existing `if mediaInfo != nil` blocks already populate `article.MediaURL` / `article.MediaType` from it.

- [ ] **Step 2: Update `internal/api/feed.go`**

Find the call at line ~283 (`mediaInfo := rss.ExtractMedia(item)`). Replace with the same two-line pattern:

```go
mediaInfo := rss.ExtractVideoMedia(item.Link)
if mediaInfo == nil {
	mediaInfo = rss.ExtractMedia(item)
}
```

- [ ] **Step 3: Update `internal/api/content.go`**

Read the current shape near line 95–112:

```go
mi = rss.ExtractMedia(item)
// ...
if mi == nil {
	mi = h.contentFetcher.FindMediaInHTML(ctx, article.URL)
}
```

Insert video detection before the `ExtractMedia` call. The exact change:

```go
// Prefer recognized video URL on the article first.
if mi == nil {
	mi = rss.ExtractVideoMedia(article.URL)
}
if mi == nil {
	mi = rss.ExtractMedia(item)
}
// ... existing FindMediaInHTML fallback continues unchanged ...
```

(If the surrounding code uses a different variable name or already has an `if mi == nil` guard, adapt the placement so video is tried *first* among the three sources — `article.URL` ➜ `item` enclosures ➜ HTML fallback.)

- [ ] **Step 4: Build to verify nothing else broke**

Run: `cd backend && go build ./...`
Expected: clean build.

Run: `cd backend && go test ./...`
Expected: PASS (no behavior change to audio paths).

- [ ] **Step 5: Commit**

```bash
git add backend/cmd/worker/main.go backend/internal/api/feed.go backend/internal/api/content.go
git commit -m "feat(media): prefer video URL detection at all media-extraction call sites"
```

---

## Task 6: `RewriteVideoIframes` — case-B iframe preservation

**Files:**
- Modify: `backend/internal/rss/video.go`
- Modify: `backend/internal/rss/video_test.go`

Walks a goquery selection and replaces matching `<iframe>` tags with a paragraph containing the placeholder. Mutates in place (matches `StripAvatars(doc)` precedent).

- [ ] **Step 1: Write the failing test**

Append to `video_test.go`. Make sure the test file imports `"strings"` and add `"github.com/PuerkitoBio/goquery"`:

```go
func TestRewriteVideoIframes(t *testing.T) {
	cases := []struct {
		name     string
		html     string
		wantHas  string // substring expected in result
		wantMiss string // substring that must not survive
	}{
		{
			"youtube_iframe",
			`<div><p>before</p><iframe src="https://www.youtube.com/embed/dQw4w9WgXcQ"></iframe><p>after</p></div>`,
			"[[video:youtube:dQw4w9WgXcQ]]",
			"<iframe",
		},
		{
			"bilibili_iframe",
			`<iframe src="https://player.bilibili.com/player.html?bvid=BV1xx411c7mD&page=2"></iframe>`,
			"[[video:bilibili:BV1xx411c7mD?page=2]]",
			"<iframe",
		},
		{
			"unrelated_iframe_left_alone",
			`<iframe src="https://example.com/widget"></iframe>`,
			"<iframe",  // unchanged
			"[[video",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := goquery.NewDocumentFromReader(strings.NewReader("<html><body>" + tc.html + "</body></html>"))
			if err != nil {
				t.Fatal(err)
			}
			RewriteVideoIframes(doc.Selection)
			out, _ := doc.Find("body").Html()
			if !strings.Contains(out, tc.wantHas) {
				t.Errorf("expected %q in %q", tc.wantHas, out)
			}
			if tc.wantMiss != "" && strings.Contains(out, tc.wantMiss) {
				t.Errorf("expected %q to be absent in %q", tc.wantMiss, out)
			}
		})
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `cd backend && go test ./internal/rss/ -run TestRewriteVideoIframes -v`
Expected: FAIL — `RewriteVideoIframes` undefined.

- [ ] **Step 3: Implement**

Append to `video.go`. Add `"github.com/PuerkitoBio/goquery"` to its imports.

```go
// RewriteVideoIframes walks selection and replaces every <iframe> whose src
// matches a recognized YouTube/Bilibili URL with a <p> containing the
// inline placeholder. Iframes that don't match are left untouched (they
// will continue to be dropped by the html-to-markdown converter as before).
//
// Call before ExtractMarkdown / mdConverter.ConvertString.
func RewriteVideoIframes(selection *goquery.Selection) {
	selection.Find("iframe").Each(func(_ int, s *goquery.Selection) {
		src, _ := s.Attr("src")
		if src == "" {
			return
		}
		// iframe srcs use the embed form; ExtractVideo handles those.
		v, ok := ExtractVideo(src)
		if !ok {
			return
		}
		// Replace the iframe with a paragraph carrying the placeholder so
		// the html-to-markdown converter keeps it intact through conversion.
		s.ReplaceWithHtml("<p>" + v.Placeholder() + "</p>")
	})
}
```

- [ ] **Step 4: Run to verify pass**

Run: `cd backend && go test ./internal/rss/ -run TestRewriteVideoIframes -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/rss/video.go backend/internal/rss/video_test.go
git commit -m "feat(rss): RewriteVideoIframes preserves YT/Bilibili iframes as placeholders"
```

---

## Task 7: `RewriteVideoLinks` — case-B URL fallback scan

**Files:**
- Modify: `backend/internal/rss/video.go`
- Modify: `backend/internal/rss/video_test.go`

Post-conversion: scan markdown for bare or `[text](url)` YouTube/Bilibili URLs and rewrite each into the placeholder form.

- [ ] **Step 1: Write the failing test**

Append to `video_test.go`:

```go
func TestRewriteVideoLinks(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			"bare_youtube",
			"Check out https://www.youtube.com/watch?v=dQw4w9WgXcQ today",
			"Check out [[video:youtube:dQw4w9WgXcQ]] today",
		},
		{
			"markdown_link",
			"See [the video](https://youtu.be/dQw4w9WgXcQ) for more.",
			"See [[video:youtube:dQw4w9WgXcQ]] for more.",
		},
		{
			"bilibili_with_page",
			"https://www.bilibili.com/video/BV1xx411c7mD?p=2",
			"[[video:bilibili:BV1xx411c7mD?page=2]]",
		},
		{
			"non_video_url_untouched",
			"https://example.com/post/abc and a [link](https://example.org/x)",
			"https://example.com/post/abc and a [link](https://example.org/x)",
		},
		{
			"existing_placeholder_idempotent",
			"[[video:youtube:dQw4w9WgXcQ]]",
			"[[video:youtube:dQw4w9WgXcQ]]",
		},
		{
			"two_matches",
			"a https://youtu.be/dQw4w9WgXcQ b https://youtu.be/abcdEFGHijk c",
			"a [[video:youtube:dQw4w9WgXcQ]] b [[video:youtube:abcdEFGHijk]] c",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := RewriteVideoLinks(tc.in)
			if got != tc.want {
				t.Errorf("RewriteVideoLinks() =\n  got:  %q\n  want: %q", got, tc.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `cd backend && go test ./internal/rss/ -run TestRewriteVideoLinks -v`
Expected: FAIL — `RewriteVideoLinks` undefined.

- [ ] **Step 3: Implement**

Append to `video.go`:

```go
// videoURLRegex matches absolute http(s) URLs that could be YouTube or
// Bilibili. It is intentionally permissive at the regex level — actual
// matching/parsing is delegated to ExtractVideo so we get the same result
// as iframe and top-card detection. Excludes whitespace and common URL
// terminators (), <>, ", '), but allows query strings.
var videoURLRegex = regexp.MustCompile(
	`https?://(?:www\.|m\.|music\.)?(?:youtube\.com|youtube-nocookie\.com|youtu\.be|bilibili\.com)/[^\s)<>"']+`,
)

// markdownLinkRegex matches [text](url) form. Used to rewrite the entire
// link (including the bracket) into a placeholder when url is a video.
var markdownLinkRegex = regexp.MustCompile(`\[([^\]]*)\]\((https?://[^)\s]+)\)`)

// RewriteVideoLinks scans md and replaces YouTube/Bilibili URLs (both
// markdown-link form and bare URLs) with [[video:...]] placeholders.
// Idempotent — placeholders are not re-rewritten because the regex only
// matches http(s) URLs.
func RewriteVideoLinks(md string) string {
	// Pass 1: rewrite [text](url) where url is a video URL.
	md = markdownLinkRegex.ReplaceAllStringFunc(md, func(m string) string {
		sub := markdownLinkRegex.FindStringSubmatch(m)
		if len(sub) != 3 {
			return m
		}
		v, ok := ExtractVideo(sub[2])
		if !ok {
			return m
		}
		return v.Placeholder()
	})
	// Pass 2: rewrite bare URLs.
	md = videoURLRegex.ReplaceAllStringFunc(md, func(u string) string {
		v, ok := ExtractVideo(u)
		if !ok {
			return u
		}
		return v.Placeholder()
	})
	return md
}
```

- [ ] **Step 4: Run to verify pass**

Run: `cd backend && go test ./internal/rss/ -run TestRewriteVideoLinks -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/rss/video.go backend/internal/rss/video_test.go
git commit -m "feat(rss): RewriteVideoLinks turns YT/Bilibili URLs into placeholders"
```

---

## Task 8: `StripDuplicateVideo` — A3 dedup

**Files:**
- Modify: `backend/internal/rss/video.go`
- Modify: `backend/internal/rss/video_test.go`

When the article will render a top-card video, remove any in-body placeholder for the same video so it isn't shown twice.

- [ ] **Step 1: Write the failing test**

Append to `video_test.go`:

```go
func TestStripDuplicateVideo(t *testing.T) {
	embed := &VideoEmbed{Platform: "youtube", ID: "dQw4w9WgXcQ"}

	t.Run("removes_matching_placeholder", func(t *testing.T) {
		in := "Intro paragraph.\n\n[[video:youtube:dQw4w9WgXcQ]]\n\nMore text."
		want := "Intro paragraph.\n\nMore text."
		if got := StripDuplicateVideo(in, embed); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("removes_with_query_params", func(t *testing.T) {
		in := "[[video:youtube:dQw4w9WgXcQ?start=42]]\n\nbody"
		want := "body"
		if got := StripDuplicateVideo(in, embed); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("keeps_different_id", func(t *testing.T) {
		in := "[[video:youtube:abcdEFGHijk]]\n\nbody"
		if got := StripDuplicateVideo(in, embed); got != in {
			t.Errorf("expected unchanged, got %q", got)
		}
	})

	t.Run("keeps_different_platform", func(t *testing.T) {
		in := "[[video:bilibili:BV1xx411c7mD]]\n\nbody"
		if got := StripDuplicateVideo(in, embed); got != in {
			t.Errorf("expected unchanged, got %q", got)
		}
	})

	t.Run("nil_embed_passthrough", func(t *testing.T) {
		in := "[[video:youtube:dQw4w9WgXcQ]]"
		if got := StripDuplicateVideo(in, nil); got != in {
			t.Errorf("expected unchanged, got %q", got)
		}
	})
}
```

- [ ] **Step 2: Run to verify failure**

Run: `cd backend && go test ./internal/rss/ -run TestStripDuplicateVideo -v`
Expected: FAIL — `StripDuplicateVideo` undefined.

- [ ] **Step 3: Implement**

Append to `video.go`:

```go
// StripDuplicateVideo removes any [[video:platform:id...]] placeholder in md
// whose platform+id matches embed. Used after a top-card video is decided
// so the body doesn't render the same video a second time. Surrounding
// blank lines collapse so we don't leave a 3-newline gap.
func StripDuplicateVideo(md string, embed *VideoEmbed) string {
	if embed == nil || embed.Platform == "" || embed.ID == "" {
		return md
	}
	pat := regexp.MustCompile(
		`\n*\[\[video:` + regexp.QuoteMeta(embed.Platform) + `:` +
			regexp.QuoteMeta(embed.ID) + `(?:\?[^\]]*)?\]\]\n*`,
	)
	out := pat.ReplaceAllString(md, "\n\n")
	return strings.TrimSpace(out)
}
```

- [ ] **Step 4: Run to verify pass**

Run: `cd backend && go test ./internal/rss/ -run TestStripDuplicateVideo -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/rss/video.go backend/internal/rss/video_test.go
git commit -m "feat(rss): StripDuplicateVideo removes in-body placeholder when top-card video is set"
```

---

## Task 9: Wire iframe + URL rewriters into the content pipeline

**Files:**
- Modify: `backend/internal/rss/content.go` — `ExtractMarkdown` (~line 229)
- Modify: `backend/cmd/worker/main.go` — call `StripDuplicateVideo` after `mediaInfo` is known

- [ ] **Step 1: Modify `ExtractMarkdown` in `content.go`**

Find the function (~line 229):

```go
func ExtractMarkdown(selection *goquery.Selection) string {
	html, err := selection.Html()
	if err != nil || strings.TrimSpace(html) == "" {
		return strings.TrimSpace(selection.Text())
	}
	md, err := mdConverter.ConvertString(html)
	if err != nil {
		return strings.TrimSpace(selection.Text())
	}
	return strings.TrimSpace(md)
}
```

Replace with:

```go
func ExtractMarkdown(selection *goquery.Selection) string {
	// Pre-conversion: rewrite recognized video iframes to placeholder paragraphs
	// so the html-to-markdown converter doesn't drop them.
	RewriteVideoIframes(selection)

	html, err := selection.Html()
	if err != nil || strings.TrimSpace(html) == "" {
		return strings.TrimSpace(selection.Text())
	}
	md, err := mdConverter.ConvertString(html)
	if err != nil {
		return strings.TrimSpace(selection.Text())
	}
	// Post-conversion: catch YouTube/Bilibili URLs that were links rather than iframes.
	md = RewriteVideoLinks(md)
	return strings.TrimSpace(md)
}
```

- [ ] **Step 2: Build + run existing content tests**

Run: `cd backend && go build ./... && go test ./internal/rss/ -v`
Expected: PASS (existing podcast/audio/markdown tests must still pass).

- [ ] **Step 3: Wire dedup in worker**

In `backend/cmd/worker/main.go`, locate the goroutine body around line ~265 where `article.MediaURL`/`article.MediaType` are populated from `mediaInfo`. The current shape is:

```go
if mediaInfo != nil {
	article.MediaURL = mediaInfo.URL
	article.MediaType = mediaInfo.Type
	article.MediaDurationSeconds = mediaInfo.Duration
}
```

Replace with:

```go
if mediaInfo != nil {
	article.MediaURL = mediaInfo.URL
	article.MediaType = mediaInfo.Type
	article.MediaDurationSeconds = mediaInfo.Duration
	// If this is a video and the body also mentions the same video,
	// strip the in-body placeholder so it isn't rendered twice.
	if strings.HasPrefix(mediaInfo.Type, "video/") {
		if v, ok := rss.ParseEmbedURL(mediaInfo.URL); ok {
			article.Content = rss.StripDuplicateVideo(article.Content, v)
		}
	}
}
```

(Confirm `"strings"` is already imported in `cmd/worker/main.go`. Per current code, it isn't — add it to the import block.)

- [ ] **Step 4: Build to verify**

Run: `cd backend && go build ./...`
Expected: clean build.

Run: `cd backend && go test ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/rss/content.go backend/cmd/worker/main.go
git commit -m "feat(rss): wire video iframe/link rewrites + worker dedup into pipeline"
```

---

## Task 10: Frontend — placeholder parser (pure function)

**Files:**
- Create: `frontend/src/components/parseVideoPlaceholder.ts`

A pure function that returns `{ platform, id, start?, page? }` from either a `[[video:...]]` placeholder or a stored embed URL. Used by both `MarkdownArticle` (placeholder) and `ArticlePlayerCard` (URL).

- [ ] **Step 1: Create the file**

Create `frontend/src/components/parseVideoPlaceholder.ts`:

```ts
export type VideoPlatform = 'youtube' | 'bilibili'

export interface VideoEmbedData {
  platform: VideoPlatform
  id: string
  start?: number
  page?: number
}

const PLACEHOLDER_RE = /^\[\[video:(youtube|bilibili):([\w-]+)(?:\?([\w=&]+))?]]$/
const YT_ID_RE = /^[A-Za-z0-9_-]{11}$/
const BV_ID_RE = /^BV[0-9A-Za-z]{10}$/

export function parsePlaceholder(text: string): VideoEmbedData | null {
  const m = text.trim().match(PLACEHOLDER_RE)
  if (!m) return null
  const platform = m[1] as VideoPlatform
  const id = m[2]
  if (platform === 'youtube' && !YT_ID_RE.test(id)) return null
  if (platform === 'bilibili' && !BV_ID_RE.test(id)) return null
  const out: VideoEmbedData = { platform, id }
  if (m[3]) {
    const params = new URLSearchParams(m[3])
    const start = params.get('start')
    const page = params.get('page')
    if (start && /^\d+$/.test(start)) out.start = parseInt(start, 10)
    if (page && /^\d+$/.test(page)) out.page = parseInt(page, 10)
  }
  return out
}

export function parseStoredEmbedURL(rawURL: string, mediaType: string): VideoEmbedData | null {
  if (!rawURL || !mediaType) return null
  let u: URL
  try {
    u = new URL(rawURL)
  } catch {
    return null
  }
  if (mediaType === 'video/youtube') {
    if (!u.pathname.startsWith('/embed/')) return null
    const id = u.pathname.slice('/embed/'.length).split('/')[0]
    if (!YT_ID_RE.test(id)) return null
    const out: VideoEmbedData = { platform: 'youtube', id }
    const start = u.searchParams.get('start')
    if (start && /^\d+$/.test(start)) out.start = parseInt(start, 10)
    return out
  }
  if (mediaType === 'video/bilibili') {
    const id = u.searchParams.get('bvid') ?? ''
    if (!BV_ID_RE.test(id)) return null
    const out: VideoEmbedData = { platform: 'bilibili', id }
    const page = u.searchParams.get('page')
    const start = u.searchParams.get('t')
    if (page && /^\d+$/.test(page)) out.page = parseInt(page, 10)
    if (start && /^\d+$/.test(start)) out.start = parseInt(start, 10)
    return out
  }
  return null
}
```

- [ ] **Step 2: Type-check the new file**

Run: `cd frontend && npx tsc --noEmit`
Expected: clean — no type errors. (The repo uses `tsc && vite build` in `npm run build`, so this is the canonical check.)

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/parseVideoPlaceholder.ts
git commit -m "feat(frontend): parseVideoPlaceholder + parseStoredEmbedURL pure helpers"
```

---

## Task 11: Frontend — `<VideoEmbed>` component

**Files:**
- Create: `frontend/src/components/VideoEmbed.tsx`

- [ ] **Step 1: Create the file**

Create `frontend/src/components/VideoEmbed.tsx`:

```tsx
import { VideoEmbedData } from './parseVideoPlaceholder'

function buildSrc(d: VideoEmbedData): string {
  if (d.platform === 'youtube') {
    let s = `https://www.youtube-nocookie.com/embed/${d.id}?rel=0`
    if (d.start && d.start > 0) s += `&start=${d.start}`
    return s
  }
  // bilibili
  const page = d.page && d.page > 0 ? d.page : 1
  let s = `https://player.bilibili.com/player.html?bvid=${d.id}&high_quality=1&autoplay=0&page=${page}`
  if (d.start && d.start > 0) s += `&t=${d.start}`
  return s
}

export default function VideoEmbed(props: VideoEmbedData) {
  const src = buildSrc(props)
  return (
    <div
      style={{
        position: 'relative',
        width: '100%',
        maxWidth: 800,
        aspectRatio: '16 / 9',
        margin: '12px 0',
        background: '#000',
        borderRadius: 8,
        overflow: 'hidden',
      }}
    >
      <iframe
        src={src}
        title={`${props.platform} video ${props.id}`}
        allow="encrypted-media; picture-in-picture"
        allowFullScreen
        loading="lazy"
        referrerPolicy="no-referrer"
        style={{
          position: 'absolute',
          inset: 0,
          width: '100%',
          height: '100%',
          border: 0,
        }}
      />
    </div>
  )
}
```

- [ ] **Step 2: Type-check**

Run: `cd frontend && npx tsc --noEmit`
Expected: clean.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/VideoEmbed.tsx
git commit -m "feat(frontend): VideoEmbed renders 16:9 responsive YouTube/Bilibili iframe"
```

---

## Task 12: Wire `<VideoEmbed>` into `ArticlePlayerCard` (case A)

**Files:**
- Modify: `frontend/src/components/ArticlePlayerCard.tsx`

- [ ] **Step 1: Replace the file body**

Replace the entire contents of `frontend/src/components/ArticlePlayerCard.tsx` with:

```tsx
import { Article } from '../api/client'
import { usePlayer } from '../player/PlayerContext'
import Spinner from './Spinner'
import VideoEmbed from './VideoEmbed'
import { parseStoredEmbedURL } from './parseVideoPlaceholder'

function fmtMinSec(sec: number): string {
  if (!sec || sec <= 0) return ''
  const m = Math.floor(sec / 60)
  const s = sec % 60
  return `${m}分${s.toString().padStart(2, '0')}秒`
}

export default function ArticlePlayerCard({ article }: { article: Article }) {
  if (!article.media_url) return null

  // Branch on media_type: video → embedded iframe; otherwise → audio player.
  if (article.media_type && article.media_type.startsWith('video/')) {
    const v = parseStoredEmbedURL(article.media_url, article.media_type)
    if (!v) return null
    return <VideoEmbed {...v} />
  }

  return <AudioCard article={article} />
}

function AudioCard({ article }: { article: Article }) {
  const p = usePlayer()
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
        aria-label={isCurrent && p.loading ? '加载中' : playing ? '暂停' : '播放'}
        disabled={isCurrent && p.loading && !playing}
        style={{
          width: 56,
          height: 56,
          borderRadius: 999,
          background: '#0066cc',
          color: '#fff',
          border: 'none',
          fontSize: 24,
          cursor: 'pointer',
          flexShrink: 0,
          display: 'inline-flex',
          alignItems: 'center',
          justifyContent: 'center',
        }}
      >
        {isCurrent && p.loading ? <Spinner size={24} color="#fff" /> : playing ? '⏸' : '▶'}
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

(Behavior unchanged for audio — the previous body just moved into `<AudioCard>`.)

- [ ] **Step 2: Type-check**

Run: `cd frontend && npx tsc --noEmit`
Expected: clean.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/ArticlePlayerCard.tsx
git commit -m "feat(frontend): ArticlePlayerCard renders VideoEmbed for video media"
```

---

## Task 13: Wire placeholder paragraph override into `MarkdownArticle` (case B)

**Files:**
- Modify: `frontend/src/components/MarkdownArticle.tsx`

- [ ] **Step 1: Add the paragraph override**

In `frontend/src/components/MarkdownArticle.tsx`, add two imports at the top of the file (after the existing imports):

```tsx
import VideoEmbed from './VideoEmbed'
import { parsePlaceholder } from './parseVideoPlaceholder'
```

Inside the `<ReactMarkdown components={...}>` overrides (after the `a:` override, before the closing `}}`), add:

```tsx
p: ({ children, node, ...rest }) => {
  // children is normally a React element / array. When the paragraph
  // contains exactly one text node matching our placeholder, render
  // a VideoEmbed instead of the <p>.
  const text = extractParagraphText(children)
  if (text) {
    const v = parsePlaceholder(text)
    if (v) return <VideoEmbed {...v} />
  }
  return <p {...rest}>{children}</p>
},
```

And add this helper at the top of the file (above the `MarkdownArticle` export, near the existing helpers):

```tsx
// Returns the plain-text content of paragraph children when it consists
// of a single text run, otherwise null. Used to detect placeholder
// paragraphs without false-positives on rich content.
function extractParagraphText(children: unknown): string | null {
  if (typeof children === 'string') return children
  if (Array.isArray(children)) {
    if (children.length !== 1) return null
    return extractParagraphText(children[0])
  }
  return null
}
```

- [ ] **Step 2: Type-check**

Run: `cd frontend && npx tsc --noEmit`
Expected: clean. (If the `p:` override types complain about `node`, the simplest fix is `p: (props: any) => { ... }` — but try without that first; `react-markdown` v10 should accept the typed form.)

- [ ] **Step 3: Build the frontend bundle**

Run: `cd frontend && npm run build`
Expected: build succeeds (this runs `tsc && vite build`).

- [ ] **Step 4: Commit**

```bash
git add frontend/src/components/MarkdownArticle.tsx
git commit -m "feat(frontend): MarkdownArticle renders VideoEmbed for [[video:...]] placeholders"
```

---

## Task 14: Manual smoke test in Docker

**Files:** none (verification only)

This is the only end-to-end check. Per project memory, frontend changes require a Docker rebuild because nginx serves a pre-built bundle.

- [ ] **Step 1: Rebuild and bring up the stack**

Run: `docker-compose up -d --build`
Expected: all four services (postgres, api, worker, frontend) reach a healthy/running state. Tail logs briefly with `docker-compose logs -f api worker` to spot startup errors.

- [ ] **Step 2: Add a YouTube channel feed and verify case A**

In the running app, add a feed URL like `https://www.youtube.com/feeds/videos.xml?channel_id=UCsBjURrPoezykLs9EqgamOA` (Fireship — pick any channel you like). Wait one fetch cycle (≤ 1 minute), open one of its articles, and confirm:
- A 16:9 embedded YouTube player appears at the top of the article body.
- The body does not contain a duplicate "watch on YouTube" link.
- Clicking play actually plays the video without navigation.

If the article body shows `[[video:youtube:...]]` text instead of an embed, the frontend bundle didn't rebuild — re-run Step 1 and hard-refresh the browser.

- [ ] **Step 3: Add a Bilibili RSSHub feed and verify**

Add a Bilibili feed URL via your RSSHub instance, e.g. `https://rsshub.app/bilibili/user/video/2267573`. Wait one fetch cycle, open an article. Confirm a Bilibili player shows at top, no duplicate link in body.

- [ ] **Step 4: Verify case B with an existing blog feed**

Pick any existing blog feed and find an article that mentions a YouTube link (most tech blogs do). Either let the worker re-fetch it via the existing short-content loop, or trigger a manual re-fetch via the article-content API endpoint. Confirm the YouTube link in the body now renders as an embedded player in place.

- [ ] **Step 5: Negative check — no false positives**

Pick any article whose body has no YouTube/Bilibili link. Confirm rendering is unchanged (no surprise embeds, no broken markdown).

- [ ] **Step 6: Commit nothing (verification step)**

If all four checks pass, proceed to Task 15. If any fail, stop and triage — do not commit a workaround.

---

## Task 15: Final cleanup commit (only if needed)

**Files:** any small follow-ups discovered during smoke test.

- [ ] **Step 1: Review state**

Run: `git status` and `git log --oneline -15`.
Expected: 9 well-named commits from Tasks 1–13, no leftover changes.

- [ ] **Step 2: If any tweaks were made during the smoke test**, commit them with a descriptive message such as `fix(rss): handle BV-id with mixed case` or `style(frontend): add max-width breakpoint for VideoEmbed`. Do not bundle unrelated changes.

- [ ] **Step 3: Done.** Report back to the dispatcher with: commit range, brief summary of what shipped, anything that surprised you during the smoke test.

---

## Spec Coverage Self-Review

Cross-checked spec sections against task numbers:

| Spec section | Implemented in |
|--------------|----------------|
| Goals — case A top card | Tasks 4, 5, 12 |
| Goals — case B inline | Tasks 6, 7, 9, 13 |
| Reuse media columns (no schema) | Task 4 (adapter to MediaInfo) |
| Detection at fetch time | Task 5 (worker + APIs) |
| Supported URL formats — YT | Task 1 |
| Supported URL formats — Bilibili | Task 2 |
| Placeholder grammar | Tasks 3, 7, 10 |
| `ExtractVideo` | Tasks 1, 2 |
| `FindVideosInMarkdown` | Task 7 (delivered as `RewriteVideoLinks` — pragmatic refinement: the spec's read-then-rewrite split into separate calls was unnecessary; a single pass is simpler and the test still covers all the cases the spec listed) |
| Iframe preservation hook | Task 6 |
| URL fallback scan | Task 7 |
| A3 dedup | Task 8 + worker wiring in Task 9 |
| Frontend `VideoEmbed` | Task 11 |
| Frontend top-card branch | Task 12 |
| Frontend paragraph override | Task 13 |
| Manual smoke (3 shapes) | Task 14 |

No gaps. No placeholders.

Type consistency check: `VideoEmbed` struct and method signatures are stable across Tasks 1–8. Frontend `VideoEmbedData` interface (Task 10) is the equivalent shape for TypeScript and is stable across Tasks 11–13. No naming drift.
