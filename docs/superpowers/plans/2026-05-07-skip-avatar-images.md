# Skip Avatar Images Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Drop author/profile avatar `<img>` tags from article content at extraction time (server, Go) and at render time (frontend, React) so they stop polluting the reader and stop firing needless `/api/proxy/image` fetches.

**Architecture:** Two coordinated filters sharing the same detection semantics. Server-side, a pure `stripAvatars(*goquery.Document)` helper runs before markdown conversion in all three extraction paths (`fetchDirect`, `FetchContentFromReader`, `extractContentFromHTML`). Frontend-side, a pure `isAvatarImg(src, alt)` predicate runs inside the existing `<img>` component override in `MarkdownArticle.tsx` and short-circuits rendering when matched.

**Tech Stack:** Go 1.24 + `github.com/PuerkitoBio/goquery`; React 18 + `react-markdown` + TypeScript.

**Spec:** `docs/superpowers/specs/2026-05-07-skip-avatar-images-design.md`

---

## File Structure

**Backend (Go):**
- Modify: `backend/internal/rss/content.go` — add `isAvatarImg` + `stripAvatars` helpers; wire into `fetchDirect` and `FetchContentFromReader`.
- Modify: `backend/internal/api/bookmarklet.go` — wire `rss.StripAvatars` into `extractContentFromHTML`.
- Modify: `backend/internal/rss/content_test.go` — add table tests for `isAvatarImg` and `stripAvatars`, plus one end-to-end test via `FetchContentFromReader`.

**Frontend (TS):**
- Modify: `frontend/src/components/MarkdownArticle.tsx` — add module-level `isAvatarImg(src, alt)` + early return in the `img` component override.

No new files. No new dependencies.

---

## Task 1: `isAvatarImg` predicate (Go)

**Files:**
- Modify: `backend/internal/rss/content.go` (add helper at end of file)
- Test: `backend/internal/rss/content_test.go` (append table test)

- [ ] **Step 1: Write the failing test**

Append to `backend/internal/rss/content_test.go`:

```go
func TestIsAvatarImg(t *testing.T) {
	cases := []struct {
		name string
		html string
		want bool
	}{
		{"class avatar", `<img class="avatar" src="https://example.com/x.png">`, true},
		{"class user-avatar", `<img class="user-avatar size-32" src="https://example.com/x.png">`, true},
		{"id author-photo", `<img id="author-photo" src="https://example.com/x.png">`, true},
		{"alt profile picture", `<img alt="John's profile picture" src="https://example.com/x.png">`, true},
		{"alt headshot", `<img alt="Headshot" src="https://example.com/x.png">`, true},
		{"gravatar host", `<img src="https://www.gravatar.com/avatar/abc123">`, true},
		{"avatars path", `<img src="https://cdn.example.com/avatars/u123.png">`, true},
		{"avatar path", `<img src="https://cdn.example.com/avatar/u123.png">`, true},
		{"both dims small", `<img width="32" height="32" src="https://example.com/x.png">`, true},
		{"both dims at threshold", `<img width="64" height="64" src="https://example.com/x.png">`, true},
		{"only width small", `<img width="32" src="https://example.com/x.png">`, false},
		{"only height small", `<img height="32" src="https://example.com/x.png">`, false},
		{"large dims", `<img width="800" height="600" src="https://example.com/x.png">`, false},
		{"plain article image", `<img src="https://example.com/screenshot.png">`, false},
		{"unrelated class", `<img class="wp-image-123" src="https://example.com/x.png">`, false},
		{"non-numeric width", `<img width="auto" height="32" src="https://example.com/x.png">`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := goquery.NewDocumentFromReader(strings.NewReader(tc.html))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			img := doc.Find("img").First()
			got := isAvatarImg(img)
			if got != tc.want {
				t.Errorf("isAvatarImg(%s) = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}
```

Add `"github.com/PuerkitoBio/goquery"` to the test file's imports if not already present.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/rss/ -run TestIsAvatarImg -v`
Expected: FAIL with `undefined: isAvatarImg`

- [ ] **Step 3: Implement `isAvatarImg`**

Append to `backend/internal/rss/content.go`:

```go
// avatarAttrKeywords are case-insensitive substrings that, when found in an
// <img>'s class/id/alt/rel attributes, mark it as an author/profile avatar.
var avatarAttrKeywords = []string{
	"avatar", "gravatar", "profile", "author",
	"user-pic", "userpic", "headshot",
}

// avatarURLKeywords are case-insensitive substrings that, when found in an
// <img> src URL, mark it as an avatar.
var avatarURLKeywords = []string{
	"gravatar.com", "/avatar/", "/avatars/",
}

// avatarMaxDimension is the upper bound (inclusive) for an <img>'s declared
// width AND height to be treated as an avatar by dimension. Both dimensions
// must be present and parseable; one alone does not qualify.
const avatarMaxDimension = 64

// isAvatarImg reports whether an <img> selection looks like an author/profile
// avatar. Returns true on either signal: keyword/URL match (class/id/alt/rel/src)
// or both width AND height attributes present and ≤ avatarMaxDimension.
func isAvatarImg(s *goquery.Selection) bool {
	for _, attr := range []string{"class", "id", "alt", "rel"} {
		v := strings.ToLower(s.AttrOr(attr, ""))
		if v == "" {
			continue
		}
		for _, kw := range avatarAttrKeywords {
			if strings.Contains(v, kw) {
				return true
			}
		}
	}
	src := strings.ToLower(s.AttrOr("src", ""))
	for _, kw := range avatarURLKeywords {
		if strings.Contains(src, kw) {
			return true
		}
	}
	w, wErr := strconv.Atoi(strings.TrimSpace(s.AttrOr("width", "")))
	h, hErr := strconv.Atoi(strings.TrimSpace(s.AttrOr("height", "")))
	if wErr == nil && hErr == nil && w > 0 && h > 0 && w <= avatarMaxDimension && h <= avatarMaxDimension {
		return true
	}
	return false
}
```

