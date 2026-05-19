# /recommended Page Improvements Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add fallback recommendations + parent-article visibility + provider-neutral copy on the `/recommended` page so the link_set section no longer goes empty when all candidates are marked completed.

**Architecture:** Split `GetLinkSetRecommendations` into a primary query (preference-ranked, excludes read) and a fallback query (quality-gated, allows read), combined by a pure Go helper that's unit-testable without DB. Frontend adds an ℹ️ help panel, parent-article link, fallback marker, and updated bestblogs copy.

**Tech Stack:** Go 1.24 (`lib/pq` for Postgres arrays), React 18 + TypeScript, Postgres 15, Docker Compose.

**Testing convention note:** The existing `backend/internal/repository/` package has **zero `_test.go` files** — the codebase convention is no DB-integration tests. To keep automated test coverage without adding new test infrastructure (sqlmock / testcontainers), the orchestration logic is extracted to a **pure helper** `combineLinkSetResults`, which is unit-tested with hand-built `[]model.Article` slices. The two SQL queries themselves get **manual verification** against the live DB in Task 11.

---

## File Structure

**Backend — created:**
- `backend/internal/repository/article_linkset_combine.go` — pure helper `combineLinkSetResults` + tiny ID-collection helper
- `backend/internal/repository/article_linkset_combine_test.go` — table-driven unit tests for the helper (4 scenarios)

**Backend — modified:**
- `backend/internal/model/model.go:24-47` — add `ParentTitle` and `IsFallback` fields to `Article` struct
- `backend/internal/repository/article.go:1188-1231` — replace `GetLinkSetRecommendations` with two query methods (primary + fallback) and a public method that calls the combinator; add `scanArticleWithParentTitle` scanner

**Frontend — modified:**
- `frontend/src/api/client.ts:100-126` — add `parent_title?` and `is_fallback?` to `Article` interface
- `frontend/src/pages/RecommendedPage.tsx` — add help panel, parent-article link in cards, fallback marker, updated bestblogs copy

---

## Task 1: Add `ParentTitle` and `IsFallback` fields to Article model

**Files:**
- Modify: `backend/internal/model/model.go:24-47`

- [ ] **Step 1: Edit the Article struct**

In `backend/internal/model/model.go`, change the `Article` struct definition (currently ending at line 47 with `MediaDurationSeconds`) to add two new fields **just before the closing brace**:

```go
type Article struct {
	ID                   int        `json:"id" db:"id"`
	FeedID               int        `json:"feed_id" db:"feed_id"`
	FeedTitle            string     `json:"feed_title,omitempty" db:"feed_title"`
	Title                string     `json:"title" db:"title"`
	URL                  string     `json:"url" db:"url"`
	Content              string     `json:"content" db:"content"`
	PublishedAt          *time.Time `json:"published_at" db:"published_at"`
	SummaryBrief         string     `json:"summary_brief" db:"summary_brief"`
	SummaryDetailed      string     `json:"summary_detailed" db:"summary_detailed"`
	FetchedAt            time.Time  `json:"fetched_at" db:"fetched_at"`
	WordCount            int        `json:"word_count" db:"word_count"`
	ReadingMinutes       int        `json:"reading_minutes" db:"reading_minutes"`
	IsRead               bool       `json:"is_read" db:"is_read"`
	LinksExtendable      *bool      `json:"links_extendable,omitempty" db:"links_extendable"`
	LinkSetSuggested     *bool      `json:"link_set_suggested,omitempty" db:"link_set_suggested"`
	ParentArticleID      *int       `json:"parent_article_id,omitempty" db:"parent_article_id"`
	ProcessingState      string     `json:"processing_state" db:"processing_state"`
	PrerankScore         *float64   `json:"prerank_score,omitempty" db:"prerank_score"`
	EditorNote           string     `json:"editor_note,omitempty" db:"editor_note"`
	MediaURL             string     `json:"media_url,omitempty" db:"media_url"`
	MediaType            string     `json:"media_type,omitempty" db:"media_type"`
	MediaDurationSeconds int        `json:"media_duration_seconds,omitempty" db:"media_duration_seconds"`
	// Transient fields populated by GetLinkSetRecommendations only — not stored in DB.
	ParentTitle string `json:"parent_title,omitempty"`
	IsFallback  bool   `json:"is_fallback,omitempty"`
}
```

- [ ] **Step 2: Build to verify the struct compiles**

Run: `cd backend && go build ./...`
Expected: builds cleanly. If any unrelated code uses field-by-field positional `Article{...}` literals, the compiler will complain — search for `model.Article{` and fix any positional literals (unlikely; the convention in this repo is named fields).

