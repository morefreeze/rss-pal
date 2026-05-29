# Briefing Navigation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace `/daily` prev/next buttons with a 📅 month-calendar popover and `/weekly` prev/next with an 8-card horizontal W-strip, both color-coded by digest status; make article-detail prev/next/back follow the briefing the user came from instead of the global article list.

**Architecture:** One new read-only backend endpoint (`GET /api/briefing/index`) returns the set of cached daily/weekly dates plus the boundary (`today_label`, `pending_window_start`) the frontend needs to derive cell colors. Two new React components (`BriefingCalendar`, `BriefingWCardStrip`) consume that endpoint. DailyPage and WeeklyPage swap their button strips for these components, sync the selected date/week to the URL query, and snapshot the briefing's article IDs + entryPath into the existing `articleNav` mechanism so article-detail navigation stays inside the briefing.

**Tech Stack:** Go 1.24 (gin + `database/sql`), React 18 + Vite (react-router-dom v6, sessionStorage-based nav snapshot already in place), PostgreSQL 15. No new dependencies. No DB migration.

---

## File Plan

### Backend — create
- `backend/internal/api/briefing_index.go` — `BriefingIndexHandler`, query-param parser helpers, `MAX_SPAN_DAYS` constant.
- `backend/internal/api/briefing_index_test.go` — parser helper tests.

### Backend — modify
- `backend/internal/repository/daily_digest.go` — add `ListDaysInRange(userID, from, to)`.
- `backend/internal/repository/weekly_digest.go` — add `ListWeeksInRange(userID, from, to)`.
- `backend/internal/api/daily.go` — add exported `MondayLabel(now)` (mirrors `TodayLabel`); promotes the helper currently inside `cmd/worker/briefing.go`.
- `backend/cmd/worker/briefing.go` — replace local `mondayShanghai` with `api.MondayLabel`.
- `backend/cmd/server/main.go` — construct + route `BriefingIndexHandler`.

### Frontend — create
- `frontend/src/components/BriefingCalendar.tsx` — month grid popover.
- `frontend/src/components/BriefingWCardStrip.tsx` — 8-card horizontal strip.

### Frontend — modify
- `frontend/src/api/client.ts` — `BriefingIndex` type + `getBriefingIndex(type, from, to)` fn.
- `frontend/src/index.css` — add `--cal-done`, `--cal-pending`, `--cal-disabled`, `--cal-today` to each `body[data-theme='X']` block (one shared cross-theme palette).
- `frontend/src/pages/DailyPage.tsx` — drop prev/next buttons, add 📅 button + popover, URL query sync, `writeNav` + `articleEntryPath` on article click.
- `frontend/src/pages/WeeklyPage.tsx` — replace prev/next with `BriefingWCardStrip`, URL query sync, same `writeNav` + `articleEntryPath` wiring.

---

## Task 1 — Promote `MondayLabel` from worker to api package

**Files:**
- Modify: `backend/internal/api/daily.go`
- Modify: `backend/cmd/worker/briefing.go`

Backend Go binary: `/Users/bytedance/homebrew/bin/go`.

- [ ] **Step 1: Add `MondayLabel` to `backend/internal/api/daily.go`**

Find the existing `DailyWindow` function. Append after it (before `type DailyHandler struct`):

```go
// MondayLabel returns the Monday at 00:00 in Asia/Shanghai of the week containing `now`.
// Symmetric with TodayLabel but week-anchored.
func MondayLabel(now time.Time) time.Time {
	t := now.In(briefingShanghai)
	weekday := int(t.Weekday())
	if weekday == 0 {
		weekday = 7
	}
	mon := t.AddDate(0, 0, -(weekday - 1))
	return time.Date(mon.Year(), mon.Month(), mon.Day(), 0, 0, 0, 0, briefingShanghai)
}
```

- [ ] **Step 2: Drop `mondayShanghai` from `backend/cmd/worker/briefing.go`**

Find and DELETE the entire `mondayShanghai` function (currently at the bottom of `fireBriefings`-related helpers — search for `func mondayShanghai`).

Then in `fireBriefings`, change:
```go
weekStart := mondayShanghai(now).AddDate(0, 0, -7)
```
to:
```go
weekStart := api.MondayLabel(now).AddDate(0, 0, -7)
```

And in `runBriefingCatchUp`, change:
```go
fireWeeklyForAllUsers(ctx, deps, mondayShanghai(now).AddDate(0, 0, -7))
```
to:
```go
fireWeeklyForAllUsers(ctx, deps, api.MondayLabel(now).AddDate(0, 0, -7))
```

(`api` is already imported in `briefing.go`.)

- [ ] **Step 3: Build and test**

```bash
cd /Users/bytedance/mygit/rss-pal/backend
/Users/bytedance/homebrew/bin/go build ./...
/Users/bytedance/homebrew/bin/go test ./...
```

Expected: all green. Existing `TestNextBriefingFire_*` / `TestIsMondayInShanghai` etc. should still pass.

- [ ] **Step 4: Commit**

```bash
git add backend/internal/api/daily.go backend/cmd/worker/briefing.go
git commit -m "refactor(api): promote MondayLabel from worker into shared api package"
```

---

## Task 2 — Repository: `ListDaysInRange` + `ListWeeksInRange`

