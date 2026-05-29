# Daily Briefing Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a worker-generated daily briefing (`/daily`) mirroring the existing weekly digest, with AI selecting 5 of 20 candidates plus an 80–120 字 Chinese intro per user per day; migrate the weekly digest to the same worker-async model.

**Architecture:** Worker fires once per day at 05:00 Asia/Shanghai (and once per week at Mon 05:00) using the existing `nextDaily0400CST` cron pattern from `cmd/worker/insights.go`. AI returns a strict JSON `{picks, intro}` parsed by a pure function. GET endpoints are read-only with a one-day fallback when the requested date isn't ready yet. Frontend gets a single `📅 简报` nav entry that redirects to the user's last-viewed tab (`/daily` or `/weekly`), stored server-side in `users.briefing_last_tab`.

**Tech Stack:** Go 1.24 backend (gin + `database/sql` + `lib/pq`), React 18 + Vite frontend, PostgreSQL 15, Docker Compose. AI calls go through the existing `Summarizer.call(ctx, prompt, maxTokens)` wrapper. No new dependencies.

---

## File Plan

### Backend — create
- `backend/migrations/031_daily_briefing.sql` — `daily_digests` table + `users.briefing_last_tab` column.
- `backend/internal/ai/daily_digest.go` — `DailyCandidate` struct + `BuildDailyPrompt` + `ParseDailyDigestJSON` + `Summarizer.GenerateDailyDigest`.
- `backend/internal/ai/daily_digest_test.go` — parse/validate unit tests + httptest-backed `GenerateDailyDigest` test.
- `backend/internal/repository/daily_digest.go` — `DailyDigestRepository` (Get / Upsert / UserIDsMissing).
- `backend/internal/api/daily.go` — `DailyHandler.Get` + exported helpers (`TodayLabel`, `DailyDate`) for testing.
- `backend/internal/api/daily_test.go` — date math + branching tests.
- `backend/internal/api/briefing.go` — `BriefingHandler.GetLastTab` + `SetLastTab`.
- `backend/internal/api/briefing_test.go` — POST enum validation.
- `backend/cmd/worker/briefing.go` — `scheduleBriefingCron`, `fireDailyForAllUsers`, `fireWeeklyForAllUsers`, `runBriefingCatchUp`.
- `backend/cmd/worker/briefing_test.go` — `nextBriefingFire` time math tests.

### Backend — modify
- `backend/internal/repository/weekly_digest.go` — add `UserIDsMissing(weekStart)`.
- `backend/internal/repository/user.go` — add `GetBriefingLastTab` + `SetBriefingLastTab`.
- `backend/internal/api/weekly.go` — remove inline AI generation, add `pending` field, drop `summarizer` dep.
- `backend/cmd/server/main.go` — construct + register daily / briefing handlers, drop `summarizer` arg from weekly handler.
- `backend/cmd/worker/main.go` — start briefing cron alongside insight cron.

### Frontend — create
- `frontend/src/pages/DailyPage.tsx`
- `frontend/src/components/BriefingTabs.tsx`
- `frontend/src/components/BriefingRedirect.tsx`

### Frontend — modify
- `frontend/src/api/client.ts` — `DailyDigest` type, `getDailyDigest`, `getBriefingLastTab`, `setBriefingLastTab`, `WeeklyDigest.pending?`.
- `frontend/src/App.tsx` — add `/daily` and `/briefing` routes.
- `frontend/src/components/Layout.tsx` — swap `周刊` nav → `简报`.
- `frontend/src/components/MoreSheet.tsx` — remove `周刊` row.
- `frontend/src/pages/WeeklyPage.tsx` — render `pending` placeholder, embed `BriefingTabs`.

---

## Task 1 — Migration

**Files:**
- Create: `backend/migrations/031_daily_briefing.sql`

- [ ] **Step 1: Write the migration**

Content:

```sql
-- 031_daily_briefing.sql
-- Daily briefing cache (mirrors 007_bestblogs_features.sql weekly_digests).
CREATE TABLE IF NOT EXISTS daily_digests (
    id SERIAL PRIMARY KEY,
    user_id INT REFERENCES users(id) ON DELETE CASCADE,
    day_start DATE NOT NULL,
    intro_text TEXT NOT NULL,
    article_ids INTEGER[] NOT NULL,
    generated_at TIMESTAMP DEFAULT NOW(),
    UNIQUE(user_id, day_start)
);
CREATE INDEX IF NOT EXISTS idx_daily_digests_user_day
    ON daily_digests(user_id, day_start DESC);

ALTER TABLE users
    ADD COLUMN IF NOT EXISTS briefing_last_tab VARCHAR(10) DEFAULT 'daily';
```

- [ ] **Step 2: Verify the SQL parses**

Run: `docker compose exec -T postgres psql -U postgres -d rsspal -c "EXPLAIN $$ SELECT 1 $$" >/dev/null && echo postgres-ok`

Expected: `postgres-ok` (just confirms the container is up; we apply the migration manually in Task 17).

- [ ] **Step 3: Commit**

```bash
git add backend/migrations/031_daily_briefing.sql
git commit -m "feat(db): 031 daily_digests + users.briefing_last_tab"
```

---

## Task 2 — AI: prompt + JSON parser (pure functions, TDD)

**Files:**
- Create: `backend/internal/ai/daily_digest.go`
- Test: `backend/internal/ai/daily_digest_test.go`

- [ ] **Step 1: Write the failing test file**

```go
package ai

import (
	"strings"
	"testing"
)

func TestParseDailyDigestJSON_HappyPath(t *testing.T) {
	raw := `{"picks":[0,2,3,7,11],"intro":"` + strings.Repeat("某种主题", 25) + `"}`
	picks, intro, err := ParseDailyDigestJSON(raw, 20, 5)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(picks) != 5 {
		t.Fatalf("picks len = %d", len(picks))
	}
	wantOrder := []int{0, 2, 3, 7, 11}
	for i, p := range picks {
		if p != wantOrder[i] {
			t.Errorf("picks[%d] = %d want %d", i, p, wantOrder[i])
		}
	}
	if !strings.Contains(intro, "某种主题") {
		t.Errorf("intro lost: %q", intro)
	}
}

func TestParseDailyDigestJSON_FenceWrapped(t *testing.T) {
	body := `{"picks":[1,2,3,4,5],"intro":"` + strings.Repeat("主题", 50) + `"}`
	raw := "```json\n" + body + "\n```"
	picks, _, err := ParseDailyDigestJSON(raw, 20, 5)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(picks) != 5 {
		t.Errorf("picks len = %d", len(picks))
	}
}

func TestParseDailyDigestJSON_WrongPickCount(t *testing.T) {
	raw := `{"picks":[0,1,2],"intro":"` + strings.Repeat("文章", 50) + `"}`
	_, _, err := ParseDailyDigestJSON(raw, 20, 5)
	if err == nil {
		t.Fatal("expected err on wrong pick count")
	}
}

func TestParseDailyDigestJSON_DuplicateIndex(t *testing.T) {
	raw := `{"picks":[0,0,1,2,3],"intro":"` + strings.Repeat("文章", 50) + `"}`
	_, _, err := ParseDailyDigestJSON(raw, 20, 5)
	if err == nil {
		t.Fatal("expected err on duplicate")
	}
}

func TestParseDailyDigestJSON_IndexOutOfRange(t *testing.T) {
	raw := `{"picks":[0,1,2,3,99],"intro":"` + strings.Repeat("文章", 50) + `"}`
	_, _, err := ParseDailyDigestJSON(raw, 20, 5)
	if err == nil {
		t.Fatal("expected err on out-of-range")
	}
}

func TestParseDailyDigestJSON_IntroTooShort(t *testing.T) {
	raw := `{"picks":[0,1,2,3,4],"intro":"太短了"}`
	_, _, err := ParseDailyDigestJSON(raw, 20, 5)
	if err == nil {
		t.Fatal("expected err on short intro")
	}
}

func TestParseDailyDigestJSON_IntroTooLong(t *testing.T) {
	raw := `{"picks":[0,1,2,3,4],"intro":"` + strings.Repeat("字", 300) + `"}`
	_, _, err := ParseDailyDigestJSON(raw, 20, 5)
	if err == nil {
		t.Fatal("expected err on long intro")
	}
}

func TestParseDailyDigestJSON_Malformed(t *testing.T) {
	_, _, err := ParseDailyDigestJSON("not json", 20, 5)
	if err == nil {
		t.Fatal("expected err on garbage")
	}
}

func TestParseDailyDigestJSON_DynamicN(t *testing.T) {
	// Candidate pool of 3, asked for 3 picks. Must accept.
	raw := `{"picks":[0,1,2],"intro":"` + strings.Repeat("文", 100) + `"}`
	picks, _, err := ParseDailyDigestJSON(raw, 3, 3)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(picks) != 3 {
		t.Errorf("picks len = %d", len(picks))
	}
}

func TestBuildDailyPrompt_IncludesCandidatesAndN(t *testing.T) {
	cands := []DailyCandidate{
		{Idx: 0, Title: "A", SummaryBrief: "brief A"},
		{Idx: 1, Title: "B", SummaryBrief: "brief B"},
	}
	prompt := BuildDailyPrompt(cands, 2)
	if !strings.Contains(prompt, "[0] 《A》") || !strings.Contains(prompt, "[1] 《B》") {
		t.Errorf("prompt missing candidate lines: %q", prompt)
	}
	if !strings.Contains(prompt, "精选 2 篇") {
		t.Errorf("prompt missing N=2: %q", prompt)
	}
}
```

