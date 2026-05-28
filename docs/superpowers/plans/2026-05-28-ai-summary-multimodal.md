# AI 总结多模态支持 — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make AI summarization see article images for image-heavy posts (heuristic in worker backfill, force-on for frontend regen), with a TTL-cleaned local image cache and uniform soft-fallback to the existing text-only path on any failure.

**Architecture:** Add a new `internal/imagefetch` package that downloads + resizes + caches images locally to `/backups/ai_summary_cache/<id>/<idx>.jpg`. Extract the SSRF-guarded HTTP client out of `internal/api/proxy.go` into a shared `internal/httpx`. Extend `internal/ai` so `chatMessage.Content` can carry either `string` (legacy) or `[]contentBlock` (vision OpenAI schema), plus two new entry points `SummarizeWithImages` + `SummarizeWithImagesStream` that use a separately-configured vision model. Worker routes via a heuristic; HTTP layer routes via a new `force_vision=1` query param.

**Tech Stack:** Go 1.24, `golang.org/x/image/draw` (BiLinear resize), stdlib `image/png` + `image/jpeg` + `image/gif`, Gin (existing HTTP framework), table-driven tests with `httptest.Server` + `t.TempDir()`.

**Spec reference:** `docs/superpowers/specs/2026-05-28-ai-summary-multimodal-design.md`