**Files:**
- Modify: `backend/internal/repository/daily_digest.go`
- Modify: `backend/internal/repository/weekly_digest.go`

- [ ] **Step 1: Add `ListDaysInRange` to `daily_digest.go`**

Append after `UserIDsMissing`:

```go
// ListDaysInRange returns the day_start values this user has digests for
// where from ≤ day_start ≤ to. Ordered ascending. Used by the briefing
// index endpoint to paint the calendar.
func (r *DailyDigestRepository) ListDaysInRange(userID int, from, to time.Time) ([]time.Time, error) {
	rows, err := r.db.Query(`
		SELECT day_start FROM daily_digests
		WHERE user_id = $1 AND day_start BETWEEN $2 AND $3
		ORDER BY day_start
	`, userID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []time.Time
	for rows.Next() {
		var d time.Time
		if err := rows.Scan(&d); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
```

- [ ] **Step 2: Add `ListWeeksInRange` to `weekly_digest.go`**

Append after `UserIDsMissing`:

```go
// ListWeeksInRange returns the week_start values this user has digests for
// where from ≤ week_start ≤ to. Ordered ascending.
func (r *WeeklyDigestRepository) ListWeeksInRange(userID int, from, to time.Time) ([]time.Time, error) {
	rows, err := r.db.Query(`
		SELECT week_start FROM weekly_digests
		WHERE user_id = $1 AND week_start BETWEEN $2 AND $3
		ORDER BY week_start
	`, userID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []time.Time
	for rows.Next() {
		var d time.Time
		if err := rows.Scan(&d); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
```

- [ ] **Step 3: Build check**

```bash
cd /Users/bytedance/mygit/rss-pal/backend
/Users/bytedance/homebrew/bin/go build ./internal/repository/...
```

Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add backend/internal/repository/daily_digest.go backend/internal/repository/weekly_digest.go
git commit -m "feat(repo): ListDaysInRange + ListWeeksInRange for briefing index"
```

---

## Task 3 — API: `BriefingIndexHandler` with TDD parser helpers

**Files:**
- Create: `backend/internal/api/briefing_index.go`
- Test: `backend/internal/api/briefing_index_test.go`

- [ ] **Step 1: Write failing tests `briefing_index_test.go`**

```go
package api

import (
	"testing"
	"time"
)

func TestParseBriefingIndexType_Valid(t *testing.T) {
	if got, err := parseBriefingIndexType("daily"); err != nil || got != "daily" {
		t.Errorf("daily: got (%q, %v)", got, err)
	}
	if got, err := parseBriefingIndexType("weekly"); err != nil || got != "weekly" {
		t.Errorf("weekly: got (%q, %v)", got, err)
	}
}

func TestParseBriefingIndexType_Invalid(t *testing.T) {
	for _, in := range []string{"", "DAILY", "monthly", "day"} {
		if _, err := parseBriefingIndexType(in); err == nil {
			t.Errorf("%q: expected error", in)
		}
	}
}

func TestParseBriefingIndexRange_Valid(t *testing.T) {
	from, to, err := parseBriefingIndexRange("2026-05-01", "2026-05-31")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	wantFrom := time.Date(2026, 5, 1, 0, 0, 0, 0, briefingShanghai)
	wantTo := time.Date(2026, 5, 31, 0, 0, 0, 0, briefingShanghai)
	if !from.Equal(wantFrom) || !to.Equal(wantTo) {
		t.Errorf("got (%s, %s), want (%s, %s)", from, to, wantFrom, wantTo)
	}
}

func TestParseBriefingIndexRange_FromAfterTo(t *testing.T) {
	if _, _, err := parseBriefingIndexRange("2026-06-01", "2026-05-01"); err == nil {
		t.Error("expected error when from > to")
	}
}

func TestParseBriefingIndexRange_TooWide(t *testing.T) {
	// 401 days span — must reject (cap is 400).
	if _, _, err := parseBriefingIndexRange("2025-04-25", "2026-06-01"); err == nil {
		t.Error("expected error on 400+ day span")
	}
}

func TestParseBriefingIndexRange_Empty(t *testing.T) {
	if _, _, err := parseBriefingIndexRange("", "2026-05-31"); err == nil {
		t.Error("expected error on empty from")
	}
	if _, _, err := parseBriefingIndexRange("2026-05-01", ""); err == nil {
		t.Error("expected error on empty to")
	}
}

func TestParseBriefingIndexRange_BadFormat(t *testing.T) {
	if _, _, err := parseBriefingIndexRange("2026/05/01", "2026-05-31"); err == nil {
		t.Error("expected error on slash format")
	}
}
```

- [ ] **Step 2: Run — expect compile failures**

```bash
cd /Users/bytedance/mygit/rss-pal/backend
/Users/bytedance/homebrew/bin/go test ./internal/api/ -run "BriefingIndex" -v 2>&1 | tail -15
```

Expected: `undefined: parseBriefingIndexType` / `undefined: parseBriefingIndexRange`.

- [ ] **Step 3: Write `briefing_index.go`**

```go
package api

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/gin-gonic/gin"
)

// briefingIndexMaxSpanDays caps the from/to range. Keeps the SQL bounded
// and the JSON payload small even when the user requests months of data.
const briefingIndexMaxSpanDays = 400

