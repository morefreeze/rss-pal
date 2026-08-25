# Weekly Digest Generation Status Implementation Plan

> **For agentic workers:** Choose the execution mode with the Execution Routing section below. Use superpowers:executing-plans for small or tightly coupled plans, and superpowers:subagent-driven-development for larger plans with independently reviewable tasks. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Distinguish scheduled, pending, expired, and ready weekly digests and show the exact Beijing-time generation hour before generation begins.

**Architecture:** Put the weekly schedule and missing-digest state calculation in a focused shared Go file under `internal/api`, which the worker already imports. Extend the weekly API response with backward-compatible metadata, then use a small pure TypeScript formatter to select the empty-state message in `WeeklyPage`.

**Tech Stack:** Go, Gin, PostgreSQL repositories, React 18, TypeScript, Vitest, GitHub Actions, Docker Compose on Tencent.

---

## Execution Routing

Use **Inline Execution** with `superpowers:executing-plans`. The two implementation tasks share one state contract and are small enough that a single context reduces the risk of mismatched status names or time semantics.

## File Map

- Create `backend/internal/api/weekly_schedule.go`: shared schedule constants, status type, scheduled timestamp, recent completed weeks, and pure state calculation.
- Create `backend/internal/api/weekly_schedule_test.go`: deterministic boundary and compatibility tests for all four states.
- Modify `backend/internal/api/weekly.go`: inject a clock, add API metadata, and preserve the legacy `pending` field.
- Create `backend/internal/api/weekly_test.go`: HTTP-level assertions for the new JSON fields.
- Modify `backend/cmd/worker/briefing.go`: reuse the shared two-week rule and recent-week helper.
- Modify `backend/cmd/worker/briefing_test.go`: assert the worker receives the same two-week sequence through the shared helper.
- Modify `frontend/src/api/client.ts`: extend the `WeeklyDigest` response type.
- Create `frontend/src/util/weeklyGenerationStatus.ts`: pure timestamp formatting and empty-state message selection.
- Create `frontend/src/util/weeklyGenerationStatus.test.ts`: scheduled, pending, not-planned, ready, invalid timestamp, and legacy response tests.
- Modify `frontend/src/pages/WeeklyPage.tsx`: render the selected status message before the normal digest content path.

### Task 1: Shared weekly schedule and API status contract

**Files:**
- Create: `backend/internal/api/weekly_schedule.go`
- Create: `backend/internal/api/weekly_schedule_test.go`
- Modify: `backend/internal/api/weekly.go:12-84`
- Create: `backend/internal/api/weekly_test.go`
- Modify: `backend/cmd/worker/briefing.go:14-42,288-296`
- Modify: `backend/cmd/worker/briefing_test.go:52-81`

- [ ] **Step 1: Write failing pure schedule tests**

Create `backend/internal/api/weekly_schedule_test.go`:

```go
package api

import (
	"testing"
	"time"
)

func weeklyTestTime(year int, month time.Month, day, hour, minute int) time.Time {
	return time.Date(year, month, day, hour, minute, 0, 0, shanghai)
}

func TestWeeklyGenerationMetadata(t *testing.T) {
	weekStart := weeklyTestTime(2026, time.August, 24, 0, 0)
	tests := []struct {
		name       string
		now        time.Time
		cached     bool
		wantStatus WeeklyGenerationStatus
		wantPending bool
		wantETA    string
	}{
		{"before schedule", weeklyTestTime(2026, time.August, 25, 12, 0), false, WeeklyGenerationScheduled, true, "2026-08-31T05:00:00+08:00"},
		{"at schedule", weeklyTestTime(2026, time.August, 31, 5, 0), false, WeeklyGenerationPending, true, ""},
		{"second completed week", weeklyTestTime(2026, time.September, 7, 8, 0), false, WeeklyGenerationPending, true, ""},
		{"outside catch-up", weeklyTestTime(2026, time.September, 14, 8, 0), false, WeeklyGenerationNotPlanned, false, ""},
		{"cached wins", weeklyTestTime(2026, time.September, 14, 8, 0), true, WeeklyGenerationReady, false, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := WeeklyGenerationMetadataAt(tt.now, weekStart, tt.cached)
			if got.Status != tt.wantStatus || got.Pending != tt.wantPending {
				t.Fatalf("metadata = %+v, want status=%s pending=%v", got, tt.wantStatus, tt.wantPending)
			}
			gotETA := ""
			if got.EstimatedGenerationAt != nil {
				gotETA = got.EstimatedGenerationAt.Format(time.RFC3339)
			}
			if gotETA != tt.wantETA {
				t.Fatalf("eta = %q, want %q", gotETA, tt.wantETA)
			}
		})
	}
}

func TestRecentCompletedWeekStartsUsesSharedWindow(t *testing.T) {
	got := RecentCompletedWeekStarts(weeklyTestTime(2026, time.August, 25, 10, 0), WeeklyCatchUpWeekCount)
	want := []time.Time{
		weeklyTestTime(2026, time.August, 17, 0, 0),
		weeklyTestTime(2026, time.August, 10, 0, 0),
	}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if !got[i].Equal(want[i]) {
			t.Fatalf("week[%d] = %s, want %s", i, got[i], want[i])
		}
	}
}
```