- [ ] **Step 2: Run tests (expect compile failures)**

Run: `cd backend && go test ./internal/ai/ -run DailyDigest -v 2>&1 | tail -20`

Expected: `undefined: ParseDailyDigestJSON` / `undefined: BuildDailyPrompt` / `undefined: DailyCandidate`.

- [ ] **Step 3: Write the implementation**

Create `backend/internal/ai/daily_digest.go`:

```go
package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

// DailyCandidate is the minimum payload BuildDailyPrompt needs.
type DailyCandidate struct {
	Idx          int
	Title        string
	SummaryBrief string
}

// BuildDailyPrompt produces the Chinese prompt asking the model to pick
// nPick of the given candidates and write an 80-120 字 intro.
func BuildDailyPrompt(candidates []DailyCandidate, nPick int) string {
	var b strings.Builder
	for _, c := range candidates {
		fmt.Fprintf(&b, "[%d] 《%s》\n    摘要：%s\n\n", c.Idx, c.Title, truncateContent(c.SummaryBrief))
	}
	return fmt.Sprintf(`以下是过去 24 小时按个性化推荐分数挑出的 %d 篇候选文章：

%s请从中精选 %d 篇组成「今日精选日报」,并写一段 80-120 字的中文导语,回答这个问题:
「为什么这 %d 篇值得今天读?这些文章共同指向什么趋势或思考?」

要求:
- 严格输出 JSON,不要 Markdown 代码块,不要任何包裹文字:
  {"picks":[i,j,k,l,m],"intro":"..."}
- picks 是 %d 个互不相同的 0-%d 整数下标,按推荐顺序排列。
- intro 80-120 字(中文字符数),从候选中提炼共同主题、张力或对比;不要逐篇复述;不要 Markdown、不要分点列表;语气专业、克制。`,
		len(candidates), b.String(), nPick, nPick, nPick, len(candidates)-1)
}

// ParseDailyDigestJSON parses the model output and validates picks + intro.
// nCandidates is the upper bound for pick indices (exclusive).
// nPick is the exact pick count expected.
func ParseDailyDigestJSON(raw string, nCandidates, nPick int) (picks []int, intro string, err error) {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)

	var payload struct {
		Picks []int  `json:"picks"`
		Intro string `json:"intro"`
	}
	if jerr := json.Unmarshal([]byte(raw), &payload); jerr != nil {
		return nil, "", fmt.Errorf("daily digest parse: %w", jerr)
	}

	if len(payload.Picks) != nPick {
		return nil, "", fmt.Errorf("daily digest: picks len %d, want %d", len(payload.Picks), nPick)
	}
	seen := make(map[int]bool, nPick)
	for _, p := range payload.Picks {
		if p < 0 || p >= nCandidates {
			return nil, "", fmt.Errorf("daily digest: pick %d out of [0,%d)", p, nCandidates)
		}
		if seen[p] {
			return nil, "", fmt.Errorf("daily digest: duplicate pick %d", p)
		}
		seen[p] = true
	}

	intro = strings.TrimSpace(payload.Intro)
	runes := utf8.RuneCountInString(intro)
	// Spec asks for 80-120 字; accept 60-250 to absorb minor drift.
	if runes < 60 || runes > 250 {
		return nil, "", fmt.Errorf("daily digest: intro %d runes, want 60-250", runes)
	}

	return payload.Picks, intro, nil
}

// GenerateDailyDigest asks the model to pick min(5, len(candidates)) and write
// the intro. Returns (nil, "", nil) when candidates is empty.
func (s *Summarizer) GenerateDailyDigest(ctx context.Context, candidates []DailyCandidate) (picks []int, intro string, err error) {
	if len(candidates) == 0 {
		return nil, "", nil
	}
	nPick := 5
	if len(candidates) < nPick {
		nPick = len(candidates)
	}
	prompt := BuildDailyPrompt(candidates, nPick)
	raw, callErr := s.call(ctx, prompt, 800)
	if callErr != nil {
		return nil, "", callErr
	}
	return ParseDailyDigestJSON(raw, len(candidates), nPick)
}
```

- [ ] **Step 4: Run tests — expect green**

Run: `cd backend && go test ./internal/ai/ -run DailyDigest -v 2>&1 | tail -25`

Expected: all `TestParseDailyDigestJSON_*` and `TestBuildDailyPrompt_*` PASS.

- [ ] **Step 5: Add `GenerateDailyDigest` integration test (httptest)**

Append to `backend/internal/ai/daily_digest_test.go`:

```go
func TestGenerateDailyDigest_HappyPath(t *testing.T) {
	body := `{"picks":[0,1,2,3,4],"intro":"` + strings.Repeat("主题", 50) + `"}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"choices":[{"message":{"content":%q}}]}`, body)
	}))
	defer srv.Close()

	s := NewSummarizerWithModel("k", srv.URL, "m")
	cands := make([]DailyCandidate, 20)
	for i := range cands {
		cands[i] = DailyCandidate{Idx: i, Title: fmt.Sprintf("T%d", i), SummaryBrief: "s"}
	}
	picks, intro, err := s.GenerateDailyDigest(context.Background(), cands)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(picks) != 5 {
		t.Errorf("picks len = %d", len(picks))
	}
	if intro == "" {
		t.Errorf("intro empty")
	}
}

func TestGenerateDailyDigest_EmptyReturnsNil(t *testing.T) {
	s := NewSummarizerWithModel("k", "http://localhost:1", "m") // never called
	picks, intro, err := s.GenerateDailyDigest(context.Background(), nil)
	if err != nil || picks != nil || intro != "" {
		t.Errorf("want (nil,\"\",nil); got (%v,%q,%v)", picks, intro, err)
	}
}
```

Add the imports at the top of the file if not already present: `"context"`, `"fmt"`, `"net/http"`, `"net/http/httptest"`.

- [ ] **Step 6: Run the new tests**

Run: `cd backend && go test ./internal/ai/ -run DailyDigest -v 2>&1 | tail -25`

Expected: all 10+ tests PASS.

- [ ] **Step 7: Commit**

```bash
git add backend/internal/ai/daily_digest.go backend/internal/ai/daily_digest_test.go
git commit -m "feat(ai): GenerateDailyDigest with strict JSON parser and validation"
```

---

## Task 3 — Repository: DailyDigestRepository + WeeklyDigestRepository.UserIDsMissing

**Files:**
- Create: `backend/internal/repository/daily_digest.go`
- Modify: `backend/internal/repository/weekly_digest.go`

- [ ] **Step 1: Write `daily_digest.go`**

```go
package repository

import (
	"database/sql"
	"errors"
	"time"

	"github.com/lib/pq"
)

type DailyDigest struct {
	UserID      int
	DayStart    time.Time
	IntroText   string
	ArticleIDs  []int64
	GeneratedAt time.Time
}

type DailyDigestRepository struct {
	db *sql.DB
}

func NewDailyDigestRepository(db *sql.DB) *DailyDigestRepository {
	return &DailyDigestRepository{db: db}
}

func (r *DailyDigestRepository) Get(userID int, dayStart time.Time) (*DailyDigest, error) {
	var d DailyDigest
	var ids pq.Int64Array
	err := r.db.QueryRow(`
		SELECT user_id, day_start, intro_text, article_ids, generated_at
		FROM daily_digests WHERE user_id = $1 AND day_start = $2
	`, userID, dayStart).Scan(&d.UserID, &d.DayStart, &d.IntroText, &ids, &d.GeneratedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	d.ArticleIDs = ids
	return &d, nil
}

func (r *DailyDigestRepository) Upsert(userID int, dayStart time.Time, intro string, articleIDs []int) error {
	ids := make(pq.Int64Array, len(articleIDs))
	for i, id := range articleIDs {
		ids[i] = int64(id)
	}
	_, err := r.db.Exec(`
		INSERT INTO daily_digests (user_id, day_start, intro_text, article_ids)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (user_id, day_start) DO UPDATE SET
			intro_text = EXCLUDED.intro_text,
			article_ids = EXCLUDED.article_ids,
			generated_at = NOW()
	`, userID, dayStart, intro, ids)
	return err
}

// UserIDsMissing returns user IDs that do not yet have a daily_digests row
// for `dayStart`. Used by the worker to pick up users it hasn't generated for.
func (r *DailyDigestRepository) UserIDsMissing(dayStart time.Time) ([]int, error) {
	rows, err := r.db.Query(`
		SELECT u.id FROM users u
		LEFT JOIN daily_digests d ON d.user_id = u.id AND d.day_start = $1
		WHERE d.id IS NULL
		ORDER BY u.id
	`, dayStart)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
```