type BriefingIndexHandler struct {
	dailyRepo  *repository.DailyDigestRepository
	weeklyRepo *repository.WeeklyDigestRepository
}

func NewBriefingIndexHandler(dailyRepo *repository.DailyDigestRepository, weeklyRepo *repository.WeeklyDigestRepository) *BriefingIndexHandler {
	return &BriefingIndexHandler{dailyRepo: dailyRepo, weeklyRepo: weeklyRepo}
}

func parseBriefingIndexType(s string) (string, error) {
	if s == "daily" || s == "weekly" {
		return s, nil
	}
	return "", fmt.Errorf("type 必须是 daily 或 weekly")
}

func parseBriefingIndexRange(from, to string) (time.Time, time.Time, error) {
	if from == "" {
		return time.Time{}, time.Time{}, errors.New("from 必填")
	}
	if to == "" {
		return time.Time{}, time.Time{}, errors.New("to 必填")
	}
	f, err := ParseDailyDate(from)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("from 格式错误: %w", err)
	}
	t, err := ParseDailyDate(to)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("to 格式错误: %w", err)
	}
	if f.After(t) {
		return time.Time{}, time.Time{}, errors.New("from 不能晚于 to")
	}
	if t.Sub(f) > time.Duration(briefingIndexMaxSpanDays)*24*time.Hour {
		return time.Time{}, time.Time{}, fmt.Errorf("范围超过 %d 天上限", briefingIndexMaxSpanDays)
	}
	return f, t, nil
}