**Branch:** `feature/ai-summary-multimodal` (PR #34 open).

---

## File Structure

| Path | Responsibility | Status |
|---|---|---|
| `backend/internal/config/config.go` | Add `AIConfig.Vision` struct + env wiring | **Modify** |
| `backend/internal/httpx/client.go` | Shared SSRF-guarded `*http.Client` + `ValidateURL` (extracted from `proxy.go`) | **Create** |
| `backend/internal/httpx/client_test.go` | Tests for `ValidateURL` (parity with original `validateImageURL` behavior) | **Create** |
| `backend/internal/api/proxy.go` | Switch to `httpx.NewClient` + `httpx.ValidateURL`; keep `Handle` shape | **Modify** |
| `backend/internal/rss/content.go` | Export `IsAvatarImageURL(src, alt string) bool` (URL+alt variant of `isAvatarImg`) | **Modify** |
| `backend/internal/rss/avatar_url_test.go` | Tests for `IsAvatarImageURL` | **Create** |
| `backend/internal/ai/imageurls.go` | `ExtractImageURLs(md string) []string`, `CountTextRunes(md string) int` (markdown helpers) | **Create** |
| `backend/internal/ai/imageurls_test.go` | Tests for the above | **Create** |
| `backend/internal/ai/policy.go` | `ShouldUseVisionAuto(article model.Article, cfg config.VisionConfig) bool` | **Create** |
| `backend/internal/ai/policy_test.go` | Heuristic table test | **Create** |
| `backend/internal/imagefetch/imagefetch.go` | `Config`, `FetchAndStore`, `CleanupExpired` | **Create** |
| `backend/internal/imagefetch/imagefetch_test.go` | Table tests with `httptest.Server` + `t.TempDir()` | **Create** |
| `backend/internal/ai/summarizer.go` | `chatMessage.Content interface{}`, `contentBlock` types, `visionModel` field, `SummarizeWithImages*` entry points + fallback wiring | **Modify** |
| `backend/internal/ai/summarizer_vision_test.go` | Mocked-server tests asserting request shape + fallback on vision failure | **Create** |
| `backend/internal/service/summarizer.go` | `SummarizeWithImages*` wrapper methods | **Modify** |
| `backend/internal/api/article.go` | `force_vision=1` parsing + routing in `GenerateSummary` and `streamSummary` | **Modify** |
| `backend/cmd/worker/main.go` | Construct summarizer with vision model; route `backfillSummaries`; per-cycle `CleanupExpired` call | **Modify** |
| `frontend/src/api/client.ts` | Add `forceVision?: boolean` to `generateSummaryStream` opts; emit query param | **Modify** |
| `frontend/src/pages/ArticlePage.tsx` | Pass `forceVision: true` on user regen click | **Modify** |

---

## Pre-flight

### Task 0: Sanity check

**Files:** none

- [ ] **Step 0.1: Verify branch**

Run: `git branch --show-current`
Expected: `feature/ai-summary-multimodal`

- [ ] **Step 0.2: Verify spec exists**

Run: `ls docs/superpowers/specs/2026-05-28-ai-summary-multimodal-design.md`
Expected: file exists

- [ ] **Step 0.3: Baseline backend tests + build pass**

Run: `cd backend && go build ./... && go test ./...`
Expected: PASS (no test failures, no compile errors). This is the baseline — every later task must keep this clean.

- [ ] **Step 0.4: Baseline frontend type-check pass**

Run: `cd frontend && npx tsc --noEmit`
Expected: PASS

---

## Task 1: Config — `AIConfig.Vision`

**Files:**
- Modify: `backend/internal/config/config.go`

Add the new env wiring as a separate top-level `AI` field on `Config`. Eight new env vars; default values match the spec table.

- [ ] **Step 1.1: Extend Config struct + Load()**

Replace the file `backend/internal/config/config.go` (full rewrite — file is small):

```go
package config

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	Server   ServerConfig
	Database DatabaseConfig
	Claude   ClaudeConfig
	AI       AIConfig
	Auth     AuthConfig
	JWT      JWTConfig
	RSSHub   RSSHubConfig
	Backup   BackupConfig
}

type BackupConfig struct {
	Dir string // host-mounted; survives container removal
}

type ServerConfig struct {
	Port string
}

type DatabaseConfig struct {
	Host     string
	Port     string
	User     string
	Password string
	DBName   string
	SSLMode  string
}

type ClaudeConfig struct {
	APIKey  string
	BaseURL string
}

type AIConfig struct {
	Vision VisionConfig
}

// VisionConfig groups everything the vision-summary path needs.
// Defaults are tuned for z.ai's glm-4v-plus, 6-image cap, 1024 longest-side,
// 4 MB base64 payload budget, 24h cache TTL.
type VisionConfig struct {
	Model           string        // chat completions "model" field for vision calls
	MaxImages       int           // hard cap per article
	MaxLongSide     int           // resize threshold; px
	PayloadBudgetMB int           // base64 budget; drops tail images on overflow
	MinImages       int           // auto-trigger image-count floor
	MaxTextChars    int           // auto-trigger text-length ceiling
	CacheDir        string        // temp cache root
	CacheTTL        time.Duration // cache file age limit
}

type AuthConfig struct {
	Password string
}

type JWTConfig struct {
	Secret string
}

type RSSHubConfig struct {
	BaseURL string
}

func Load() *Config {
	return &Config{
		Server: ServerConfig{
			Port: getEnv("SERVER_PORT", "8080"),
		},
		Database: DatabaseConfig{
			Host:     getEnv("DB_HOST", "localhost"),
			Port:     getEnv("DB_PORT", "5432"),
			User:     getEnv("DB_USER", "postgres"),
			Password: getEnv("DB_PASSWORD", "postgres"),
			DBName:   getEnv("DB_NAME", "rsspal"),
			SSLMode:  getEnv("DB_SSLMODE", "disable"),
		},
		Claude: ClaudeConfig{
			APIKey:  getEnv("CLAUDE_API_KEY", ""),
			BaseURL: getEnv("CLAUDE_BASE_URL", "https://api.anthropic.com"),
		},
		AI: AIConfig{
			Vision: VisionConfig{
				Model:           getEnv("AI_VISION_MODEL", "glm-4v-plus"),
				MaxImages:       getEnvInt("AI_VISION_MAX_IMAGES", 6),
				MaxLongSide:     getEnvInt("AI_VISION_MAX_LONG_SIDE", 1024),
				PayloadBudgetMB: getEnvInt("AI_VISION_PAYLOAD_BUDGET_MB", 4),
				MinImages:       getEnvInt("AI_VISION_MIN_IMAGES", 3),
				MaxTextChars:    getEnvInt("AI_VISION_MAX_TEXT_CHARS", 2000),
				CacheDir:        getEnv("AI_VISION_CACHE_DIR", "/backups/ai_summary_cache"),
				CacheTTL:        time.Duration(getEnvInt("AI_VISION_CACHE_TTL_HOURS", 24)) * time.Hour,
			},
		},
		Auth: AuthConfig{
			Password: getEnv("AUTH_PASSWORD", "admin"),
		},
		JWT: JWTConfig{
			Secret: getEnv("JWT_SECRET", "rss-pal-default-secret-change-me"),
		},
		RSSHub: RSSHubConfig{
			BaseURL: getEnv("RSSHUB_BASE_URL", "http://rsshub:1200"),
		},
		Backup: BackupConfig{
			Dir: getEnv("BACKUP_DIR", "/backups"),
		},
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	v := os.Getenv(key)
	if v == "" {
		return defaultValue
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return defaultValue
	}
	return n
}
```

- [ ] **Step 1.2: Build**

Run: `cd backend && go build ./...`
Expected: PASS (no compile errors). No existing call site reads `cfg.AI`, so adding the field is backward-compatible.

- [ ] **Step 1.3: Commit**

```bash
git add backend/internal/config/config.go
git commit -m "feat(config): add AIConfig.Vision with env wiring"
```

---

## Task 2: Extract SSRF-guarded HTTP client into `internal/httpx`

**Files:**
- Create: `backend/internal/httpx/client.go`
- Create: `backend/internal/httpx/client_test.go`
- Modify: `backend/internal/api/proxy.go`

The existing `proxy.go` has both the proxy handler (Gin-specific) and the SSRF guard + HTTP client construction (reusable). Extract the latter so `imagefetch` can use it without depending on the `api` package.

- [ ] **Step 2.1: Write the failing test for the new package**

Create `backend/internal/httpx/client_test.go`:

```go
package httpx

import (
	"net"
	"strings"
	"testing"
)

func TestValidateURL(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		wantErr string // substring; "" means expect success
	}{
		{"http public host", "http://example.com/x.jpg", ""},
		{"https public host", "https://example.com/x.jpg", ""},
		{"empty", "", "empty url"},
		{"ftp scheme", "ftp://example.com/x", "unsupported scheme"},
		{"no host", "http:///x", "missing host"},
		{"loopback ipv4", "http://127.0.0.1/x", "blocked address"},
		{"rfc1918", "http://10.0.0.5/x", "blocked address"},
		{"link-local", "http://169.254.169.254/latest/meta-data/", "blocked address"},
		{"loopback ipv6", "http://[::1]/x", "blocked address"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ValidateURL(tc.input)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("want ok, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("want err containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("want err containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestIsBlockedIP(t *testing.T) {
	cases := []struct {
		ip   string
		want bool
	}{
		{"127.0.0.1", true},
		{"10.1.2.3", true},
		{"172.20.0.1", true},
		{"192.168.1.1", true},
		{"169.254.169.254", true},
		{"::1", true},
		{"fc00::1", true},
		{"fe80::1", true},
		{"8.8.8.8", false},
		{"2606:4700:4700::1111", false},
	}
	for _, tc := range cases {
		t.Run(tc.ip, func(t *testing.T) {
			got := isBlockedIP(net.ParseIP(tc.ip))
			if got != tc.want {
				t.Fatalf("ip=%s want=%v got=%v", tc.ip, tc.want, got)
			}
		})
	}
}
```

- [ ] **Step 2.2: Run test — expect compile failure**

Run: `cd backend && go test ./internal/httpx/`
Expected: FAIL (`no Go files`)

- [ ] **Step 2.3: Create the package**

Create `backend/internal/httpx/client.go`:

```go
// Package httpx provides an SSRF-guarded HTTP client + URL validator shared
// by image proxy + imagefetch (vision summary input). All callers that fetch
// arbitrary external image URLs must go through ValidateURL + the client
// returned by NewClient so we don't accidentally serve cloud metadata or
// internal services to attackers.
package httpx

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"
)

// blockedCIDRs is the IPv4/IPv6 ranges we refuse to talk to: loopback,
// RFC1918, link-local (covers AWS/GCP/Azure metadata 169.254.169.254),
// IPv6 ULA, IPv6 link-local.
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

// ValidateURL parses raw, requires http/https, and rejects hosts whose
// resolved IPs land in any blocked range. Returns the parsed URL on success.
func ValidateURL(raw string) (*url.URL, error) {
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

// UserAgent is the User-Agent string used by both the image proxy and the
// imagefetch downloader. Mirrors a Chrome desktop UA so hotlink-protected
// CDNs (WeChat, Zhihu) don't 403.
const UserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

// NewClient returns a client that re-validates redirect targets against the
// SSRF guard. timeout caps the full request-to-response duration.
func NewClient(timeout time.Duration) *http.Client {
	c := &http.Client{Timeout: timeout}
	c.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("too many redirects")
		}
		if _, err := ValidateURL(req.URL.String()); err != nil {
			return fmt.Errorf("redirect rejected: %w", err)
		}
		return nil
	}
	return c
}
```

- [ ] **Step 2.4: Run httpx tests — expect PASS**

Run: `cd backend && go test ./internal/httpx/ -v`
Expected: PASS (all subtests).

- [ ] **Step 2.5: Refactor proxy.go to use httpx**

Open `backend/internal/api/proxy.go`. Delete the old `blockedCIDRs`, `isBlockedIP`, `validateImageURL`, `proxyUserAgent` declarations and the inline `CheckRedirect` body — replace the file body (keep the package header) with this consolidated version:

```go
package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/bytedance/rss-pal/internal/httpx"
)

const (
	proxyMaxBytes = 10 * 1024 * 1024 // 10MB
	proxyTimeout  = 30 * time.Second
)

// ImageProxy serves remote images through this server. Constructed via
// NewImageProxy for production use; tests instantiate the struct directly
// with custom dependencies.
type ImageProxy struct {
	Validate func(rawURL string) (*url.URL, error)
	Client   *http.Client
}

// NewImageProxy returns a production-ready proxy: strict SSRF validation,
// 30s timeout, 10MB cap, and redirect re-validation against the SSRF guard.
func NewImageProxy() *ImageProxy {
	return &ImageProxy{
		Validate: httpx.ValidateURL,
		Client:   httpx.NewClient(proxyTimeout),
	}
}

// Handle is the gin handler.
func (p *ImageProxy) Handle(c *gin.Context) {
	raw := c.Query("url")
	target, err := p.Validate(raw)
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
	req.Header.Set("User-Agent", httpx.UserAgent)

	resp, err := p.Client.Do(req)
	if err != nil {
		c.String(http.StatusBadGateway, "upstream: %s", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		c.String(http.StatusBadGateway, "upstream status %d", resp.StatusCode)
		return
	}

	// Content-Length precheck: reject if upstream declares oversize body.
	if cl := resp.ContentLength; cl > proxyMaxBytes {
		c.String(http.StatusBadGateway, "upstream too large: %d bytes", cl)
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
	c.Header("Cache-Control", "public, max-age=604800, immutable")
	c.Status(http.StatusOK)
	_, _ = io.Copy(c.Writer, io.LimitReader(resp.Body, proxyMaxBytes))
}

// Silence the unused `errors` import if Go's import-pruner is unhappy.
var _ = errors.New
var _ = fmt.Sprintf
```

(The trailing `var _ =` lines are removed in the commit if `gofmt`/`goimports` strips them — they exist only to suppress any transient "imported and not used" complaint while you reshape; running `goimports -w` on the file before committing should clean these out automatically.)

- [ ] **Step 2.6: Run proxy.go formatting cleanup**

Run: `cd backend && goimports -w internal/api/proxy.go && gofmt -w internal/api/proxy.go`
Expected: clean exit. If `goimports` is missing, `go install golang.org/x/tools/cmd/goimports@latest` first.

Inspect the final file: only `context`, `io`, `net/http`, `net/url`, `strings`, `time`, `gin`, `httpx` should remain in the import list.

- [ ] **Step 2.7: Build + run all backend tests**

Run: `cd backend && go build ./... && go test ./...`
Expected: PASS. Any existing test that referenced `validateImageURL` would have broken; the refactor removed it (now `httpx.ValidateURL`) — search and fix if so:

Run: `grep -rn 'validateImageURL\|blockedCIDRs\|isBlockedIP\|proxyUserAgent' backend/internal/api/`
Expected: 0 matches inside `proxy.go` (we removed those symbols). If any other file still uses them, those usages need updating (the proxy.go was the only home — extremely unlikely).

- [ ] **Step 2.8: Commit**

```bash
git add backend/internal/httpx/ backend/internal/api/proxy.go
git commit -m "refactor(httpx): extract SSRF-guarded HTTP client out of proxy.go"
```

---

## Task 3: `rss.IsAvatarImageURL` helper

**Files:**
- Modify: `backend/internal/rss/content.go`
- Create: `backend/internal/rss/avatar_url_test.go`

The existing `isAvatarImg` works on a `*goquery.Selection` (needs a DOM node). The vision path only has URL+alt strings, so expose a thin URL-string version reusing the same keyword lists.

- [ ] **Step 3.1: Write the failing test**

Create `backend/internal/rss/avatar_url_test.go`:

```go
package rss

import "testing"

func TestIsAvatarImageURL(t *testing.T) {
	cases := []struct {
		name string
		src  string
		alt  string
		want bool
	}{
		{"gravatar host", "https://www.gravatar.com/avatar/abc123", "", true},
		{"avatar in path", "https://cdn.example.com/avatars/u123.png", "", true},
		{"avatar substring path", "https://example.com/some/avatar/x.png", "", true},
		{"alt avatar keyword", "https://example.com/x.png", "author avatar", true},
		{"alt headshot", "https://example.com/x.png", "headshot of jane", true},
		{"normal image, plain alt", "https://example.com/cat.png", "a cat", false},
		{"normal image, empty alt", "https://example.com/cat.png", "", false},
		{"case insensitive — uppercase Avatar in alt", "https://example.com/x.png", "AVATAR", true},
		{"case insensitive — capitalized GRAVATAR in URL", "https://www.GRAVATAR.com/avatar/abc", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := IsAvatarImageURL(tc.src, tc.alt)
			if got != tc.want {
				t.Fatalf("src=%q alt=%q want=%v got=%v", tc.src, tc.alt, tc.want, got)
			}
		})
	}
}
```

- [ ] **Step 3.2: Run — expect compile fail**

Run: `cd backend && go test ./internal/rss/ -run TestIsAvatarImageURL`
Expected: FAIL (`undefined: IsAvatarImageURL`).

- [ ] **Step 3.3: Implement**

Open `backend/internal/rss/content.go`. After the existing `isAvatarImg` function (around line 447), add:

```go
// IsAvatarImageURL is the markdown-time companion to isAvatarImg: it inspects
// just the src URL and the alt text (the only signals that survive HTML →
// markdown round-trip). Shared keyword lists with the DOM-based detector.
func IsAvatarImageURL(src, alt string) bool {
	srcLower := strings.ToLower(src)
	for _, kw := range avatarURLKeywords {
		if strings.Contains(srcLower, kw) {
			return true
		}
	}
	altLower := strings.ToLower(alt)
	if altLower == "" {
		return false
	}
	for _, kw := range avatarAttrKeywords {
		if strings.Contains(altLower, kw) {
			return true
		}
	}
	return false
}
```

- [ ] **Step 3.4: Run the test — expect PASS**

Run: `cd backend && go test ./internal/rss/ -run TestIsAvatarImageURL -v`
Expected: PASS.

- [ ] **Step 3.5: Build + full rss test pass**

Run: `cd backend && go test ./internal/rss/`
Expected: PASS (existing tests untouched).

- [ ] **Step 3.6: Commit**

```bash
git add backend/internal/rss/content.go backend/internal/rss/avatar_url_test.go
git commit -m "feat(rss): IsAvatarImageURL — url+alt avatar detector for markdown callers"
```

---

## Task 4: Markdown image-URL extraction + text-rune count

**Files:**
- Create: `backend/internal/ai/imageurls.go`
- Create: `backend/internal/ai/imageurls_test.go`

These two pure helpers feed both the heuristic and the worker-side URL selection.

- [ ] **Step 4.1: Write the failing test**

Create `backend/internal/ai/imageurls_test.go`:

```go
package ai

import (
	"reflect"
	"testing"
)

func TestExtractImageURLs(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"empty", "", nil},
		{"no images", "just some text", nil},
		{"single", "before ![](https://a.com/x.png) after", []string{"https://a.com/x.png"}},
		{"with alt", "![cat](https://a.com/x.png)", []string{"https://a.com/x.png"}},
		{"preserves order, dedupes nothing", "![](https://a.com/1.png) ![](https://a.com/2.png) ![](https://a.com/1.png)",
			[]string{"https://a.com/1.png", "https://a.com/2.png", "https://a.com/1.png"}},
		{"local article-images URL", "![](/api/articles/42/images/0.png)", []string{"/api/articles/42/images/0.png"}},
		{"url with parens encoded", "![](https://a.com/x.png?q=1&x=y)", []string{"https://a.com/x.png?q=1&x=y"}},
		{"newlines around", "intro\n\n![](https://a.com/x.png)\n\nmore", []string{"https://a.com/x.png"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ExtractImageURLs(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("want %#v got %#v", tc.want, got)
			}
		})
	}
}

func TestCountTextRunes(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want int
	}{
		{"empty", "", 0},
		{"plain ascii", "hello world", 11},
		{"chinese", "你好世界", 4},
		{"strips image alts and urls", "你好![alt](https://a.com/x.png)再见", 4 + 2}, // "你好" + "再见"
		{"only images", "![](https://a.com/1.png)![](https://b.com/2.png)", 0},
		{"newlines kept as runes", "line1\nline2", 11},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := CountTextRunes(tc.in)
			if got != tc.want {
				t.Fatalf("want=%d got=%d (input %q)", tc.want, got, tc.in)
			}
		})
	}
}
```

- [ ] **Step 4.2: Run — expect compile fail**

Run: `cd backend && go test ./internal/ai/ -run 'TestExtractImageURLs|TestCountTextRunes'`
Expected: FAIL (`undefined: ExtractImageURLs`).

- [ ] **Step 4.3: Implement**

Create `backend/internal/ai/imageurls.go`:

```go
package ai

import (
	"regexp"
	"unicode/utf8"
)

// imgRE matches markdown image syntax `![alt](url)` and captures the URL.
// Intentionally simple — does not handle parenthesised URLs (rare; tolerable
// to miss). Mirrors the lightweight style of util/imageAlt.flattenImageAltBlankLines
// rather than pulling in a full markdown parser.
var imgRE = regexp.MustCompile(`!\[[^\]]*\]\(([^)]+)\)`)

// ExtractImageURLs returns the URLs of `![](...)` markdown images in source
// order. Returns nil (not []string{}) when no images are present, so the
// caller can treat zero-result as "skip vision path" with a simple len()==0.
func ExtractImageURLs(md string) []string {
	matches := imgRE.FindAllStringSubmatch(md, -1)
	if len(matches) == 0 {
		return nil
	}
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		if len(m) >= 2 && m[1] != "" {
			out = append(out, m[1])
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// CountTextRunes returns the rune count of md after stripping all markdown
// image tags. Used to apply the AI_VISION_MAX_TEXT_CHARS heuristic without
// counting alt text or URL chars as "text".
func CountTextRunes(md string) int {
	stripped := imgRE.ReplaceAllString(md, "")
	return utf8.RuneCountInString(stripped)
}
```

