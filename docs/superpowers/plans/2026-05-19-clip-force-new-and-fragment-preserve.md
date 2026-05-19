# Clip force-new + fragment-preserving URL norm — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix two bookmarklet/extension capture pain points — false-positive dedup on hash-route SPAs (Gmail-like), and the lack of a "create a separate article" option in the duplicate prompt.

**Architecture:** Two coupled changes. (B) `util.NormalizeURLKeepFragment` is used only on the clip path so fragments survive (Gmail `#inbox/abc` ≠ `#inbox/def`). (A) `force_new` flag in the capture request bypasses dedup and inserts a new row; the DB allows it because a new `articles.is_clip` column carves clip articles out of the `(feed_id, url)` unique index. The extension popup gains a 新建 button between 覆盖 and 取消.

**Tech Stack:** Go 1.24 (Gin, `database/sql` + `lib/pq`), PostgreSQL 15, MV3 Chrome extension (vanilla JS), Docker Compose.

**Key spec reference:** `docs/superpowers/specs/2026-05-19-clip-force-new-and-fragment-preserve-design.md`

**Testing posture (matches repo):** Pure-function tests where the function is pure (`urlnorm_test.go`). No new HTTP-handler integration tests — the repo doesn't have any today and the spec's "force_new" path is a straight early-return that's easier validated by manual smoke than by setting up a test postgres harness. Manual smoke steps are spelled out in T5 and T7.

---

## File map

**Create**
- `backend/migrations/025_articles_is_clip.sql`

**Modify**
- `backend/internal/util/urlnorm.go` — new `NormalizeURLKeepFragment` helper
- `backend/internal/util/urlnorm_test.go` — three cases for the new helper
- `backend/internal/model/model.go` — add `IsClip bool` to `Article` (verify the file path with `grep -l "type Article struct" backend/internal/model/`)
- `backend/internal/repository/article.go` — extend `Create` INSERT with `is_clip`; add `ORDER BY a.fetched_at DESC` to `FindByOwnerAndURL`
- `backend/internal/api/bookmarklet.go` — swap `NormalizeURL` → `NormalizeURLKeepFragment`; accept `force_new`; set `IsClip=true` on Create
- `extension/popup.html` — add 新建 button
- `extension/popup.js` — handler + extended `sendToServer` signature
- `extension/manifest.json` — version bump

---

## Tasks

### Task 1: Add the `is_clip` migration file

**Files:**
- Create: `backend/migrations/025_articles_is_clip.sql`

- [ ] **Step 1: Write the migration**

```sql
-- 025_articles_is_clip.sql
-- Add is_clip boolean to articles so the dedup unique index can exclude
-- clip-bin captures. The clip path needs to allow multiple rows sharing
-- (feed_id, url) — the user explicitly clicks 新建 to keep both when two
-- captures legitimately have the same URL but different content.

ALTER TABLE articles
  ADD COLUMN IF NOT EXISTS is_clip BOOLEAN NOT NULL DEFAULT false;

UPDATE articles SET is_clip = true
  WHERE feed_id IN (SELECT id FROM feeds WHERE feed_type = 'clip');

DROP INDEX IF EXISTS uniq_articles_feed_url_no_child;
CREATE UNIQUE INDEX uniq_articles_feed_url_no_child
  ON articles(feed_id, url)
  WHERE parent_article_id IS NULL AND NOT is_clip;
```

- [ ] **Step 2: Commit**

```bash
git add backend/migrations/025_articles_is_clip.sql
git commit -m "feat(migration): 025 add articles.is_clip and narrow dedup index"
```

> **Do not apply yet.** Backend code doesn't reference the column until Task 3+4. Migration is applied in Task 5.

---

### Task 2: `NormalizeURLKeepFragment` + tests