// Get serves GET /api/briefing/index?type=&from=&to=
func (h *BriefingIndexHandler) Get(c *gin.Context) {
	userID := getUserID(c)
	kind, err := parseBriefingIndexType(c.Query("type"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	from, to, err := parseBriefingIndexRange(c.Query("from"), c.Query("to"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	now := time.Now()

	if kind == "daily" {
		days, dErr := h.dailyRepo.ListDaysInRange(userID, from, to)
		if dErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": dErr.Error()})
			return
		}
		today := TodayLabel(now)
		c.JSON(http.StatusOK, gin.H{
			"type":                 "daily",
			"today_label":          today.Format("2006-01-02"),
			"pending_window_start": today.AddDate(0, 0, -3).Format("2006-01-02"),
			"cached":               formatDates(days),
		})
		return
	}

	// weekly
	weeks, wErr := h.weeklyRepo.ListWeeksInRange(userID, from, to)
	if wErr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": wErr.Error()})
		return
	}
	thisWeek := MondayLabel(now)
	c.JSON(http.StatusOK, gin.H{
		"type":                 "weekly",
		"this_week_start":      thisWeek.Format("2006-01-02"),
		"pending_window_start": thisWeek.AddDate(0, 0, -7).Format("2006-01-02"),
		"cached":               formatDates(weeks),
	})
}

func formatDates(ts []time.Time) []string {
	out := make([]string, len(ts))
	for i, t := range ts {
		out[i] = t.Format("2006-01-02")
	}
	return out
}
```

- [ ] **Step 4: Run tests — expect green**

```bash
cd /Users/bytedance/mygit/rss-pal/backend
/Users/bytedance/homebrew/bin/go test ./internal/api/ -run "BriefingIndex" -v 2>&1 | tail -20
```

Expected: 7 PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/api/briefing_index.go backend/internal/api/briefing_index_test.go
git commit -m "feat(api): GET /api/briefing/index for calendar/W-strip status"
```

---

## Task 4 — Server wiring for `/api/briefing/index`

**Files:**
- Modify: `backend/cmd/server/main.go`

- [ ] **Step 1: Add the constructor + route**

Find the existing block (around line 67-69 after Tasks 5/6 from prior plan):

```go
weeklyHandler := api.NewWeeklyHandler(articleRepo, weeklyDigestRepo)
dailyHandler := api.NewDailyHandler(articleRepo, dailyDigestRepo)
briefingHandler := api.NewBriefingHandler(userRepo)
```

Add immediately after the third line:

```go
briefingIndexHandler := api.NewBriefingIndexHandler(dailyDigestRepo, weeklyDigestRepo)
```

Then find the existing routes:

```go
		// Weekly / daily briefings (worker generates async; API is read-only)
		apiGroup.GET("/weekly-digest", weeklyHandler.Get)
		apiGroup.GET("/daily-digest", dailyHandler.Get)
		apiGroup.GET("/briefing/last-tab", briefingHandler.GetLastTab)
		apiGroup.POST("/briefing/last-tab", briefingHandler.SetLastTab)
```

Add immediately after:

```go
		apiGroup.GET("/briefing/index", briefingIndexHandler.Get)
```

- [ ] **Step 2: Build + run all tests**

```bash
cd /Users/bytedance/mygit/rss-pal/backend
/Users/bytedance/homebrew/bin/go build ./cmd/server
/Users/bytedance/homebrew/bin/go test ./...
```

Expected: all green.

- [ ] **Step 3: Commit**

```bash
git add backend/cmd/server/main.go
git commit -m "feat(server): wire /api/briefing/index route"
```

---

## Task 5 — Frontend API client

**Files:**
- Modify: `frontend/src/api/client.ts`

- [ ] **Step 1: Add types + function**

Find the existing block:

```ts
export type BriefingTab = 'daily' | 'weekly'

export const getBriefingLastTab = () =>
  api.get<{ tab: BriefingTab }>('/briefing/last-tab').then(r => r.data)

export const setBriefingLastTab = (tab: BriefingTab) =>
  api.post('/briefing/last-tab', { tab })
```

Append immediately after:

```ts
export interface BriefingIndex {
  type: 'daily' | 'weekly'
  today_label?: string         // present when type='daily'
  this_week_start?: string     // present when type='weekly'
  pending_window_start: string
  cached: string[]
}

export const getBriefingIndex = (type: 'daily' | 'weekly', from: string, to: string) =>
  api.get<BriefingIndex>('/briefing/index', { params: { type, from, to } }).then(r => r.data)
```

- [ ] **Step 2: Type-check**

```bash
cd /Users/bytedance/mygit/rss-pal/frontend
npx tsc --noEmit
```

Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/api/client.ts
git commit -m "feat(client): BriefingIndex type + getBriefingIndex"
```

---

## Task 6 — CSS palette for calendar/W-card cells

**Files:**
- Modify: `frontend/src/index.css`

- [ ] **Step 1: Add palette to each theme block**

Find each `body[data-theme='X']` block in `frontend/src/index.css` (themes are `paper`, `quiet`, `pearl`, and possibly others). For EACH theme block, immediately before the closing `}`, add these four lines:

```css
  --cal-done: #22c55e;
  --cal-pending: #f97316;
  --cal-disabled: rgba(0, 0, 0, 0.08);
  --cal-today: var(--accent);
```

If the codebase has a dark theme (e.g. `body[data-theme='midnight']` or similar), use this instead in that block:

```css
  --cal-done: #16a34a;
  --cal-pending: #ea580c;
  --cal-disabled: rgba(255, 255, 255, 0.10);
  --cal-today: var(--accent);
```

(Check `frontend/src/index.css` for each `body[data-theme='X']` selector and add to each. Greens and oranges chosen to read on both light tan and white backgrounds.)

- [ ] **Step 2: Type-check (sanity)**

```bash
cd /Users/bytedance/mygit/rss-pal/frontend
npx tsc --noEmit
```

Expected: no errors (CSS isn't type-checked but tsc confirms no JS regressions from accidental file edits).

- [ ] **Step 3: Commit**

```bash
git add frontend/src/index.css
git commit -m "feat(ui): cal-* palette tokens for briefing calendar/W-cards"
```

---

## Task 7 — `BriefingCalendar` component

**Files:**
- Create: `frontend/src/components/BriefingCalendar.tsx`

- [ ] **Step 1: Write the component**

```tsx
import { useEffect, useMemo, useState } from 'react'
import { getBriefingIndex, BriefingIndex } from '../api/client'

interface Props {
  currentDate: string                  // YYYY-MM-DD — the digest's shown_date
  onPick: (date: string) => void
  onClose: () => void
}

type CellStatus = 'done' | 'pending' | 'disabled' | 'today' | 'future'

function pad(n: number): string {
  return n < 10 ? '0' + n : '' + n
}

function ymd(d: Date): string {
  return d.getFullYear() + '-' + pad(d.getMonth() + 1) + '-' + pad(d.getDate())
}

// Asia/Shanghai-anchored date parse. Treats the input as midnight Shanghai
// and returns a Date whose YMD in any tz still corresponds to that calendar date.
function parseShanghai(s: string): Date {
  return new Date(s + 'T00:00:00+08:00')
}

function firstOfMonth(year: number, month0: number): Date {
  return new Date(Date.UTC(year, month0, 1, -8)) // 00:00 Shanghai
}

function classifyCell(d: string, idx: BriefingIndex | null): CellStatus {
  if (!idx || !idx.today_label) return 'disabled'
  if (d > idx.today_label) return 'future'
  if (d === idx.today_label) return 'today'
  if (idx.cached.includes(d)) return 'done'
  if (d >= idx.pending_window_start) return 'pending'
  return 'disabled'
}

const WEEKDAY_LABELS = ['一', '二', '三', '四', '五', '六', '日']

export default function BriefingCalendar({ currentDate, onPick, onClose }: Props) {
  // Calendar month-anchor: derived initial month from currentDate.
  const initialMonth = useMemo(() => {
    const d = parseShanghai(currentDate)
    // Express as "YYYY-MM"
    return d.getUTCFullYear() + '-' + pad(d.getUTCMonth() + 1)
  }, [currentDate])

  const [month, setMonth] = useState(initialMonth)
  const [index, setIndex] = useState<BriefingIndex | null>(null)

  const [year, monthOneBased] = useMemo(() => {
    const [y, m] = month.split('-').map(Number)
    return [y, m]
  }, [month])
  const month0 = monthOneBased - 1

  // Fetch index for the displayed month (with a small bleed so the
  // calendar's leading/trailing 7-day "fade-in" of adjacent months also
  // gets colored — keeps the API range broad enough that successive month
  // navigation can hit cache if the browser keeps the response).
  useEffect(() => {
    const first = firstOfMonth(year, month0)
    const last = new Date(first)
    last.setUTCMonth(first.getUTCMonth() + 1)
    last.setUTCDate(0)
    // Bleed 7 days on each side.
    const fromDate = new Date(first); fromDate.setUTCDate(first.getUTCDate() - 7)
    const toDate = new Date(last); toDate.setUTCDate(last.getUTCDate() + 7)
    getBriefingIndex('daily', ymd(fromDate), ymd(toDate))
      .then(setIndex)
      .catch(() => { /* leave cells uncolored on error */ })
  }, [year, month0])

  // 6-row × 7-col grid, Monday-first. Compute the first grid day.
  const grid = useMemo(() => {
    const first = firstOfMonth(year, month0)
    // first.getUTCDay() returns 0=Sun .. 6=Sat. We want Monday-first:
    // weekday 1=Mon .. 7=Sun.
    let firstDow = first.getUTCDay()
    if (firstDow === 0) firstDow = 7
    // grid start = first - (firstDow - 1) days
    const gridStart = new Date(first)
    gridStart.setUTCDate(first.getUTCDate() - (firstDow - 1))
    const days: { date: string; inMonth: boolean }[] = []
    for (let i = 0; i < 42; i++) {
      const d = new Date(gridStart)
      d.setUTCDate(gridStart.getUTCDate() + i)
      days.push({
        date: ymd(d),
        inMonth: d.getUTCMonth() === month0,
      })
    }
    return days
  }, [year, month0])

  const shiftMonth = (delta: number) => {
    const next = new Date(Date.UTC(year, month0 + delta, 1, -8))
    setMonth(next.getUTCFullYear() + '-' + pad(next.getUTCMonth() + 1))
  }

  // Click outside / ESC close handled by parent (DailyPage).
  return (
    <div
      role="dialog"
      aria-label="选择日期"
      style={{
        background: 'var(--surface)',
        border: '1px solid var(--border)',
        borderRadius: 8,
        boxShadow: '0 8px 24px rgba(0,0,0,0.18)',
        padding: 12,
        width: 280,
      }}
    >
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 8 }}>
        <button type="button" onClick={() => shiftMonth(-1)} aria-label="上一月" style={btnStyle}>‹</button>
        <div style={{ fontWeight: 600 }}>{year} 年 {monthOneBased} 月</div>
        <button type="button" onClick={() => shiftMonth(1)} aria-label="下一月" style={btnStyle}>›</button>
      </div>
      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(7, 1fr)', gap: 2, marginBottom: 6 }}>
        {WEEKDAY_LABELS.map(w => (
          <div key={w} style={{ textAlign: 'center', fontSize: 11, color: 'var(--fg-muted)', padding: '2px 0' }}>{w}</div>
        ))}
      </div>
      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(7, 1fr)', gap: 2 }}>
        {grid.map(({ date, inMonth }) => {
          const status: CellStatus = inMonth ? classifyCell(date, index) : 'future'
          const disabled = !inMonth || status === 'disabled' || status === 'future'
          const isCurrent = date === currentDate
          return (
            <button
              type="button"
              key={date}
              disabled={disabled}
              aria-disabled={disabled}
              aria-current={isCurrent ? 'true' : undefined}
              onClick={() => { if (!disabled) onPick(date) }}
              data-status={inMonth ? status : 'out'}
              style={{
                height: 32,
                fontSize: 13,
                border: isCurrent ? '2px solid var(--accent)' : '1px solid transparent',
                borderRadius: 4,
                background:
                  !inMonth ? 'transparent' :
                  status === 'done' ? 'var(--cal-done)' :
                  status === 'pending' ? 'var(--cal-pending)' :
                  status === 'today' ? 'var(--cal-today)' :
                  'var(--cal-disabled)',
                color:
                  !inMonth ? 'var(--fg-muted)' :
                  status === 'done' || status === 'pending' || status === 'today' ? '#fff' :
                  'var(--fg)',
                opacity: !inMonth ? 0.45 : status === 'disabled' ? 0.45 : 1,
                cursor: disabled ? 'not-allowed' : 'pointer',
              }}
            >
              {Number(date.slice(-2))}
            </button>
          )
        })}
      </div>
      <div style={{ marginTop: 8, display: 'flex', justifyContent: 'flex-end' }}>
        <button type="button" onClick={onClose} style={btnStyle}>关闭</button>
      </div>
    </div>
  )
}

