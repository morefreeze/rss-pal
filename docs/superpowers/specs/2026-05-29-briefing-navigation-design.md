# Briefing Navigation UX Design

> Status: design — pending review
> Date: 2026-05-29
> Related: `docs/superpowers/specs/2026-05-29-daily-briefing-design.md`
> (builds on the daily briefing feature; this spec only adds navigation UX)

## Goal

Replace the prev/next button strip on `/daily` and `/weekly` with richer entry pickers, and make article-detail navigation context-aware so prev/next/back stay inside the simplified set the user came from.

Three coordinated changes:

1. **Calendar picker on `/daily`** — a single 📅 button opens a month-grid popover with each day color-coded by status (done / pending / disabled / today).
2. **W-card strip on `/weekly`** — horizontal scroll of 8 weekly cards, each showing month-relative week number and date range, color-coded with the same palette.
3. **Article navigation context** — opening an article from `/daily` or `/weekly` snapshots that page's 5 / 10 article IDs into the existing nav-list mechanism and sets the back target to the briefing page (with the specific date / week preserved in the URL).

## Non-goals

- No on-demand backfill for old dates. Grey cells / cards are read-only; the worker is the only writer.
- No new worker scheduling. The auto-generation window stays at "yesterday daily + last-Monday weekly + 3-day startup catch-up."
- No change to the briefing article list rendering, intro card, or article cards themselves.
- No mobile-only or desktop-only treatment; the same layout adapts via existing CSS vars.
- No "重新生成" / "regenerate this digest" UI. That belongs in a separate spec if it ever lands.

## Status palette (shared by daily & weekly)

| State | Color | Daily meaning | Weekly meaning |
|---|---|---|---|
| Done | 🟢 `--cal-done` | `daily_digests` row exists | `weekly_digests` row exists |
| Pending | 🟠 `--cal-pending` | No row, `today_label - 3 ≤ D < today_label` | No row, `D == 上周一` (last Monday) |
| Disabled | ⚪ `--cal-disabled` | No row, `D < today_label - 3` | No row, `D < 上周一` |
| Today / In-progress | 🔵 `--cal-today` | `D == today_label` | `D == 本周周一` |

Pending derives from the worker's auto-schedule window (last 3 days for daily startup catch-up; last week for weekly Monday cron). Anything outside that window is "abandoned" and grey-disabled.

Disabled cells/cards are non-interactive: `aria-disabled="true"`, `pointer-events: none`, dimmed by `--cal-disabled` (≈ 30 % opacity of `--fg`).

CSS vars added to `frontend/src/styles/theme.css` or wherever the existing palette lives. Light/dark each get their own definitions; the four colors should be distinguishable in both modes.

## Backend — shared index endpoint

Single endpoint covers both modes, keeps the surface small:

```
GET /api/briefing/index?type=daily&from=YYYY-MM-DD&to=YYYY-MM-DD
GET /api/briefing/index?type=weekly&from=YYYY-MM-DD&to=YYYY-MM-DD
```

`from` and `to` are inclusive, in Asia/Shanghai. `from` must be ≤ `to` and the span ≤ 400 days (server-side cap to avoid pathological scans). Outside range → 400.

Response (daily):
```json
{
  "type": "daily",
  "today_label": "2026-05-28",
  "pending_window_start": "2026-05-26",
  "cached": ["2026-05-27", "2026-05-21", "2026-05-15"]
}
```

Response (weekly):
```json
{
  "type": "weekly",
  "this_week_start": "2026-05-25",
  "pending_window_start": "2026-05-18",
  "cached": ["2026-05-18", "2026-05-11", "2026-04-27"]
}
```

- `today_label` for daily, `this_week_start` for weekly: lets the frontend mark the 🔵 cell without re-deriving boundary logic client-side.
- `pending_window_start` is inclusive lower bound for the 🟠 state. Daily = `today_label - 3`; weekly = `today_label`'s Monday `- 7` (i.e. "last week").
- `cached` is the list of dates / week_starts the user already has digests for. Frontend cross-references to mark 🟢.

