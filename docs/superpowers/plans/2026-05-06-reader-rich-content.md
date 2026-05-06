# Reader Rich Content + Reading Mode Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Refactor the article scraper to preserve markdown structure (images, code blocks, tables, lists) and add an opt-in reading mode with adjustable font size, font family, and background theme.

**Architecture:** Backend `ContentFetcher` switches from plain-text extraction to HTML→Markdown conversion via `html-to-markdown/v2`. New `/api/proxy/image` route streams images server-side with SSRF guards and a Referer injection (fixes WeChat/Zhihu hotlink blocking). Frontend renders markdown via `react-markdown` + `remark-gfm` + `rehype-highlight`. A new full-screen `ReadingLayout` component is conditionally rendered when the user toggles reading mode; settings (font size 12–24px, sans/serif, 5 background presets) live in a `useReaderSettings` hook backed by `localStorage`.

**Tech Stack:**
- Backend: Go 1.20 (existing), `gin`, `goquery`, new `github.com/JohannesKaufmann/html-to-markdown/v2`
- Frontend: React 18, Vite, `react-markdown` (existing), new `remark-gfm`, `rehype-highlight`, `highlight.js`
- Storage: PostgreSQL (no schema change), `localStorage` for reader settings
- Source spec: `docs/superpowers/specs/2026-05-06-reader-rich-content-design.md`

---

## File Structure

**New backend files:**
- `backend/internal/api/proxy.go` — image proxy handler + SSRF guard
- `backend/internal/api/proxy_test.go` — proxy handler unit/integration tests
- `backend/cmd/backfill_content/main.go` — one-shot CLI to refetch all articles in markdown format

**Modified backend files:**
- `backend/internal/rss/content.go` — switch text extraction to markdown via `html-to-markdown/v2`
- `backend/internal/rss/content_test.go` (new) — coverage for img/code/table/list/heading conversion
- `backend/cmd/server/main.go` — register `/api/proxy/image` route (public, no auth)
- `backend/go.mod` / `backend/go.sum` — new dependency

**New frontend files:**
- `frontend/src/components/MarkdownArticle.tsx` — wraps `ReactMarkdown` with GFM + highlight, rewrites `<img>` to proxy URL
- `frontend/src/components/ReaderSettingsPanel.tsx` — Aa floating button + popover with font/bg controls
- `frontend/src/components/ReadingLayout.tsx` — full-screen reading layout (article + collapsible summary)
- `frontend/src/hooks/useReaderSettings.ts` — `localStorage`-backed reader settings hook

**Modified frontend files:**
- `frontend/src/pages/ArticlePage.tsx` — wire reader settings; conditionally render `ReadingLayout`; replace `\n{2,}` split with `MarkdownArticle`; add `r` keyboard shortcut and `📖 阅读模式` button
- `frontend/src/index.css` — `[data-reader-bg]` CSS variable themes; reading-layout typography; markdown body styles
- `frontend/package.json` / `frontend/package-lock.json` — new dependencies

**Out of scope:** database schema changes, HTML sanitization layer, server-side reader preferences, image disk caching.

---

## Phase 1 — Backend: Markdown content extraction

### Task 1: Add `html-to-markdown/v2` dependency

**Files:**
- Modify: `backend/go.mod`, `backend/go.sum`

- [ ] **Step 1: Add dependency**

```bash
cd backend
go get github.com/JohannesKaufmann/html-to-markdown/v2@latest
go mod tidy
```

- [ ] **Step 2: Verify compile**

```bash
cd backend
go build ./...
```

Expected: builds clean, no errors.

- [ ] **Step 3: Commit**

```bash
git add backend/go.mod backend/go.sum
git commit -m "chore(backend): add html-to-markdown/v2 dependency"
```

---

### Task 2: Test-drive markdown extraction (paragraphs + headings)

Replace plain-text extraction with HTML→Markdown conversion. Start with the simplest case (paragraphs and a heading) so we can drive the API design before adding richer fixtures in Task 3.

**Files:**
- Create: `backend/internal/rss/content_test.go`
- Modify: `backend/internal/rss/content.go`

- [ ] **Step 1: Write the failing test**

Create `backend/internal/rss/content_test.go`:

```go
package rss

import (
	"strings"
	"testing"
)

func TestFetchContentFromReader_HeadingsAndParagraphs(t *testing.T) {
	html := `<html><body><article>
		<h2>Hello World</h2>
		<p>This is the first paragraph with enough length to clear filters.</p>
		<p>And here is a second paragraph, also long enough to be kept.</p>
	</article></body></html>`

	f := NewContentFetcher()
	got, err := f.FetchContentFromReader(strings.NewReader(html))
	if err != nil {
		t.Fatalf("FetchContentFromReader: %v", err)
	}
	if !strings.Contains(got, "## Hello World") {
		t.Errorf("expected '## Hello World' in output, got:\n%s", got)
	}
	if !strings.Contains(got, "first paragraph") {
		t.Errorf("expected first paragraph text in output, got:\n%s", got)
	}
	if !strings.Contains(got, "second paragraph") {
		t.Errorf("expected second paragraph text in output, got:\n%s", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd backend
go test ./internal/rss/ -run TestFetchContentFromReader_HeadingsAndParagraphs -v
```

Expected: FAIL — current `FetchContentFromReader` returns plain text without `##` markers.

- [ ] **Step 3: Refactor `content.go` to emit markdown**

Replace `extractText`, `FetchContentFromReader`, and the success branch of `fetchDirect` so they produce markdown. Concretely:

- Add a package-level converter built once via `html-to-markdown/v2`, with the GFM commonmark + table plugins enabled.
- `extractMarkdown(selection *goquery.Selection) (string, error)` renders the selection's HTML through the converter.
- `fetchDirect`, after picking the main selector with the existing length heuristic, calls `extractMarkdown` instead of `extractText`.
- `FetchContentFromReader` does the same selector loop as `fetchDirect` then converts the chosen selection.

Edit `backend/internal/rss/content.go` — replace the `extractText` definition (currently at the bottom of the file) and the call sites in `fetchDirect` / `FetchContentFromReader`. Add the converter init at the top.

```go
import (
	// existing imports +
	htmltomd "github.com/JohannesKaufmann/html-to-markdown/v2/converter"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/commonmark"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/table"
)

var mdConverter = htmltomd.NewConverter(
	htmltomd.WithPlugins(
		commonmark.NewCommonmarkPlugin(),
		table.NewTablePlugin(),
	),
)
```

Replace `extractText` with:

