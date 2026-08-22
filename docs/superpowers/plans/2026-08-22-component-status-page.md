# Component Status Page Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Upgrade the existing public `/status` page to report six RSS Pal components with current two-state health, 72 hourly history buckets, uptime, and desktop/mobile details.

**Architecture:** Keep `status-monitor` as the public status-page owner. Add a PostgreSQL-backed Worker heartbeat and an internal Go health endpoint, then let the Python monitor probe five dependencies plus its own loop every 60 seconds, persist sanitized samples in SQLite, aggregate 72 hourly buckets server-side, and render the standalone status page.

**Tech Stack:** Go 1.24, Gin, PostgreSQL migrations/repository layer, Python 3.12 standard library, SQLite, Nginx, Docker Compose, Go tests, Python `unittest`.

---

## File map

- Create `backend/migrations/037_service_heartbeats.sql`: durable last-seen timestamp for background services.
- Create `backend/internal/repository/service_heartbeat.go`: database-time heartbeat upsert/read boundary.
- Create `backend/internal/repository/service_heartbeat_test.go`: migrated-schema repository coverage.
- Create `backend/internal/api/system_health.go`: internal Worker health JSON handler.
- Create `backend/internal/api/system_health_test.go`: handler threshold and error coverage.
- Modify `backend/cmd/worker/main.go`: emit an immediate heartbeat and keep it fresh on an independent 60-second ticker.
- Modify `backend/cmd/server/main.go`: construct/register the internal health handler.
- Create `status-monitor/components.py`: component definitions, HTTP probes, JSON validation, error sanitization.
- Create `status-monitor/aggregation.py`: SQLite query-to-72-bucket aggregation.
- Create `status-monitor/test_components.py`: probe and sanitization tests.
- Create `status-monitor/test_aggregation.py`: time bucket, uptime, and detail tests.
- Modify `status-monitor/server.py`: concurrent sampling loop, component API response, standalone page, tooltip behavior.
- Create `status-monitor/test_server.py`: response schema, self-health, refresh, and safe rendering tests.
- Modify `status-monitor/Dockerfile`: copy the new Python modules.
- Modify `docker-compose.yml`: configure the five probe URLs and retain 60-second sampling.
- Modify `frontend/nginx.conf`: deny the internal Worker endpoint before the generic `/api` proxy.
- Modify `README.md`: document the component status page and operational checks.

### Task 1: Persist Worker heartbeats using database time

**Files:**
- Create: `backend/migrations/037_service_heartbeats.sql`
- Create: `backend/internal/repository/service_heartbeat.go`
- Create: `backend/internal/repository/service_heartbeat_test.go`

- [ ] **Step 1: Write the failing migrated-schema repository tests**

Create `backend/internal/repository/service_heartbeat_test.go` with these cases:

```go
package repository_test

import (
    "testing"
    "time"

    "github.com/bytedance/rss-pal/internal/repository"
    "github.com/bytedance/rss-pal/internal/repository/testdb"
)

func TestServiceHeartbeatRepository_UpsertAndGet(t *testing.T) {
    db, cleanup := testdb.New(t)
    defer cleanup()
    repo := repository.NewServiceHeartbeatRepository(db)

    if err := repo.Beat("worker"); err != nil { t.Fatalf("Beat: %v", err) }
    first, err := repo.LastSeen("worker")
    if err != nil { t.Fatalf("LastSeen: %v", err) }
    if time.Since(first) > 5*time.Second { t.Fatalf("stale heartbeat: %v", first) }

    time.Sleep(10 * time.Millisecond)
    if err := repo.Beat("worker"); err != nil { t.Fatalf("second Beat: %v", err) }
    second, err := repo.LastSeen("worker")
    if err != nil { t.Fatalf("second LastSeen: %v", err) }
    if !second.After(first) { t.Fatalf("heartbeat did not advance: %v <= %v", second, first) }
}

func TestServiceHeartbeatRepository_Missing(t *testing.T) {
    db, cleanup := testdb.New(t)
    defer cleanup()
    repo := repository.NewServiceHeartbeatRepository(db)
    if _, err := repo.LastSeen("missing"); err != repository.ErrHeartbeatNotFound {
        t.Fatalf("got %v, want ErrHeartbeatNotFound", err)
    }
}
```

- [ ] **Step 2: Run the focused tests and verify the missing API failure**