**Files:**
- Modify: `backend/internal/util/urlnorm.go`
- Modify: `backend/internal/util/urlnorm_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `backend/internal/util/urlnorm_test.go` (after the existing `TestNormalizeURL` block):

```go
func TestNormalizeURLKeepFragment(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"keeps simple fragment", "https://example.com/a#section", "https://example.com/a#section"},
		{"keeps gmail hash-route", "https://mail.google.com/mail/u/0/#inbox/abc", "https://mail.google.com/mail/u/0/#inbox/abc"},
		{"keeps hash-bang route", "https://example.com/app#!/page/1", "https://example.com/app#!/page/1"},
		{"strips utm while keeping fragment", "https://Example.com/a?utm_source=x&id=1#sec", "https://example.com/a?id=1#sec"},
		{"lowercases host but keeps fragment", "https://EXAMPLE.com/path#frag", "https://example.com/path#frag"},
		{"unparseable returned as-is", "not a url", "not a url"},
		{"clean url without fragment unchanged", "https://example.com/a", "https://example.com/a"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeURLKeepFragment(tt.in)
			if got != tt.want {
				t.Errorf("NormalizeURLKeepFragment(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /Users/bytedance/mygit/rss-pal/backend && /Users/bytedance/sdk/go1.24.10/bin/go test ./internal/util/ -run TestNormalizeURLKeepFragment 2>&1 | tail -5
```

Expected: FAIL with `undefined: NormalizeURLKeepFragment` (compile error).

- [ ] **Step 3: Implement the helper**

In `backend/internal/util/urlnorm.go`, append after the closing brace of `NormalizeURL`:

```go
// NormalizeURLKeepFragment is like NormalizeURL but preserves the URL's
// fragment. Used by the clip-capture path (bookmarklet / extension), where
// hash-route SPAs (Gmail, Bilibili, ...) put the real page identity after
// '#'. Stripping it there produces false-positive duplicates across distinct
// emails / videos that share a stable base URL.
//
// Inputs that fail to parse are returned unchanged so the caller can still
// match exotic URLs by exact-string equality.
func NormalizeURLKeepFragment(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return raw
	}

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

	// Twitter / X canonicalization — same rules as NormalizeURL so a tweet
	// captured via clip is dedup-comparable with a tweet from an RSS feed.
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

> Note: code is intentionally a near-copy of `NormalizeURL` minus the two `u.Fragment = ""` / `u.RawFragment = ""` lines. Don't try to factor a common helper now — duplication is cheaper than the wrong abstraction here.

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd /Users/bytedance/mygit/rss-pal/backend && /Users/bytedance/sdk/go1.24.10/bin/go test ./internal/util/ 2>&1 | tail -5
```

Expected: `ok ... internal/util` with both `TestNormalizeURL` and `TestNormalizeURLKeepFragment` passing.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/util/urlnorm.go backend/internal/util/urlnorm_test.go
git commit -m "feat(util): NormalizeURLKeepFragment for clip-path URL dedup"
```

---

### Task 3: `Article.IsClip` field + `Create` insert + `FindByOwnerAndURL` ordering

**Files:**
- Modify: `backend/internal/model/model.go` (the `Article` struct)
- Modify: `backend/internal/repository/article.go` (the `Create` function around line 402, `FindByOwnerAndURL` around line 466)

- [ ] **Step 1: Add `IsClip` to `model.Article`**

First locate the struct: `grep -n "type Article struct" backend/internal/model/model.go` — confirm line. Then add a field. Inside the `Article` struct definition, place `IsClip` next to other boolean flags (e.g., near `IsRead`, `IsLinkSet`, or `LinksExtendable` — pick whichever is closest in the existing layout). Use this exact line:

```go
	IsClip bool `json:"is_clip,omitempty"`
```

If unsure where, place it at the end of the struct (just before the closing `}`).

- [ ] **Step 2: Extend `Create` INSERT**

In `backend/internal/repository/article.go`, the `Create` function around line 402–419 currently has:

```go
func (r *ArticleRepository) Create(article *model.Article) error {
	query := `INSERT INTO articles (feed_id, title, url, content, published_at, word_count, reading_minutes, media_url, media_type, media_duration_seconds, parent_article_id, processing_state, prerank_score, editor_note) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14) RETURNING id, fetched_at`
	...
	return r.db.QueryRow(query, article.FeedID, article.Title, article.URL, article.Content, article.PublishedAt, article.WordCount, article.ReadingMinutes, mediaURL, mediaType, mediaDuration, parentArticleID, state, prerankScore, article.EditorNote).Scan(&article.ID, &article.FetchedAt)
}
```

Add `is_clip` to the columns and `$15` to the values, and append `article.IsClip` to the QueryRow args. Replacement (full body shown — copy verbatim):

```go
func (r *ArticleRepository) Create(article *model.Article) error {
	query := `INSERT INTO articles (feed_id, title, url, content, published_at, word_count, reading_minutes, media_url, media_type, media_duration_seconds, parent_article_id, processing_state, prerank_score, editor_note, is_clip) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15) RETURNING id, fetched_at`
	mediaURL := nullableString(article.MediaURL)
	mediaType := nullableString(article.MediaType)
	mediaDuration := nullableInt(article.MediaDurationSeconds)
	var parentArticleID sql.NullInt64
	if article.ParentArticleID != nil {
		parentArticleID = sql.NullInt64{Int64: int64(*article.ParentArticleID), Valid: true}
	}
	var prerankScore sql.NullFloat64
	if article.PrerankScore != nil {
		prerankScore = sql.NullFloat64{Float64: *article.PrerankScore, Valid: true}
	}
	state := article.ProcessingState
	if state == "" {
		state = "ready"
	}
	return r.db.QueryRow(query, article.FeedID, article.Title, article.URL, article.Content, article.PublishedAt, article.WordCount, article.ReadingMinutes, mediaURL, mediaType, mediaDuration, parentArticleID, state, prerankScore, article.EditorNote, article.IsClip).Scan(&article.ID, &article.FetchedAt)
}
```

- [ ] **Step 3: Add explicit ordering to `FindByOwnerAndURL`**

In `backend/internal/repository/article.go` around line 466, the SQL currently ends with `LIMIT 1`. Change:

```go
	query := `
		SELECT a.id, a.feed_id, a.title, a.url, a.content, a.published_at, a.summary_brief, a.summary_detailed, a.fetched_at, a.links_extendable, a.parent_article_id, a.processing_state, a.prerank_score, a.editor_note
		FROM articles a
		JOIN feeds f ON a.feed_id = f.id
		WHERE (f.owner_id IS NULL OR f.owner_id = $1) AND a.url = $2
		LIMIT 1
	`
```

to:

```go
	query := `
		SELECT a.id, a.feed_id, a.title, a.url, a.content, a.published_at, a.summary_brief, a.summary_detailed, a.fetched_at, a.links_extendable, a.parent_article_id, a.processing_state, a.prerank_score, a.editor_note
		FROM articles a
		JOIN feeds f ON a.feed_id = f.id
		WHERE (f.owner_id IS NULL OR f.owner_id = $1) AND a.url = $2
		ORDER BY a.fetched_at DESC
		LIMIT 1
	`
```

This is a no-op for the single-row dominant case and returns the most recent capture when multiple clip articles share a URL (after T4–T5 enable that).

- [ ] **Step 4: Build**

```bash
cd /Users/bytedance/mygit/rss-pal/backend && /Users/bytedance/sdk/go1.24.10/bin/go build ./... 2>&1 | tail -5
```

Expected: empty (success). If errors mention `IsClip` undefined in scanner code for `Article`, it's because we're only adding to the INSERT and not the SELECT scans — that's intentional. The field defaults to false on SELECT; we never need to read it back.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/model/model.go backend/internal/repository/article.go
git commit -m "feat(article): is_clip column on Create; FindByOwnerAndURL ORDER BY fetched_at DESC"
```

> Build compiles but **runtime will fail any insert** until Task 5 applies the migration (column doesn't exist yet). That's expected — don't deploy/restart between T3 and T5.

---

### Task 4: Wire `force_new` + `NormalizeURLKeepFragment` into `bookmarklet.Capture`

**Files:**
- Modify: `backend/internal/api/bookmarklet.go` (around lines 103–134 for the request struct and URL normalization, around lines 222–228 for the article creation path)

- [ ] **Step 1: Add `ForceNew` to the request struct**

Around line 103, the current struct is:

```go
var req struct {
	URL   string `json:"url"`
	Title string `json:"title"`
	HTML  string `json:"html"`
	Force bool   `json:"force"`
}
```

Replace with:

```go
var req struct {
	URL      string `json:"url"`
	Title    string `json:"title"`
	HTML     string `json:"html"`
	Force    bool   `json:"force"`
	ForceNew bool   `json:"force_new"`
}
```

- [ ] **Step 2: Use the fragment-preserving normalizer**

Line 124:

```go
normalized := util.NormalizeURL(req.URL)
```

→

```go
normalized := util.NormalizeURLKeepFragment(req.URL)
```

- [ ] **Step 3: Gate the dedup branch on `ForceNew`**

Around line 161–213 the handler currently has:

```go
existing, err := h.articleRepo.FindByOwnerAndURL(user.ID, normalized)
if err != nil {
	log.Printf("bookmarklet: lookup failed for user=%d url=%s: %v", user.ID, normalized, err)
	c.JSON(http.StatusInternalServerError, gin.H{"error": "查询文章失败"})
	return
}

if existing != nil {
	// ... shouldPromptDuplicate / UpdateContent / UpdateSummary / response ...
}

feed, err := h.feedRepo.GetOrCreateClipFeed(user.ID)
...
```

Wrap the lookup + branch so `ForceNew` skips it:

```go
if !req.ForceNew {
	existing, err := h.articleRepo.FindByOwnerAndURL(user.ID, normalized)
	if err != nil {
		log.Printf("bookmarklet: lookup failed for user=%d url=%s: %v", user.ID, normalized, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询文章失败"})
		return
	}

	if existing != nil {
		// ...existing body unchanged: shouldPromptDuplicate / UpdateContent / UpdateSummary / response...
		// (DO NOT modify the inner block; only wrap the outer if-block in the new `if !req.ForceNew` guard.)
	}
}

feed, err := h.feedRepo.GetOrCreateClipFeed(user.ID)
...
```

> Implementation detail for the engineer: rather than re-indenting ~50 lines, you can leave the existing structure in place and add an early-bypass:
>
> ```go
> var existing *model.Article
> if !req.ForceNew {
>     var err error
>     existing, err = h.articleRepo.FindByOwnerAndURL(user.ID, normalized)
>     if err != nil {
>         log.Printf("bookmarklet: lookup failed for user=%d url=%s: %v", user.ID, normalized, err)
>         c.JSON(http.StatusInternalServerError, gin.H{"error": "查询文章失败"})
>         return
>     }
> }
> if existing != nil {
>     // unchanged body
> }
> // fall through to Create path
> ```
>
> Pick whichever shape produces the smaller diff against the current file. Both are equivalent.

- [ ] **Step 4: Set `IsClip=true` on the created article**

Around line 222 the current code is:

```go
article := &model.Article{
	FeedID:      feed.ID,
	Title:       title,
	URL:         normalized,
	Content:     content,
	PublishedAt: publishedAt,
}
```

Add `IsClip: true`:

```go
article := &model.Article{
	FeedID:      feed.ID,
	Title:       title,
	URL:         normalized,
	Content:     content,
	PublishedAt: publishedAt,
	IsClip:      true,
}
```

- [ ] **Step 5: Build**

```bash
cd /Users/bytedance/mygit/rss-pal/backend && /Users/bytedance/sdk/go1.24.10/bin/go build ./... 2>&1 | tail -5
```

Expected: empty (success).

- [ ] **Step 6: Commit**

```bash
git add backend/internal/api/bookmarklet.go
git commit -m "feat(bookmarklet): force_new flag bypasses dedup; preserve URL fragment"
```

> Runtime still requires the migration. Task 5 applies it and restarts services.

---

### Task 5: Apply migration + restart api/worker

> **DB safety gate — pause for explicit user confirmation before running Step 3.** Per project memory, DB-touching actions need backup + dry-run + per-turn confirmation. The implementer should run Steps 1–2 (backup, dry-run), report results to the user, then wait for an OK before Step 3.

**Files:** None (operations only)

- [ ] **Step 1: Take a backup**

```bash
cd /Users/bytedance/mygit/rss-pal && BACKUP_FILE="backups.pre-migrate-$(date +%Y%m%d-%H%M%S).sql" && docker-compose exec -T postgres pg_dump -U postgres rsspal > "$BACKUP_FILE" && ls -la "$BACKUP_FILE"
```

Expected: a multi-MB `.sql` file appears in the repo root.

- [ ] **Step 2: Dry-run inspection**

```bash
docker-compose exec -T postgres psql -U postgres -d rsspal -c "SELECT COUNT(*) FROM articles;"
docker-compose exec -T postgres psql -U postgres -d rsspal -c "\d articles" | grep -E "is_clip|uniq_articles_feed_url_no_child"
docker-compose exec -T postgres psql -U postgres -d rsspal -c "SELECT feed_type, COUNT(*) FROM feeds GROUP BY feed_type ORDER BY feed_type;"
```

Expected: a count of articles (note it), no `is_clip` column yet, the existing `uniq_articles_feed_url_no_child` index showing `WHERE parent_article_id IS NULL`, and a `clip` feed_type with a positive count (from the previous spec's migration).

> **PAUSE HERE. Report to user and wait for explicit OK before applying.**

- [ ] **Step 3: Apply migration (after user OK)**

```bash
docker-compose exec -T postgres psql -U postgres -d rsspal < backend/migrations/025_articles_is_clip.sql
```

Expected output:
```
ALTER TABLE
UPDATE N    (where N matches the count of clip-feed articles from step 2)
DROP INDEX
CREATE INDEX
```

- [ ] **Step 4: Verify post-state**

```bash
docker-compose exec -T postgres psql -U postgres -d rsspal -c "SELECT is_clip, COUNT(*) FROM articles GROUP BY is_clip ORDER BY is_clip;"
docker-compose exec -T postgres psql -U postgres -d rsspal -c "\d articles" | grep -A1 "uniq_articles_feed_url_no_child"
```

Expected: rows for `is_clip=true` (matching the clip article count) and `is_clip=false` (everything else). The index predicate should now read `WHERE parent_article_id IS NULL AND NOT is_clip`.

- [ ] **Step 5: Rebuild + restart api+worker**

```bash
cd /Users/bytedance/mygit/rss-pal && docker-compose up -d --build api worker 2>&1 | tail -10
sleep 3
docker-compose logs --tail=10 api 2>&1 | tail -10
```

Expected: api and worker recreate cleanly, "Server starting on port 8080" in logs.

- [ ] **Step 6: Smoke-test endpoint shape**

```bash
curl -s -o /dev/null -w "HTTP %{http_code}\n" "http://localhost:8080/api/bookmarklet/capture"
```

Expected: HTTP 401 (no token) or 405 (method not allowed — GET vs POST). NOT 500. That confirms the handler is alive and reachable.

- [ ] **Step 7: No commit needed**

This task is operational only.

---

### Task 6: Extension UI — 新建 button + version bump

**Files:**
- Modify: `extension/popup.html` (the duplicate-prompt block, currently around lines 35–44)
- Modify: `extension/popup.js` (the `sendToServer` signature around line 223, the `lastCapture`-bound overwrite handler around line 290, and an analogous new handler for 新建; also a `newBtn` ref near the top)
- Modify: `extension/manifest.json` (version bump)

- [ ] **Step 1: Add the 新建 button to popup.html**

In `extension/popup.html`, find the duplicate-prompt actions block (`<div class="duplicate-actions">` around lines 40–43):

```html
<div class="duplicate-actions">
  <button class="btn btn-danger" id="overwriteBtn">覆盖</button>
  <button class="btn btn-secondary" id="cancelBtn">取消</button>
</div>
```

Replace with:

```html
<div class="duplicate-actions">
  <button class="btn btn-danger" id="overwriteBtn">覆盖</button>
  <button class="btn btn-primary" id="newBtn">新建</button>
  <button class="btn btn-secondary" id="cancelBtn">取消</button>
</div>
```

- [ ] **Step 2: Wire the 新建 button in popup.js**

In `extension/popup.js`, at the top of the IIFE where other DOM refs are grabbed (search for `const overwriteBtn = $('overwriteBtn');` to find the right area), add:

```js
const newBtn = $('newBtn');
```

Update the `sendToServer` signature (currently at line 223):

```js
async function sendToServer(url, title, html, serverUrl, token, force) {
  const resp = await fetch(serverUrl.replace(/\/+$/, '') + '/api/bookmarklet/capture', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      'Authorization': 'Bearer ' + token,
    },
    body: JSON.stringify({ url, title, html, force: !!force }),
  });
  ...
}
```

→

```js
async function sendToServer(url, title, html, serverUrl, token, force, forceNew) {
  const resp = await fetch(serverUrl.replace(/\/+$/, '') + '/api/bookmarklet/capture', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      'Authorization': 'Bearer ' + token,
    },
    body: JSON.stringify({ url, title, html, force: !!force, force_new: !!forceNew }),
  });
  ...
}
```

(only the signature and the `body` line change — leave the rest of the function intact.)

Find the existing `overwriteBtn.addEventListener('click', async () => { ... })` block (around lines 290–306). Right after it, add a parallel handler for 新建:

```js
newBtn.addEventListener('click', async () => {
  if (!lastCapture) return;
  hideDuplicate();
  setLoading(newBtn, true);
  try {
    const result = await sendToServer(
      lastCapture.url, lastCapture.title, lastCapture.html,
      lastCapture.serverUrl, lastCapture.token, false, true
    );
    const link = buildArticleLink(lastCapture.serverUrl, result.article_id);
    showStatus('success', '✅ ' + escapeHtml(result.message || '已加入网摘') + link);
  } catch (err) {
    showStatus('error', '❌ ' + escapeHtml(err.message));
  } finally {
    setLoading(newBtn, false);
  }
});
```

(Note: the 6th arg is `false` — not forcing overwrite — and the 7th is `true` to set `force_new`.)

- [ ] **Step 3: Bump the manifest version**

In `extension/manifest.json` line 4:

```json
  "version": "1.2.2",
