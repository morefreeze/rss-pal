# Insight Article Recommendations — Design

**Date:** 2026-05-07
**Branch:** `feature/insights-full`
**Status:** Approved (brainstorm complete, going straight to plan + implementation)

## Goal

Extend the AI insight (currently a markdown blob with 4 sections) so it also produces concrete article recommendations drawn from the user's article pool, grouped by interest direction, each with a reason. Clicking a recommended article navigates into RSS Pal's normal article view so reading-progress and like/save signals continue to flow back into the recommender.

## Non-Goals

- Click-through analytics (no per-recommendation click tracking yet — JSONB column is enough; can split into a table later if needed).
- A separate "discovery feed" page; the feature lives inside the existing `/insights` page.
- Re-using recommendations across insight runs; each insight regenerates them from scratch.
- Re-prompting on malformed AI output (one shot; lenient validation drops bad entries).

## Decisions (from brainstorm)

| # | Decision |
|---|----------|
| Q1 | AI returns structured JSON `{markdown, recommendations}`; backend validates and stores. |
| Q2 | Candidate pool = mostly unread + a small slice of past favorites (read 30–180d ago, previously liked/saved). |
| Q3 | 50 candidates total (40 unread + 10 past favorites) → AI emits 3–5 recommended articles. |
| Q4 | Recommendations are **grouped by direction** (`core` = strengthen existing top interest; `emerging` = weak-but-recurring new interest). 2–3 directions, each with 1–2 articles. |
| Q5 | Both `auto` (cron) and `manual` flows produce recommendations. |
| Q6 | Storage: add `recommendations JSONB` column to `user_insights`. |
| Q7 | Frontend: a new card below the AI insight markdown card, on `/insights`. |
| Q8 | Lenient validation: parse failures or invalid `article_id`s drop the offending entry; insight still goes to `done`. |

## Architecture

```
Generate (handler) / runDailyInsightJob (cron)
  └─ 1. topics, tags, recent titles   (existing: prefRepo)
  └─ 2. candidates                    (NEW: ArticleRepository.GetInsightCandidates)
        ├─ unread top 40   (score-ranked, fall back to recency)
        └─ past favorites top 10 (read 30–180d ago, ever liked/saved)
  └─ 3. prompt = buildInsightPrompt(topics, tags, titles, candidates)
  └─ 4. AI call (GenerateUserInsightJSON, response_format=json_object)
  └─ 5. parseAndValidate(rawJSON, candidatePoolIDs)
        → markdown, []ValidatedRecommendation
  └─ 6. UserInsightRepository.MarkDoneWithRecs(id, markdown, recsJSON)
```

**Layered prompt invariant preserved.** The cron's `buildLayeredPrompt` adds the candidate-pool block plus the JSON-output instructions. The manual path migrates from `buildSimplePrompt` to the same builder so both flows are consistent.

## Backend Changes

### 1. Migration `010_insight_recommendations.sql`

```sql
ALTER TABLE user_insights
  ADD COLUMN IF NOT EXISTS recommendations JSONB;
-- No index for now: read pattern is "fetch latest insight for user"; the JSONB
-- comes along on that single-row read. If a future feature reverse-looks-up by
-- article_id, add a GIN index then.
```

Existing rows stay `recommendations IS NULL` and the API treats null/absent as "no recommendations" — UI shows the empty state.

### 2. Model

```go
// internal/model/model.go
type ArticleRecommendation struct {
    ArticleID int    `json:"article_id"`
    Reason    string `json:"reason"`
}

type RecommendationDirection struct {
    Direction     string                  `json:"direction"`
    DirectionKind string                  `json:"direction_kind"` // "core" | "emerging"
    Articles      []ArticleRecommendation `json:"articles"`
}

// UserInsight gains:
//   Recommendations []RecommendationDirection `json:"recommendations,omitempty" db:"recommendations"`
```

The DB column is `JSONB`; we marshal/unmarshal the slice in the repo layer.

### 3. Repository

`internal/repository/article.go` — new method:

```go
// GetInsightCandidates returns up to (unreadLimit + readLimit) candidate articles
// for the AI to choose from when generating recommendations.
//   - unread: visible to userID, not is_completed, ranked by COALESCE(pref_score, 0) DESC
//             then published_at DESC NULLS LAST. Caps at unreadLimit.
//   - past favorites: visible to userID, is_completed=true, has like/save signal,
//                     last_read_at between 30 and 180 days ago. Same score+recency
//                     ranking. Caps at readLimit. Tagged AlreadyRead=true on output.
//
// The two sub-queries run independently and are concatenated; pool order is
// (unread block first, read block after). De-duplication is implicit because
// is_completed=true vs false are disjoint.
func (r *ArticleRepository) GetInsightCandidates(userID, unreadLimit, readLimit int) ([]model.InsightCandidate, error)
```

The candidate type lives in `model` (not `repository`) so the `ai` package can consume it without cycles:

```go
// internal/model/model.go
type InsightCandidate struct {
    Article     Article
    AlreadyRead bool   // true when from the past-favorites slice
    BriefShort  string // first 60 runes of summary_brief, "" if none
}
```

`internal/repository/insight.go` — extend MarkDone or add a sibling:

```go
// MarkDoneWithRecs upgrades a pending row to status='done', writing both the
// markdown and the validated recommendations. nil/empty recs is allowed.
func (r *UserInsightRepository) MarkDoneWithRecs(id int, content string, recs []model.RecommendationDirection) error
```

`GetLatest` is updated to `SELECT ... recommendations` and unmarshal the JSONB into `Recommendations`. Null → empty slice.

### 4. Prompt Builder

Shared package: **`internal/ai/insight_prompt.go`**. Both `internal/api/insights.go` and `cmd/worker/insights.go` import and call it; their existing local builders (`buildSimplePrompt`, `buildLayeredPrompt`) are removed.

```go
// BuildInsightPrompt builds the user prompt that asks for JSON output.
// Candidates are rendered as "[id=N] Title — feed · brief" so the AI can
// reference exact ids in the response.
func BuildInsightPrompt(topics []model.InterestTopic, tags []model.InterestTag,
    recentTitles []string, candidates []model.InsightCandidate) string
```

Prompt skeleton (Chinese, matching existing tone):

```
基于用户的兴趣画像和最近阅读，输出 JSON：

{
  "markdown": "（4 段中文 markdown：核心兴趣领域 / 近期偏好变化 / 可能的新兴趣点 / 阅读建议）",
  "recommendations": [
    {
      "direction": "...",
      "direction_kind": "core" | "emerging",
      "articles": [
        {"article_id": <候选池中的 id>, "reason": "≤100 字"}
      ]
    }
  ]
}

约束：
- direction 2-3 个；总文章数 3-5
- core = 强化已有核心兴趣；emerging = 弱信号反复出现的新兴趣点
- article_id 必须来自下方候选池，禁止编造
- 每个 reason ≤ 100 字，需说明为什么属于该方向
- 候选池为空或无合适文章时，"recommendations": []

## 用户兴趣主题（按权重）
- {topic} ({weight})
...

## 用户关键词（top 20）
- {tag} ({weight})
...

## 最近阅读
- {title}
...

## 候选文章池（共 N 条，[r] 表示已读过的过往收藏）
[id=123] {title} — {feed_title} · {brief60}
[id=456] [r] {title} — {feed_title} · {brief60}
...
```

### 5. AI Client

`internal/ai/summarizer.go` adds:

```go
// GenerateUserInsightJSON is like GenerateUserInsight but requests JSON output
// (response_format={"type":"json_object"}) and returns the raw string for the
// caller to parse and validate.
func (s *Summarizer) GenerateUserInsightJSON(ctx context.Context, prompt string) (string, error)
```

Implemented by adding an optional `ResponseFormat` field to `chatRequest` (omitempty) and a small private call helper that sets it — minimal, no impact on other call sites.

Token budget: keep `maxTokens=2000` (was 1500) so JSON + markdown + reasons all fit.

### 6. Validation

Shared package: **`internal/ai/insight_parse.go`** (sibling of the prompt builder; both worker and api call it).

```go
// ParseInsightJSON extracts markdown + validated recommendations from raw AI output.
// Returns the markdown (always), validated recs (possibly empty), and a slice of
// debug-only drop reasons for logging.
func ParseInsightJSON(raw string, candidateIDs map[int]bool) (string, []model.RecommendationDirection, []string)
```

Rules:
- Try `json.Unmarshal(raw, &envelope)`. On failure: log + return `(stripCodeFences(raw), nil, ["json parse failed"])` so the markdown still renders. (Some models wrap JSON in ```` ```json ... ``` ````; we strip a single leading/trailing fence before retrying.)
- For each direction: drop if `direction_kind ∉ {"core","emerging"}` or `direction == ""`.
- For each article in a direction: drop if `article_id ∉ candidateIDs`, `reason == ""`, or `len([]rune(reason)) > 200`.
- Drop a direction whose articles list ends up empty.
- Cap at 3 directions, 5 articles total (defense-in-depth against AI ignoring the spec).

### 7. Wiring

`InsightsHandler.Generate` async path and `cmd/worker/insights.go` `generateDailyInsights`:

1. Call `articleRepo.GetInsightCandidates(userID, 40, 10)`. (Worker needs `articleRepo` — it already does, see `insightCronDeps`.)
2. Build candidate-id set for whitelist.
3. `prompt := BuildInsightPrompt(topics, tags, titles, candidates)`.
4. Token-budget guard remains (`estimateTokens > insightTokenBudget` skip).
5. `raw, err := summarizer.GenerateUserInsightJSON(ctx, prompt)`.
6. `markdown, recs, dropped := parseInsightJSON(raw, idSet)`.
7. Log dropped reasons (not user-visible).
8. `userInsightsRepo.MarkDoneWithRecs(id, markdown, recs)`.