Run: `cd backend && go test ./internal/repository -run ServiceHeartbeat -count=1`

Expected: FAIL because `NewServiceHeartbeatRepository` and `ErrHeartbeatNotFound` do not exist.

- [ ] **Step 3: Add migration 037**

Create `backend/migrations/037_service_heartbeats.sql`:

```sql
CREATE TABLE IF NOT EXISTS service_heartbeats (
    component TEXT PRIMARY KEY,
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

GRANT SELECT, INSERT, UPDATE ON service_heartbeats TO rsspal_app;
```

Do not enable RLS: this table contains global process liveness, not user-owned data.

- [ ] **Step 4: Implement the focused repository**

Create `backend/internal/repository/service_heartbeat.go`:

```go
package repository

import (
    "database/sql"
    "errors"
    "time"
)

var ErrHeartbeatNotFound = errors.New("service heartbeat not found")

type ServiceHeartbeatRepository struct { db Querier }

func NewServiceHeartbeatRepository(db *sql.DB) *ServiceHeartbeatRepository {
    return &ServiceHeartbeatRepository{db: db}
}

func (r *ServiceHeartbeatRepository) Beat(component string) error {
    _, err := r.db.Exec(`
        INSERT INTO service_heartbeats (component, last_seen_at)
        VALUES ($1, NOW())
        ON CONFLICT (component) DO UPDATE SET last_seen_at = NOW()
    `, component)
    return err
}

func (r *ServiceHeartbeatRepository) LastSeen(component string) (time.Time, error) {
    var lastSeen time.Time
    err := r.db.QueryRow(
        `SELECT last_seen_at FROM service_heartbeats WHERE component = $1`, component,
    ).Scan(&lastSeen)
    if errors.Is(err, sql.ErrNoRows) { return time.Time{}, ErrHeartbeatNotFound }
    return lastSeen, err
}
```

- [ ] **Step 5: Run repository tests**

Run: `cd backend && go test ./internal/repository -run ServiceHeartbeat -count=1`

Expected: PASS when PostgreSQL is available; otherwise the existing `testdb` helper reports a skip rather than a failure.

- [ ] **Step 6: Commit the heartbeat persistence**

```bash
git add backend/migrations/037_service_heartbeats.sql backend/internal/repository/service_heartbeat.go backend/internal/repository/service_heartbeat_test.go
git commit -m "feat: persist worker heartbeat"
```

### Task 2: Expose and update Worker health

**Files:**
- Create: `backend/internal/api/system_health.go`
- Create: `backend/internal/api/system_health_test.go`
- Modify: `backend/cmd/server/main.go:25-145`
- Modify: `backend/cmd/worker/main.go:32-140`

- [ ] **Step 1: Write failing handler tests with a clock seam**

Define a narrow reader interface and test exact threshold/error semantics in `backend/internal/api/system_health_test.go`:

```go
package api

import (
    "errors"
    "net/http"
    "net/http/httptest"
    "testing"
    "time"

    "github.com/gin-gonic/gin"
)

type fakeHeartbeatReader struct { at time.Time; err error }
func (f fakeHeartbeatReader) LastSeen(string) (time.Time, error) { return f.at, f.err }

func TestWorkerHealth(t *testing.T) {
    now := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
    cases := []struct{name string; at time.Time; err error; code int; status string}{
        {"fresh", now.Add(-3*time.Minute), nil, http.StatusOK, "ok"},
        {"stale", now.Add(-3*time.Minute-time.Second), nil, http.StatusServiceUnavailable, "down"},
        {"read error", time.Time{}, errors.New("db unavailable"), http.StatusServiceUnavailable, "down"},
    }
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            gin.SetMode(gin.TestMode)
            h := NewSystemHealthHandler(fakeHeartbeatReader{at:tc.at, err:tc.err}, func() time.Time{return now})
            r := gin.New(); r.GET("/api/internal/health/worker", h.Worker)
            w := httptest.NewRecorder(); r.ServeHTTP(w, httptest.NewRequest("GET", "/api/internal/health/worker", nil))
            if w.Code != tc.code { t.Fatalf("code=%d body=%s", w.Code, w.Body.String()) }
            if !strings.Contains(w.Body.String(), `"status":"`+tc.status+`"`) { t.Fatalf("body=%s", w.Body.String()) }
            if strings.Contains(w.Body.String(), "db unavailable") { t.Fatalf("leaked error: %s", w.Body.String()) }
        })
    }
}
```