- [ ] **Step 2: Run the pure tests and verify RED**

Run:

```bash
cd backend
go test ./internal/api -run 'TestWeeklyGenerationMetadata|TestRecentCompletedWeekStartsUsesSharedWindow' -count=1
```

Expected: compilation fails because `WeeklyGenerationStatus`, `WeeklyGenerationMetadataAt`, `RecentCompletedWeekStarts`, and `WeeklyCatchUpWeekCount` do not exist.

- [ ] **Step 3: Implement the shared schedule module**

Create `backend/internal/api/weekly_schedule.go`:

```go
package api

import "time"

const (
	WeeklyGenerationHourCST = 5
	WeeklyCatchUpWeekCount   = 2
)

type WeeklyGenerationStatus string

const (
	WeeklyGenerationReady      WeeklyGenerationStatus = "ready"
	WeeklyGenerationScheduled  WeeklyGenerationStatus = "scheduled"
	WeeklyGenerationPending    WeeklyGenerationStatus = "pending"
	WeeklyGenerationNotPlanned WeeklyGenerationStatus = "not_planned"
)

type WeeklyGenerationMetadata struct {
	Status                WeeklyGenerationStatus
	Pending               bool
	EstimatedGenerationAt *time.Time
}

func WeeklyScheduledAt(weekStart time.Time) time.Time {
	nextMonday := startOfWeek(weekStart).AddDate(0, 0, 7)
	return time.Date(nextMonday.Year(), nextMonday.Month(), nextMonday.Day(), WeeklyGenerationHourCST, 0, 0, 0, shanghai)
}

func RecentCompletedWeekStarts(now time.Time, count int) []time.Time {
	thisWeek := startOfWeek(now)
	weeks := make([]time.Time, 0, count)
	for k := 1; k <= count; k++ {
		weeks = append(weeks, thisWeek.AddDate(0, 0, -7*k))
	}
	return weeks
}

func WeeklyGenerationMetadataAt(now, weekStart time.Time, cached bool) WeeklyGenerationMetadata {
	if cached {
		return WeeklyGenerationMetadata{Status: WeeklyGenerationReady}
	}
	weekStart = startOfWeek(weekStart)
	scheduledAt := WeeklyScheduledAt(weekStart)
	if now.In(shanghai).Before(scheduledAt) {
		return WeeklyGenerationMetadata{
			Status:                WeeklyGenerationScheduled,
			Pending:               true,
			EstimatedGenerationAt: &scheduledAt,
		}
	}
	oldestEligible := startOfWeek(now).AddDate(0, 0, -7*WeeklyCatchUpWeekCount)
	if !weekStart.Before(oldestEligible) && weekStart.Before(startOfWeek(now)) {
		return WeeklyGenerationMetadata{Status: WeeklyGenerationPending, Pending: true}
	}
	return WeeklyGenerationMetadata{Status: WeeklyGenerationNotPlanned}
}
```

- [ ] **Step 4: Run the pure tests and verify GREEN**

Run:

```bash
cd backend
go test ./internal/api -run 'TestWeeklyGenerationMetadata|TestRecentCompletedWeekStartsUsesSharedWindow' -count=1
```

Expected: both tests pass.

- [ ] **Step 5: Write a failing HTTP response test**