- [ ] **Step 3: Commit**

```bash
git add backend/internal/model/model.go
git commit -m "$(cat <<'EOF'
feat(model): add ParentTitle and IsFallback transient fields to Article

Used exclusively by GetLinkSetRecommendations to surface the parent
link_set article and mark fallback-recommended items.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 2: Add pure combinator helper + unit tests

**Files:**
- Create: `backend/internal/repository/article_linkset_combine.go`
- Create: `backend/internal/repository/article_linkset_combine_test.go`

- [ ] **Step 1: Write the failing tests first**

Create `backend/internal/repository/article_linkset_combine_test.go`:

```go
package repository

import (
	"errors"
	"testing"

	"github.com/bytedance/rss-pal/backend/internal/model"
)

func art(id int) model.Article { return model.Article{ID: id} }

func TestCombineLinkSetResults_PrimaryFull(t *testing.T) {
	primary := []model.Article{art(1), art(2), art(3)}
	fallbackCalled := false
	got, err := combineLinkSetResults(primary, func() ([]model.Article, error) {
		fallbackCalled = true
		return nil, nil
	}, 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fallbackCalled {
		t.Errorf("fallback should not be called when primary >= limit")
	}
	if len(got) != 3 {
		t.Fatalf("want 3 articles, got %d", len(got))
	}
	for i, a := range got {
		if a.IsFallback {
			t.Errorf("got[%d].IsFallback = true, want false", i)
		}
	}
}

func TestCombineLinkSetResults_PrimaryEmptyFallbackFills(t *testing.T) {
	primary := []model.Article{}
	fallback := []model.Article{art(10), art(11), art(12)}
	got, err := combineLinkSetResults(primary, func() ([]model.Article, error) {
		return fallback, nil
	}, 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("want 3 articles, got %d", len(got))
	}
	for i, a := range got {
		if !a.IsFallback {
			t.Errorf("got[%d].IsFallback = false, want true (id=%d)", i, a.ID)
		}
	}
}

func TestCombineLinkSetResults_PartialPrimaryPlusFallback(t *testing.T) {
	primary := []model.Article{art(1), art(2)}
	fallback := []model.Article{art(20), art(21), art(22)}
	got, err := combineLinkSetResults(primary, func() ([]model.Article, error) {
		return fallback, nil
	}, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 5 {
		t.Fatalf("want 5 articles, got %d", len(got))
	}
	if got[0].ID != 1 || got[1].ID != 2 {
		t.Errorf("primary order corrupted: got ids %d, %d", got[0].ID, got[1].ID)
	}
	if got[0].IsFallback || got[1].IsFallback {
		t.Errorf("primary articles should have IsFallback=false")
	}
	for i := 2; i < 5; i++ {
		if !got[i].IsFallback {
			t.Errorf("got[%d].IsFallback = false, want true", i)
		}
	}
}

func TestCombineLinkSetResults_FallbackErrorReturnsPrimary(t *testing.T) {
	primary := []model.Article{art(1)}
	got, err := combineLinkSetResults(primary, func() ([]model.Article, error) {
		return nil, errors.New("simulated fallback failure")
	}, 5)
	if err != nil {
		t.Fatalf("fallback error should be swallowed, got %v", err)
	}
	if len(got) != 1 || got[0].ID != 1 {
		t.Fatalf("want only primary article (id=1), got %v", got)
	}
	if got[0].IsFallback {
		t.Errorf("primary article should not be marked fallback")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd backend && go test ./internal/repository/ -run TestCombineLinkSetResults -v`
Expected: FAIL — `undefined: combineLinkSetResults`.

- [ ] **Step 3: Implement the helper**

Create `backend/internal/repository/article_linkset_combine.go`:

```go
package repository

import (
	"log"

	"github.com/bytedance/rss-pal/backend/internal/model"
)

// combineLinkSetResults merges primary recommendations with a fallback batch.
//
// Primary articles are marked IsFallback=false. If len(primary) >= limit,
// fallbackFn is not invoked. Otherwise fallbackFn supplies up to
// (limit - len(primary)) extra articles, which are appended and marked
// IsFallback=true.
//
// If fallbackFn returns an error, it is logged and the primary slice is
// returned alone — fallback is best-effort and must never block a
// successful primary result.
func combineLinkSetResults(
	primary []model.Article,
	fallbackFn func() ([]model.Article, error),
	limit int,
) ([]model.Article, error) {
	for i := range primary {
		primary[i].IsFallback = false
	}
	if len(primary) >= limit {
		return primary, nil
	}
	fallback, err := fallbackFn()
	if err != nil {
		log.Printf("link_set fallback query failed: %v", err)
		return primary, nil
	}
	for i := range fallback {
		fallback[i].IsFallback = true
	}
	return append(primary, fallback...), nil
}

// collectArticleIDs returns the IDs of all articles in the slice, in order.
// Used to build the exclude-list passed to the fallback query.
func collectArticleIDs(articles []model.Article) []int {
	ids := make([]int, len(articles))
	for i, a := range articles {
		ids[i] = a.ID
	}
	return ids
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd backend && go test ./internal/repository/ -run TestCombineLinkSetResults -v`
Expected: all 4 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/repository/article_linkset_combine.go backend/internal/repository/article_linkset_combine_test.go
git commit -m "$(cat <<'EOF'
feat(repo): add pure combineLinkSetResults helper with unit tests

Extracts the primary+fallback merge orchestration so it can be unit-tested
without DB infrastructure. Tested: primary-full skip, primary-empty
fallback-fills, partial-primary + fallback, fallback-error swallowed.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 3: Add `scanArticleWithParentTitle` scanner

**Files:**
- Modify: `backend/internal/repository/article.go` (insert new scanner right after the existing `scanArticleNoFeedTitle` block, which ends around line 168)

- [ ] **Step 1: Read the existing `scanArticleNoFeedTitle` to determine the exact insertion point**

Run: `grep -n "^func (r \*ArticleRepository)" backend/internal/repository/article.go | head -10`
Look at the line **after** `scanArticleNoFeedTitle` ends — insert the new function there.

- [ ] **Step 2: Insert the new scanner**

Add this function immediately after `scanArticleNoFeedTitle` (after its closing `}`):

```go
// scanArticleWithParentTitle is like scanArticleNoFeedTitle but expects an
// extra trailing column `parent_title` from a JOIN against the parent article.
// Used by GetLinkSetRecommendations primary and fallback queries.
func (r *ArticleRepository) scanArticleWithParentTitle(rows *sql.Rows) ([]model.Article, error) {
	var articles []model.Article
	for rows.Next() {
		var a model.Article
		var content, summaryBrief, summaryDetailed, mediaURL, mediaType, parentTitle sql.NullString
		var mediaDuration sql.NullInt64
		var linksExtendable sql.NullBool
		var parentArticleID sql.NullInt64
		var processingState, editorNote sql.NullString
		var prerankScore sql.NullFloat64
		err := rows.Scan(
			&a.ID, &a.FeedID, &a.Title, &a.URL, &content, &a.PublishedAt,
			&summaryBrief, &summaryDetailed, &a.FetchedAt, &a.WordCount,
			&a.ReadingMinutes, &mediaURL, &mediaType, &mediaDuration,
			&linksExtendable, &parentArticleID, &processingState,
			&prerankScore, &editorNote, &parentTitle,
		)
		if err != nil {
			return nil, err
		}
		a.Content = content.String
		a.SummaryBrief = summaryBrief.String
		a.SummaryDetailed = summaryDetailed.String
		if linksExtendable.Valid {
			v := linksExtendable.Bool
			a.LinksExtendable = &v
		}
		if parentArticleID.Valid {
			v := int(parentArticleID.Int64)
			a.ParentArticleID = &v
		}
		a.ProcessingState = processingState.String
		if prerankScore.Valid {
			v := prerankScore.Float64
			a.PrerankScore = &v
		}
		a.EditorNote = editorNote.String
		a.MediaURL = mediaURL.String
		a.MediaType = mediaType.String
		a.MediaDurationSeconds = int(mediaDuration.Int64)
		a.ParentTitle = parentTitle.String
		articles = append(articles, a)
	}
	return articles, nil
}
```

- [ ] **Step 3: Verify it compiles**

Run: `cd backend && go build ./...`
Expected: builds cleanly. The function is unused at this point — that's fine, the linter only warns on unused unexported functions, not errors out.

- [ ] **Step 4: Commit**

```bash
git add backend/internal/repository/article.go
git commit -m "$(cat <<'EOF'
feat(repo): add scanArticleWithParentTitle scanner

Identical to scanArticleNoFeedTitle but reads one extra trailing column
(parent_title) from a JOIN. Will be used by GetLinkSetRecommendations
primary + fallback queries in the next commit.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 4: Replace `GetLinkSetRecommendations` with primary + fallback queries

**Files:**
- Modify: `backend/internal/repository/article.go:1188-1231` (the existing `GetLinkSetRecommendations` function)

- [ ] **Step 1: Replace the existing function**

Delete the existing `GetLinkSetRecommendations` (function block at `article.go:1188-1231`) and replace it with three new functions:

```go
// GetLinkSetRecommendations returns processed children from link_set parents
// fetched in the last `days` days. It runs a primary preference-ranked query
// (excludes already-read) and tops up from a quality-gated fallback query
// (allows already-read) if primary returns fewer than `limit` results.
//
// Articles from the fallback are marked with IsFallback=true so the UI can
// label them. Both queries JOIN the parent article to surface parent_title.
func (r *ArticleRepository) GetLinkSetRecommendations(userID, days, limit int) ([]model.Article, error) {
	primary, err := r.queryLinkSetPrimary(userID, days, limit)
	if err != nil {
		return nil, err
	}
	return combineLinkSetResults(primary, func() ([]model.Article, error) {
		excludeIDs := collectArticleIDs(primary)
		return r.queryLinkSetFallback(userID, days, limit-len(primary), excludeIDs)
	}, limit)
}

// queryLinkSetPrimary is the preference-ranked recommendation query.
// Filters: ready, has parent, parent fetched within `days`, visible to user,
// not already completed. Ordered by (pref_score + prerank_score) DESC then
// published_at DESC.
func (r *ArticleRepository) queryLinkSetPrimary(userID, days, limit int) ([]model.Article, error) {
	query := `
		SELECT a.id, a.feed_id, a.title, a.url, a.content, a.published_at,
		       a.summary_brief, a.summary_detailed, a.fetched_at, a.word_count,
		       a.reading_minutes, a.media_url, a.media_type, a.media_duration_seconds,
		       a.links_extendable, a.parent_article_id, a.processing_state,
		       a.prerank_score, a.editor_note, parent.title AS parent_title
		FROM articles a
		JOIN articles parent ON a.parent_article_id = parent.id
		JOIN feeds f ON a.feed_id = f.id
		LEFT JOIN (
			SELECT article_id, SUM(
				CASE signal_type
					WHEN 'like' THEN 5.0 * signal_value
					WHEN 'dislike' THEN -10.0 * signal_value
					WHEN 'save' THEN 3.0 * signal_value
					WHEN 'read_duration' THEN signal_value / 60.0
					WHEN 'completed_listen' THEN 8.0 * signal_value
					ELSE 1.0 * signal_value
				END
			) AS pref_score
			FROM user_preferences
			WHERE created_at > NOW() - INTERVAL '30 days' AND user_id = $1
			GROUP BY article_id
		) p ON a.id = p.article_id
		LEFT JOIN reading_progress rp ON a.id = rp.article_id AND rp.user_id = $1
		WHERE a.processing_state = 'ready'
		  AND a.parent_article_id IS NOT NULL
		  AND parent.fetched_at > NOW() - ($2 || ' days')::INTERVAL
		  AND (f.owner_id IS NULL OR f.owner_id = $1)
		  AND COALESCE(rp.is_completed, false) = false
		ORDER BY COALESCE(p.pref_score, 0) + COALESCE(a.prerank_score, 0) DESC,
		         a.published_at DESC NULLS LAST
		LIMIT $3
	`
	rows, err := r.db.Query(query, userID, days, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return r.scanArticleWithParentTitle(rows)
}

// queryLinkSetFallback is the quality-gated fallback used when primary
// returns fewer than `limit`. Drops the pref_score JOIN and the
// is_completed=false filter, adds word_count >= 500 and summary_brief IS
// NOT NULL as quality gates, excludes IDs already returned by primary.
func (r *ArticleRepository) queryLinkSetFallback(userID, days, limit int, excludeIDs []int) ([]model.Article, error) {
	if limit <= 0 {
		return nil, nil
	}
	query := `
		SELECT a.id, a.feed_id, a.title, a.url, a.content, a.published_at,
		       a.summary_brief, a.summary_detailed, a.fetched_at, a.word_count,
		       a.reading_minutes, a.media_url, a.media_type, a.media_duration_seconds,
		       a.links_extendable, a.parent_article_id, a.processing_state,
		       a.prerank_score, a.editor_note, parent.title AS parent_title
		FROM articles a
		JOIN articles parent ON a.parent_article_id = parent.id
		JOIN feeds f ON a.feed_id = f.id
		WHERE a.processing_state = 'ready'
		  AND a.parent_article_id IS NOT NULL
		  AND parent.fetched_at > NOW() - ($2 || ' days')::INTERVAL
		  AND (f.owner_id IS NULL OR f.owner_id = $1)
		  AND a.word_count >= 500
		  AND a.summary_brief IS NOT NULL
		  AND a.summary_brief <> ''
		  AND NOT (a.id = ANY($4::int[]))
		ORDER BY a.prerank_score DESC NULLS LAST,
		         a.published_at DESC NULLS LAST
		LIMIT $3
	`
	rows, err := r.db.Query(query, userID, days, limit, pq.Array(excludeIDs))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return r.scanArticleWithParentTitle(rows)
}
```

**Note on `summary_brief`**: in this schema the column is `TEXT NOT NULL DEFAULT ''`, so `IS NOT NULL` alone isn't enough — also check `<> ''` to truly require AI-generated content. (Verified via `\d articles` against the live DB during spec drafting; both filters are belt-and-suspenders.)

- [ ] **Step 2: Verify `pq` is already imported**

Run: `grep '"github.com/lib/pq"' backend/internal/repository/article.go`
Expected output: `"github.com/lib/pq"` — already present at line 10 (verified during planning). If missing for any reason, add it to the import block.

- [ ] **Step 3: Build to verify**

Run: `cd backend && go build ./...`
Expected: builds cleanly.

- [ ] **Step 4: Run the combinator tests again (they still pass — pure helper untouched)**

Run: `cd backend && go test ./internal/repository/ -v`
Expected: 4 PASS, 0 FAIL.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/repository/article.go
git commit -m "$(cat <<'EOF'
feat(repo): split GetLinkSetRecommendations into primary + fallback

Primary: existing preference-ranked query (excludes is_completed).
Fallback: new quality-gated query (word_count >= 500 AND non-empty
summary_brief) that allows already-read articles to surface when primary
returns < limit. Combined by combineLinkSetResults; both queries return
parent_title via JOIN.

Fixes: /recommended link_set section being empty when all candidates are
marked completed.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 5: Manual live-DB verification of the new queries

> No `git` step here — read-only SELECT against the running Docker postgres. Goal: confirm both queries return what we expect on the user's actual data.

- [ ] **Step 1: Sanity-check primary still returns 0 for the known case**

Run:
```bash
docker exec rss-pal-postgres-1 psql -U postgres -d rsspal -c "
SELECT COUNT(*) AS primary_count FROM (
  SELECT a.id FROM articles a
  JOIN articles parent ON a.parent_article_id = parent.id
  JOIN feeds f ON a.feed_id = f.id
  LEFT JOIN reading_progress rp ON a.id = rp.article_id AND rp.user_id = 1
  WHERE a.processing_state='ready' AND a.parent_article_id IS NOT NULL
    AND parent.fetched_at > NOW() - INTERVAL '7 days'
    AND (f.owner_id IS NULL OR f.owner_id = 1)
    AND COALESCE(rp.is_completed, false) = false
) sub;
"
```
Expected: `primary_count = 0` (matches the known "all HN children are completed" condition).

- [ ] **Step 2: Sanity-check fallback returns the HN children**

Run:
```bash
docker exec rss-pal-postgres-1 psql -U postgres -d rsspal -c "
SELECT a.id, a.title, a.word_count, LENGTH(a.summary_brief) AS sb_len, parent.title AS parent_title
FROM articles a
JOIN articles parent ON a.parent_article_id = parent.id
JOIN feeds f ON a.feed_id = f.id
WHERE a.processing_state='ready' AND a.parent_article_id IS NOT NULL
  AND parent.fetched_at > NOW() - INTERVAL '7 days'
  AND (f.owner_id IS NULL OR f.owner_id = 1)
  AND a.word_count >= 500
  AND a.summary_brief IS NOT NULL AND a.summary_brief <> ''
  AND NOT (a.id = ANY(ARRAY[]::int[]))
ORDER BY a.prerank_score DESC NULLS LAST, a.published_at DESC NULLS LAST
LIMIT 20;
"
```
Expected: some subset of the 10 HN children (561–564, 434–441) — those with `word_count >= 500` and a non-empty `summary_brief`. If 0 returned, investigate: maybe most HN children have short word_count (lower the threshold to 300 and re-document in the spec) or no summary_brief (the AI summary worker hasn't processed them).

- [ ] **Step 3: If Step 2 returns 0, decide & document**

If the live data shows the quality gates are too strict, decide:
- (a) Lower `word_count >= 500` to `>= 300` in `queryLinkSetFallback`, OR
- (b) Drop the `word_count` gate entirely and rely only on `summary_brief <> ''`

Apply the change to `backend/internal/repository/article.go`, rebuild, re-run Step 2. Add a follow-up commit documenting the tuning:

```bash
git commit -am "tune(repo): lower fallback word_count threshold to <chosen-value> based on live data"
```

> If Step 2 returns >= 1 row, skip Step 3 entirely.

---

## Task 6: Update TypeScript `Article` interface

**Files:**
- Modify: `frontend/src/api/client.ts:100-126`

- [ ] **Step 1: Add the two new optional fields**

In `frontend/src/api/client.ts`, find the `Article` interface (line 100) and add `parent_title` and `is_fallback` to the link_set fields block:

```typescript
export interface Article {
  id: number
  feed_id: number
  feed_title?: string
  title: string
  url: string
  content: string
  published_at: string | null
  summary_brief: string
  summary_detailed: string
  fetched_at: string
  word_count?: number
  reading_minutes?: number
  is_read?: boolean
  media_url?: string
  media_type?: string
  media_duration_seconds?: number
  // link_set fields
  is_link_set?: boolean
  links_extendable?: boolean | null  // tri-state: null = unchecked, true/false = checked
  link_set_suggested?: boolean | null  // worker thinks article is a link list, awaiting user confirmation
  parent_article_id?: number | null
  parent_title?: string  // populated only by GET /articles/recommended/link_set
  is_fallback?: boolean  // true = surfaced by quality-fallback (may be already-read)
  processing_state?: 'ready' | 'stub' | 'processing' | 'failed'
  prerank_score?: number | null
  editor_note?: string
  manual_tags: UserTag[]
}
```

- [ ] **Step 2: Verify the frontend type-checks**

Run: `cd frontend && npx tsc --noEmit`
Expected: no new errors. (Some pre-existing errors unrelated to this change may exist — only confirm no NEW errors involving `Article`.)

- [ ] **Step 3: Commit**

```bash
git add frontend/src/api/client.ts
git commit -m "$(cat <<'EOF'
feat(client): add parent_title and is_fallback to Article interface

Populated by GET /articles/recommended/link_set only. Used by the
RecommendedPage to show "来自《...》" and a "兜底推荐" marker.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 7: Update `RecommendedPage.tsx` — help panel, parent link, fallback marker, bestblogs copy

**Files:**
- Modify: `frontend/src/pages/RecommendedPage.tsx` (whole file replacement)

- [ ] **Step 1: Replace the whole file**

Replace the contents of `frontend/src/pages/RecommendedPage.tsx` with:

```tsx
import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { getRecommendedFeeds, subscribeRecommendedFeed, getLinkSetRecommendations, RecommendedFeed, Article } from '../api/client'
import { toast } from '../utils/toast'
import { CATEGORY_LABELS, CATEGORY_ORDER } from '../components/categoryLabels'

export default function RecommendedPage() {
  const navigate = useNavigate()
  const [items, setItems] = useState<RecommendedFeed[]>([])
  const [loading, setLoading] = useState(true)
  const [busyId, setBusyId] = useState<number | null>(null)
  const [linkSetRecs, setLinkSetRecs] = useState<Article[]>([])
  const [showHelp, setShowHelp] = useState(false)

  useEffect(() => {
    getLinkSetRecommendations(7, 20)
      .then(setLinkSetRecs)
      .catch(() => setLinkSetRecs([]))
  }, [])

  useEffect(() => { load() }, [])

  const load = async () => {
    setLoading(true)
    try {
      const data = await getRecommendedFeeds()
      setItems(data)
    } finally {
      setLoading(false)
    }
  }

  const handleSubscribe = async (id: number) => {
    setBusyId(id)
    try {
      await subscribeRecommendedFeed(id)
      await load()
    } catch (err: any) {
      toast.error(err?.response?.data?.error || '订阅失败')
    } finally {
      setBusyId(null)
    }
  }

  if (loading) return <div className="card">加载中…</div>

  const grouped: Record<string, RecommendedFeed[]> = {}
  for (const it of items) {
    if (!grouped[it.category]) grouped[it.category] = []
    grouped[it.category].push(it)
  }

  return (
    <div>
      {linkSetRecs.length > 0 && (
        <section className="mb-8">
          <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 12 }}>
            <h2 className="text-lg font-semibold" style={{ margin: 0 }}>本周精选 link_set 链接</h2>
            <button
              onClick={() => setShowHelp((v) => !v)}
              aria-label="说明"
              aria-expanded={showHelp}
              style={{
                background: 'none',
                border: 'none',
                cursor: 'pointer',
                fontSize: 16,
                padding: 0,
                lineHeight: 1,
              }}
            >
              ℹ️
            </button>
          </div>
          {showHelp && (
            <div className="card text-sm" style={{ background: 'var(--surface-hover)', marginBottom: 12 }}>
              <p style={{ marginTop: 0 }}>
                这里的文章来自你订阅源里"内含链接合集"的文章(如 Hacker Newsletter)。系统会自动展开链接、抓取正文,按以下规则推荐:
              </p>
              <ol style={{ paddingLeft: 20, marginBottom: 8 }}>
                <li>优先按你的偏好(过去 30 天 like / save / 收听时长加权)排序</li>
                <li>没有偏好数据时按编辑加权 + 发布时间排序,保证质量</li>
                <li>已读完的文章默认不出现,但当合格文章不足时会作为兜底补齐(会标注"兜底推荐")</li>
              </ol>
              <p className="text-muted" style={{ marginBottom: 0 }}>
                如果某期 newsletter 没出现,可能是该订阅源还未被系统识别为"含链接合集",或该期所有文章都已读完。
              </p>
            </div>
          )}
          <div className="space-y-3">
            {linkSetRecs.map((a) => (
              <div
                key={a.id}
                className="card"
                style={{ cursor: 'pointer' }}
                onClick={() => navigate(`/articles/${a.id}`)}
              >
                <div className="text-bold">{a.title}</div>
                {a.summary_brief && (
                  <div className="text-muted text-sm mt-1">{a.summary_brief.slice(0, 120)}…</div>
                )}
                {a.feed_title && (
                  <div className="text-muted text-sm mt-1" style={{ color: 'var(--accent)' }}>{a.feed_title}</div>
                )}
                {a.parent_title && a.parent_article_id != null && (
                  <div className="text-muted text-sm mt-1">
                    来自《
                    <span
                      onClick={(e) => {
                        e.stopPropagation()
                        navigate(`/articles/${a.parent_article_id}`)
                      }}
                      style={{ color: 'var(--accent)', cursor: 'pointer' }}
                    >
                      {a.parent_title}
                    </span>
                    》
                    {a.is_fallback && (
                      <span className="text-muted" style={{ marginLeft: 8, fontSize: 11 }}>
                        · 兜底推荐(可能已读过)
                      </span>
                    )}
                  </div>
                )}
              </div>
            ))}
          </div>
        </section>
      )}
      <h2 style={{ marginBottom: 16 }}>推荐订阅</h2>
      <p className="text-muted text-sm" style={{ marginBottom: 16 }}>
        以下是预置的优质订阅源,按内容方向分类。点击「订阅」加入你的订阅列表。
      </p>
      {CATEGORY_ORDER.filter(c => grouped[c]?.length).map(cat => (
        <section key={cat} style={{ marginBottom: 24 }}>
          <h3 style={{ fontSize: 16, fontWeight: 600, marginBottom: 8 }}>
            {CATEGORY_LABELS[cat] || cat}
          </h3>
          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(280px, 1fr))', gap: 12 }}>
            {grouped[cat].map(rf => (
              <div key={rf.id} className="card" style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
                <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', gap: 8 }}>
                  <strong style={{ fontSize: 14 }}>{rf.title}</strong>
                  <span style={{ fontSize: 11, padding: '2px 6px', background: 'var(--surface-hover)', borderRadius: 4 }}>
                    {rf.language === 'zh' ? '中文' : 'English'}
                  </span>
                </div>
                {rf.description && (
                  <div className="text-muted text-sm" style={{ fontSize: 12 }}>{rf.description}</div>
                )}
                <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginTop: 'auto' }}>
                  {rf.is_broken ? (
                    <span style={{ fontSize: 12, color: '#c33' }}>⚠ 当前路由不可用</span>
                  ) : rf.subscribed ? (
                    <span style={{ fontSize: 12, color: '#28a745' }}>✓ 已订阅</span>
                  ) : (
                    <button
                      onClick={() => handleSubscribe(rf.id)}
                      disabled={busyId === rf.id}
                      style={{ padding: '4px 10px', fontSize: 12 }}
                    >
                      {busyId === rf.id ? '订阅中…' : '订阅'}
                    </button>
                  )}
                </div>
              </div>
            ))}
          </div>
        </section>
      ))}
      {items.length === 0 && (
        <div className="card text-muted">暂无推荐源,请等待管理员配置。</div>
      )}
    </div>
  )
}
```

- [ ] **Step 2: Type-check**

Run: `cd frontend && npx tsc --noEmit`
Expected: no new errors.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/pages/RecommendedPage.tsx
git commit -m "$(cat <<'EOF'
feat(recommended): help panel, parent-article link, fallback marker, copy

- ℹ️ icon next to "本周精选 link_set 链接" toggles an explanation panel
- Cards now show "来自《<parent.title>》" with click-to-jump to parent
- Fallback-sourced articles get a "· 兜底推荐(可能已读过)" tag
- Bestblogs copy generalized: no longer mentions bestblogs.dev by name

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 8: Rebuild & smoke-test the whole stack

> No commit in this task — verification only.

- [ ] **Step 1: Rebuild backend + frontend**

Run:
```bash
docker-compose up -d --build api frontend
```
Expected: both containers come up healthy. Watch for build errors.

- [ ] **Step 2: Wait for healthcheck, then check API directly**

Run:
```bash
docker-compose logs --tail=20 api
```
Expected: no panic / startup error. API is listening on :8080.

Then hit the endpoint with curl (use your JWT — find via browser DevTools → Application → Cookies → `auth_token`):
```bash
TOKEN="<paste-jwt-here>"
curl -s -H "Authorization: Bearer $TOKEN" \
  "http://localhost:8080/api/articles/recommended/link_set?days=7&limit=20" | jq 'map({id, title, parent_title, is_fallback})'