const btnStyle: React.CSSProperties = {
  background: 'transparent',
  border: '1px solid var(--border)',
  borderRadius: 4,
  padding: '4px 8px',
  cursor: 'pointer',
  color: 'var(--fg)',
}
```

- [ ] **Step 2: Type-check**

```bash
cd /Users/bytedance/mygit/rss-pal/frontend
npx tsc --noEmit
```

Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/BriefingCalendar.tsx
git commit -m "feat(ui): BriefingCalendar month-grid popover with status colors"
```

---

## Task 8 — `DailyPage` rewrite

**Files:**
- Modify: `frontend/src/pages/DailyPage.tsx`

The whole file is being replaced — read it first if needed to confirm imports, then write the new content.

- [ ] **Step 1: Replace file content**

```tsx
import { useEffect, useRef, useState } from 'react'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'
import { getDailyDigest, DailyDigest } from '../api/client'
import ReadingMeta from '../components/ReadingMeta'
import BriefingTabs from '../components/BriefingTabs'
import BriefingCalendar from '../components/BriefingCalendar'
import { writeNav } from '../utils/articleNav'
import { toast } from '../utils/toast'

export default function DailyPage() {
  const [params] = useSearchParams()
  const navigate = useNavigate()

  const [date, setDate] = useState<string | undefined>(() => params.get('date') ?? undefined)
  const [digest, setDigest] = useState<DailyDigest | null>(null)
  const [loading, setLoading] = useState(true)
  const [calOpen, setCalOpen] = useState(false)
  const calBtnRef = useRef<HTMLButtonElement>(null)
  const calPopRef = useRef<HTMLDivElement>(null)

  useEffect(() => { load(date) }, [date])

  // Sync date → URL so back-from-article (using sessionStorage entryPath) lands here cleanly.
  useEffect(() => {
    if (date === undefined && params.get('date') !== null) {
      navigate('/daily', { replace: true })
    } else if (date !== undefined && params.get('date') !== date) {
      navigate('/daily?date=' + date, { replace: true })
    }
  }, [date])

  // Close popover on outside-click / ESC.
  useEffect(() => {
    if (!calOpen) return
    const onDown = (e: MouseEvent) => {
      const t = e.target as Node
      if (calPopRef.current?.contains(t)) return
      if (calBtnRef.current?.contains(t)) return
      setCalOpen(false)
    }
    const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') setCalOpen(false) }
    document.addEventListener('mousedown', onDown)
    document.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('mousedown', onDown)
      document.removeEventListener('keydown', onKey)
    }
  }, [calOpen])

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

  const pickDate = (d: string) => {
    setDate(d)
    setCalOpen(false)
  }

  const onClickArticle = () => {
    if (!digest) return
    writeNav(digest.articles.map(a => a.id), null)
    try {
      sessionStorage.setItem('articleEntryPath', '/daily?date=' + digest.shown_date)
    } catch { /* ignore */ }
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

  const headerTitle = digest.mode === 'live'
    ? `今日精选 · ${digest.shown_date}（收集中）`
    : `本日精选 · ${digest.shown_date}`
  const showStaleTag = digest.pending && digest.shown_date !== digest.requested_date

  return (
    <div>
      <BriefingTabs current="daily" />
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 16, position: 'relative' }}>
        <h2 style={{ margin: 0 }}>{headerTitle}</h2>
        <button
          ref={calBtnRef}
          type="button"
          aria-label="选择日期"
          aria-expanded={calOpen}
          className="secondary"
          onClick={() => setCalOpen(o => !o)}
        >
          📅
        </button>
        {calOpen && (
          <div ref={calPopRef} style={{ position: 'absolute', top: '100%', right: 0, marginTop: 8, zIndex: 100 }}>
            <BriefingCalendar
              currentDate={digest.shown_date}
              onPick={pickDate}
              onClose={() => setCalOpen(false)}
            />
          </div>
        )}
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
                <Link
                  key={a.id}
                  to={`/articles/${a.id}`}
                  onClick={onClickArticle}
                  className="card"
                  style={{ display: 'block', textDecoration: 'none', color: 'inherit' }}
                >
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

```bash
cd /Users/bytedance/mygit/rss-pal/frontend
npx tsc --noEmit
```

Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/pages/DailyPage.tsx
git commit -m "feat(ui): DailyPage uses 📅 calendar popover + URL sync + writeNav"
```