Failure modes:
- AI call errors → existing `MarkFailed` path, unchanged.
- JSON parse fails but raw text is non-empty → store as markdown, recs empty, status `done`. Log warning.
- Empty topics+tags → existing skip, no AI call. Manual path returns `no_data` JSON, unchanged.

### 8. API

`GET /api/insights/latest` already returns the full `UserInsight` row; with the new column it will include `recommendations`. To make the article cards renderable without an extra round-trip, the handler enriches the response with article metadata:

```go
// Latest response shape (new fields):
{
  "insight": { ...existing..., "recommendations": [...] },
  "remaining_today": 3,
  "remaining_month": 100,
  "rec_articles": {           // NEW: id → minimal article info
    "123": {"id":123,"title":"...","feed_title":"...","brief":"...","is_read":false}
  }
}
```

`rec_articles` is built by collecting all article_ids across the recommendations and calling `articleRepo.GetByIDsForUser(userID, ids)` (already exists). If an id no longer resolves (article deleted between generation and read), it is omitted; the frontend skips entries without metadata.

## Frontend Changes

### 1. Types (`frontend/src/api/client.ts`)

```ts
export interface ArticleRecommendation {
  article_id: number
  reason: string
}
export interface RecommendationDirection {
  direction: string
  direction_kind: 'core' | 'emerging'
  articles: ArticleRecommendation[]
}
export interface RecArticleMeta {
  id: number
  title: string
  feed_title: string
  brief: string
  is_read: boolean
}
export interface PersistedInsight {
  // existing fields...
  recommendations?: RecommendationDirection[]
}
export interface LatestInsightsResponse {
  insight: PersistedInsight | null
  remaining_today: number
  remaining_month: number
  rec_articles?: Record<string, RecArticleMeta>
}
```

### 2. UI

In `InsightsPage.tsx`, after the existing AI insight card, render a `RecommendationsCard` only when `insight.status === 'done'`:

```
┌─ 📍 为你推荐 ──────────────┐
│ ▸ 强化你的核心兴趣：分布式系统     │  (core 用蓝)
│   ┌──────────────────────┐ │
│   │ 文章标题（点击跳详情）  ✔已读 │ │
│   │ feed · brief                │ │
│   │ 推荐理由：...              │ │
│   └──────────────────────┘ │
│ ▸ 新兴趣点：LLM 可观测性      │  (emerging 用紫)
│   ...                          │
└────────────────────────────┘
```

- `core`/`emerging` differ only by an icon/color tag. Use existing CSS classes (`card`, `text-muted`, etc.).
- Card click → `navigate(\`/articles/${id}\`)`.
- "已读" small grey badge when `rec_articles[id].is_read === true` (mostly relevant for the past-favorites slice).
- If `recommendations` is empty/undefined: do not render the recommendations card at all (keeps the page clean for users with no recs yet).

### 3. Polling

Existing `pollRef` polls `getLatestInsights()` every 2s while pending. No change — when status flips to `done`, `recommendations` and `rec_articles` arrive together.

## Edge Cases

| Case | Behavior |
|------|----------|
| User has zero candidates (no feeds, brand new) | Existing `no_data` path, no AI call. |
| Candidate pool has only unread, no past favorites | Pool just smaller (e.g., 30 unread + 0 read); AI still works. |
| AI returns markdown only, no `recommendations` field | `recs = []`, insight done. UI hides recs card. |
| AI returns invalid JSON | Strip code fences, retry parse once; if still fails store raw as markdown, `recs = []`. |
| AI references article_id not in pool | Drop that one; keep direction if other articles in it survive. |
| Article deleted between generation and read | API omits from `rec_articles`; frontend skips. |
| `direction_kind` is not "core"/"emerging" | Drop the whole direction. |
| Reason exceeds 200 runes | Drop that article (other articles in direction kept). |
| Pending row exists, user navigates away and back | Polling resumes; UI shows the spinner card. |

## Testing

- Unit: `parseInsightJSON` — table-driven, covers each validation rule (valid, invalid kind, fake id, empty reason, fence-wrapped JSON, total garbage).
- Unit: `BuildInsightPrompt` — golden test that pinned candidate render uses `[id=N]` and `[r]` markers correctly.
- Repo: `GetInsightCandidates` — fixture with read/unread mix, check dedup and limits. Use the existing test scaffolding.
- Manual smoke: trigger manual insight in dev (`INSIGHTS_RUN_NOW=1` for cron path; click Generate for handler path), verify recs surface in UI.

## Rollout

- Single migration; no feature flag. Frontend tolerates absent `recommendations` so the rollout is order-independent (DB + worker + frontend can ship together via Docker compose rebuild).
- After deploy: trigger a manual generate to populate one user's recs; eyeball the UI.

## Open Questions Settled by Recommendation

- **Past-favorites window:** 30–180 days. Avoids "I read this last week" déjà vu while not surfacing ancient links.
- **Brief in candidate prompt:** include 60 runes — token cost ~3000 chars total, well under budget; meaningful uplift to AI's relevance judgment.
