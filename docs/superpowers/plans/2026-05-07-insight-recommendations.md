# Insight Article Recommendations Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extend AI insight generation so it also returns 3–5 concrete article recommendations grouped by interest direction (core/emerging), each with a reason; render them as a clickable card on `/insights`.

**Architecture:** Backend builds a candidate pool (40 unread + 10 past favorites) for each user, asks AI to pick 3–5 articles in JSON format, validates against the candidate-id whitelist, stores in a new `recommendations JSONB` column on `user_insights`. Frontend reads recs + per-article metadata in the existing `/api/insights/latest` response and renders a separate card.

**Tech Stack:** Go 1.22 backend, PostgreSQL 15 (JSONB), Gin, React 18 + TypeScript + Vite, OpenAI-compatible chat API (z.ai/GLM-4.5).

**Spec:** `docs/superpowers/specs/2026-05-07-insight-recommendations-design.md`

---

## File Structure

**New files:**
- `backend/migrations/010_insight_recommendations.sql` — JSONB column.
- `backend/internal/ai/insight_prompt.go` — shared prompt builder.
- `backend/internal/ai/insight_prompt_test.go` — golden tests for prompt rendering.
- `backend/internal/ai/insight_parse.go` — JSON parser + validator.
- `backend/internal/ai/insight_parse_test.go` — table-driven validation tests.
- `frontend/src/components/RecommendationsCard.tsx` — the new UI block.

**Modified files:**
- `backend/internal/model/model.go` — `ArticleRecommendation`, `RecommendationDirection`, `InsightCandidate`; extend `UserInsight` with `Recommendations`.
- `backend/internal/repository/article.go` — add `GetInsightCandidates`.
- `backend/internal/repository/insight.go` — add `MarkDoneWithRecs`; update `GetLatest` to load JSONB.
- `backend/internal/ai/summarizer.go` — add `ResponseFormat` field + `GenerateUserInsightJSON`.
- `backend/internal/api/insights.go` — drop `buildSimplePrompt`, use shared prompt builder + parser, enrich `Latest` response with `rec_articles`.
- `backend/cmd/worker/insights.go` — drop `buildLayeredPrompt`, call shared builder + parser; pass `articleRepo`.
- `backend/cmd/server/main.go` — wire `articleRepo` into `InsightsHandler` and worker deps where missing.
- `frontend/src/api/client.ts` — add types + extend `LatestInsightsResponse`.
- `frontend/src/pages/InsightsPage.tsx` — render `RecommendationsCard`.

---

### Task 1: DB Migration

**Files:**
- Create: `backend/migrations/010_insight_recommendations.sql`

- [ ] **Step 1: Write the migration**

```sql
-- 010_insight_recommendations.sql
-- Add a structured "recommendations" payload to each insight: a JSONB array of
-- direction objects, each with a list of (article_id, reason). Existing rows
-- stay NULL and the API surfaces NULL/empty as "no recommendations yet".

ALTER TABLE user_insights
  ADD COLUMN IF NOT EXISTS recommendations JSONB;
```

- [ ] **Step 2: Apply locally to verify it parses**

```bash
docker-compose exec -T postgres psql -U postgres -d rsspal -f - < backend/migrations/010_insight_recommendations.sql
docker-compose exec -T postgres psql -U postgres -d rsspal -c "\d user_insights" | grep recommendations
```

Expected: a line containing `recommendations | jsonb`.

- [ ] **Step 3: Commit**

```bash
git add backend/migrations/010_insight_recommendations.sql
git commit -m "feat(insights): add recommendations JSONB column to user_insights"
```

---

### Task 2: Model Types

**Files:**
- Modify: `backend/internal/model/model.go`

- [ ] **Step 1: Add the four new types and extend UserInsight**

Locate the `UserInsight` block (around line 86) and replace it + add the helpers right after `Classification`:

```go
// UserInsight is one persisted AI-generated insight (auto or manual).
type UserInsight struct {
	ID              int                       `json:"id" db:"id"`
	UserID          int                       `json:"user_id" db:"user_id"`
	Content         string                    `json:"content" db:"content"`
	Status          string                    `json:"status" db:"status"` // "pending" | "done" | "failed"
	ErrorMsg        string                    `json:"error_msg,omitempty" db:"error_msg"`
	TriggeredBy     string                    `json:"triggered_by" db:"triggered_by"` // "auto" | "manual"
	Model           string                    `json:"model,omitempty" db:"model"`
	GeneratedAt     time.Time                 `json:"generated_at" db:"generated_at"`
	Recommendations []RecommendationDirection `json:"recommendations,omitempty" db:"recommendations"`
}

// ArticleRecommendation is one (article_id, reason) entry inside a direction.
type ArticleRecommendation struct {
	ArticleID int    `json:"article_id"`
	Reason    string `json:"reason"`
}

// RecommendationDirection groups article recommendations under one interest
// direction. Kind is "core" (strengthen existing top interest) or "emerging"
// (weak signal that recurs).
type RecommendationDirection struct {
	Direction     string                  `json:"direction"`
	DirectionKind string                  `json:"direction_kind"`
	Articles      []ArticleRecommendation `json:"articles"`
}

// InsightCandidate is one row from ArticleRepository.GetInsightCandidates,
// shipped to the AI prompt as a candidate article it may select.
type InsightCandidate struct {
	Article     Article
	AlreadyRead bool   // true when from the past-favorites slice (read 30–180d ago, ever liked/saved)
	BriefShort  string // first 60 runes of summary_brief, "" if none
}
```

- [ ] **Step 2: Verify compile**

```bash
docker-compose exec -T api sh -c "cd /app && go build ./..."
```

If `docker-compose exec` is not available locally, run from host:

```bash
cd backend && go build ./...
```

Expected: no output (success).

- [ ] **Step 3: Commit**

```bash
git add backend/internal/model/model.go
git commit -m "feat(insights): model types for recommendations + insight candidates"
```

---

### Task 3: Repo — GetInsightCandidates

**Files:**
- Modify: `backend/internal/repository/article.go`

- [ ] **Step 1: Add the method**

Append at the end of `article.go`:

```go
// GetInsightCandidates returns up to (unreadLimit + readLimit) candidate
// articles for the AI prompt:
//
//   - unread block: visible to userID, not yet completed, ranked by score
//     (existing user_preferences signal weighting) DESC, then published_at
//     DESC NULLS LAST. Caps at unreadLimit. AlreadyRead=false.
//
//   - past-favorites block: visible to userID, is_completed=true, has at least
//     one 'like' or 'save' signal, last_read_at between 30 and 180 days ago.
//     Same score+recency ranking. Caps at readLimit. AlreadyRead=true.
//
// The two blocks are concatenated (unread first). They are disjoint by
// is_completed, so no extra dedup is needed.
func (r *ArticleRepository) GetInsightCandidates(userID, unreadLimit, readLimit int) ([]model.InsightCandidate, error) {
	const (
		unreadQuery = `