State derivation (client):
```ts
function dailyStatus(d: string, idx: DailyIndex): Status {
  if (d > idx.today_label) return 'future_disabled' // future days dropped from UI anyway
  if (d === idx.today_label) return 'today'
  if (idx.cached.includes(d)) return 'done'
  if (d >= idx.pending_window_start) return 'pending'
  return 'disabled'
}
```

### Repository changes

Add to `DailyDigestRepository`:
```go
// ListDaysInRange returns the day_start values for this user with
// from ≤ day_start ≤ to. Ordered ascending.
func (r *DailyDigestRepository) ListDaysInRange(userID int, from, to time.Time) ([]time.Time, error)
```
SQL:
```sql
SELECT day_start FROM daily_digests
WHERE user_id = $1 AND day_start BETWEEN $2 AND $3
ORDER BY day_start
```

Add to `WeeklyDigestRepository`:
```go
func (r *WeeklyDigestRepository) ListWeeksInRange(userID int, from, to time.Time) ([]time.Time, error)
```
Same shape against `weekly_digests.week_start`.

### Handler

New file `backend/internal/api/briefing_index.go`:

```go
type BriefingIndexHandler struct {
    dailyRepo  *repository.DailyDigestRepository
    weeklyRepo *repository.WeeklyDigestRepository
}

func NewBriefingIndexHandler(dailyRepo *repository.DailyDigestRepository, weeklyRepo *repository.WeeklyDigestRepository) *BriefingIndexHandler

// Get serves GET /api/briefing/index
func (h *BriefingIndexHandler) Get(c *gin.Context)
```

Logic:
1. Parse `type` ∈ `{daily, weekly}`; else 400.
2. Parse `from`, `to` via `ParseDailyDate` (reuse from daily.go); enforce `from ≤ to`, span `≤ 400 days`.
3. Daily branch:
   - `today_label := TodayLabel(time.Now())`
   - `pending_window_start := today_label.AddDate(0, 0, -3)`
   - `days := dailyRepo.ListDaysInRange(userID, from, to)`
   - format `days` as `[]string` (YYYY-MM-DD)
   - return JSON
4. Weekly branch:
   - `this_week_start := mondayShanghai(time.Now())` — extract `mondayShanghai` to `internal/api/daily.go` or a small helper since it lives in worker now
   - `pending_window_start := this_week_start.AddDate(0, 0, -7)`
   - `weeks := weeklyRepo.ListWeeksInRange(userID, from, to)`
   - format + return

`mondayShanghai`: currently in `cmd/worker/briefing.go`. Refactor to `internal/api/daily.go` (rename `MondayLabel(now)` to match `TodayLabel(now)` style; both move to a tiny "briefing time" section), and worker imports the api package's version. The worker already imports `internal/api` for `TodayLabel` so this introduces no new edge.

Route wiring in `cmd/server/main.go`:
```go
briefingIndexHandler := api.NewBriefingIndexHandler(dailyDigestRepo, weeklyDigestRepo)
apiGroup.GET("/briefing/index", briefingIndexHandler.Get)
```

### Tests

`internal/api/briefing_index_test.go`:
- Parse-level tests via helpers: `parseTypeParam`, `parseFromTo` (extracted as pure functions).
- Manual integration check captured in implementation plan's Task X.

## Frontend — DailyPage

### Header

Current:
```
本日精选 · 2026-05-27          [‹ 前一天] [后一天 ›] [昨天]
```

Becomes:
```
本日精选 · 2026-05-27                         [📅]
```

(Live-mode header `今日精选 · 2026-05-29（收集中）` unchanged.)

### Popover state

```tsx
const [calOpen, setCalOpen] = useState(false)
const [calMonth, setCalMonth] = useState<string>(/* shown_date's YYYY-MM */)
```

`📅` button is `aria-expanded={calOpen}` and toggles state.

Popover positioned absolute, anchored to the 📅 button. Click outside (any mousedown outside the popover root) → close. ESC → close. Picking a date → `setDate(picked)` + close.

