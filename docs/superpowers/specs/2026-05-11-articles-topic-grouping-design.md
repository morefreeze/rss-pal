# Articles Topic Grouping — Design

**Date:** 2026-05-11
**Scope:** backend + frontend
**Files touched:**
- backend: `internal/repository/article.go`, `internal/api/article.go`, `cmd/server/main.go`
- frontend: `src/pages/ArticleListPage.tsx`, `src/api/client.ts`, new `src/components/GroupedArticleView.tsx`

## Problem

`/articles` is a flat reverse-chronological list. When the user wants to skim "what's happening in AI today" vs. "what's happening in macro today," they have to scroll the whole feed and mentally group by source/topic. Each article already carries a 2-4 字 中文 `topic` (assigned by `ai.ClassifyArticle`) and the system already maintains per-user `interest_topics` weights — those signals are unused on the list page.

## Goal

Add a **`📚 分组` toggle** on `/articles`. When on, the article list is replaced by a topic-grouped view ordered by the user's interest weights. The flat view is unchanged.

## Non-goals (YAGNI)

- **No** topic-based filter on the flat view (no `?topic=...` query, no chip-to-filter navigation).
- **No** AI-driven secondary clustering of topic names into super-clusters (e.g. "大模型 / RAG / agent" → "AI 工程"). Use the raw `articles.topic` value as the group key.
- **No** offline pre-aggregation table or worker task.
- **No** "+ N more topics" hint for groups that don't make the top 8 — they're simply not visible in this view (the user chose lean).
- **No** pagination, prefetch, or "load more" inside the grouped view — single response.

## Design

### Toggle & state