```

→

```json
  "version": "1.3.0",
```

(Minor bump — new user-visible button.)

- [ ] **Step 4: Sanity-check the extension files**

```bash
grep -n "newBtn\|force_new\|forceNew" /Users/bytedance/mygit/rss-pal/extension/popup.html /Users/bytedance/mygit/rss-pal/extension/popup.js
```

Expected: 4 hits — `newBtn` in popup.html (the button id), `newBtn` in popup.js (the ref grab + click handler + setLoading calls), `force_new` in popup.js (the JSON body), `forceNew` in popup.js (the function param + arg pass).

```bash
grep -n "\"version\":" /Users/bytedance/mygit/rss-pal/extension/manifest.json
```

Expected: `"version": "1.3.0",`.

- [ ] **Step 5: Commit**

```bash
git add extension/popup.html extension/popup.js extension/manifest.json
git commit -m "feat(extension): 新建 button in duplicate prompt; v1.3.0"
```

> The user reloads the extension in Chrome (chrome://extensions → 🔄 on RSS Pal). The new button appears in the duplicate prompt; the version label reads 1.3.0.

---

### Task 7: Manual smoke verification

**Files:** None (manual testing only)

These steps require the user's browser and a configured RSS Pal extension. Document the steps so the user can run through them. The implementer subagent should NOT attempt to drive a real browser — just write the checklist to a note or paste it into the report.

- [ ] **Step 1: Reload the extension in Chrome**

User: chrome://extensions → find "RSS Pal" → click the reload icon. Confirm version reads 1.3.0.

- [ ] **Step 2: Fragment-preserving normalization (B path)**

User: open a Gmail thread (or any hash-route SPA) → click extension → 发送. Then navigate to a DIFFERENT email at the same Gmail base URL → click extension → 发送 again.

Expected: both captures succeed with the green "✅ 已加入网摘" status. NO duplicate prompt on the second one. Both rows appear in `/articles?view=clip`.

- [ ] **Step 3: 新建 escape hatch (A path)**

User: clip the same exact URL twice (same Gmail email, no fragment change between captures). On the second attempt the duplicate prompt appears with 覆盖 / 新建 / 取消.

Click 新建. Expected: green "✅ 已加入网摘" status with a link to the new article ID. Visit `/articles?view=clip` — TWO rows with the same URL but distinct content.

- [ ] **Step 4: 覆盖 still works (regression check)**

User: clip the same URL once more. When the duplicate prompt appears, click 覆盖. Expected: the most-recently-clipped row (the latest by fetched_at) is updated in place; no new row appears.

- [ ] **Step 5: DB shape check**

```bash
docker-compose exec -T postgres psql -U postgres -d rsspal -c "SELECT id, is_clip, url FROM articles WHERE feed_id = (SELECT id FROM feeds WHERE feed_type='clip' LIMIT 1) ORDER BY id DESC LIMIT 10;"
```

Expected: recent clip articles all show `is_clip = t`. URLs preserve their fragments (e.g., `#inbox/...`).

- [ ] **Step 6: Backend log spot-check**

```bash
docker-compose logs --tail=30 api 2>&1 | grep "bookmarklet:" | tail -10
```

Expected: log lines for `created` and `updated` actions, no error or panic spam.

---

## Notes for the executing engineer

- **Do not skip Task 5.** Tasks 3 and 4 leave the build technically working but any clip insert will fail at runtime (the column doesn't exist) until the migration is applied. Don't conclude "broken" without checking DB state first.
- **`signal_type='save'` stays untouched.** This plan only touches the clip-bin path and a single column on `articles`. The unrelated star-signal logic (`?saved=true`, `savedOnly`, `signal_type='save'`) is not in scope.
- **Per project memory:** every change under `extension/` requires a `manifest.json` version bump. We bump in Task 6; if the implementer adds further extension tweaks, the version bump moves accordingly.
- **No Docker frontend rebuild needed.** This plan doesn't touch `frontend/src/`. The api+worker rebuild is in Task 5.
