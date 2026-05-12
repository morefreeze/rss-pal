# link_set — Design

**Date:** 2026-05-12
**Scope:** backend + frontend + worker + migrations
**Files touched:**
- backend: `internal/model/model.go`, `internal/repository/feed.go`, `internal/repository/article.go`, new `internal/repository/link_set.go`, `internal/api/feed.go`, `internal/api/article.go`, new `internal/rss/linkset_extract.go`, `internal/rss/fetcher.go`, new `internal/service/linkset_rank.go`
- worker: `cmd/worker/main.go`, new `cmd/worker/linkset.go`
- migration: new `migrations/020_link_set.sql`
- frontend: `src/pages/FeedListPage.tsx`, `src/pages/ArticlePage.tsx`, `src/pages/RecommendedPage.tsx`, `src/api/client.ts`, new `src/components/LinkSetChildren.tsx`

## Problem

A `link_set` is any source whose primary content is a list of outbound links — Buttondown newsletters (e.g. Hacker Newsletter), HN-style digests, awesome-list snapshots, "weekly reading" emails. The user wants each outbound link to be treated as a first-class article (full content fetch + AI summary), and wants the recommendation engine to surface the best of them across all subscribed link_sets.

The existing system can subscribe to such sources via `feed_type='rss'` or `feed_type='html'`, but it stores each RSS item / HTML link as a single flat article. The body's outbound links are not expanded into independent articles, so they are not summarized and not scoreable by the recommendation engine.

## Concept

- **link_set parent** — an article whose body is a curated list of outbound links. Marked with `articles.is_link_set = true`. Retains its original body (editor commentary + the list itself).
- **link_set child** — an outbound link extracted from a parent's body, promoted to a normal article record. Marked with `articles.parent_article_id IS NOT NULL`. Has its own title, URL, content, summary, reading progress, like/save state, share token.
- **`feeds.expand_links`** — a boolean flag on the feed. When `true`, every article ingested into this feed is treated as a link_set parent and its links expanded by the worker. When `false`, the feed behaves exactly as today.

The user opts a feed into link_set semantics **at subscribe time** via a checkbox in the preview→confirm flow.

## User stories

**S1 — Subscribe to a newsletter that's a link_set.**
User pastes `https://buttondown.com/hacker-newsletter`. Preview discovers `/rss`, fetches it, shows the latest 10 issues. User ticks "这是 link_set（爆开其中的链接）" and confirms. Future issues are auto-fetched. Each issue is a parent article; opening it shows children sorted by personalised score.

**S2 — Process a single issue one-off.**
User pastes `https://buttondown.com/hacker-newsletter/archive/793/`. Preview tries the URL directly (not RSS), tries parent-path RSS probes (`/rss`, `/feed`, `/atom.xml`) — finds `https://buttondown.com/hacker-newsletter/rss`. Preview returns two options: `subscribe_to_feed_url` and `process_as_oneoff`. User clicks "只处理这一期"; a pseudo-feed (`feed_type='link_set'`, `is_active=false`, `expand_links=true`) is created with the one URL as its only parent article. Children are extracted and processed.

**S3 — Reading a link_set parent.**
User opens HN newsletter #793 from the article list. The parent's editor intro renders normally at the top. Below it, a "本期链接" section lists children sorted by pre-rank score: top 5 rendered as cards with title + summary preview; the rest as compact stubs ("点击展开摘要"). Clicking a card opens the child as a normal article. Clicking a stub triggers fetch+summary in the background.

**S4 — Cross-issue recommendation.**
On `RecommendedPage`, a new section "本周精选 link_set 链接" lists processed (non-stub) children from link_set parents fetched in the last 7 days, ordered by the existing recommendation score formula. Stubs are excluded until processed.

## Schema (migration 020)