- [ ] **Step 2: Add `UserIDsMissing` to weekly_digest.go**

Edit `backend/internal/repository/weekly_digest.go`, append after `Upsert`:

```go
// UserIDsMissing returns user IDs that do not yet have a weekly_digests row
// for `weekStart`. Mirrors DailyDigestRepository.UserIDsMissing.
func (r *WeeklyDigestRepository) UserIDsMissing(weekStart time.Time) ([]int, error) {
	rows, err := r.db.Query(`
		SELECT u.id FROM users u
		LEFT JOIN weekly_digests d ON d.user_id = u.id AND d.week_start = $1
		WHERE d.id IS NULL
		ORDER BY u.id
	`, weekStart)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
```

- [ ] **Step 3: Compile-check both files**

Run: `cd backend && go build ./internal/repository/...`

Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add backend/internal/repository/daily_digest.go backend/internal/repository/weekly_digest.go
git commit -m "feat(repo): DailyDigestRepository + UserIDsMissing on both digests"
```

---

## Task 4 — User repository: briefing tab getter/setter

**Files:**
- Modify: `backend/internal/repository/user.go`

- [ ] **Step 1: Add the two methods**

Edit `backend/internal/repository/user.go`. Append after `SetBookmarkletToken` (line 216):

```go
// GetBriefingLastTab returns the user's most recently viewed briefing tab,
// defaulting to "daily" if the column is somehow null.
func (r *UserRepository) GetBriefingLastTab(userID int) (string, error) {
	var tab sql.NullString
	err := r.db.QueryRow(`SELECT briefing_last_tab FROM users WHERE id = $1`, userID).Scan(&tab)
	if err == sql.ErrNoRows {
		return "daily", nil
	}
	if err != nil {
		return "", err
	}
	if !tab.Valid || tab.String == "" {
		return "daily", nil
	}
	return tab.String, nil
}

// SetBriefingLastTab persists the user's briefing tab choice.
// Caller must validate `tab` ∈ {"daily","weekly"} before calling.
func (r *UserRepository) SetBriefingLastTab(userID int, tab string) error {
	_, err := r.db.Exec(`UPDATE users SET briefing_last_tab = $1 WHERE id = $2`, tab, userID)
	return err
}
```

- [ ] **Step 2: Compile-check**

Run: `cd backend && go build ./internal/repository/...`

Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add backend/internal/repository/user.go
git commit -m "feat(repo): users.briefing_last_tab getter/setter"
```

---

## Task 5 — API: Daily handler with date helpers (TDD)

**Files:**
- Create: `backend/internal/api/daily.go`
- Test: `backend/internal/api/daily_test.go`

The hard logic to test is the date math (`TodayLabel`, `ParseDailyDate`). The full handler is a thin orchestration that exercises repos and is verified manually in Task 17.

- [ ] **Step 1: Write the failing tests**

Create `backend/internal/api/daily_test.go`:

```go
package api

import (
	"testing"
	"time"
)

func sh(year int, month time.Month, day, hour int) time.Time {
	return time.Date(year, month, day, hour, 0, 0, 0, briefingShanghai)
}

func TestTodayLabel_Before5amBelongsToYesterday(t *testing.T) {
	now := sh(2026, 5, 29, 3)
	got := TodayLabel(now)
	want := time.Date(2026, 5, 28, 0, 0, 0, 0, briefingShanghai)
	if !got.Equal(want) {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestTodayLabel_AtOrAfter5amBelongsToToday(t *testing.T) {
	now := sh(2026, 5, 29, 5)
	got := TodayLabel(now)
	want := time.Date(2026, 5, 29, 0, 0, 0, 0, briefingShanghai)
	if !got.Equal(want) {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestTodayLabel_AcrossUTCDayInsideShanghaiDay(t *testing.T) {
	// 2026-05-29 23:30 UTC == 2026-05-30 07:30 Shanghai → label 2026-05-30
	now := time.Date(2026, 5, 29, 23, 30, 0, 0, time.UTC)
	got := TodayLabel(now)
	want := time.Date(2026, 5, 30, 0, 0, 0, 0, briefingShanghai)
	if !got.Equal(want) {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestParseDailyDate_Valid(t *testing.T) {
	got, err := ParseDailyDate("2026-05-29")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	want := time.Date(2026, 5, 29, 0, 0, 0, 0, briefingShanghai)
	if !got.Equal(want) {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestParseDailyDate_Invalid(t *testing.T) {
	if _, err := ParseDailyDate("2026/05/29"); err == nil {
		t.Error("expected err on slash format")
	}
	if _, err := ParseDailyDate(""); err == nil {
		t.Error("expected err on empty")
	}
}

func TestDailyWindow_StartsAt5amEndsAt5amNextDay(t *testing.T) {
	day := time.Date(2026, 5, 28, 0, 0, 0, 0, briefingShanghai)
	start, end := DailyWindow(day)
	wantStart := time.Date(2026, 5, 28, 5, 0, 0, 0, briefingShanghai)
	wantEnd := time.Date(2026, 5, 29, 5, 0, 0, 0, briefingShanghai)
	if !start.Equal(wantStart) {
		t.Errorf("start = %s, want %s", start, wantStart)
	}
	if !end.Equal(wantEnd) {
		t.Errorf("end = %s, want %s", end, wantEnd)
	}
}
```

- [ ] **Step 2: Run tests (expect compile failures)**

Run: `cd backend && go test ./internal/api/ -run "TodayLabel|ParseDailyDate|DailyWindow" -v 2>&1 | tail -15`

Expected: `undefined: TodayLabel` / `undefined: briefingShanghai` etc.

- [ ] **Step 3: Write `daily.go`**

Create `backend/internal/api/daily.go`:

```go
package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/bytedance/rss-pal/internal/model"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/gin-gonic/gin"
)

// briefingShanghai is shared by daily and (eventually) weekly handlers.
// Fixed offset so we don't depend on tzdata in containers.
var briefingShanghai = time.FixedZone("Asia/Shanghai", 8*3600)

// briefingDayCutoffHour: window for "day D" is [D 05:00, D+1 05:00) Asia/Shanghai.
const briefingDayCutoffHour = 5

// briefingMaxLookbackDays: GET refuses requests further back than this.
const briefingMaxLookbackDays = 30

// TodayLabel returns the calendar date D in Asia/Shanghai such that
// `now` falls inside [D 05:00, D+1 05:00). Before 05:00 the label is yesterday.
func TodayLabel(now time.Time) time.Time {
	t := now.In(briefingShanghai)
	if t.Hour() < briefingDayCutoffHour {
		t = t.AddDate(0, 0, -1)
	}
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, briefingShanghai)
}

// ParseDailyDate parses YYYY-MM-DD in Asia/Shanghai and returns the date at 00:00.
func ParseDailyDate(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, errors.New("empty date")
	}
	return time.ParseInLocation("2006-01-02", s, briefingShanghai)
}

// DailyWindow returns the [start, end) bounds for the briefing day D.
// start = D 05:00, end = D+1 05:00 (both Asia/Shanghai).
func DailyWindow(day time.Time) (time.Time, time.Time) {
	d := day.In(briefingShanghai)
	start := time.Date(d.Year(), d.Month(), d.Day(), briefingDayCutoffHour, 0, 0, 0, briefingShanghai)
	end := start.AddDate(0, 0, 1)
	return start, end
}

type DailyHandler struct {
	articleRepo *repository.ArticleRepository
	digestRepo  *repository.DailyDigestRepository
}

func NewDailyHandler(articleRepo *repository.ArticleRepository, digestRepo *repository.DailyDigestRepository) *DailyHandler {
	return &DailyHandler{articleRepo: articleRepo, digestRepo: digestRepo}
}

// Get serves GET /api/daily-digest?date=YYYY-MM-DD.
func (h *DailyHandler) Get(c *gin.Context) {
	userID := getUserID(c)
	now := time.Now()
	today := TodayLabel(now)

	requested := today.AddDate(0, 0, -1)
	if q := c.Query("date"); q != "" {
		parsed, err := ParseDailyDate(q)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "date 必须是 YYYY-MM-DD 格式"})
			return
		}
		requested = parsed
	}

	if requested.After(today) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "date 不能晚于今天"})
		return
	}
	lookbackLimit := today.AddDate(0, 0, -briefingMaxLookbackDays)
	if requested.Before(lookbackLimit) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "date 超出回溯范围"})
		return
	}

	// Live branch: in-progress today
	if requested.Equal(today) {
		start, _ := DailyWindow(today)
		articles, err := h.articleRepo.GetTopArticlesInRange(userID, start, now, 5)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if articles == nil {
			articles = []model.Article{}
		}
		c.JSON(http.StatusOK, gin.H{
			"requested_date": requested.Format("2006-01-02"),
			"shown_date":     requested.Format("2006-01-02"),
			"pending":        false,
			"intro_text":     "",
			"articles":       articles,
			"mode":           "live",
		})
		return
	}

	// Cached branch: try requested, then fall back one day if missing.
	dd, err := h.digestRepo.Get(userID, requested)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if dd != nil {
		h.respondCached(c, userID, requested, requested, false, dd)
		return
	}
	fallback := requested.AddDate(0, 0, -1)
	if !fallback.Before(lookbackLimit) {
		fb, ferr := h.digestRepo.Get(userID, fallback)
		if ferr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": ferr.Error()})
			return
		}
		if fb != nil {
			h.respondCached(c, userID, requested, fallback, true, fb)
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"requested_date": requested.Format("2006-01-02"),
		"shown_date":     requested.Format("2006-01-02"),
		"pending":        true,
		"intro_text":     "",
		"articles":       []model.Article{},
		"mode":           "pending",
	})
}

func (h *DailyHandler) respondCached(c *gin.Context, userID int, requested, shown time.Time, pending bool, dd *repository.DailyDigest) {
	ids := make([]int, len(dd.ArticleIDs))
	for i, id := range dd.ArticleIDs {
		ids[i] = int(id)
	}
	articles, err := h.articleRepo.GetByIDsForUser(userID, ids)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if articles == nil {
		articles = []model.Article{}
	}
	c.JSON(http.StatusOK, gin.H{
		"requested_date": requested.Format("2006-01-02"),
		"shown_date":     shown.Format("2006-01-02"),
		"pending":        pending,
		"intro_text":     dd.IntroText,
		"articles":       articles,
		"mode":           "cached",
	})
}
```

- [ ] **Step 4: Run tests — expect green**

Run: `cd backend && go test ./internal/api/ -run "TodayLabel|ParseDailyDate|DailyWindow" -v 2>&1 | tail -20`

Expected: 6 PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/api/daily.go backend/internal/api/daily_test.go
git commit -m "feat(api): /daily-digest handler with read-only cache + fallback"
```

---

## Task 6 — API: Briefing tab handler

**Files:**
- Create: `backend/internal/api/briefing.go`
- Test: `backend/internal/api/briefing_test.go`

- [ ] **Step 1: Write the failing test**

Create `backend/internal/api/briefing_test.go`:

```go
package api

import "testing"

func TestValidateBriefingTab_Ok(t *testing.T) {
	for _, tab := range []string{"daily", "weekly"} {
		if !ValidateBriefingTab(tab) {
			t.Errorf("ValidateBriefingTab(%q) = false, want true", tab)
		}
	}
}

func TestValidateBriefingTab_Rejects(t *testing.T) {
	for _, tab := range []string{"", "DAILY", "monthly", "  daily "} {
		if ValidateBriefingTab(tab) {
			t.Errorf("ValidateBriefingTab(%q) = true, want false", tab)
		}
	}
}
```

- [ ] **Step 2: Run — expect compile failure**

Run: `cd backend && go test ./internal/api/ -run ValidateBriefingTab -v 2>&1 | tail -10`

Expected: `undefined: ValidateBriefingTab`.

- [ ] **Step 3: Write `briefing.go`**

Create `backend/internal/api/briefing.go`:

```go
package api

import (
	"net/http"

	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/gin-gonic/gin"
)

type BriefingHandler struct {
	userRepo *repository.UserRepository
}

func NewBriefingHandler(userRepo *repository.UserRepository) *BriefingHandler {
	return &BriefingHandler{userRepo: userRepo}
}

// ValidateBriefingTab returns true iff `tab` is a known enum value.
func ValidateBriefingTab(tab string) bool {
	return tab == "daily" || tab == "weekly"
}

// GetLastTab serves GET /api/briefing/last-tab.
func (h *BriefingHandler) GetLastTab(c *gin.Context) {
	userID := getUserID(c)
	tab, err := h.userRepo.GetBriefingLastTab(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if !ValidateBriefingTab(tab) {
		tab = "daily"
	}
	c.JSON(http.StatusOK, gin.H{"tab": tab})
}

// SetLastTab serves POST /api/briefing/last-tab.
func (h *BriefingHandler) SetLastTab(c *gin.Context) {
	var body struct {
		Tab string `json:"tab"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if !ValidateBriefingTab(body.Tab) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "tab 必须是 daily 或 weekly"})
		return
	}
	userID := getUserID(c)
	if err := h.userRepo.SetBriefingLastTab(userID, body.Tab); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"tab": body.Tab})
}
```

- [ ] **Step 4: Run tests — expect green**

Run: `cd backend && go test ./internal/api/ -run ValidateBriefingTab -v 2>&1 | tail -10`

Expected: 2 PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/api/briefing.go backend/internal/api/briefing_test.go
git commit -m "feat(api): briefing last-tab GET/POST endpoints"
```

---

## Task 7 — Migrate weekly handler to read-only

**Files:**
- Modify: `backend/internal/api/weekly.go`

- [ ] **Step 1: Rewrite `weekly.go`**

Replace the whole file with:

```go
package api

import (
	"net/http"
	"time"

	"github.com/bytedance/rss-pal/internal/model"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/gin-gonic/gin"
)

type WeeklyHandler struct {
	articleRepo *repository.ArticleRepository
	digestRepo  *repository.WeeklyDigestRepository
}

// NewWeeklyHandler constructs a read-only weekly digest handler. The worker
// is the sole writer of weekly_digests; the API never invokes the summarizer.
func NewWeeklyHandler(articleRepo *repository.ArticleRepository, digestRepo *repository.WeeklyDigestRepository) *WeeklyHandler {
	return &WeeklyHandler{articleRepo: articleRepo, digestRepo: digestRepo}
}

var shanghai = time.FixedZone("Asia/Shanghai", 8*3600)

func startOfWeek(t time.Time) time.Time {
	t = t.In(shanghai)
	weekday := int(t.Weekday())
	if weekday == 0 {
		weekday = 7
	}
	monday := t.AddDate(0, 0, -(weekday - 1))
	return time.Date(monday.Year(), monday.Month(), monday.Day(), 0, 0, 0, 0, shanghai)
}

func (h *WeeklyHandler) Get(c *gin.Context) {
	userID := getUserID(c)

	weekStart := startOfWeek(time.Now())
	if w := c.Query("week"); w != "" {
		parsed, err := time.ParseInLocation("2006-01-02", w, shanghai)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "week 必须是 YYYY-MM-DD 格式"})
			return
		}
		weekStart = startOfWeek(parsed)
	}

	cached, err := h.digestRepo.Get(userID, weekStart)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if cached == nil {
		c.JSON(http.StatusOK, gin.H{
			"week_start": weekStart.Format("2006-01-02"),
			"intro_text": "",
			"articles":   []model.Article{},
			"pending":    true,
		})
		return
	}

	ids := make([]int, len(cached.ArticleIDs))
	for i, id := range cached.ArticleIDs {
		ids[i] = int(id)
	}
	articles, err := h.articleRepo.GetByIDsForUser(userID, ids)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if articles == nil {
		articles = []model.Article{}
	}
	c.JSON(http.StatusOK, gin.H{
		"week_start": weekStart.Format("2006-01-02"),
		"intro_text": cached.IntroText,
		"articles":   articles,
		"pending":    false,
	})
}
```

- [ ] **Step 2: Build check**

Run: `cd backend && go build ./...`

Expected: failure in `cmd/server/main.go` because `NewWeeklyHandler` signature changed. That's wired up in Task 8.

- [ ] **Step 3: Commit (will not yet compile end-to-end — that's fine, Task 8 immediately follows)**

```bash
git add backend/internal/api/weekly.go
git commit -m "refactor(api): weekly handler read-only (worker generates async)"
```

---

## Task 8 — Server wiring

**Files:**
- Modify: `backend/cmd/server/main.go`

- [ ] **Step 1: Add the new constructions**

Edit `backend/cmd/server/main.go`.

Find (around line 36):

```go
	weeklyDigestRepo := repository.NewWeeklyDigestRepository(db)