### Layout & date math

`shiftDay` and the `前一天/后一天/昨天` buttons are removed. The "stale tag" (`requested_date 还在生成中`) keeps working since it's part of the pending-fallback rendering, not the buttons.

## Frontend — BriefingCalendar component

File: `frontend/src/components/BriefingCalendar.tsx`

```ts
interface Props {
  currentDate: string         // YYYY-MM-DD — the digest's shown_date
  onPick: (date: string) => void
  onClose: () => void
}
```

### Layout

- Header row: `‹` month-prev / `{year} 年 {month} 月` / `›` month-next / `✕`
- Day-of-week strip: `一 二 三 四 五 六 日` (Monday-first, matching China convention)
- Grid: 6 rows × 7 cols of date cells. Cells outside the displayed month are dim grey placeholders (no interactivity, no status color).
- Status color applies to in-month cells only.

### Cell rendering

Each cell:
```tsx
<button
  className="cal-cell"
  data-status={status}      // done | pending | disabled | today
  aria-disabled={status === 'disabled'}
  disabled={status === 'disabled'}
  onClick={() => status !== 'disabled' && onPick(d)}
>
  {dayNumber}
  {d === currentDate && <span className="dot" />}
</button>
```

CSS:
```css
.cal-cell[data-status="done"]    { background: var(--cal-done); color: #fff; }
.cal-cell[data-status="pending"] { background: var(--cal-pending); color: #fff; }
.cal-cell[data-status="disabled"]{ background: var(--cal-disabled); color: var(--fg); opacity: .35; cursor: not-allowed; }
.cal-cell[data-status="today"]   { background: var(--cal-today); color: #fff; }
.cal-cell .dot                   { /* small indicator on current selection */ }
```

### Data loading

On mount and on `calMonth` change:
```ts
useEffect(() => {
  const from = firstOfMonth(calMonth)
  const to = lastOfMonth(calMonth)
  getBriefingIndex('daily', from, to).then(setIndex)
}, [calMonth])
```

While loading: cells render uncolored (just numbers); status applies once `index` resolves.

Month switch never auto-closes the popover.

### API client

`frontend/src/api/client.ts`:
```ts
export interface BriefingIndex {
  type: 'daily' | 'weekly'
  today_label?: string          // present when type='daily'
  this_week_start?: string      // present when type='weekly'
  pending_window_start: string
  cached: string[]
}

export const getBriefingIndex = (type: 'daily' | 'weekly', from: string, to: string) =>
  api.get<BriefingIndex>('/briefing/index', { params: { type, from, to } }).then(r => r.data)
```

## Frontend — WeeklyPage

### Header

Replace the existing prev/next/this-week button row with a `<BriefingWCardStrip>`. Title row `本周精选 · {week_start}` stays.

```
[W3 ◯  W2 ●  W1 ◉  W5(4月) ◯ ...]      ← horizontal scroll, latest on right, selected highlighted
本周精选 · 2026-05-25
{intro card}
{article cards}
```

(Pending placeholder render `周报生成中,稍后刷新…` unchanged.)

## Frontend — BriefingWCardStrip component

File: `frontend/src/components/BriefingWCardStrip.tsx`

```ts
interface Props {
  currentWeekStart: string   // YYYY-MM-DD (Monday Asia/Shanghai)
  onPick: (weekStart: string) => void
}
```

### Week enumeration

- Compute 8 consecutive Mondays ending at `this_week_start` (inclusive). E.g. if today is 2026-05-29 (Friday), `this_week_start = 2026-05-25`; the strip shows weeks starting `2026-04-06, 04-13, 04-20, 04-27, 05-04, 05-11, 05-18, 05-25`.
- Cards rendered left → right, oldest → newest. Strip scrolls horizontally; initial scroll position = right-most card visible.

### Month-relative W number