Add `"strconv"` to the imports block at the top of `content.go` (alongside `regexp`, `strings`, etc.).

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/rss/ -run TestIsAvatarImg -v`
Expected: PASS — all 16 sub-tests pass.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/rss/content.go backend/internal/rss/content_test.go
git commit -m "feat(rss): isAvatarImg predicate for author/profile <img> detection

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 2: `stripAvatars` document mutator (Go)

**Files:**
- Modify: `backend/internal/rss/content.go` (add helper + exported wrapper)
- Test: `backend/internal/rss/content_test.go` (append table test)

- [ ] **Step 1: Write the failing test**

Append to `backend/internal/rss/content_test.go`:

```go
func TestStripAvatars(t *testing.T) {
	html := `<html><body><article>
		<p>Intro paragraph.</p>
		<p><img class="avatar" src="https://example.com/me.png" alt="me"></p>
		<p><img src="https://www.gravatar.com/avatar/abc"></p>
		<p><img width="32" height="32" src="https://example.com/tiny.png"></p>
		<p><img src="https://example.com/screenshot.png" alt="a real screenshot"></p>
		<p>Trailing paragraph.</p>
	</article></body></html>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	stripAvatars(doc)

	imgs := doc.Find("img")
	if imgs.Length() != 1 {
		t.Fatalf("expected 1 surviving img, got %d", imgs.Length())
	}
	src, _ := imgs.First().Attr("src")
	if src != "https://example.com/screenshot.png" {
		t.Errorf("wrong img survived: src=%q", src)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/rss/ -run TestStripAvatars -v`
Expected: FAIL with `undefined: stripAvatars`

- [ ] **Step 3: Implement `stripAvatars`**

Append to `backend/internal/rss/content.go`:

```go
// stripAvatars removes <img> elements matching avatar heuristics from doc,
// mutating it in place. Called before markdown conversion so avatars never
// enter stored content.
func stripAvatars(doc *goquery.Document) {
	doc.Find("img").Each(func(_ int, s *goquery.Selection) {
		if isAvatarImg(s) {
			s.Remove()
		}
	})
}

// StripAvatars is the exported wrapper for callers in other packages
// (e.g. internal/api/bookmarklet.go) that hold a goquery.Document.
func StripAvatars(doc *goquery.Document) { stripAvatars(doc) }
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/rss/ -run TestStripAvatars -v`
Expected: PASS — 1 surviving img with src=`screenshot.png`.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/rss/content.go backend/internal/rss/content_test.go
git commit -m "feat(rss): stripAvatars helper to drop avatar imgs from goquery doc

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 3: Wire `stripAvatars` into all three extraction paths (Go)

**Files:**
- Modify: `backend/internal/rss/content.go` (`fetchDirect` ~line 107, `FetchContentFromReader` ~line 280)
- Modify: `backend/internal/api/bookmarklet.go` (`extractContentFromHTML` ~line 228)
- Test: `backend/internal/rss/content_test.go` (append integration test)

- [ ] **Step 1: Write the failing integration test**

Append to `backend/internal/rss/content_test.go`:

```go
func TestFetchContentFromReader_StripsAvatars(t *testing.T) {
	html := `<html><body><article>
		<p>Intro paragraph long enough to keep around for the selector.</p>
		<p><img class="avatar" src="https://example.com/byline.png" alt="me"></p>
		<p><img src="https://example.com/figure.png" alt="figure"></p>
		<p>Trailing paragraph long enough to keep around as well.</p>
	</article></body></html>`

	f := NewContentFetcher()
	got, err := f.FetchContentFromReader(strings.NewReader(html))
	if err != nil {
		t.Fatalf("FetchContentFromReader: %v", err)
	}
	if strings.Contains(got, "byline.png") {
		t.Errorf("expected avatar to be stripped, got:\n%s", got)
	}
	if !strings.Contains(got, "figure.png") {
		t.Errorf("expected real figure to survive, got:\n%s", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/rss/ -run TestFetchContentFromReader_StripsAvatars -v`
Expected: FAIL — output contains `byline.png` because no path is wired yet.

- [ ] **Step 3: Wire `stripAvatars` into `fetchDirect`**

In `backend/internal/rss/content.go`, locate the `fetchDirect` method. After the existing chrome-stripping line (search for `doc.Find("script, style, nav, header, footer, aside`):

```go
	// Remove unwanted elements
	doc.Find("script, style, nav, header, footer, aside, .sidebar, .comments, .advertisement, .ad, .social-share, .related-posts, .tags, [class*=share], [class*=comment], [class*=recommend]").Remove()
```

Add directly below it:

```go
	stripAvatars(doc)
```

- [ ] **Step 4: Wire `stripAvatars` into `FetchContentFromReader`**

In the same file, in `FetchContentFromReader`, after:

```go
	doc.Find("script, style, nav, header, footer, aside").Remove()
```

Add:

```go
	stripAvatars(doc)
```

- [ ] **Step 5: Wire `rss.StripAvatars` into `extractContentFromHTML`**

In `backend/internal/api/bookmarklet.go`, locate `extractContentFromHTML`. After:

```go
	doc.Find("script, style, nav, header, footer, aside, .sidebar, .comments, .advertisement, .ad, .social-share, .related-posts, .tags, [class*=share], [class*=comment], [class*=recommend]").Remove()
```

Add:

```go
	rss.StripAvatars(doc)
```

(`rss` is already imported in this file — verify with `grep "internal/rss" backend/internal/api/bookmarklet.go`.)

- [ ] **Step 6: Run integration test + full rss package + bookmarklet package to verify**

Run: `cd backend && go test ./internal/rss/ ./internal/api/ -v 2>&1 | tail -30`
Expected: PASS — including `TestFetchContentFromReader_StripsAvatars`. No regressions in existing tests (especially `TestFetchContentFromReader_PreservesImage`, which uses `https://example.com/cat.png` with no avatar signals).

- [ ] **Step 7: Commit**

```bash
git add backend/internal/rss/content.go backend/internal/rss/content_test.go backend/internal/api/bookmarklet.go
git commit -m "feat(rss): strip avatar imgs in all three extraction paths

Wires stripAvatars into fetchDirect, FetchContentFromReader, and
bookmarklet's extractContentFromHTML so author/profile avatars never
enter stored article content.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 4: Frontend renderer skips avatars (TS)

**Files:**
- Modify: `frontend/src/components/MarkdownArticle.tsx`

No frontend test runner is configured — verification is via manual smoke test in Task 5.

- [ ] **Step 1: Add `isAvatarImg` helper + render skip**

Replace the contents of `frontend/src/components/MarkdownArticle.tsx` with:

```tsx
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import rehypeHighlight from 'rehype-highlight'
import 'highlight.js/styles/github.css'

type Props = {
  source: string
}

const AVATAR_ATTR_KEYWORDS = [
  'avatar', 'gravatar', 'profile', 'author',
  'user-pic', 'userpic', 'headshot',
]
const AVATAR_URL_KEYWORDS = [
  'gravatar.com', '/avatar/', '/avatars/',
]

// isAvatarImg mirrors the server-side detector (Signal 1 only — class/id/width
// /height attributes don't survive markdown round-trip, so dimension matching
// is unreachable client-side). Returns true if the image's URL or alt text
// contains any avatar keyword.
function isAvatarImg(src: string | undefined, alt: string | undefined): boolean {
  const url = (src ?? '').toLowerCase()
  for (const kw of AVATAR_URL_KEYWORDS) {
    if (url.includes(kw)) return true
  }
  const altLower = (alt ?? '').toLowerCase()
  if (!altLower) return false
  for (const kw of AVATAR_ATTR_KEYWORDS) {
    if (altLower.includes(kw)) return true
  }
  return false
}

// Rewrites <img src="..."> to go through the backend proxy so hotlink-
// protected sites (WeChat, Zhihu) actually render. Author/profile avatars
// are dropped entirely (see isAvatarImg). External links open in a new tab.
export default function MarkdownArticle({ source }: Props) {
  return (
    <div className="markdown-body">
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        rehypePlugins={[rehypeHighlight]}
        components={{
          img: ({ src, alt, ...rest }) => {
            if (isAvatarImg(src, alt)) return null
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

- [ ] **Step 2: Type-check the frontend**

Run: `cd frontend && npx tsc --noEmit 2>&1 | tail -20`
Expected: No errors. (If `tsc` reports unrelated pre-existing errors, confirm none mention `MarkdownArticle.tsx` or `isAvatarImg`.)

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/MarkdownArticle.tsx
git commit -m "feat(frontend): skip avatar imgs in MarkdownArticle renderer

Mirrors the server-side avatar detector (URL + alt keywords only;
dimensions don't survive markdown). Avatars render as null — no DOM
node, no proxy fetch.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 5: Manual smoke test (Docker)

**Context:** Per memory, Docker builds from the **main** worktree (`/Users/bytedance/mygit/rss-pal`), not from `.worktrees/`. To smoke-test, the branch must be checked out there first.

- [ ] **Step 1: Bring the branch into the main worktree**

From the main repo dir:

```bash
cd /Users/bytedance/mygit/rss-pal
git fetch . feature/skip-avatar-images:feature/skip-avatar-images-test 2>/dev/null || true
git checkout feature/skip-avatar-images
```

(If the user prefers to keep `feature/insights-full` checked out in main and merge later, this step is deferred until merge time. Note that as a manual step.)

- [ ] **Step 2: Rebuild and start services**

```bash
docker-compose up -d --build api worker frontend
docker-compose logs -f api 2>&1 | head -30
```

Wait for `api` log to show `Listening on :8080` (or equivalent ready signal).

- [ ] **Step 3: Open an existing article known to contain a gravatar/avatar**

In the browser at `http://localhost:5173` (or the configured frontend port), open an article that the user identifies as containing an author avatar. Open DevTools → Network tab, filter by `proxy/image`.

Expected:
- The avatar `<img>` does NOT render in the article body.
- No `/api/proxy/image?url=...gravatar...` request fires.
- Other images in the article (screenshots, figures) render normally and DO fire proxy requests.

- [ ] **Step 4: Capture a fresh article via bookmarklet (or wait for worker to fetch one)**

Either:
- Use the bookmarklet on a page known to contain an avatar in its byline, then open the new article in RSS Pal.
- Or trigger `INSIGHTS_RUN_NOW=1` / wait for the next worker cycle on a feed whose articles contain avatars.

Verify the stored markdown for that article has no `![…](…/avatar/…)` reference. Check via:

```bash
docker-compose exec postgres psql -U postgres -d rsspal -c "SELECT id, substring(content, 1, 500) FROM articles ORDER BY id DESC LIMIT 1;"
```

- [ ] **Step 5: Report findings to the user**

Summarize:
- Frontend skip working? Y/N + which article tested.
- Server-side strip working on freshly-captured article? Y/N.
- Any false positives observed (legitimate images that disappeared)? List them.

If all green, proceed to merge per `superpowers:finishing-a-development-branch`.

---

## Self-Review

**Spec coverage:**
- Detection rule (signal 1 keywords + signal 2 dimensions, ≤64 both required) → Task 1.
- A. Server-side `stripAvatars` helper + wiring → Tasks 2 + 3.
- C. Frontend renderer skip → Task 4.
- Tests (table for `isAvatarImg`, integration via `FetchContentFromReader`) → Tasks 1, 2, 3.
- Frontend test runner skip → noted in Task 4 + manual smoke in Task 5.
- Acceptance criteria 1–4 → covered by Tasks 3 (test pass) and 5 (manual verification).
- Acceptance criterion 5 (no false positives in dev DB) → covered by Task 5 step 5.

**Placeholder scan:** All steps contain concrete code, exact paths, and explicit commands. No "implement later" or "add error handling" stubs.

**Type consistency:** `isAvatarImg(s *goquery.Selection) bool` — same signature in test and impl. `stripAvatars(*goquery.Document)` / exported `StripAvatars` — both used consistently. Frontend `isAvatarImg(src?, alt?)` — only consumer is the `img` override, signature matches.