```
Expected: array with at least 1 entry, each carrying `parent_title` and (likely) `is_fallback: true` for the HN children case.

- [ ] **Step 3: Browser smoke test**

Open `http://localhost/recommended` in your normal browser session.

Verify:
1. "本周精选 link_set 链接" section shows the HN children (was empty before).
2. ℹ️ icon next to the heading toggles a help panel with the explanation copy.
3. Each child card shows `来自《Hacker Newsletter #79x》` at the bottom, in the accent color.
4. Clicking the parent-article link navigates to `/articles/<parent-id>` and does NOT also trigger the child card click.
5. Each card shows the `· 兜底推荐(可能已读过)` marker (since all HN children are completed → all came from fallback).
6. Bestblogs section copy reads: "以下是预置的优质订阅源,按内容方向分类。点击「订阅」加入你的订阅列表。"
7. Subscribe / 已订阅 / ⚠ states on the recommended feeds still work.

If any check fails, fix in a follow-up commit before proceeding.

---

## Task 9: Push branch & open PR

- [ ] **Step 1: Push branch to origin**

```bash
git push -u origin feature/recommended-page-improvements
```

- [ ] **Step 2: Open PR**

```bash
gh pr create --title "feat(recommended): fallback link_set recs + parent-article visibility + copy" --body "$(cat <<'EOF'
## Summary
- Split `GetLinkSetRecommendations` into a primary (preference-ranked, excludes read) + fallback (quality-gated, allows read) pair so the `/recommended` link_set section no longer goes empty when all candidates are already-read.
- Surface `parent_title` per child article and render a clickable "来自《...》" pointer back to the source newsletter.
- ℹ️ help panel next to the section heading explains link_set sourcing and the ranking rules.
- Generalize the bestblogs section copy — no longer hard-codes the source name.

## Design
See `docs/superpowers/specs/2026-05-18-recommended-page-improvements-design.md` (included in this PR).

## Out of scope
Link_set auto-confirmation for `link_set_suggested=true` RSS articles — separate concern, tracked in memory.

## Test plan
- [ ] `go test ./internal/repository/ -v` passes (4 new unit tests for `combineLinkSetResults`).
- [ ] `npx tsc --noEmit` passes in `frontend/`.
- [ ] Live: `/api/articles/recommended/link_set?days=7&limit=20` returns articles with `parent_title` populated and `is_fallback: true` for HN children.
- [ ] Browser: `/recommended` shows the 10 HN children, ℹ️ panel toggles, parent links jump correctly without bubbling, fallback marker visible.

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

- [ ] **Step 3: Report PR URL back to user**

Copy the URL printed by `gh pr create` and report it in chat.

---

## Self-Review Checklist (run before declaring done)

- All 8 spec requirements (问题 A–D, fix sections 1–4) covered by tasks 1–7? ✓
- No "TBD" / "TODO" / "implement later" in plan? ✓ (Task 5 Step 3 has a conditional "if X then tune Y" — that's a real decision branch, not a placeholder)
- Type / function names consistent across tasks? `combineLinkSetResults`, `collectArticleIDs`, `queryLinkSetPrimary`, `queryLinkSetFallback`, `scanArticleWithParentTitle` — all used as defined.
- `pq.Array` import — covered by existing `lib/pq` import at `article.go:10`, verified in Task 4 Step 2.
- Frontend Docker rebuild reminder honored in Task 8 Step 1 (memory: `feedback_frontend_docker_rebuild.md`).
- No database mutation anywhere in the plan (only SELECTs in Task 5). Memory `feedback_db_safety.md` honored.
