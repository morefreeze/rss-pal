# Daily Briefing (日报) Design

> Status: design — pending review
> Date: 2026-05-29
> Related: `docs/superpowers/specs/2026-05-06-bestblogs-inspired-features-design.md`
> (the spec that introduced the weekly digest this feature mirrors)

## Goal

Add a personal "daily briefing" (日报) modeled after the existing weekly digest (周刊). One AI call per user per day curates **5 articles + an 80–120 字 Chinese intro** from the last 24-hour window. Worker pre-generates in the background; the UI reads cache only.

While shipping the daily, also migrate the existing weekly digest to the same worker-driven async generation model so both surfaces behave consistently and the first page-open never blocks on AI.

The motivating UX problem: weekly digest's first GET currently blocks 5-10 s while the AI summarizer runs in the request handler (`api/weekly.go:81-99`). For a daily-cadence surface that delay would compound — the worker-async model fixes it for both.

## Non-goals

- Email / push delivery of the daily briefing. UI-only consumption.
- Cross-device sync of UI preferences beyond the single `briefing_last_tab` bit.
- Monthly briefing or other cadences. If a third cadence is added later, that is the trigger to extract a generic `PeriodicDigest` abstraction — not now.
- Per-feed daily reports.

## Time window definition

- Timezone: `Asia/Shanghai` (reuse `var shanghai = time.FixedZone("Asia/Shanghai", 8*3600)` from `api/weekly.go`).
- A "day D" briefing window = `[D 05:00, D+1 05:00)` Asia/Shanghai. `D` is a `DATE`.
- `today_label(now)`:
  - `t := now.In(shanghai)`
  - if `t.Hour() < 5` → `today_label = t.Date - 1` (pre-5am still belongs to yesterday's window)
  - else → `today_label = t.Date`
- Default GET (no `date` query param) returns `today_label - 1` — the most recently *completed* window.
- The in-progress current window is `today_label`; UI labels it `今天（收集中）` and handles it via the "live" branch (see §3).

## Architecture overview

```
                  ┌──────────────────────────────────────────────┐
                  │ worker briefingScheduler (new goroutine)    │
                  │  - 05:00 daily   → enqueue D-1 daily/user    │
                  │  - Mon 05:00     → enqueue last-week/user    │
                  │  - on startup    → catch-up: last 3d daily,  │
                  │                    last 1w weekly per user   │
                  └────────────────┬─────────────────────────────┘
                                   │
                                   ▼ (one AI call per item, throttled
                                      by existing aiSemaphore = 2)
                  ┌──────────────────────────────────────────────┐
                  │ ai.GenerateDailyDigest(candidates) →         │
                  │   (picks []int, intro string, err error)     │
                  │ ai.GenerateWeeklyIntro(...) — unchanged      │
                  └────────────────┬─────────────────────────────┘
                                   │
                                   ▼ upsert
                  ┌──────────────────────────────────────────────┐
                  │ daily_digests / weekly_digests tables        │
                  └────────────────┬─────────────────────────────┘
                                   ▲
                                   │ read-only
       ┌───────────────────────────┴────────────────────────────┐
       │ GET /api/daily-digest?date=…                          │
       │ GET /api/weekly-digest?week=…  (now read-only)        │
       │ GET/POST /api/briefing/last-tab                       │
       └────────────────────────────────────────────────────────┘
                                   ▲
                                   │
                  Frontend: /daily, /weekly, /briefing redirect
```

## Database changes — `031_daily_briefing.sql`

```sql
-- Daily briefing cache (mirrors weekly_digests)
CREATE TABLE IF NOT EXISTS daily_digests (
    id SERIAL PRIMARY KEY,
    user_id INT REFERENCES users(id) ON DELETE CASCADE,
    day_start DATE NOT NULL,                -- D, the window's first calendar date in Asia/Shanghai
    intro_text TEXT NOT NULL,
    article_ids INTEGER[] NOT NULL,         -- AI-picked 5 article ids, in display order
    generated_at TIMESTAMP DEFAULT NOW(),
    UNIQUE(user_id, day_start)
);
CREATE INDEX IF NOT EXISTS idx_daily_digests_user_day
    ON daily_digests(user_id, day_start DESC);

-- Last-viewed briefing tab; drives the /briefing redirect entry.
ALTER TABLE users
    ADD COLUMN IF NOT EXISTS briefing_last_tab VARCHAR(10) DEFAULT 'daily';
```

Per memory note: this migration must be applied manually on existing DBs (`docker-entrypoint-initdb.d` only runs on empty volumes). Run `docker compose exec -T postgres psql -U postgres -d rsspal < backend/migrations/031_daily_briefing.sql` after deploy.

## AI generation

### Single-call contract

New file `internal/ai/daily_digest.go`:

```go
type DailyCandidate struct {
    Idx          int    // 0-based index in the candidate slice
    Title        string
    SummaryBrief string
}

// GenerateDailyDigest picks N articles (N = min(5, len(candidates))) and writes
// an 80-120 字 Chinese intro. Returns (picks, intro, err). picks is N
// 0-based indices into candidates, in display order, with no duplicates.
// On any parse/validation failure, returns err — caller must NOT cache.
func (s *Summarizer) GenerateDailyDigest(ctx context.Context, candidates []DailyCandidate) (picks []int, intro string, err error)
```

### Prompt

```
以下是过去 24 小时按个性化推荐分数挑出的 {N_candidates} 篇候选文章：

[0] 《标题》
    摘要：...

[1] 《标题》
    摘要：...

...

[19] 《标题》
    摘要：...

请从中精选 {N_pick} 篇组成「今日精选日报」，并写一段 80-120 字的中文导语，回答这个问题：
「为什么这 {N_pick} 篇值得今天读？这些文章共同指向什么趋势或思考？」

要求：
- 严格输出 JSON，不要 Markdown 代码块、不要任何包裹文字：
  {"picks":[i,j,k,l,m],"intro":"..."}
- picks 是 {N_pick} 个互不相同的 0-{N_candidates-1} 整数下标，按推荐顺序排列。
- intro 80-120 字（中文字符数），从候选中提炼共同主题、张力或对比；不要逐篇复述；不要 Markdown、不要分点列表；语气专业、克制。
```

`{N_pick} = min(5, N_candidates)`. If `N_candidates == 0`, skip the AI call entirely and do not write a cache row (UI will surface `当日无候选文章` for that date).

### Parsing & validation

1. `strings.TrimSpace`, strip a leading `\`\`\`json` / trailing `\`\`\`` if present (cheap defense; we don't promise the model never adds them).
2. `json.Unmarshal` into `struct { Picks []int; Intro string }`.
3. Validate:
   - `len(Picks) == N_pick`
   - All `0 ≤ p < N_candidates`, no duplicates
   - `100 ≤ utf8.RuneCountInString(Intro) ≤ 250` (one bound looser than the prompted 80–120 to absorb minor drift)
4. Any failure → return wrapped error like `fmt.Errorf("daily digest parse: %w", err)`. Caller logs and skips upsert. The next scheduled tick or the startup catch-up will retry.

Reuse `Summarizer.call(ctx, prompt, maxTokens=800)`. Existing retry / timeout / `AI_VISION_*` plumbing is untouched.

### Tests (`internal/ai/daily_digest_test.go`)

Table-driven over a fake `Summarizer.call` (intercept via interface or test seam already used by other summarizer tests):

- valid response → picks/intro returned
- picks length wrong, duplicate index, out-of-range index → error, no panic
- intro too short / too long → error
- JSON wrapped in `\`\`\`json` fence → still parses
- malformed JSON → error
- `N_candidates = 3` → prompt asks for 3, response with 3 picks passes
- `N_candidates = 0` → function returns `(nil, "", nil)` without calling the model

## Backend Repository

New file `internal/repository/daily_digest.go`, structure mirrors `weekly_digest.go`:

```go
type DailyDigest struct {
    UserID      int
    DayStart    time.Time   // 00:00 on D in Asia/Shanghai (DB stores DATE, scanned as time.Time)
    IntroText   string
    ArticleIDs  []int64
    GeneratedAt time.Time
}

func NewDailyDigestRepository(db *sql.DB) *DailyDigestRepository
func (r *DailyDigestRepository) Get(userID int, day time.Time) (*DailyDigest, error)
func (r *DailyDigestRepository) Upsert(userID int, day time.Time, intro string, articleIDs []int) error

// Catch-up support — return user IDs missing a daily for `day`.
func (r *DailyDigestRepository) UserIDsMissing(day time.Time) ([]int, error)
```

`UserIDsMissing` does `SELECT id FROM users WHERE id NOT IN (SELECT user_id FROM daily_digests WHERE day_start = $1)`. Mirror the same method on `WeeklyDigestRepository`.

Candidate fetch reuses `ArticleRepository.GetTopArticlesInRange(userID, start, end, 20)` — already exists, no new repo method needed.

Tests:
- `internal/repository/daily_digest_test.go` — upsert idempotency, `Get` not-found, `UserIDsMissing`.
- Use the same test harness pattern as `weekly_digest`-adjacent tests; reuse the existing test DB plumbing.

## Backend API

### `GET /api/daily-digest`

Query: `?date=YYYY-MM-DD` (Asia/Shanghai). Optional.

Handler in `internal/api/daily.go`:

```
1. requested := parseDate(c.Query("date"))
   if absent: requested = today_label - 1
2. if requested > today_label OR requested < today_label - 30:
       400 "date 超出范围"
3. if requested == today_label:
     // live branch — no cache, no AI, no row written
     start := time.Date(D, 5, 0, 0, 0, 0, shanghai)
     end   := time.Now()
     arts  := articleRepo.GetTopArticlesInRange(userID, start, end, 5)
     return {requested_date: D, shown_date: D, pending: false,
             intro_text: "", articles: arts, mode: "live"}
4. // requested < today_label — try cache, with one-day fallback
   cached := dailyRepo.Get(userID, requested)
   if cached != nil:
       return assemble(requested, requested, false, cached)
   fallbackDay := requested - 1
   if fallbackDay >= today_label - 30:
       fb := dailyRepo.Get(userID, fallbackDay)
       if fb != nil:
           return assemble(requested, fallbackDay, true, fb)
   return {requested_date: requested, shown_date: requested,
           pending: true, intro_text: "", articles: []}
```

`assemble` loads the snapshot's articles via `articleRepo.GetByIDsForUser` (same as weekly handler) and returns the JSON body below.

Response body:

```json
{
  "requested_date": "2026-05-28",
  "shown_date": "2026-05-28",
  "pending": false,
  "intro_text": "…",
  "articles": [/* model.Article, in cached id order */],
  "mode": "cached"
}
```

`mode` enum:
- `"cached"` — read from `daily_digests` (the requested day or a one-day fallback).
- `"live"` — `requested == today_label`; in-progress window, articles are SQL-ranked at request time, no row written, no AI.
- `"pending"` — neither requested nor fallback day is cached yet; `articles: []`, the worker will fill it on the next tick.

`pending` (boolean) is `true` when `mode == "pending"` **or** when `mode == "cached" && shown_date != requested_date` (a stale fallback). The frontend renders the right-aligned tag `{requested_date} 还在生成中` when `pending && shown_date != requested_date`, and a full-page placeholder when `pending && articles empty`.

### `GET /api/briefing/last-tab` and `POST /api/briefing/last-tab`

Tiny user-preference endpoints — schema-checked enum `daily | weekly`:

- `GET` → `{tab: "daily"}` (reads `users.briefing_last_tab`).
- `POST` body `{tab: "daily"}` → writes the column; 400 on other values.

Lives in `internal/api/briefing.go`. Single repo method on `UserRepository`: `GetBriefingLastTab(userID) (string, error)` and `SetBriefingLastTab(userID, tab string) error`.

### Wire-up (`cmd/server/main.go`)

- Construct `dailyDigestRepo := repository.NewDailyDigestRepository(db)`.
- Construct `dailyHandler := api.NewDailyHandler(articleRepo, dailyDigestRepo)`.
- Construct `briefingHandler := api.NewBriefingHandler(userRepo)`.
- New routes inside the existing `apiGroup` (auth-required):
  - `GET /daily-digest`
  - `GET /briefing/last-tab`
  - `POST /briefing/last-tab`

### Tests (`internal/api/daily_test.go`)

Mirror the style of existing `api/*_test.go`:

- no `date` param → `requested_date == today_label - 1`
- `date == today_label` → live mode, no cache row read or written, `mode: "live"`
- `date < today_label`, cache hit → cached payload
- `date < today_label`, miss + miss-1 → `pending=true`, `articles: []`
- `date < today_label`, miss but `date-1` cached → `shown_date == date-1`, `pending=true`
- `date > today_label` → 400
- `date < today_label - 30` → 400

## Worker — `briefingScheduler`

In `cmd/worker/main.go` start a dedicated goroutine `briefingScheduler` alongside the existing RSS poll loop, before the main loop blocks.

Pseudocode:

```go
func runBriefingScheduler(ctx, deps) {
    runCatchUp(ctx, deps)   // §"Startup catch-up" below

    ticker := time.NewTicker(60 * time.Second)
    defer ticker.Stop()

    var lastFireDaily, lastFireWeekly time.Time
    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            now := time.Now().In(shanghai)
            // Daily: first tick in the 05:00–05:59 window per calendar day
            if now.Hour() == 5 && !sameYMD(lastFireDaily, now) {
                fireDaily(ctx, deps, today_label(now)-1)   // D-1 just completed
                lastFireDaily = now
            }
            // Weekly: first tick in 05:00–05:59 on Monday per calendar week
            if now.Weekday() == time.Monday && now.Hour() == 5 && !sameYMD(lastFireWeekly, now) {
                fireWeekly(ctx, deps, mondayOf(now)-7d)
                lastFireWeekly = now
            }
        }
    }
}
```

`sameYMD` short-circuits restarts within the same hour from re-firing. Per-user idempotency is enforced at the DB layer via the `UNIQUE(user_id, day_start)` / `UNIQUE(user_id, week_start)` constraints plus a pre-check in `fireDaily`/`fireWeekly`.

`fireDaily(ctx, deps, D)`:
1. Iterate `dailyRepo.UserIDsMissing(D)` (so already-generated users are skipped — handles partial-failure restarts cleanly).
2. For each user, acquire `aiSemaphore` (existing 2-slot semaphore in worker).
3. Query candidates: `articleRepo.GetTopArticlesInRange(userID, D 05:00, D+1 05:00, 20)`.
4. If `len(candidates) == 0` → log `briefing.daily.skip user=N day=D reason=no_candidates`, do nothing (no row, will retry next catch-up).
5. Call `ai.GenerateDailyDigest`. On error → log, no row.
6. Map `picks` → `[]int` of `article.ID`, in pick order. `dailyRepo.Upsert(userID, D, intro, ids)`.
7. Release semaphore.

`fireWeekly(ctx, deps, weekStart)` is structurally identical but calls `summarizer.GenerateWeeklyIntro` and writes to `weekly_digests`. The 10-article cap and candidate selection (SQL-only top 10) are unchanged from current `api/weekly.go`.

### Startup catch-up

Right after worker boot (before entering the main poll loop):

```go
// Daily: for each of the last 3 completed days, fire any missing
for k := 1; k <= 3; k++ {
    fireDaily(ctx, deps, today_label(time.Now()) - k)
}
// Weekly: the last completed week
fireWeekly(ctx, deps, mondayOf(time.Now()) - 7d)
```

Bounds the recovery work to a finite window. If the worker has been down longer than that, older days are silently skipped — acceptable for a personal tool.

### Tests

Worker scheduling is hard to unit test deterministically. Approach:

- Extract `fireDaily` and `fireWeekly` into testable functions that take a `now time.Time` parameter and accept injected `summarizer` / repos.
- Unit test `fireDaily` and `fireWeekly` against an in-memory or test DB, mocking `Summarizer.call`. Verify upsert, no-write on AI error, no-write on empty candidates.
- The scheduler tick loop itself stays uncovered — its only logic is `if hour == 5 && !alreadyFired then fire`, which is trivial.

## Weekly digest migration to async

Goal: `GET /api/weekly-digest` becomes a pure read of `weekly_digests`. Worker is the only writer.

Changes to `api/weekly.go`:

- Drop the `summarizer` field on `WeeklyHandler` and the constructor parameter.
- Delete lines 81–99 (the inline AI generation block).
- When cache miss: return `pending=true`, `articles: []`, `intro_text: ""`. (Worker will fill it on next 05:00 Monday tick or via startup catch-up.)
- Add `pending` field to the JSON response. Keep `week_start`, `intro_text`, `articles` unchanged for backward compat.

Changes to `cmd/server/main.go`: drop the `summarizer` arg from `NewWeeklyHandler`.

Frontend `WeeklyPage.tsx`: read `pending` (default `false`); if `pending && articles empty`, show placeholder `周报生成中,稍后刷新…`. Existing rendering otherwise unchanged.

Existing weekly tests in `internal/api/` (if any reference the inline AI path) need updating to the read-only contract. Audit and update during implementation.

## Frontend

### Files

- New: `src/pages/DailyPage.tsx`
- New: `src/components/BriefingTabs.tsx` (shared by daily + weekly pages)
- New: `src/components/BriefingRedirect.tsx` (route handler for `/briefing`)
- Edit: `src/pages/WeeklyPage.tsx` (read `pending`, embed `BriefingTabs`)
- Edit: `src/App.tsx` (add `/daily` and `/briefing` routes)
- Edit: `src/components/Layout.tsx` (replace `周刊` nav item with `简报`)
- Edit: `src/components/MoreSheet.tsx` (remove `周刊` item)
- Edit: `src/api/client.ts` (add daily + briefing-tab APIs; add `pending` to `WeeklyDigest`)

### `DailyPage.tsx` (sketch)

- State: `digest: DailyDigest | null`, `date?: string`, `loading`.
- On mount + on `date` change: `getDailyDigest(date)`.
- Header row mirrors WeeklyPage: title `本日精选 · {shown_date}` + buttons `‹ 前一天 / 后一天 ›` + `回到昨天` reset.
- "Pending" tag (top-right of header): when `pending && shown_date != requested_date`, show `{requested_date} 还在生成中` in muted styling.
- Live mode (`mode === "live"`): top banner `今日还在收集中,明早 5 点后生成正式日报`.
- Empty-pending (`pending && articles empty`): full-card placeholder `日报生成中,稍后刷新…`.
- Article list: identical card markup to WeeklyPage.
- Bound the back-arrow: disable when `shown_date <= today_label - 30`.

### `BriefingTabs.tsx`

```tsx
<div role="tablist" className="briefing-tabs">
  <Link to="/daily"  ... aria-selected={current === 'daily'}  >日报</Link>
  <Link to="/weekly" ... aria-selected={current === 'weekly'} >周报</Link>
</div>
```

`onClick` for each tab fires `setBriefingLastTab(tab)` (fire-and-forget — best-effort). The actual navigation uses `<Link>` so back/forward works.

### `BriefingRedirect.tsx`

Mounted at `/briefing`. On mount:

1. `await getBriefingLastTab()` → `{tab: "daily" | "weekly"}`.
2. `navigate('/' + tab, { replace: true })`.

While the request is in flight render a tiny spinner card. On API error fall back to `/daily`.

### Date math helper

```ts
function shiftDay(date: string, days: number): string {
  // 'YYYY-MM-DD' is interpreted in Asia/Shanghai. Same trick as WeeklyPage's
  // shiftWeek — anchor the parse in +08:00, shift by days, render via UTC + 8h
  // offset.
}
```

### `api/client.ts` additions

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

export const getBriefingLastTab = () =>
  api.get<{ tab: 'daily' | 'weekly' }>('/briefing/last-tab').then(r => r.data)

export const setBriefingLastTab = (tab: 'daily' | 'weekly') =>
  api.post<void>('/briefing/last-tab', { tab })

export interface WeeklyDigest {
  week_start: string
  intro_text: string
  articles: Article[]
  pending?: boolean   // new, defaults to false on legacy responses
}
```

### Routes (`App.tsx`)

```tsx
<Route path="briefing" element={<BriefingRedirect />} />
<Route path="daily"    element={<DailyPage />} />
<Route path="weekly"   element={<WeeklyPage />} />  {/* existing */}
```

### Nav changes

- `Layout.tsx NAV_ITEMS`: replace `{ to: '/weekly', icon: '📅', label: '周刊' }` with `{ to: '/briefing', icon: '📅', label: '简报' }`. Keep position (between 订阅 and 推荐).
- `MoreSheet.tsx ITEMS`: remove the `周刊` line.

Per memory note: every frontend change requires `docker-compose up -d --build frontend` to take effect.

## Edge cases

- **Candidate pool < 5**: AI prompt asks for `N = min(5, len)`; validation accepts `len(picks) == N`. Pool == 0 → no cache row written; UI shows `当日无候选文章`.
- **AI parse failure**: no cache row, log line `briefing.daily.ai_parse_error user=N day=D msg=…`. Next 05:00 tick / next worker restart's catch-up will retry. After ~3 days the catch-up window expires and the day is permanently empty; acceptable.
- **Worker downtime spanning multiple days**: startup catch-up covers last 3 daily + last 1 weekly. Older gaps stay empty.
- **User created mid-day**: on first 05:00 after registration the worker will generate D-1. If the user had `<5` candidates that day, see "pool < 5" above.
- **Cron drift / double-fire on restart**: `lastFireDaily` timestamp + `UserIDsMissing` pre-check make per-day generation idempotent.
- **Future-dated `date` query**: 400. Negative very-old: 400 (`< today_label - 30`).
- **`today_label` boundary at 04:59 → 05:00 transition**: a request at 04:59 sees `today_label = D-1`, so default lands on `D-2`. A request at 05:00 sees `today_label = D`, default = `D-1`. The first 05:00 worker tick generates `D-1`, so the user opening at 05:30 hits the cache. Acceptable.

## Testing strategy

### Unit

- `internal/ai/daily_digest_test.go` — prompt construction, JSON parse/validation, fence stripping, N-pick branch.
- `internal/repository/daily_digest_test.go` — upsert idempotency, `Get` not-found, `UserIDsMissing` correctness.
- `internal/api/daily_test.go` — date-param branches, fallback to D-2, 400 bounds, live mode.
- `internal/api/briefing_test.go` — get / set tab, enum validation.
- Worker `fireDaily` / `fireWeekly` unit tests with mocked summarizer (extracted to standalone functions for testability).

### Manual integration

1. Apply migration: `docker compose exec -T postgres psql -U postgres -d rsspal < backend/migrations/031_daily_briefing.sql`.
2. `docker compose up -d --build api worker frontend`.
3. Backfill scenario: seed yesterday's articles, restart worker, observe catch-up generates `D-1` daily and the latest weekly.
4. Browser: navigate to `/briefing` → lands on `/daily` (default). Verify article cards, intro text, switch to `/weekly` and back (last-tab updates server-side; logout/login on another browser session lands on the last-chosen tab).
5. Force a parse failure (mock or temporarily break the prompt) → confirm no cache row, retry on next tick.

## Open questions

None at design time — see Q&A history. Re-open if implementation surfaces something unexpected.

## Migration / rollout notes

- Migration `031_daily_briefing.sql` is additive and safe to re-run (uses `IF NOT EXISTS`).
- Old API clients (mobile bookmarks etc.) continue to hit `/api/weekly-digest`; the only behavior change is "weekly intro may take one worker tick to appear" on a fresh week. UI placeholder covers it.
- No frontend feature flag — the nav swap from `周刊` → `简报` is the user-visible cutover. Backend and frontend should ship together to avoid a dangling `周刊` nav item.