Include the missing `strings` import in the actual file.

- [ ] **Step 2: Run the handler test and verify it fails**

Run: `cd backend && go test ./internal/api -run WorkerHealth -count=1`

Expected: FAIL because `NewSystemHealthHandler` is undefined.

- [ ] **Step 3: Implement the internal health handler**

Create `backend/internal/api/system_health.go` with a `HeartbeatReader`, a 3-minute constant, and this response contract:

```go
type HeartbeatReader interface { LastSeen(component string) (time.Time, error) }
type SystemHealthHandler struct { heartbeats HeartbeatReader; now func() time.Time }

func (h *SystemHealthHandler) Worker(c *gin.Context) {
    seen, err := h.heartbeats.LastSeen("worker")
    if err != nil || h.now().Sub(seen) > 3*time.Minute {
        c.JSON(http.StatusServiceUnavailable, gin.H{"status":"down"})
        return
    }
    c.JSON(http.StatusOK, gin.H{"status":"ok", "last_seen_at":seen.UTC()})
}
```

Keep constructors in the same file and never serialize repository errors.

- [ ] **Step 4: Wire server and Worker**

In `backend/cmd/server/main.go`, construct `heartbeatRepo` beside other repositories, construct `systemHealthHandler`, and register this route before authenticated groups:

```go
router.GET("/api/internal/health/worker", systemHealthHandler.Worker)
```

In `backend/cmd/worker/main.go`, construct the same repository from the bypass pool. Call `Beat("worker")` once after startup initialization, then start a cancellable goroutine with a 60-second ticker so long-running fetch cycles cannot make a healthy Worker appear stale. Log failures without stopping business work:

```go
func beatWorker(repo *repository.ServiceHeartbeatRepository) {
    if err := repo.Beat("worker"); err != nil { log.Printf("worker heartbeat: %v", err) }
}

func startWorkerHeartbeat(ctx context.Context, repo *repository.ServiceHeartbeatRepository) {
    beatWorker(repo)
    go func() {
        ticker := time.NewTicker(time.Minute)
        defer ticker.Stop()
        for {
            select {
            case <-ctx.Done(): return
            case <-ticker.C: beatWorker(repo)
            }
        }
    }()
}
```

- [ ] **Step 5: Add wiring tests**

Add source-level wiring assertions beside existing `backend/cmd/worker/*_wiring_test.go` patterns to prove the startup call, one-minute ticker, and context cancellation path exist, and extend the API test to assert no database details appear in error responses.

Run: `cd backend && go test ./cmd/worker ./internal/api -run 'Heartbeat|WorkerHealth' -count=1`

Expected: PASS.

- [ ] **Step 6: Run all Go tests and commit**

Run: `cd backend && go test ./...`

Expected: PASS.

```bash
git add backend/cmd/server/main.go backend/cmd/worker/main.go backend/cmd/worker/heartbeat_wiring_test.go backend/internal/api/system_health.go backend/internal/api/system_health_test.go
git commit -m "feat: report worker health"
```

### Task 3: Build safe component probes

**Files:**
- Create: `status-monitor/components.py`
- Create: `status-monitor/test_components.py`
- Modify: `status-monitor/Dockerfile`

- [ ] **Step 1: Write failing probe tests**

Use `unittest`, `unittest.mock.patch`, and small fake responses in `status-monitor/test_components.py`. Add six explicit test methods:

- `test_api_requires_200_json_ok`: HTTP 200 with `{"status":"ok"}` returns `up`; HTTP 200 with `{"status":"down"}` returns `down`.
- `test_api_rejects_invalid_json`: HTTP 200 with invalid JSON returns `down` and `invalid_response`.
- `test_frontend_accepts_302`: an HTTP 302 response returns `up` for an `http` probe.
- `test_rsshub_rejects_500`: an HTTP 500 response returns `down` and `http_error`.
- `test_timeout_is_down_and_sanitized`: `TimeoutError` returns `down` and `connection_timeout`.
- `test_error_never_contains_url_credentials_or_traceback`: an exception containing a credential-bearing URL returns only `connection_failed`.