```go
// extractMarkdown converts the HTML inside the goquery selection into Markdown.
// Falls back to the selection's plain text if conversion fails (which should
// not happen under normal use but keeps the pipeline robust).
func extractMarkdown(selection *goquery.Selection) string {
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

In `fetchDirect`, replace:

```go
content = extractText(selection)
```

with:

```go
content = extractMarkdown(selection)
```

In `FetchContentFromReader`, replace the body so it mirrors `fetchDirect`'s selector loop and uses `extractMarkdown`:

```go
func (f *ContentFetcher) FetchContentFromReader(r io.Reader) (string, error) {
	doc, err := goquery.NewDocumentFromReader(r)
	if err != nil {
		return "", err
	}

	doc.Find("script, style, nav, header, footer, aside").Remove()

	selectors := []string{"article", "[role='main']", "main", ".content", ".post", "#content", "body"}
	var content string
	for _, sel := range selectors {
		if doc.Find(sel).Length() == 0 {
			continue
		}
		md := extractMarkdown(doc.Find(sel).First())
		if len(md) > 50 {
			content = md
			break
		}
	}

	if content == "" {
		// Last-resort paragraph fallback (kept for ultra-stripped pages)
		doc.Find("p").Each(func(_ int, s *goquery.Selection) {
			t := strings.TrimSpace(s.Text())
			if len(t) > 30 {
				content += t + "\n\n"
			}
		})
	}

	return cleanContent(content), nil
}
```

Delete the now-unused `extractText` function.

- [ ] **Step 4: Run test to verify it passes**

```bash
cd backend
go test ./internal/rss/ -run TestFetchContentFromReader_HeadingsAndParagraphs -v
```

Expected: PASS.

- [ ] **Step 5: Run full rss package tests**

```bash
cd backend
go test ./internal/rss/ -v
```

Expected: all existing tests still PASS (metrics tests untouched).

- [ ] **Step 6: Commit**

```bash
git add backend/internal/rss/content.go backend/internal/rss/content_test.go
git commit -m "refactor(rss): emit markdown from content extractor

Switch ContentFetcher to convert the main-content HTML selection into
markdown via html-to-markdown/v2 (GFM + tables). Preserves the existing
selector heuristic and 50k char ceiling. extractText removed; downstream
storage is now markdown text."
```

---

### Task 3: Coverage for images, code blocks, lists, tables

Lock down the markdown shape we expect for the rich-content cases that motivated this work.

**Files:**
- Modify: `backend/internal/rss/content_test.go`

- [ ] **Step 1: Add image, code, list, table tests**

Append to `content_test.go`:

```go
func TestFetchContentFromReader_PreservesImage(t *testing.T) {
	html := `<html><body><article>
		<p>Intro paragraph long enough to keep around.</p>
		<p><img src="https://example.com/cat.png" alt="a cat"></p>
		<p>Trailing paragraph long enough to keep around.</p>
	</article></body></html>`

	f := NewContentFetcher()
	got, err := f.FetchContentFromReader(strings.NewReader(html))
	if err != nil {
		t.Fatalf("FetchContentFromReader: %v", err)
	}
	if !strings.Contains(got, "![a cat](https://example.com/cat.png)") {
		t.Errorf("expected markdown image, got:\n%s", got)
	}
}

func TestFetchContentFromReader_PreservesCodeBlock(t *testing.T) {
	html := `<html><body><article>
		<p>Here is some Go code we definitely want to keep:</p>
		<pre><code class="language-go">package main

func main() {
	println("hello")
}</code></pre>
		<p>And some text after the code block to pad it out.</p>
	</article></body></html>`

	f := NewContentFetcher()
	got, err := f.FetchContentFromReader(strings.NewReader(html))
	if err != nil {
		t.Fatalf("FetchContentFromReader: %v", err)
	}
	if !strings.Contains(got, "```") {
		t.Errorf("expected fenced code block, got:\n%s", got)
	}
	if !strings.Contains(got, `println("hello")`) {
		t.Errorf("expected code body preserved, got:\n%s", got)
	}
}

func TestFetchContentFromReader_PreservesList(t *testing.T) {
	html := `<html><body><article>
		<p>Reasons we care, padded so the article selector keeps it:</p>
		<ul>
			<li>First reason here</li>
			<li>Second reason here</li>
			<li>Third reason here</li>
		</ul>
	</article></body></html>`

	f := NewContentFetcher()
	got, err := f.FetchContentFromReader(strings.NewReader(html))
	if err != nil {
		t.Fatalf("FetchContentFromReader: %v", err)
	}
	for _, want := range []string{"- First reason", "- Second reason", "- Third reason"} {
		if !strings.Contains(got, want) {
			t.Errorf("expected %q in output, got:\n%s", want, got)
		}
	}
}

func TestFetchContentFromReader_PreservesTable(t *testing.T) {
	html := `<html><body><article>
		<p>Performance table for the readers in our experiment:</p>
		<table>
			<thead><tr><th>Lib</th><th>QPS</th></tr></thead>
			<tbody>
				<tr><td>A</td><td>120</td></tr>
				<tr><td>B</td><td>340</td></tr>
			</tbody>
		</table>
	</article></body></html>`

	f := NewContentFetcher()
	got, err := f.FetchContentFromReader(strings.NewReader(html))
	if err != nil {
		t.Fatalf("FetchContentFromReader: %v", err)
	}
	if !strings.Contains(got, "| Lib | QPS |") && !strings.Contains(got, "| Lib  | QPS |") {
		t.Errorf("expected GFM table header in output, got:\n%s", got)
	}
	if !strings.Contains(got, "120") || !strings.Contains(got, "340") {
		t.Errorf("expected row values in table output, got:\n%s", got)
	}
}
```

- [ ] **Step 2: Run new tests**

```bash
cd backend
go test ./internal/rss/ -v
```

Expected: all four new tests PASS along with the heading/paragraph test from Task 2.

> If a test fails because the converter output differs in spacing or surrounding markup, prefer relaxing the assertion (use `strings.Contains` checks for the meaningful tokens) rather than rewriting the converter — the goal is structural preservation, not byte-exact output.

- [ ] **Step 3: Commit**

```bash
git add backend/internal/rss/content_test.go
git commit -m "test(rss): cover image/code/list/table markdown extraction"
```

---

## Phase 2 — Backend: Image proxy

### Task 4: SSRF guard helper

**Files:**
- Create: `backend/internal/api/proxy.go`
- Create: `backend/internal/api/proxy_test.go`

- [ ] **Step 1: Write the failing test**

Create `backend/internal/api/proxy_test.go`:

```go
package api

import (
	"net"
	"testing"
)

func TestIsBlockedIP(t *testing.T) {
	cases := []struct {
		ip      string
		blocked bool
	}{
		{"127.0.0.1", true},
		{"10.0.0.1", true},
		{"172.16.0.1", true},
		{"192.168.1.1", true},
		{"169.254.169.254", true}, // metadata
		{"::1", true},
		{"fc00::1", true},
		{"fe80::1", true},
		{"8.8.8.8", false},
		{"1.1.1.1", false},
		{"2606:4700:4700::1111", false},
	}
	for _, tc := range cases {
		ip := net.ParseIP(tc.ip)
		if ip == nil {
			t.Fatalf("parse %q failed", tc.ip)
		}
		if got := isBlockedIP(ip); got != tc.blocked {
			t.Errorf("isBlockedIP(%s) = %v, want %v", tc.ip, got, tc.blocked)
		}
	}
}