- [ ] **Step 4.4: Run — expect PASS**

Run: `cd backend && go test ./internal/ai/ -run 'TestExtractImageURLs|TestCountTextRunes' -v`
Expected: PASS.

- [ ] **Step 4.5: Commit**

```bash
git add backend/internal/ai/imageurls.go backend/internal/ai/imageurls_test.go
git commit -m "feat(ai): markdown image-URL extractor + text-rune counter"
```

---

## Task 5: Vision heuristic — `ShouldUseVisionAuto`

**Files:**
- Create: `backend/internal/ai/policy.go`
- Create: `backend/internal/ai/policy_test.go`

- [ ] **Step 5.1: Write the failing test**

Create `backend/internal/ai/policy_test.go`:

```go
package ai

import (
	"testing"

	"github.com/bytedance/rss-pal/internal/config"
)

func TestShouldUseVisionAuto(t *testing.T) {
	defaultCfg := config.VisionConfig{
		MinImages:    3,
		MaxTextChars: 2000,
	}
	cases := []struct {
		name    string
		content string
		cfg     config.VisionConfig
		want    bool
	}{
		{"empty content", "", defaultCfg, false},
		{"3 images, zero text", "![](https://a.com/1.png)![](https://a.com/2.png)![](https://a.com/3.png)", defaultCfg, true},
		{"2 images (below MinImages)", "![](https://a.com/1.png)![](https://a.com/2.png)", defaultCfg, false},
		{"3 images but too much text",
			"![](https://a.com/1.png)![](https://a.com/2.png)![](https://a.com/3.png)" +
				strings.Repeat("text ", 500), // 2500 chars of text
			defaultCfg, false},
		{"5 images, short text", "![](a)![](b)![](c)![](d)![](e)\n短正文", defaultCfg, true},
		{"text exactly at the boundary still triggers (<2000 chars)",
			"![](a)![](b)![](c)" + strings.Repeat("x", 1999), defaultCfg, true},
		{"text just over boundary fails",
			"![](a)![](b)![](c)" + strings.Repeat("x", 2000), defaultCfg, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ShouldUseVisionAuto(tc.content, tc.cfg)
			if got != tc.want {
				t.Fatalf("want=%v got=%v", tc.want, got)
			}
		})
	}
}
```

Add the `strings` import to the top of `policy_test.go`:

```go
import (
	"strings"
	"testing"

	"github.com/bytedance/rss-pal/internal/config"
)
```

- [ ] **Step 5.2: Run — expect compile fail**

Run: `cd backend && go test ./internal/ai/ -run TestShouldUseVisionAuto`
Expected: FAIL (`undefined: ShouldUseVisionAuto`).

- [ ] **Step 5.3: Implement**

Create `backend/internal/ai/policy.go`:

```go
package ai

import "github.com/bytedance/rss-pal/internal/config"

// ShouldUseVisionAuto reports whether the worker-backfill path should route
// an article through the vision summarizer instead of the plain text path.
// The heuristic is intentionally simple: enough images AND short enough text.
// Image-heavy articles like a sequence of `![](image)` with no body get
// vision; text-heavy articles with the occasional inline image stay on text.
//
// Boundary: text rune count must be STRICTLY LESS THAN MaxTextChars. So
// MaxTextChars=2000 means 1999 runes triggers, 2000 does not. This matches
// the spec table.
func ShouldUseVisionAuto(content string, cfg config.VisionConfig) bool {
	if content == "" {
		return false
	}
	urls := ExtractImageURLs(content)
	if len(urls) < cfg.MinImages {
		return false
	}
	if CountTextRunes(content) >= cfg.MaxTextChars {
		return false
	}
	return true
}
```

- [ ] **Step 5.4: Run — expect PASS**

Run: `cd backend && go test ./internal/ai/ -run TestShouldUseVisionAuto -v`
Expected: PASS.

- [ ] **Step 5.5: Commit**

```bash
git add backend/internal/ai/policy.go backend/internal/ai/policy_test.go
git commit -m "feat(ai): ShouldUseVisionAuto heuristic for worker backfill routing"
```

---

## Task 6: `imagefetch.FetchAndStore`

**Files:**
- Create: `backend/internal/imagefetch/imagefetch.go`
- Create: `backend/internal/imagefetch/imagefetch_test.go`

- [ ] **Step 6.1: Write the failing test**

Create `backend/internal/imagefetch/imagefetch_test.go`:

```go
package imagefetch

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// solidPNG returns a w×h opaque-red PNG.
func solidPNG(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	red := color.RGBA{R: 255, A: 255}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, red)
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

// solidJPEG returns a w×h opaque-red JPEG.
func solidJPEG(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	red := color.RGBA{R: 255, A: 255}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, red)
		}
	}
	var buf bytes.Buffer
	_ = jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90})
	return buf.Bytes()
}

func TestFetchAndStore_basic(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(solidPNG(200, 100))
	}))
	t.Cleanup(srv.Close)

	cfg := Config{
		Dir:                   t.TempDir(),
		LocalArticleImagesDir: t.TempDir(),
		MaxLongSide:           1024,
		TTL:                   24 * time.Hour,
	}
	paths, err := FetchAndStore(context.Background(), 42, []string{srv.URL + "/a.png"}, cfg)
	if err != nil {
		t.Fatalf("FetchAndStore: %v", err)
	}
	if len(paths) != 1 {
		t.Fatalf("want 1 path, got %d", len(paths))
	}
	if !strings.HasSuffix(paths[0], "/42/0.jpg") {
		t.Errorf("expected path ending /42/0.jpg, got %s", paths[0])
	}
	st, err := os.Stat(paths[0])
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if st.Size() == 0 {
		t.Errorf("expected non-zero file")
	}
}

func TestFetchAndStore_resizesOversize(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(solidPNG(3000, 1500))
	}))
	t.Cleanup(srv.Close)

	cfg := Config{
		Dir:         t.TempDir(),
		MaxLongSide: 1024,
		TTL:         24 * time.Hour,
	}
	paths, err := FetchAndStore(context.Background(), 7, []string{srv.URL + "/x.png"}, cfg)
	if err != nil || len(paths) != 1 {
		t.Fatalf("FetchAndStore: paths=%v err=%v", paths, err)
	}
	f, err := os.Open(paths[0])
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	b := img.Bounds()
	if b.Dx() > 1024 || b.Dy() > 1024 {
		t.Errorf("expected long side <= 1024, got %dx%d", b.Dx(), b.Dy())
	}
	if b.Dx() != 1024 {
		t.Errorf("expected width=1024 (3000 was longest), got %d", b.Dx())
	}
}

func TestFetchAndStore_cacheHitRefreshesMtime(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(solidPNG(50, 50))
	}))
	t.Cleanup(srv.Close)

	cfg := Config{Dir: t.TempDir(), MaxLongSide: 1024, TTL: 24 * time.Hour}
	first, err := FetchAndStore(context.Background(), 1, []string{srv.URL + "/a.png"}, cfg)
	if err != nil || len(first) != 1 {
		t.Fatalf("first: paths=%v err=%v", first, err)
	}

	// Backdate mtime by 1 hour.
	old := time.Now().Add(-1 * time.Hour)
	if err := os.Chtimes(first[0], old, old); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	second, err := FetchAndStore(context.Background(), 1, []string{srv.URL + "/a.png"}, cfg)
	if err != nil || len(second) != 1 || second[0] != first[0] {
		t.Fatalf("second: paths=%v err=%v", second, err)
	}
	st, err := os.Stat(second[0])
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if !st.ModTime().After(old) {
		t.Errorf("expected mtime refresh; mtime=%v old=%v", st.ModTime(), old)
	}
}

func TestFetchAndStore_localArticleImagesURL(t *testing.T) {
	// Pre-seed an on-disk PDF clip image as if from a previous extraction.
	localDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(localDir, "9"), 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(localDir, "9", "0.png")
	if err := os.WriteFile(target, solidPNG(20, 20), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := Config{
		Dir:                   t.TempDir(),
		LocalArticleImagesDir: localDir,
		MaxLongSide:           1024,
		TTL:                   24 * time.Hour,
	}
	paths, err := FetchAndStore(context.Background(), 9, []string{"/api/articles/9/images/0.png"}, cfg)
	if err != nil {
		t.Fatalf("FetchAndStore: %v", err)
	}
	if len(paths) != 1 {
		t.Fatalf("want 1 path, got %d", len(paths))
	}
	if paths[0] != target {
		t.Errorf("want local path %s, got %s", target, paths[0])
	}
	// Verify the cache dir was NOT used.
	cacheEntries, _ := os.ReadDir(cfg.Dir)
	if len(cacheEntries) != 0 {
		t.Errorf("expected cache dir untouched, got entries: %v", cacheEntries)
	}
}

func TestFetchAndStore_skipsFailures(t *testing.T) {
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(solidJPEG(40, 40))
	}))
	t.Cleanup(good.Close)

	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(bad.Close)

	corrupt := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("not actually a png"))
	}))
	t.Cleanup(corrupt.Close)

	cfg := Config{Dir: t.TempDir(), MaxLongSide: 1024, TTL: 24 * time.Hour}
	paths, err := FetchAndStore(context.Background(), 5, []string{
		good.URL + "/a.jpg",
		bad.URL + "/b.jpg",
		corrupt.URL + "/c.png",
		good.URL + "/d.jpg",
	}, cfg)
	if err != nil {
		t.Fatalf("FetchAndStore: %v", err)
	}
	if len(paths) != 2 {
		t.Fatalf("expected 2 successful paths (indices 0 and 3 of good server), got %d: %v", len(paths), paths)
	}
}
```

- [ ] **Step 6.2: Run — expect compile fail**

Run: `cd backend && go test ./internal/imagefetch/`
Expected: FAIL (`no Go files`).

- [ ] **Step 6.3: Add the `golang.org/x/image` dependency**

Run: `cd backend && go get golang.org/x/image/draw`
Expected: succeeds, updates `go.mod` and `go.sum`. (`golang.org/x/image` is a stdlib-adjacent subrepo; no licensing issues.)

- [ ] **Step 6.4: Implement `FetchAndStore`**

Create `backend/internal/imagefetch/imagefetch.go`:

```go
// Package imagefetch downloads article images for the AI vision summary path
// to a local TTL-cleaned cache. The downloaded files live under
// cfg.Dir/<articleID>/<idx>.jpg (always normalised to JPEG) and are reused
// across repeated summarize calls within cfg.TTL.
//
// Images already on local disk (those whose URL points to the rss-pal
// article-images endpoint, served by api/article_images.go) are resolved to
// their pdfextract-managed location and returned directly — never copied or
// modified, never affected by CleanupExpired.
package imagefetch

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/gif"  // register GIF decoder
	_ "image/png"  // register PNG decoder
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/image/draw"

	"github.com/bytedance/rss-pal/internal/httpx"
)

const (
	downloadTimeout = 30 * time.Second
	maxDownload     = 10 * 1024 * 1024 // 10 MB pre-decode cap; matches proxy
)

// Config holds all knobs FetchAndStore + CleanupExpired need. Construct
// from config.VisionConfig at the call site.
type Config struct {
	Dir                   string        // AI summary image cache root
	LocalArticleImagesDir string        // where PDF clip images live (read-only from this pkg's POV)
	MaxLongSide           int           // resize trigger; px
	TTL                   time.Duration // cache file age limit
}

var localArticleImageRE = regexp.MustCompile(`^/api/articles/(\d+)/images/(\d+)\.([a-z0-9]+)$`)

// FetchAndStore implements the spec contract. See package doc.
func FetchAndStore(ctx context.Context, articleID int, urls []string, cfg Config) ([]string, error) {
	if cfg.Dir == "" {
		return nil, errors.New("imagefetch: Config.Dir is required")
	}
	if cfg.MaxLongSide <= 0 {
		cfg.MaxLongSide = 1024
	}

	client := httpx.NewClient(downloadTimeout)
	out := make([]string, 0, len(urls))
	for idx, raw := range urls {
		path, err := fetchOne(ctx, client, articleID, idx, raw, cfg)
		if err != nil {
			log.Printf("imagefetch: article %d idx %d %q: %v", articleID, idx, raw, err)
			continue
		}
		out = append(out, path)
	}
	return out, nil
}

func fetchOne(ctx context.Context, client *http.Client, articleID, idx int, raw string, cfg Config) (string, error) {
	// Local article-images URL → resolve to disk, no copy.
	if m := localArticleImageRE.FindStringSubmatch(raw); m != nil {
		if cfg.LocalArticleImagesDir == "" {
			return "", errors.New("local article-images URL but LocalArticleImagesDir not configured")
		}
		// m[1] = source article id (may differ from articleID if quoted from elsewhere — use the URL's, not articleID).
		path := filepath.Join(cfg.LocalArticleImagesDir, m[1], m[2]+"."+m[3])
		st, err := os.Stat(path)
		if err != nil {
			return "", fmt.Errorf("local image missing: %w", err)
		}
		if st.Size() == 0 {
			return "", errors.New("local image empty")
		}
		return path, nil
	}

	// Remote URL — go through SSRF guard, cache.
	if _, err := httpx.ValidateURL(raw); err != nil {
		return "", fmt.Errorf("validate: %w", err)
	}

	dir := filepath.Join(cfg.Dir, strconv.Itoa(articleID))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("mkdir: %w", err)
	}
	dest := filepath.Join(dir, strconv.Itoa(idx)+".jpg")

	// Cache hit: refresh mtime, return.
	if st, err := os.Stat(dest); err == nil && st.Size() > 0 {
		now := time.Now()
		_ = os.Chtimes(dest, now, now)
		return dest, nil
	}

	// Download.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return "", fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("User-Agent", httpx.UserAgent)
	// Spoof a Referer matching origin so hotlink-protected CDNs (WeChat, Zhihu) don't 403.
	if i := strings.Index(raw, "://"); i > 0 {
		if j := strings.Index(raw[i+3:], "/"); j > 0 {
			req.Header.Set("Referer", raw[:i+3+j]+"/")
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("get: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("upstream status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxDownload+1))
	if err != nil {
		return "", fmt.Errorf("read body: %w", err)
	}
	if len(body) > maxDownload {
		return "", fmt.Errorf("upstream too large: > %d bytes", maxDownload)
	}

	// Decode (PNG/JPEG/GIF — see blank imports above).
	img, _, err := image.Decode(byteReader(body))
	if err != nil {
		return "", fmt.Errorf("decode: %w", err)
	}

	// Resize if over budget.
	img = resizeIfNeeded(img, cfg.MaxLongSide)

	// Encode as JPEG q85, atomic write.
	tmp := dest + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return "", fmt.Errorf("create tmp: %w", err)
	}
	if err := jpeg.Encode(f, img, &jpeg.Options{Quality: 85}); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return "", fmt.Errorf("encode: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("close tmp: %w", err)
	}
	if err := os.Rename(tmp, dest); err != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("rename: %w", err)
	}
	return dest, nil
}

// resizeIfNeeded scales img down so max(W,H) ≤ maxLongSide, preserving aspect.
// Uses BiLinear for a reasonable speed/quality tradeoff; CatmullRom would be
// sharper but ~3-4x slower.
func resizeIfNeeded(src image.Image, maxLongSide int) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	long := w
	if h > long {
		long = h
	}
	if long <= maxLongSide {
		return src
	}
	ratio := float64(maxLongSide) / float64(long)
	nw := int(float64(w) * ratio)
	nh := int(float64(h) * ratio)
	if nw < 1 {
		nw = 1
	}
	if nh < 1 {
		nh = 1
	}
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	draw.BiLinear.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Over, nil)
	return dst
}

// byteReader wraps a []byte in an io.Reader without adopting bytes.Buffer's
// extra allocation cost.
func byteReader(b []byte) io.Reader { return &sliceReader{b: b} }

type sliceReader struct {
	b   []byte
	pos int
}

func (r *sliceReader) Read(p []byte) (int, error) {
	if r.pos >= len(r.b) {
		return 0, io.EOF
	}
	n := copy(p, r.b[r.pos:])
	r.pos += n
	return n, nil
}
```

- [ ] **Step 6.5: Run — expect PASS**

Run: `cd backend && go test ./internal/imagefetch/ -v`
Expected: PASS (all 5 subtests). If `TestFetchAndStore_resizesOversize` reports a slightly-off width (e.g. 1023 instead of 1024) due to float rounding, adjust the assertion in the test to allow `≥1023 && ≤1024` rather than tweaking the rounding behavior — the spec only requires "longest side ≤ MaxLongSide".

- [ ] **Step 6.6: Commit**

```bash
git add backend/internal/imagefetch/imagefetch.go backend/internal/imagefetch/imagefetch_test.go backend/go.mod backend/go.sum
git commit -m "feat(imagefetch): FetchAndStore with TTL cache + JPEG normalize"
```

---

## Task 7: `imagefetch.CleanupExpired`

**Files:**
- Modify: `backend/internal/imagefetch/imagefetch.go`
- Modify: `backend/internal/imagefetch/imagefetch_test.go`

- [ ] **Step 7.1: Write the failing test**

Append to `backend/internal/imagefetch/imagefetch_test.go`:

```go
func TestCleanupExpired(t *testing.T) {
	cacheDir := t.TempDir()
	localDir := t.TempDir()

	// Seed cache files at varied ages.
	fresh := filepath.Join(cacheDir, "10", "0.jpg")
	old := filepath.Join(cacheDir, "11", "0.jpg")
	if err := os.MkdirAll(filepath.Dir(fresh), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(old), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fresh, []byte("fresh"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(old, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Backdate `old` to 25 hours ago.
	backdate := time.Now().Add(-25 * time.Hour)
	if err := os.Chtimes(old, backdate, backdate); err != nil {
		t.Fatal(err)
	}

	// Seed a file in LocalArticleImagesDir that's even older — must NOT be touched.
	localImg := filepath.Join(localDir, "99", "0.jpg")
	if err := os.MkdirAll(filepath.Dir(localImg), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(localImg, []byte("local"), 0o644); err != nil {
		t.Fatal(err)
	}
	ancient := time.Now().Add(-30 * 24 * time.Hour)
	if err := os.Chtimes(localImg, ancient, ancient); err != nil {
		t.Fatal(err)
	}

	cfg := Config{
		Dir:                   cacheDir,
		LocalArticleImagesDir: localDir,
		MaxLongSide:           1024,
		TTL:                   24 * time.Hour,
	}
	removed, err := CleanupExpired(context.Background(), cfg)
	if err != nil {
		t.Fatalf("CleanupExpired: %v", err)
	}
	if removed != 1 {
		t.Errorf("want removed=1, got %d", removed)
	}

	if _, err := os.Stat(fresh); err != nil {
		t.Errorf("fresh file should still exist: %v", err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Errorf("old file should be removed, got err=%v", err)
	}
	if _, err := os.Stat(filepath.Dir(old)); !os.IsNotExist(err) {
		t.Errorf("empty article-id subdir should be removed, got err=%v", err)
	}
	if _, err := os.Stat(localImg); err != nil {
		t.Errorf("local file must not be touched: %v", err)
	}
}
```

- [ ] **Step 7.2: Run — expect compile fail**

Run: `cd backend && go test ./internal/imagefetch/ -run TestCleanupExpired`
Expected: FAIL (`undefined: CleanupExpired`).

- [ ] **Step 7.3: Implement**

Append to `backend/internal/imagefetch/imagefetch.go`:

```go
// CleanupExpired walks cfg.Dir and removes every regular file whose mtime is
// older than cfg.TTL. After processing each <articleID> subdir, removes the
// subdir if it is now empty. Errors per-file are logged + counted; the walk
// is not aborted on individual failures. Returns the number of files
// successfully removed.
//
// cfg.LocalArticleImagesDir is never visited.
func CleanupExpired(ctx context.Context, cfg Config) (int, error) {
	if cfg.Dir == "" {
		return 0, errors.New("imagefetch: Config.Dir is required")
	}
	if cfg.TTL <= 0 {
		return 0, errors.New("imagefetch: Config.TTL must be positive")
	}
	threshold := time.Now().Add(-cfg.TTL)
	removed := 0

	rootEntries, err := os.ReadDir(cfg.Dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("readdir: %w", err)
	}

	for _, subEnt := range rootEntries {
		if !subEnt.IsDir() {
			continue
		}
		subPath := filepath.Join(cfg.Dir, subEnt.Name())
		fileEntries, err := os.ReadDir(subPath)
		if err != nil {
			log.Printf("imagefetch cleanup: readdir %s: %v", subPath, err)
			continue
		}
		remaining := 0
		for _, fEnt := range fileEntries {
			if fEnt.IsDir() {
				remaining++
				continue
			}
			fPath := filepath.Join(subPath, fEnt.Name())
			info, err := fEnt.Info()
			if err != nil {
				log.Printf("imagefetch cleanup: info %s: %v", fPath, err)
				remaining++
				continue
			}
			if info.ModTime().Before(threshold) {
				if err := os.Remove(fPath); err != nil {
					log.Printf("imagefetch cleanup: remove %s: %v", fPath, err)
					remaining++
					continue
				}
				removed++
			} else {
				remaining++
			}
		}
		if remaining == 0 {
			if err := os.Remove(subPath); err != nil {
				log.Printf("imagefetch cleanup: rmdir %s: %v", subPath, err)
			}
		}
		// Cooperative cancellation between subdirs.
		select {
		case <-ctx.Done():
			return removed, ctx.Err()
		default:
		}
	}
	return removed, nil
}
```

- [ ] **Step 7.4: Run — expect PASS**

Run: `cd backend && go test ./internal/imagefetch/ -v`
Expected: PASS for all tests.

- [ ] **Step 7.5: Commit**

```bash
git add backend/internal/imagefetch/imagefetch.go backend/internal/imagefetch/imagefetch_test.go
git commit -m "feat(imagefetch): CleanupExpired with TTL sweep and empty-subdir prune"
```

---

## Task 8: `internal/ai` content polymorphism + vision model wiring