```sql
-- 020_link_set.sql

ALTER TABLE feeds
  ADD COLUMN IF NOT EXISTS expand_links BOOLEAN NOT NULL DEFAULT false;

ALTER TABLE articles
  ADD COLUMN IF NOT EXISTS is_link_set        BOOLEAN NOT NULL DEFAULT false,
  ADD COLUMN IF NOT EXISTS parent_article_id  INT REFERENCES articles(id) ON DELETE CASCADE,
  ADD COLUMN IF NOT EXISTS processing_state   VARCHAR(16) NOT NULL DEFAULT 'ready',
  -- 'ready' (normal article), 'stub' (link_set child awaiting fetch+summary),
  -- 'processing' (in-flight), 'failed' (fetch or summary failed permanently)
  ADD COLUMN IF NOT EXISTS prerank_score      FLOAT,
  ADD COLUMN IF NOT EXISTS editor_note        TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_articles_parent
  ON articles(parent_article_id) WHERE parent_article_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_articles_link_set
  ON articles(feed_id, fetched_at DESC) WHERE is_link_set = true;

CREATE INDEX IF NOT EXISTS idx_articles_stubs
  ON articles(id) WHERE processing_state = 'stub';

-- Existing UNIQUE(feed_id, url) would block re-use of the same URL across
-- different parents in the same feed (e.g. two HN newsletter issues both
-- link to the same blog post). Keep it for top-level articles, but allow
-- children to coexist by scoping their uniqueness to (parent_article_id, url).
-- Implemented as a partial unique index alongside the existing constraint.
CREATE UNIQUE INDEX IF NOT EXISTS uniq_link_set_child_url
  ON articles(parent_article_id, url) WHERE parent_article_id IS NOT NULL;

-- Relax the existing feed_id+url uniqueness for child rows. The original
-- constraint was created without WHERE; replace it with a partial that
-- excludes children (children are governed by uniq_link_set_child_url above).
ALTER TABLE articles DROP CONSTRAINT IF EXISTS articles_feed_id_url_key;
CREATE UNIQUE INDEX IF NOT EXISTS uniq_articles_feed_url_no_child
  ON articles(feed_id, url) WHERE parent_article_id IS NULL;
```

**Why a new feed_type='link_set' for one-off:** the existing `feed_type='saved'` is the bookmarklet store; reusing it would conflate two visibly different flows in the feed list UI. `'link_set'` is a clean new value; the worker skips it when `is_active=false`, which is the one-off default. (Subscription mode uses `feed_type='rss'` + `expand_links=true`.)

## Model additions

```go
// internal/model/model.go

type Feed struct {
    // ...existing fields...
    ExpandLinks bool `json:"expand_links" db:"expand_links"`
}

type Article struct {
    // ...existing fields...
    IsLinkSet       bool   `json:"is_link_set" db:"is_link_set"`
    ParentArticleID *int   `json:"parent_article_id,omitempty" db:"parent_article_id"`
    ProcessingState string `json:"processing_state" db:"processing_state"`
    PrerankScore    *float64 `json:"prerank_score,omitempty" db:"prerank_score"`
    EditorNote      string `json:"editor_note,omitempty" db:"editor_note"`
}

type AddFeedRequest struct {
    URL         string `json:"url"`
    FeedType    string `json:"feed_type"`
    ExpandLinks bool   `json:"expand_links"`
}

type PreviewResult struct {
    // ...existing fields...
    DiscoveredRSSURL string `json:"discovered_rss_url,omitempty"` // only set when probing parent path finds RSS
}
```

## Link extraction (`internal/rss/linkset_extract.go`)

Input: parent article's HTML content + the parent's own URL (for own-host filtering).
Output: ordered slice of `{Title, URL, EditorNote}` candidates.

```go
type Candidate struct {
    Title      string
    URL        string  // absolute, normalised (no #fragment, no utm_*)
    EditorNote string  // closest preceding/following text, ≤ 280 chars
}

func ExtractCandidates(html, parentURL string) []Candidate
```

