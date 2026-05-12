# Twitter / X Tweet Bookmarklet Capture — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** When the existing bookmarklet captures a `x.com` / `twitter.com` / `mobile.twitter.com` single-tweet URL, route the HTML through a new Twitter-aware parser that pulls focal-tweet text + images + author byline + quoted-tweet URL, instead of running the generic article extractor.

**Architecture:** Pure-Go server-side parser in `internal/rss/twitter.go` using `goquery`. Bookmarklet code stays unchanged. The Capture handler detects Twitter URLs via a new `IsTwitterStatusURL` helper, calls `ExtractTweet`, and builds article fields with helper functions. On parser error, fall through to the existing generic extractor so the user never gets a 422 just because Twitter changed DOM.

**Tech Stack:** Go, `github.com/PuerkitoBio/goquery`, existing `internal/util.NormalizeURL`, existing `BookmarkletHandler.Capture`.

**Spec:** `docs/superpowers/specs/2026-05-12-twitter-capture-design.md`

**File map:**
- Modify: `backend/internal/util/urlnorm.go` — add Twitter host rewrite + status-path query strip
- Modify: `backend/internal/util/urlnorm_test.go` — add 5 cases
- Create: `backend/internal/rss/twitter.go` — `IsTwitterStatusURL`, `TweetCapture`, `ErrTweetNotFound`, `ExtractTweet`
- Create: `backend/internal/rss/twitter_test.go` — unit tests
- Create: `backend/internal/rss/testdata/twitter/{tweet_text_only,tweet_with_images,tweet_with_quote,tweet_image_only,tweet_not_found}.html` — fixtures
- Modify: `backend/internal/api/bookmarklet.go` — Twitter branch + `buildTweetTitle`/`buildTweetContent` helpers
- Modify: `backend/internal/api/bookmarklet_test.go` — handler integration test

**Conventions to follow:**
- TDD: failing test → minimal impl → verify → commit, per step.
- Test pattern: copy the table-driven shape of `urlnorm_test.go` and the fixture-driven shape of `linkset_extract_test.go`.
- Run tests from repo root: `cd backend && go test ./internal/<pkg>/... -run <TestName> -v`
- Commit after each green task. Conventional commit messages: `feat(twitter): ...`, `test(twitter): ...`.
- The user works in Docker; backend tests run on the host with `cd backend && go test`. No docker rebuild needed for test iteration.

---

## Task 1: `NormalizeURL` — Twitter host rewrite + status-path query strip

**Files:**
- Modify: `backend/internal/util/urlnorm.go`
- Test: `backend/internal/util/urlnorm_test.go`

- [ ] **Step 1: Add failing test cases**

Edit `backend/internal/util/urlnorm_test.go`, inside the existing `tests` table in `TestNormalizeURL`, append these entries before the closing `}`:

```go
{"rewrites twitter.com host to x.com", "https://twitter.com/x/status/1", "https://x.com/x/status/1"},
{"rewrites mobile.twitter.com host to x.com", "https://mobile.twitter.com/x/status/1", "https://x.com/x/status/1"},
{"rewrites www.twitter.com host to x.com", "https://www.twitter.com/x/status/1", "https://x.com/x/status/1"},
{"rewrites www.x.com host to x.com", "https://www.x.com/x/status/1", "https://x.com/x/status/1"},
{"strips share-tracking query on x.com status path", "https://x.com/x/status/1?s=20", "https://x.com/x/status/1"},
{"strips multi-key share-tracking query", "https://x.com/x/status/1?t=abc&s=20", "https://x.com/x/status/1"},
{"keeps query on non-status x.com path", "https://x.com/search?q=go", "https://x.com/search?q=go"},
{"preserves handle casing on status path", "https://x.com/Karpathy/status/2053872850101285137", "https://x.com/Karpathy/status/2053872850101285137"},
{"twitter.com profile path keeps existing rules", "https://twitter.com/karpathy?ref_src=x", "https://x.com/karpathy"},
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd backend && go test ./internal/util/... -run TestNormalizeURL -v
```

Expected: 9 new sub-tests FAIL (host still `twitter.com` or query still present).

- [ ] **Step 3: Implement the new rules**

Edit `backend/internal/util/urlnorm.go`. Replace the body of `NormalizeURL` to insert Twitter handling right after the host lowercase. Final function:

```go
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

	// Twitter / X canonicalization: collapse legacy hosts onto x.com, then
	// strip share-tracking query (`?s=20`, `?t=...`) from single-tweet
	// permalinks. Tweet IDs are globally unique, so the query carries no
	// content — only attribution noise that would otherwise split dedup.
	switch u.Host {
	case "twitter.com", "www.twitter.com", "mobile.twitter.com", "www.x.com":
		u.Host = "x.com"
	}
	if u.Host == "x.com" && twitterStatusPathRe.MatchString(u.Path) {
		u.RawQuery = ""
	}

	return u.String()
}
```

At the bottom of the file (just before EOF), add the package-level regex:

```go
// twitterStatusPathRe matches /<handle>/status/<numeric_id> with optional
// trailing slash. Used by NormalizeURL to recognize single-tweet permalinks
// whose query string is always pure tracking. Kept here (not in package rss)
// because url normalization is a util concern.
var twitterStatusPathRe = regexp.MustCompile(`^/[A-Za-z0-9_]{1,15}/status/[0-9]+/?$`)
```

Add `"regexp"` to the imports.

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd backend && go test ./internal/util/... -run TestNormalizeURL -v
```

Expected: all sub-tests PASS.

- [ ] **Step 5: Commit**

```bash
cd backend && cd ..
git add backend/internal/util/urlnorm.go backend/internal/util/urlnorm_test.go
git commit -m "$(printf 'feat(util): NormalizeURL collapses twitter hosts to x.com and strips share-tracking query\n')"
```

---

## Task 2: `IsTwitterStatusURL` pure helper

**Files:**
- Create: `backend/internal/rss/twitter.go`
- Create: `backend/internal/rss/twitter_test.go`

- [ ] **Step 1: Write failing test**

Create `backend/internal/rss/twitter_test.go`:

```go
package rss

import "testing"