Create `backend/internal/api/weekly_test.go` in package `api`. Use `testdb.New(t)`, insert a user, build the real repositories, override the handler clock, set `userID` on a Gin test context, and request `?week=2026-08-24`:

```go
func TestWeeklyHandlerScheduledResponse(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	var userID int
	if err := db.QueryRow(`INSERT INTO users (username, password_hash) VALUES ('weekly-state-user', 'x') RETURNING id`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	h := NewWeeklyHandler(repository.NewArticleRepository(db), repository.NewWeeklyDigestRepository(db))
	h.now = func() time.Time { return weeklyTestTime(2026, time.August, 25, 12, 0) }

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/weekly-digest?week=2026-08-24", nil)
	c.Set("userID", userID)
	h.Get(c)

	var body struct {
		Pending               bool                   `json:"pending"`
		GenerationStatus       WeeklyGenerationStatus `json:"generation_status"`
		EstimatedGenerationAt string                 `json:"estimated_generation_at"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusOK || !body.Pending || body.GenerationStatus != WeeklyGenerationScheduled || body.EstimatedGenerationAt != "2026-08-31T05:00:00+08:00" {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}
```

Include imports for `encoding/json`, `net/http`, `net/http/httptest`, `testing`, `time`, the repository packages, and Gin.

- [ ] **Step 6: Run the HTTP test and verify RED**

Run:

```bash
cd backend
go test ./internal/api -run TestWeeklyHandlerScheduledResponse -count=1
```

Expected: compilation fails because `WeeklyHandler` has no injectable `now` function, or the response assertion fails because the new fields are absent.

- [ ] **Step 7: Add API metadata and clock injection**

Modify `backend/internal/api/weekly.go`:

```go
type WeeklyHandler struct {
	articleRepo *repository.ArticleRepository
	digestRepo  *repository.WeeklyDigestRepository
	now         func() time.Time
}

func NewWeeklyHandler(articleRepo *repository.ArticleRepository, digestRepo *repository.WeeklyDigestRepository) *WeeklyHandler {
	return &WeeklyHandler{articleRepo: articleRepo, digestRepo: digestRepo, now: time.Now}
}
```

Use `now := h.now()` for the default week and metadata. For a missing row:

```go
metadata := WeeklyGenerationMetadataAt(now, weekStart, false)
response := gin.H{
	"week_start":        weekStart.Format("2006-01-02"),
	"intro_text":        "",
	"articles":          []model.Article{},
	"pending":           metadata.Pending,
	"generation_status": metadata.Status,
}
if metadata.EstimatedGenerationAt != nil {
	response["estimated_generation_at"] = metadata.EstimatedGenerationAt.Format(time.RFC3339)
}
c.JSON(http.StatusOK, response)
```

For a cached row, add:

```go
"generation_status": WeeklyGenerationReady,
```

and keep `pending: false`.

- [ ] **Step 8: Run API tests and verify GREEN**

Run:

```bash
cd backend
go test ./internal/api -run 'TestWeekly' -count=1
```

Expected: the schedule and HTTP tests pass.

- [ ] **Step 9: Make the worker reuse the shared window**

In `backend/cmd/worker/briefing.go`, remove the local `weeklyCatchUpWeekCount` constant and `recentCompletedWeekStarts` function. Keep only:

```go
const briefingHourCST = 5
```

Change startup catch-up to:

```go
for _, weekStart := range api.RecentCompletedWeekStarts(now, api.WeeklyCatchUpWeekCount) {
	fireWeeklyForAllUsers(ctx, deps, weekStart)
}
```

Update `backend/cmd/worker/briefing_test.go` to call:

```go
got := api.RecentCompletedWeekStarts(tt.now, api.WeeklyCatchUpWeekCount)
```

and add the `internal/api` import.

- [ ] **Step 10: Run worker and backend tests**

Run:

```bash
cd backend
gofmt -w internal/api/weekly_schedule.go internal/api/weekly_schedule_test.go internal/api/weekly.go internal/api/weekly_test.go cmd/worker/briefing.go cmd/worker/briefing_test.go
go test ./cmd/worker ./internal/api -count=1
go test ./... -count=1
```

Expected: all backend packages pass.

- [ ] **Step 11: Commit the backend contract**

```bash
git add backend/internal/api/weekly_schedule.go backend/internal/api/weekly_schedule_test.go backend/internal/api/weekly.go backend/internal/api/weekly_test.go backend/cmd/worker/briefing.go backend/cmd/worker/briefing_test.go
git commit -m "feat: expose weekly generation states"
```

### Task 2: Frontend weekly empty-state messages

**Files:**
- Modify: `frontend/src/api/client.ts:887-895`
- Create: `frontend/src/util/weeklyGenerationStatus.ts`
- Create: `frontend/src/util/weeklyGenerationStatus.test.ts`
- Modify: `frontend/src/pages/WeeklyPage.tsx:1-9,64-86`

- [ ] **Step 1: Extend the API type**

Modify `WeeklyDigest` in `frontend/src/api/client.ts`:

```ts
export type WeeklyGenerationStatus = 'ready' | 'scheduled' | 'pending' | 'not_planned'