```

Add immediately after:

```go
	dailyDigestRepo := repository.NewDailyDigestRepository(db)
```

Find (around line 66):

```go
	weeklyHandler := api.NewWeeklyHandler(articleRepo, weeklyDigestRepo, summarizer)
```

Replace with:

```go
	weeklyHandler := api.NewWeeklyHandler(articleRepo, weeklyDigestRepo)
	dailyHandler := api.NewDailyHandler(articleRepo, dailyDigestRepo)
	briefingHandler := api.NewBriefingHandler(userRepo)
```

Find (around line 233):

```go
		// Weekly digest
		apiGroup.GET("/weekly-digest", weeklyHandler.Get)
```

Replace with:

```go
		// Weekly / daily briefings (worker generates async; API is read-only)
		apiGroup.GET("/weekly-digest", weeklyHandler.Get)
		apiGroup.GET("/daily-digest", dailyHandler.Get)
		apiGroup.GET("/briefing/last-tab", briefingHandler.GetLastTab)
		apiGroup.POST("/briefing/last-tab", briefingHandler.SetLastTab)
```

- [ ] **Step 2: Build the server**

Run: `cd backend && go build ./cmd/server`

Expected: no errors.

- [ ] **Step 3: Run all backend tests**

Run: `cd backend && go test ./...`

Expected: all PASS. If any existing weekly test references the old `summarizer` arg, update it now.

- [ ] **Step 4: Commit**

```bash
git add backend/cmd/server/main.go
git commit -m "feat(server): wire daily + briefing handlers, drop summarizer from weekly"
```

---

## Task 9 — Worker: briefing cron

**Files:**
- Create: `backend/cmd/worker/briefing.go`
- Test: `backend/cmd/worker/briefing_test.go`

- [ ] **Step 1: Write the failing tests**

Create `backend/cmd/worker/briefing_test.go`:

```go
package main

import (
	"testing"
	"time"
)

func sh(year int, month time.Month, day, hour, min int) time.Time {
	loc := time.FixedZone("Asia/Shanghai", 8*3600)
	return time.Date(year, month, day, hour, min, 0, 0, loc)
}