For a `weekStart` Monday:
- Let `M` be its month. Enumerate Mondays of `M` in calendar order: `M_first, M_second, ...`.
- `W{n}` = its position in that list.
- Example: 2026-05-04 is the first Monday of May → W1. 2026-05-25 is the fourth → W4. 2026-04-27 is the last Monday of April → W{whatever it is in April}.
- Edge case: if `weekStart` itself is in the previous month (very rare since we anchor to Monday), still use its Monday's month. Not relevant in practice.

Helper:
```ts
function monthRelativeWeekNumber(weekStart: string): number {
  const d = new Date(weekStart + 'T00:00:00+08:00')
  const firstMondayOfMonth = /* find first Monday of d.getMonth() */
  return Math.floor((d.getTime() - firstMondayOfMonth.getTime()) / (7 * 86400 * 1000)) + 1
}
```

### Card layout

```
┌──────────┐
│   W3     │   ← 18px bold
│05.18-05.24│  ← 11px muted
└──────────┘
```

Width: 88px each. Color via `data-status` mirrors calendar cells. Selected card: extra `border: 2px solid var(--accent)`.

```tsx
<button
  data-status={status}
  aria-current={ws === currentWeekStart ? 'true' : undefined}
  aria-disabled={status === 'disabled'}
  disabled={status === 'disabled'}
  onClick={() => status !== 'disabled' && onPick(ws)}
>
  <div className="w-num">W{n}</div>
  <div className="w-range">{rangeText(ws)}</div>
</button>
```

### Data loading

```ts
useEffect(() => {
  const from = first of the 8 weeks
  const to = today + 1
  getBriefingIndex('weekly', from, to).then(setIndex)
}, [currentWeekStart])
```

Range text: `MM.DD-MM.DD` (e.g. `05.18-05.24`).

## Article navigation context

Two minimal edits, leveraging the existing `articleNav` snapshot:

### `frontend/src/pages/DailyPage.tsx`

Add a helper:
```tsx
import { writeNav } from '../utils/articleNav'

const onClickArticle = () => {
  if (!digest) return
  writeNav(digest.articles.map(a => a.id), null)
  try {
    sessionStorage.setItem('articleEntryPath', `/daily?date=${digest.shown_date}`)
  } catch { /* ignore */ }
}
```

Then in the article-list render:
```tsx
<Link key={a.id} to={`/articles/${a.id}`}
      onClick={onClickArticle}
      className="card" ...>
```

`navContext` is `null` because the daily list is bounded (max 5 articles, no pagination).

### `frontend/src/pages/WeeklyPage.tsx`

Mirror:
```tsx
const onClickArticle = () => {
  if (!digest) return
  writeNav(digest.articles.map(a => a.id), null)
  try {
    sessionStorage.setItem('articleEntryPath', `/weekly?week=${digest.week_start}`)
  } catch { /* ignore */ }
}
```

### `ArticlePage` — no code change

Existing logic at `ArticlePage.tsx:50-54`:
```tsx
const entryPath =
  (location.state as { from?: string } | null)?.from
  ?? (() => { try { return sessionStorage.getItem('articleEntryPath') } catch { return null } })()
  ?? '/articles'
```

When user clicks back, `navigate(entryPath)` lands on `/daily?date=2026-05-27` (or `/weekly?week=...`), and DailyPage/WeeklyPage's existing `useEffect(() => load(date), [date])` picks up the query and renders the exact briefing. Symmetric round-trip.

### Daily/Weekly route + query handling

DailyPage and WeeklyPage currently read `date` / `week` only from React state, not from URL. Update them to read the initial value from URL:

```tsx
// DailyPage.tsx
import { useSearchParams } from 'react-router-dom'

const [params] = useSearchParams()
const [date, setDate] = useState<string | undefined>(() => params.get('date') ?? undefined)
```

And when `setDate(d)` is called (from the calendar pick), also reflect to URL:
```tsx
const navigate = useNavigate()
const pickDate = (d: string) => {
  setDate(d)
  navigate(`/daily?date=${d}`, { replace: true })
}
```

(Same pattern for `setDate(undefined)` → `navigate('/daily', { replace: true })`. WeeklyPage same with `week` param.)