Assert the public result is exactly shaped as `ProbeResult(status, code, latency_ms, error)` and sanitized errors are one of `connection_timeout`, `connection_failed`, `invalid_response`, or `http_error`.

- [ ] **Step 2: Run tests and verify the module is missing**

Run: `cd status-monitor && python -m unittest -v test_components.py`

Expected: ERROR importing `components`.

- [ ] **Step 3: Implement definitions and probes**

Create `status-monitor/components.py` with immutable component metadata loaded through an explicit environment seam:

```python
def load_components(env):
    return (
        Component("frontend", "Frontend", env["FRONTEND_URL"], "http"),
        Component("api", "API", env["API_HEALTH_URL"], "json_ok"),
        Component("worker", "Worker", env["WORKER_HEALTH_URL"], "json_ok"),
        Component("rsshub", "RSSHub", env["RSSHUB_HEALTH_URL"], "http"),
        Component("public", "公网入口", env["PUBLIC_HEALTH_URL"], "json_ok"),
    )
```

Implement `probe(component, timeout=10)` with monotonic latency, 2xx/3xx acceptance for `http`, strict HTTP 200 plus JSON `status == "ok"` for `json_ok`, body draining, and error-category mapping. Do not include exception text in `ProbeResult.error`.

- [ ] **Step 4: Copy the module into the image and run tests**

Update `status-monitor/Dockerfile`:

```dockerfile
COPY server.py components.py aggregation.py ./
```

Run: `cd status-monitor && python -m unittest -v test_components.py`

Expected: PASS.

- [ ] **Step 5: Commit probe logic**

```bash
git add status-monitor/components.py status-monitor/test_components.py status-monitor/Dockerfile
git commit -m "feat: add component health probes"
```

### Task 4: Aggregate 72 hourly buckets

**Files:**
- Create: `status-monitor/aggregation.py`
- Create: `status-monitor/test_aggregation.py`

- [ ] **Step 1: Write failing bucket tests**

Build an in-memory SQLite fixture with samples on hour boundaries and assert:

```python
self.assertEqual(len(component["hours"]), 72)
self.assertEqual(red_bucket["status"], "down")
self.assertEqual(red_bucket["successful_checks"], 59)
self.assertEqual(red_bucket["total_checks"], 60)
self.assertEqual(red_bucket["uptime_pct"], 98.33)
self.assertEqual(red_bucket["avg_latency_ms"], 42)
self.assertEqual(red_bucket["last_error"], "connection_timeout")
self.assertIsNone(gray_bucket["status"])
self.assertEqual(component["uptime_pct"], expected_sample_weighted_pct)
```

Also cover CST natural-hour boundaries, oldest-to-newest ordering, a failure anywhere making the hour red, missing hours staying gray, latest check determining current status, and SQLite errors propagating instead of returning all green.

- [ ] **Step 2: Run tests and verify failure**

Run: `cd status-monitor && python -m unittest -v test_aggregation.py`

Expected: ERROR importing `aggregation`.

- [ ] **Step 3: Implement aggregation**

Create `status-monitor/aggregation.py` with four pure helpers and these exact signatures:

- `hour_floor(value: datetime) -> datetime`: normalize to CST and clear minute, second, and microsecond.
- `build_hour_buckets(rows, now, hours=72) -> list[dict]`: return exactly 72 oldest-to-newest buckets.
- `component_summary(conn, component, now, hours=72) -> dict`: query one component and return current/sample-weighted summary.
- `status_payload(conn, component_defs, now, interval_seconds) -> dict`: preserve configured component order and derive overall status.

Each bucket returns `start`, `end`, `status`, `uptime_pct`, `successful_checks`, `total_checks`, `avg_latency_ms`, `last_error`, and `last_error_at`. The payload returns `generated_at`, `refresh_interval_seconds`, `overall_status`, and ordered `components`.

- [ ] **Step 4: Run aggregation tests and commit**

Run: `cd status-monitor && python -m unittest -v test_aggregation.py`

Expected: PASS.

```bash
git add status-monitor/aggregation.py status-monitor/test_aggregation.py
git commit -m "feat: aggregate component uptime history"
```

### Task 5: Replace the sampler and status API

**Files:**
- Modify: `status-monitor/server.py:1-190,429-466`
- Create: `status-monitor/test_server.py`

- [ ] **Step 1: Write failing monitor-service tests**