SELECT a.id, a.feed_id, a.title, a.url, a.content, a.published_at,
       a.summary_brief, a.summary_detailed, a.fetched_at, a.word_count, a.reading_minutes,
       f.title AS feed_title
FROM articles a
JOIN feeds f ON a.feed_id = f.id
LEFT JOIN reading_progress rp ON a.id = rp.article_id AND rp.user_id = $1
LEFT JOIN (
    SELECT article_id, SUM(
        CASE signal_type
            WHEN 'like' THEN 5.0 * signal_value
            WHEN 'dislike' THEN -10.0 * signal_value
            WHEN 'save' THEN 3.0 * signal_value
            WHEN 'read_duration' THEN signal_value / 60.0
            ELSE 1.0 * signal_value
        END
    ) AS score
    FROM user_preferences
    WHERE user_id = $1 AND created_at > NOW() - INTERVAL '30 days'
    GROUP BY article_id
) p ON a.id = p.article_id
WHERE (f.owner_id IS NULL OR f.owner_id = $1)
  AND COALESCE(rp.is_completed, false) = false
ORDER BY COALESCE(p.score, 0) DESC, a.published_at DESC NULLS LAST
LIMIT $2
`
		readQuery = `
SELECT a.id, a.feed_id, a.title, a.url, a.content, a.published_at,
       a.summary_brief, a.summary_detailed, a.fetched_at, a.word_count, a.reading_minutes,
       f.title AS feed_title
FROM articles a
JOIN feeds f ON a.feed_id = f.id
JOIN reading_progress rp ON a.id = rp.article_id AND rp.user_id = $1
JOIN (
    SELECT article_id, SUM(
        CASE signal_type
            WHEN 'like' THEN 5.0 * signal_value
            WHEN 'save' THEN 3.0 * signal_value
            ELSE 0
        END
    ) AS score
    FROM user_preferences
    WHERE user_id = $1 AND signal_type IN ('like','save')
    GROUP BY article_id
) p ON a.id = p.article_id
WHERE (f.owner_id IS NULL OR f.owner_id = $1)
  AND rp.is_completed = true
  AND rp.last_read_at BETWEEN NOW() - INTERVAL '180 days' AND NOW() - INTERVAL '30 days'
  AND p.score > 0
ORDER BY p.score DESC, rp.last_read_at DESC
LIMIT $2
`
	)

	scan := func(rows *sql.Rows, alreadyRead bool, out *[]model.InsightCandidate) error {
		defer rows.Close()
		for rows.Next() {
			var a model.Article
			if err := rows.Scan(&a.ID, &a.FeedID, &a.Title, &a.URL, &a.Content, &a.PublishedAt,
				&a.SummaryBrief, &a.SummaryDetailed, &a.FetchedAt, &a.WordCount, &a.ReadingMinutes,
				&a.FeedTitle); err != nil {
				return err
			}
			brief := []rune(a.SummaryBrief)
			if len(brief) > 60 {
				brief = brief[:60]
			}
			*out = append(*out, model.InsightCandidate{
				Article:     a,
				AlreadyRead: alreadyRead,
				BriefShort:  string(brief),
			})
		}
		return rows.Err()
	}

	var out []model.InsightCandidate

	if unreadLimit > 0 {
		rows, err := r.db.Query(unreadQuery, userID, unreadLimit)
		if err != nil {
			return nil, err
		}
		if err := scan(rows, false, &out); err != nil {
			return nil, err
		}
	}
	if readLimit > 0 {
		rows, err := r.db.Query(readQuery, userID, readLimit)
		if err != nil {
			return nil, err
		}
		if err := scan(rows, true, &out); err != nil {
			return nil, err
		}
	}
	return out, nil
}
```

- [ ] **Step 2: Verify compile**

```bash
cd backend && go build ./...
```

Expected: no output.

- [ ] **Step 3: Commit**

```bash
git add backend/internal/repository/article.go
git commit -m "feat(insights): ArticleRepository.GetInsightCandidates (unread + past favorites)"
```

---

### Task 4: Repo — Persist & Read Recommendations

**Files:**
- Modify: `backend/internal/repository/insight.go`

- [ ] **Step 1: Update GetLatest to scan recommendations**

Replace the `GetLatest` body so it selects + unmarshals the JSONB column:

```go
// GetLatest returns the most recent insight for a user (any status), or nil.
func (r *UserInsightRepository) GetLatest(userID int) (*model.UserInsight, error) {
	row := r.db.QueryRow(`
		SELECT id, user_id, COALESCE(content, ''), status, COALESCE(error_msg, ''),
		       triggered_by, COALESCE(model, ''), generated_at, recommendations
		FROM user_insights
		WHERE user_id = $1
		ORDER BY generated_at DESC
		LIMIT 1
	`, userID)
	var ui model.UserInsight
	var recsRaw sql.NullString
	err := row.Scan(&ui.ID, &ui.UserID, &ui.Content, &ui.Status, &ui.ErrorMsg,
		&ui.TriggeredBy, &ui.Model, &ui.GeneratedAt, &recsRaw)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if recsRaw.Valid && recsRaw.String != "" {
		if jerr := json.Unmarshal([]byte(recsRaw.String), &ui.Recommendations); jerr != nil {
			// Don't fail the whole insight read; log and surface empty.
			ui.Recommendations = nil
		}
	}
	return &ui, nil
}
```

Add `"encoding/json"` to the import block at the top of the file.

- [ ] **Step 2: Add MarkDoneWithRecs**

Insert this method just below the existing `MarkDone`:

```go
// MarkDoneWithRecs upgrades a pending row to status='done' with both the
// markdown content and the validated recommendations slice (may be empty).
func (r *UserInsightRepository) MarkDoneWithRecs(id int, content string, recs []model.RecommendationDirection) error {
	var recsJSON []byte
	if len(recs) > 0 {
		b, err := json.Marshal(recs)
		if err != nil {
			return fmt.Errorf("marshal recs: %w", err)
		}
		recsJSON = b
	}
	res, err := r.db.Exec(`
		UPDATE user_insights
		SET content = $2, status = 'done', error_msg = NULL,
		    recommendations = $3::jsonb, generated_at = NOW()
		WHERE id = $1 AND status = 'pending'
	`, id, content, nullableJSONB(recsJSON))
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("no pending insight with id=%d", id)
	}
	return nil
}

// nullableJSONB returns nil for empty input so SQL stores NULL instead of
// the literal "null" string, matching the convention for the column.
func nullableJSONB(b []byte) interface{} {
	if len(b) == 0 {
		return nil
	}
	return string(b)
}
```