This way back-from-article lands on the right date even after a hard reload.

## Edge cases

- **Popover positioning on narrow viewports** — `position: absolute` anchored to the button with `max-width: calc(100vw - 16px)`; CSS clamps. If clipped at right, transform-origin shifts to top-right.
- **Calendar's currentDate not in `calMonth`** — when user picks a date in a different month and re-opens, popover opens to current `shown_date`'s month, not the previously viewed month.
- **`shown_date != requested_date`** (stale fallback rendering daily) — calendar `currentDate` is `digest.shown_date` (what's actually rendered), so the dot lands on the shown date, not the requested one.
- **W-card spans a month boundary** — already handled; we anchor to `weekStart`'s month. A week that starts April 27 and ends May 3 is W{n of April}.
- **Tab focus order** — calendar popover sets `autoFocus` on the current-date cell; W-card strip leaves natural left-to-right tab order.
- **First-visit user with zero digests** — `cached` empty; only 🔵 today / 🟠 pending-window cells distinguishable; everything else grey. Acceptable.

## Files changed

### Backend — create
- `backend/internal/api/briefing_index.go`
- `backend/internal/api/briefing_index_test.go`

### Backend — modify
- `backend/internal/repository/daily_digest.go` — add `ListDaysInRange`
- `backend/internal/repository/weekly_digest.go` — add `ListWeeksInRange`
- `backend/internal/api/daily.go` — promote `mondayShanghai` from worker (rename to keep symmetry with `TodayLabel`)
- `backend/cmd/worker/briefing.go` — replace local `mondayShanghai` with the api package's
- `backend/cmd/server/main.go` — construct and route `BriefingIndexHandler`

### Frontend — create
- `frontend/src/components/BriefingCalendar.tsx`
- `frontend/src/components/BriefingWCardStrip.tsx`

### Frontend — modify
- `frontend/src/api/client.ts` — `BriefingIndex` type + `getBriefingIndex` fn
- `frontend/src/pages/DailyPage.tsx` — drop prev/next buttons, add 📅 + popover, URL query sync, writeNav on article click
- `frontend/src/pages/WeeklyPage.tsx` — replace prev/next with W-card strip, URL query sync, writeNav on article click
- `frontend/src/styles/` (whichever file holds the palette CSS vars) — add `--cal-done`, `--cal-pending`, `--cal-disabled`, `--cal-today` for both themes

## Testing strategy

### Unit (Go)
- `briefing_index_test.go` — type/from/to validation, span-cap, response shape per type. Use httptest + the existing gin setup pattern (see `daily_test.go` for in-process testing of helpers; the full handler can be exercised via a minimal in-test gin engine).
- Existing daily/weekly tests still pass after `mondayShanghai` moves from worker → api package.

### Type-check (TS)
- `cd frontend && npx tsc --noEmit` after each task.

### Manual integration (executed in plan's final task)
1. Apply nothing (no migration). Rebuild `api`, `frontend`.
2. Open `/daily`. Click 📅. Calendar shows current month with today 🔵, yesterday's failed dates 🟠, 5/27 🟢.
3. Click 5/27 in calendar → page loads 5/27 digest. URL becomes `/daily?date=2026-05-27`. Browser reload still loads 5/27.
4. Click an article → ArticlePage. Prev/Next iterate only the 5 articles. Back button → `/daily?date=2026-05-27`.
5. Repeat 2-4 with `/weekly` + W-cards.
6. Pick a grey card / cell → no-op (cursor not-allowed).
7. Resize browser to ~400px wide; popover doesn't clip off-screen.

## Open questions

None at design time.

## Migration / rollout notes

- Pure additive change, no DB migration.
- Frontend changes require Docker rebuild (per memory note).
- `mondayShanghai` move: same-package symbol relocation. Worker depends on api package (already does via `TodayLabel`); no new circular risk.
- If the index endpoint somehow returns 500, calendar / W-strip gracefully renders all cells uncolored; users can still navigate via direct URL or by typing a date.