func TestValidateImageURL(t *testing.T) {
	cases := []struct {
		raw     string
		wantErr bool
	}{
		{"https://example.com/img.png", false},
		{"http://example.com/img.png", false},
		{"ftp://example.com/img.png", true},
		{"file:///etc/passwd", true},
		{"https://127.0.0.1/img.png", true},
		{"https://192.168.1.1/img.png", true},
		{"not-a-url", true},
		{"", true},
	}
	for _, tc := range cases {
		_, err := validateImageURL(tc.raw)
		if (err != nil) != tc.wantErr {
			t.Errorf("validateImageURL(%q) err=%v, wantErr=%v", tc.raw, err, tc.wantErr)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd backend
go test ./internal/api/ -run "TestIsBlockedIP|TestValidateImageURL" -v
```

Expected: FAIL — `isBlockedIP` and `validateImageURL` do not exist.

- [ ] **Step 3: Implement guard**

Create `backend/internal/api/proxy.go`:

```go
package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// blockedCIDRs is the IPv4/IPv6 ranges we refuse to proxy to. Covers loopback,
// RFC1918 private ranges, link-local, IPv6 ULA, and the cloud metadata IP.
var blockedCIDRs = func() []*net.IPNet {
	raw := []string{
		"127.0.0.0/8",
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"169.254.0.0/16",
		"::1/128",
		"fc00::/7",
		"fe80::/10",
	}
	out := make([]*net.IPNet, 0, len(raw))
	for _, c := range raw {
		_, n, err := net.ParseCIDR(c)
		if err != nil {
			panic(fmt.Sprintf("bad cidr %q: %v", c, err))
		}
		out = append(out, n)
	}
	return out
}()

func isBlockedIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	for _, n := range blockedCIDRs {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// validateImageURL parses raw, requires http/https, and rejects hosts whose
// resolved IPs land in any blocked range. Returns the parsed URL on success.
func validateImageURL(raw string) (*url.URL, error) {
	if raw == "" {
		return nil, errors.New("empty url")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("unsupported scheme %q", u.Scheme)
	}
	if u.Host == "" {
		return nil, errors.New("missing host")
	}
	host := u.Hostname()
	if ip := net.ParseIP(host); ip != nil {
		if isBlockedIP(ip) {
			return nil, errors.New("blocked address")
		}
		return u, nil
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return nil, fmt.Errorf("dns: %w", err)
	}
	for _, ip := range ips {
		if isBlockedIP(ip) {
			return nil, errors.New("blocked address")
		}
	}
	return u, nil
}
```

(The `ProxyImage` handler will be added in Task 5; this task is just the guard.)

- [ ] **Step 4: Run test to verify it passes**

```bash
cd backend
go test ./internal/api/ -run "TestIsBlockedIP|TestValidateImageURL" -v
```

Expected: both tests PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/api/proxy.go backend/internal/api/proxy_test.go
git commit -m "feat(api): add image-proxy SSRF guard"
```

---

### Task 5: Image proxy handler

**Files:**
- Modify: `backend/internal/api/proxy.go`
- Modify: `backend/internal/api/proxy_test.go`

- [ ] **Step 1: Write the failing handler test**

Append to `backend/internal/api/proxy_test.go`:

```go
import (
	// also keep imports from Task 4
	"net/http/httptest"
	"strings"

	"github.com/gin-gonic/gin"
)

func TestProxyImage_StreamsAndInjectsReferer(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var gotReferer string
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotReferer = r.Header.Get("Referer")
		w.Header().Set("Content-Type", "image/png")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("\x89PNG\r\n\x1a\nFAKEBYTES"))
	}))
	defer origin.Close()

	r := gin.New()
	r.GET("/api/proxy/image", ProxyImage)

	req := httptest.NewRequest(http.MethodGet, "/api/proxy/image?url="+origin.URL+"/cat.png", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/png" {
		t.Errorf("content-type = %q, want image/png", ct)
	}
	if !strings.HasPrefix(gotReferer, origin.URL) {
		t.Errorf("referer = %q, want prefix %q", gotReferer, origin.URL)
	}
	if !strings.Contains(rec.Body.String(), "FAKEBYTES") {
		t.Errorf("body did not stream upstream payload: %q", rec.Body.String())
	}
}

func TestProxyImage_RejectsNonImageContentType(t *testing.T) {
	gin.SetMode(gin.TestMode)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<html>"))
	}))
	defer origin.Close()

	r := gin.New()
	r.GET("/api/proxy/image", ProxyImage)
	req := httptest.NewRequest(http.MethodGet, "/api/proxy/image?url="+origin.URL+"/foo", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Errorf("expected non-200 for text/html upstream, got 200")
	}
}

func TestProxyImage_RejectsBadScheme(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/proxy/image", ProxyImage)
	req := httptest.NewRequest(http.MethodGet, "/api/proxy/image?url=file:///etc/passwd", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest && rec.Code != http.StatusForbidden {
		t.Errorf("expected 4xx for file:// scheme, got %d", rec.Code)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd backend
go test ./internal/api/ -run TestProxyImage -v
```

Expected: FAIL — `ProxyImage` is undefined.

- [ ] **Step 3: Implement handler**

Append to `backend/internal/api/proxy.go`:

```go
const (
	proxyMaxBytes  = 10 * 1024 * 1024 // 10MB
	proxyTimeout   = 30 * time.Second
	proxyUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
)

var proxyClient = &http.Client{Timeout: proxyTimeout}

// ProxyImage streams an image from a remote URL through this server. It is
// unauthenticated by design — image tags do not carry our auth cookie/JWT
// reliably and the content is public anyway. SSRF is the real risk and is
// handled by validateImageURL.
func ProxyImage(c *gin.Context) {
	raw := c.Query("url")
	target, err := validateImageURL(raw)
	if err != nil {
		c.String(http.StatusBadRequest, "invalid url: %s", err)
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), proxyTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		c.String(http.StatusBadRequest, "build request: %s", err)
		return
	}
	// Inject a Referer matching the target origin to defeat hotlink protection
	// (notably WeChat / Zhihu).
	req.Header.Set("Referer", target.Scheme+"://"+target.Host+"/")
	req.Header.Set("User-Agent", proxyUserAgent)

	resp, err := proxyClient.Do(req)
	if err != nil {
		c.String(http.StatusBadGateway, "upstream: %s", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		c.String(http.StatusBadGateway, "upstream status %d", resp.StatusCode)
		return
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "image/") {
		c.String(http.StatusUnsupportedMediaType, "non-image content-type: %s", ct)
		return
	}

	c.Header("Content-Type", ct)
	if et := resp.Header.Get("ETag"); et != "" {
		c.Header("ETag", et)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "" {
		c.Header("Cache-Control", cc)
	} else {
		c.Header("Cache-Control", "public, max-age=86400, immutable")
	}
	c.Status(http.StatusOK)
	_, _ = io.Copy(c.Writer, io.LimitReader(resp.Body, proxyMaxBytes))
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd backend
go test ./internal/api/ -run TestProxyImage -v
```

Expected: all three handler tests PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/api/proxy.go backend/internal/api/proxy_test.go
git commit -m "feat(api): image proxy handler with referer injection

Streams remote images through this server with a 10MB cap and a
Referer header matching the target origin (defeats WeChat/Zhihu
hotlink blocking). Non-image content-types are rejected. SSRF
guard from validateImageURL applies."
```

---

### Task 6: Wire proxy route into router

**Files:**
- Modify: `backend/cmd/server/main.go`

- [ ] **Step 1: Register the route as public**

Open `backend/cmd/server/main.go`. Find the "Public share route (no auth required)" comment block (currently around the `router.GET("/api/share/:token", ...)` line). Add the proxy route alongside it:

```go
	// Public share route (no auth required)
	router.GET("/api/share/:token", shareHandler.GetByToken)

	// Public image proxy (no auth — <img> tags can't reliably carry auth headers).
	router.GET("/api/proxy/image", api.ProxyImage)
```

- [ ] **Step 2: Build and run the server briefly**

```bash
cd backend
go build ./cmd/server
```

Expected: builds clean.

- [ ] **Step 3: Smoke check the route**

Spin up the server (or use an existing local one) and curl the route:

```bash
curl -sI "http://localhost:8080/api/proxy/image?url=https://www.google.com/images/branding/googlelogo/2x/googlelogo_color_272x92dp.png" | head -5
```

Expected: `HTTP/1.1 200 OK` and `Content-Type: image/png`. (If you don't want to start a real server, skip — Task 5 tests already cover the handler.)

- [ ] **Step 4: Commit**

```bash
git add backend/cmd/server/main.go
git commit -m "feat(server): expose /api/proxy/image route"
```

---

## Phase 3 — Backend: Backfill CLI

### Task 7: One-shot backfill command

**Files:**
- Create: `backend/cmd/backfill_content/main.go`

- [ ] **Step 1: Write the binary**

Create `backend/cmd/backfill_content/main.go`:

```go
// Command backfill_content re-fetches every article that already has content
// (i.e. previously scraped under the plain-text era) and overwrites it with
// the new markdown output. Idempotent: re-running converges. Rate-limited via
// --qps so it does not trip source-side rate limiters.
package main

import (
	"context"
	"database/sql"
	"flag"
	"log"
	"time"

	"github.com/bytedance/rss-pal/internal/config"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/bytedance/rss-pal/internal/rss"
)

func main() {
	qps := flag.Float64("qps", 1.0, "max requests per second to source sites")
	feedID := flag.Int("feed-id", 0, "limit to one feed id (0 = all feeds)")
	dryRun := flag.Bool("dry-run", false, "log work without writing to DB")
	flag.Parse()

	cfg := config.Load()
	db, err := repository.NewDB(&cfg.Database)
	if err != nil {
		log.Fatalf("db connect: %v", err)
	}
	defer db.Close()

	rows, err := selectArticles(db, *feedID)
	if err != nil {
		log.Fatalf("select: %v", err)
	}
	defer rows.Close()

	articleRepo := repository.NewArticleRepository(db)
	fetcher := rss.NewContentFetcher()

	interval := time.Duration(float64(time.Second) / *qps)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	type job struct {
		ID  int
		URL string
	}
	jobs := []job{}
	for rows.Next() {
		var j job
		if err := rows.Scan(&j.ID, &j.URL); err != nil {
			log.Fatalf("scan: %v", err)
		}
		jobs = append(jobs, j)
	}
	total := len(jobs)
	log.Printf("backfill: %d articles queued (qps=%.2f, dryRun=%v)", total, *qps, *dryRun)

	ok, fail := 0, 0
	for i, j := range jobs {
		<-ticker.C
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		md, err := fetcher.FetchContent(ctx, j.URL)
		cancel()
		if err != nil || md == "" {
			fail++
			log.Printf("[%d/%d] ✗ id=%d url=%s err=%v", i+1, total, j.ID, j.URL, err)
			continue
		}
		if *dryRun {
			ok++
			log.Printf("[%d/%d] ✓ id=%d (dry-run, %d chars)", i+1, total, j.ID, len(md))
			continue
		}
		wc, rm := rss.ComputeMetrics(md)
		if err := articleRepo.UpdateContent(j.ID, md, wc, rm); err != nil {
			fail++
			log.Printf("[%d/%d] ✗ id=%d update: %v", i+1, total, j.ID, err)
			continue
		}
		ok++
		log.Printf("[%d/%d] ✓ id=%d (%d chars)", i+1, total, j.ID, len(md))
	}
	log.Printf("backfill done: ok=%d fail=%d total=%d", ok, fail, total)
}

func selectArticles(db *sql.DB, feedID int) (*sql.Rows, error) {
	if feedID > 0 {
		return db.Query(
			`SELECT id, url FROM articles WHERE feed_id=$1 AND content IS NOT NULL AND content != '' ORDER BY id`,
			feedID,
		)
	}
	return db.Query(
		`SELECT id, url FROM articles WHERE content IS NOT NULL AND content != '' ORDER BY id`,
	)
}
```

> If `repository.ArticleRepository.UpdateContent` has a different signature in your tree, adjust the call site to match. The signature used here matches the call in `internal/api/content.go::FetchContent`.

- [ ] **Step 2: Build it**

```bash
cd backend
go build ./cmd/backfill_content
```

Expected: builds clean.

- [ ] **Step 3: Dry-run smoke against a single feed**

(Optional — only if you have a running DB locally.)

```bash
cd backend
go run ./cmd/backfill_content --dry-run --feed-id 1 --qps 2
```

Expected: prints `[i/total] ✓ id=… (dry-run, N chars)` lines and a final summary; nothing written to DB.

- [ ] **Step 4: Commit**

```bash
git add backend/cmd/backfill_content/main.go
git commit -m "feat(backfill): one-shot CLI to refetch articles as markdown

Idempotent rate-limited refetch over the articles table; defaults to
qps=1 to stay polite to source sites. --feed-id scopes to one feed,
--dry-run skips DB writes."
```

> Operationally: run on the host with `go run ./cmd/backfill_content --qps 1` or build a binary and exec it inside the api container. Off-peak is preferred — at qps=1 a 5k-article archive takes ~80 minutes.

---

## Phase 4 — Frontend: Markdown rendering infrastructure

### Task 8: Install frontend dependencies

**Files:**
- Modify: `frontend/package.json`, `frontend/package-lock.json`

- [ ] **Step 1: Install**

```bash
cd frontend
npm install remark-gfm rehype-highlight highlight.js
```

- [ ] **Step 2: Verify the dev build**

```bash
cd frontend
npm run build
```

Expected: build succeeds. (If you don't have time for a full build, `npx tsc --noEmit` is a faster sanity check.)

- [ ] **Step 3: Commit**

```bash
git add frontend/package.json frontend/package-lock.json
git commit -m "chore(frontend): add remark-gfm, rehype-highlight, highlight.js"
```

---

### Task 9: `MarkdownArticle` component

**Files:**
- Create: `frontend/src/components/MarkdownArticle.tsx`

- [ ] **Step 1: Write the component**

Create `frontend/src/components/MarkdownArticle.tsx`:

```tsx
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import rehypeHighlight from 'rehype-highlight'
import 'highlight.js/styles/github.css'

type Props = {
  source: string
}

// Rewrites <img src="..."> to go through the backend proxy so hotlink-
// protected sites (WeChat, Zhihu) actually render. Also forces external
// links to open in a new tab.
export default function MarkdownArticle({ source }: Props) {
  return (
    <div className="markdown-body">
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        rehypePlugins={[rehypeHighlight]}
        components={{
          img: ({ src, alt, ...rest }) => {
            const proxied = src
              ? `/api/proxy/image?url=${encodeURIComponent(src)}`
              : undefined
            return (
              <img
                src={proxied}
                alt={alt ?? ''}
                loading="lazy"
                decoding="async"
                style={{ maxWidth: '100%', height: 'auto' }}
                {...rest}
              />
            )
          },
          a: ({ href, children, ...rest }) => (
            <a href={href} target="_blank" rel="noopener noreferrer" {...rest}>
              {children}
            </a>
          ),
        }}
      >
        {source}
      </ReactMarkdown>
    </div>
  )
}
```

- [ ] **Step 2: Type-check**

```bash
cd frontend
npx tsc --noEmit
```

Expected: clean. (If `ReactMarkdown` types complain about the `img`/`a` component shape, you can loosen with `components={{ img: (props: any) => ... }}` — react-markdown's typings have been a moving target across versions.)

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/MarkdownArticle.tsx
git commit -m "feat(frontend): MarkdownArticle component with image proxy + GFM"
```

---

### Task 10: Use `MarkdownArticle` in normal mode

Wire the new renderer in before adding reading mode so any regression is isolated.

**Files:**
- Modify: `frontend/src/pages/ArticlePage.tsx`

- [ ] **Step 1: Replace the body-rendering block**

In `frontend/src/pages/ArticlePage.tsx`, find the JSX block that splits `article.content` by `\n{2,}` (around the "原文内容" section). Replace it with `<MarkdownArticle>`:

Add the import near the top:

```tsx
import MarkdownArticle from '../components/MarkdownArticle'
```

Replace this block:

```tsx
{article.content ? (
  <div style={{ lineHeight: 1.8, fontSize: 15 }}>
    {article.content.split(/\n{2,}/).map((para, i) => {
      const trimmed = para.trim()
      if (!trimmed) return null
      return (
        <p key={i} style={{ marginBottom: '0.9em', whiteSpace: 'pre-line' }}>{trimmed}</p>
      )
    })}
  </div>
) : (
  <div className="text-muted">暂无内容，点击"重新抓取"从原文链接抓取</div>
)}
```

with:

```tsx
{article.content ? (
  <div style={{ lineHeight: 1.8, fontSize: 15 }}>
    <MarkdownArticle source={article.content} />
  </div>
) : (
  <div className="text-muted">暂无内容，点击"重新抓取"从原文链接抓取</div>
)}
```

- [ ] **Step 2: Rebuild Docker frontend and smoke-test in browser**

```bash
docker-compose up -d --build frontend
```

Then open an existing article in the app and confirm:
- Plain-text articles still render as paragraphs (markdown renders prose unchanged).
- After clicking "重新抓取" on an article with images, the images load (verify in DevTools Network tab that requests go to `/api/proxy/image?url=...`).
- Clicking a link in the article body opens in a new tab.

> If images don't load, check that the backend proxy route is reachable (Task 6) and that nginx in `docker-compose` proxies `/api/*` to the api container.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/pages/ArticlePage.tsx
git commit -m "feat(frontend): render article body as markdown via MarkdownArticle"
```

---

## Phase 5 — Frontend: Reading mode

### Task 11: `useReaderSettings` hook

**Files:**
- Create: `frontend/src/hooks/useReaderSettings.ts`

- [ ] **Step 1: Write the hook**

Create `frontend/src/hooks/useReaderSettings.ts`:

```ts
import { useCallback, useEffect, useState } from 'react'

export type ReaderMode = 'normal' | 'reading'
export type ReaderFontFamily = 'sans' | 'serif'
export type ReaderBgTheme = 'default' | 'sepia' | 'green' | 'gray' | 'dark'

export type ReaderSettings = {
  mode: ReaderMode
  fontSize: number      // 12..24, step 1
  fontFamily: ReaderFontFamily
  bgTheme: ReaderBgTheme
}

const STORAGE_KEY = 'rsspal:reader-settings'

const DEFAULTS: ReaderSettings = {
  mode: 'normal',
  fontSize: 16,
  fontFamily: 'sans',
  bgTheme: 'default',
}

function load(): ReaderSettings {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (!raw) return DEFAULTS
    const parsed = JSON.parse(raw) as Partial<ReaderSettings>
    return {
      mode: parsed.mode === 'reading' ? 'reading' : 'normal',
      fontSize: clampFont(parsed.fontSize ?? DEFAULTS.fontSize),
      fontFamily: parsed.fontFamily === 'serif' ? 'serif' : 'sans',
      bgTheme: isTheme(parsed.bgTheme) ? parsed.bgTheme : 'default',
    }
  } catch {
    return DEFAULTS
  }
}

function clampFont(n: number): number {
  if (!Number.isFinite(n)) return DEFAULTS.fontSize
  return Math.max(12, Math.min(24, Math.round(n)))
}

function isTheme(v: unknown): v is ReaderBgTheme {
  return v === 'default' || v === 'sepia' || v === 'green' || v === 'gray' || v === 'dark'
}

export function useReaderSettings() {
  const [settings, setSettings] = useState<ReaderSettings>(() => load())

  // Persist on every change
  useEffect(() => {
    try { localStorage.setItem(STORAGE_KEY, JSON.stringify(settings)) } catch {}
  }, [settings])

  // Sync across tabs
  useEffect(() => {
    const onStorage = (e: StorageEvent) => {
      if (e.key === STORAGE_KEY) setSettings(load())
    }
    window.addEventListener('storage', onStorage)
    return () => window.removeEventListener('storage', onStorage)
  }, [])

  const setMode = useCallback((mode: ReaderMode) => setSettings(s => ({ ...s, mode })), [])
  const setFontSize = useCallback((fontSize: number) =>
    setSettings(s => ({ ...s, fontSize: clampFont(fontSize) })), [])
  const setFontFamily = useCallback((fontFamily: ReaderFontFamily) =>
    setSettings(s => ({ ...s, fontFamily })), [])
  const setBgTheme = useCallback((bgTheme: ReaderBgTheme) =>
    setSettings(s => ({ ...s, bgTheme })), [])
  const toggleMode = useCallback(() =>
    setSettings(s => ({ ...s, mode: s.mode === 'reading' ? 'normal' : 'reading' })), [])

  return {
    ...settings,
    setMode,
    toggleMode,
    setFontSize,
    setFontFamily,
    setBgTheme,
  }
}
```

- [ ] **Step 2: Type-check**

```bash
cd frontend
npx tsc --noEmit
```

Expected: clean.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/hooks/useReaderSettings.ts
git commit -m "feat(frontend): localStorage-backed reader settings hook"
```

---

### Task 12: `ReaderSettingsPanel` component

**Files:**
- Create: `frontend/src/components/ReaderSettingsPanel.tsx`

- [ ] **Step 1: Write the component**

Create `frontend/src/components/ReaderSettingsPanel.tsx`:

```tsx
import { useEffect, useRef, useState } from 'react'
import type {
  ReaderBgTheme,
  ReaderFontFamily,
} from '../hooks/useReaderSettings'

type Props = {
  fontSize: number
  fontFamily: ReaderFontFamily
  bgTheme: ReaderBgTheme
  onFontSize: (n: number) => void
  onFontFamily: (f: ReaderFontFamily) => void
  onBgTheme: (b: ReaderBgTheme) => void
}

const THEMES: { key: ReaderBgTheme; label: string; swatch: string }[] = [
  { key: 'default', label: '白',     swatch: '#ffffff' },
  { key: 'sepia',   label: '米黄',   swatch: '#f5edd6' },
  { key: 'green',   label: '护眼绿', swatch: '#cce8cf' },
  { key: 'gray',    label: '浅灰',   swatch: '#ebebeb' },
  { key: 'dark',    label: '暗色',   swatch: '#1a1a1a' },
]

export default function ReaderSettingsPanel({
  fontSize,
  fontFamily,
  bgTheme,
  onFontSize,
  onFontFamily,
  onBgTheme,
}: Props) {
  const [open, setOpen] = useState(false)
  const ref = useRef<HTMLDivElement>(null)

  // Close on outside click + Esc
  useEffect(() => {
    if (!open) return
    const onClick = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false)
    }
    const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') setOpen(false) }
    document.addEventListener('mousedown', onClick)
    document.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('mousedown', onClick)
      document.removeEventListener('keydown', onKey)
    }
  }, [open])

  return (
    <div ref={ref} style={{ position: 'fixed', right: 24, bottom: 24, zIndex: 1100 }}>
      {open && (
        <div style={{
          position: 'absolute',
          right: 0,
          bottom: 64,
          width: 240,
          background: '#fff',
          color: '#1a1a1a',
          border: '1px solid #ddd',
          borderRadius: 10,
          padding: 14,
          boxShadow: '0 8px 24px rgba(0,0,0,0.12)',
        }}>
          <div style={{ fontSize: 11, color: '#888', marginBottom: 6 }}>字号</div>
          <div style={{ display: 'flex', gap: 6, marginBottom: 14 }}>
            <button onClick={() => onFontSize(fontSize - 1)} disabled={fontSize <= 12} style={fsBtn}>A−</button>
            <div style={{ flex: 2, padding: 6, textAlign: 'center', background: '#fff', border: '1px solid #ddd', borderRadius: 4, fontSize: 12 }}>
              {fontSize} px
            </div>
            <button onClick={() => onFontSize(fontSize + 1)} disabled={fontSize >= 24} style={{ ...fsBtn, fontSize: 14 }}>A+</button>
          </div>

          <div style={{ fontSize: 11, color: '#888', marginBottom: 6 }}>字体</div>
          <div style={{ display: 'flex', gap: 6, marginBottom: 14 }}>
            <button onClick={() => onFontFamily('sans')}
              style={{ ...familyBtn, ...(fontFamily === 'sans' ? familyBtnActive : {}) }}>Sans</button>
            <button onClick={() => onFontFamily('serif')}
              style={{ ...familyBtn, ...(fontFamily === 'serif' ? familyBtnActive : {}), fontFamily: '"Source Han Serif SC", "Songti SC", serif' }}>
              Serif
            </button>
          </div>

          <div style={{ fontSize: 11, color: '#888', marginBottom: 6 }}>背景</div>
          <div style={{ display: 'flex', gap: 8 }}>
            {THEMES.map(t => (
              <button key={t.key}
                aria-label={t.label}
                title={t.label}
                onClick={() => onBgTheme(t.key)}
                style={{
                  width: 28, height: 28, borderRadius: '50%',
                  background: t.swatch,
                  border: bgTheme === t.key ? '2px solid #222' : '1px solid #ccc',
                  padding: 0, cursor: 'pointer',
                }}
              />
            ))}
          </div>
        </div>
      )}

      <button
        onClick={() => setOpen(o => !o)}
        aria-label="阅读设置"
        title="阅读设置"
        style={{
          width: 48, height: 48, borderRadius: '50%',
          background: '#222', color: '#fff', fontWeight: 600,
          border: 'none', cursor: 'pointer',
          boxShadow: '0 4px 12px rgba(0,0,0,0.2)',
        }}
      >Aa</button>
    </div>
  )
}

const fsBtn: React.CSSProperties = {
  flex: 1, padding: 6, textAlign: 'center', background: '#f3f3f3', border: 'none', borderRadius: 4, fontSize: 12, cursor: 'pointer',
}
const familyBtn: React.CSSProperties = {
  flex: 1, padding: 8, textAlign: 'center', background: '#f3f3f3', border: '1px solid transparent', borderRadius: 4, fontSize: 13, cursor: 'pointer',
}
const familyBtnActive: React.CSSProperties = { background: '#fff', border: '1px solid #222' }
```

- [ ] **Step 2: Type-check**

```bash
cd frontend
npx tsc --noEmit
```

Expected: clean.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/ReaderSettingsPanel.tsx
git commit -m "feat(frontend): reader settings popover (Aa floating button)"
```

---

### Task 13: `ReadingLayout` component

**Files:**
- Create: `frontend/src/components/ReadingLayout.tsx`

- [ ] **Step 1: Write the component**

Create `frontend/src/components/ReadingLayout.tsx`:

```tsx
import { useEffect, useState } from 'react'
import ReactMarkdown from 'react-markdown'
import MarkdownArticle from './MarkdownArticle'
import ReaderSettingsPanel from './ReaderSettingsPanel'
import type {
  ReaderBgTheme,
  ReaderFontFamily,
} from '../hooks/useReaderSettings'

type ArticleLite = {
  title: string
  url: string
  published_at: string | null
  word_count: number
  reading_minutes: number
  content: string
  summary_brief: string
  summary_detailed: string
}

type Props = {
  article: ArticleLite
  fontSize: number
  fontFamily: ReaderFontFamily
  bgTheme: ReaderBgTheme
  onExit: () => void
  onFontSize: (n: number) => void
  onFontFamily: (f: ReaderFontFamily) => void
  onBgTheme: (b: ReaderBgTheme) => void
}

export default function ReadingLayout(props: Props) {
  const { article, fontSize, fontFamily, bgTheme, onExit } = props

  // Apply theme on <body> so the entire viewport adopts the bg color.
  useEffect(() => {
    const prev = document.body.getAttribute('data-reader-bg')
    document.body.setAttribute('data-reader-bg', bgTheme)
    document.body.classList.add('reading-mode-active')
    return () => {
      if (prev !== null) document.body.setAttribute('data-reader-bg', prev)
      else document.body.removeAttribute('data-reader-bg')
      document.body.classList.remove('reading-mode-active')
    }
  }, [bgTheme])

  const [summaryOpen, setSummaryOpen] = useState(false)

  const fmtDate = (s: string | null) => s ? new Date(s).toLocaleString('zh-CN') : ''
  const ff = fontFamily === 'serif'
    ? '"Source Han Serif SC", "Songti SC", serif'
    : 'system-ui, -apple-system, "PingFang SC", "Microsoft YaHei", sans-serif'

  return (
    <div className="reading-layout" style={{ fontFamily: ff }}>
      <div className="reading-toolbar">
        <button className="reading-exit" onClick={onExit} title="退出阅读模式 (Esc / r)">← 退出阅读模式</button>
      </div>

      <article className="reading-article" style={{ fontSize }}>
        <h1 className="reading-title">{article.title}</h1>
        <div className="reading-meta">
          <span>{fmtDate(article.published_at)}</span>
          {article.word_count > 0 && <span> · {article.word_count} 字</span>}
          {article.reading_minutes > 0 && <span> · 约 {article.reading_minutes} 分钟</span>}
          <span> · </span>
          <a href={article.url} target="_blank" rel="noopener noreferrer">原文链接</a>
        </div>

        {(article.summary_brief || article.summary_detailed) && (
          <div className="reading-summary">
            <button className="reading-summary-toggle" onClick={() => setSummaryOpen(o => !o)}>
              {summaryOpen ? '▼' : '▶'} AI 摘要
            </button>
            {summaryOpen && (
              <div className="reading-summary-body">
                {article.summary_brief && <ReactMarkdown>{article.summary_brief}</ReactMarkdown>}
                {article.summary_detailed && (
                  <>
                    <hr />
                    <ReactMarkdown>{article.summary_detailed}</ReactMarkdown>
                  </>
                )}
              </div>
            )}
          </div>
        )}

        {article.content
          ? <MarkdownArticle source={article.content} />
          : <div className="text-muted">暂无内容</div>
        }
      </article>

      <ReaderSettingsPanel
        fontSize={fontSize}
        fontFamily={fontFamily}
        bgTheme={bgTheme}
        onFontSize={props.onFontSize}
        onFontFamily={props.onFontFamily}
        onBgTheme={props.onBgTheme}
      />
    </div>
  )
}
```

- [ ] **Step 2: Type-check**

```bash
cd frontend
npx tsc --noEmit
```

Expected: clean.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/ReadingLayout.tsx
git commit -m "feat(frontend): ReadingLayout — full-screen reading with collapsible summary"
```

---

### Task 14: Wire reading mode into `ArticlePage` + CSS + keyboard

**Files:**
- Modify: `frontend/src/pages/ArticlePage.tsx`
- Modify: `frontend/src/index.css`

- [ ] **Step 1: Add reader theme + reading-layout CSS**

Append to `frontend/src/index.css`:

```css
/* ===== Reader-mode themes ===== */
body[data-reader-bg='default'] { --reader-bg:#fff;    --reader-fg:#1a1a1a; --reader-muted:#666;    --reader-code-bg:#f3f3f3; --reader-link:#0066cc; }
body[data-reader-bg='sepia']   { --reader-bg:#f5edd6; --reader-fg:#3a2f1a; --reader-muted:#7a6f55; --reader-code-bg:#ebe2c8; --reader-link:#5b3b00; }
body[data-reader-bg='green']   { --reader-bg:#cce8cf; --reader-fg:#1f2e1f; --reader-muted:#456b48; --reader-code-bg:#bcdcbf; --reader-link:#1a4d1a; }
body[data-reader-bg='gray']    { --reader-bg:#ebebeb; --reader-fg:#262626; --reader-muted:#666;    --reader-code-bg:#dcdcdc; --reader-link:#0066cc; }
body[data-reader-bg='dark']    { --reader-bg:#1a1a1a; --reader-fg:#d4d4d4; --reader-muted:#888;    --reader-code-bg:#262626; --reader-link:#7eb6ff; }

body.reading-mode-active {
  background: var(--reader-bg);
  color: var(--reader-fg);
}

/* When reading mode is active, dim app chrome (Layout nav, etc.). The Layout
   keeps rendering but recedes so the article dominates. If your Layout has
   a top nav class, you can extend the rule below. */
body.reading-mode-active > #root > .layout > nav,
body.reading-mode-active > #root > .layout > header,
body.reading-mode-active > #root > .layout > aside { display: none; }

.reading-layout { background: var(--reader-bg); color: var(--reader-fg); min-height: 100vh; }
.reading-toolbar { padding: 16px 24px; }
.reading-exit { background: transparent; border: 1px solid var(--reader-muted); color: var(--reader-fg); border-radius: 4px; padding: 4px 10px; cursor: pointer; }

.reading-article { max-width: 720px; margin: 0 auto; padding: 8px 24px 96px; line-height: 1.8; }
.reading-article .reading-title { color: var(--reader-fg); margin-bottom: 8px; }
.reading-article .reading-meta { color: var(--reader-muted); font-size: 13px; margin-bottom: 24px; }
.reading-article .reading-meta a { color: var(--reader-link); }

.reading-summary { border: 1px dashed var(--reader-muted); border-radius: 6px; padding: 8px 12px; margin-bottom: 24px; }
.reading-summary-toggle { background: transparent; border: none; color: var(--reader-fg); cursor: pointer; font-size: 13px; padding: 0; }
.reading-summary-body { margin-top: 8px; color: var(--reader-fg); }
.reading-summary-body hr { border: none; border-top: 1px solid var(--reader-muted); margin: 12px 0; }

.reading-article .markdown-body img { max-width: 100%; height: auto; border-radius: 4px; }
.reading-article .markdown-body pre { overflow-x: auto; padding: 12px; border-radius: 6px; background: var(--reader-code-bg); }
.reading-article .markdown-body code { background: var(--reader-code-bg); padding: 2px 4px; border-radius: 3px; }
.reading-article .markdown-body pre code { background: transparent; padding: 0; }
.reading-article .markdown-body blockquote { border-left: 3px solid var(--reader-muted); padding-left: 12px; color: var(--reader-muted); }
.reading-article .markdown-body table { border-collapse: collapse; margin: 12px 0; }
.reading-article .markdown-body th, .reading-article .markdown-body td { border: 1px solid var(--reader-muted); padding: 6px 10px; }
.reading-article .markdown-body a { color: var(--reader-link); }
```

> The "dim app chrome" rule above assumes Layout uses `<nav>`/`<header>`/`<aside>` semantic tags. If your `frontend/src/components/Layout.tsx` uses different selectors, adjust.

- [ ] **Step 2: Refactor `ArticlePage.tsx`**

In `frontend/src/pages/ArticlePage.tsx`, add imports near the top:

```tsx
import ReadingLayout from '../components/ReadingLayout'
import { useReaderSettings } from '../hooks/useReaderSettings'
```

Inside the `ArticlePage` component, wire up the hook (place near the other useState calls):

```tsx
const reader = useReaderSettings()
```

Add a "📖 阅读模式" button to the existing button row (next to the like/save row), e.g.:

```tsx
<button
  className="secondary"
  onClick={() => reader.setMode('reading')}
  title="进入阅读模式 (r)"
  style={{ fontSize: 13 }}
>
  📖 阅读模式
</button>
```

Add the keyboard `r` shortcut in the existing keyboard `useEffect`. Find the `handler` function (around line 104) and extend the conditional chain:

```tsx
} else if (e.key === 'r') {
  reader.toggleMode()
}
```

Add `reader` to the effect dependency list at the bottom of that `useEffect`.

Finally, branch on `reader.mode`. Right before the existing `return (...)` JSX of the page, insert:

```tsx
if (reader.mode === 'reading') {
  return (
    <ReadingLayout
      article={{
        title: article.title,
        url: article.url,
        published_at: article.published_at,
        word_count: article.word_count,
        reading_minutes: article.reading_minutes,
        content: article.content,
        summary_brief: article.summary_brief,
        summary_detailed: article.summary_detailed,
      }}
      fontSize={reader.fontSize}
      fontFamily={reader.fontFamily}
      bgTheme={reader.bgTheme}
      onExit={() => reader.setMode('normal')}
      onFontSize={reader.setFontSize}
      onFontFamily={reader.setFontFamily}
      onBgTheme={reader.setBgTheme}
    />
  )
}
```

- [ ] **Step 3: Type-check**

```bash
cd frontend
npx tsc --noEmit
```

Expected: clean.

- [ ] **Step 4: Build and verify in browser**

```bash
docker-compose up -d --build frontend
```

Manually exercise:
1. Open an article. Click "📖 阅读模式". Page switches to ReadingLayout, app chrome hidden, content centered.
2. Click Aa. Adjust font size A−/A+; verify it changes article body (12 lower bound, 24 upper bound).
3. Toggle Sans/Serif. Verify visible difference (Serif renders with `Source Han Serif SC`/`Songti SC` if available).
4. Click each of the 5 background swatches. Verify the entire viewport background changes.
5. Click "▶ AI 摘要" — summary expands; click again — collapses.
6. Click "← 退出阅读模式". Page returns to normal mode.
7. Press `r` while on the article — toggles in/out of reading mode.
8. Press `Esc` while in reading mode — Settings panel closes if open; otherwise (if you want full-page Esc behavior, that's the existing handler — confirm it doesn't conflict).
9. Open the article in a second browser tab — settings sync via `storage` event.
10. Refresh the page — last mode + last theme + last fontSize persist (per the "remember last mode" decision).

> If the app chrome (top nav) doesn't hide because Layout uses different selectors, inspect the rendered DOM and adjust the `body.reading-mode-active > ...` selector in `index.css`. As a fallback, you can wrap the chrome in a class and target that.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/pages/ArticlePage.tsx frontend/src/index.css
git commit -m "feat(frontend): reading-mode toggle + theme CSS + r shortcut

Wires useReaderSettings into ArticlePage; renders ReadingLayout when
mode=reading; adds 5 background CSS variable themes + reading-layout
typography; r key toggles in/out of reading mode. App chrome (nav/
header/aside) hides while reading mode is active."
```

---

## Phase 6 — End-to-end verification

### Task 15: Run backfill and final smoke test

**Files:** none (operational)

- [ ] **Step 1: Run a small backfill**

Pick one feed id with ~10 articles for a real end-to-end check:

```bash
cd backend
go run ./cmd/backfill_content --feed-id <ID> --qps 1
```

Open one of those articles in the UI (normal mode):
- Body renders as markdown with images, code blocks, tables (if present in source).
- Image requests in DevTools Network tab go to `/api/proxy/image?url=…`.

- [ ] **Step 2: Reading-mode regression sweep**

In an article with rich content:
- Toggle reading mode via button.
- Cycle all 5 themes; verify code blocks stay legible on each (especially `dark`).
- Set font to 24, then 12, then back to 16.
- Switch Sans ↔ Serif.
- Reload the page and confirm preferences stick.

- [ ] **Step 3: Run full backend test suite**

```bash
cd backend
go test ./...
```

Expected: all tests pass.

- [ ] **Step 4: Commit nothing (operational checkpoint)**

If any issues surfaced, file follow-up tasks. If clean, this completes the plan.

---

## Self-Review Notes

Spec coverage check:
- §3 decisions 1–9: each landed in tasks (markdown=2, backfill=7, proxy=4–6, gfm/highlight=8–9, mode=11–14, content=13, themes=14, font=11–12, persistence=11)
- §5.2 content.go refactor: Tasks 2–3
- §5.3 proxy.go: Tasks 4–6
- §5.4 backfill cmd: Task 7
- §5.5 backend tests: included inline in 2/3/4/5
- §6.2 MarkdownArticle: Task 9
- §6.3 useReaderSettings: Task 11
- §6.4 ReaderSettingsPanel: Task 12
- §6.5 ArticlePage refactor: Tasks 10 + 14
- §6.6 index.css: Task 14
- §6.7 keyboard: Task 14
- §6.8 manual test checklist: Task 14 step 4 + Task 15
- §7 deploy order: tasks ordered backend-first (1–7), frontend after (8–14), backfill last (15)

Type/name consistency: `ReaderSettings` shape, `bgTheme` keys, `mode` values, `fontSize` clamp 12–24 — all consistent across hook/panel/layout.

No placeholders / TBDs found.