In `status-monitor/test_server.py`, use a temporary SQLite file, an injected probe function, and deterministic `now` arguments. Add these exact cases:

- `test_sample_cycle_records_all_six_components`: query SQLite and assert one row for every configured component plus `status-monitor`.
- `test_one_failure_does_not_block_other_components`: make the API probe fail and assert the remaining probe rows are still written.
- `test_monitor_gap_over_120_seconds_records_self_down`: seed an old completion time and assert the next self sample is `down`.
- `test_api_status_has_six_components_and_72_hours`: call the handler, parse JSON, assert six ordered components and 72 buckets per component.
- `test_sqlite_read_error_returns_500_not_fake_green`: close/remove the test database and assert HTTP 500 with `status_data_unavailable`.
- `test_error_response_contains_no_internal_url`: inject an error containing an internal URL and assert neither the URL nor raw exception appears in the API response.

- [ ] **Step 2: Run the tests and verify old behavior fails**

Run: `cd status-monitor && python -m unittest -v test_server.py`

Expected: FAIL because the current loop only records `local`, the API returns `local/domain_stats`, and no injectable service exists.

- [ ] **Step 3: Refactor sampling into a testable service**

In `status-monitor/server.py`, introduce `MonitorService` with `sample_once(now)`, `payload(now)`, and `last_cycle_completed_at`. Use a bounded `ThreadPoolExecutor(max_workers=5)` so the five external probes run independently. At the beginning of a cycle, record `status-monitor=down` if the previous completion gap exceeds 120 seconds; otherwise record it as `up`. Record each other probe result separately, then update `last_cycle_completed_at`.

Keep DB connections short-lived and set a SQLite busy timeout. Preserve seven-day cleanup.

- [ ] **Step 4: Replace `/api/status` response handling**

Return `application/json` with the aggregated component schema. Catch SQLite read failures at the HTTP boundary and return:

```json
{"error":"status_data_unavailable"}
```

with HTTP 500. Never catch the error and substitute empty green data.

- [ ] **Step 5: Run all Python service tests**

Run: `cd status-monitor && python -m unittest -v test_components.py test_aggregation.py test_server.py`

Expected: PASS.

- [ ] **Step 6: Commit the monitor backend**

```bash
git add status-monitor/server.py status-monitor/test_server.py
git commit -m "feat: collect component status history"
```

### Task 6: Render the public 72-hour page and details

**Files:**
- Modify: `status-monitor/server.py:HTML_PAGE`
- Modify: `status-monitor/test_server.py`

- [ ] **Step 1: Add failing page contract tests**

Extend `status-monitor/test_server.py` to assert the HTML contains:

```python
self.assertIn("RSS Pal Status", html)
self.assertIn("过去 72 小时可用情况", html)
self.assertIn("setInterval(loadData, 60000)", html)
self.assertIn('role="tooltip"', html)
self.assertIn("pointerdown", html)  # mobile click/tap support
self.assertNotIn("innerHTML = incidents", html)
```

Add a DOM-data escaping test around the JSON-to-text rendering helper so a malicious error such as `<img src=x onerror=alert(1)>` is assigned with `textContent`, never interpolated into HTML.

- [ ] **Step 2: Run the page tests and verify failure**

Run: `cd status-monitor && python -m unittest -v test_server.py`

Expected: FAIL because the old page has two sections, 30-second refresh, and no hourly tooltip.

- [ ] **Step 3: Implement the approved layout**

Replace the embedded page with:

- title, generated time, and 60-second refresh label;
- green “所有系统运行正常” or red “部分系统故障” banner;
- six ordered component rows;
- 72 buttons per row with green/red/gray classes and accessible labels;
- sample-weighted uptime and the 72-hours-ago/now axis;
- a single reusable `role="tooltip"` populated using `textContent`;
- mouse enter/leave and focus/blur for desktop/keyboard;
- pointer/click toggle for mobile;
- retained last successful render plus “数据暂时无法刷新” on refresh failure.

Use CSS media queries to reduce bar gaps below 650px without hiding names, status, or uptime.

- [ ] **Step 4: Run Python tests**

Run: `cd status-monitor && python -m unittest -v`

Expected: PASS.

- [ ] **Step 5: Commit the page**

```bash
git add status-monitor/server.py status-monitor/test_server.py
git commit -m "feat: render 72-hour component status page"
```