**Algorithm** (goquery-based, reuses existing dependency):

1. Parse HTML; resolve all `<a href>` to absolute URLs using parent URL as base.
2. For each `<a>`, **reject** if:
   - `href` is empty, `mailto:`, `tel:`, `javascript:`, or a fragment-only `#…`.
   - host equals parent host or is a subdomain of it (own-site link).
   - host matches known social/share/unsubscribe denylist:
     `t.co`, `twitter.com/intent/`, `x.com/intent/`, `facebook.com/sharer`,
     `linkedin.com/shareArticle`, `reddit.com/submit`, `addtoany.com`,
     paths containing `/unsubscribe`, `/manage-subscription`, `/preferences`,
     `buttondown.com/<author>` *root* (newsletter homepage link),
     `buttondown.com/emails/`.
   - link text length < 6 chars after trimming **and** link contains only `<img>` (pure icon).
   - link text is one of `here`, `click`, `read more`, `more`, `link`, `→`, `↗` (case-insensitive).
3. Normalise URL: lowercase host, strip `utm_*`, `ref`, `mc_cid`, `mc_eid` query params, strip trailing slash, strip `#fragment`.
4. Dedup within the issue by normalised URL (keep first occurrence).
5. For each surviving link, extract `EditorNote`:
   - If link sits inside an `<li>` or `<p>`, take the text of that container minus the link's own text. Trim, collapse whitespace, cap at 280 chars.
   - Otherwise, take the immediate following sibling's text node (Buttondown's bullet-with-blurb pattern). Same trim/cap.
   - Empty string if nothing usable.
6. Return candidates in document order.

The function is pure and unit-tested with fixture files under `internal/rss/testdata/linkset/` (Buttondown sample issue, HN newsletter sample, awesome-list page).

## Pre-rank scoring

Lives in new `internal/service/linkset_rank.go` (needs DB access for interest_topics and historical host sets — sits in `service/` rather than `rss/`):

```go
func PrerankCandidates(ctx context.Context, candidates []Candidate, userID int, topicRepo *repository.InterestTopicRepository, prefRepo *repository.PreferenceRepository) []float64
```

Score per candidate, in `[0, 1]`:

- `+0.35` if any of user's top-10 `interest_topics` (by weight) appears as a case-insensitive substring of `Title + " " + EditorNote`.
- `+0.20` for the candidate's host appearing in user's "liked-host" set (hosts of articles the user liked/saved in the last 30 days; computed once per call).
- `+0.15` for the candidate's host appearing in user's "completed-read" set (host of articles with `reading_progress.is_completed = true` last 30 days).
- `-0.25` if the candidate's host appears in user's "disliked-host" set.
- `+0.05` baseline so candidates with no signal still get ordered by document position (stable sort).

The five small numbers add up to a bounded score and are easy to tune later. No ML; intentionally simple.

**Top-K rule:** `K = 5`. If the issue has ≤ 8 candidates, process all (the small-issue case where ranking provides no real saving). Otherwise process the top 5 by `prerank_score` and leave the rest as stubs.

## Worker pipeline (`cmd/worker/linkset.go`)

A new pass `processLinkSetParents` runs in the worker main loop, after the existing RSS fetch / HTML scrape passes, before the summarisation pass:

1. **Find parents needing expansion** — articles where `is_link_set = true` AND there are no rows in `articles` with `parent_article_id = this.id`. (Idempotent: re-running on the same parent is a no-op.)
2. For each such parent:
   1. Run `ExtractCandidates(parent.content, parent.url)`.
   2. Compute `PrerankCandidates` for the parent's owning user (via `feeds.owner_id`).
   3. Insert child rows in one transaction. Top-K go in with `processing_state='processing'`; the rest go in with `'stub'`. Common fields: `feed_id=parent.feed_id`, `parent_article_id=parent.id`, `title=candidate.Title`, `url=candidate.URL`, `editor_note=candidate.EditorNote`, `prerank_score=...`, `published_at=parent.published_at`, `content=''`, `summary_brief=''`, `summary_detailed=''`.