- [ ] **Step 3: Verify compile**

```bash
cd backend && go build ./...
```

Expected: no output.

- [ ] **Step 4: Commit**

```bash
git add backend/internal/repository/insight.go
git commit -m "feat(insights): persist + load recommendations on user_insights"
```

---

### Task 5: AI Client — JSON Mode

**Files:**
- Modify: `backend/internal/ai/summarizer.go`

- [ ] **Step 1: Add response_format support**

In the `chatRequest` struct definition, add the new field:

```go
type chatRequest struct {
	Model          string          `json:"model"`
	MaxTokens      int             `json:"max_tokens"`
	Messages       []chatMessage   `json:"messages"`
	ResponseFormat *responseFormat `json:"response_format,omitempty"`
}

type responseFormat struct {
	Type string `json:"type"` // "json_object"
}
```

- [ ] **Step 2: Add a private callJSON helper**

Right after the existing `call` function, add:

```go
// callJSON is like call but asks the API to return a JSON object. Server-side
// schema enforcement varies by provider; the parser must still validate.
func (s *Summarizer) callJSON(ctx context.Context, prompt string, maxTokens int) (string, error) {
	req := chatRequest{
		Model:     s.model,
		MaxTokens: maxTokens,
		Messages: []chatMessage{
			{Role: "system", Content: systemGuardrail},
			{Role: "user", Content: prompt},
		},
		ResponseFormat: &responseFormat{Type: "json_object"},
	}
	body, err := json.Marshal(req)
	if err != nil {
		return "", err
	}
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(time.Duration(attempt) * 3 * time.Second):
			}
		}
		result, err := s.doCall(ctx, body, maxTokens)
		if err == nil {
			return result, nil
		}
		lastErr = err
	}
	return "", lastErr
}
```

- [ ] **Step 3: Add GenerateUserInsightJSON**

Below `GenerateUserInsight` add:

```go
// GenerateUserInsightJSON is like GenerateUserInsight but asks the AI to
// return a JSON object with markdown + recommendations. Returns the raw body
// for the caller to parse and validate. maxTokens=2000 leaves room for the
// JSON envelope plus markdown plus reasons.
func (s *Summarizer) GenerateUserInsightJSON(ctx context.Context, prompt string) (string, error) {
	return s.callJSON(ctx, prompt, 2000)
}
```

- [ ] **Step 4: Verify compile**

```bash
cd backend && go build ./...
```

Expected: no output.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/ai/summarizer.go
git commit -m "feat(insights): GenerateUserInsightJSON with response_format=json_object"
```

---

### Task 6: Prompt Builder

**Files:**
- Create: `backend/internal/ai/insight_prompt.go`
- Create: `backend/internal/ai/insight_prompt_test.go`

- [ ] **Step 1: Write the failing test**

```go
// backend/internal/ai/insight_prompt_test.go
package ai

import (
	"strings"
	"testing"

	"github.com/bytedance/rss-pal/internal/model"
)

func TestBuildInsightPromptCandidatesIncludeIDsAndReadMarker(t *testing.T) {
	topics := []model.InterestTopic{{Topic: "AI", Weight: 5.0}}
	tags := []model.InterestTag{{Tag: "transformers", Weight: 3.0}}
	titles := []string{"Why GPT-5 matters"}
	cands := []model.InsightCandidate{
		{
			Article:    model.Article{ID: 123, Title: "Mixture of Experts deep dive", FeedTitle: "ML Weekly"},
			BriefShort: "How sparse routing works",
		},
		{
			Article:     model.Article{ID: 456, Title: "Old favorite on RAG", FeedTitle: "Search Blog"},
			AlreadyRead: true,
			BriefShort:  "",
		},
	}
	got := BuildInsightPrompt(topics, tags, titles, cands)
	mustContain := []string{
		"[id=123]",
		"Mixture of Experts deep dive",
		"ML Weekly",
		"How sparse routing works",
		"[id=456]",
		"[r]",                  // already-read marker
		"Old favorite on RAG",
		"\"recommendations\"", // tells AI the JSON shape
		"json",                 // schema hint
		"core",
		"emerging",
		"AI",                   // topic
		"transformers",         // tag
		"Why GPT-5 matters",    // recent title
	}
	for _, s := range mustContain {
		if !strings.Contains(got, s) {
			t.Errorf("prompt missing %q\n--- prompt ---\n%s", s, got)
		}
	}
}