---

## Task 9 — `BriefingWCardStrip` component

**Files:**
- Create: `frontend/src/components/BriefingWCardStrip.tsx`

- [ ] **Step 1: Write the component**

```tsx
import { useEffect, useMemo, useRef, useState } from 'react'
import { getBriefingIndex, BriefingIndex } from '../api/client'

interface Props {
  currentWeekStart: string             // YYYY-MM-DD (Monday Asia/Shanghai)
  onPick: (weekStart: string) => void
}

type Status = 'done' | 'pending' | 'disabled' | 'today'

function pad(n: number): string {
  return n < 10 ? '0' + n : '' + n
}
function ymd(d: Date): string {
  return d.getUTCFullYear() + '-' + pad(d.getUTCMonth() + 1) + '-' + pad(d.getUTCDate())
}
function parseMondayUTC(s: string): Date {
  return new Date(s + 'T00:00:00+08:00')
}

// monthRelativeWeekNumber: for a Monday `weekStart`, return its position
// in the calendar order of Mondays within its month (1-based). E.g.
// first Monday of the month → 1, second → 2 …
function monthRelativeWeekNumber(weekStart: string): number {
  const d = parseMondayUTC(weekStart)
  // Find the first Monday of d's month, then count weeks.
  const firstOfMonth = new Date(Date.UTC(d.getUTCFullYear(), d.getUTCMonth(), 1, -8))
  let dow = firstOfMonth.getUTCDay() // 0=Sun
  if (dow === 0) dow = 7
  const offsetToFirstMonday = dow === 1 ? 0 : (8 - dow)
  const firstMonday = new Date(firstOfMonth)
  firstMonday.setUTCDate(firstOfMonth.getUTCDate() + offsetToFirstMonday)
  const diffDays = Math.round((d.getTime() - firstMonday.getTime()) / 86400000)
  return Math.floor(diffDays / 7) + 1
}

function rangeText(weekStart: string): string {
  const start = parseMondayUTC(weekStart)
  const end = new Date(start); end.setUTCDate(start.getUTCDate() + 6)
  return pad(start.getUTCMonth() + 1) + '.' + pad(start.getUTCDate())
    + '-' + pad(end.getUTCMonth() + 1) + '.' + pad(end.getUTCDate())
}

function classify(ws: string, idx: BriefingIndex | null): Status {
  if (!idx || !idx.this_week_start) return 'disabled'
  if (ws === idx.this_week_start) return 'today'
  if (idx.cached.includes(ws)) return 'done'
  if (ws >= idx.pending_window_start && ws < idx.this_week_start) return 'pending'
  return 'disabled'
}

export default function BriefingWCardStrip({ currentWeekStart, onPick }: Props) {
  const [index, setIndex] = useState<BriefingIndex | null>(null)
  const stripRef = useRef<HTMLDivElement>(null)

  // 8 consecutive Mondays ending at this_week_start. Computed from
  // currentWeekStart first; once index loads we anchor to this_week_start
  // so the strip's right-most card is always "本周".
  const anchor = index?.this_week_start ?? currentWeekStart
  const weeks = useMemo(() => {
    const anchorD = parseMondayUTC(anchor)
    const out: string[] = []
    for (let i = 7; i >= 0; i--) {
      const d = new Date(anchorD); d.setUTCDate(anchorD.getUTCDate() - i * 7)
      out.push(ymd(d))
    }
    return out
  }, [anchor])

  // Fetch index spanning the 8 displayed weeks plus a small bleed.
  useEffect(() => {
    if (weeks.length === 0) return
    const from = weeks[0]
    const to = parseMondayUTC(weeks[weeks.length - 1])
    to.setUTCDate(to.getUTCDate() + 6)
    getBriefingIndex('weekly', from, ymd(to))
      .then(setIndex)
      .catch(() => { /* leave uncolored */ })
  }, [weeks])

  // On first paint (and when weeks change), scroll the strip all the way right
  // so the most recent week is in view.
  useEffect(() => {
    const el = stripRef.current
    if (el) el.scrollLeft = el.scrollWidth
  }, [weeks])

  return (
    <div
      ref={stripRef}
      style={{
        display: 'flex',
        gap: 8,
        overflowX: 'auto',
        paddingBottom: 8,
        marginBottom: 16,
      }}
    >
      {weeks.map(ws => {
        const status = classify(ws, index)
        const disabled = status === 'disabled'
        const isCurrent = ws === currentWeekStart
        return (
          <button
            type="button"
            key={ws}
            disabled={disabled}
            aria-disabled={disabled}
            aria-current={isCurrent ? 'true' : undefined}
            data-status={status}
            onClick={() => { if (!disabled) onPick(ws) }}
            style={{
              flex: '0 0 auto',
              width: 88,
              padding: '8px 6px',
              border: isCurrent ? '2px solid var(--accent)' : '1px solid transparent',
              borderRadius: 8,
              background:
                status === 'done' ? 'var(--cal-done)' :
                status === 'pending' ? 'var(--cal-pending)' :
                status === 'today' ? 'var(--cal-today)' :
                'var(--cal-disabled)',
              color:
                status === 'done' || status === 'pending' || status === 'today' ? '#fff' :
                'var(--fg)',
              opacity: status === 'disabled' ? 0.45 : 1,
              cursor: disabled ? 'not-allowed' : 'pointer',
              textAlign: 'center',
            }}
          >
            <div style={{ fontSize: 18, fontWeight: 700, lineHeight: 1 }}>
              W{monthRelativeWeekNumber(ws)}
            </div>
            <div style={{ fontSize: 11, marginTop: 4, opacity: 0.9 }}>
              {rangeText(ws)}
            </div>
          </button>
        )
      })}
    </div>
  )
}
```

