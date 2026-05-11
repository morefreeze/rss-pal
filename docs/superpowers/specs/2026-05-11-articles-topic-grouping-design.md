# Articles Category Grouping — Design (v2)

**Date:** 2026-05-11
**Scope:** backend + frontend
**Supersedes:** the v1 topic-based design previously in this file.
**Files touched:**
- backend: `internal/repository/article.go`, `internal/repository/preference.go`, `internal/api/article.go`, `internal/model/model.go`, `internal/ai/classify.go`, `cmd/worker/classify.go`, `cmd/server/main.go`, new migration `migrations/017_article_category.sql`
- frontend: `src/pages/ArticleListPage.tsx`, `src/api/client.ts`, `src/components/GroupedArticleView.tsx`, new `src/components/categoryLabels.ts`

## Why v2

v1 grouped on `articles.topic` — a **per-user-engagement classification cache** assigned by `ai.ClassifyArticle` only when a user gave a strong signal (like / save / completed_listen / read_duration ≥ 60s). The result on a real database: 5 classified articles out of 135, so the grouped view collapses to 2 small buckets + one giant `未分类`.

Two intertwined problems:

1. **Sparsity** — most articles never get a topic, because `topic` was designed to *fuel* `interest_topics` weights, not to *organize* the article list.
2. **Granularity** — the prompt asks for 2-4 字 中文 names with mild self-stabilization. The steady-state distribution is long-tailed ("大模型 / Agent / RAG / 美联储 / 播客 / ..."), which forces the grouping view into top-N truncation and still ends up looking like a noisy taxonomy rather than a clean shelf.

v2 introduces a **coarse, closed-enum** `articles.category` field that's:
- assigned to **every** article (not gated on user actions),
- drawn from a **fixed 10-value enum**,
- mirrored by `interest_categories` (per-user weights, same shape as `interest_topics`) so the grouping view can order by interest the same way the original spec called for.

`articles.topic` stays as-is — it still drives insights and the existing classification pipeline.

## The enum

```
ai_eng | ai | cn_tech | enterprise | youtube | podcast | news | blog | health | business
```

The first 6 are the existing values used by `recommended_feeds.category` (and the `CATEGORY_LABELS` map in `RecommendedPage.tsx`). The last 4 are new — chosen to cover the coverage gaps the existing 6 leave on the user's actual subscription set (time-news feeds, personal blogs, health, business/finance).

Validation is **app-level only** — no DB `CHECK` constraint, so the enum can grow without a migration. Both Go and TypeScript hold the canonical list.

## Schema (migration 017)

```sql
ALTER TABLE articles ADD COLUMN IF NOT EXISTS category VARCHAR(20);
CREATE INDEX IF NOT EXISTS idx_articles_category
  ON articles(category) WHERE category IS NOT NULL;
-- Partial index helps the unclassified-bucket query that filters
-- `category IS NULL OR category = ''`.
CREATE INDEX IF NOT EXISTS idx_articles_no_category
  ON articles(id) WHERE category IS NULL;

CREATE TABLE IF NOT EXISTS interest_categories (
  id                 SERIAL PRIMARY KEY,
  user_id            INT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  category           VARCHAR(20) NOT NULL,
  weight             FLOAT NOT NULL DEFAULT 0,
  last_reinforced_at TIMESTAMP NOT NULL DEFAULT NOW(),
  UNIQUE(user_id, category)
);
CREATE INDEX IF NOT EXISTS idx_interest_categories_user_weight
  ON interest_categories(user_id, weight DESC);
```

## AI prompt change

`ai.ClassifyArticle` returns one extra field:

```json
{"topic": "...", "tags": ["...", "...", "..."], "category": "..."}
```

Prompt addition:

> - category：从以下闭合列表里**必选一个**，不允许新建：[ai_eng, ai, cn_tech, enterprise, youtube, podcast, news, blog, health, business]

`model.Classification` gains `Category string`. Parser validates against the enum; out-of-enum values are coerced to `""` so we never write garbage. The existing `topic` and `tags` outputs are unchanged.

## Worker change

`ArticleRepository.FindArticlesNeedingClassification` was previously gated on "7 days + strong signal" — that gate is what produced sparse coverage. Replace with:

```sql
SELECT ...
FROM articles a
WHERE a.category IS NULL
ORDER BY a.fetched_at DESC
LIMIT $1
```

Rationale: classification is now needed for *every* article, not just engaged ones. The `ORDER BY fetched_at DESC` + per-batch `LIMIT 50` keeps the worker bounded; the existing once-per-loop scheduler naturally drains the backlog within a few cycles (130 backlog ÷ 50/loop = ~3 loops). When the backlog is empty, the candidate list is empty and the pass is a no-op.

On classify success the worker:
1. Writes both `articles.topic` (existing) and `articles.category` (new).
2. For each user with a strong signal on the article: `UpsertTopic` (existing) **and** `UpsertCategory` (new — same signal-weight formula via `api.SignalToTopicWeight`).

Topic and category writes are independent: a row with a valid category but invalid topic (and vice versa) is fine.

## Backfill

No dedicated `cmd/backfill_categories` binary. The widened worker pass naturally backfills the existing ~130 NULL-category articles within a few minutes of the next worker restart. If a one-shot backfill is needed later (e.g., after a prompt revision), the existing `cmd/backfill_metrics` template is a 60-line copy job.

## Grouping endpoint switch

Rename and rewrite `ArticleRepository.GetGroupedByTopic` → `GetGroupedByCategory`. Same shape; SQL changes:

1. `visible` CTE filter on `a.category IS NOT NULL AND a.category <> ''` instead of `a.topic ...`.
2. `topic_stats` CTE joins `interest_categories ic` on `(user_id, category)` instead of `interest_topics it on (user_id, topic)`.
3. `GROUP BY c.category`, `ORDER BY weight DESC, article_count DESC, c.category ASC`.
4. Unclassified CTE filters on `category IS NULL OR category = ''`.

Constants stay the same (`GroupedTopN = 8`, `GroupedPerGroupCap = 20`). The handler `GetGrouped` keeps the same response shape; only the JSON key remains `topic` for backward compatibility with already-deployed frontend code (the value carried is the category enum slug, e.g. `"ai_eng"`).

## Frontend change

- Lift the `CATEGORY_LABELS` map currently in `RecommendedPage.tsx` into a shared `src/components/categoryLabels.ts` module so both pages import from one place.
- Extend the label map with the 4 new categories:
  ```ts
  news: '时事'
  blog: '博客随笔'
  health: '健康'
  business: '商业'
  ```
- `GroupedArticleView` passes the group's `topic` (which is now actually a category slug) through the label map before rendering — falling back to the raw slug if the map misses (defensive; covers prompt-output drift).
- No change to the toggle button, the persistence key, or the API client signature.

## Out of scope

- No reclassification of articles whose category was assigned under an older prompt.
- No user-editable category enum.
- No per-feed category default / override.
- No topic ↔ category mapping table.
- No `interest_categories` UI yet (preference page can be added later if desired). The table is populated automatically by the worker via strong-signal reinforcement.

## Migration / rollout

- New migration runs on next `docker-compose up`.
- Old `/api/articles/grouped` endpoint (from PR #17) is replaced in the same merge — the response JSON shape is unchanged so the deployed frontend continues to work; only the values inside `"topic"` change (now category slugs instead of free-form Chinese nouns). The frontend update lands in the same PR so users never see raw slugs.
- The existing v1 implementation on the feature branch is fully superseded by v2's commits; the topic-grouped state shipped in PR #17 will not survive merge.