**Files:**
- Modify: `backend/internal/ai/summarizer.go`
- Create: `backend/internal/ai/summarizer_vision_test.go`

The change: extend `chatMessage.Content` to `interface{}` and add the `contentBlock` types. All legacy callers continue to pass strings — their wire shape is unchanged. Add `visionModel` to `Summarizer` and a `SetVisionModel` method.

- [ ] **Step 8.1: Modify the type definitions**

In `backend/internal/ai/summarizer.go`, replace the existing `chatMessage` declaration (line 51-54):

```go
type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}
```

with the polymorphic version + new block types:

```go
// chatMessage carries either a string (legacy text-only path) or a
// []contentBlock (vision OpenAI schema) as Content.
// json.Marshal handles both via interface{}: strings serialize to "...",
// blocks serialize to [{"type":"text",...},{"type":"image_url",...}].
type chatMessage struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"`
}

type contentBlock struct {
	Type     string         `json:"type"` // "text" | "image_url"
	Text     string         `json:"text,omitempty"`
	ImageURL *imageURLBlock `json:"image_url,omitempty"`
}

type imageURLBlock struct {
	URL string `json:"url"` // either an http(s) URL or "data:image/jpeg;base64,..."
}
```

Then add a `visionModel` field to the Summarizer struct (line 17-22). Replace:

```go
type Summarizer struct {
	apiKey     string
	baseURL    string
	model      string
	httpClient *http.Client
}
```

with:

```go
type Summarizer struct {
	apiKey      string
	baseURL     string
	model       string
	visionModel string // optional; set via SetVisionModel for SummarizeWithImages*
	httpClient  *http.Client
}
```

After the `NewSummarizerWithModel` function (around line 38), add:

```go
// SetVisionModel records the model id used by SummarizeWithImages*. Must be
// set before calling those methods; the text-only summary path is unaffected.
func (s *Summarizer) SetVisionModel(m string) { s.visionModel = m }

// VisionModel returns the configured vision model id (or "" if unset).
func (s *Summarizer) VisionModel() string { return s.visionModel }
```

- [ ] **Step 8.2: Build — expect PASS**

Run: `cd backend && go build ./...`
Expected: PASS. All existing callers pass `string` for Content; that still satisfies `interface{}` and serialises identically. The new types are unused but valid.

- [ ] **Step 8.3: Run existing tests — expect PASS**

Run: `cd backend && go test ./internal/ai/`
Expected: PASS.

- [ ] **Step 8.4: Write tests verifying request shape stays compatible AND new vision shape works**

Create `backend/internal/ai/summarizer_vision_test.go`:

```go
package ai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"image"
	"image/color"
	"image/jpeg"
	"bytes"
)

// captureRequests starts a fake OpenAI-compatible chat server that records
// every inbound request body and replies with a fixed brief/detailed message.
type captured struct {
	bodies [][]byte
}

func newCaptureServer(t *testing.T, replies ...string) (*httptest.Server, *captured) {
	t.Helper()
	cap := &captured{}
	idx := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		cap.bodies = append(cap.bodies, body)
		reply := "ok"
		if idx < len(replies) {
			reply = replies[idx]
		}
		idx++
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":` + jsonString(reply) + `}}]}`))
	}))
	t.Cleanup(srv.Close)
	return srv, cap
}

func jsonString(s string) string { b, _ := json.Marshal(s); return string(b) }

func writeTestJPEG(t *testing.T, path string) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 10, 10))
	for y := 0; y < 10; y++ {
		for x := 0; x < 10; x++ {
			img.Set(x, y, color.RGBA{R: 0, G: 255, B: 0, A: 255})
		}
	}
	var buf bytes.Buffer
	_ = jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80})
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write jpeg: %v", err)
	}
}

func TestSummarize_legacyContentShape(t *testing.T) {
	srv, cap := newCaptureServer(t, "brief content", "detailed content")
	s := NewSummarizerWithModel("test-key", srv.URL, "test-model")

	_, err := s.Summarize(context.Background(), "Title", "Body text")
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if len(cap.bodies) != 2 {
		t.Fatalf("expected 2 requests, got %d", len(cap.bodies))
	}
	for i, body := range cap.bodies {
		var parsed map[string]any
		if err := json.Unmarshal(body, &parsed); err != nil {
			t.Fatalf("body %d not json: %v", i, err)
		}
		msgs, _ := parsed["messages"].([]any)
		for _, m := range msgs {
			mm, _ := m.(map[string]any)
			c, _ := mm["content"]
			if _, isString := c.(string); !isString {
				t.Errorf("body %d: expected legacy string content, got %T = %v", i, c, c)
			}
		}
	}
}

func TestSummarizeWithImages_visionShape(t *testing.T) {
	srv, cap := newCaptureServer(t, "brief vision", "detailed vision")
	s := NewSummarizerWithModel("test-key", srv.URL, "text-model")
	s.SetVisionModel("vision-model")

	dir := t.TempDir()
	imgPath := filepath.Join(dir, "0.jpg")
	writeTestJPEG(t, imgPath)

	_, err := s.SummarizeWithImages(context.Background(), "Title", "Some text", []string{imgPath})
	if err != nil {
		t.Fatalf("SummarizeWithImages: %v", err)
	}
	if len(cap.bodies) != 2 {
		t.Fatalf("expected 2 requests, got %d", len(cap.bodies))
	}
	for i, body := range cap.bodies {
		var parsed map[string]any
		if err := json.Unmarshal(body, &parsed); err != nil {
			t.Fatalf("body %d not json: %v", i, err)
		}
		if got := parsed["model"]; got != "vision-model" {
			t.Errorf("body %d: model field want=%q got=%v", i, "vision-model", got)
		}
		msgs, _ := parsed["messages"].([]any)
		userFound := false
		for _, m := range msgs {
			mm, _ := m.(map[string]any)
			if mm["role"] != "user" {
				continue
			}
			userFound = true
			arr, isArr := mm["content"].([]any)
			if !isArr {
				t.Errorf("body %d: expected user content as array, got %T", i, mm["content"])
				continue
			}
			if len(arr) < 2 {
				t.Errorf("body %d: expected ≥1 text + ≥1 image block, got %d total", i, len(arr))
			}
			hasText, hasImage := false, false
			for _, blk := range arr {
				bb, _ := blk.(map[string]any)
				switch bb["type"] {
				case "text":
					hasText = true
				case "image_url":
					hasImage = true
					iu, _ := bb["image_url"].(map[string]any)
					url, _ := iu["url"].(string)
					if !startsWith(url, "data:image/jpeg;base64,") {
						t.Errorf("body %d: image_url not base64 data URL: %q", i, truncate(url, 40))
					}
				}
			}
			if !hasText || !hasImage {
				t.Errorf("body %d: hasText=%v hasImage=%v", i, hasText, hasImage)
			}
		}
		if !userFound {
			t.Errorf("body %d: no user message in payload", i)
		}
	}
}

func TestSummarizeWithImages_emptyImageList_fallsBackToTextPath(t *testing.T) {
	srv, cap := newCaptureServer(t, "brief", "detailed")
	s := NewSummarizerWithModel("test-key", srv.URL, "text-model")
	s.SetVisionModel("vision-model")

	_, err := s.SummarizeWithImages(context.Background(), "Title", "Body", nil)
	if err != nil {
		t.Fatalf("SummarizeWithImages with nil images: %v", err)
	}
	if len(cap.bodies) != 2 {
		t.Fatalf("expected 2 requests, got %d", len(cap.bodies))
	}
	var parsed map[string]any
	_ = json.Unmarshal(cap.bodies[0], &parsed)
	if parsed["model"] != "text-model" {
		t.Errorf("with no images, expected text-model fallback, got %v", parsed["model"])
	}
}

func startsWith(s, prefix string) bool { return len(s) >= len(prefix) && s[:len(prefix)] == prefix }
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
```

- [ ] **Step 8.5: Run new tests — expect compile fail on `SummarizeWithImages`**

Run: `cd backend && go test ./internal/ai/ -run TestSummarize`
Expected: FAIL — `SummarizeWithImages` not yet defined. Move to Task 9.

- [ ] **Step 8.6: Commit the in-progress refactor (compiles, existing tests pass)**

```bash
git add backend/internal/ai/summarizer.go backend/internal/ai/summarizer_vision_test.go
git commit -m "refactor(ai): chatMessage.Content -> interface{} + visionModel field"
```

(The new tests will be activated when Task 9 lands. They currently fail to compile because they reference `SummarizeWithImages`, which is fine — the next task adds it and the same commit fix will pass.)

Actually — if compilation in the `ai` package fails, NO tests can be run on it. To avoid breaking the test suite mid-plan, comment out the body of `TestSummarizeWithImages_visionShape` and `TestSummarizeWithImages_emptyImageList_fallsBackToTextPath` with a `t.Skip("uncomment in Task 9")` line, leaving the function signatures intact. Then Task 9 reverts the Skip.

Apply this skip before the commit:

In `summarizer_vision_test.go`, both `TestSummarizeWithImages_*` bodies replace their first non-empty line with:

```go
	t.Skip("re-enabled in Task 9 once SummarizeWithImages exists")
```

(Place it as the first statement; the rest stays so we keep all the code visible.)

Re-run the build:

Run: `cd backend && go build ./... && go test ./internal/ai/`
Expected: PASS (skipped vision tests are not failures).

Re-commit if you needed to edit:

```bash
git add backend/internal/ai/summarizer_vision_test.go
git commit --amend --no-edit
```

---

## Task 9: `SummarizeWithImages` (non-streaming)

**Files:**
- Modify: `backend/internal/ai/summarizer.go`
- Modify: `backend/internal/ai/summarizer_vision_test.go`

- [ ] **Step 9.1: Implement `SummarizeWithImages` + helpers**

Open `backend/internal/ai/summarizer.go`. After `SummarizeWithTemplate` (around line 403), append:

```go
// loadImageBlock reads an on-disk image file and returns an image_url content
// block with a base64 data URL. JPEG mime is hardcoded because imagefetch
// always normalises to JPEG.
func loadImageBlock(path string) (*contentBlock, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	enc := base64.StdEncoding.EncodeToString(data)
	return &contentBlock{
		Type:     "image_url",
		ImageURL: &imageURLBlock{URL: "data:image/jpeg;base64," + enc},
	}, nil
}

// buildVisionMessages assembles a system + user message pair with image
// blocks attached to the user message. Returns ([]chatMessage{system,user},
// nil) on success; if every image fails to load it returns (nil, nil) so the
// caller can fall back to the text path.
func buildVisionMessages(prompt string, imagePaths []string) ([]chatMessage, error) {
	if len(imagePaths) == 0 {
		return nil, nil
	}
	userBlocks := []contentBlock{{Type: "text", Text: prompt}}
	loaded := 0
	for _, p := range imagePaths {
		blk, err := loadImageBlock(p)
		if err != nil {
			log.Printf("vision: skip image %s: %v", p, err)
			continue
		}
		userBlocks = append(userBlocks, *blk)
		loaded++
	}
	if loaded == 0 {
		return nil, nil
	}
	return []chatMessage{
		{Role: "system", Content: systemGuardrail},
		{Role: "user", Content: userBlocks},
	}, nil
}

