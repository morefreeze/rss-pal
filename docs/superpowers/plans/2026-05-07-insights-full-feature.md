# 洞察完整功能 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the end-to-end insights feature (topic+tag extraction pipeline, daily cron, streaming + persisted insights, quota, dual tag clouds) per `docs/superpowers/specs/2026-05-07-insights-full-feature-design.md`.

**Architecture:** Worker scans articles with strong signals, calls AI once per article (single JSON returning topic+tags), caches on `articles.topic`/`articles.tags`. API server uses cache to upsert into `interest_topics`/`interest_tags` synchronously when present. Daily 04:00 UTC+8 cron decays both interest tables and generates AI insights for active users into `user_insights` (append-only). Frontend reads latest, streams manual regenerations via NDJSON, with 3/day · 100/month quota.

**Tech Stack:** Go 1.21 + Gin + database/sql + lib/pq (backend), React 18 + TypeScript + Vite (frontend), PostgreSQL 15. AI provider: z.ai `glm-4.5` via OpenAI-compatible HTTP. No new dependencies.

**Branch:** `feature/insights-full` (already created off master, spec already committed).

**Tests:** Follow existing repo convention — pure logic gets `_test.go` (table-driven), DB/handler integration via manual smoke commands (psql + curl). Frontend has no test framework; verify in browser after `docker-compose up -d --build frontend`.

---

## Milestones

1. **Schema & data layer** (Tasks 1-5) — DB migration, model structs, repository methods. No AI yet.
2. **AI classification + cache hit path** (Tasks 6-8) — `ClassifyArticle`, signal handler cache-hit branch.
3. **Worker integration** (Tasks 9-11) — scan loop, daily decay + insights cron.
4. **API endpoints** (Tasks 12-14) — `/insights/latest`, streaming, tag endpoints.
5. **Frontend** (Tasks 15-17) — client API, state machine, dual cloud UI.

Commit after each task. Each task is self-contained and leaves the tree green (`go build ./...`).

---

### Task 1: Schema migration

**Files:**
- Create: `backend/migrations/008_insights.sql`

- [ ] **Step 1: Write the migration**

```sql
-- backend/migrations/008_insights.sql
-- Adds the data backbone for the insights feature: per-article classification cache,
-- per-user fine-grained tag interests, and persisted insight generations.

ALTER TABLE articles
  ADD COLUMN IF NOT EXISTS topic TEXT,
  ADD COLUMN IF NOT EXISTS tags  TEXT[];

CREATE INDEX IF NOT EXISTS idx_articles_no_topic
  ON articles (id) WHERE topic IS NULL;

CREATE TABLE IF NOT EXISTS interest_tags (
  id                 SERIAL PRIMARY KEY,
  user_id            INT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  tag                TEXT NOT NULL,
  weight             FLOAT NOT NULL DEFAULT 0,
  last_reinforced_at TIMESTAMP NOT NULL DEFAULT NOW(),
  UNIQUE (user_id, tag)
);

CREATE INDEX IF NOT EXISTS idx_interest_tags_user_weight
  ON interest_tags (user_id, weight DESC);

CREATE TABLE IF NOT EXISTS user_insights (
  id           SERIAL PRIMARY KEY,
  user_id      INT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  content      TEXT NOT NULL,
  triggered_by VARCHAR(16) NOT NULL CHECK (triggered_by IN ('auto','manual')),
  model        VARCHAR(64),
  generated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_user_insights_user_latest
  ON user_insights (user_id, generated_at DESC);

CREATE INDEX IF NOT EXISTS idx_user_insights_quota
  ON user_insights (user_id, triggered_by, generated_at);
```

- [ ] **Step 2: Apply migration manually for local dev**

Run: `docker-compose exec -T postgres psql -U postgres -d rsspal < backend/migrations/008_insights.sql`
Expected: a series of `ALTER TABLE` / `CREATE TABLE` / `CREATE INDEX` outputs, no errors.

- [ ] **Step 3: Verify schema**

Run: `docker-compose exec postgres psql -U postgres -d rsspal -c "\d interest_tags"`
Expected: shows the table with `user_id`, `tag`, `weight`, `last_reinforced_at`, the UNIQUE constraint, and the index.

Run: `docker-compose exec postgres psql -U postgres -d rsspal -c "\d user_insights"`
Expected: shows the table with the `triggered_by` CHECK constraint.

Run: `docker-compose exec postgres psql -U postgres -d rsspal -c "\d articles" | grep -E "topic|tags"`
Expected: shows `topic | text` and `tags | text[]`.

- [ ] **Step 4: Commit**

```bash
git add backend/migrations/008_insights.sql
git commit -m "feat(db): add insights schema (articles.topic/tags, interest_tags, user_insights)"
```

---

### Task 2: Model structs

**Files:**
- Modify: `backend/internal/model/model.go`

- [ ] **Step 1: Add new types at the end of `backend/internal/model/model.go`**

Append after `PreferenceRequest`:

```go
// InterestTag is the fine-grained counterpart of InterestTopic.
type InterestTag struct {
	ID               int       `json:"id" db:"id"`
	Tag              string    `json:"tag" db:"tag"`
	Weight           float64   `json:"weight" db:"weight"`
	LastReinforcedAt time.Time `json:"last_reinforced_at" db:"last_reinforced_at"`
}

// UserInsight is one persisted AI-generated insight (auto or manual).
type UserInsight struct {
	ID          int       `json:"id" db:"id"`
	UserID      int       `json:"user_id" db:"user_id"`
	Content     string    `json:"content" db:"content"`
	TriggeredBy string    `json:"triggered_by" db:"triggered_by"` // "auto" | "manual"
	Model       string    `json:"model,omitempty" db:"model"`
	GeneratedAt time.Time `json:"generated_at" db:"generated_at"`
}

// Classification is what the AI returns for one article.
type Classification struct {
	Topic string   `json:"topic"`
	Tags  []string `json:"tags"`
}
```

- [ ] **Step 2: Verify build**