3. **Process queued children** — articles where `processing_state='processing'` (Top-K children + on-demand ones from the API trigger below):
   1. `ContentFetcher.FetchContent(url)` (reuses existing Jina fallback).
   2. On success: persist `content`, `word_count`, `reading_minutes`. Leave `processing_state='processing'` for now.
   3. On failure: increment `refetch_attempts` (existing column); after 3 failures, transition to `'failed'` and stop retrying.

**State machine:**
- `'stub'` — title+URL only, no content, no summary. Default for non-Top-K children.
- `'processing'` — has been queued; content fetch and/or summary still pending.
- `'ready'` — content present AND summary present. The summariser sets this when writing `summary_brief` for an article that was previously `'processing'`.
- `'failed'` — fetch retries exhausted. UI surfaces a "重试" button.

Concretely: the existing summariser pass (`internal/ai/summarizer.go` callers in `cmd/worker`) is extended to also transition `processing_state` from `'processing'` to `'ready'` when it writes a summary for a previously-`'processing'` article. Non-link_set articles default to `'ready'` from creation, so this code path is a no-op for them.

The parent article itself is **not** re-summarised after children are created; it keeps whatever summary the existing pipeline produced (or empty if newly created).

## API additions

### `POST /api/feeds/preview` — extended

Same endpoint. Response gains `discovered_rss_url` when the requested URL is not itself RSS but a `/rss`, `/feed`, or `/atom.xml` exists at the parent path. The frontend renders an extra "订阅整个 newsletter" button when this is non-empty.

### `POST /api/feeds` — extended

Accepts `expand_links: bool` in the body, persisted to `feeds.expand_links`.

### `POST /api/feeds/oneoff_link_set` — new

```json
Request:  { "url": "..." }
Response: { "feed_id": 42, "parent_article_id": 1337, "candidate_count": 27 }
```

Creates a `feed_type='link_set'`, `is_active=false`, `expand_links=true` pseudo-feed owned by the caller; inserts the URL as a single parent article (`is_link_set=true`); leaves child creation to the worker pass (runs within seconds). Frontend redirects to the parent article page after creation.

### `POST /api/articles/:id/expand` — new

User-triggered expansion of a single stub child. Body: empty. Response:
```json
{ "article_id": 9001, "state": "processing" }
```
Server transitions `processing_state` from `'stub'` to `'processing'`; the worker pass picks it up on its next cycle. 4xx if the article is not a stub.

### `GET /api/articles/:id` — extended

When the article has `is_link_set=true`, the response gains an additional field:

```json
"children": [
  {
    "id": 9001, "title": "...", "url": "...", "editor_note": "...",
    "processing_state": "ready", "prerank_score": 0.55,
    "summary_brief": "..."  // empty when state != "ready"
  },
  ...
]
```

Children are returned ordered by `prerank_score DESC, id ASC`.

### `GET /api/articles/recommended/link_set` — new

```
Query: ?days=7&limit=20
Response: [Article, ...]  // standard article shape
```

Returns processed children from link_set parents in the last `days` days, scored by the existing recommendation formula (`user_preferences` aggregated weights), filtered to `processing_state='ready'`. Used by the new `RecommendedPage` section.

## Frontend

### Subscribe flow (`FeedListPage.tsx`)

In the preview→confirm step, add a checkbox: **"这是 link_set（爆开其中的链接）"**. State held in the existing preview-modal state; sent as `expand_links` in the confirm POST.

When `preview.discovered_rss_url` is set and differs from the user's pasted URL, render an extra blue panel above the article list:

> 发现这个站点的 RSS：`https://buttondown.com/hacker-newsletter/rss`
> [订阅整个 newsletter]  [只处理这一期]