### Task 7: Wire Compose, protect the internal endpoint, and document operations

**Files:**
- Modify: `docker-compose.yml:141-156`
- Modify: `frontend/nginx.conf:37-70`
- Modify: `README.md:65-120`

- [ ] **Step 1: Add a failing Nginx configuration assertion**

Add a lightweight Python test in `status-monitor/test_server.py` that reads `../frontend/nginx.conf` and asserts the exact deny block appears before `location ^~ /api`:

```nginx
location = /api/internal/health/worker {
    return 404;
}
```

Run: `cd status-monitor && python -m unittest -v test_server.py`

Expected: FAIL until the deny block exists.

- [ ] **Step 2: Update Nginx and Compose**

Insert the exact Nginx block before both `/api/status` and the generic `/api` proxy.

Replace legacy monitor variables with:

```yaml
environment:
  FRONTEND_URL: http://frontend:80/
  API_HEALTH_URL: http://api:8080/api/health
  WORKER_HEALTH_URL: http://api:8080/api/internal/health/worker
  RSSHUB_HEALTH_URL: http://rsshub:1200/
  PUBLIC_HEALTH_URL: https://rss.morefreeze.top/api/health
  CHECK_INTERVAL: 60
  MONITOR_PORT: 8090
```

Remove the obsolete host-mounted `domain.db`; the monitor now performs and persists the public probe itself. Preserve the named `status_data` volume.

- [ ] **Step 3: Document the status page**

Update `README.md` to list the six components, 60-second interval, Worker 3-minute threshold, public `/status` URL, and these verification commands:

```bash
curl -fsS http://127.0.0.1:8090/api/status
curl -fsS http://127.0.0.1/status
curl -fsS https://rss.morefreeze.top/status
```

- [ ] **Step 4: Validate configs and tests**

Run:

```bash
docker compose config --quiet
docker run --rm -v "$PWD/frontend/nginx.conf:/etc/nginx/conf.d/default.conf:ro" nginx:alpine nginx -t
cd status-monitor && python -m unittest -v
```

Expected: Compose config succeeds, Nginx reports configuration successful, and Python tests pass.

- [ ] **Step 5: Commit integration config**

```bash
git add docker-compose.yml frontend/nginx.conf README.md status-monitor/test_server.py
git commit -m "chore: wire component status monitoring"
```

### Task 8: Full verification and manual acceptance

**Files:**
- No new source files expected.

- [ ] **Step 1: Run backend verification**

Run: `cd backend && go test ./...`

Expected: PASS.

- [ ] **Step 2: Run status-monitor verification**

Run: `cd status-monitor && python -m unittest -v`

Expected: PASS.

- [ ] **Step 3: Run frontend regression verification**

Run: `cd frontend && npm test -- --run && npm run build`

Expected: 32 or more test files pass, 231 or more tests pass, and Vite build completes.

- [ ] **Step 4: Build and start the affected Compose services**

Run:

```bash
docker compose build api worker status-monitor frontend
docker compose up -d api worker status-monitor frontend
docker compose ps
```

Expected: affected containers are running; PostgreSQL dependency remains healthy.

- [ ] **Step 5: Verify runtime contracts**

Run:

```bash
curl -fsS http://127.0.0.1:8080/api/health
curl -i http://127.0.0.1/api/internal/health/worker
curl -fsS http://127.0.0.1:8090/api/status
curl -fsS http://127.0.0.1/status
```

Expected: API health is `ok`; public Nginx returns 404 for the internal Worker endpoint; `/api/status` returns six components and 72 buckets each; `/status` renders without authentication.

- [ ] **Step 6: Verify browser interactions**

Open `/status` at desktop and narrow mobile widths. Confirm Hover/focus details on desktop, tap details on mobile, safe gray missing history, red failure buckets, no horizontal loss of names/status/uptime, and one network refresh after 60 seconds.

- [ ] **Step 7: Check the final diff and commit any verification-only fixes**

Run:

```bash
git diff --check
git status --short
git log --oneline --decorate -8
```

Expected: no whitespace errors and no unrelated files. If verification required a source correction, commit only those exact files with a focused `fix:` message and rerun the affected verification command.

Production deployment is not implied by this implementation plan. Deploy only after a separate explicit request, then verify the running image, local/public health endpoints, public `/status`, and the deployed bundle/runtime revision.