func TestBuildInsightPromptEmptyCandidatesStillProducesPrompt(t *testing.T) {
	got := BuildInsightPrompt(nil, nil, nil, nil)
	if !strings.Contains(got, "\"recommendations\"") {
		t.Errorf("prompt should still describe schema even when empty:\n%s", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd backend && go test ./internal/ai/ -run TestBuildInsightPrompt -v
```

Expected: FAIL with "BuildInsightPrompt undefined".

- [ ] **Step 3: Write the implementation**

```go
// backend/internal/ai/insight_prompt.go
package ai

import (
	"fmt"
	"strings"

	"github.com/bytedance/rss-pal/internal/model"
)

// BuildInsightPrompt builds the user prompt for JSON-mode insight generation.
// Candidates are rendered as "[id=N] Title — feed · brief" so the AI references
// exact ids in the response. "[r]" prefixes already-read candidates so the AI
// can frame them as "revisit" recommendations when relevant.
func BuildInsightPrompt(topics []model.InterestTopic, tags []model.InterestTag,
	recentTitles []string, candidates []model.InsightCandidate) string {

	var b strings.Builder
	b.WriteString(`基于用户的兴趣画像和最近阅读，请输出严格 JSON：

{
  "markdown": "（4 段中文 markdown：核心兴趣领域 / 近期偏好变化 / 可能的新兴趣点 / 阅读建议）",
  "recommendations": [
    {
      "direction": "...",
      "direction_kind": "core" | "emerging",
      "articles": [
        {"article_id": <候选池中的整数 id>, "reason": "≤100 字"}
      ]
    }
  ]
}

约束：
- direction 共 2-3 个；总文章数 3-5 篇
- core = 强化已有核心兴趣；emerging = 弱信号反复出现的新兴趣点
- article_id 必须严格来自下方候选池，禁止编造 / 改动 id
- 每个 reason ≤ 100 字，必须解释为什么属于该方向
- 候选池为空或无合适文章时，"recommendations": []
- 输出必须是合法 JSON，不要包裹 markdown 代码块

`)

	if len(topics) > 0 {
		b.WriteString("## 用户兴趣主题（按权重）\n")
		for _, t := range topics {
			fmt.Fprintf(&b, "- %s (%.2f)\n", t.Topic, t.Weight)
		}
		b.WriteString("\n")
	}

	if len(tags) > 0 {
		b.WriteString("## 用户关键词（top 20，按权重）\n")
		max := 20
		if len(tags) < max {
			max = len(tags)
		}
		for i := 0; i < max; i++ {
			fmt.Fprintf(&b, "- %s (%.2f)\n", tags[i].Tag, tags[i].Weight)
		}
		b.WriteString("\n")
	}

	if len(recentTitles) > 0 {
		b.WriteString("## 最近阅读\n")
		for _, t := range recentTitles {
			fmt.Fprintf(&b, "- %s\n", t)
		}
		b.WriteString("\n")
	}

	b.WriteString(fmt.Sprintf("## 候选文章池（共 %d 条；[r] 表示已读过的过往收藏）\n", len(candidates)))
	for _, c := range candidates {
		marker := ""
		if c.AlreadyRead {
			marker = "[r] "
		}
		feed := c.Article.FeedTitle
		brief := c.BriefShort
		switch {
		case feed != "" && brief != "":
			fmt.Fprintf(&b, "[id=%d] %s%s — %s · %s\n", c.Article.ID, marker, c.Article.Title, feed, brief)
		case feed != "":
			fmt.Fprintf(&b, "[id=%d] %s%s — %s\n", c.Article.ID, marker, c.Article.Title, feed)
		default:
			fmt.Fprintf(&b, "[id=%d] %s%s\n", c.Article.ID, marker, c.Article.Title)
		}
	}

	return b.String()
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd backend && go test ./internal/ai/ -run TestBuildInsightPrompt -v
```

Expected: PASS for both cases.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/ai/insight_prompt.go backend/internal/ai/insight_prompt_test.go
git commit -m "feat(insights): shared prompt builder with candidate pool + JSON schema"
```

---

### Task 7: JSON Parser & Validator

**Files:**
- Create: `backend/internal/ai/insight_parse.go`
- Create: `backend/internal/ai/insight_parse_test.go`

- [ ] **Step 1: Write the failing tests**

```go
// backend/internal/ai/insight_parse_test.go
package ai

import (
	"strings"
	"testing"
)

func TestParseInsightJSON_HappyPath(t *testing.T) {
	raw := `{
		"markdown": "## 核心兴趣\n你喜欢分布式系统",
		"recommendations": [
			{
				"direction": "分布式系统",
				"direction_kind": "core",
				"articles": [
					{"article_id": 1, "reason": "深度讨论一致性"},
					{"article_id": 2, "reason": "Raft 算法解析"}
				]
			}
		]
	}`
	pool := map[int]bool{1: true, 2: true, 3: true}
	md, recs, dropped := ParseInsightJSON(raw, pool)
	if !strings.Contains(md, "核心兴趣") {
		t.Errorf("markdown lost: %q", md)
	}
	if len(recs) != 1 {
		t.Fatalf("want 1 direction, got %d", len(recs))
	}
	if recs[0].DirectionKind != "core" || len(recs[0].Articles) != 2 {
		t.Errorf("direction wrong: %+v", recs[0])
	}
	if len(dropped) != 0 {
		t.Errorf("did not expect drops, got %v", dropped)
	}
}

func TestParseInsightJSON_FenceWrapped(t *testing.T) {
	raw := "```json\n{\"markdown\":\"hi\",\"recommendations\":[]}\n```"
	md, recs, _ := ParseInsightJSON(raw, nil)
	if md != "hi" {
		t.Errorf("markdown = %q", md)
	}
	if len(recs) != 0 {
		t.Errorf("recs should be empty: %+v", recs)
	}
}

func TestParseInsightJSON_DropsInvalidIDsAndKinds(t *testing.T) {
	raw := `{
		"markdown": "ok",
		"recommendations": [
			{"direction": "A", "direction_kind": "core", "articles": [
				{"article_id": 1, "reason": "valid"},
				{"article_id": 999, "reason": "fake id"}
			]},
			{"direction": "B", "direction_kind": "weird", "articles": [
				{"article_id": 1, "reason": "kind not allowed"}
			]},
			{"direction": "", "direction_kind": "emerging", "articles": [
				{"article_id": 1, "reason": "empty direction"}
			]},
			{"direction": "C", "direction_kind": "emerging", "articles": [
				{"article_id": 1, "reason": ""}
			]}
		]
	}`
	pool := map[int]bool{1: true, 2: true}
	md, recs, dropped := ParseInsightJSON(raw, pool)
	if md != "ok" {
		t.Errorf("md = %q", md)
	}
	if len(recs) != 1 {
		t.Fatalf("want only the first direction, got %d: %+v", len(recs), recs)
	}
	if len(recs[0].Articles) != 1 || recs[0].Articles[0].ArticleID != 1 {
		t.Errorf("survivor wrong: %+v", recs[0])
	}
	if len(dropped) == 0 {
		t.Errorf("expected drop reasons logged, got none")
	}
}

func TestParseInsightJSON_TotalGarbage(t *testing.T) {
	md, recs, dropped := ParseInsightJSON("not json at all", map[int]bool{})
	if md != "not json at all" {
		t.Errorf("md should fall back to raw: %q", md)
	}
	if len(recs) != 0 {
		t.Errorf("recs should be empty: %+v", recs)
	}
	if len(dropped) == 0 {
		t.Errorf("should record a drop reason for unparseable input")
	}
}

func TestParseInsightJSON_CapsAt3DirectionsAnd5Articles(t *testing.T) {
	raw := `{
		"markdown": "x",
		"recommendations": [
			{"direction":"A","direction_kind":"core","articles":[{"article_id":1,"reason":"r"}]},
			{"direction":"B","direction_kind":"core","articles":[{"article_id":2,"reason":"r"}]},
			{"direction":"C","direction_kind":"core","articles":[{"article_id":3,"reason":"r"}]},
			{"direction":"D","direction_kind":"emerging","articles":[{"article_id":4,"reason":"r"}]}
		]
	}`
	pool := map[int]bool{1: true, 2: true, 3: true, 4: true, 5: true, 6: true}
	_, recs, _ := ParseInsightJSON(raw, pool)
	if len(recs) != 3 {
		t.Errorf("expected 3-direction cap, got %d", len(recs))
	}

	raw2 := `{
		"markdown": "x",
		"recommendations": [
			{"direction":"A","direction_kind":"core","articles":[
				{"article_id":1,"reason":"r"},
				{"article_id":2,"reason":"r"},
				{"article_id":3,"reason":"r"},
				{"article_id":4,"reason":"r"},
				{"article_id":5,"reason":"r"},
				{"article_id":6,"reason":"r"}
			]}
		]
	}`
	_, recs2, _ := ParseInsightJSON(raw2, pool)
	total := 0
	for _, d := range recs2 {
		total += len(d.Articles)
	}
	if total != 5 {
		t.Errorf("expected 5-article cap, got %d", total)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd backend && go test ./internal/ai/ -run TestParseInsightJSON -v
```

Expected: FAIL with "ParseInsightJSON undefined".

- [ ] **Step 3: Write the implementation**

```go
// backend/internal/ai/insight_parse.go
package ai

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/bytedance/rss-pal/internal/model"
)

const (
	maxDirections     = 3
	maxArticlesTotal  = 5
	maxReasonRunes    = 200
)

type insightEnvelope struct {
	Markdown        string                          `json:"markdown"`
	Recommendations []model.RecommendationDirection `json:"recommendations"`
}

// ParseInsightJSON extracts markdown + validated recommendations from raw AI
// output. The candidate-id whitelist is enforced; entries failing any rule are
// dropped (with a reason recorded) instead of failing the whole insight.
//
// On total parse failure it returns the raw string as markdown so the user
// still sees something readable; recs are nil; drop reasons explain why.
func ParseInsightJSON(raw string, candidateIDs map[int]bool) (string, []model.RecommendationDirection, []string) {
	dropped := []string{}

	body := stripCodeFence(strings.TrimSpace(raw))
	// Some models prepend chatter; extractJSON trims to the first balanced object.
	if !strings.HasPrefix(body, "{") {
		if j := extractJSON(body); j != "" {
			body = j
		}
	}

	var env insightEnvelope
	if err := json.Unmarshal([]byte(body), &env); err != nil {
		dropped = append(dropped, fmt.Sprintf("json parse failed: %v", err))
		return raw, nil, dropped
	}

	out := make([]model.RecommendationDirection, 0, len(env.Recommendations))
	totalArticles := 0
	for di, d := range env.Recommendations {
		if len(out) >= maxDirections {
			dropped = append(dropped, fmt.Sprintf("direction[%d] over cap %d", di, maxDirections))
			continue
		}
		if d.Direction == "" {
			dropped = append(dropped, fmt.Sprintf("direction[%d] empty name", di))
			continue
		}
		if d.DirectionKind != "core" && d.DirectionKind != "emerging" {
			dropped = append(dropped, fmt.Sprintf("direction[%d] bad kind %q", di, d.DirectionKind))
			continue
		}
		validArticles := make([]model.ArticleRecommendation, 0, len(d.Articles))
		for ai_, a := range d.Articles {
			if totalArticles >= maxArticlesTotal {
				dropped = append(dropped, fmt.Sprintf("direction[%d].article[%d] over cap %d", di, ai_, maxArticlesTotal))
				continue
			}
			if !candidateIDs[a.ArticleID] {
				dropped = append(dropped, fmt.Sprintf("direction[%d].article[%d] id=%d not in pool", di, ai_, a.ArticleID))
				continue
			}
			reason := strings.TrimSpace(a.Reason)
			if reason == "" {
				dropped = append(dropped, fmt.Sprintf("direction[%d].article[%d] empty reason", di, ai_))
				continue
			}
			if len([]rune(reason)) > maxReasonRunes {
				dropped = append(dropped, fmt.Sprintf("direction[%d].article[%d] reason too long", di, ai_))
				continue
			}
			validArticles = append(validArticles, model.ArticleRecommendation{
				ArticleID: a.ArticleID,
				Reason:    reason,
			})
			totalArticles++
		}
		if len(validArticles) == 0 {
			dropped = append(dropped, fmt.Sprintf("direction[%d] no surviving articles", di))
			continue
		}
		out = append(out, model.RecommendationDirection{
			Direction:     d.Direction,
			DirectionKind: d.DirectionKind,
			Articles:      validArticles,
		})
	}

	return env.Markdown, out, dropped
}

// stripCodeFence removes a single leading/trailing ```...``` fence if present.
func stripCodeFence(s string) string {
	if !strings.HasPrefix(s, "```") {
		return s
	}
	// Drop the first line (``` or ```json).
	if nl := strings.IndexByte(s, '\n'); nl >= 0 {
		s = s[nl+1:]
	}
	// Drop trailing fence.
	if i := strings.LastIndex(s, "```"); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd backend && go test ./internal/ai/ -run TestParseInsightJSON -v
```

Expected: PASS for all 5 cases.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/ai/insight_parse.go backend/internal/ai/insight_parse_test.go
git commit -m "feat(insights): lenient JSON parser with whitelist + caps"
```

---

### Task 8: Wire API Handler

**Files:**
- Modify: `backend/internal/api/insights.go`
- Modify: `backend/cmd/server/main.go` (pass `articleRepo` if not already)

- [ ] **Step 1: Inspect main.go to see how InsightsHandler is constructed**

```bash
grep -n "InsightsHandler\|NewInsightsHandler" backend/cmd/server/main.go
```

If `articleRepo` isn't already available where `InsightsHandler` is instantiated, add it. (It's used elsewhere in the file already.)

- [ ] **Step 2: Update InsightsHandler struct + constructor**

In `backend/internal/api/insights.go`, replace the struct + constructor:

```go
type InsightsHandler struct {
	prefRepo         *repository.PreferenceRepository
	articleRepo      *repository.ArticleRepository
	templateRepo     *repository.TemplateRepository
	userInsightsRepo *repository.UserInsightRepository
	summarizer       *ai.Summarizer
	cfg              *config.Config
}

func NewInsightsHandler(prefRepo *repository.PreferenceRepository, articleRepo *repository.ArticleRepository,
	templateRepo *repository.TemplateRepository, userInsightsRepo *repository.UserInsightRepository,
	summarizer *ai.Summarizer, cfg *config.Config) *InsightsHandler {
	return &InsightsHandler{
		prefRepo:         prefRepo,
		articleRepo:      articleRepo,
		templateRepo:     templateRepo,
		userInsightsRepo: userInsightsRepo,
		summarizer:       summarizer,
		cfg:              cfg,
	}
}
```

In `backend/cmd/server/main.go`, update the construction site to pass `articleRepo`:

```go
insightsHandler := api.NewInsightsHandler(prefRepo, articleRepo, templateRepo, userInsightsRepo, summarizer, cfg)
```

(If the variable name there is different, adapt — keep arg order matching the new constructor.)

- [ ] **Step 3: Replace `Generate` and `runAsyncManual` to use the new prompt + parser**

```go
// Generate kicks off an async insight job. Returns immediately with the
// updated quota; the actual AI call runs in a background goroutine and
// updates the user_insights row from 'pending' to 'done' (or 'failed').
func (h *InsightsHandler) Generate(c *gin.Context) {
	userID := getUserID(c)

	quota, ok := h.computeQuota(userID)
	if !ok {
		c.JSON(http.StatusTooManyRequests, gin.H{
			"error":           "quota_exceeded",
			"remaining_today": quota.RemainingToday,
			"remaining_month": quota.RemainingMonth,
		})
		return
	}

	topics, err := h.prefRepo.GetTopics(userID)
	if err != nil || len(topics) == 0 {
		c.JSON(http.StatusOK, gin.H{
			"status":          "no_data",
			"message":         "暂无足够的阅读数据来生成洞察，请先多阅读并标记文章",
			"remaining_today": quota.RemainingToday,
			"remaining_month": quota.RemainingMonth,
		})
		return
	}
	tags, _ := h.prefRepo.GetTags(userID)
	titles, _ := h.prefRepo.GetRecentReadTitles(userID, 20)
	candidates, err := h.articleRepo.GetInsightCandidates(userID, 40, 10)
	if err != nil {
		log.Printf("insights: GetInsightCandidates user=%d: %v", userID, err)
		candidates = nil
	}

	summarizer := h.chooseSummarizer(userID)
	id, err := h.userInsightsRepo.InsertPending(userID, "manual", summarizer.Model())
	if err != nil {
		if errors.Is(err, repository.ErrPendingExists) {
			c.JSON(http.StatusConflict, gin.H{
				"error":           "already_pending",
				"remaining_today": quota.RemainingToday,
				"remaining_month": quota.RemainingMonth,
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	prompt := ai.BuildInsightPrompt(topics, tags, titles, candidates)

	go h.runAsyncManual(id, userID, summarizer, prompt, candidates)

	c.JSON(http.StatusAccepted, gin.H{
		"status":          "pending",
		"id":              id,
		"remaining_today": quota.RemainingToday,
		"remaining_month": quota.RemainingMonth,
	})
}

func (h *InsightsHandler) runAsyncManual(id, userID int, s *ai.Summarizer, prompt string, candidates []model.InsightCandidate) {
	ctx, cancel := context.WithTimeout(context.Background(), asyncGenTimeout)
	defer cancel()
	raw, err := s.GenerateUserInsightJSON(ctx, prompt)
	if err != nil {
		log.Printf("insights: async user=%d id=%d failed: %v", userID, id, err)
		_ = h.userInsightsRepo.MarkFailed(id, err.Error())
		return
	}
	idSet := make(map[int]bool, len(candidates))
	for _, c := range candidates {
		idSet[c.Article.ID] = true
	}
	markdown, recs, dropped := ai.ParseInsightJSON(raw, idSet)
	if len(dropped) > 0 {
		log.Printf("insights: user=%d id=%d dropped %d entries: %v", userID, id, len(dropped), dropped)
	}
	if err := h.userInsightsRepo.MarkDoneWithRecs(id, markdown, recs); err != nil {
		log.Printf("insights: async user=%d id=%d MarkDoneWithRecs: %v", userID, id, err)
		return
	}
	log.Printf("insights: async user=%d id=%d ok (%dB md, %d recs)", userID, id, len(markdown), len(recs))
}
```

Add `"github.com/bytedance/rss-pal/internal/model"` to the imports if not already present. Remove the old `buildSimplePrompt` helper (it's replaced by `ai.BuildInsightPrompt`).

- [ ] **Step 4: Enrich Latest response with rec_articles**

Replace `Latest`:

```go
func (h *InsightsHandler) Latest(c *gin.Context) {
	userID := getUserID(c)
	ins, _ := h.userInsightsRepo.GetLatest(userID)
	quota, _ := h.computeQuota(userID)
	resp := gin.H{
		"insight":         ins,
		"remaining_today": quota.RemainingToday,
		"remaining_month": quota.RemainingMonth,
	}
	if ins != nil && len(ins.Recommendations) > 0 {
		// Collect ids in deterministic order so the JSON map is stable enough.
		ids := make([]int, 0)
		seen := map[int]bool{}
		for _, d := range ins.Recommendations {
			for _, a := range d.Articles {
				if !seen[a.ArticleID] {
					seen[a.ArticleID] = true
					ids = append(ids, a.ArticleID)
				}
			}
		}
		if len(ids) > 0 {
			arts, err := h.articleRepo.GetByIDsForUser(userID, ids)
			if err != nil {
				log.Printf("insights: Latest GetByIDsForUser user=%d: %v", userID, err)
			} else {
				meta := make(map[string]gin.H, len(arts))
				for _, a := range arts {
					brief := []rune(a.SummaryBrief)
					if len(brief) > 80 {
						brief = brief[:80]
					}
					meta[strconv.Itoa(a.ID)] = gin.H{
						"id":         a.ID,
						"title":      a.Title,
						"feed_title": a.FeedTitle,
						"brief":      string(brief),
						"is_read":    a.IsRead,
					}
				}
				resp["rec_articles"] = meta
			}
		}
	}
	c.JSON(http.StatusOK, resp)
}
```

(The `IsRead` field already exists on `model.Article` because `GetByIDsForUser` selects `COALESCE(rp.is_completed, false) AS is_read`. Verify with `grep -n "IsRead" backend/internal/model/model.go`.)

- [ ] **Step 5: Verify compile**

```bash
cd backend && go build ./...
```

Expected: no output.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/api/insights.go backend/cmd/server/main.go
git commit -m "feat(insights): handler builds candidate pool, parses JSON, enriches Latest"
```

---

### Task 9: Wire Worker Cron

**Files:**
- Modify: `backend/cmd/worker/insights.go`
- Modify: `backend/cmd/worker/main.go` (only if `articleRepo` not yet wired)

- [ ] **Step 1: Verify worker has articleRepo**

```bash
grep -n "articleRepo\|insightCronDeps" backend/cmd/worker/main.go backend/cmd/worker/insights.go | head -20
```

`insightCronDeps` already has `articleRepo *repository.ArticleRepository`. Confirm `main.go` passes it.

- [ ] **Step 2: Replace generateDailyInsights**

In `backend/cmd/worker/insights.go`, replace the `generateDailyInsights` function and remove the now-unused `buildLayeredPrompt`/`pickedArticle`/`nonEmpty`/`estimateTokens` helpers (the prompt builder lives in `internal/ai` now and we no longer hand-pick the layered structure):

```go
func generateDailyInsights(ctx context.Context, deps insightCronDeps) {
	users, err := deps.userRepo.ListAll()
	if err != nil {
		log.Printf("daily cron: ListAll users: %v", err)
		return
	}
	for _, u := range users {
		topics, _ := deps.prefRepo.GetTopics(u.ID)
		tags, _ := deps.prefRepo.GetTags(u.ID)
		if len(topics) == 0 && len(tags) == 0 {
			continue
		}
		titles, _ := deps.prefRepo.GetRecentReadTitles(u.ID, 20)
		candidates, err := deps.articleRepo.GetInsightCandidates(u.ID, 40, 10)
		if err != nil {
			log.Printf("daily cron: user %d GetInsightCandidates: %v", u.ID, err)
			candidates = nil
		}

		prompt := ai.BuildInsightPrompt(topics, tags, titles, candidates)
		id, err := deps.userInsightsRepo.InsertPending(u.ID, "auto", deps.defaultModel)
		if err != nil {
			log.Printf("daily cron: user %d InsertPending: %v", u.ID, err)
			continue
		}
		raw, err := deps.summarizer.GenerateUserInsightJSON(ctx, prompt)
		if err != nil {
			log.Printf("daily cron: user %d generate: %v", u.ID, err)
			_ = deps.userInsightsRepo.MarkFailed(id, err.Error())
			continue
		}
		idSet := make(map[int]bool, len(candidates))
		for _, c := range candidates {
			idSet[c.Article.ID] = true
		}
		markdown, recs, dropped := ai.ParseInsightJSON(raw, idSet)
		if len(dropped) > 0 {
			log.Printf("daily cron: user %d dropped %d entries: %v", u.ID, len(dropped), dropped)
		}
		if err := deps.userInsightsRepo.MarkDoneWithRecs(id, markdown, recs); err != nil {
			log.Printf("daily cron: user %d MarkDoneWithRecs: %v", u.ID, err)
			continue
		}
		log.Printf("daily cron: user %d ok (topics=%d tags=%d, %dB md, %d recs)",
			u.ID, len(topics), len(tags), len(markdown), len(recs))
		select {
		case <-ctx.Done():
			return
		case <-time.After(200 * time.Millisecond):
		}
	}
}
```

- [ ] **Step 3: Delete now-dead helpers**

Remove `buildLayeredPrompt`, `pickedArticle`, `nonEmpty`, `estimateTokens`, and the `insightTokenBudget` constant from `insights.go`. Imports of `strings` and `fmt` may become unused — let `goimports`/the compiler tell you.

If `cmd/worker/insights_test.go` references any of those helpers, update the test in this same task to either delete obsolete cases or migrate them onto `BuildInsightPrompt`. Run:

```bash
cd backend && go test ./cmd/worker/ -v
```

Fix any compile/assertion errors before continuing.

- [ ] **Step 4: Verify compile**

```bash
cd backend && go build ./...
```

Expected: no output.

- [ ] **Step 5: Commit**

```bash
git add backend/cmd/worker/insights.go backend/cmd/worker/insights_test.go
git commit -m "feat(insights): worker cron uses shared prompt + JSON parser"
```

---

### Task 10: Frontend Types

**Files:**
- Modify: `frontend/src/api/client.ts`

- [ ] **Step 1: Inspect existing types around insights**

```bash
grep -n "PersistedInsight\|getLatestInsights\|generateInsights" frontend/src/api/client.ts
```

- [ ] **Step 2: Add new exported types and extend PersistedInsight + the Latest response**

Add near the existing insight types:

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
```

In the existing `PersistedInsight` interface, add the optional field:

```ts
export interface PersistedInsight {
  // ... existing fields ...
  recommendations?: RecommendationDirection[]
}
```

In the `getLatestInsights` response type (whatever its name — find via the grep above), add:

```ts
rec_articles?: Record<string, RecArticleMeta>
```

- [ ] **Step 3: Verify TS compile**

```bash
cd frontend && npx tsc --noEmit
```

Expected: no output.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/api/client.ts
git commit -m "feat(insights): frontend types for recommendations + rec_articles"
```

---

### Task 11: RecommendationsCard Component

**Files:**
- Create: `frontend/src/components/RecommendationsCard.tsx`

- [ ] **Step 1: Write the component**

```tsx
// frontend/src/components/RecommendationsCard.tsx
import { useNavigate } from 'react-router-dom'
import type { RecommendationDirection, RecArticleMeta } from '../api/client'

interface Props {
  recommendations: RecommendationDirection[]
  articles: Record<string, RecArticleMeta>
}

const KIND_LABEL: Record<string, string> = {
  core: '强化你的核心兴趣',
  emerging: '可能的新兴趣点',
}
const KIND_COLOR: Record<string, string> = {
  core: '#1a56db',
  emerging: '#7c3aed',
}

export default function RecommendationsCard({ recommendations, articles }: Props) {
  const navigate = useNavigate()
  if (!recommendations || recommendations.length === 0) return null

  // Filter directions whose articles all lost their metadata.
  const visibleDirs = recommendations
    .map(d => ({ ...d, articles: d.articles.filter(a => articles[String(a.article_id)]) }))
    .filter(d => d.articles.length > 0)
  if (visibleDirs.length === 0) return null

  return (
    <div className="card">
      <h3 className="mb-2">📍 为你推荐</h3>
      {visibleDirs.map((d, i) => (
        <div key={i} style={{ marginBottom: 16 }}>
          <div
            style={{
              fontWeight: 600,
              color: KIND_COLOR[d.direction_kind] || '#1a56db',
              marginBottom: 8,
              fontSize: 14,
            }}
          >
            ▸ {KIND_LABEL[d.direction_kind] || ''}：{d.direction}
          </div>
          <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
            {d.articles.map(a => {
              const meta = articles[String(a.article_id)]
              return (
                <div
                  key={a.article_id}
                  onClick={() => navigate(`/articles/${a.article_id}`)}
                  style={{
                    padding: 12,
                    border: '1px solid #e5e7eb',
                    borderRadius: 8,
                    cursor: 'pointer',
                    background: '#fafafa',
                    transition: 'background 0.1s',
                  }}
                  onMouseEnter={e => (e.currentTarget.style.background = '#f3f4f6')}
                  onMouseLeave={e => (e.currentTarget.style.background = '#fafafa')}
                >
                  <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'baseline' }}>
                    <span style={{ fontWeight: 500 }}>{meta.title}</span>
                    {meta.is_read && (
                      <span
                        style={{
                          fontSize: 11,
                          color: '#9ca3af',
                          background: '#e5e7eb',
                          padding: '2px 8px',
                          borderRadius: 10,
                          marginLeft: 8,
                          flexShrink: 0,
                        }}
                      >
                        已读过
                      </span>
                    )}
                  </div>
                  <div className="text-muted text-sm" style={{ marginTop: 4 }}>
                    {meta.feed_title}
                    {meta.brief ? ` · ${meta.brief}` : ''}
                  </div>
                  <div style={{ marginTop: 6, fontSize: 13, color: '#374151' }}>💡 {a.reason}</div>
                </div>
              )
            })}
          </div>
        </div>
      ))}
    </div>
  )
}
```

- [ ] **Step 2: Verify TS compile**

```bash
cd frontend && npx tsc --noEmit
```

Expected: no output.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/RecommendationsCard.tsx
git commit -m "feat(insights): RecommendationsCard renders direction-grouped article cards"
```

---

### Task 12: Wire RecommendationsCard into InsightsPage

**Files:**
- Modify: `frontend/src/pages/InsightsPage.tsx`

- [ ] **Step 1: Import the component and the new types**

At the top of `InsightsPage.tsx`, add:

```tsx
import RecommendationsCard from '../components/RecommendationsCard'
import type { RecArticleMeta } from '../api/client'
```

- [ ] **Step 2: Track rec_articles state**

Add state next to the existing `insight` state:

```tsx
const [recArticles, setRecArticles] = useState<Record<string, RecArticleMeta>>({})
```

- [ ] **Step 3: Update both refresh paths to capture rec_articles**

In `refresh`:

```tsx
const refresh = async () => {
  const latest = await getLatestInsights().catch(() => null)
  if (!latest) return null
  setRemainingToday(latest.remaining_today)
  setRemainingMonth(latest.remaining_month)
  setInsight(latest.insight)
  setRecArticles(latest.rec_articles || {})
  return latest.insight
}
```

And in the initial `useEffect`:

```tsx
if (latest) {
  setRemainingToday(latest.remaining_today)
  setRemainingMonth(latest.remaining_month)
  setInsight(latest.insight)
  setRecArticles(latest.rec_articles || {})
}
```

- [ ] **Step 4: Render RecommendationsCard after the insight markdown card**

Locate the closing `</div>` of the AI insight `card` (the one containing `<ReactMarkdown>{insight.content}</ReactMarkdown>`) and insert immediately after it:

```tsx
{insight?.status === 'done' && insight.recommendations && (
  <RecommendationsCard
    recommendations={insight.recommendations}
    articles={recArticles}
  />
)}
```

- [ ] **Step 5: Verify TS compile**

```bash
cd frontend && npx tsc --noEmit
```

Expected: no output.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/pages/InsightsPage.tsx
git commit -m "feat(insights): render RecommendationsCard on InsightsPage"
```

---

### Task 13: Manual Smoke + Docker Rebuild

**Files:** none (verification only).

Frontend changes don't hot-reload — nginx serves a built bundle (per project memory).

- [ ] **Step 1: Apply the migration in the running DB**

```bash
docker-compose exec -T postgres psql -U postgres -d rsspal -f - < backend/migrations/010_insight_recommendations.sql
```

Expected: `ALTER TABLE` and no errors.

- [ ] **Step 2: Rebuild backend + frontend images**

```bash
docker-compose up -d --build api worker frontend
```

Expected: builds succeed; all three containers come up healthy.

- [ ] **Step 3: Trigger a manual generation**

Open `/insights` in the browser, log in if needed, click the generate button. Wait ~30–60s for the pending row to flip to done.

Verify in DB:

```bash
docker-compose exec -T postgres psql -U postgres -d rsspal -c "SELECT id, status, jsonb_array_length(recommendations), LENGTH(content) FROM user_insights ORDER BY id DESC LIMIT 1;"
```

Expected: status `done`, `jsonb_array_length` 1–3, `content` length non-zero.

- [ ] **Step 4: Verify UI renders the recommendations card**

In the browser:
- The "📍 为你推荐" card appears below the insight markdown card.
- 1–3 direction sections, each with 1–2 article cards.
- Clicking an article card navigates to `/articles/:id`.
- An already-read article (if present) shows the "已读过" badge.

If no recommendations were produced (rare — small candidate pool, AI returned empty), trigger again or seed more reading signals first.

- [ ] **Step 5: Quick failure-mode sanity**

Force a JSON parse failure to verify the lenient path:

```bash
docker-compose exec -T postgres psql -U postgres -d rsspal -c "INSERT INTO user_insights (user_id, content, status, triggered_by) VALUES (1, 'fallback markdown', 'done', 'manual');"
```

Reload `/insights` and confirm:
- Markdown still renders.
- Recommendations card does not appear (because `recommendations` is null).

Clean up the test row:

```bash
docker-compose exec -T postgres psql -U postgres -d rsspal -c "DELETE FROM user_insights WHERE content = 'fallback markdown';"
```

---

### Task 14: Push Branch & Open PR

- [ ] **Step 1: Confirm branch state**

```bash
git status
git log --oneline master..HEAD
```

Expected: clean tree; ~10 new commits since master diverged on this feature.

- [ ] **Step 2: Push and create PR**

```bash
git push -u origin feature/insights-full
gh pr create --title "feat(insights): article recommendations grouped by interest direction" --body "$(cat <<'EOF'
## Summary
- Add a structured `recommendations` payload to each AI insight: 2–3 interest directions (core / emerging), each with 1–2 articles drawn from a 40-unread + 10-past-favorites candidate pool.
- AI returns JSON; backend validates ids against the candidate whitelist (lenient — drops bad entries instead of failing the insight).
- New `RecommendationsCard` on `/insights` renders the cards; clicking opens the normal article view so reading-progress + like/save signals continue feeding the recommender.

## Spec & Plan
- Spec: `docs/superpowers/specs/2026-05-07-insight-recommendations-design.md`
- Plan: `docs/superpowers/plans/2026-05-07-insight-recommendations.md`

## Test plan
- [x] Backend unit tests: `BuildInsightPrompt`, `ParseInsightJSON` (table-driven).
- [x] Manual smoke: generate insight from UI; verify recs render and click-through works.
- [x] Failure path: legacy insight without `recommendations` still renders markdown only.

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

- [ ] **Step 3: Capture and report the PR URL.**

---

## Self-Review Notes

- **Spec coverage:** All 8 spec sections (migration, model, repo, prompt, parser, AI client, handler, worker, frontend) have a task.
- **No placeholders.** Each step has concrete code or commands.
- **Type consistency:** `model.RecommendationDirection`/`ArticleRecommendation`/`InsightCandidate`, `MarkDoneWithRecs`, `BuildInsightPrompt`, `ParseInsightJSON`, `GenerateUserInsightJSON`, `GetInsightCandidates` — all referenced consistently in later tasks.
- **Frontend rebuild gotcha:** Task 13 explicitly rebuilds frontend per project memory ("Frontend changes require Docker rebuild").