Clicking the first switches the form's URL to the discovered one and previews it. Clicking the second hits `POST /api/feeds/oneoff_link_set` with the pasted URL and routes to the parent article page.

### Article page (`ArticlePage.tsx`)

When the loaded article has `is_link_set=true`, render a new `<LinkSetChildren>` component below the article body:

- Heading: "本期链接（{N} 条）" where N is `children.length`.
- Top 5 children (by `prerank_score`) rendered as expanded cards: title (linked to child's `/articles/:id`), summary brief if `state='ready'`, `editor_note` as a muted subhead.
- Remaining children rendered as compact rows: title + URL host + a "展开摘要" button. Clicking the button calls `POST /api/articles/:id/expand`, swaps the button for a spinner, and polls `GET /api/articles/:id` every 4 seconds until `processing_state='ready'`.
- Children in `processing_state='failed'` render as muted rows with a "重试" button (re-triggers expand).

The component is self-contained and used only when `is_link_set=true`. The existing article view (body, summary, like/dislike/save toolbar) renders unchanged above it.

### RecommendedPage

Add a new section above the existing recommendation list:

> **本周精选 link_set 链接**

Renders the response of `GET /api/articles/recommended/link_set?days=7&limit=20` using the existing `ArticleCard` component (these children are normal articles with summaries; the card works as-is). If the response is empty, the section is hidden entirely (no empty-state copy).

## Edge cases

- **Parent body is empty / has no extractable links.** Worker pass logs and skips; `is_link_set` stays true so a future re-process attempt could be triggered. No children created. The UI's `<LinkSetChildren>` shows "本期没有提取到链接".
- **Two issues in the same feed link to the same URL.** Each issue gets its own child row, scoped by `(parent_article_id, url)`. The user sees the link in both parents; the recommendation feed deduplicates by article ID downstream of scoring, so the URL can still appear twice — acceptable v1 cost.
- **Child URL coincides with an existing top-level article in the same feed.** Allowed by the partial unique indexes: the existing article and the new child coexist as separate rows. Likes/reads on one don't transfer to the other (intentional simplification — they came from different contexts).
- **A child URL is itself a link_set page** (e.g. HN newsletter links to "this week's best of awesome-list"). No recursive expansion in v1. The child is fetched and summarised as a normal article.
- **Paywalled / 403 / Cloudflare-blocked URL.** `ContentFetcher.FetchContent` already falls back to Jina Reader. If Jina also fails, the existing `refetch_attempts < 3` rule applies; after 3 attempts the child transitions to `'failed'`.
- **User edits feed to flip `expand_links`.** Out of scope. The flag is set at subscribe time and not editable in v1 (no API or UI). Future-work note only.
- **One-off URL is itself RSS.** `Preview` already detects this. The "只处理这一期" path is not offered (the discovered_rss_url panel surfaces the subscribe path instead).
- **Worker restart mid-expansion.** Children left in `'processing'` are re-picked up by the next pass; `FetchContent` is idempotent on the article level (re-writing `content` is harmless). No partial-state cleanup needed.

## Out of scope (v1)

- **"Rolling" link_sets** where the same URL re-loads with different content over time (HN front page, awesome-list main page). v1 only handles snapshot pages (subscribed via RSS to discrete issues, or one-off paste).
- **User-editable `expand_links` after creation.** Flag is immutable post-subscribe.
- **Auto-detecting "this is a link_set" heuristically.** Always opt-in.
- **Recursive expansion** (child that is itself a link_set).
- **De-duplication across users / across feeds.** Each parent gets its own children.
- **Sharing a link_set parent** (the existing share-token flow works on individual articles; sharing a parent does not auto-share children). Children have their own share tokens via the existing endpoint.
- **Per-feed analytics on link_set hit rate.** No "Top-K hit rate" dashboard.
- **Worker concurrency tuning.** Children are processed one at a time within the worker pass, same as existing articles. If 30 children land in one issue, processing spans several worker cycles — acceptable.