- Place the `📚 分组` button in the existing filter bar on `ArticleListPage.tsx`, adjacent to `未读` / `网摘`.
- Persist state in `sessionStorage` under key `articlesGrouped` (string `"true"` / absent), matching the `unreadOnly` pattern.
- When the search box has a non-empty query, the grouped view is hidden and search results take over (existing behavior). The toggle remains visible but its effect resumes when the search clears.
- Switching the toggle does not change the URL (consistent with the page's other filters today).

### View behavior when grouped mode is ON

| Aspect | Flat mode (current) | Grouped mode (new) |
|---|---|---|
| `为你推荐` panel | shown | hidden |
| Article list rendering | flat, time-desc | grouped (see below) |
| Feed selection (sidebar) | filters | same filter applies |
| `未读` / `网摘` toggles | filter | same filters apply |
| `加载更多` / prefetch | active | not used (single response) |

### Grouping rules

1. **Group ordering** — `ORDER BY COALESCE(it.weight, 0) DESC, group_article_count DESC`
   - `it.weight` comes from `interest_topics` joined on `(user_id, topic)`.
   - For a cold-start user (no `interest_topics` rows), the COALESCE makes every group tie on weight=0, so ordering naturally falls back to article count.
2. **Top-N cap** — show only the first **8** topic groups. Topic groups that don't make the cut are not rendered in grouped mode.
3. **Unclassified bucket** — articles with `topic IS NULL OR topic = ''` form one extra group titled `未分类`, rendered last regardless of size. Not counted against the top-8 cap.
4. **Within a group** — `ORDER BY COALESCE(p.score, 0) DESC, a.published_at DESC NULLS LAST`, where `p.score` is the per-article user preference score aggregated exactly like the existing `GetReadingList` / `GetRecommended` paths use:
   ```
   like     →  signal_value * 5
   dislike  →  signal_value * -10
   save     →  signal_value * 3
   read_duration → signal_value / 60
   ```
   (other signal types omitted to match current `GetRecommended` SUM expression).
5. **Per-group cap** — server returns at most **20** articles per group. UI shows the first **5**; an `展开更多 (N)` row reveals the rest of the up-to-20. There is no path to articles beyond 20 in a single group within this view (by design — the user can switch to flat mode).

### API

**`GET /api/articles/grouped`**

Query parameters mirror `GET /api/articles` semantics **exactly** — the implementation should share its `WHERE` clause builder with the existing flat-list query rather than re-deriving filter logic:
- `feed_id` (int, optional)
- `unread` (bool, optional) — same `COALESCE(rp.is_completed, false) = false` semantics used today (articles with no `reading_progress` row are considered unread).
- `saved` (bool, optional) — same as today: includes both `user_preferences` save signals **and** articles from the bookmarklet `网摘` feed (per the post-refactor unified definition in `c54ea25`-era commits).

Server-side constants (not exposed as params): `TOP_GROUPS = 8`, `PER_GROUP_CAP = 20`.

Response:
```json
{
  "groups": [
    {
      "topic": "大模型",
      "total_count": 14,
      "articles": [ /* up to PER_GROUP_CAP Article objects, same shape as GET /api/articles */ ]
    }
  ],
  "unclassified": {
    "total_count": 3,
    "articles": [ /* up to PER_GROUP_CAP */ ]
  }
}
```

`total_count` reflects the count under the same filters, not the global count. If `unclassified.total_count` is 0, the `unclassified` field is still present with an empty `articles` array (frontend will simply skip rendering it).

### Backend query sketch

A single endpoint, two SQL passes:

1. **Pick top-8 topics + per-group articles**, using a CTE for the filtered article set and `LATERAL` to take the top-20-by-score per topic:
   ```sql
   WITH visible AS (
     SELECT a.id, a.topic, a.published_at, a.feed_id, ...
     FROM articles a
     JOIN feeds f ON a.feed_id = f.id
     WHERE <feed_id / unread / saved / tenancy filters>
       AND a.topic IS NOT NULL AND a.topic <> ''
   ),
   topic_stats AS (
     SELECT v.topic,
            COUNT(*) AS article_count,
            COALESCE(it.weight, 0) AS weight
     FROM visible v
     LEFT JOIN interest_topics it ON it.user_id = $userId AND it.topic = v.topic
     GROUP BY v.topic, it.weight
     ORDER BY weight DESC, article_count DESC
     LIMIT 8
   )
   SELECT ts.topic, ts.article_count, art.*
   FROM topic_stats ts
   JOIN LATERAL (
     SELECT v.*, COALESCE(p.score, 0) AS score
     FROM visible v
     LEFT JOIN per_article_score p ON p.article_id = v.id   -- subquery, same SUM as GetRecommended
     WHERE v.topic = ts.topic
     ORDER BY score DESC, v.published_at DESC NULLS LAST
     LIMIT 20
   ) art ON true;
   ```
2. **Unclassified**: same shape, filter `topic IS NULL OR topic = ''`, capped at 20.

Implementation can be one repository method `GetGroupedByTopic(userID int, filters ArticleFilters) (*model.GroupedArticles, error)` returning a struct that the handler serializes directly.

### Frontend changes

- `ArticleListPage.tsx`:
  - New state `const [grouped, setGrouped] = useState(...)` read from `sessionStorage.articlesGrouped`.
  - When `grouped && !searchQuery`, fetch from `/api/articles/grouped` instead of `/api/articles`. Hide the recommended panel.
  - The filter-change effect (currently keyed on `selectedFeed, unreadOnly, savedOnly`) must also re-fire when `grouped` flips.
- New `GroupedArticleView.tsx` component:
  - Props: `groups: Group[]`, `unclassified: Group | null`, plus the action callbacks that `ArticleCard` already needs (like / dislike / mark-read, etc.).
  - Renders each group as a section header `主题名 · N 篇` followed by 5 `ArticleCard` rows, then an `展开更多 (N)` button if `articles.length > 5`. Expand state is local component state, per group (`Map<topic, boolean>`).
- `client.ts`: add `GroupedArticles` type + `getGroupedArticles(filters)` helper.

### Error handling

- Backend errors → standard `500` JSON (`{ "error": "..." }`). Frontend shows a toast and falls back to flat view for that render cycle (does not auto-disable the toggle — user toggles back manually if desired).
- Empty result (no articles match filters) → `{ "groups": [], "unclassified": { "total_count": 0, "articles": [] } }`. Frontend renders the same "暂无文章" placeholder the flat view uses.

### Testing

- Repository unit test: seed 3 users + 30 articles across 12 topics with varying `interest_topics` weights and `user_preferences` signals; assert (a) only top-8 topics returned, (b) group order matches weight then count, (c) within-group order matches score then time, (d) unclassified bucket present when applicable, (e) tenancy: user B never sees user A's private feeds.
- API integration test: hit `/api/articles/grouped` with `unread=true` and `saved=true` and assert filter pass-through.
- Frontend: no test infra in the project today (no vitest/jest configured); manual verification via the existing docker-compose dev flow.

### Migration / rollout

- No DB migration. All required tables and columns (`articles.topic`, `interest_topics`, `user_preferences`, `reading_progress`) already exist.
- No backfill. Existing rows without a `topic` simply land in `未分类` until the classifier eventually backfills (it's already running for new articles).
- Feature is purely additive — flat view is untouched, so risk to existing users is limited to the new endpoint and the new toggle.