- [ ] **Step 2: Type-check**

```bash
cd /Users/bytedance/mygit/rss-pal/frontend
npx tsc --noEmit
```

Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/BriefingWCardStrip.tsx
git commit -m "feat(ui): BriefingWCardStrip — 8-card horizontal week navigator"
```

---

## Task 10 — `WeeklyPage` rewrite

**Files:**
- Modify: `frontend/src/pages/WeeklyPage.tsx`

- [ ] **Step 1: Replace file content**

```tsx
import { useEffect, useState } from 'react'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'
import { getWeeklyDigest, WeeklyDigest } from '../api/client'
import ReadingMeta from '../components/ReadingMeta'
import BriefingTabs from '../components/BriefingTabs'
import BriefingWCardStrip from '../components/BriefingWCardStrip'
import { writeNav } from '../utils/articleNav'
import { toast } from '../utils/toast'

export default function WeeklyPage() {
  const [params] = useSearchParams()
  const navigate = useNavigate()

  const [week, setWeek] = useState<string | undefined>(() => params.get('week') ?? undefined)
  const [digest, setDigest] = useState<WeeklyDigest | null>(null)
  const [loading, setLoading] = useState(true)

  useEffect(() => { load(week) }, [week])

  // Sync week → URL for back-from-article.
  useEffect(() => {
    if (week === undefined && params.get('week') !== null) {
      navigate('/weekly', { replace: true })
    } else if (week !== undefined && params.get('week') !== week) {
      navigate('/weekly?week=' + week, { replace: true })
    }
  }, [week])

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

  const pickWeek = (ws: string) => setWeek(ws)

  const onClickArticle = () => {
    if (!digest) return
    writeNav(digest.articles.map(a => a.id), null)
    try {
      sessionStorage.setItem('articleEntryPath', '/weekly?week=' + digest.week_start)
    } catch { /* ignore */ }
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
      <BriefingWCardStrip currentWeekStart={digest.week_start} onPick={pickWeek} />
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 16 }}>
        <h2 style={{ margin: 0 }}>本周精选 · {digest.week_start}</h2>
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
                <Link
                  key={a.id}
                  to={`/articles/${a.id}`}
                  onClick={onClickArticle}
                  className="card"
                  style={{ display: 'block', textDecoration: 'none', color: 'inherit' }}
                >
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