// callVision is the multimodal equivalent of (s *Summarizer).call. It uses
// s.visionModel for the chat completion request and does NOT retry: with
// large image payloads, retries are too expensive — let the caller decide
// to fall back to text-only.
func (s *Summarizer) callVision(ctx context.Context, prompt string, imagePaths []string, maxTokens int) (string, error) {
	if s.visionModel == "" {
		return "", errors.New("vision model not configured (call SetVisionModel)")
	}
	msgs, err := buildVisionMessages(prompt, imagePaths)
	if err != nil {
		return "", err
	}
	if msgs == nil {
		return "", errors.New("no images loaded")
	}
	req := chatRequest{
		Model:     s.visionModel,
		MaxTokens: maxTokens,
		Messages:  msgs,
	}
	body, err := json.Marshal(req)
	if err != nil {
		return "", err
	}
	return s.doCall(ctx, body, maxTokens)
}

// SummarizeWithImages produces brief + detailed summaries informed by the
// images at imagePaths. On any failure (vision model error, all images failed
// to load), it falls back to the text-only Summarize() path so the caller
// always gets a SummaryResult if the model is reachable in any form.
func (s *Summarizer) SummarizeWithImages(ctx context.Context, title, content string, imagePaths []string) (*SummaryResult, error) {
	if len(imagePaths) == 0 {
		return s.Summarize(ctx, title, content)
	}
	content = truncateContent(content)

	briefPrompt := fmt.Sprintf(`请为以下文章生成3-5个要点的简短总结，每个要点用一行表示，以"• "开头。结合附带的图片内容：

标题：%s

内容：
%s

请只输出要点列表，不要其他内容。`, title, content)

	brief, err := s.callVision(ctx, briefPrompt, imagePaths, briefMaxTokens)
	if err != nil {
		log.Printf("vision summary failed, falling back to text: %v", err)
		return s.Summarize(ctx, title, content)
	}

	detailedPrompt := fmt.Sprintf(`请为以下文章生成详细的中文总结，包括主要观点、关键信息和结论。结合附带的图片内容：

标题：%s

内容：
%s

请用中文输出详细总结。`, title, content)

	detailed, err := s.callVision(ctx, detailedPrompt, imagePaths, detailedMaxTokens)
	if err != nil {
		// Brief already succeeded; fall back only the detailed half.
		log.Printf("vision detailed failed, falling back to text detailed: %v", err)
		detText, derr := s.generateDetailed(ctx, title, content)
		if derr != nil {
			return nil, fmt.Errorf("vision detailed + text-fallback both failed: vision=%v text=%v", err, derr)
		}
		detailed = detText
	}

	return &SummaryResult{Brief: brief, Detailed: detailed}, nil
}
```

Add the imports at the top of `summarizer.go`. Open the file and replace the existing import block (lines 3-13):

```go
import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)
```

with:

```go
import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)
```

- [ ] **Step 9.2: Un-skip the vision tests**

In `backend/internal/ai/summarizer_vision_test.go`, remove the `t.Skip("re-enabled in Task 9 once SummarizeWithImages exists")` lines you added in Step 8.6 from both `TestSummarizeWithImages_visionShape` and `TestSummarizeWithImages_emptyImageList_fallsBackToTextPath`.

- [ ] **Step 9.3: Run the new tests**

Run: `cd backend && go test ./internal/ai/ -run TestSummarize -v`
Expected: PASS — `TestSummarize_legacyContentShape`, `TestSummarizeWithImages_visionShape`, `TestSummarizeWithImages_emptyImageList_fallsBackToTextPath`.

- [ ] **Step 9.4: Run full ai test pass**

Run: `cd backend && go test ./internal/ai/`
Expected: PASS.

- [ ] **Step 9.5: Commit**

```bash
git add backend/internal/ai/summarizer.go backend/internal/ai/summarizer_vision_test.go
git commit -m "feat(ai): SummarizeWithImages with vision model + text fallback"
```

---

## Task 10: `SummarizeWithImagesStream`

**Files:**
- Modify: `backend/internal/ai/summarizer.go`

- [ ] **Step 10.1: Add the streaming counterpart**

In `backend/internal/ai/summarizer.go`, after `SummarizeWithImages`, append:

```go
// callVisionStream is the streaming companion of callVision. No retry (same
// reason as callStream: any partial output already delivered to the caller
// cannot be undone).
func (s *Summarizer) callVisionStream(ctx context.Context, prompt string, imagePaths []string, maxTokens int, onDelta func(string)) (string, error) {
	if s.visionModel == "" {
		return "", errors.New("vision model not configured (call SetVisionModel)")
	}
	msgs, err := buildVisionMessages(prompt, imagePaths)
	if err != nil {
		return "", err
	}
	if msgs == nil {
		return "", errors.New("no images loaded")
	}
	req := chatStreamRequest{
		Model:     s.visionModel,
		MaxTokens: maxTokens,
		Stream:    true,
		Messages:  msgs,
	}
	body, err := json.Marshal(req)
	if err != nil {
		return "", err
	}
	httpReq, err := http.NewRequestWithContext(ctx, "POST", s.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+s.apiKey)
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := s.httpClient.Do(httpReq)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("vision stream error %d: %s", resp.StatusCode, string(respBody))
	}

	var full strings.Builder
	reader := bufio.NewReader(resp.Body)
	for {
		line, rerr := reader.ReadString('\n')
		if rerr != nil && rerr != io.EOF {
			return full.String(), rerr
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			if rerr == io.EOF {
				break
			}
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			if rerr == io.EOF {
				break
			}
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "[DONE]" {
			break
		}
		var chunk streamChunk
		if jerr := json.Unmarshal([]byte(payload), &chunk); jerr != nil {
			if rerr == io.EOF {
				break
			}
			continue
		}
		for _, ch := range chunk.Choices {
			if ch.Delta.Content != "" {
				full.WriteString(ch.Delta.Content)
				onDelta(ch.Delta.Content)
			}
		}
		if rerr == io.EOF {
			break
		}
	}
	return full.String(), nil
}

// SummarizeWithImagesStream is the streaming variant of SummarizeWithImages.
// Same fallback contract: any vision-side failure (model error, no images,
// empty stream) drops to SummarizeStream for the affected half.
func (s *Summarizer) SummarizeWithImagesStream(ctx context.Context, title, content string,
	imagePaths []string,
	onBriefDelta, onDetailedDelta func(string)) (*SummaryResult, error) {
	if len(imagePaths) == 0 {
		return s.SummarizeStream(ctx, title, content, onBriefDelta, onDetailedDelta)
	}
	content = truncateContent(content)

	briefPrompt := fmt.Sprintf(`请为以下文章生成3-5个要点的简短总结，每个要点用一行表示，以"• "开头。结合附带的图片内容：

标题：%s

内容：
%s

请只输出要点列表，不要其他内容。`, title, content)

	brief, err := s.callVisionStream(ctx, briefPrompt, imagePaths, briefMaxTokens, onBriefDelta)
	if err != nil {
		log.Printf("vision stream brief failed, falling back to text stream: %v", err)
		return s.SummarizeStream(ctx, title, content, onBriefDelta, onDetailedDelta)
	}

	detailedPrompt := fmt.Sprintf(`请为以下文章生成详细的中文总结，包括主要观点、关键信息和结论。结合附带的图片内容：

标题：%s

内容：
%s

请用中文输出详细总结。`, title, content)

	detailed, err := s.callVisionStream(ctx, detailedPrompt, imagePaths, detailedMaxTokens, onDetailedDelta)
	if err != nil {
		log.Printf("vision stream detailed failed, falling back to text stream detailed: %v", err)
		dText, derr := s.callStream(ctx, detailedPrompt, detailedMaxTokens, onDetailedDelta)
		if derr != nil {
			return nil, fmt.Errorf("vision detailed + text-fallback both failed: vision=%v text=%v", err, derr)
		}
		detailed = dText
	}

	return &SummaryResult{Brief: brief, Detailed: detailed}, nil
}
```

- [ ] **Step 10.2: Build + run all ai tests**

Run: `cd backend && go build ./... && go test ./internal/ai/`
Expected: PASS.

- [ ] **Step 10.3: Commit**

```bash
git add backend/internal/ai/summarizer.go
git commit -m "feat(ai): SummarizeWithImagesStream"
```

---

## Task 11: Service-layer wrappers

**Files:**
- Modify: `backend/internal/service/summarizer.go`

- [ ] **Step 11.1: Add SummarizeWithImages wrappers**

Open `backend/internal/service/summarizer.go`. Append (after `ExtractTopics`):

```go
// SummarizeWithImages routes through the vision path. imagePaths are local
// files (typically from imagefetch.FetchAndStore).
func (s *SummarizerService) SummarizeWithImages(ctx context.Context, article *model.Article, imagePaths []string) (brief, detailed string, err error) {
	content := article.Content
	if content == "" {
		content = article.Title
	}
	result, err := s.summarizer.SummarizeWithImages(ctx, article.Title, content, imagePaths)
	if err != nil {
		return "", "", err
	}
	return result.Brief, result.Detailed, nil
}

// SummarizeWithImagesStream is the streaming variant.
func (s *SummarizerService) SummarizeWithImagesStream(ctx context.Context, article *model.Article, imagePaths []string,
	onBriefDelta, onDetailedDelta func(string)) (brief, detailed string, err error) {
	content := article.Content
	if content == "" {
		content = article.Title
	}
	result, err := s.summarizer.SummarizeWithImagesStream(ctx, article.Title, content, imagePaths, onBriefDelta, onDetailedDelta)
	if err != nil {
		return "", "", err
	}
	return result.Brief, result.Detailed, nil
}

// Summarizer returns the underlying *ai.Summarizer (for VisionModel inspection
// at handler-construction time).
func (s *SummarizerService) Summarizer() *ai.Summarizer { return s.summarizer }
```

The `Summarizer()` accessor is needed by the HTTP layer to check whether vision is configured before forcing the path.

- [ ] **Step 11.2: Build**

Run: `cd backend && go build ./...`
Expected: PASS.

- [ ] **Step 11.3: Commit**

```bash
git add backend/internal/service/summarizer.go
git commit -m "feat(service): SummarizeWithImages + SummarizeWithImagesStream wrappers"
```

---

## Task 12: Worker — backfill routing + summarizer construction

**Files:**
- Modify: `backend/cmd/worker/main.go`

- [ ] **Step 12.1: Wire vision model on summarizer construction**

Find the line that constructs `summarizer` (around line 67):

```go
		summarizer = ai.NewSummarizer(cfg.Claude.APIKey, cfg.Claude.BaseURL)
```

Replace with:

```go
		summarizer = ai.NewSummarizer(cfg.Claude.APIKey, cfg.Claude.BaseURL)
		summarizer.SetVisionModel(cfg.AI.Vision.Model)
```

- [ ] **Step 12.2: Route `backfillSummaries` through the heuristic**

Find `backfillSummaries` in `backend/cmd/worker/main.go` (around line 159–198) and replace its inner goroutine body. The relevant block is:

```go
		go func(article *model.Article) {
			defer wg.Done()
			sumSem <- struct{}{}
			defer func() { <-sumSem }()
			sCtx, cancel := context.WithTimeout(ctx, 6*time.Minute)
			defer cancel()
			result, err := summarizer.Summarize(sCtx, article.Title, article.Content)
			if err != nil {
				log.Printf("Failed to backfill summary for article %d: %v", article.ID, err)
				return
			}
			if err := articleRepo.UpdateSummary(article.ID, result.Brief, result.Detailed); err != nil {
				log.Printf("Failed to save backfill summary for article %d: %v", article.ID, err)
			} else {
				log.Printf("Backfilled summary for article %d", article.ID)
			}
		}(a)
