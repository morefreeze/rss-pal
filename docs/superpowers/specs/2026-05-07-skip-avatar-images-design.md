# Skip Avatar Images in Article Content

**Date:** 2026-05-07
**Branch:** `feature/skip-avatar-images`
**Worktree:** `.worktrees/feature/skip-avatar-images`

## Problem

When RSS Pal scrapes article HTML (worker RSS path or bookmarklet capture), every `<img>` in the page survives into the stored markdown — including author/profile avatars that appear in bylines. These avatars then:

- Pollute the article view (small profile photos appearing inline as decoration noise).
- Trigger needless `/api/proxy/image` requests when the article is rendered.
- Inflate the markdown image count, which feeds into bookmarklet duplicate-detection logic and can mislead the "more/fewer images" prompt.

## Goal

Drop avatar `<img>` tags at two layers:

- **Extraction time** (server) — newly scraped articles never store avatars.
- **Render time** (frontend) — already-stored articles also stop rendering and proxy-fetching avatars.

## Out of Scope

- The image proxy itself (`/api/proxy/image`) is unchanged. It will keep serving any URL it's asked for; the goal is for nobody to ask in the first place.
- Backfill / migration of existing `articles.content` rows. Frontend filtering covers them at render time, which is good enough.
- Detection via DOM position heuristics (`.author` ancestor, `rel="author"` siblings). Reserved for follow-up if keyword + dimension matching proves insufficient.

## Detection Rule

An `<img>` is an avatar if **either** of these is true:

### Signal 1 — Keyword / URL match

Case-insensitive substring match on:

- **Attribute keywords** (in `class`, `id`, `alt`, or `rel`):
  `avatar`, `gravatar`, `profile`, `author`, `user-pic`, `userpic`, `headshot`
- **URL host/path keywords**:
  `gravatar.com`, `/avatar/`, `/avatars/`

### Signal 2 — Dimension match (server only)

Both `width` AND `height` attributes are present on the `<img>` and both parse to integers ≤ 64.

If either dimension is missing or unparseable, this signal does NOT fire (avoids false positives on attribute-less article images).

### Combination

Either signal alone is sufficient. The signals are independent OR-ed.

## A. Server-Side: Extraction

### New helper

In `backend/internal/rss/content.go`, add:

```go
// stripAvatars removes <img> elements matching avatar heuristics from
// the document, mutating it in place. Called before markdown conversion
// so avatars never enter stored content.
func stripAvatars(doc *goquery.Document)
```

Detection logic lives in a private `isAvatarImg(s *goquery.Selection) bool` helper to keep `stripAvatars` itself trivial and the detector unit-testable.

### Wiring

Call `stripAvatars(doc)` immediately after the existing chrome-stripping `doc.Find(...).Remove()` call, before the content-selector loop:

- `backend/internal/rss/content.go::fetchDirect` — line ~107.
- `backend/internal/api/bookmarklet.go::extractContentFromHTML` — line ~228.

Both paths converge on `rss.ExtractMarkdown`, so a single `stripAvatars` invocation per document is enough.

### Frontend-affected mode

The Jina Reader fallback (`fetchViaJina`) returns pre-rendered markdown — there is no `<img>` element to inspect at that point. Markdown image syntax `![alt](url)` survives unchanged. We do NOT post-process Jina output server-side; the frontend filter (Section C) handles avatars from this path.

## C. Frontend: Renderer

### Detector

In `frontend/src/components/MarkdownArticle.tsx`, add a module-level helper:

```ts
function isAvatarImg(src: string | undefined, alt: string | undefined): boolean
```

Implements **Signal 1 only** (Signal 2 is unreachable client-side because dimensions are unknown until after the image fetch we're trying to avoid). Substring lists match the server's exactly:

- alt keywords: `avatar`, `gravatar`, `profile`, `author`, `user-pic`, `userpic`, `headshot`
- URL host/path keywords: `gravatar.com`, `/avatar/`, `/avatars/`

Match is case-insensitive on the lowercased haystack.

### Render change

In the existing `img` component override, return `null` when `isAvatarImg` matches. This means no DOM `<img>` element, no proxy URL, no fetch.

```tsx
img: ({ src, alt, ...rest }) => {
  if (isAvatarImg(src, alt)) return null
  // existing proxy-rewrite logic
}
```

## Tests

### Backend

Extend `backend/internal/rss/content_test.go` with a table-driven test for `isAvatarImg` covering:

- `<img class="avatar" src="...">` → true (class keyword)
- `<img src="https://www.gravatar.com/avatar/abc">` → true (URL host)
- `<img src="https://cdn.example.com/avatars/u123.png">` → true (URL path)
- `<img width="32" height="32" src="...">` → true (dimensions)
- `<img width="32" src="...">` → false (only one dimension)
- `<img src="https://example.com/article-photo.jpg" width="800" height="600">` → false (regular image)
- `<img src="https://example.com/header.jpg">` → false (no signals)

Plus an integration-shape test that runs `stripAvatars` on a small HTML fixture containing one avatar and one real image, asserting only the real image survives the round-trip through `ExtractMarkdown`.

### Frontend

Frontend currently has no Vitest/Jest setup configured (verify this; if absent, skip). If a test runner is already wired up, add a unit test for `isAvatarImg` covering the same URL/alt cases above.

If no runner exists, document the manual smoke test: build the frontend, open an existing article known to contain a gravatar, confirm the avatar no longer renders.

## Worktree

Per project convention (CLAUDE.md note: Docker builds from main worktree):

- Branch: `feature/skip-avatar-images` from `master`.
- Worktree path: `.worktrees/feature/skip-avatar-images`.
- Implementation happens in the worktree. Once complete, work is merged or rebased back to a branch in the main repo dir before a docker rebuild verification, since `docker-compose up -d --build` only sees the main worktree's checkout.

## Acceptance Criteria

1. `go test ./backend/internal/rss/...` passes, including new `isAvatarImg` and `stripAvatars` table tests.
2. A scraped article whose source HTML contains an `<img class="avatar">` produces stored markdown with no reference to that image.
3. An existing article in the DB whose stored markdown contains a `gravatar.com` URL renders in the browser with the avatar omitted (no `/api/proxy/image?url=...gravatar...` request fires — verifiable via Network tab).
4. A normal in-content image (`<img src="https://example.com/screenshot.png">` with no avatar signals) renders unchanged.
5. No false positives on the existing test corpus — diff-check a few previously-captured articles in the dev DB to confirm legitimate images aren't hidden.