func TestNextBriefingFire_BeforeFive(t *testing.T) {
	now := sh(2026, 5, 29, 3, 0)
	got := nextBriefingFire(now)
	want := sh(2026, 5, 29, 5, 0)
	if !got.Equal(want) {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestNextBriefingFire_AtFive(t *testing.T) {
	// Already past today's 05:00 — next is tomorrow 05:00.
	now := sh(2026, 5, 29, 5, 0)
	got := nextBriefingFire(now)
	want := sh(2026, 5, 30, 5, 0)
	if !got.Equal(want) {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestNextBriefingFire_AfterFive(t *testing.T) {
	now := sh(2026, 5, 29, 14, 30)
	got := nextBriefingFire(now)
	want := sh(2026, 5, 30, 5, 0)
	if !got.Equal(want) {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestIsMondayInShanghai(t *testing.T) {
	mon := sh(2026, 5, 25, 5, 0) // 2026-05-25 was a Monday
	if !isMondayShanghai(mon) {
		t.Errorf("expected Mon for %s", mon)
	}
	tue := sh(2026, 5, 26, 5, 0)
	if isMondayShanghai(tue) {
		t.Errorf("expected !Mon for %s", tue)
	}
}
```

- [ ] **Step 2: Run — expect compile failures**

Run: `cd backend && go test ./cmd/worker/ -run "NextBriefingFire|MondayInShanghai" -v 2>&1 | tail -10`

Expected: `undefined: nextBriefingFire` / `undefined: isMondayShanghai`.

- [ ] **Step 3: Write `briefing.go`**

Create `backend/cmd/worker/briefing.go`:

```go
package main

import (
	"context"
	"log"
	"os"
	"sync"
	"time"

	"github.com/bytedance/rss-pal/internal/ai"
	"github.com/bytedance/rss-pal/internal/api"
	"github.com/bytedance/rss-pal/internal/repository"
)

const briefingHourCST = 5

var briefingShanghai = time.FixedZone("Asia/Shanghai", 8*3600)

// nextBriefingFire returns the next 05:00 Asia/Shanghai strictly after `now`.
func nextBriefingFire(now time.Time) time.Time {
	n := now.In(briefingShanghai)
	target := time.Date(n.Year(), n.Month(), n.Day(), briefingHourCST, 0, 0, 0, briefingShanghai)
	if !target.After(n) {
		target = target.AddDate(0, 0, 1)
	}
	return target
}

func isMondayShanghai(t time.Time) bool {
	return t.In(briefingShanghai).Weekday() == time.Monday
}

type briefingDeps struct {
	articleRepo *repository.ArticleRepository
	dailyRepo   *repository.DailyDigestRepository
	weeklyRepo  *repository.WeeklyDigestRepository
	summarizer  *ai.Summarizer
}

// scheduleBriefingCron fires fireDailyForAllUsers every day at 05:00 Asia/Shanghai,
// and fireWeeklyForAllUsers additionally on Mondays. Mirrors scheduleDailyInsightCron.
// Dev hook: BRIEFING_RUN_NOW=1 fires both jobs immediately on startup.
func scheduleBriefingCron(deps briefingDeps) context.CancelFunc {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		runBriefingCatchUp(ctx, deps)
		if os.Getenv("BRIEFING_RUN_NOW") == "1" {
			log.Printf("briefing cron: BRIEFING_RUN_NOW=1 → firing immediately")
			fireBriefings(ctx, deps, time.Now())
		}
		for {
			next := nextBriefingFire(time.Now())
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Until(next)):
				log.Printf("briefing cron: firing at %s", time.Now().Format(time.RFC3339))
				fireBriefings(ctx, deps, time.Now())
			}
		}
	}()
	return cancel
}

// fireBriefings runs the daily job, and if `now` is Monday Asia/Shanghai, the weekly job too.
func fireBriefings(ctx context.Context, deps briefingDeps, now time.Time) {
	today := api.TodayLabel(now)
	yesterday := today.AddDate(0, 0, -1)
	fireDailyForAllUsers(ctx, deps, yesterday)
	if isMondayShanghai(now) {
		// "Last week" = the Monday 7 days before today's Monday (which is today).
		weekStart := mondayShanghai(now).AddDate(0, 0, -7)
		fireWeeklyForAllUsers(ctx, deps, weekStart)
	}
}

// mondayShanghai returns the Monday at 00:00 in Asia/Shanghai of the week containing `t`.
func mondayShanghai(t time.Time) time.Time {
	tt := t.In(briefingShanghai)
	weekday := int(tt.Weekday())
	if weekday == 0 {
		weekday = 7
	}
	mon := tt.AddDate(0, 0, -(weekday - 1))
	return time.Date(mon.Year(), mon.Month(), mon.Day(), 0, 0, 0, 0, briefingShanghai)
}

// fireDailyForAllUsers picks up the users missing a daily for `day` and generates one each.
// AI errors / empty candidate pools result in no row written.
func fireDailyForAllUsers(ctx context.Context, deps briefingDeps, day time.Time) {
	if deps.summarizer == nil {
		log.Printf("briefing: daily skipped, no summarizer (CLAUDE_API_KEY?)")
		return
	}
	ids, err := deps.dailyRepo.UserIDsMissing(day)
	if err != nil {
		log.Printf("briefing.daily: UserIDsMissing(%s): %v", day.Format("2006-01-02"), err)
		return
	}
	if len(ids) == 0 {
		return
	}
	log.Printf("briefing.daily: %d users to generate for %s", len(ids), day.Format("2006-01-02"))
	start, end := api.DailyWindow(day)
	var wg sync.WaitGroup
	for _, uid := range ids {
		wg.Add(1)
		go func(userID int) {
			defer wg.Done()
			sumSem <- struct{}{}
			defer func() { <-sumSem }()
			generateOneDaily(ctx, deps, userID, day, start, end)
		}(uid)
	}
	wg.Wait()
}

func generateOneDaily(ctx context.Context, deps briefingDeps, userID int, day, start, end time.Time) {
	arts, err := deps.articleRepo.GetTopArticlesInRange(userID, start, end, 20)
	if err != nil {
		log.Printf("briefing.daily user=%d: GetTopArticlesInRange: %v", userID, err)
		return
	}
	if len(arts) == 0 {
		log.Printf("briefing.daily user=%d day=%s: no candidates, skip", userID, day.Format("2006-01-02"))
		return
	}
	cands := make([]ai.DailyCandidate, len(arts))
	for i, a := range arts {
		cands[i] = ai.DailyCandidate{Idx: i, Title: a.Title, SummaryBrief: a.SummaryBrief}
	}
	cctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	picks, intro, err := deps.summarizer.GenerateDailyDigest(cctx, cands)
	if err != nil {
		log.Printf("briefing.daily user=%d day=%s: ai parse: %v", userID, day.Format("2006-01-02"), err)
		return
	}
	if len(picks) == 0 {
		return
	}
	ids := make([]int, len(picks))
	for i, p := range picks {
		ids[i] = arts[p].ID
	}
	if err := deps.dailyRepo.Upsert(userID, day, intro, ids); err != nil {
		log.Printf("briefing.daily user=%d day=%s: upsert: %v", userID, day.Format("2006-01-02"), err)
		return
	}
	log.Printf("briefing.daily user=%d day=%s: ok (%d picks)", userID, day.Format("2006-01-02"), len(ids))
}

// fireWeeklyForAllUsers picks up users missing a weekly for `weekStart` and generates one each.
func fireWeeklyForAllUsers(ctx context.Context, deps briefingDeps, weekStart time.Time) {
	if deps.summarizer == nil {
		log.Printf("briefing: weekly skipped, no summarizer")
		return
	}
	ids, err := deps.weeklyRepo.UserIDsMissing(weekStart)
	if err != nil {
		log.Printf("briefing.weekly: UserIDsMissing(%s): %v", weekStart.Format("2006-01-02"), err)
		return
	}
	if len(ids) == 0 {
		return
	}
	log.Printf("briefing.weekly: %d users to generate for week of %s", len(ids), weekStart.Format("2006-01-02"))
	end := weekStart.AddDate(0, 0, 7)
	var wg sync.WaitGroup
	for _, uid := range ids {
		wg.Add(1)
		go func(userID int) {
			defer wg.Done()
			sumSem <- struct{}{}
			defer func() { <-sumSem }()
			generateOneWeekly(ctx, deps, userID, weekStart, end)
		}(uid)
	}
	wg.Wait()
}

func generateOneWeekly(ctx context.Context, deps briefingDeps, userID int, weekStart, end time.Time) {
	arts, err := deps.articleRepo.GetTopArticlesInRange(userID, weekStart, end, 10)
	if err != nil {
		log.Printf("briefing.weekly user=%d: GetTopArticlesInRange: %v", userID, err)
		return
	}
	if len(arts) == 0 {
		log.Printf("briefing.weekly user=%d week=%s: no candidates, skip", userID, weekStart.Format("2006-01-02"))
		return
	}
	items := make([]ai.WeeklyDigestItem, len(arts))
	for i, a := range arts {
		items[i] = ai.WeeklyDigestItem{Title: a.Title, SummaryBrief: a.SummaryBrief}
	}
	cctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	intro, err := deps.summarizer.GenerateWeeklyIntro(cctx, items)
	if err != nil || intro == "" {
		if err != nil {
			log.Printf("briefing.weekly user=%d week=%s: ai: %v", userID, weekStart.Format("2006-01-02"), err)
		}
		return
	}
	ids := make([]int, len(arts))
	for i, a := range arts {
		ids[i] = a.ID
	}
	if err := deps.weeklyRepo.Upsert(userID, weekStart, intro, ids); err != nil {
		log.Printf("briefing.weekly user=%d week=%s: upsert: %v", userID, weekStart.Format("2006-01-02"), err)
		return
	}
	log.Printf("briefing.weekly user=%d week=%s: ok", userID, weekStart.Format("2006-01-02"))
}

// runBriefingCatchUp generates any missing dailies for the last 3 completed days
// and the last completed weekly. Called once at worker startup.
func runBriefingCatchUp(ctx context.Context, deps briefingDeps) {
	now := time.Now()
	today := api.TodayLabel(now)
	for k := 1; k <= 3; k++ {
		fireDailyForAllUsers(ctx, deps, today.AddDate(0, 0, -k))
	}
	fireWeeklyForAllUsers(ctx, deps, mondayShanghai(now).AddDate(0, 0, -7))
}
```

- [ ] **Step 4: Run tests — expect green**

Run: `cd backend && go test ./cmd/worker/ -run "NextBriefingFire|MondayInShanghai" -v 2>&1 | tail -15`

Expected: 4 PASS.

- [ ] **Step 5: Build check**

Run: `cd backend && go build ./cmd/worker`

Expected: no errors.

- [ ] **Step 6: Commit**

```bash
git add backend/cmd/worker/briefing.go backend/cmd/worker/briefing_test.go
git commit -m "feat(worker): briefing cron with 05:00 daily + Mon weekly + catch-up"
```

---

## Task 10 — Worker wiring

**Files:**
- Modify: `backend/cmd/worker/main.go`

- [ ] **Step 1: Add construction + cron start**

Edit `backend/cmd/worker/main.go`.

Find (around line 52):

```go
	userInsightsRepo := repository.NewUserInsightRepository(db)
```

Add immediately after:

```go
	dailyDigestRepo := repository.NewDailyDigestRepository(db)
	weeklyDigestRepo := repository.NewWeeklyDigestRepository(db)
```

Find the block that starts cron (around lines 73-84):

```go
	if summarizer != nil {
		stopCron := scheduleDailyInsightCron(insightCronDeps{
			…
		})
		defer stopCron()
	}
```

Add immediately after the closing `}`:

```go
	if summarizer != nil {
		stopBriefing := scheduleBriefingCron(briefingDeps{
			articleRepo: articleRepo,
			dailyRepo:   dailyDigestRepo,
			weeklyRepo:  weeklyDigestRepo,
			summarizer:  summarizer,
		})
		defer stopBriefing()
	}
```

- [ ] **Step 2: Build the worker**

Run: `cd backend && go build ./cmd/worker`

Expected: no errors.

- [ ] **Step 3: Run all backend tests**

Run: `cd backend && go test ./...`

Expected: all PASS.

- [ ] **Step 4: Commit**

```bash
git add backend/cmd/worker/main.go
git commit -m "feat(worker): start briefing cron alongside daily insights"
```

---

## Task 11 — Frontend API client

**Files:**
- Modify: `frontend/src/api/client.ts`

- [ ] **Step 1: Add types and functions**

Find the existing `WeeklyDigest` interface and replace it with:

```ts
export interface WeeklyDigest {
  week_start: string
  intro_text: string
  articles: Article[]
  pending?: boolean
}
```

Append after `export const getWeeklyDigest = …`:

```ts
export interface DailyDigest {
  requested_date: string
  shown_date: string
  pending: boolean
  intro_text: string
  articles: Article[]
  mode: 'cached' | 'live' | 'pending'
}

export const getDailyDigest = (date?: string) =>
  api.get<DailyDigest>('/daily-digest', { params: date ? { date } : {} }).then(r => r.data)

export type BriefingTab = 'daily' | 'weekly'

export const getBriefingLastTab = () =>
  api.get<{ tab: BriefingTab }>('/briefing/last-tab').then(r => r.data)

export const setBriefingLastTab = (tab: BriefingTab) =>
  api.post('/briefing/last-tab', { tab })
```

- [ ] **Step 2: Type-check**

Run: `cd frontend && npx tsc --noEmit`

Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/api/client.ts
git commit -m "feat(client): DailyDigest type, briefing tab API, pending on WeeklyDigest"
```

---

## Task 12 — Frontend: BriefingTabs component

**Files:**
- Create: `frontend/src/components/BriefingTabs.tsx`

- [ ] **Step 1: Write the component**

```tsx
import { Link } from 'react-router-dom'
import { setBriefingLastTab, BriefingTab } from '../api/client'

interface Props {
  current: BriefingTab
}

export default function BriefingTabs({ current }: Props) {
  const onClick = (tab: BriefingTab) => {
    setBriefingLastTab(tab).catch(() => { /* best-effort */ })
  }
  const baseStyle: React.CSSProperties = {
    padding: '8px 16px',
    borderRadius: 8,
    textDecoration: 'none',
    fontWeight: 600,
    fontSize: 14,
  }
  const activeStyle: React.CSSProperties = {
    ...baseStyle,
    background: 'var(--accent, #2563eb)',
    color: '#fff',
  }
  const inactiveStyle: React.CSSProperties = {
    ...baseStyle,
    background: 'transparent',
    color: 'var(--fg)',
    border: '1px solid var(--border)',
  }
  return (
    <div role="tablist" style={{ display: 'flex', gap: 8, marginBottom: 16 }}>
      <Link
        to="/daily"
        role="tab"
        aria-selected={current === 'daily'}
        onClick={() => onClick('daily')}
        style={current === 'daily' ? activeStyle : inactiveStyle}
      >
        日报
      </Link>
      <Link
        to="/weekly"
        role="tab"
        aria-selected={current === 'weekly'}
        onClick={() => onClick('weekly')}
        style={current === 'weekly' ? activeStyle : inactiveStyle}
      >
        周报
      </Link>
    </div>
  )
}
```

- [ ] **Step 2: Type-check**

Run: `cd frontend && npx tsc --noEmit`

Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/BriefingTabs.tsx
git commit -m "feat(ui): BriefingTabs shared daily/weekly tab strip"
```

---

## Task 13 — Frontend: BriefingRedirect component

**Files:**
- Create: `frontend/src/components/BriefingRedirect.tsx`

- [ ] **Step 1: Write the component**

```tsx
import { useEffect } from 'react'
import { useNavigate } from 'react-router-dom'
import { getBriefingLastTab } from '../api/client'

export default function BriefingRedirect() {
  const navigate = useNavigate()
  useEffect(() => {
    let cancelled = false
    getBriefingLastTab()
      .then(({ tab }) => {
        if (cancelled) return
        navigate('/' + tab, { replace: true })
      })
      .catch(() => {
        if (cancelled) return
        navigate('/daily', { replace: true })
      })
    return () => { cancelled = true }
  }, [navigate])
  return (
    <div className="card">加载中…</div>
  )
}
```

- [ ] **Step 2: Type-check**

Run: `cd frontend && npx tsc --noEmit`

Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/BriefingRedirect.tsx
git commit -m "feat(ui): BriefingRedirect lands on user's last-viewed tab"
```

---

## Task 14 — Frontend: DailyPage

**Files:**
- Create: `frontend/src/pages/DailyPage.tsx`

- [ ] **Step 1: Write the page**

```tsx
import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { getDailyDigest, DailyDigest } from '../api/client'
import ReadingMeta from '../components/ReadingMeta'
import BriefingTabs from '../components/BriefingTabs'
import { toast } from '../utils/toast'

function shiftDay(date: string, days: number): string {
  const d = new Date(date + 'T00:00:00+08:00')
  d.setDate(d.getDate() + days)
  const shanghai = new Date(d.getTime() + 8 * 3600 * 1000)
  return shanghai.toISOString().slice(0, 10)
}

export default function DailyPage() {
  const [digest, setDigest] = useState<DailyDigest | null>(null)
  const [loading, setLoading] = useState(true)
  const [date, setDate] = useState<string | undefined>(undefined)

  useEffect(() => { load(date) }, [date])

  const load = async (d?: string) => {
    setLoading(true)
    try {
      const data = await getDailyDigest(d)
      setDigest(data)
    } catch (err: any) {
      toast.error(err?.response?.data?.error || '加载日报失败')
    } finally {
      setLoading(false)
    }
  }

  if (loading) return (
    <div>
      <BriefingTabs current="daily" />
      <div className="card">加载中…</div>
    </div>
  )
  if (!digest) return (
    <div>
      <BriefingTabs current="daily" />
      <div className="card">暂无数据</div>
    </div>
  )

  const headerTitle = digest.mode === 'live' ? `今日精选 · ${digest.shown_date}（收集中）` : `本日精选 · ${digest.shown_date}`
  const showStaleTag = digest.pending && digest.shown_date !== digest.requested_date

  return (
    <div>
      <BriefingTabs current="daily" />
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 16 }}>
        <h2 style={{ margin: 0 }}>{headerTitle}</h2>
        <div style={{ display: 'flex', gap: 8 }}>
          <button className="secondary" title="前一天" onClick={() => setDate(shiftDay(digest.shown_date, -1))}>‹ 前一天</button>
          <button className="secondary" title="后一天" onClick={() => setDate(shiftDay(digest.shown_date, 1))}>后一天 ›</button>
          {date !== undefined && (
            <button className="secondary" title="回到昨天" onClick={() => setDate(undefined)}>昨天</button>
          )}
        </div>
      </div>

      {showStaleTag && (
        <div className="card text-muted" style={{ marginBottom: 12, fontSize: 13 }}>
          {digest.requested_date} 的日报还在生成中,先看 {digest.shown_date} 的。
        </div>
      )}

      {digest.mode === 'live' && (
        <div className="card text-muted" style={{ marginBottom: 12, fontSize: 13 }}>
          今日还在收集中,明早 5 点后生成正式日报(含 AI 导语)。
        </div>
      )}

      {digest.pending && digest.articles.length === 0 ? (
        <div className="card">日报生成中,稍后刷新…</div>
      ) : (
        <>
          {digest.intro_text ? (
            <div className="card" style={{ marginBottom: 16, lineHeight: 1.7 }}>
              {digest.intro_text}
            </div>
          ) : digest.mode === 'cached' ? (
            <div className="card text-muted" style={{ marginBottom: 16, fontSize: 13 }}>
              本日导语生成失败或暂未生成,以下是入选文章:
            </div>
          ) : null}

          {digest.articles.length === 0 ? (
            <div className="card text-muted">当日无候选文章。</div>
          ) : (
            <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
              {digest.articles.map(a => (
                <Link key={a.id} to={`/articles/${a.id}`} className="card" style={{ display: 'block', textDecoration: 'none', color: 'inherit' }}>
                  <div style={{ fontSize: 15, fontWeight: 600, marginBottom: 4 }}>{a.title}</div>
                  <div style={{ display: 'flex', gap: 12, alignItems: 'center', flexWrap: 'wrap', marginBottom: 6 }}>
                    {a.feed_title && <span className="text-muted text-sm">{a.feed_title}</span>}
                    {a.published_at && <span className="text-muted text-sm">{new Date(a.published_at).toLocaleDateString('zh-CN')}</span>}
                    <ReadingMeta wordCount={a.word_count} readingMinutes={a.reading_minutes} />
                  </div>
                  {a.summary_brief && <div className="text-muted" style={{ fontSize: 13, lineHeight: 1.5 }}>{a.summary_brief}</div>}
                </Link>
              ))}
            </div>
          )}
        </>
      )}
    </div>
  )
}
```

- [ ] **Step 2: Type-check**

Run: `cd frontend && npx tsc --noEmit`

Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/pages/DailyPage.tsx
git commit -m "feat(ui): DailyPage with live mode, pending fallback, day pagination"
```

---

## Task 15 — Frontend: WeeklyPage pending + tabs

**Files:**
- Modify: `frontend/src/pages/WeeklyPage.tsx`

- [ ] **Step 1: Read the current file**

(Engineer reads `frontend/src/pages/WeeklyPage.tsx` to confirm shape — current contents shown in spec Task summary.)

- [ ] **Step 2: Rewrite to add tabs + pending**

Replace the file contents with:

```tsx
import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { getWeeklyDigest, WeeklyDigest } from '../api/client'
import ReadingMeta from '../components/ReadingMeta'
import BriefingTabs from '../components/BriefingTabs'
import { toast } from '../utils/toast'

function shiftWeek(weekStart: string, days: number): string {
  const d = new Date(weekStart + 'T00:00:00+08:00')
  d.setDate(d.getDate() + days)
  const shanghai = new Date(d.getTime() + 8 * 3600 * 1000)
  return shanghai.toISOString().slice(0, 10)
}

export default function WeeklyPage() {
  const [digest, setDigest] = useState<WeeklyDigest | null>(null)
  const [loading, setLoading] = useState(true)
  const [week, setWeek] = useState<string | undefined>(undefined)

  useEffect(() => { load(week) }, [week])

  const load = async (w?: string) => {
    setLoading(true)
    try {
      const data = await getWeeklyDigest(w)
      setDigest(data)
    } catch (err: any) {
      toast.error(err?.response?.data?.error || '加载周刊失败')
    } finally {
      setLoading(false)
    }
  }

  if (loading) return (
    <div>
      <BriefingTabs current="weekly" />
      <div className="card">加载中…</div>
    </div>
  )
  if (!digest) return (
    <div>
      <BriefingTabs current="weekly" />
      <div className="card">暂无数据</div>
    </div>
  )

  return (
    <div>
      <BriefingTabs current="weekly" />
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 16 }}>
        <h2 style={{ margin: 0 }}>本周精选 · {digest.week_start}</h2>
        <div style={{ display: 'flex', gap: 8 }}>
          <button className="secondary" title="查看上一周" onClick={() => setWeek(shiftWeek(digest.week_start, -7))}>‹ 上一周</button>
          <button className="secondary" title="查看下一周" onClick={() => setWeek(shiftWeek(digest.week_start, 7))}>下一周 ›</button>
          {week !== undefined && (
            <button className="secondary" title="回到本周" onClick={() => setWeek(undefined)}>本周</button>
          )}
        </div>
      </div>

      {digest.pending && digest.articles.length === 0 ? (
        <div className="card">周报生成中,稍后刷新…</div>
      ) : (
        <>
          {digest.intro_text ? (
            <div className="card" style={{ marginBottom: 16, lineHeight: 1.7 }}>
              {digest.intro_text}
            </div>
          ) : (
            <div className="card text-muted" style={{ marginBottom: 16, fontSize: 13 }}>
              {digest.articles.length === 0
                ? '本周暂无入选文章。'
                : '本周导语生成失败或暂未生成,以下是入选文章:'}
            </div>
          )}

          {digest.articles.length === 0 ? null : (
            <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
              {digest.articles.map(a => (
                <Link key={a.id} to={`/articles/${a.id}`} className="card" style={{ display: 'block', textDecoration: 'none', color: 'inherit' }}>
                  <div style={{ fontSize: 15, fontWeight: 600, marginBottom: 4 }}>{a.title}</div>
                  <div style={{ display: 'flex', gap: 12, alignItems: 'center', flexWrap: 'wrap', marginBottom: 6 }}>
                    {a.feed_title && <span className="text-muted text-sm">{a.feed_title}</span>}
                    {a.published_at && <span className="text-muted text-sm">{new Date(a.published_at).toLocaleDateString('zh-CN')}</span>}
                    <ReadingMeta wordCount={a.word_count} readingMinutes={a.reading_minutes} />
                  </div>
                  {a.summary_brief && <div className="text-muted" style={{ fontSize: 13, lineHeight: 1.5 }}>{a.summary_brief}</div>}
                </Link>
              ))}
            </div>
          )}
        </>
      )}
    </div>
  )
}
```

- [ ] **Step 3: Type-check**

Run: `cd frontend && npx tsc --noEmit`

Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/pages/WeeklyPage.tsx
git commit -m "feat(ui): WeeklyPage shows pending placeholder + BriefingTabs"
```