```bash
cd /Users/bytedance/mygit/rss-pal/frontend
npx tsc --noEmit
```

Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/pages/WeeklyPage.tsx
git commit -m "feat(ui): WeeklyPage uses W-card strip + URL sync + writeNav"
```

---

## Task 11 — Manual integration

**Files:** none (manual ops).

- [ ] **Step 1: Rebuild api + frontend**

```bash
cd /Users/bytedance/mygit/rss-pal
docker compose up -d --build api frontend
```

Expected: containers come up, `/api/health` returns 200.

```bash
curl -s http://localhost:8080/api/health
```

Expected: `{"status":"ok"}`.

- [ ] **Step 2: Verify daily calendar**

In browser:
1. Open `/daily`. Confirm 📅 button in header, no prev/next/yesterday buttons.
2. Click 📅. Calendar popover shows current month. Today's cell is `--cal-today` color (typically same accent). 5/27 (which has a digest) is green. 5/28 / 5/26 (in pending window, no row) are orange. Older days with no row are grey.
3. Click 5/27. Page loads `/daily?date=2026-05-27`. Reload (`Cmd+R`) — still loads 5/27.
4. Click 📅 → click `‹` once. Month switches to April; status colors update from a fresh API call.
5. Click outside the popover → it closes. Click 📅 again → reopens.
6. Click any grey day → no-op.

- [ ] **Step 3: Verify weekly W-strip**

1. Open `/weekly`. Confirm a horizontal strip of 8 cards, no prev/next/this-week buttons.
2. Right-most card is this week, blue (`--cal-today`). 2nd from right is last week — orange if no row, green if generated. Older weeks with rows are green. Others grey.
3. Click any non-grey card → page loads that week. URL updates to `/weekly?week=YYYY-MM-DD`.
4. Reload page — still on that week.
5. Click a grey card → no-op.

- [ ] **Step 4: Verify article navigation context**

1. From `/daily?date=2026-05-27` click the first article. ArticlePage opens.
2. Click Next (→) — moves to article #2 of the 5 in the digest. No global pagination.
3. Click Next until end (5th). Next button should be disabled (no fetch context).
4. Click Back. Lands on `/daily?date=2026-05-27` (not `/articles`).
5. Repeat 1-4 with `/weekly` and any week.

- [ ] **Step 5: Push branch**

```bash
git push origin feature/daily-briefing
gh pr view 35 --json url
```

Expected: PR #35 has additional commits.

---

## Self-review notes

- **Spec coverage:**
  - §1 backend index endpoint → Tasks 1 (helper move), 2 (repo), 3 (handler), 4 (route).
  - §2 daily palette → Task 6 (CSS), Task 7 (calendar uses vars).
  - §3 DailyPage redesign → Task 8.
  - §4 BriefingCalendar → Task 7.
  - §5 WeeklyPage redesign → Task 10.
  - §6 BriefingWCardStrip → Task 9.
  - §7 article navigation context → embedded in Tasks 8 & 10 (`writeNav` + `articleEntryPath`).
  - §8 testing → Task 3 unit tests, Task 11 manual.

- **Placeholder scan:** Tasks contain exact file paths, exact code blocks, exact commands. No TBD / TODO / "handle edge cases".

- **Type consistency:** `BriefingIndex` shape matches between API (Task 3) and client (Task 5). `Status` enum is consistent across `BriefingCalendar` (Task 7) and `BriefingWCardStrip` (Task 9) — both use `'done' | 'pending' | 'disabled' | 'today'` (calendar has an extra `'future'` for out-of-month / future cells; W-strip never shows future weeks so doesn't need it). The `writeNav` signature matches what's already in `frontend/src/utils/articleNav.ts`.

- **Architectural notes:**
  - `MondayLabel` moves from worker → api package because the index endpoint needs it. Worker still uses it via `api.MondayLabel`. No circular dep — worker already imports `api` for `TodayLabel` / `DailyWindow`.
  - The frontend index fetch covers a small "bleed" (1 month for calendar, 1 week for W-strip) so adjacent navigation feels snappy without hammering the endpoint.
  - URL ↔ state sync uses `replace: true` so the back button doesn't accumulate intermediate states (only meaningful navigations to/from articles count).