export interface WeeklyDigest {
  week_start: string
  intro_text: string
  articles: Article[]
  pending?: boolean
  generation_status?: WeeklyGenerationStatus
  estimated_generation_at?: string
}
```

- [ ] **Step 2: Write failing formatter tests**

Create `frontend/src/util/weeklyGenerationStatus.test.ts`:

```ts
import { describe, expect, it } from 'vitest'
import { weeklyEmptyStateMessage } from './weeklyGenerationStatus'

const digest = (overrides: Record<string, unknown>) => ({
  week_start: '2026-08-24',
  intro_text: '',
  articles: [],
  ...overrides,
})

describe('weeklyEmptyStateMessage', () => {
  it('shows the scheduled Beijing hour', () => {
    expect(weeklyEmptyStateMessage(digest({
      pending: true,
      generation_status: 'scheduled',
      estimated_generation_at: '2026-08-31T05:00:00+08:00',
    }))).toBe('预计于 2026-08-31 05:00（北京时间）开始生成')
  })

  it('shows pending after generation becomes eligible', () => {
    expect(weeklyEmptyStateMessage(digest({ pending: true, generation_status: 'pending' })))
      .toBe('周报生成中，稍后刷新…')
  })

  it('shows that an expired digest will not be generated', () => {
    expect(weeklyEmptyStateMessage(digest({ pending: false, generation_status: 'not_planned' })))
      .toBe('该周报已过自动生成范围，不再生成。')
  })

  it('keeps the legacy pending fallback', () => {
    expect(weeklyEmptyStateMessage(digest({ pending: true })))
      .toBe('周报生成中，稍后刷新…')
  })

  it('falls back when a scheduled timestamp is invalid', () => {
    expect(weeklyEmptyStateMessage(digest({ pending: true, generation_status: 'scheduled', estimated_generation_at: 'invalid' })))
      .toBe('周报生成中，稍后刷新…')
  })

  it('returns null for a ready empty digest', () => {
    expect(weeklyEmptyStateMessage(digest({ pending: false, generation_status: 'ready' }))).toBeNull()
  })
})
```

- [ ] **Step 3: Run formatter tests and verify RED**

Run:

```bash
cd frontend
PATH=/Users/bytedance/.nvm/versions/node/v22.19.0/bin:$PATH npm test -- src/util/weeklyGenerationStatus.test.ts
```

Expected: the test fails because `weeklyGenerationStatus.ts` does not exist.

- [ ] **Step 4: Implement the pure formatter**

Create `frontend/src/util/weeklyGenerationStatus.ts`:

```ts
import type { WeeklyDigest } from '../api/client'

const pendingMessage = '周报生成中，稍后刷新…'

function formatBeijingHour(value: string): string | null {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return null
  const parts = new Intl.DateTimeFormat('en-CA', {
    timeZone: 'Asia/Shanghai',
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    hourCycle: 'h23',
  }).formatToParts(date)
  const get = (type: Intl.DateTimeFormatPartTypes) => parts.find(part => part.type === type)?.value
  const year = get('year')
  const month = get('month')
  const day = get('day')
  const hour = get('hour')
  const minute = get('minute')
  return year && month && day && hour && minute ? `${year}-${month}-${day} ${hour}:${minute}` : null
}