func TestIsTwitterStatusURL(t *testing.T) {
	tests := []struct {
		name     string
		in       string
		wantID   string
		wantOK   bool
	}{
		{"x.com status", "https://x.com/karpathy/status/2053872850101285137", "2053872850101285137", true},
		{"twitter.com status", "https://twitter.com/x/status/1", "1", true},
		{"mobile.twitter.com status", "https://mobile.twitter.com/x/status/42", "42", true},
		{"www.x.com status", "https://www.x.com/x/status/9", "9", true},
		{"uppercase host", "https://X.com/karpathy/status/2053872850101285137", "2053872850101285137", true},
		{"trailing slash", "https://x.com/karpathy/status/123/", "123", true},
		{"with query (already normalized away, but accept)", "https://x.com/x/status/1?s=20", "1", true},
		{"profile page", "https://x.com/karpathy", "", false},
		{"with_replies", "https://x.com/karpathy/with_replies", "", false},
		{"search page", "https://x.com/search?q=go", "", false},
		{"lists page", "https://x.com/i/lists/1234", "", false},
		{"non-numeric status id", "https://x.com/karpathy/status/abc", "", false},
		{"status without id", "https://x.com/karpathy/status/", "", false},
		{"non-twitter host", "https://example.com/karpathy/status/1", "", false},
		{"empty", "", "", false},
		{"unparseable", "not a url", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotID, gotOK := IsTwitterStatusURL(tt.in)
			if gotID != tt.wantID || gotOK != tt.wantOK {
				t.Errorf("IsTwitterStatusURL(%q) = (%q, %v); want (%q, %v)", tt.in, gotID, gotOK, tt.wantID, tt.wantOK)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd backend && go test ./internal/rss/... -run TestIsTwitterStatusURL -v
```

Expected: build failure ("undefined: IsTwitterStatusURL").

- [ ] **Step 3: Implement `IsTwitterStatusURL` in a new file**

Create `backend/internal/rss/twitter.go`:

```go
package rss

import (
	"net/url"
	"regexp"
	"strings"
)

// twitterHosts is the set of canonical and legacy hosts that serve Twitter /
// X content. Mobile and www subdomains are accepted because users land on
// them depending on the device and the link source.
var twitterHosts = map[string]struct{}{
	"x.com":               {},
	"www.x.com":           {},
	"twitter.com":         {},
	"www.twitter.com":     {},
	"mobile.twitter.com":  {},
}

// twitterStatusPathRe matches a single-tweet permalink path. The handle is
// limited to Twitter's documented 15-char max plus `_` to avoid swallowing
// internal `/i/...` system routes.
var twitterStatusPathRe = regexp.MustCompile(`^/[A-Za-z0-9_]{1,15}/status/([0-9]+)/?$`)

// IsTwitterStatusURL reports whether raw is a Twitter / X single-tweet
// permalink. On match it returns the numeric tweet id from the path so the
// caller can pin DOM extraction to the focal tweet (a tweet page renders
// many tweets in the conversation; only one matches the URL).
func IsTwitterStatusURL(raw string) (statusID string, ok bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", false
	}
	host := strings.ToLower(u.Host)
	if _, ok := twitterHosts[host]; !ok {
		return "", false
	}
	m := twitterStatusPathRe.FindStringSubmatch(u.Path)
	if m == nil {
		return "", false
	}
	return m[1], true
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd backend && go test ./internal/rss/... -run TestIsTwitterStatusURL -v
```

Expected: all sub-tests PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/rss/twitter.go backend/internal/rss/twitter_test.go
git commit -m "feat(rss): IsTwitterStatusURL detects x.com/twitter.com single-tweet permalinks"
```

---

## Task 3: `TweetCapture` struct, `ErrTweetNotFound`, focal-tweet finder

**Files:**
- Modify: `backend/internal/rss/twitter.go`
- Modify: `backend/internal/rss/twitter_test.go`
- Create: `backend/internal/rss/testdata/twitter/tweet_text_only.html`

- [ ] **Step 1: Create the text-only fixture**

Create `backend/internal/rss/testdata/twitter/tweet_text_only.html`. This is a hand-crafted minimal HTML that mirrors the Twitter / X DOM structure (the parser only cares about `data-testid` and `role` markers, not surrounding chrome). Replace with a real capture later when convenient — selectors are what matters.

```html
<!DOCTYPE html>
<html><head><title>karpathy on X</title></head><body>
<main>
  <article role="article" data-testid="tweet" tabindex="-1">
    <div data-testid="User-Name">
      <a role="link" href="/karpathy"><span>Andrej Karpathy</span></a>
      <a role="link" href="/karpathy"><span>@karpathy</span></a>
    </div>
    <div data-testid="tweetText">
      <span>The biggest unlock from </span><span>LLMs</span><span> for me has been the </span><a href="https://example.com/blog" role="link">blog post</a><span> on </span><span>building intuition</span><span>.</span>
    </div>
    <a href="/karpathy/status/9999999999999999999" role="link"><time datetime="2026-04-21T09:00:00.000Z">Apr 21</time></a>
  </article>

  <article role="article" data-testid="tweet" tabindex="0">
    <div data-testid="tweetText"><span>this is a reply, not the focal tweet</span></div>
    <a href="/someone/status/8888888888888888888" role="link"><time datetime="2026-04-21T09:30:00.000Z">Apr 21</time></a>
  </article>
</main>
</body></html>
```

The focal tweet's status id is `9999999999999999999`. The trailing reply article (`tabindex="0"`) exercises the disambiguation rule.

- [ ] **Step 2: Write failing test**

Append to `backend/internal/rss/twitter_test.go`:

```go
func TestExtractTweet_FocalSelection(t *testing.T) {
	data := mustReadFixture(t, "tweet_text_only.html")
	cap, err := ExtractTweet(data, "9999999999999999999")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cap == nil {
		t.Fatal("got nil capture")
	}
}

func TestExtractTweet_FocalNotInHTML(t *testing.T) {
	data := mustReadFixture(t, "tweet_text_only.html")
	_, err := ExtractTweet(data, "1111111111111111111")
	if !errors.Is(err, ErrTweetNotFound) {
		t.Fatalf("want ErrTweetNotFound, got %v", err)
	}
}

func mustReadFixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/twitter/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
```

Add to imports of `twitter_test.go`:

```go
import (
	"errors"
	"os"
	"testing"
)
```

- [ ] **Step 3: Run tests to verify they fail**

```bash
cd backend && go test ./internal/rss/... -run TestExtractTweet -v
```

Expected: build failure (`ExtractTweet`, `ErrTweetNotFound`, `TweetCapture` undefined).

- [ ] **Step 4: Add struct, error, and focal-finder**

Append to `backend/internal/rss/twitter.go`:

```go
import (
	// existing imports stay; add these alongside:
	// "errors"
	// "fmt"
	// "strings"
	// "time"
	// "github.com/PuerkitoBio/goquery"
)

// TweetCapture is the structured result of parsing a logged-in Twitter / X
// page's HTML for a single focal tweet. Empty fields mean "not present on the
// page"; callers decide whether to render or skip each section.
type TweetCapture struct {
	Author       string    // handle without leading @, e.g. "karpathy"
	DisplayName  string    // display name, e.g. "Andrej Karpathy"
	PublishedAt  time.Time // RFC3339 from <time datetime="...">, zero if absent
	TextMarkdown string    // tweet text rendered as markdown
	ImageURLs    []string  // pbs.twimg.com URLs, upgraded to ?name=large
	QuoteURL     string    // x.com permalink of quoted tweet, normalized
}

// ErrTweetNotFound means the focal tweet identified by statusID was not
// present in the HTML, or was so degenerate that no field could be extracted.
// Callers fall back to the generic extractor so the capture still produces an
// article rather than a 422.
var ErrTweetNotFound = errors.New("twitter: focal tweet not found in html")

// ExtractTweet parses html (typically the full document the bookmarklet
// shipped) and returns the focal tweet identified by statusID. The function
// is pure: no I/O, no global state.
func ExtractTweet(html string, statusID string) (*TweetCapture, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil, fmt.Errorf("twitter: parse html: %w", err)
	}

	focal := findFocalTweet(doc, statusID)
	if focal == nil {
		return nil, ErrTweetNotFound
	}

	return &TweetCapture{}, nil // fields filled in later tasks
}

// findFocalTweet picks the <article> element that represents the tweet whose
// permalink matches statusID. A conversation page may include the focal
// tweet, replies, and ancestors; the focal tweet is the one whose permalink
// link matches the URL the user is looking at, with tabindex="-1" as a
// tiebreaker (Twitter marks the focal tweet that way).
func findFocalTweet(doc *goquery.Document, statusID string) *goquery.Selection {
	wantSuffix := "/status/" + statusID
	var match *goquery.Selection
	doc.Find(`article[role="article"][data-testid="tweet"]`).EachWithBreak(func(_ int, art *goquery.Selection) bool {
		// Does this article contain a permalink to our statusID?
		found := false
		art.Find(`a[href]`).EachWithBreak(func(_ int, a *goquery.Selection) bool {
			href, _ := a.Attr("href")
			if strings.HasSuffix(href, wantSuffix) || strings.HasSuffix(href, wantSuffix+"/") {
				found = true
				return false
			}
			return true
		})
		if !found {
			return true
		}
		// Prefer the focal one (tabindex="-1") over context tweets that
		// happen to share the same id (rare but possible with quoted tweets).
		if tabindex, _ := art.Attr("tabindex"); tabindex == "-1" {
			match = art
			return false
		}
		if match == nil {
			match = art
		}
		return true
	})
	return match
}
```

Update the imports at the top of `twitter.go` to include `errors`, `fmt`, `time`, and `github.com/PuerkitoBio/goquery`. Final import block:

```go
import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)
```

- [ ] **Step 5: Run tests to verify they pass**

```bash
cd backend && go test ./internal/rss/... -run TestExtractTweet -v
```

Expected: both sub-tests PASS.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/rss/twitter.go backend/internal/rss/twitter_test.go backend/internal/rss/testdata/twitter/tweet_text_only.html
git commit -m "feat(rss): ExtractTweet scaffolding — TweetCapture, ErrTweetNotFound, focal finder"
```

---

## Task 4: Text extraction

**Files:**
- Modify: `backend/internal/rss/twitter.go`
- Modify: `backend/internal/rss/twitter_test.go`

- [ ] **Step 1: Write failing test**

Append to `twitter_test.go`:

```go
func TestExtractTweet_TextOnly(t *testing.T) {
	data := mustReadFixture(t, "tweet_text_only.html")
	cap, err := ExtractTweet(data, "9999999999999999999")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "The biggest unlock from LLMs for me has been the [blog post](https://example.com/blog) on building intuition."
	if cap.TextMarkdown != want {
		t.Errorf("TextMarkdown mismatch\n got: %q\nwant: %q", cap.TextMarkdown, want)
	}
}
```

- [ ] **Step 2: Run test, expect failure**

```bash
cd backend && go test ./internal/rss/... -run TestExtractTweet_TextOnly -v
```

Expected: FAIL (TextMarkdown is empty string).

- [ ] **Step 3: Implement text walker**

In `backend/internal/rss/twitter.go`, replace the `return &TweetCapture{}, nil` line in `ExtractTweet` with:

```go
out := &TweetCapture{
	TextMarkdown: extractTweetText(focal),
}
return out, nil
```

Add these helpers at the bottom of the file:

```go
// extractTweetText walks the [data-testid="tweetText"] subtree and renders
// it as markdown. Twitter's tweetText is a sequence of spans and anchors;
// emoji are rendered as <img alt="..."> whose `alt` is the emoji char.
func extractTweetText(focal *goquery.Selection) string {
	textNode := focal.Find(`[data-testid="tweetText"]`).First()
	if textNode.Length() == 0 {
		return ""
	}
	var b strings.Builder
	walkTextMarkdown(textNode, &b)
	// Collapse runs of >2 newlines and trim trailing whitespace.
	out := strings.TrimSpace(b.String())
	for strings.Contains(out, "\n\n\n") {
		out = strings.ReplaceAll(out, "\n\n\n", "\n\n")
	}
	return out
}

func walkTextMarkdown(sel *goquery.Selection, b *strings.Builder) {
	sel.Contents().Each(func(_ int, n *goquery.Selection) {
		node := n.Get(0)
		if node == nil {
			return
		}
		switch node.Type {
		case 1: // html.TextNode
			b.WriteString(node.Data)
		case 3: // html.ElementNode
			switch node.Data {
			case "br":
				b.WriteString("\n")
			case "a":
				href, _ := n.Attr("href")
				inner := strings.TrimSpace(n.Text())
				if href != "" && inner != "" {
					fmt.Fprintf(b, "[%s](%s)", inner, href)
				} else {
					b.WriteString(inner)
				}
			case "img":
				if alt, ok := n.Attr("alt"); ok {
					b.WriteString(alt)
				}
			default:
				walkTextMarkdown(n, b)
			}
		}
	})
}
```

Note: `golang.org/x/net/html` exposes the constants `html.TextNode == 1` and `html.ElementNode == 3`; we use the int literals to avoid importing the package just for two constants. The existing codebase uses goquery's `*html.Node` access pattern (`Get(0)`) — copy that style.

Actually, import the html package for clarity. Replace the int-literal switch with the named constants:

```go
import (
	// ...existing...
	"golang.org/x/net/html"
)
```

```go
		switch node.Type {
		case html.TextNode:
			b.WriteString(node.Data)
		case html.ElementNode:
			switch node.Data {
			// ...
			}
		}
```

- [ ] **Step 4: Run test, expect pass**

```bash
cd backend && go test ./internal/rss/... -run TestExtractTweet_TextOnly -v
```

Expected: PASS.

- [ ] **Step 5: Also run full TestExtractTweet to verify no regressions**

```bash
cd backend && go test ./internal/rss/... -run TestExtractTweet -v
```

Expected: all PASS.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/rss/twitter.go backend/internal/rss/twitter_test.go
git commit -m "feat(rss): ExtractTweet renders tweetText spans + links as markdown"
```

---

## Task 5: Author handle, display name, published timestamp

**Files:**
- Modify: `backend/internal/rss/twitter.go`
- Modify: `backend/internal/rss/twitter_test.go`

- [ ] **Step 1: Write failing test**

Append to `twitter_test.go`:

```go
func TestExtractTweet_AuthorAndTimestamp(t *testing.T) {
	data := mustReadFixture(t, "tweet_text_only.html")
	cap, err := ExtractTweet(data, "9999999999999999999")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cap.Author != "karpathy" {
		t.Errorf("Author = %q, want %q", cap.Author, "karpathy")
	}
	if cap.DisplayName != "Andrej Karpathy" {
		t.Errorf("DisplayName = %q, want %q", cap.DisplayName, "Andrej Karpathy")
	}
	wantTime := time.Date(2026, 4, 21, 9, 0, 0, 0, time.UTC)
	if !cap.PublishedAt.Equal(wantTime) {
		t.Errorf("PublishedAt = %v, want %v", cap.PublishedAt, wantTime)
	}
}
```

- [ ] **Step 2: Run test, expect failure**

```bash
cd backend && go test ./internal/rss/... -run TestExtractTweet_AuthorAndTimestamp -v
```

Expected: FAIL on all three fields (zero values).

- [ ] **Step 3: Implement extractors**

In `ExtractTweet`, expand the `out` construction:

```go
out := &TweetCapture{
	Author:       extractAuthorHandle(focal),
	DisplayName:  extractDisplayName(focal),
	PublishedAt:  extractPublishedAt(focal),
	TextMarkdown: extractTweetText(focal),
}
return out, nil
```

Add helpers:

```go
// extractAuthorHandle reads the first profile anchor inside the focal
// article. The href is always /<handle> (no nested path) for the byline
// link. Falls back to empty string — the caller may still know the handle
// from the URL.
var profileHrefRe = regexp.MustCompile(`^/([A-Za-z0-9_]{1,15})$`)

func extractAuthorHandle(focal *goquery.Selection) string {
	var handle string
	focal.Find(`[data-testid="User-Name"] a[href][role="link"]`).EachWithBreak(func(_ int, a *goquery.Selection) bool {
		href, _ := a.Attr("href")
		if m := profileHrefRe.FindStringSubmatch(href); m != nil {
			handle = m[1]
			return false
		}
		return true
	})
	return handle
}

// extractDisplayName takes the first non-empty <span> text inside the
// User-Name container. Twitter renders the display name first, then the
// `@handle` in a second span; trimming and stopping at the first useful
// value picks the display name.
func extractDisplayName(focal *goquery.Selection) string {
	var name string
	focal.Find(`[data-testid="User-Name"] span`).EachWithBreak(func(_ int, s *goquery.Selection) bool {
		txt := strings.TrimSpace(s.Text())
		if txt == "" || strings.HasPrefix(txt, "@") {
			return true
		}
		name = txt
		return false
	})
	return name
}

// extractPublishedAt parses the first <time datetime="..."> inside the
// focal article. Twitter renders it as RFC3339 / ISO 8601 UTC. Failure
// returns the zero time; callers leave Article.PublishedAt nil in that case.
func extractPublishedAt(focal *goquery.Selection) time.Time {
	var ts time.Time
	focal.Find(`time[datetime]`).EachWithBreak(func(_ int, tm *goquery.Selection) bool {
		dt, _ := tm.Attr("datetime")
		if t, err := time.Parse(time.RFC3339, dt); err == nil {
			ts = t.UTC()
			return false
		}
		return true
	})
	return ts
}
```

- [ ] **Step 4: Run tests, expect pass**

```bash
cd backend && go test ./internal/rss/... -run TestExtractTweet -v
```

Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/rss/twitter.go backend/internal/rss/twitter_test.go
git commit -m "feat(rss): ExtractTweet pulls author handle, display name, published timestamp"
```

---

## Task 6: Image extraction

**Files:**
- Modify: `backend/internal/rss/twitter.go`
- Modify: `backend/internal/rss/twitter_test.go`
- Create: `backend/internal/rss/testdata/twitter/tweet_with_images.html`

- [ ] **Step 1: Create fixture**

`backend/internal/rss/testdata/twitter/tweet_with_images.html`:

```html
<!DOCTYPE html>
<html><body>
<main>
  <article role="article" data-testid="tweet" tabindex="-1">
    <div data-testid="User-Name">
      <a role="link" href="/karpathy"><span>Andrej Karpathy</span></a>
      <a role="link" href="/karpathy"><span>@karpathy</span></a>
    </div>
    <div data-testid="tweetText"><span>Two photos from today's experiment.</span></div>
    <div data-testid="tweetPhoto">
      <img src="https://pbs.twimg.com/media/AAA111.jpg?format=jpg&name=small" alt="photo 1" />
    </div>
    <div data-testid="tweetPhoto">
      <img src="https://pbs.twimg.com/media/BBB222.jpg?format=jpg&name=medium" alt="photo 2" />
    </div>
    <!-- Profile image should NOT be picked up -->
    <img src="https://pbs.twimg.com/profile_images/123/avatar.jpg" alt="avatar" />
    <!-- Image inside a role=link (quote-tweet card) — also excluded -->
    <div role="link">
      <div data-testid="tweetPhoto">
        <img src="https://pbs.twimg.com/media/CCC333.jpg?format=jpg&name=small" alt="quoted photo" />
      </div>
    </div>
    <a href="/karpathy/status/2222222222222222222" role="link"><time datetime="2026-04-22T10:00:00.000Z">Apr 22</time></a>
  </article>
</main>
</body></html>
```

- [ ] **Step 2: Write failing test**

```go
func TestExtractTweet_Images(t *testing.T) {
	data := mustReadFixture(t, "tweet_with_images.html")
	cap, err := ExtractTweet(data, "2222222222222222222")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{
		"https://pbs.twimg.com/media/AAA111.jpg?format=jpg&name=large",
		"https://pbs.twimg.com/media/BBB222.jpg?format=jpg&name=large",
	}
	if len(cap.ImageURLs) != len(want) {
		t.Fatalf("ImageURLs len = %d, want %d: %v", len(cap.ImageURLs), len(want), cap.ImageURLs)
	}
	for i := range want {
		if cap.ImageURLs[i] != want[i] {
			t.Errorf("ImageURLs[%d] = %q, want %q", i, cap.ImageURLs[i], want[i])
		}
	}
}
```

- [ ] **Step 3: Run test, expect failure**

```bash
cd backend && go test ./internal/rss/... -run TestExtractTweet_Images -v
```

Expected: FAIL (`ImageURLs` empty).

- [ ] **Step 4: Implement image extraction**

In `ExtractTweet`, add `ImageURLs` to the `out` construction:

```go
out := &TweetCapture{
	Author:       extractAuthorHandle(focal),
	DisplayName:  extractDisplayName(focal),
	PublishedAt:  extractPublishedAt(focal),
	TextMarkdown: extractTweetText(focal),
	ImageURLs:    extractTweetImages(focal),
}
```

Add helper:

```go
// extractTweetImages collects photo URLs from the focal tweet, excluding:
//   - profile avatars (path contains /profile_images/)
//   - images inside any role="link" container (these are quote-tweet
//     thumbnails; the quote tweet itself is captured separately as a link).
// Each URL has its `name=...` query parameter rewritten to `name=large` to
// pull the highest-quality variant Twitter serves anonymously.
func extractTweetImages(focal *goquery.Selection) []string {
	var urls []string
	focal.Find(`[data-testid="tweetPhoto"] img[src]`).Each(func(_ int, img *goquery.Selection) {
		// Skip if any ancestor up to focal has role="link" (quote card).
		if hasAncestor(img, focal, `[role="link"]`) {
			return
		}
		src, _ := img.Attr("src")
		if !strings.Contains(src, "pbs.twimg.com") || strings.Contains(src, "/profile_images/") {
			return
		}
		urls = append(urls, upgradeTwitterImageURL(src))
	})
	return urls
}

// hasAncestor reports whether sel has an ancestor matching selector within
// the subtree rooted at stop (exclusive).
func hasAncestor(sel, stop *goquery.Selection, selector string) bool {
	stopNode := stop.Get(0)
	cur := sel.Parent()
	for cur.Length() > 0 {
		if cur.Get(0) == stopNode {
			return false
		}
		if cur.Is(selector) {
			return true
		}
		cur = cur.Parent()
	}
	return false
}

// upgradeTwitterImageURL rewrites the `name=` query param to `large`, which
// is the highest-resolution variant Twitter serves to anonymous callers. If
// there's no `name=` param, the URL is returned as-is.
func upgradeTwitterImageURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	q := u.Query()
	if _, has := q["name"]; !has {
		return raw
	}
	q.Set("name", "large")
	u.RawQuery = q.Encode()
	return u.String()
}
```

- [ ] **Step 5: Run test, expect pass**

```bash
cd backend && go test ./internal/rss/... -run TestExtractTweet -v
```

Expected: all PASS.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/rss/twitter.go backend/internal/rss/twitter_test.go backend/internal/rss/testdata/twitter/tweet_with_images.html
git commit -m "feat(rss): ExtractTweet pulls focal photos, upgrades to name=large, skips avatars and quote cards"
```

---

## Task 7: Quote-tweet URL

**Files:**
- Modify: `backend/internal/rss/twitter.go`
- Modify: `backend/internal/rss/twitter_test.go`
- Create: `backend/internal/rss/testdata/twitter/tweet_with_quote.html`

- [ ] **Step 1: Create fixture**

`backend/internal/rss/testdata/twitter/tweet_with_quote.html` (modeled after the user's example URL):

```html
<!DOCTYPE html>
<html><body>
<main>
  <article role="article" data-testid="tweet" tabindex="-1">
    <div data-testid="User-Name">
      <a role="link" href="/karpathy"><span>Andrej Karpathy</span></a>
      <a role="link" href="/karpathy"><span>@karpathy</span></a>
    </div>
    <div data-testid="tweetText">
      <span>+1 to this excellent thread.</span>
    </div>
    <!-- Quote-tweet card: wrapped in role="link" pointing at the quoted permalink -->
    <div role="link">
      <a href="/someone_else/status/3333333333333333333">
        <div data-testid="User-Name"><span>Other Person</span><span>@someone_else</span></div>
        <div data-testid="tweetText"><span>quoted tweet body — we ignore this</span></div>
      </a>
    </div>
    <a href="/karpathy/status/2053872850101285137" role="link"><time datetime="2026-04-23T08:00:00.000Z">Apr 23</time></a>
  </article>
</main>
</body></html>
```

- [ ] **Step 2: Write failing test**

```go
func TestExtractTweet_QuoteURL(t *testing.T) {
	data := mustReadFixture(t, "tweet_with_quote.html")
	cap, err := ExtractTweet(data, "2053872850101285137")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "https://x.com/someone_else/status/3333333333333333333"
	if cap.QuoteURL != want {
		t.Errorf("QuoteURL = %q, want %q", cap.QuoteURL, want)
	}
	// The quoted tweet's text must NOT leak into our TextMarkdown.
	if strings.Contains(cap.TextMarkdown, "quoted tweet body") {
		t.Errorf("quoted tweet body leaked into TextMarkdown: %q", cap.TextMarkdown)
	}
}
```

Add `"strings"` to test imports if not already present.

- [ ] **Step 3: Run test, expect failure**

```bash
cd backend && go test ./internal/rss/... -run TestExtractTweet_QuoteURL -v
```

Expected: FAIL — `QuoteURL` empty, and potentially text leak (since `walkTextMarkdown` recurses into `<a>` text only via the anchor-handler; but the quote card's `<a>` wraps multiple containers, so the leak test guards us).

- [ ] **Step 4: Implement quote-URL extraction**

In `ExtractTweet`, add `QuoteURL` and pass the URL package's normalizer:

```go
out := &TweetCapture{
	Author:       extractAuthorHandle(focal),
	DisplayName:  extractDisplayName(focal),
	PublishedAt:  extractPublishedAt(focal),
	TextMarkdown: extractTweetText(focal),
	ImageURLs:    extractTweetImages(focal),
	QuoteURL:     extractQuoteURL(focal, statusID),
}
```

Add helper. The quote tweet's link is the *first* link inside a `role="link"` container whose href is a `/status/<id>` path and not equal to the focal permalink:

```go
// extractQuoteURL returns the x.com permalink of a tweet quoted by the
// focal tweet. Twitter renders the quote card inside a [role="link"] div
// whose first anchor's href is the quoted permalink. The focal tweet's own
// permalink anchor lives outside any role="link" container, so we don't
// accidentally pick ourselves up.
func extractQuoteURL(focal *goquery.Selection, focalStatusID string) string {
	var quote string
	focal.Find(`[role="link"]`).EachWithBreak(func(_ int, link *goquery.Selection) bool {
		link.Find(`a[href]`).EachWithBreak(func(_ int, a *goquery.Selection) bool {
			href, _ := a.Attr("href")
			if !twitterStatusPathRe.MatchString(href) {
				return true
			}
			if strings.HasSuffix(href, "/status/"+focalStatusID) ||
				strings.HasSuffix(href, "/status/"+focalStatusID+"/") {
				return true
			}
			// Build absolute URL and run through the package's own
			// canonicalizer-equivalent (host already x.com, query is none).
			quote = "https://x.com" + strings.TrimSuffix(href, "/")
			return false
		})
		return quote == ""
	})
	return quote
}
```

- [ ] **Step 5: Run test, expect pass**

```bash
cd backend && go test ./internal/rss/... -run TestExtractTweet -v
```

Expected: all PASS.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/rss/twitter.go backend/internal/rss/twitter_test.go backend/internal/rss/testdata/twitter/tweet_with_quote.html
git commit -m "feat(rss): ExtractTweet picks quoted-tweet permalink as QuoteURL"
```

---

## Task 8: Image-only tweet (empty text is valid)

**Files:**
- Modify: `backend/internal/rss/twitter.go`
- Modify: `backend/internal/rss/twitter_test.go`
- Create: `backend/internal/rss/testdata/twitter/tweet_image_only.html`

- [ ] **Step 1: Create fixture**

`backend/internal/rss/testdata/twitter/tweet_image_only.html`:

```html
<!DOCTYPE html>
<html><body>
<main>
  <article role="article" data-testid="tweet" tabindex="-1">
    <div data-testid="User-Name">
      <a role="link" href="/karpathy"><span>Andrej Karpathy</span></a>
      <a role="link" href="/karpathy"><span>@karpathy</span></a>
    </div>
    <!-- no tweetText element at all -->
    <div data-testid="tweetPhoto">
      <img src="https://pbs.twimg.com/media/IMGONLY.jpg?format=jpg&name=small" alt="image only" />
    </div>
    <a href="/karpathy/status/4444444444444444444" role="link"><time datetime="2026-04-24T11:00:00.000Z">Apr 24</time></a>
  </article>
</main>
</body></html>
```

- [ ] **Step 2: Write failing test**

```go
func TestExtractTweet_ImageOnly(t *testing.T) {
	data := mustReadFixture(t, "tweet_image_only.html")
	cap, err := ExtractTweet(data, "4444444444444444444")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cap.TextMarkdown != "" {
		t.Errorf("TextMarkdown should be empty, got %q", cap.TextMarkdown)
	}
	if len(cap.ImageURLs) != 1 {
		t.Errorf("want 1 image, got %d", len(cap.ImageURLs))
	}
}
```

- [ ] **Step 3: Run test, expect pass on a fresh run**

```bash
cd backend && go test ./internal/rss/... -run TestExtractTweet_ImageOnly -v
```

Expected: PASS — current code already returns empty `TextMarkdown` when `tweetText` is absent, and image extraction is independent. No code change needed if the assertion passes. If it fails (e.g. extractor returned `ErrTweetNotFound` because all fields-empty check is too strict — but no such check has been added yet), proceed to Step 4.

- [ ] **Step 4: If failure — add the empty-tweet guard**

If the test passes, skip this step. Otherwise, append to the end of `ExtractTweet` body (right before `return out, nil`):

```go
if out.TextMarkdown == "" && len(out.ImageURLs) == 0 && out.QuoteURL == "" {
	return nil, ErrTweetNotFound
}
```

Then re-run.

- [ ] **Step 5: Commit (if any change made; otherwise skip)**

```bash
git add backend/internal/rss/twitter.go backend/internal/rss/twitter_test.go backend/internal/rss/testdata/twitter/tweet_image_only.html
git commit -m "test(rss): ExtractTweet handles image-only tweet (no tweetText element)"
```

If no `.go` change was needed, only the fixture and test are added; commit message:

```bash
git add backend/internal/rss/twitter_test.go backend/internal/rss/testdata/twitter/tweet_image_only.html
git commit -m "test(rss): ExtractTweet handles image-only tweet (no tweetText element)"
```

---

## Task 9: Not-found / degenerate page returns ErrTweetNotFound

**Files:**
- Modify: `backend/internal/rss/twitter.go`
- Modify: `backend/internal/rss/twitter_test.go`
- Create: `backend/internal/rss/testdata/twitter/tweet_not_found.html`

- [ ] **Step 1: Create fixture**

`backend/internal/rss/testdata/twitter/tweet_not_found.html` (a profile page with no matching tweet):

```html
<!DOCTYPE html>
<html><body>
<main>
  <article role="article" data-testid="tweet" tabindex="0">
    <div data-testid="tweetText"><span>some unrelated tweet</span></div>
    <a href="/randomuser/status/7777777777777777777" role="link"><time datetime="2026-04-25T12:00:00.000Z">Apr 25</time></a>
  </article>
</main>
</body></html>
```

- [ ] **Step 2: Write failing test**

```go
func TestExtractTweet_ErrTweetNotFound(t *testing.T) {
	data := mustReadFixture(t, "tweet_not_found.html")
	_, err := ExtractTweet(data, "1234567890123456789")
	if !errors.Is(err, ErrTweetNotFound) {
		t.Fatalf("want ErrTweetNotFound, got %v", err)
	}
}
```

- [ ] **Step 3: Run test, expect pass**

```bash
cd backend && go test ./internal/rss/... -run TestExtractTweet_ErrTweetNotFound -v
```

Expected: PASS — the focal finder already returns nil when no article matches the statusID, and `ExtractTweet` returns `ErrTweetNotFound` in that branch.

- [ ] **Step 4: Run the full ExtractTweet suite to confirm no regressions**

```bash
cd backend && go test ./internal/rss/... -run TestExtractTweet -v
```

Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/rss/twitter_test.go backend/internal/rss/testdata/twitter/tweet_not_found.html
git commit -m "test(rss): ExtractTweet returns ErrTweetNotFound when focal statusID is absent"
```

---

## Task 10: Handler — wire Twitter branch + content builder

**Files:**
- Modify: `backend/internal/api/bookmarklet.go`
- Modify: `backend/internal/api/bookmarklet_test.go`

Note: the existing `bookmarklet_test.go` only tests pure helpers (`extractContentFromHTML`). There is no HTTP-handler test scaffolding. We add unit tests for the new pure helpers (`buildTweetContent`, `buildTweetTitle`) and verify the handler branch via the manual smoke check in Task 11. Setting up an httptest router + mock repos for one new path is more code than it pays for.

- [ ] **Step 1: Write failing tests for `buildTweetContent` and `buildTweetTitle`**

Append to `backend/internal/api/bookmarklet_test.go`:

```go
func TestBuildTweetContent_FullCase(t *testing.T) {
	cap := &rss.TweetCapture{
		Author:       "karpathy",
		DisplayName:  "Andrej Karpathy",
		PublishedAt:  time.Date(2026, 4, 23, 8, 0, 0, 0, time.UTC),
		TextMarkdown: "+1 to this excellent thread.",
		ImageURLs:    []string{"https://pbs.twimg.com/media/AAA111.jpg?name=large"},
		QuoteURL:     "https://x.com/someone_else/status/3333333333333333333",
	}
	got := buildTweetContent(cap)
	want := "> @karpathy (Andrej Karpathy) · 2026-04-23\n\n" +
		"+1 to this excellent thread.\n\n" +
		"![](https://pbs.twimg.com/media/AAA111.jpg?name=large)\n\n" +
		"引用: https://x.com/someone_else/status/3333333333333333333"
	if got != want {
		t.Errorf("buildTweetContent mismatch\n got:\n%s\nwant:\n%s", got, want)
	}
}

func TestBuildTweetContent_ImageOnly(t *testing.T) {
	cap := &rss.TweetCapture{
		Author:    "karpathy",
		ImageURLs: []string{"https://pbs.twimg.com/media/IMG.jpg?name=large"},
	}
	got := buildTweetContent(cap)
	want := "> @karpathy\n\n![](https://pbs.twimg.com/media/IMG.jpg?name=large)"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestBuildTweetContent_NoTimestamp(t *testing.T) {
	cap := &rss.TweetCapture{
		Author:       "karpathy",
		DisplayName:  "Andrej Karpathy",
		TextMarkdown: "hi",
	}
	got := buildTweetContent(cap)
	want := "> @karpathy (Andrej Karpathy)\n\nhi"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestBuildTweetTitle_ShortText(t *testing.T) {
	cap := &rss.TweetCapture{
		Author:       "karpathy",
		TextMarkdown: "+1 to this excellent thread.",
	}
	if got := buildTweetTitle(cap); got != "+1 to this excellent thread." {
		t.Errorf("got %q", got)
	}
}

func TestBuildTweetTitle_LongText(t *testing.T) {
	long := strings.Repeat("a", 80)
	cap := &rss.TweetCapture{TextMarkdown: long}
	got := buildTweetTitle(cap)
	if len([]rune(got)) != 61 { // 60 a's + ellipsis
		t.Errorf("title rune len = %d, want 61; got %q", len([]rune(got)), got)
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("title should end with ellipsis: %q", got)
	}
}

func TestBuildTweetTitle_NewlinesFlatten(t *testing.T) {
	cap := &rss.TweetCapture{TextMarkdown: "first line\nsecond line"}
	if got := buildTweetTitle(cap); got != "first line second line" {
		t.Errorf("got %q", got)
	}
}

func TestBuildTweetTitle_ImageOnlyFallback(t *testing.T) {
	cap := &rss.TweetCapture{
		Author:    "karpathy",
		ImageURLs: []string{"x"},
	}
	if got := buildTweetTitle(cap); got != "@karpathy 的推文" {
		t.Errorf("got %q", got)
	}
}
```

Update the imports of `bookmarklet_test.go` to include:

```go
import (
	"strings"
	"testing"
	"time"

	"github.com/bytedance/rss-pal/internal/rss"
)
```

- [ ] **Step 2: Run tests, expect failure**

```bash
cd backend && go test ./internal/api/... -run "TestBuildTweet" -v
```

Expected: build failure (`buildTweetContent` / `buildTweetTitle` undefined).

- [ ] **Step 3: Add the Twitter branch + builders to the handler**

Open `backend/internal/api/bookmarklet.go`. In `Capture`, immediately after the line `normalized := util.NormalizeURL(req.URL)` and BEFORE the line `content, err := extractContentFromHTML(...)`, insert:

```go
var (
	content        string
	title          = strings.TrimSpace(req.Title)
	publishedAt    *time.Time
	wasTwitter     bool
)

if statusID, ok := rss.IsTwitterStatusURL(normalized); ok {
	cap, err := rss.ExtractTweet(req.HTML, statusID)
	if err == nil {
		content = buildTweetContent(cap)
		title = buildTweetTitle(cap)
		if !cap.PublishedAt.IsZero() {
			t := cap.PublishedAt
			publishedAt = &t
		}
		wasTwitter = true
	} else {
		log.Printf("bookmarklet: twitter extract for %s failed (%v); falling back to generic extractor", normalized, err)
	}
}

if !wasTwitter {
	c, err := extractContentFromHTML(req.HTML, req.URL)
	if err != nil || strings.TrimSpace(c) == "" {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "无法从页面提取正文"})
		return
	}
	content = c
}

if title == "" {
	title = normalized
}
```

Now find and DELETE the original lines (the ones we are replacing):

```go
content, err := extractContentFromHTML(req.HTML, req.URL)
if err != nil || strings.TrimSpace(content) == "" {
	c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "无法从页面提取正文"})
	return
}

title := strings.TrimSpace(req.Title)
if title == "" {
	title = normalized
}
```

**WARNING:** the new block shadows `c` for the gin context. Rename the local variable in the fallback to avoid that:

```go
if !wasTwitter {
	body, err := extractContentFromHTML(req.HTML, req.URL)
	if err != nil || strings.TrimSpace(body) == "" {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "无法从页面提取正文"})
		return
	}
	content = body
}
```

In the final `&model.Article{...}` literal further down, add:

```go
article := &model.Article{
	FeedID:      feed.ID,
	Title:       title,
	URL:         normalized,
	Content:     content,
	PublishedAt: publishedAt, // nil for non-twitter (preserves existing behavior)
}
```

Add the missing import for `time` at the top of the file if it's not already present.

Also add the builder functions at the bottom of the file (or in a small new file `backend/internal/api/bookmarklet_twitter.go` — whichever you prefer; doing it inline avoids package-internal symbol churn):

```go
// buildTweetContent renders a TweetCapture as the article body. The first
// line is a markdown blockquote that carries the author byline and date,
// which the reader renders just like Twitter's own attribution row. Empty
// fields are silently dropped — image-only and quote-only tweets still
// produce a useful article body.
func buildTweetContent(cap *rss.TweetCapture) string {
	var sections []string

	if byline := buildTweetByline(cap); byline != "" {
		sections = append(sections, byline)
	}
	if cap.TextMarkdown != "" {
		sections = append(sections, cap.TextMarkdown)
	}
	for _, img := range cap.ImageURLs {
		sections = append(sections, "![]("+img+")")
	}
	if cap.QuoteURL != "" {
		sections = append(sections, "引用: "+cap.QuoteURL)
	}

	return strings.Join(sections, "\n\n")
}

// buildTweetByline produces "> @handle (DisplayName) · YYYY-MM-DD" when
// possible, degrading gracefully when any component is missing. Returns ""
// only when even the handle is missing (extremely rare — we got the
// statusID from a URL that also contained the handle, so this would mean
// the parser couldn't find any profile anchor inside the focal article).
func buildTweetByline(cap *rss.TweetCapture) string {
	if cap.Author == "" && cap.DisplayName == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString("> ")
	if cap.Author != "" {
		b.WriteString("@")
		b.WriteString(cap.Author)
	}
	if cap.DisplayName != "" {
		if cap.Author != "" {
			b.WriteString(" ")
		}
		b.WriteString("(")
		b.WriteString(cap.DisplayName)
		b.WriteString(")")
	}
	if !cap.PublishedAt.IsZero() {
		b.WriteString(" · ")
		b.WriteString(cap.PublishedAt.UTC().Format("2006-01-02"))
	}
	return b.String()
}

// buildTweetTitle takes the first 60 runes of the tweet text (newlines
// flattened to spaces), or falls back to "@handle 的推文" for image-only
// tweets. Final fallback if even the handle is missing is "Twitter 推文".
func buildTweetTitle(cap *rss.TweetCapture) string {
	text := strings.TrimSpace(cap.TextMarkdown)
	if text != "" {
		text = strings.ReplaceAll(text, "\n", " ")
		runes := []rune(text)
		if len(runes) <= 60 {
			return text
		}
		return string(runes[:60]) + "…"
	}
	if cap.Author != "" {
		return "@" + cap.Author + " 的推文"
	}
	return "Twitter 推文"
}
```

Add `"github.com/bytedance/rss-pal/internal/rss"` import if not already present (the file already imports `"github.com/bytedance/rss-pal/internal/rss"` per the existing `rss.ComputeMetrics` call — confirm before editing).

- [ ] **Step 4: Run all backend tests to check nothing else broke**

```bash
cd backend && go test ./internal/api/... ./internal/rss/... ./internal/util/... -v
```

Expected: all PASS, including the new Twitter handler test and the unchanged generic-capture tests.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/api/bookmarklet.go backend/internal/api/bookmarklet_test.go
git commit -m "feat(api): bookmarklet Capture routes Twitter URLs to ExtractTweet + byline content builder"
```

---

## Task 11: End-to-end manual smoke check + rebuild

**Files:** none (manual verification)

- [ ] **Step 1: Rebuild the api container**

```bash
docker-compose up -d --build api
docker-compose logs -f api | head -30
```

Expected: api comes up, no startup errors.

- [ ] **Step 2: Browse to one tweet via the bookmarklet**

In a logged-in `x.com` tab (any tweet — e.g. `https://x.com/karpathy/status/2053872850101285137`), click the existing 📑 bookmarklet.

Expected:
- Capture succeeds (status 201 created or 200 updated).
- The new article appears in the "📑 收藏" feed.
- Open the article in the reader: byline `> @karpathy (Andrej Karpathy) · YYYY-MM-DD` at top, tweet text below, images below text (if any), `引用: ...` link at bottom (if the tweet had a quote).

- [ ] **Step 3: Browse to a non-Twitter page and capture**

Click the bookmarklet on a generic article (e.g. a blog post). Verify the regular capture path is unaffected — the article shows up with normal content extraction.

- [ ] **Step 4: Commit (final, only if the manual check surfaced a needed tweak)**

If no code change was needed, this step is a no-op.

```bash
# Only if a fix was applied:
git add <files>
git commit -m "fix(twitter): <issue>"
```

---

## Wrap-up

- [ ] **Push to the open PR (#22)**

```bash
git push origin feature/twitter-capture
```

PR #22 will auto-update with the new commits. The spec lands first (already in the PR); implementation commits follow.

- [ ] **Verify PR contents**

```bash
gh pr view 22 --json title,url,additions,deletions,files
```

Expected files in the diff: the spec, the new `twitter.go` + tests + fixtures, the modified `bookmarklet.go` + tests, the modified `urlnorm.go` + tests.

---

## Notes for the implementing agent

- **Fixtures are hand-crafted templates.** They model the selectors Twitter actually uses today (`data-testid="tweet"`, `data-testid="tweetText"`, `data-testid="tweetPhoto"`, `data-testid="User-Name"`, `role="link"`, `tabindex="-1"`, `time[datetime]`). If during manual smoke testing (Task 11) the parser fails on a real tweet, the fix is to grab the real HTML via `agent-browser --session-name twitter`, save it to `testdata/twitter/<name>.html`, and update the parser (and possibly the test expectations) to match. Do not loosen selectors speculatively — wait for a real failure.
- **No new env vars, no migrations, no docker-compose changes.** This is a pure backend code change.
- **Frontend doesn't change.** The reader already renders markdown blockquotes, images, and links correctly. If during manual testing the byline doesn't render as a blockquote (markdown not rendered), that's a frontend renderer bug to file separately, not a blocker for this PR.
- **Do NOT modify the bookmarklet JS.** Existing tokens and installed bookmarklets must keep working.