---

## Task 16 — Frontend: nav swap

**Files:**
- Modify: `frontend/src/App.tsx`
- Modify: `frontend/src/components/Layout.tsx`
- Modify: `frontend/src/components/MoreSheet.tsx`

- [ ] **Step 1: Add new routes to `App.tsx`**

Find the existing `<Route path="weekly" element={<WeeklyPage />} />` line. Add two adjacent imports near the other page imports:

```tsx
import DailyPage from './pages/DailyPage'
import BriefingRedirect from './components/BriefingRedirect'
```

And add two route lines just before `<Route path="weekly" …>`:

```tsx
          <Route path="briefing" element={<BriefingRedirect />} />
          <Route path="daily" element={<DailyPage />} />
```

(Leave the existing `<Route path="weekly" element={<WeeklyPage />} />` line in place.)

- [ ] **Step 2: Swap nav entry in `Layout.tsx`**

In `NAV_ITEMS`, replace this line:

```tsx
  { to: '/weekly',             icon: '📅', label: '周刊' },
```

with:

```tsx
  { to: '/briefing',           icon: '📅', label: '简报' },
```

- [ ] **Step 3: Remove `周刊` row from `MoreSheet.tsx`**

In `ITEMS`, delete the line:

```tsx
  { icon: '📅', label: '周刊',     to: '/weekly' },
```