Run: `cd backend && go build ./...`
Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add backend/internal/model/model.go
git commit -m "feat(model): add InterestTag, UserInsight, Classification structs"
```

---

### Task 3: ArticleRepository — classification methods

**Files:**
- Modify: `backend/internal/repository/article.go` (append methods at end of file)

- [ ] **Step 1: Add the four methods**

Append at end of `backend/internal/repository/article.go`:

```go
// FindArticlesNeedingClassification returns up to `limit` articles that have
// strong signals in the last 7 days but no cached topic.
func (r *ArticleRepository) FindArticlesNeedingClassification(limit int) ([]model.Article, error) {
	query := `
		SELECT DISTINCT a.id, a.title, COALESCE(a.content, '')
		FROM articles a
		JOIN user_preferences up ON up.article_id = a.id
		WHERE a.topic IS NULL
		  AND up.created_at > NOW() - INTERVAL '7 days'
		  AND (
		    up.signal_type IN ('like','save')
		    OR (up.signal_type = 'read_duration' AND up.signal_value >= 60)
		  )
		LIMIT $1
	`
	rows, err := r.db.Query(query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Article
	for rows.Next() {
		var a model.Article
		if err := rows.Scan(&a.ID, &a.Title, &a.Content); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, nil
}

// SetClassification writes topic + tags onto an article. Pass empty string and
// empty slice to mark the article as "AI returned nothing" (still cached, won't retry).
func (r *ArticleRepository) SetClassification(articleID int, topic string, tags []string) error {
	_, err := r.db.Exec(
		`UPDATE articles SET topic = $1, tags = $2 WHERE id = $3`,
		nullableString(topic), pq.Array(tags), articleID,
	)
	return err
}

// GetClassification reads the cached topic + tags for one article.
// Returns ("", nil, nil) when not yet classified.
func (r *ArticleRepository) GetClassification(articleID int) (string, []string, error) {
	var topic sql.NullString
	var tags pq.StringArray
	err := r.db.QueryRow(
		`SELECT topic, tags FROM articles WHERE id = $1`, articleID,
	).Scan(&topic, &tags)
	if err != nil {
		return "", nil, err
	}
	return topic.String, []string(tags), nil
}

// GetTopTopicVocabulary returns the most-frequent topics across articles, used
// as a recommendation list for the AI classifier (B3 self-stabilizing vocabulary).
func (r *ArticleRepository) GetTopTopicVocabulary(limit int) ([]string, error) {
	rows, err := r.db.Query(`
		SELECT topic
		FROM articles
		WHERE topic IS NOT NULL AND topic <> ''
		GROUP BY topic
		ORDER BY COUNT(*) DESC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
}

func nullableString(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}
```

- [ ] **Step 2: Verify build**

Run: `cd backend && go build ./...`
Expected: no errors. (`pq.Array` and `pq.StringArray` are already imported via `lib/pq`.)

- [ ] **Step 3: Commit**

```bash
git add backend/internal/repository/article.go
git commit -m "feat(repo): article classification cache methods (Find/Set/Get/Vocab)"
```

---

### Task 4: PreferenceRepository — tag, decay, signal-strength methods

**Files:**
- Modify: `backend/internal/repository/preference.go`

- [ ] **Step 1: Append tag-mirror, decay-all, signal-strength, and delete methods**

Append at end of `backend/internal/repository/preference.go`:

```go
// --- interest_tags (mirror of interest_topics, finer grain) ---

func (r *PreferenceRepository) GetTags(userID int) ([]model.InterestTag, error) {
	rows, err := r.db.Query(
		`SELECT id, tag, weight, last_reinforced_at FROM interest_tags
		 WHERE user_id = $1 ORDER BY weight DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.InterestTag
	for rows.Next() {
		var t model.InterestTag
		if err := rows.Scan(&t.ID, &t.Tag, &t.Weight, &t.LastReinforcedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
}

func (r *PreferenceRepository) UpsertTag(userID int, tag string, weightDelta float64) error {
	if tag == "" {
		return nil
	}
	_, err := r.db.Exec(`
		INSERT INTO interest_tags (user_id, tag, weight, last_reinforced_at)
		VALUES ($1, $2, $3, NOW())
		ON CONFLICT (user_id, tag) DO UPDATE SET
		  weight = interest_tags.weight + $3,
		  last_reinforced_at = NOW()
	`, userID, tag, weightDelta)
	return err
}

func (r *PreferenceRepository) DeleteTopic(userID, id int) (int64, error) {
	res, err := r.db.Exec(
		`DELETE FROM interest_topics WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (r *PreferenceRepository) DeleteTag(userID, id int) (int64, error) {
	res, err := r.db.Exec(
		`DELETE FROM interest_tags WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// --- decay (all users) ---

func (r *PreferenceRepository) DecayAllTopics(factor float64) error {
	_, err := r.db.Exec(
		`UPDATE interest_topics SET weight = weight * $1 WHERE weight > 0.01`, factor)
	return err
}

func (r *PreferenceRepository) DecayAllTags(factor float64) error {
	_, err := r.db.Exec(
		`UPDATE interest_tags SET weight = weight * $1 WHERE weight > 0.01`, factor)
	return err
}

// --- signal strength aggregation (used by worker) ---

type UserSignalStrength struct {
	UserID   int
	Strength float64
}

// GetUsersWithStrongSignal returns each user's MAX signal strength against an article.
// Used by the worker after classifying to attribute the topic/tags to all interested users.
func (r *PreferenceRepository) GetUsersWithStrongSignal(articleID int) ([]UserSignalStrength, error) {
	rows, err := r.db.Query(`
		SELECT user_id,
		       MAX(CASE signal_type
		           WHEN 'save' THEN 2.0
		           WHEN 'like' THEN 1.0
		           WHEN 'read_duration' THEN
		             CASE WHEN signal_value >= 60 THEN 0.5 ELSE 0 END
		           ELSE 0
		       END) AS strength
		FROM user_preferences
		WHERE article_id = $1
		GROUP BY user_id
		HAVING MAX(CASE signal_type
		           WHEN 'save' THEN 2.0
		           WHEN 'like' THEN 1.0
		           WHEN 'read_duration' THEN
		             CASE WHEN signal_value >= 60 THEN 0.5 ELSE 0 END
		           ELSE 0
		       END) > 0
	`, articleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UserSignalStrength
	for rows.Next() {
		var u UserSignalStrength
		if err := rows.Scan(&u.UserID, &u.Strength); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, nil
}
```

- [ ] **Step 2: Verify build**

Run: `cd backend && go build ./...`
Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add backend/internal/repository/preference.go
git commit -m "feat(repo): interest_tags methods, decay-all, signal-strength aggregation, deletes"
```

---

### Task 5: UserInsightRepository

**Files:**
- Create: `backend/internal/repository/insight.go`

- [ ] **Step 1: Write the new repository**

```go
// backend/internal/repository/insight.go
package repository

import (
	"database/sql"
	"fmt"

	"github.com/bytedance/rss-pal/internal/model"
)

type UserInsightRepository struct {
	db *sql.DB
}

func NewUserInsightRepository(db *sql.DB) *UserInsightRepository {
	return &UserInsightRepository{db: db}
}

func (r *UserInsightRepository) Insert(userID int, content, triggeredBy, model string) error {
	if triggeredBy != "auto" && triggeredBy != "manual" {
		return fmt.Errorf("invalid triggered_by: %s", triggeredBy)
	}
	_, err := r.db.Exec(`
		INSERT INTO user_insights (user_id, content, triggered_by, model)
		VALUES ($1, $2, $3, NULLIF($4, ''))
	`, userID, content, triggeredBy, model)
	return err
}

// GetLatest returns the most recent insight for a user, or nil if none.
func (r *UserInsightRepository) GetLatest(userID int) (*model.UserInsight, error) {
	row := r.db.QueryRow(`
		SELECT id, user_id, content, triggered_by, COALESCE(model, ''), generated_at
		FROM user_insights
		WHERE user_id = $1
		ORDER BY generated_at DESC
		LIMIT 1
	`, userID)
	var ui model.UserInsight
	err := row.Scan(&ui.ID, &ui.UserID, &ui.Content, &ui.TriggeredBy, &ui.Model, &ui.GeneratedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &ui, nil
}

// CountManualSince returns how many manual generations the user has done within
// the given Postgres interval (e.g. "1 day", "30 days").
func (r *UserInsightRepository) CountManualSince(userID int, interval string) (int, error) {
	q := fmt.Sprintf(`
		SELECT COUNT(*) FROM user_insights
		WHERE user_id = $1 AND triggered_by = 'manual'
		  AND generated_at > NOW() - INTERVAL '%s'
	`, interval)
	var n int
	if err := r.db.QueryRow(q, userID).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}
```

Note: the interval string is constant in callers (`"1 day"` / `"30 days"`), not user input — no SQL injection risk.

- [ ] **Step 2: Verify build**

Run: `cd backend && go build ./...`
Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add backend/internal/repository/insight.go
git commit -m "feat(repo): UserInsightRepository (Insert/GetLatest/CountManualSince)"
```

---

### Task 6: AI ClassifyArticle + extractJSON helper

**Files:**
- Create: `backend/internal/ai/classify.go`
- Create: `backend/internal/ai/classify_test.go`

- [ ] **Step 1: Write the failing test**

```go
// backend/internal/ai/classify_test.go
package ai

import (
	"reflect"
	"testing"
)

func TestExtractJSON(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain", `{"topic":"AI","tags":["a"]}`, `{"topic":"AI","tags":["a"]}`},
		{"with fence", "```json\n{\"topic\":\"AI\",\"tags\":[]}\n```", `{"topic":"AI","tags":[]}`},
		{"prefix garbage", "Sure! {\"topic\":\"x\"}", `{"topic":"x"}`},
		{"trailing text", `{"topic":"x","tags":["y"]}  more notes`, `{"topic":"x","tags":["y"]}`},
		{"no braces", `not json at all`, ``},
		{"nested", `{"topic":"x","tags":["a","b"],"extra":{"k":1}}`, `{"topic":"x","tags":["a","b"],"extra":{"k":1}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := extractJSON(tc.in)
			if got != tc.want {
				t.Errorf("extractJSON(%q) = %q; want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestParseClassification(t *testing.T) {
	cases := []struct {
		name      string
		raw       string
		wantTopic string
		wantTags  []string
		wantErr   bool
	}{
		{"happy", `{"topic":"AI","tags":["OpenAI","GPT-5"]}`, "AI", []string{"OpenAI", "GPT-5"}, false},
		{"with fence", "```\n{\"topic\":\"金融\",\"tags\":[\"FOMC\"]}\n```", "金融", []string{"FOMC"}, false},
		{"empty tags", `{"topic":"编程","tags":[]}`, "编程", []string{}, false},
		{"missing tags ok", `{"topic":"AI"}`, "AI", nil, false},
		{"junk", `not json`, "", nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cls, err := parseClassification(tc.raw)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %+v", cls)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if cls.Topic != tc.wantTopic {
				t.Errorf("topic = %q; want %q", cls.Topic, tc.wantTopic)
			}
			if !reflect.DeepEqual(cls.Tags, tc.wantTags) {
				t.Errorf("tags = %v; want %v", cls.Tags, tc.wantTags)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/ai/ -run "TestExtractJSON|TestParseClassification" -v`
Expected: FAIL — `extractJSON` and `parseClassification` undefined.

- [ ] **Step 3: Write the implementation**

```go
// backend/internal/ai/classify.go
package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/bytedance/rss-pal/internal/model"
)

// extractJSON returns the substring from the first '{' to its matching '}',
// or "" if no balanced object is found. Tolerates markdown fences and prefix text.
func extractJSON(s string) string {
	start := strings.Index(s, "{")
	if start < 0 {
		return ""
	}
	depth := 0
	inStr := false
	escape := false
	for i := start; i < len(s); i++ {
		c := s[i]
		if escape {
			escape = false
			continue
		}
		if c == '\\' && inStr {
			escape = true
			continue
		}
		if c == '"' {
			inStr = !inStr
			continue
		}
		if inStr {
			continue
		}
		switch c {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	return ""
}

func parseClassification(raw string) (*model.Classification, error) {
	j := extractJSON(raw)
	if j == "" {
		return nil, fmt.Errorf("no JSON object in AI response")
	}
	var cls model.Classification
	if err := json.Unmarshal([]byte(j), &cls); err != nil {
		return nil, fmt.Errorf("invalid classification JSON: %w", err)
	}
	cls.Topic = strings.TrimSpace(cls.Topic)
	cleaned := cls.Tags[:0]
	for _, t := range cls.Tags {
		t = strings.TrimSpace(t)
		if t != "" {
			cleaned = append(cleaned, t)
		}
	}
	cls.Tags = cleaned
	return &cls, nil
}

// ClassifyArticle asks the AI to assign one topic + 3-5 tags to an article.
// recommendedTopics is the B3 vocabulary list (DB-frequency-driven + seeds).
func (s *Summarizer) ClassifyArticle(ctx context.Context, title, content string,
	recommendedTopics []string) (*model.Classification, error) {
	content = truncateContent(content)
	rec := strings.Join(recommendedTopics, ", ")
	prompt := fmt.Sprintf(`你是文章分类助手。请分析以下文章并返回 JSON：

{"topic": "...", "tags": ["...", "...", "..."]}

- topic：单选，最贴合的主题。优先从已有主题中选：[%s]，
  如均不贴合可创建新主题（控制在 2-4 字的中文名词）。
- tags：3-5 个具体关键词（人名、产品名、公司、概念）。

仅输出 JSON，无其他内容。

标题：%s

内容：
%s`, rec, title, content)

	raw, err := s.call(ctx, prompt, 200)
	if err != nil {
		return nil, err
	}
	return parseClassification(raw)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/ai/ -run "TestExtractJSON|TestParseClassification" -v`
Expected: PASS for all cases.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/ai/classify.go backend/internal/ai/classify_test.go
git commit -m "feat(ai): ClassifyArticle returning structured topic+tags JSON"
```

---

### Task 7: Signal-to-weight pure function + tests

**Files:**
- Create: `backend/internal/api/signalweight.go`
- Create: `backend/internal/api/signalweight_test.go`

- [ ] **Step 1: Write the failing test**

```go
// backend/internal/api/signalweight_test.go
package api

import "testing"

func TestSignalToTopicWeight(t *testing.T) {
	cases := []struct {
		strength float64
		want     float64
	}{
		{2.0, 2.0}, // save
		{1.0, 1.0}, // like
		{0.5, 0.5}, // read>=60
		{0.0, 0.0}, // none
	}
	for _, tc := range cases {
		got := SignalToTopicWeight(tc.strength)
		if got != tc.want {
			t.Errorf("SignalToTopicWeight(%v) = %v; want %v", tc.strength, got, tc.want)
		}
	}
}

func TestSignalToTagWeight(t *testing.T) {
	if got := SignalToTagWeight(2.0); got != 1.0 {
		t.Errorf("save tag weight = %v; want 1.0", got)
	}
	if got := SignalToTagWeight(1.0); got != 0.5 {
		t.Errorf("like tag weight = %v; want 0.5", got)
	}
	if got := SignalToTagWeight(0.0); got != 0.0 {
		t.Errorf("zero tag weight = %v; want 0.0", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/api/ -run "TestSignalTo" -v`
Expected: FAIL — undefined `SignalToTopicWeight` / `SignalToTagWeight`.

- [ ] **Step 3: Write implementation**

```go
// backend/internal/api/signalweight.go
package api

// SignalToTopicWeight returns the weight delta to apply to interest_topics
// for a given signal strength (already aggregated: save=2.0, like=1.0, read>=60=0.5).
func SignalToTopicWeight(strength float64) float64 {
	return strength
}

// SignalToTagWeight returns the weight delta for interest_tags. Half of the
// topic weight, so the magnitude stays comparable despite tags being more numerous.
func SignalToTagWeight(strength float64) float64 {
	return strength * 0.5
}

// StrengthFromSignal aggregates a single signal_type+value into the same
// scale used by GetUsersWithStrongSignal (kept here so handler cache-hit
// branches can compute strength without an extra DB round-trip).
func StrengthFromSignal(signalType string, signalValue float64) float64 {
	switch signalType {
	case "save":
		return 2.0
	case "like":
		return 1.0
	case "read_duration":
		if signalValue >= 60 {
			return 0.5
		}
		return 0
	}
	return 0
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/api/ -run "TestSignalTo" -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/api/signalweight.go backend/internal/api/signalweight_test.go
git commit -m "feat(api): pure signal-to-weight helpers (topic/tag/strength)"
```

---

### Task 8: Wire cache-hit branch in like / save / read-duration handlers

**Files:**
- Modify: `backend/internal/api/preference.go`

- [ ] **Step 1: Read the current handlers**

Open `backend/internal/api/preference.go` and locate `Like`, `Save`, `RecordReadDuration`. They each call `h.prefRepo.Add(pref)`.

- [ ] **Step 2: Add the article repo dependency**

Update the `PreferenceHandler` struct and constructor at the top of the file:

```go
type PreferenceHandler struct {
	prefRepo    *repository.PreferenceRepository
	articleRepo *repository.ArticleRepository
}

func NewPreferenceHandler(prefRepo *repository.PreferenceRepository, articleRepo *repository.ArticleRepository) *PreferenceHandler {
	return &PreferenceHandler{prefRepo: prefRepo, articleRepo: articleRepo}
}
```

- [ ] **Step 3: Add a private helper that runs the cache-hit branch**

Append below the constructor (before `Like`):

```go
// applyCachedClassification, when the article already has a cached classification,
// upserts the topic + tags into the user's interest tables synchronously. Silently
// no-ops when the article is not yet classified (worker will pick it up).
func (h *PreferenceHandler) applyCachedClassification(userID, articleID int, signalType string, signalValue float64) {
	if h.articleRepo == nil {
		return
	}
	topic, tags, err := h.articleRepo.GetClassification(articleID)
	if err != nil || topic == "" {
		return
	}
	strength := StrengthFromSignal(signalType, signalValue)
	if strength <= 0 {
		return
	}
	tw := SignalToTopicWeight(strength)
	gw := SignalToTagWeight(strength)
	_ = h.prefRepo.UpsertTopic(userID, topic, tw)
	for _, t := range tags {
		_ = h.prefRepo.UpsertTag(userID, t, gw)
	}
}
```

- [ ] **Step 4: Call the helper from each handler**

In `Like`, after the existing `h.prefRepo.Add(pref)` block, add:

```go
	h.applyCachedClassification(pref.UserID, pref.ArticleID, "like", 1.0)
```

In `Save`, after `Add`:

```go
	h.applyCachedClassification(pref.UserID, pref.ArticleID, "save", 1.0)
```

In `RecordReadDuration`, after `Add`:

```go
	h.applyCachedClassification(pref.UserID, pref.ArticleID, "read_duration", req.DurationSeconds)
```

- [ ] **Step 5: Update the constructor call site**

Modify `backend/cmd/server/main.go`. Find:

```go
prefHandler := api.NewPreferenceHandler(prefRepo)
```

Change to:

```go
prefHandler := api.NewPreferenceHandler(prefRepo, articleRepo)
```

- [ ] **Step 6: Verify build**

Run: `cd backend && go build ./...`
Expected: no errors.

- [ ] **Step 7: Commit**

```bash
git add backend/internal/api/preference.go backend/cmd/server/main.go
git commit -m "feat(api): cache-hit branch upserts topic+tags on like/save/read-duration"
```

---

### Task 9: Worker — scanAndClassify loop

**Files:**
- Create: `backend/cmd/worker/classify.go`

- [ ] **Step 1: Write the new file**

```go
// backend/cmd/worker/classify.go
package main

import (
	"context"
	"log"
	"time"

	"github.com/bytedance/rss-pal/internal/ai"
	"github.com/bytedance/rss-pal/internal/api"
	"github.com/bytedance/rss-pal/internal/repository"
)

const (
	classifyBatchSize = 50
	classifyTimeout   = 30 * time.Second
)

var seedTopics = []string{"AI", "金融", "编程", "创业", "科技", "时事", "文化", "健康"}

// runClassifyCycle finds articles with strong signals but no cached topic and
// asks the AI to classify them in one JSON call per article. After classification,
// every user with a strong signal against that article gets the topic + tags
// applied to their interest_topics / interest_tags tables.
func runClassifyCycle(ctx context.Context, articleRepo *repository.ArticleRepository,
	prefRepo *repository.PreferenceRepository, summarizer *ai.Summarizer) {
	if summarizer == nil {
		return
	}

	candidates, err := articleRepo.FindArticlesNeedingClassification(classifyBatchSize)
	if err != nil {
		log.Printf("classify: find candidates: %v", err)
		return
	}
	if len(candidates) == 0 {
		return
	}

	vocab := buildVocab(articleRepo)
	log.Printf("classify: %d articles to classify; vocab=%v", len(candidates), vocab[:min(len(vocab), 5)])

	for i := range candidates {
		art := &candidates[i]
		cCtx, cancel := context.WithTimeout(ctx, classifyTimeout)
		cls, err := summarizer.ClassifyArticle(cCtx, art.Title, art.Content, vocab)
		cancel()
		if err != nil {
			log.Printf("classify: article %d failed: %v", art.ID, err)
			continue
		}

		// Always cache (even empty) so we don't retry forever.
		if err := articleRepo.SetClassification(art.ID, cls.Topic, cls.Tags); err != nil {
			log.Printf("classify: SetClassification(%d): %v", art.ID, err)
			continue
		}

		users, err := prefRepo.GetUsersWithStrongSignal(art.ID)
		if err != nil {
			log.Printf("classify: GetUsersWithStrongSignal(%d): %v", art.ID, err)
			continue
		}
		for _, u := range users {
			tw := api.SignalToTopicWeight(u.Strength)
			gw := api.SignalToTagWeight(u.Strength)
			if cls.Topic != "" {
				_ = prefRepo.UpsertTopic(u.UserID, cls.Topic, tw)
			}
			for _, t := range cls.Tags {
				_ = prefRepo.UpsertTag(u.UserID, t, gw)
			}
		}
		log.Printf("classify: article %d → topic=%q tags=%v users=%d",
			art.ID, cls.Topic, cls.Tags, len(users))
	}
}

func buildVocab(articleRepo *repository.ArticleRepository) []string {
	top, err := articleRepo.GetTopTopicVocabulary(20)
	if err != nil {
		log.Printf("classify: GetTopTopicVocabulary: %v", err)
		top = nil
	}
	seen := make(map[string]struct{}, len(top)+len(seedTopics))
	out := make([]string, 0, len(top)+len(seedTopics))
	for _, t := range top {
		if _, ok := seen[t]; !ok {
			seen[t] = struct{}{}
			out = append(out, t)
		}
	}
	for _, t := range seedTopics {
		if _, ok := seen[t]; !ok {
			seen[t] = struct{}{}
			out = append(out, t)
		}
	}
	return out
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
```

- [ ] **Step 2: Wire into `runFetchCycle`**

Modify `backend/cmd/worker/main.go`. Find:

```go
func runFetchCycle(ctx context.Context, feedRepo *repository.FeedRepository, articleRepo *repository.ArticleRepository, fetcher *rss.Fetcher, contentFetcher *rss.ContentFetcher, summarizer *ai.Summarizer) {
	if !cycleMu.TryLock() {
		log.Println("Previous fetch cycle still running, skipping")
		return
	}
	defer cycleMu.Unlock()

	fetchAllFeeds(ctx, feedRepo, articleRepo, fetcher, contentFetcher, summarizer)
	refetchShortContent(ctx, articleRepo, contentFetcher, summarizer)
	if summarizer != nil {
		backfillSummaries(ctx, articleRepo, summarizer)
	}
}
```

Change the signature and body:

```go
func runFetchCycle(ctx context.Context, feedRepo *repository.FeedRepository, articleRepo *repository.ArticleRepository, prefRepo *repository.PreferenceRepository, fetcher *rss.Fetcher, contentFetcher *rss.ContentFetcher, summarizer *ai.Summarizer) {
	if !cycleMu.TryLock() {
		log.Println("Previous fetch cycle still running, skipping")
		return
	}
	defer cycleMu.Unlock()

	fetchAllFeeds(ctx, feedRepo, articleRepo, fetcher, contentFetcher, summarizer)
	refetchShortContent(ctx, articleRepo, contentFetcher, summarizer)
	if summarizer != nil {
		backfillSummaries(ctx, articleRepo, summarizer)
		runClassifyCycle(ctx, articleRepo, prefRepo, summarizer)
	}
}
```

Then update both call sites in `main`. Find:

```go
	feedRepo := repository.NewFeedRepository(db)
	articleRepo := repository.NewArticleRepository(db)
```

Add right after:

```go
	prefRepo := repository.NewPreferenceRepository(db)
```

Find both `runFetchCycle(context.Background(), feedRepo, articleRepo, fetcher, contentFetcher, summarizer)` calls (one initial, one inside `for range ticker.C`) and update them to pass `prefRepo`:

```go
	runFetchCycle(context.Background(), feedRepo, articleRepo, prefRepo, fetcher, contentFetcher, summarizer)
```

- [ ] **Step 3: Verify build**

Run: `cd backend && go build ./...`
Expected: no errors.

- [ ] **Step 4: Smoke test classify cycle (manual)**

Start services:
```bash
docker-compose up -d --build worker
```

After ~1 min, in another terminal:
```bash
docker-compose logs --tail=100 worker | grep -i classify
```

If you have a non-empty `articles + user_preferences` setup:
Expected: a line like `classify: 12 articles to classify; vocab=[...]` followed by `classify: article N → topic="AI" tags=[...] users=K` lines.

If the DB is fresh, you'll see no log lines (no candidates) — that's also OK.

- [ ] **Step 5: Commit**

```bash
git add backend/cmd/worker/classify.go backend/cmd/worker/main.go
git commit -m "feat(worker): scan-and-classify loop wires AI topic+tags into interests"
```

---

### Task 10: Worker — daily cron scaffold

**Files:**
- Create: `backend/cmd/worker/insights.go`

This task only adds the cron scaffolding (timer + decay). Insight generation comes in Task 11.

- [ ] **Step 1: Write the file**

```go
// backend/cmd/worker/insights.go
package main

import (
	"context"
	"log"
	"time"

	"github.com/bytedance/rss-pal/internal/ai"
	"github.com/bytedance/rss-pal/internal/repository"
)

// dailyHourCST is 04:00 UTC+8.
const dailyHourCST = 4

const decayFactor = 0.98

// scheduleDailyInsightCron arranges generateDailyInsights to run every 24h at
// 04:00 UTC+8. Stop the returned cancel func to abort. Survives missed wakeups
// (always reschedules from "now → next 04:00").
func scheduleDailyInsightCron(deps insightCronDeps) context.CancelFunc {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		for {
			next := nextDaily0400CST(time.Now())
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Until(next)):
				log.Printf("daily insight cron: firing at %s", time.Now().Format(time.RFC3339))
				runDailyInsightJob(ctx, deps)
			}
		}
	}()
	return cancel
}

func nextDaily0400CST(now time.Time) time.Time {
	cst := time.FixedZone("CST", 8*3600)
	n := now.In(cst)
	target := time.Date(n.Year(), n.Month(), n.Day(), dailyHourCST, 0, 0, 0, cst)
	if !target.After(n) {
		target = target.Add(24 * time.Hour)
	}
	return target
}

type insightCronDeps struct {
	userRepo         *repository.UserRepository
	prefRepo         *repository.PreferenceRepository
	articleRepo      *repository.ArticleRepository
	userInsightsRepo *repository.UserInsightRepository
	templateRepo     *repository.TemplateRepository
	summarizer       *ai.Summarizer
	defaultModel     string
}

func runDailyInsightJob(ctx context.Context, deps insightCronDeps) {
	if err := deps.prefRepo.DecayAllTopics(decayFactor); err != nil {
		log.Printf("daily cron: DecayAllTopics: %v", err)
	}
	if err := deps.prefRepo.DecayAllTags(decayFactor); err != nil {
		log.Printf("daily cron: DecayAllTags: %v", err)
	}
	// generateDailyInsights filled in by Task 11
	generateDailyInsights(ctx, deps)
}
```

- [ ] **Step 2: Add a stub for `generateDailyInsights` in the same file**

Append:

```go
func generateDailyInsights(ctx context.Context, deps insightCronDeps) {
	// implemented in Task 11
}
```

- [ ] **Step 3: Wire into `worker/main.go`**

Add a `userInsightsRepo`, `templateRepo`, `userRepo` near the existing `articleRepo` initialization, then schedule the cron. After `defer db.Close()`:

```go
	userRepo := repository.NewUserRepository(db)
	templateRepo := repository.NewTemplateRepository(db)
	userInsightsRepo := repository.NewUserInsightRepository(db)
```

After the summarizer is created, near the bottom of `main()` before the ticker loop:

```go
	if summarizer != nil {
		stopCron := scheduleDailyInsightCron(insightCronDeps{
			userRepo:         userRepo,
			prefRepo:         prefRepo,
			articleRepo:      articleRepo,
			userInsightsRepo: userInsightsRepo,
			templateRepo:     templateRepo,
			summarizer:       summarizer,
			defaultModel:     ai.DefaultModel,
		})
		defer stopCron()
	}
```

- [ ] **Step 4: Add a focused test for the cron-time math**

Create `backend/cmd/worker/insights_test.go`:

```go
package main

import (
	"testing"
	"time"
)

func TestNextDaily0400CST(t *testing.T) {
	cst := time.FixedZone("CST", 8*3600)
	cases := []struct {
		name string
		now  time.Time
		want time.Time
	}{
		{
			name: "before 4am same day",
			now:  time.Date(2026, 5, 7, 1, 30, 0, 0, cst),
			want: time.Date(2026, 5, 7, 4, 0, 0, 0, cst),
		},
		{
			name: "after 4am next day",
			now:  time.Date(2026, 5, 7, 9, 0, 0, 0, cst),
			want: time.Date(2026, 5, 8, 4, 0, 0, 0, cst),
		},
		{
			name: "exactly 4am next day (target must be strictly after now)",
			now:  time.Date(2026, 5, 7, 4, 0, 0, 0, cst),
			want: time.Date(2026, 5, 8, 4, 0, 0, 0, cst),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := nextDaily0400CST(tc.now)
			if !got.Equal(tc.want) {
				t.Errorf("nextDaily0400CST(%v) = %v; want %v", tc.now, got, tc.want)
			}
		})
	}
}
```

Run: `cd backend && go test ./cmd/worker/ -run TestNextDaily0400CST -v`
Expected: PASS.

- [ ] **Step 5: Verify worker builds and starts**

Run: `cd backend && go build ./...`
Expected: no errors.

Run: `docker-compose up -d --build worker && sleep 5 && docker-compose logs --tail=20 worker`
Expected: worker starts; no panics. (No "cron firing" log yet because next 04:00 is hours away.)

- [ ] **Step 6: Commit**

```bash
git add backend/cmd/worker/insights.go backend/cmd/worker/insights_test.go backend/cmd/worker/main.go
git commit -m "feat(worker): daily 04:00 UTC+8 cron scaffold + decay-all"
```

---

### Task 11: Worker — generate daily insights (fills the stub)

**Files:**
- Modify: `backend/cmd/worker/insights.go`
- Modify: `backend/internal/ai/summarizer.go` (export `Call` so worker can reuse it for non-stream insight generation, or — preferred — add a public `GenerateUserInsight` wrapper)

- [ ] **Step 1: Add a public `GenerateUserInsight` to the Summarizer**

In `backend/internal/ai/summarizer.go` append (next to existing `GenerateInsights`):

```go
// GenerateUserInsight runs a non-streaming chat completion with the layered
// prompt the worker built. maxTokens defaults to 1500 if zero.
func (s *Summarizer) GenerateUserInsight(ctx context.Context, prompt string) (string, error) {
	return s.call(ctx, prompt, 1500)
}

// Model returns the configured model id (used by user_insights.model column).
func (s *Summarizer) Model() string {
	return s.model
}
```

Verify build: `cd backend && go build ./...`.

- [ ] **Step 2: Add prompt-builder helpers in worker**

Append to `backend/cmd/worker/insights.go`:

```go
import "fmt"
import "strings"

import "github.com/bytedance/rss-pal/internal/model"

// (Adjust the existing import block in insights.go to include the above; do not add duplicate imports.)

const insightTokenBudget = 6000 // approx; chinese chars ~1.5 tokens each

// estimateTokens is intentionally approximate: 1 token ≈ 2 bytes for Chinese-heavy text.
func estimateTokens(s string) int {
	return len(s) / 2
}

type pickedArticle struct {
	Article model.Article
	Brief   string
	Detail  string
	Signal  string // "save"|"like"|"read"|"dislike"
	Within7 bool
}

// gatherUserContext loads the L1/L2/L3 articles plus topics/tags for a user.
// Heavy lifting is in additional repository helpers added in Task 12 — this task
// composes them into the prompt string.
func buildLayeredPrompt(topics []model.InterestTopic, tags []model.InterestTag,
	l3, l2, l1 []pickedArticle) string {
	var b strings.Builder
	b.WriteString("基于用户的兴趣画像与多层级阅读行为，请进行个性化洞察分析。\n\n")

	if len(topics) > 0 {
		b.WriteString("## 用户兴趣主题（粗粒度，按权重，已做时间衰减）\n")
		for _, t := range topics {
			fmt.Fprintf(&b, "- %s (%.2f)\n", t.Topic, t.Weight)
		}
		b.WriteString("\n")
	}

	if len(tags) > 0 {
		b.WriteString("## 用户关键词（细粒度，top 20，按权重）\n")
		max := 20
		if len(tags) < max {
			max = len(tags)
		}
		for i := 0; i < max; i++ {
			fmt.Fprintf(&b, "- %s (%.2f)\n", tags[i].Tag, tags[i].Weight)
		}
		b.WriteString("\n")
	}

	if len(l3) > 0 {
		b.WriteString("## 高强度信号（深度互动，含详细总结）\n")
		for i, p := range l3 {
			fmt.Fprintf(&b, "%d. [%s] 标题：%s\n   主题：%s · 标签：%v\n   摘要：%s\n",
				i+1, p.Signal, p.Article.Title, p.Article.SummaryBrief, /* placeholder if no topic */
				deriveTagsForArticle(p.Article), nonEmpty(p.Detail, p.Brief))
		}
		b.WriteString("\n")
	}

	if len(l2) > 0 {
		b.WriteString("## 强信号（含 brief）\n")
		for _, p := range l2 {
			fmt.Fprintf(&b, "- [%s] %s\n  要点：%s\n", p.Signal, p.Article.Title, p.Brief)
		}
		b.WriteString("\n")
	}

	if len(l1) > 0 {
		b.WriteString("## 浏览过的文章（仅标题）\n")
		var w7, w30 []string
		for _, p := range l1 {
			if p.Within7 {
				w7 = append(w7, p.Article.Title)
			} else {
				w30 = append(w30, p.Article.Title)
			}
		}
		if len(w7) > 0 {
			fmt.Fprintf(&b, "- 近 7 天：%s\n", strings.Join(w7, "、"))
		}
		if len(w30) > 0 {
			fmt.Fprintf(&b, "- 近 30 天：%s\n", strings.Join(w30, "、"))
		}
		b.WriteString("\n")
	}

	b.WriteString(`请用中文 markdown 输出：
1. **核心兴趣领域**（3-5 个，按确定性排序，结合主题与高频标签）
2. **近期偏好变化**（对比 7d vs 30d）
3. **可能的新兴趣点**（弱信号但反复出现）
4. **阅读建议**（结合"不感兴趣"做反向建议）
`)
	return b.String()
}

func nonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// deriveTagsForArticle is a stub that returns empty until Task 12 wires the
// per-article tags lookup. Kept inline here to keep this task self-contained.
func deriveTagsForArticle(_ model.Article) []string { return nil }
```

(Note: in Task 12 we replace the `deriveTagsForArticle` stub by joining `articles.tags` from a per-article fetch. For now, the prompt contains an empty list slot — still useful.)

- [ ] **Step 3: Replace the stub `generateDailyInsights`**

Replace the placeholder body added in Task 10 with:

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
		// L1/L2/L3 fetched from new repo helpers in Task 12; for the cron MVP
		// pass empty slices, which still produces useful "topic+tag overview"
		// insights.
		prompt := buildLayeredPrompt(topics, tags, nil, nil, nil)
		if estimateTokens(prompt) > insightTokenBudget {
			log.Printf("daily cron: user %d prompt too long, skipping", u.ID)
			continue
		}
		content, err := deps.summarizer.GenerateUserInsight(ctx, prompt)
		if err != nil {
			log.Printf("daily cron: user %d generate: %v", u.ID, err)
			continue
		}
		if err := deps.userInsightsRepo.Insert(u.ID, content, "auto", deps.defaultModel); err != nil {
			log.Printf("daily cron: user %d insert: %v", u.ID, err)
			continue
		}
		log.Printf("daily cron: user %d ok (topics=%d tags=%d, %dB)", u.ID, len(topics), len(tags), len(content))
		// Per spec: 200ms inter-user pause to avoid bursting the AI provider.
		select {
		case <-ctx.Done():
			return
		case <-time.After(200 * time.Millisecond):
		}
	}
}
```

- [ ] **Step 4: Add `userRepo.ListAll`**

Open `backend/internal/repository/user.go`. If `ListAll` doesn't exist, append:

```go
// ListAll returns every user (id-ordered). Used by daily cron.
func (r *UserRepository) ListAll() ([]model.User, error) {
	rows, err := r.db.Query(`SELECT id, username, COALESCE(is_admin, false) FROM users ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.User
	for rows.Next() {
		var u model.User
		if err := rows.Scan(&u.ID, &u.Username, &u.IsAdmin); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, nil
}
```

If `ListAll` (or equivalent) already exists with a different signature, adapt the call site instead — do not add a second method.

- [ ] **Step 5: Manual smoke (force a run)**

Add a temporary test hook to `backend/cmd/worker/insights_test.go`:

```go
func TestRunDailyInsightJob_Smoke(t *testing.T) {
	t.Skip("manual smoke; run with -run TestRunDailyInsightJob_Smoke -v -count=1")
	// Fill in deps from a live DB if you want to manually exercise it.
}
```

Or just temporarily change `dailyHourCST` to `time.Now().Hour()+1` and observe.

For automated CI we rely on `TestNextDaily0400CST` already added.

- [ ] **Step 6: Verify build**

Run: `cd backend && go build ./...`
Expected: no errors.

- [ ] **Step 7: Commit**

```bash
git add backend/cmd/worker/insights.go backend/cmd/worker/insights_test.go backend/internal/ai/summarizer.go backend/internal/repository/user.go
git commit -m "feat(worker): daily insight generation calls AI per active user"
```

---

### Task 12: API — `/insights/latest` + `/insights/generate` (non-stream) + quota

**Files:**
- Modify: `backend/internal/api/insights.go`
- Modify: `backend/cmd/server/main.go`

- [ ] **Step 1: Replace `insights.go` with the expanded handler**

Open `backend/internal/api/insights.go` and replace the `InsightsHandler` definition + `Generate` with:

```go
package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/bytedance/rss-pal/internal/ai"
	"github.com/bytedance/rss-pal/internal/config"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/gin-gonic/gin"
)

type InsightsHandler struct {
	prefRepo         *repository.PreferenceRepository
	templateRepo     *repository.TemplateRepository
	userInsightsRepo *repository.UserInsightRepository
	summarizer       *ai.Summarizer
	cfg              *config.Config
}

func NewInsightsHandler(prefRepo *repository.PreferenceRepository, templateRepo *repository.TemplateRepository,
	userInsightsRepo *repository.UserInsightRepository, summarizer *ai.Summarizer, cfg *config.Config) *InsightsHandler {
	return &InsightsHandler{
		prefRepo:         prefRepo,
		templateRepo:     templateRepo,
		userInsightsRepo: userInsightsRepo,
		summarizer:       summarizer,
		cfg:              cfg,
	}
}

const (
	dailyManualLimit   = 3
	monthlyManualLimit = 100
)

type insightQuota struct {
	RemainingToday int `json:"remaining_today"`
	RemainingMonth int `json:"remaining_month"`
}

func (h *InsightsHandler) computeQuota(userID int) (insightQuota, bool) {
	today, _ := h.userInsightsRepo.CountManualSince(userID, "1 day")
	month, _ := h.userInsightsRepo.CountManualSince(userID, "30 days")
	q := insightQuota{
		RemainingToday: dailyManualLimit - today,
		RemainingMonth: monthlyManualLimit - month,
	}
	if q.RemainingToday < 0 {
		q.RemainingToday = 0
	}
	if q.RemainingMonth < 0 {
		q.RemainingMonth = 0
	}
	return q, q.RemainingToday > 0 && q.RemainingMonth > 0
}

// Latest returns the most recent insight + quota.
func (h *InsightsHandler) Latest(c *gin.Context) {
	userID := getUserID(c)
	ins, _ := h.userInsightsRepo.GetLatest(userID)
	quota, _ := h.computeQuota(userID)
	c.JSON(http.StatusOK, gin.H{
		"insight":         ins,
		"remaining_today": quota.RemainingToday,
		"remaining_month": quota.RemainingMonth,
	})
}

func (h *InsightsHandler) chooseSummarizer(userID int) *ai.Summarizer {
	if h.templateRepo == nil {
		return h.summarizer
	}
	aiCfg, err := h.templateRepo.GetUserAIConfig(userID)
	if err != nil || aiCfg == nil || aiCfg.APIKey == "" {
		return h.summarizer
	}
	baseURL := aiCfg.BaseURL
	if baseURL == "" {
		baseURL = h.cfg.Claude.BaseURL
	}
	return ai.NewSummarizerWithModel(aiCfg.APIKey, baseURL, aiCfg.Model)
}

// Generate (non-streaming, kept for backward compat). Streaming variant in Task 13.
func (h *InsightsHandler) Generate(c *gin.Context) {
	if c.Query("stream") == "1" {
		h.GenerateStream(c)
		return
	}

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

	topics, err := h.prefRepo.GetTopicStrings(userID)
	if err != nil || len(topics) == 0 {
		c.JSON(http.StatusOK, gin.H{
			"insights":        "",
			"message":         "暂无足够的阅读数据来生成洞察，请先多阅读并标记文章",
			"remaining_today": quota.RemainingToday,
			"remaining_month": quota.RemainingMonth,
		})
		return
	}

	titles, _ := h.prefRepo.GetRecentReadTitles(userID, 20)
	summarizer := h.chooseSummarizer(userID)

	insights, err := summarizer.GenerateInsights(c.Request.Context(), topics, strings.Join(titles, "\n"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "生成洞察失败: " + err.Error()})
		return
	}

	if err := h.userInsightsRepo.Insert(userID, insights, "manual", summarizer.Model()); err != nil {
		// Persistence failure is non-fatal: still return content; just log.
		c.Header("X-Insight-Save-Error", err.Error())
	}

	quota, _ = h.computeQuota(userID)
	c.JSON(http.StatusOK, gin.H{
		"insights":        insights,
		"remaining_today": quota.RemainingToday,
		"remaining_month": quota.RemainingMonth,
	})
}

// GenerateStream is implemented in insights_stream.go (Task 13).

// DeleteTopic / DeleteTag handlers belong on PreferenceHandler — added in Task 14.

// (helper) parseIDParam parses :id from the route.
func parseIDParam(c *gin.Context, key string) (int, bool) {
	v := c.Param(key)
	id, err := strconv.Atoi(v)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid " + key})
		return 0, false
	}
	return id, true
}
```

- [ ] **Step 2: Wire into `cmd/server/main.go`**

Find:

```go
insightsHandler := api.NewInsightsHandler(prefRepo, templateRepo, summarizer, cfg)
```

Add a new repo and update the constructor:

```go
userInsightsRepo := repository.NewUserInsightRepository(db)
insightsHandler := api.NewInsightsHandler(prefRepo, templateRepo, userInsightsRepo, summarizer, cfg)
```

Find the existing route:

```go
		apiGroup.POST("/insights/generate", insightsHandler.Generate)
```

Add a new route directly above or below it:

```go
		apiGroup.GET("/insights/latest", insightsHandler.Latest)
```

- [ ] **Step 3: Verify build**

Run: `cd backend && go build ./...`
Expected: no errors.

- [ ] **Step 4: Smoke `/insights/latest`**

```bash
docker-compose up -d --build api
TOKEN=$(curl -s -X POST localhost:8080/api/auth/login -d '{"username":"<u>","password":"<p>"}' -H 'content-type: application/json' | jq -r .token)
curl -s -H "Authorization: Bearer $TOKEN" localhost:8080/api/insights/latest | jq
```

Expected: `{"insight":null,"remaining_today":3,"remaining_month":100}` for a fresh user.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/api/insights.go backend/cmd/server/main.go
git commit -m "feat(api): /insights/latest + quota; persist manual generations"
```

---

### Task 13: Streaming `/insights/generate?stream=1`

**Files:**
- Create: `backend/internal/api/insights_stream.go`

- [ ] **Step 1: Write the streaming handler**

```go
// backend/internal/api/insights_stream.go
package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/bytedance/rss-pal/internal/ai"
	"github.com/gin-gonic/gin"
)

func (h *InsightsHandler) GenerateStream(c *gin.Context) {
	userID := getUserID(c)

	c.Header("Content-Type", "application/x-ndjson")
	c.Header("Cache-Control", "no-cache, no-transform")
	c.Header("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)

	emit := func(payload any) {
		b, err := json.Marshal(payload)
		if err != nil {
			return
		}
		c.Writer.Write(b)
		c.Writer.Write([]byte("\n"))
		if f, ok := c.Writer.(http.Flusher); ok {
			f.Flush()
		}
	}

	quota, ok := h.computeQuota(userID)
	if !ok {
		emit(map[string]any{
			"type":            "error",
			"msg":             "quota_exceeded",
			"remaining_today": quota.RemainingToday,
			"remaining_month": quota.RemainingMonth,
		})
		return
	}

	topics, err := h.prefRepo.GetTopicStrings(userID)
	if err != nil || len(topics) == 0 {
		emit(map[string]any{
			"type":            "error",
			"msg":             "no_data",
			"remaining_today": quota.RemainingToday,
			"remaining_month": quota.RemainingMonth,
		})
		return
	}
	titles, _ := h.prefRepo.GetRecentReadTitles(userID, 20)

	summarizer := h.chooseSummarizer(userID)
	prompt := buildInsightStreamPrompt(topics, titles)

	var full strings.Builder
	_, err = streamCall(c, summarizer, prompt, func(delta string) {
		full.WriteString(delta)
		emit(map[string]any{"type": "delta", "text": delta})
	})
	if err != nil {
		emit(map[string]any{"type": "error", "msg": err.Error()})
		return
	}

	if err := h.userInsightsRepo.Insert(userID, full.String(), "manual", summarizer.Model()); err != nil {
		// emit error but content is already streamed; client treats as soft-error
		emit(map[string]any{"type": "error", "msg": "save_failed: " + err.Error()})
		return
	}

	quota, _ = h.computeQuota(userID)
	emit(map[string]any{
		"type":            "done",
		"full":            full.String(),
		"remaining_today": quota.RemainingToday,
		"remaining_month": quota.RemainingMonth,
	})
}

// streamCall is a thin shim invoking the existing summarizer.callStream method,
// which is package-private. Expose a public `CallStream` on Summarizer first.
func streamCall(c *gin.Context, s *ai.Summarizer, prompt string, onDelta func(string)) (string, error) {
	return s.CallStream(c.Request.Context(), prompt, 1500, onDelta)
}

func buildInsightStreamPrompt(topics, titles []string) string {
	return "基于用户的兴趣主题和最近阅读，请用中文 markdown 给出洞察分析（核心兴趣领域 / 近期偏好变化 / 可能的新兴趣点 / 阅读建议）：\n\n" +
		"## 用户兴趣主题（按权重排序）\n" + strings.Join(topics, "\n") + "\n\n" +
		"## 最近阅读的文章标题\n" + strings.Join(titles, "\n")
}
```

- [ ] **Step 2: Expose `CallStream` on Summarizer**

In `backend/internal/ai/summarizer.go`, add (next to `GenerateUserInsight` from Task 11):

```go
// CallStream is the public wrapper of callStream — used by the API server's
// streaming insight endpoint.
func (s *Summarizer) CallStream(ctx context.Context, prompt string, maxTokens int, onDelta func(string)) (string, error) {
	return s.callStream(ctx, prompt, maxTokens, onDelta)
}
```

- [ ] **Step 3: Verify build**

Run: `cd backend && go build ./...`
Expected: no errors.

- [ ] **Step 4: Smoke the stream end-to-end**

```bash
docker-compose up -d --build api
TOKEN=...
curl -N -H "Authorization: Bearer $TOKEN" -H 'content-type: application/json' \
  -X POST 'localhost:8080/api/insights/generate?stream=1'
```

Expected: a sequence of NDJSON lines with `{"type":"delta","text":"..."}` (assuming the user has topics) then a final `{"type":"done","full":"...","remaining_today":2,"remaining_month":99}`. Then `curl -s -H ... /api/insights/latest | jq` shows the persisted record with `triggered_by: "manual"`.

If the user has no topics: a single `{"type":"error","msg":"no_data",...}` line.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/api/insights_stream.go backend/internal/ai/summarizer.go
git commit -m "feat(api): NDJSON streaming /insights/generate?stream=1 with quota frame"
```

---

### Task 14: Tag endpoints + topic/tag delete handlers

**Files:**
- Modify: `backend/internal/api/preference.go`
- Modify: `backend/cmd/server/main.go`

- [ ] **Step 1: Add three new handlers to `PreferenceHandler`**

Append to `backend/internal/api/preference.go` (after `GetTopics`):

```go
func (h *PreferenceHandler) GetTags(c *gin.Context) {
	tags, err := h.prefRepo.GetTags(getUserID(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, tags)
}

func (h *PreferenceHandler) DeleteTopic(c *gin.Context) {
	id, ok := parseIDParam(c, "id")
	if !ok {
		return
	}
	rows, err := h.prefRepo.DeleteTopic(getUserID(c), id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if rows == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *PreferenceHandler) DeleteTag(c *gin.Context) {
	id, ok := parseIDParam(c, "id")
	if !ok {
		return
	}
	rows, err := h.prefRepo.DeleteTag(getUserID(c), id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if rows == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	c.Status(http.StatusNoContent)
}
```

(`parseIDParam` was added in Task 12 inside `insights.go` — both files share the `api` package so it's reusable as-is.)

- [ ] **Step 2: Register routes in server**

In `backend/cmd/server/main.go`, find the existing block:

```go
		apiGroup.GET("/preferences/topics", prefHandler.GetTopics)
```

Add directly below:

```go
		apiGroup.GET("/preferences/tags", prefHandler.GetTags)
		apiGroup.DELETE("/preferences/topics/:id", prefHandler.DeleteTopic)
		apiGroup.DELETE("/preferences/tags/:id", prefHandler.DeleteTag)
```

- [ ] **Step 3: Verify build**

Run: `cd backend && go build ./...`
Expected: no errors.

- [ ] **Step 4: Smoke**

```bash
docker-compose up -d --build api
TOKEN=...
curl -s -H "Authorization: Bearer $TOKEN" localhost:8080/api/preferences/tags | jq
# Insert a fake tag for smoke:
docker-compose exec postgres psql -U postgres -d rsspal -c \
  "INSERT INTO interest_tags (user_id, tag, weight) VALUES (1, 'smoke', 1.0);"
TAG_ID=$(curl -s -H "Authorization: Bearer $TOKEN" localhost:8080/api/preferences/tags | jq '.[0].id')
curl -s -X DELETE -H "Authorization: Bearer $TOKEN" "localhost:8080/api/preferences/tags/$TAG_ID" -i
```

Expected: `204 No Content`. Subsequent `GET /preferences/tags` returns `[]` (or without the deleted row).

- [ ] **Step 5: Commit**

```bash
git add backend/internal/api/preference.go backend/cmd/server/main.go
git commit -m "feat(api): GET /preferences/tags + DELETE topic/tag endpoints"
```

---

### Task 15: Frontend — API client additions

**Files:**
- Modify: `frontend/src/api/client.ts`

- [ ] **Step 1: Add types and functions**

In `frontend/src/api/client.ts`, find the existing `InterestTopic` interface and add directly below:

```ts
export interface InterestTag {
  id: number
  tag: string
  weight: number
  last_reinforced_at: string
}

export interface PersistedInsight {
  id: number
  content: string
  triggered_by: 'auto' | 'manual'
  model?: string
  generated_at: string
}

export interface InsightsLatest {
  insight: PersistedInsight | null
  remaining_today: number
  remaining_month: number
}
```

- [ ] **Step 2: Replace the existing `generateInsights` block with the new functions**

Find:

```ts
export const generateInsights = () =>
  api.post<{ insights: string; message?: string }>('/insights/generate').then(res => res.data)
```

Replace with:

```ts
export const getLatestInsights = () =>
  api.get<InsightsLatest>('/insights/latest').then(res => res.data)

export const generateInsights = () =>
  api.post<{
    insights: string
    message?: string
    remaining_today: number
    remaining_month: number
  }>('/insights/generate').then(res => res.data)

export const getTags = () =>
  api.get<InterestTag[]>('/preferences/tags').then(res => res.data)

export const deleteTopic = (id: number) =>
  api.delete(`/preferences/topics/${id}`)

export const deleteTag = (id: number) =>
  api.delete(`/preferences/tags/${id}`)

export type InsightStreamHandlers = {
  onDelta?: (text: string) => void
  onDone?: (full: string, quota: { remaining_today: number; remaining_month: number }) => void
  onError?: (msg: string, quota?: { remaining_today: number; remaining_month: number }) => void
}

export async function generateInsightsStream(
  handlers: InsightStreamHandlers,
  signal?: AbortSignal,
): Promise<void> {
  const token = localStorage.getItem('token')
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
    Accept: 'application/x-ndjson',
  }
  if (token) headers['Authorization'] = `Bearer ${token}`

  let resp: Response
  try {
    resp = await fetch('/api/insights/generate?stream=1', {
      method: 'POST',
      credentials: 'include',
      headers,
      signal,
    })
  } catch (e: any) {
    if (e?.name !== 'AbortError') handlers.onError?.(e?.message || 'network error')
    return
  }

  if (!resp.ok || !resp.body) {
    const text = await resp.text().catch(() => '')
    handlers.onError?.(text || `HTTP ${resp.status}`)
    return
  }

  const reader = resp.body.getReader()
  const decoder = new TextDecoder()
  let buf = ''
  try {
    while (true) {
      const { value, done } = await reader.read()
      if (done) break
      buf += decoder.decode(value, { stream: true })
      let nl = buf.indexOf('\n')
      while (nl !== -1) {
        const line = buf.slice(0, nl).trim()
        buf = buf.slice(nl + 1)
        if (line) dispatchInsightFrame(line, handlers)
        nl = buf.indexOf('\n')
      }
    }
    if (buf.trim()) dispatchInsightFrame(buf.trim(), handlers)
  } catch (e: any) {
    if (e?.name === 'AbortError') return
    handlers.onError?.(e?.message || 'stream error')
  }
}

function dispatchInsightFrame(line: string, h: InsightStreamHandlers) {
  let frame: any
  try { frame = JSON.parse(line) } catch { return }
  switch (frame.type) {
    case 'delta':
      h.onDelta?.(frame.text || '')
      break
    case 'done':
      h.onDone?.(frame.full || '', {
        remaining_today: frame.remaining_today ?? 0,
        remaining_month: frame.remaining_month ?? 0,
      })
      break
    case 'error':
      h.onError?.(frame.msg || 'unknown error', {
        remaining_today: frame.remaining_today ?? 0,
        remaining_month: frame.remaining_month ?? 0,
      })
      break
  }
}
```

- [ ] **Step 3: Verify type-check**

Run: `cd frontend && npx tsc --noEmit`
Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/api/client.ts
git commit -m "feat(frontend): insights latest + stream + tag client APIs"
```

---

### Task 16: Frontend — InsightsPage state machine + dual cloud + streaming

**Files:**
- Modify: `frontend/src/pages/InsightsPage.tsx`

- [ ] **Step 1: Replace the page contents**

Overwrite `frontend/src/pages/InsightsPage.tsx` with:

```tsx
import { useState, useEffect, useRef } from 'react'
import { useNavigate } from 'react-router-dom'
import ReactMarkdown from 'react-markdown'
import {
  getTopics, getTags, getLatestInsights, generateInsightsStream,
  deleteTopic, deleteTag,
  InterestTopic, InterestTag, PersistedInsight,
} from '../api/client'

type Phase = 'loading' | 'empty' | 'has' | 'streaming'

export default function InsightsPage() {
  const navigate = useNavigate()
  const [phase, setPhase] = useState<Phase>('loading')
  const [topics, setTopics] = useState<InterestTopic[]>([])
  const [tags, setTags] = useState<InterestTag[]>([])
  const [insight, setInsight] = useState<PersistedInsight | null>(null)
  const [streamText, setStreamText] = useState('')
  const [remainingToday, setRemainingToday] = useState(3)
  const [remainingMonth, setRemainingMonth] = useState(100)
  const [errorMsg, setErrorMsg] = useState<string | null>(null)
  const abortRef = useRef<AbortController | null>(null)

  useEffect(() => {
    Promise.all([
      getTopics().then(d => d || []).catch(() => []),
      getTags().then(d => d || []).catch(() => []),
      getLatestInsights().catch(() => null),
    ]).then(([t, g, latest]) => {
      setTopics(t)
      setTags(g)
      if (latest) {
        setRemainingToday(latest.remaining_today)
        setRemainingMonth(latest.remaining_month)
        setInsight(latest.insight)
      }
      const empty = t.length === 0 && g.length === 0 && (!latest || !latest.insight)
      setPhase(empty ? 'empty' : 'has')
    })
    return () => abortRef.current?.abort()
  }, [])

  const handleGenerate = () => {
    if (remainingToday <= 0) return
    setPhase('streaming')
    setStreamText('')
    setErrorMsg(null)
    abortRef.current = new AbortController()
    generateInsightsStream({
      onDelta: t => setStreamText(prev => prev + t),
      onDone: (full, quota) => {
        setInsight({
          id: 0, // server reassigns; not used in UI
          content: full,
          triggered_by: 'manual',
          generated_at: new Date().toISOString(),
        })
        setStreamText('')
        setRemainingToday(quota.remaining_today)
        setRemainingMonth(quota.remaining_month)
        setPhase('has')
      },
      onError: (msg, quota) => {
        setErrorMsg(msg === 'quota_exceeded' ? '今日已达上限' :
                    msg === 'no_data' ? '暂无足够数据' : `生成失败：${msg}`)
        if (quota) {
          setRemainingToday(quota.remaining_today)
          setRemainingMonth(quota.remaining_month)
        }
        setStreamText('')
        setPhase(insight ? 'has' : 'empty')
      },
    }, abortRef.current.signal)
  }

  const handleDeleteTopic = async (id: number) => {
    setTopics(prev => prev.filter(t => t.id !== id))
    try { await deleteTopic(id) }
    catch { /* keep optimistic UI; refresh will reconcile */ }
  }
  const handleDeleteTag = async (id: number) => {
    setTags(prev => prev.filter(t => t.id !== id))
    try { await deleteTag(id) }
    catch { /* keep optimistic UI */ }
  }

  if (phase === 'loading') return <div className="card">Loading...</div>
  if (phase === 'empty') return <EmptyState onGo={() => navigate('/articles')} />

  const buttonLabel = phase === 'streaming' ? '分析中...' :
                      remainingToday <= 0 ? '今日已达上限' :
                      `重新生成 (今日 ${3 - remainingToday}/3)`
  const subtitle = insight ? formatSubtitle(insight) : ''

  return (
    <div>
      <h2 className="mb-2">兴趣洞察</h2>

      <Cloud
        title="兴趣主题"
        size="lg"
        empty="暂无主题，多阅读并标记后将出现"
        items={topics.map(t => ({ id: t.id, label: t.topic, weight: t.weight }))}
        onDelete={handleDeleteTopic}
      />

      <Cloud
        title="关键词"
        size="sm"
        empty="暂无关键词"
        items={tags.slice(0, 30).map(t => ({ id: t.id, label: t.tag, weight: t.weight }))}
        onDelete={handleDeleteTag}
      />

      <div className="card">
        <div className="flex-between mb-2">
          <h3>AI 个性化洞察</h3>
          <button
            onClick={handleGenerate}
            disabled={phase === 'streaming' || remainingToday <= 0}
            title={`今日剩 ${remainingToday} 次 · 本月剩 ${remainingMonth} 次`}
            style={{ fontSize: 13, padding: '4px 12px' }}
          >
            {buttonLabel}
          </button>
        </div>
        {subtitle && <div className="text-muted text-sm mb-1">{subtitle}</div>}
        {errorMsg && <div className="text-muted text-sm mb-1" style={{ color: '#c0392b' }}>{errorMsg}</div>}

        {phase === 'streaming' ? (
          <div className="markdown-body">
            <ReactMarkdown>{streamText || '正在分析…'}</ReactMarkdown>
          </div>
        ) : insight ? (
          <div className="markdown-body">
            <ReactMarkdown>{insight.content}</ReactMarkdown>
          </div>
        ) : (
          <div className="text-muted text-sm">点击右上角生成洞察</div>
        )}
      </div>

      <div className="card">
        <h3 className="mb-2">提升推荐质量</h3>
        <ul style={{ paddingLeft: 20, lineHeight: 2 }}>
          <li>标记喜欢的文章会提升相关主题的权重</li>
          <li>标记不喜欢的文章会降低相关主题的权重</li>
          <li>保存文章表示你对这个主题特别感兴趣</li>
          <li>阅读时长也会影响推荐算法</li>
        </ul>
      </div>
    </div>
  )
}

function formatSubtitle(ins: PersistedInsight): string {
  const ago = formatAgo(ins.generated_at)
  return ins.triggered_by === 'auto'
    ? `${ago} · 由系统自动生成`
    : `${ago} · 你触发的`
}

function formatAgo(iso: string): string {
  const t = new Date(iso).getTime()
  const dm = (Date.now() - t) / 60000
  if (dm < 1) return '刚刚'
  if (dm < 60) return `${Math.floor(dm)} 分钟前`
  const h = dm / 60
  if (h < 24) return `${Math.floor(h)} 小时前`
  return `${Math.floor(h / 24)} 天前`
}

function EmptyState({ onGo }: { onGo: () => void }) {
  return (
    <div className="card" style={{ textAlign: 'center', padding: 40 }}>
      <h3>💡 还没有足够数据生成洞察</h3>
      <p className="text-muted">洞察基于你对文章的反应生成。试着：</p>
      <ul style={{ display: 'inline-block', textAlign: 'left', lineHeight: 2 }}>
        <li>多阅读一会文章</li>
        <li>给文章点个 ❤️</li>
        <li>收藏感兴趣的文章</li>
      </ul>
      <div style={{ marginTop: 16 }}>
        <button onClick={onGo}>去阅读文章 →</button>
      </div>
    </div>
  )
}

function Cloud({ title, size, items, empty, onDelete }: {
  title: string
  size: 'lg' | 'sm'
  items: { id: number; label: string; weight: number }[]
  empty: string
  onDelete: (id: number) => void
}) {
  const [hover, setHover] = useState<number | null>(null)
  const baseSize = size === 'lg' ? 14 : 11
  const grow = size === 'lg' ? 2 : 1

  return (
    <div className="card">
      <h3 className="mb-2">{title}</h3>
      {items.length === 0 ? (
        <div className="text-muted">{empty}</div>
      ) : (
        <div className="flex gap-1" style={{ flexWrap: 'wrap' }}>
          {items.map(it => (
            <span
              key={it.id}
              onMouseEnter={() => setHover(it.id)}
              onMouseLeave={() => setHover(null)}
              style={{
                position: 'relative',
                padding: '4px 12px',
                paddingRight: hover === it.id ? 28 : 12,
                background: '#e8f0fe',
                borderRadius: 20,
                color: '#1a56db',
                fontSize: Math.min(baseSize + it.weight * grow, size === 'lg' ? 24 : 16),
                fontWeight: it.weight > 3 ? 600 : 400,
                transition: 'padding 0.1s',
              }}
            >
              {it.label}
              {hover === it.id && (
                <button
                  onClick={() => onDelete(it.id)}
                  style={{
                    position: 'absolute', right: 4, top: '50%', transform: 'translateY(-50%)',
                    width: 18, height: 18, borderRadius: '50%', border: 'none',
                    background: '#c0392b', color: 'white', cursor: 'pointer',
                    fontSize: 11, lineHeight: 1, padding: 0,
                  }}
                  aria-label="删除"
                >×</button>
              )}
            </span>
          ))}
        </div>
      )}
    </div>
  )
}
```

- [ ] **Step 2: Type-check**

Run: `cd frontend && npx tsc --noEmit`
Expected: no errors.

- [ ] **Step 3: Build & verify in browser**

```bash
docker-compose up -d --build frontend
```

Open `http://localhost/insights` (or whatever the dev URL is).

Verify, with a logged-in user:
1. Empty state when no signals: shows the 💡 card and "去阅读文章 →" button.
2. After liking ≥3 articles + waiting 1 minute: refresh; topic + tag clouds appear.
3. Click a topic chip — `×` appears on hover; clicking removes it from the UI.
4. Click "生成洞察": typewriter streaming visible, then content settles + button shows "重新生成 (今日 1/3)".
5. Click 3 times: button disables with "今日已达上限".

- [ ] **Step 4: Commit**

```bash
git add frontend/src/pages/InsightsPage.tsx
git commit -m "feat(frontend): InsightsPage with streaming, dual clouds, quota, empty state"
```

---

### Task 17: End-to-end verification + polish

**Files:** none (this is a verification task that may produce small fixes)

- [ ] **Step 1: Run all backend tests**

Run: `cd backend && go test ./... -count=1`
Expected: all PASS. (If a test in an unrelated package fails, that's pre-existing — note it but do not necessarily fix.)

- [ ] **Step 2: Manual E2E walkthrough**

With `docker-compose up -d --build` (full rebuild):

1. Login as a user with no prior signals: `/insights` shows empty state.
2. Open `/articles`, like 5 distinct articles.
3. Watch worker logs for ~1 minute: `docker-compose logs -f worker | grep classify`
   Expected: lines like `classify: 5 articles to classify; vocab=[...]` then `classify: article N → topic="..." tags=[...] users=1`.
4. Verify DB:
   ```bash
   docker-compose exec postgres psql -U postgres -d rsspal -c "SELECT id, topic, tags FROM articles WHERE topic IS NOT NULL ORDER BY id DESC LIMIT 5;"
   docker-compose exec postgres psql -U postgres -d rsspal -c "SELECT topic, weight FROM interest_topics WHERE user_id = <YOUR_ID> ORDER BY weight DESC;"
   docker-compose exec postgres psql -U postgres -d rsspal -c "SELECT tag, weight FROM interest_tags WHERE user_id = <YOUR_ID> ORDER BY weight DESC LIMIT 10;"
   ```
   Expected: articles classified, both interest tables populated.
5. Reload `/insights`: topic + keyword clouds visible. Click "生成洞察"; observe typewriter streaming. Final markdown renders. Subtitle shows "刚刚 · 你触发的".
6. Reload again: insight loads from DB (no AI call); button reads `重新生成 (今日 1/3)`.
7. Force the cron: in `backend/cmd/worker/insights.go` change `dailyHourCST` to the next hour and `--build worker`. Wait until firing time. Check `docker-compose logs worker | grep "daily cron"`. Expected: a log line per active user. Revert the change and rebuild.
8. Like the same article a 2nd time: server logs show no new AI call; `interest_topics.weight` for that topic increases.
9. Delete a tag chip — verify `DELETE` request fires and the chip is removed; refresh page → still gone.
10. Trigger manual generate 3 times. 4th time → button disabled, no request fires.
11. Hit the API directly while quota is exhausted:
    ```bash
    curl -i -X POST -H "Authorization: Bearer $TOKEN" 'localhost:8080/api/insights/generate'
    ```
    Expected: HTTP 429 with `{"error":"quota_exceeded",...}`.

- [ ] **Step 2.5: Update auto-memory**

Add a project memory entry recording that the insights feature is now wired end-to-end (so future sessions don't re-investigate the half-residual state):

Update `~/.claude/projects/-Users-bytedance-mygit-rss-pal/memory/project_rss_pal.md` (or whichever file currently tracks rss-pal status) — note this is **outside the repo**, not a git operation.

- [ ] **Step 3: Final commit (if any cleanup edits made)**

```bash
git status
# If clean, no commit needed.
# Otherwise:
git add <files>
git commit -m "chore(insights): end-to-end verification fixes"
```

- [ ] **Step 4: Open PR**

```bash
git push -u origin feature/insights-full
gh pr create --title "feat: full insights feature (topic+tag pipeline, daily cron, streaming, quota)" --body "$(cat <<'EOF'
## Summary
- Adds the topic + tag classification pipeline (worker scans, AI single-call JSON, cached on `articles.topic` / `articles.tags`).
- Adds daily 04:00 UTC+8 worker cron that decays both interest tables and generates AI insights for each active user.
- Adds persisted `user_insights` with NDJSON streaming endpoint, 3/day · 100/month manual quota.
- Frontend InsightsPage rewritten: dual tag clouds, optimistic chip delete, typewriter streaming, empty-state guidance.

## Test plan
- [ ] `go test ./...` passes
- [ ] `docker-compose up -d --build` boots cleanly
- [ ] Like ≥ 3 articles, worker logs show classification, DB shows populated `interest_topics` + `interest_tags`
- [ ] `/insights` shows clouds + last insight; manual regenerate streams + persists
- [ ] Quota: 3 manual regenerations disables the button; 4th API call returns 429
- [ ] Topic / tag deletion removes the chip and persists across reload

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

(Push + PR creation only after explicit user approval per `CLAUDE.md` defaults.)

---

## Self-Review

**Spec coverage check** — every spec section maps to at least one task:

| Spec section | Task(s) |
|---|---|
| §2 schema (articles cols, interest_tags, user_insights) | 1 |
| §3.1/3.2/3.3 worker scan + AI call + apply | 6, 9 |
| §3.4 weight mapping | 7 |
| §3.5 cache-hit branch in API | 8 |
| §3.6 idempotency | 9 (SetClassification always writes) |
| §4.1 daily cron | 10, 11 |
| §4.2 endpoints | 12, 13, 14 |
| §4.3 quota | 12 |
| §4.4 layered prompt | 11 (`buildLayeredPrompt`) |
| §4.5 prompt template | 11 / 13 |
| §5.1 state machine | 16 |
| §5.2 empty state | 16 |
| §5.3 dual cloud layout | 16 (`<Cloud>` component) |
| §5.4 client API | 15 |
| §5.5 chip delete | 16 |
| §5.6 quota button | 16 |
| §6 verification | 17 |

**Gap noted:** §4.4 layered L1/L2/L3 prompt assembly currently runs with empty L1/L2/L3 slices in Task 11 (cron uses topics+tags only). The streaming endpoint uses a simpler prompt (Task 13). Wiring up L1/L2/L3 fetchers (`getStrongest`, `getStrongSignals`, `getAllInteracted`) is a quality improvement that does NOT block end-to-end function — the cron and stream still produce valid insights. Implementer should consider this a follow-up task; the primitives in `buildLayeredPrompt` are already shaped correctly to receive them.

**Placeholder scan:** `TBD`/`TODO` not present. The single internal stub `deriveTagsForArticle(_ Article) []string { return nil }` in Task 11 is documented as a stub awaiting follow-up wiring (does not break the prompt; just yields empty tag annotations on L3 entries).

**Type consistency:** `Classification.Topic` / `.Tags` consistent across Tasks 2, 6, 9. `InterestTag` / `InterestTopic` field names match between model.go (Task 2), repos (Tasks 4, 5), API responses (Tasks 12, 14), and frontend types (Task 15). `triggered_by` is `'auto' | 'manual'` everywhere.

**Method-name consistency:** `SignalToTopicWeight`/`SignalToTagWeight`/`StrengthFromSignal` (Task 7) used identically in Tasks 8, 9. `DecayAllTopics`/`DecayAllTags` (Task 4) used in Task 10. `CallStream` exposed in Task 13 used by Task 13's handler.

**Risk noted in plan:** `cd backend && go test ./...` may show pre-existing unrelated failures; Task 17 Step 1 says to triage rather than blanket-fix.

---

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-05-07-insights-full-feature.md`. Two execution options:

**1. Subagent-Driven (recommended)** — dispatch a fresh subagent per task, review between tasks, fast iteration

**2. Inline Execution** — execute tasks in this session using executing-plans, batch execution with checkpoints

Which approach?