```

Change the call to `summarizer.Summarize` into a heuristic-routed version. Replace the whole inner-goroutine body with:

```go
		go func(article *model.Article) {
			defer wg.Done()
			sumSem <- struct{}{}
			defer func() { <-sumSem }()
			sCtx, cancel := context.WithTimeout(ctx, 6*time.Minute)
			defer cancel()

			var result *ai.SummaryResult
			var err error

			visionCfg := cfg.AI.Vision
			if ai.ShouldUseVisionAuto(article.Content, visionCfg) {
				urls := ai.ExtractImageURLs(article.Content)
				urls = filterCandidateImageURLs(urls, visionCfg.MaxImages)
				if len(urls) > 0 {
					ifCfg := imagefetch.Config{
						Dir:                   visionCfg.CacheDir,
						LocalArticleImagesDir: filepath.Join(cfg.Backup.Dir, "article_images"),
						MaxLongSide:           visionCfg.MaxLongSide,
						TTL:                   visionCfg.CacheTTL,
					}
					paths, _ := imagefetch.FetchAndStore(sCtx, article.ID, urls, ifCfg)
					if len(paths) > 0 {
						log.Printf("Vision-summarizing article %d with %d images", article.ID, len(paths))
						result, err = summarizer.SummarizeWithImages(sCtx, article.Title, article.Content, paths)
					}
				}
			}
			if result == nil {
				result, err = summarizer.Summarize(sCtx, article.Title, article.Content)
			}
			if err != nil {
				log.Printf("Failed to backfill summary for article %d: %v", article.ID, err)
				return
			}
			if err := articleRepo.UpdateSummary(article.ID, result.Brief, result.Detailed); err != nil {
				log.Printf("Failed to save backfill summary for article %d: %v", article.ID, err)
			} else {
				log.Printf("Backfilled summary for article %d", article.ID)
			}
		}(a)
```

Add a helper at the bottom of `cmd/worker/main.go`:

```go
// filterCandidateImageURLs drops avatar / unsupported / out-of-budget URLs.
// Local /api/articles/<id>/images/<idx>.<ext> URLs pass through unchanged —
// imagefetch resolves them to on-disk paths without downloading.
func filterCandidateImageURLs(urls []string, maxImages int) []string {
	out := make([]string, 0, len(urls))
	for _, u := range urls {
		if rss.IsAvatarImageURL(u, "") {
			continue
		}
		if isAcceptableImageURL(u) {
			out = append(out, u)
		}
		if len(out) >= maxImages {
			break
		}
	}
	return out
}

func isAcceptableImageURL(u string) bool {
	if strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://") {
		return true
	}
	if strings.HasPrefix(u, "/api/articles/") && strings.Contains(u, "/images/") {
		return true
	}
	return false
}
```

Add to the imports at the top of `cmd/worker/main.go`:

```go
	"path/filepath"
	"strings"

	"github.com/bytedance/rss-pal/internal/imagefetch"
	"github.com/bytedance/rss-pal/internal/rss"
```

(Keep the existing imports for `ai`, `model`, etc. — only add the missing ones. Some may already be present; deduplicate as needed.)

- [ ] **Step 12.3: Build**

Run: `cd backend && go build ./...`
Expected: PASS. If the build complains about unused imports or missing package paths, run `goimports -w backend/cmd/worker/main.go` to auto-fix.

- [ ] **Step 12.4: Run worker tests if any**

Run: `cd backend && go test ./cmd/worker/...`
Expected: PASS (no failing tests; the worker package historically has no `_test.go` files, in which case the command is a no-op).

- [ ] **Step 12.5: Commit**

```bash
git add backend/cmd/worker/main.go
git commit -m "feat(worker): heuristic-routed vision summaries with avatar filter"
```

---

## Task 13: Worker — periodic cache cleanup

**Files:**
- Modify: `backend/cmd/worker/main.go`

- [ ] **Step 13.1: Call CleanupExpired once per cycle**

Find `runFetchCycle` (around line 119) in `backend/cmd/worker/main.go`. Currently:

```go
func runFetchCycle(ctx context.Context, feedRepo *repository.FeedRepository, articleRepo *repository.ArticleRepository, prefRepo *repository.PreferenceRepository, fetcher *rss.Fetcher, contentFetcher *rss.ContentFetcher, summarizer *ai.Summarizer, transcriptFetcher transcript.Fetcher, imageBaseDir string) {
	...
	backfillSummaries(ctx, articleRepo, summarizer)
	...
}
```

This function signature is constant — it doesn't take `cfg`. To add cleanup cleanly without restructuring, change `runFetchCycle`'s signature to take a `*config.Config` and pipe the call through.

Locate the line where `runFetchCycle` is invoked (around line 113):

```go
		runFetchCycle(ctx, feedRepo, articleRepo, prefRepo, fetcher, contentFetcher, summarizer, transcriptFetcher, imageBaseDir)
