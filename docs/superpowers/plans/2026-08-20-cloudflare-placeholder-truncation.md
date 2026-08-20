# Cloudflare Placeholder Truncation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Keep Cloudflare presentation placeholders out of stored Markdown, truncate content safely, deploy the fix, and selectively re-fetch affected articles from the last 72 hours.

**Architecture:** Extend the existing DOM cleanup in `backend/internal/rss/content.go` with a narrow placeholder predicate, then route both extraction paths through one safe limiter. Exercise the behavior through the existing RSS package tests. Production recovery inventories a fixed 72-hour candidate set before invoking the existing per-article content endpoint serially.

**Tech Stack:** Go, goquery, Go testing, PostgreSQL, Docker Compose, GitHub Actions, curl.

---

### Task 1: Remove explicit presentation placeholders

**Files:**
- Modify: `backend/internal/rss/content_test.go`
- Modify: `backend/internal/rss/content.go`

- [ ] **Step 1: Write the failing Cloudflare regression test**

Add a test that passes an `<article>` containing a real remote `<img>`, a sibling `<img class="pt-image-placeholder" data-image-placeholder aria-hidden="true" src="data:image/bmp;base64,...">`, and prose after both. Assert that `FetchContentFromReader` retains the remote URL and trailing prose but excludes `data:image`.

- [ ] **Step 2: Run the focused test and verify RED**

Run: `cd backend && go test ./internal/rss -run TestFetchContentRemovesPresentationImagePlaceholder -count=1`

Expected: FAIL because the Base64 placeholder is present in extracted Markdown.

- [ ] **Step 3: Implement the narrow cleanup rule**

Add `RemovePresentationImagePlaceholders(doc *goquery.Document)` and call it after `PromoteLazyImages`. Remove an image only when its trimmed `src` starts with `data:image/` and it has `data-image-placeholder`, class token `pt-image-placeholder`, or case-insensitive `aria-hidden=true`.

- [ ] **Step 4: Add the legitimate data-image preservation test**

Add a test with an unmarked `src="data:image/png;base64,..."` and assert it remains in Markdown.

- [ ] **Step 5: Run focused tests and verify GREEN**

Run: `cd backend && go test ./internal/rss -run 'TestFetchContent(RemovesPresentationImagePlaceholder|PreservesUnmarkedDataImage)' -count=1`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/rss/content.go backend/internal/rss/content_test.go
git commit -m "fix: drop decorative image placeholders"
```

### Task 2: Make the content limit structurally safe

**Files:**
- Modify: `backend/internal/rss/content_test.go`
- Modify: `backend/internal/rss/content.go`

- [ ] **Step 1: Write failing limiter tests**

Add table-driven tests for a helper named `limitContent(content string, maxBytes int) string`: ASCII below the limit is unchanged; multibyte Chinese text above the limit returns valid UTF-8 plus `...`; and a limit inside `![](data:image/png;base64,...)` drops the unmatched image opener instead of returning partial Markdown.

- [ ] **Step 2: Run the limiter tests and verify RED**

Run: `cd backend && go test ./internal/rss -run TestLimitContent -count=1`

Expected: build FAIL because `limitContent` does not exist.

- [ ] **Step 3: Implement and share the limiter**

Implement `limitContent` using byte length, backtracking while `!utf8.ValidString(prefix)`, and removing a trailing unmatched Markdown image opener when its last `![` occurs after the last `)`. Replace both `content[:50000] + "..."` sites with `limitContent(content, 50000)`.

- [ ] **Step 4: Run focused and package tests**

```bash
cd backend
go test ./internal/rss -run TestLimitContent -count=1
go test ./internal/rss -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/rss/content.go backend/internal/rss/content_test.go
git commit -m "fix: truncate extracted content safely"
```

### Task 3: Verify and publish

**Files:**
- No new files.

- [ ] **Step 1: Format and inspect**

Run: `gofmt -w backend/internal/rss/content.go backend/internal/rss/content_test.go && git diff --check`

Expected: exit 0.

- [ ] **Step 2: Run the full backend suite**

Run: `cd backend && go test ./... -count=1`

Expected: PASS with zero failing packages.

- [ ] **Step 3: Inspect scope**

Run: `git status --short && git diff master...HEAD --stat && git log --oneline master..HEAD`

Expected: only the spec, plan, RSS extraction source, and RSS extraction tests are changed.

- [ ] **Step 4: Commit the plan and any formatting-only changes**

```bash
git add docs/superpowers/plans/2026-08-20-cloudflare-placeholder-truncation.md backend/internal/rss/content.go backend/internal/rss/content_test.go
git commit -m "docs: plan placeholder truncation rollout"
```

- [ ] **Step 5: Integrate and push**

Fast-forward the verified commits onto local `master`, push `master`, and record the pushed SHA. Preserve all unrelated untracked files in the primary checkout.

### Task 4: Deploy and selectively re-fetch recent affected rows

**Files:**
- Runtime only: Tencent `/opt/rss-pal` checkout and database.

- [ ] **Step 1: Wait for and verify deployment**

Verify the GitHub Actions deployment run succeeded, Tencent `/opt/rss-pal` matches the pushed SHA, and `docker compose ps` reports healthy application services.

- [ ] **Step 2: Freeze the candidate inventory**

Run a read-only PostgreSQL query selecting `id`, `octet_length(content)`, `fetched_at`, and the matching reason for rows with `fetched_at >= now() - interval '72 hours'` and either `content LIKE '%data:image%'` or `right(content, 3) = '...'`. Save this exact result outside the repository as the audit list.

- [ ] **Step 3: Re-fetch the fixed ID set serially**

For each selected ID, invoke the existing authenticated `POST /api/articles/:id/content` route from the production host, stopping and recording the error for any failed item. Do not re-query to widen the ID set during execution.

- [ ] **Step 4: Verify repaired rows**

Query the fixed ID set for processing state, error, content byte length, `data:image` occurrence, and final text. Confirm article 2608 ends with the source conclusion about gradual RFC 9234 deployment and vendor support.

- [ ] **Step 5: Verify runtime health**

Check the host-local API health endpoint and `https://rss.morefreeze.top/api/health`; verify both return HTTP 200 and the deployed version/revision is current.