export function weeklyEmptyStateMessage(digest: Pick<WeeklyDigest, 'pending' | 'generation_status' | 'estimated_generation_at'>): string | null {
  switch (digest.generation_status) {
    case 'scheduled': {
      const time = digest.estimated_generation_at && formatBeijingHour(digest.estimated_generation_at)
      return time ? `预计于 ${time}（北京时间）开始生成` : pendingMessage
    }
    case 'pending':
      return pendingMessage
    case 'not_planned':
      return '该周报已过自动生成范围，不再生成。'
    case 'ready':
      return null
    default:
      return digest.pending ? pendingMessage : null
  }
}
```

- [ ] **Step 5: Run formatter tests and verify GREEN**

Run:

```bash
cd frontend
PATH=/Users/bytedance/.nvm/versions/node/v22.19.0/bin:$PATH npm test -- src/util/weeklyGenerationStatus.test.ts
```

Expected: six tests pass.

- [ ] **Step 6: Wire the formatter into `WeeklyPage`**

Add:

```ts
import { weeklyEmptyStateMessage } from '../util/weeklyGenerationStatus'
```

After the `digest` null check, derive:

```ts
const emptyStateMessage = digest.articles.length === 0
  ? weeklyEmptyStateMessage(digest)
  : null
```

Replace the current `digest.pending && digest.articles.length === 0` branch with:

```tsx
{emptyStateMessage ? (
  <div className="card">{emptyStateMessage}</div>
) : (
  // existing digest intro and article rendering stays unchanged
)}
```

- [ ] **Step 7: Run frontend tests and build**

Run:

```bash
cd frontend
PATH=/Users/bytedance/.nvm/versions/node/v22.19.0/bin:$PATH npm test -- --run
PATH=/Users/bytedance/.nvm/versions/node/v22.19.0/bin:$PATH npm run test:legacy
PATH=/Users/bytedance/.nvm/versions/node/v22.19.0/bin:$PATH npm run build
```

Expected: all Vitest and legacy tests pass, and TypeScript/Vite build exits 0.

- [ ] **Step 8: Commit the frontend states**

```bash
git add frontend/src/api/client.ts frontend/src/util/weeklyGenerationStatus.ts frontend/src/util/weeklyGenerationStatus.test.ts frontend/src/pages/WeeklyPage.tsx
git commit -m "feat: explain weekly generation timing"
```

### Task 3: Final verification, integration, and deployment

**Files:**
- Verify all files above
- No new production files

- [ ] **Step 1: Verify the complete branch**

Run:

```bash
git diff --check master...HEAD
cd backend && go test ./... -count=1
cd ../frontend && PATH=/Users/bytedance/.nvm/versions/node/v22.19.0/bin:$PATH npm run check
PATH=/Users/bytedance/.nvm/versions/node/v22.19.0/bin:$PATH npm run build
git status --short --branch
```

Expected: no whitespace errors, all backend/frontend tests pass, build exits 0, and the worktree is clean.

- [ ] **Step 2: Review the requirements against the diff**

Confirm from `git diff master...HEAD`:

- scheduled output includes `2026-08-31T05:00:00+08:00` for week `2026-08-24`;
- exactly at 05:00 changes to pending;
- two completed weeks remain eligible;
- older missing weeks are `not_planned`;
- cached rows are `ready`;
- `pending` remains present for old clients;
- frontend renders the three requested empty states;
- worker and API use the same two-week constant.

- [ ] **Step 3: Integrate into `master`**

Use `superpowers:finishing-a-development-branch`. Merge without rewriting unrelated history, then run the full backend/frontend verification again on `master`.

- [ ] **Step 4: Push and deploy**

Push `master` so `.github/workflows/deploy-tencent.yml` deploys through the trusted self-hosted runner. Wait for the GitHub Actions run to finish successfully.

- [ ] **Step 5: Verify Tencent and public runtime**

Verify:

- Tencent `/opt/rss-pal` deployed commit equals `origin/master`;
- API, worker, and frontend containers use the rebuilt images and are healthy;
- public `/api/health` returns 200;
- public `/api/status` reports all components up;
- authenticated `/weekly?week=2026-08-24` shows `预计于 2026-08-31 05:00（北京时间）开始生成` before the scheduled hour;
- an authenticated missing week older than the catch-up window shows `该周报已过自动生成范围，不再生成。`.

- [ ] **Step 6: Close browser sessions and report evidence**

Close the `agent-browser` session, preserve all unrelated untracked backup files, and report the deployed commit, workflow run, test counts, and the two verified production messages.