- [ ] **Step 4: Type-check**

Run: `cd frontend && npx tsc --noEmit`

Expected: no errors.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/App.tsx frontend/src/components/Layout.tsx frontend/src/components/MoreSheet.tsx
git commit -m "feat(ui): swap 周刊 nav for 简报 redirect, add /daily route"
```

---

## Task 17 — Manual integration

**Files:** none (manual ops).

- [ ] **Step 1: Apply the migration**

```bash
docker compose exec -T postgres psql -U postgres -d rsspal < backend/migrations/031_daily_briefing.sql
```

Expected: `CREATE TABLE` / `CREATE INDEX` / `ALTER TABLE` notices.

Verify schema:

```bash
docker compose exec -T postgres psql -U postgres -d rsspal -c "\d daily_digests"
docker compose exec -T postgres psql -U postgres -d rsspal -c "SELECT column_name FROM information_schema.columns WHERE table_name='users' AND column_name='briefing_last_tab'"
```

Expected: column `briefing_last_tab` shows up.

- [ ] **Step 2: Rebuild and restart services**

```bash
docker compose up -d --build api worker frontend
```

- [ ] **Step 3: Trigger immediate generation for testing**

Stop the worker, set the dev hook, restart:

```bash
docker compose stop worker
BRIEFING_RUN_NOW=1 docker compose up -d worker
docker compose logs -f worker | grep briefing
```

Expected log lines:
```
briefing cron: BRIEFING_RUN_NOW=1 → firing immediately
briefing.daily: N users to generate for 2026-05-28
briefing.daily user=1 day=2026-05-28: ok (5 picks)
```

Verify a row landed:

```bash
docker compose exec -T postgres psql -U postgres -d rsspal -c "SELECT user_id, day_start, length(intro_text), array_length(article_ids, 1) FROM daily_digests ORDER BY day_start DESC LIMIT 5"
```

Expected: at least 1 row with `length ≈ 200-600` and `array_length = 5`.

- [ ] **Step 4: Browser walkthrough**

1. Open the app, login.
2. Click the `📅 简报` nav button. First-time visit lands on `/daily` (default).
3. Verify the page renders with 5 articles + intro card.
4. Click the `周报` tab — `/weekly` page renders. Reload `/briefing` — should now land on `/weekly`.
5. Click `日报` again, navigate elsewhere, return to `/briefing` — lands on `/daily`.
6. Click `‹ 前一天` — navigates back; verify articles and intro update or `pending` banner appears for days the worker hasn't generated yet.
7. Click `后一天 ›` past today's label — query shifts but server returns 400; toast appears. (Or the button enables a navigation to "today" which renders the `live` banner — both acceptable.)
8. Open browser dev tools → Network → reload `/daily` — confirm `GET /api/daily-digest` returns the expected JSON.

- [ ] **Step 5: Final test suite**

```bash
cd backend && go test ./...
cd ../frontend && npx tsc --noEmit
```

Both green.

- [ ] **Step 6: Push branch + verify PR**

```bash
git push origin feature/daily-briefing
gh pr view 35 --json url,state,statusCheckRollup
```

Expected: PR #35 still OPEN, additional commits visible.

---

## Self-review notes

- **Spec coverage** — every section of the spec has a task:
  - §1 time window → Task 5 helpers (`TodayLabel`, `DailyWindow`).
  - §2 DB schema → Task 1.
  - §3 AI generation → Task 2.
  - §4 repo + handlers → Tasks 3, 5, 6, 8.
  - §5 worker scheduling → Tasks 9, 10.
  - §6 weekly migration → Tasks 7, 8.
  - §7 frontend → Tasks 11-16.
  - §8 edge cases — covered by branches in Tasks 2 (intro length, pick count, dynamic N), 5 (date bounds, fallback), 9 (no candidates, no AI key).
  - §9 testing → unit tests live in Tasks 2/5/6/9; manual checks in Task 17.

- **Architecture deviation from spec** — spec described a per-minute ticker (`time.NewTicker(60s)`) with `lastFire` tracking. The plan uses the existing `nextDaily0400CST`-style sleep-until-next pattern (one timer per fire) because it's already the codebase idiom (`cmd/worker/insights.go`) and cleaner. Behavior is equivalent: one fire per 05:00 boundary, idempotent via `UserIDsMissing`.

- **Placeholder scan** — every step lists exact file paths, exact commands, exact code; no TBD / TODO / "handle edge cases".

- **Type consistency** — `BriefingTab = 'daily' | 'weekly'` is consistent across `client.ts`, `BriefingTabs.tsx`, `BriefingRedirect.tsx`. `DailyCandidate.Idx` is 0-based in both AI and worker code. The `mode` field enum is consistent between handler and frontend types.

- **Out-of-scope deferrals** — repo-level DB tests are not added because no existing repository has them; tests are concentrated in pure-function layer (parsing, date math, prompt construction) which gives the highest coverage-per-effort.