```

Change all `runFetchCycle` declarations + invocations to accept `cfg *config.Config`:

Update the function signature:

```go
func runFetchCycle(ctx context.Context, cfg *config.Config, feedRepo *repository.FeedRepository, articleRepo *repository.ArticleRepository, prefRepo *repository.PreferenceRepository, fetcher *rss.Fetcher, contentFetcher *rss.ContentFetcher, summarizer *ai.Summarizer, transcriptFetcher transcript.Fetcher, imageBaseDir string) {
```

(Add `cfg *config.Config` as the second param.)

Update the call site:

```go
		runFetchCycle(ctx, cfg, feedRepo, articleRepo, prefRepo, fetcher, contentFetcher, summarizer, transcriptFetcher, imageBaseDir)
```

Inside `runFetchCycle`, after `backfillSummaries(...)`, add:

```go
	// AI summary image cache TTL sweep. Cheap walk; safe to call every cycle.
	ifCfg := imagefetch.Config{
		Dir:                   cfg.AI.Vision.CacheDir,
		LocalArticleImagesDir: filepath.Join(cfg.Backup.Dir, "article_images"),
		MaxLongSide:           cfg.AI.Vision.MaxLongSide,
		TTL:                   cfg.AI.Vision.CacheTTL,
	}
	if removed, err := imagefetch.CleanupExpired(ctx, ifCfg); err != nil {
		log.Printf("imagefetch cleanup: %v", err)
	} else if removed > 0 {
		log.Printf("imagefetch cleanup: removed %d expired files", removed)
	}
```

Also `backfillSummaries` needs `cfg` — find its declaration (around line 159):

```go
func backfillSummaries(ctx context.Context, articleRepo *repository.ArticleRepository, summarizer *ai.Summarizer) {
```

Change to:

```go
func backfillSummaries(ctx context.Context, cfg *config.Config, articleRepo *repository.ArticleRepository, summarizer *ai.Summarizer) {
```

And update the call from `runFetchCycle`:

```go
	backfillSummaries(ctx, cfg, articleRepo, summarizer)
```

(In Task 12's edit, replace `cfg.AI.Vision` reads with `cfg.AI.Vision` — that's already accessing through the cfg parameter; just make sure the param name matches.)

- [ ] **Step 13.2: Build**

Run: `cd backend && go build ./...`
Expected: PASS.

- [ ] **Step 13.3: Run all backend tests**

Run: `cd backend && go test ./...`
Expected: PASS.

- [ ] **Step 13.4: Commit**

```bash
git add backend/cmd/worker/main.go
git commit -m "feat(worker): per-cycle imagefetch.CleanupExpired sweep"
```

---

## Task 14: HTTP API — `force_vision=1`

**Files:**
- Modify: `backend/internal/api/article.go`

- [ ] **Step 14.1: Locate the streamSummary function and add the routing**

Find `streamSummary` (around line 430 in `backend/internal/api/article.go`). The current block selects between `SummarizeWithTemplateStream` and `SummarizeStream`. Add a vision branch in front.

In the *body* of `streamSummary`, right before the `if h.templateRepo != nil && templateID > 0 {` block (around line 464), add:

```go
	// Vision routing: caller passes ?force_vision=1 on the regen button. We
	// also require the article to actually have at least one usable image URL;
	// otherwise fall through to the text path so we don't pay vision model
	// cost for a no-image article.
	if c.Query("force_vision") == "1" && summarizerToUse.Summarizer().VisionModel() != "" {
		urls := ai.ExtractImageURLs(article.Content)
		urls = filterCandidateImageURLsForAPI(urls, h.cfg.AI.Vision.MaxImages)
		if len(urls) > 0 {
			ifCfg := imagefetch.Config{
				Dir:                   h.cfg.AI.Vision.CacheDir,
				LocalArticleImagesDir: filepath.Join(h.cfg.Backup.Dir, "article_images"),
				MaxLongSide:           h.cfg.AI.Vision.MaxLongSide,
				TTL:                   h.cfg.AI.Vision.CacheTTL,
			}
			paths, _ := imagefetch.FetchAndStore(c.Request.Context(), id, urls, ifCfg)
			if len(paths) > 0 {
				brief, detailed, serr = summarizerToUse.SummarizeWithImagesStream(c.Request.Context(), article, paths, onBrief, onDetailed)
				goto finish
			}
		}
	}
```

And right after the existing `if/else` block (around line 475-476), add the label:

```go
finish:
	if serr != nil {
```

Adjust the imports at the top of `backend/internal/api/article.go`. Add:

```go
	"path/filepath"

	"github.com/bytedance/rss-pal/internal/imagefetch"
```

Verify the `ai` package is already imported (the `ai.NewSummarizerWithModel` call at line ~367 means it is).

If `goto` feels awkward, an equivalent flow without `goto` is:

```go
	visionUsed := false
	if c.Query("force_vision") == "1" && summarizerToUse.Summarizer().VisionModel() != "" {
		urls := ai.ExtractImageURLs(article.Content)
		urls = filterCandidateImageURLsForAPI(urls, h.cfg.AI.Vision.MaxImages)
		if len(urls) > 0 {
			ifCfg := imagefetch.Config{
				Dir:                   h.cfg.AI.Vision.CacheDir,
				LocalArticleImagesDir: filepath.Join(h.cfg.Backup.Dir, "article_images"),
				MaxLongSide:           h.cfg.AI.Vision.MaxLongSide,
				TTL:                   h.cfg.AI.Vision.CacheTTL,
			}
			paths, _ := imagefetch.FetchAndStore(c.Request.Context(), id, urls, ifCfg)
			if len(paths) > 0 {
				brief, detailed, serr = summarizerToUse.SummarizeWithImagesStream(c.Request.Context(), article, paths, onBrief, onDetailed)
				visionUsed = true
			}
		}
	}

	if !visionUsed {
		if h.templateRepo != nil && templateID > 0 {
			tpl, terr := h.templateRepo.GetByID(templateID)
			if terr == nil && tpl != nil {
				brief, detailed, serr = summarizerToUse.SummarizeWithTemplateStream(
					c.Request.Context(), article, tpl.BriefPrompt, tpl.DetailedPrompt, onBrief, onDetailed,
				)
			} else {
				brief, detailed, serr = summarizerToUse.SummarizeStream(c.Request.Context(), article, onBrief, onDetailed)
			}
		} else {
			brief, detailed, serr = summarizerToUse.SummarizeStream(c.Request.Context(), article, onBrief, onDetailed)
		}
	}
```

Use the `visionUsed` flag version (clearer than `goto`). Replace the existing `if h.templateRepo != nil && templateID > 0 { ... } else { ... }` block in `streamSummary` with the flag-guarded form above.

Add the helper at the bottom of the file:

```go
// filterCandidateImageURLsForAPI is the api-side equivalent of the worker's
// filter helper. Kept separate to avoid a circular dep between worker and api
// over a small piece of logic; if a third caller appears, factor into a shared
// internal/ai helper.
func filterCandidateImageURLsForAPI(urls []string, maxImages int) []string {
	out := make([]string, 0, len(urls))
	for _, u := range urls {
		if rss.IsAvatarImageURL(u, "") {
			continue
		}
		if strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://") {
			out = append(out, u)
		} else if strings.HasPrefix(u, "/api/articles/") && strings.Contains(u, "/images/") {
			out = append(out, u)
		}
		if len(out) >= maxImages {
			break
		}
	}
	return out
}
```

Verify `rss` and `strings` are imported at the top.

- [ ] **Step 14.2: Build**

Run: `cd backend && go build ./...`
Expected: PASS.

- [ ] **Step 14.3: Run all tests**

Run: `cd backend && go test ./...`
Expected: PASS.

- [ ] **Step 14.4: Commit**

```bash
git add backend/internal/api/article.go
git commit -m "feat(api): force_vision=1 routes streamSummary through vision path"
```

---

## Task 15: Frontend client — `forceVision` flag

**Files:**
- Modify: `frontend/src/api/client.ts`

- [ ] **Step 15.1: Add the param**

Open `frontend/src/api/client.ts`. Find the `generateSummaryStream` function (around line 624) and change its signature + body.

The current call site builds the URL as:

```ts
    resp = await fetch(`/api/articles/${articleId}/summary?stream=1`, {
```

Change `generateSummaryStream`'s signature from:

```ts
export async function generateSummaryStream(
  articleId: number,
  templateId: number | undefined,
  handlers: SummaryStreamHandlers,
  signal?: AbortSignal,
): Promise<void> {
```

to:

```ts
export async function generateSummaryStream(
  articleId: number,
  templateId: number | undefined,
  handlers: SummaryStreamHandlers,
  signal?: AbortSignal,
  opts?: { forceVision?: boolean },
): Promise<void> {
```

Replace the URL line with:

```ts
    const qp = new URLSearchParams({ stream: '1' })
    if (opts?.forceVision) qp.set('force_vision', '1')
    resp = await fetch(`/api/articles/${articleId}/summary?${qp.toString()}`, {
```

- [ ] **Step 15.2: Type check**

Run: `cd frontend && npx tsc --noEmit`
Expected: PASS. Existing callers (in `ArticlePage.tsx`) still match — the new param is optional.

- [ ] **Step 15.3: Commit**

```bash
git add frontend/src/api/client.ts
git commit -m "feat(client): forceVision flag on generateSummaryStream"
```

---

## Task 16: ArticlePage — pass `forceVision: true` on regen

**Files:**
- Modify: `frontend/src/pages/ArticlePage.tsx`

- [ ] **Step 16.1: Update the call**

Open `frontend/src/pages/ArticlePage.tsx`. Find the `generateSummaryStream(` call (around line 742). The current call:

```ts
    await generateSummaryStream(
      article.id,
      selectedTemplateId,
      {
        onBriefDelta: ...
        ...
      },
    )
```

(The exact param order ends with `handlers` and an optional `signal`.)

The signal — if any — is the 4th arg. The new `opts` is the 5th. Add `, undefined, { forceVision: true }` at the end of the call (or place `signal` at position 4 if currently present, and `{ forceVision: true }` at position 5).

Read the actual existing call in your editor first to know the exact form. Most likely the call ends:

```ts
    await generateSummaryStream(
      article.id,
      selectedTemplateId,
      {
        // handlers ...
      },
    )
```

Change it to:

```ts
    await generateSummaryStream(
      article.id,
      selectedTemplateId,
      {
        // handlers ...
      },
      undefined,
      { forceVision: true },
    )
```

If a signal arg is already present, keep it and just append `, { forceVision: true }`.

- [ ] **Step 16.2: Type check**

Run: `cd frontend && npx tsc --noEmit`
Expected: PASS.

- [ ] **Step 16.3: Commit**

```bash
git add frontend/src/pages/ArticlePage.tsx
git commit -m "feat(ui): regen button forces vision summary"
```

---

## Task 17: Build, docker rebuild, manual smoke

**Files:** none modified — verification + handoff.

- [ ] **Step 17.1: Backend build + full test pass**

Run: `cd backend && go build ./... && go test ./...`
Expected: PASS.

- [ ] **Step 17.2: Frontend production build**

Run: `cd frontend && npm run build`
Expected: PASS (vite bundle produced).

- [ ] **Step 17.3: Docker rebuild affected services**

Run from repo root:

```bash
docker-compose up -d --build api worker frontend
```

Expected: all three containers rebuild + restart cleanly. Tail logs briefly to confirm:

Run: `docker-compose logs --tail=30 worker`
Expected: no startup errors. The new `imagefetch cleanup` log line appears within ~60s of startup (it may print `removed 0 expired files` and only after CleanupExpired ever finds something — startup log may be silent until then).

- [ ] **Step 17.4: Manual smoke — pure-image article (the spec's motivating example)**

Open `http://localhost/articles/2273` (the all-`![](image)` article). Verify:

1. Click 「重新生成」 in the AI 总结 card.
2. Observe in `docker-compose logs -f api`: a request to `/articles/2273/summary?stream=1&force_vision=1` arrives.
3. Observe in `docker-compose logs -f worker` or `api`: an `imagefetch` log line shows N images saved to `/backups/ai_summary_cache/2273/*.jpg`.
4. The streamed brief + detailed text describes the images (e.g. "图片展示了不同种类的咖啡饮品..." for the 咖啡 article 2273).

- [ ] **Step 17.5: Manual smoke — text-heavy article (regression check)**

Open any text-heavy article (e.g. `/articles/357` from earlier in the conversation). Verify:

1. Click 「重新生成」 in the AI 总结 card.
2. `force_vision=1` is sent (check the request URL in browser devtools).
3. Server fetches and inlines images IF the article has any; otherwise `imagefetch` is skipped and the text path runs. Either way the summary completes normally.

- [ ] **Step 17.6: Manual smoke — cache TTL**

Manually backdate a file inside `/backups/ai_summary_cache/<some-id>/` to test the sweep:

```bash
docker-compose exec worker touch -d "2 days ago" /backups/ai_summary_cache/2273/0.jpg
# wait ~60 seconds for next worker cycle
sleep 65
docker-compose exec worker ls /backups/ai_summary_cache/2273/ 2>/dev/null || echo "directory cleaned"
```

Expected: the backdated file is gone after the worker's next cycle; if it was the only file, the `2273` subdir is also removed.

- [ ] **Step 17.7: Push to PR #34**

Run: `git push origin feature/ai-summary-multimodal`
Expected: pushes ~16 commits to remote.

- [ ] **Step 17.8: Post a summary comment to the PR**

Run:

```bash
gh pr comment 34 --body "$(cat <<'EOF'
Implementation complete. All backend tests pass, frontend type-check + build pass, docker rebuild done.

Commits land in this order (rough order):
- feat(config): add AIConfig.Vision with env wiring
- refactor(httpx): extract SSRF-guarded HTTP client out of proxy.go
- feat(rss): IsAvatarImageURL — url+alt avatar detector for markdown callers
- feat(ai): markdown image-URL extractor + text-rune counter
- feat(ai): ShouldUseVisionAuto heuristic for worker backfill routing
- feat(imagefetch): FetchAndStore with TTL cache + JPEG normalize
- feat(imagefetch): CleanupExpired with TTL sweep and empty-subdir prune
- refactor(ai): chatMessage.Content -> interface{} + visionModel field
- feat(ai): SummarizeWithImages with vision model + text fallback
- feat(ai): SummarizeWithImagesStream
- feat(service): SummarizeWithImages + SummarizeWithImagesStream wrappers
- feat(worker): heuristic-routed vision summaries with avatar filter
- feat(worker): per-cycle imagefetch.CleanupExpired sweep
- feat(api): force_vision=1 routes streamSummary through vision path
- feat(client): forceVision flag on generateSummaryStream
- feat(ui): regen button forces vision summary

Awaiting user manual smoke (article 2273 expected primary test target).
EOF
)"
```

---

## Self-Review

**1. Spec coverage:**

| Spec section | Implementing task(s) |
|---|---|
| Heuristic auto-trigger (img_count + text_chars) | Task 5 (`ShouldUseVisionAuto`) + Task 12 (worker plumbing) |
| Frontend regen → force vision | Tasks 14–16 (`force_vision=1` server + client + UI) |
| Image filter (avatar + local + http/s) | Tasks 3 (avatar URL helper) + Task 12/14 (filter helpers in worker/api) |
| `internal/imagefetch.FetchAndStore` + LocalArticleImagesDir resolution | Task 6 |
| `internal/imagefetch.CleanupExpired` | Task 7 |
| Per-image pipeline (download → decode → resize → JPEG q85 → atomic write) | Task 6 |
| `chatMessage.Content interface{}` | Task 8 |
| `SummarizeWithImages` + `SummarizeWithImagesStream` | Tasks 9–10 |
| Soft-fallback on vision failure | Tasks 9–10 (built into both methods) |
| Worker construct with vision model | Task 12 |
| Worker cleanup tick | Task 13 |
| HTTP `force_vision=1` | Task 14 |
| Config env wiring | Task 1 |
| Test coverage matrix | Spread across Tasks 2, 3, 4, 5, 6, 7, 8, 9 (table tests for everything except worker integration, which is verified by manual smoke) |
| Backward compat (existing summaries untouched, vision failures fall back) | No active task — guaranteed by routing + fallback logic + no schema change |

No gaps.

**2. Placeholder scan:** No "TBD" / "implement later" / "etc." patterns in any step. Every code-emitting step contains exact code. Every command step shows exact command + expected output.

**3. Type consistency:**

- `Config` struct: same shape across Tasks 6, 7, 12, 13, 14 — `{Dir, LocalArticleImagesDir, MaxLongSide, TTL}`.
- `VisionConfig` fields: `Model, MaxImages, MaxLongSide, PayloadBudgetMB, MinImages, MaxTextChars, CacheDir, CacheTTL` — consistent in Task 1 and consumed by tasks 5, 12, 13, 14.
- `SummarizeWithImages(ctx, title, content string, imagePaths []string) (*SummaryResult, error)`: same signature in Task 9 (definition) and Task 11 (service wrapper) and Task 12 (worker call).
- `SummarizeWithImagesStream(ctx, title, content string, imagePaths []string, onBriefDelta, onDetailedDelta func(string)) (*SummaryResult, error)`: same in Tasks 10 + 11 + 14.
- `IsAvatarImageURL(src, alt string) bool`: same in Task 3 (definition) and Tasks 12 + 14 (callers).
- `ShouldUseVisionAuto(content string, cfg config.VisionConfig) bool`: same in Tasks 5 + 12.
- `ExtractImageURLs(md string) []string`, `CountTextRunes(md string) int`: defined Task 4, used Tasks 5, 12, 14.

All consistent.

**4. Scope check:**

One feature (vision summary), one PR, ~17 tasks of which only 13 produce new logic — the rest are wiring. Doable in a single subagent-driven execution session.

**Spec → plan mapping is complete. Plan is ready to execute via superpowers:subagent-driven-development.**
