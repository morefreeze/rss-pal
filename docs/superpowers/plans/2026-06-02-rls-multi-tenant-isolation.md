# RLS-Based Multi-Tenant Data Isolation — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add Postgres Row-Level Security as a database-layer safety net for the existing application-level user-id filtering, so future SQL bugs cannot leak one user's data to another, before the service is opened to non-admin invitees.

**Architecture:** Each authenticated HTTP request opens a Postgres transaction in middleware and executes `SET LOCAL app.user_id = <jwt.user_id>`. Repository code is refactored to take a `Querier` interface that both `*sql.DB` and `*sql.Tx` satisfy, so the same code path can run inside the request transaction. Every private table (per-user state) and the two shared content tables (`feeds`, `articles`) get RLS enabled with a `USING` policy referencing `current_setting('app.user_id')`. The worker process (cross-user batch jobs) sets `app.bypass_rls = 'true'` on its connections at startup and is exempted via the policy. Public-token endpoints (share, bookmarklet, extension ingest, PDF image serving) resolve a user id from the token and set `app.user_id` accordingly inside a manually-managed transaction.

**Tech Stack:** Go 1.24, `database/sql` + `lib/pq`, Gin, PostgreSQL 15, Docker Compose (for integration tests).

**Pre-reqs:** Architecture brainstormed in chat on 2026-06-02. Engineer must read `Explore` report in chat (table inventory, current connection model, hot-spot list) before starting Task 1.

---

## Definitions & invariants

- **`app.user_id`** (Postgres GUC, custom): set per-request to the authenticated user's id. Type: int. Default: unset → policy treats as no access.
- **`app.bypass_rls`** (Postgres GUC, custom): set to `'true'` on worker connections and during migrations / admin operations. Default: unset → not bypassed.
- **Private tables** (RLS scoped by `user_id` column):
  `reading_progress`, `playback_progress`, `user_preferences`, `user_tags`, `article_user_tags`, `tag_suggestion_dismissals`, `interest_topics`, `interest_tags`, `interest_categories`, `user_insights`, `article_events`, `weekly_digests`, `daily_digests`, `hidden_articles`, `user_ai_configs`.
- **Shared-but-owned tables** (RLS scoped by `owner_id` and/or via parent feed):
  `feeds` (owner_id), `articles` (via `feed_id → feeds.owner_id`), `summary_templates` (system rows + per-user rows).
- **Admin / no-RLS tables** (no per-row scoping; access controlled via role checks in handlers):
  `users`, `invite_codes`, `feed_health_metrics`, `link_set_candidates`, `recommended_feeds`, `share_tokens`.
- **Querier interface** (new): the minimal subset of `*sql.DB` methods used by repositories. Both `*sql.DB` and `*sql.Tx` implement it.

---

## File Structure

**New files:**
- `backend/internal/repository/querier.go` — defines `Querier` interface.
- `backend/internal/api/rls.go` — middleware that opens a request-scoped tx and sets `app.user_id`.
- `backend/internal/repository/testdb/testdb.go` — integration test helper that spins up a dedicated `rsspal_test` database, runs migrations, returns a `*sql.DB`.
- `backend/internal/repository/testdb/testdb_test.go` — sanity test that fixture boots.
- `backend/migrations/033_enable_rls.sql` — enables RLS + creates policies on private and shared-but-owned tables.
- `backend/migrations/034_rls_bypass_role.sql` *(optional, only if we choose the DB-role bypass; default plan uses the GUC-bypass approach without this file)*.
- `backend/internal/api/rls_leak_test.go` — cross-user leak e2e tests.

**Modified files:**
- `backend/internal/repository/db.go` — add helper to set bypass on a connection (used by worker).
- `backend/internal/repository/*.go` — every repository constructor switches from `*sql.DB` to `Querier`; struct field renamed `db Querier`; method bodies unchanged.
- `backend/cmd/server/main.go` — register the RLS middleware ahead of the route group; keep the existing `AuthMiddleware`.
- `backend/cmd/worker/main.go` — after `repository.NewDB`, call helper to set `app.bypass_rls = 'true'` for all worker connections.
- `backend/internal/api/share.go`, `bookmarklet.go`, `extension_ingest.go`, `article_images.go` — explicitly set `app.user_id` in a manually-managed tx for endpoints that resolve user via token instead of JWT.
- `CLAUDE.md` — append a "Multi-tenant rules" section.

---

## Phase 0 — Test infrastructure (prerequisite)

Currently there is **no integration test that hits Postgres**. RLS cannot be verified without one. This phase adds a minimal fixture.

### Task 0.1: Postgres test database helper

**Files:**
- Create: `backend/internal/repository/testdb/testdb.go`
- Create: `backend/internal/repository/testdb/testdb_test.go`

The helper expects a Postgres reachable via `TEST_DB_URL` env var (default: `postgres://postgres:postgres@127.0.0.1:5432/rsspal_test?sslmode=disable`). On each test it creates a unique schema, runs all migrations in `backend/migrations/*.sql` in order, returns a `*sql.DB` scoped to that schema, and registers `t.Cleanup` to drop the schema.

- [ ] **Step 1: Write the failing test**

```go
// backend/internal/repository/testdb/testdb_test.go
package testdb

import (
    "testing"
)

func TestNewBootsAndRunsMigrations(t *testing.T) {
    db, cleanup := New(t)
    defer cleanup()

    var n int
    if err := db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n); err != nil {
        t.Fatalf("expected users table to exist after migrations: %v", err)
    }
    if n != 0 {
        t.Fatalf("expected empty users table, got %d rows", n)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/repository/testdb/...`
Expected: FAIL — `package testdb does not exist` or `undefined: New`.

- [ ] **Step 3: Write the helper**

```go
// backend/internal/repository/testdb/testdb.go
package testdb

import (
    "database/sql"
    "fmt"
    "os"
    "path/filepath"
    "sort"
    "strings"
    "testing"

    _ "github.com/lib/pq"
)

// New returns a *sql.DB pointing at a fresh, migrated schema in the test DB.
// The schema is dropped via t.Cleanup. Concurrent tests get isolated schemas.
func New(t *testing.T) (*sql.DB, func()) {
    t.Helper()
    dsn := os.Getenv("TEST_DB_URL")
    if dsn == "" {
        dsn = "postgres://postgres:postgres@127.0.0.1:5432/rsspal_test?sslmode=disable"
    }
    adminDB, err := sql.Open("postgres", dsn)
    if err != nil {
        t.Fatalf("open admin db: %v", err)
    }
    if err := adminDB.Ping(); err != nil {
        t.Skipf("postgres not available at %s: %v", dsn, err)
    }

    schema := fmt.Sprintf("test_%s", strings.ReplaceAll(t.Name(), "/", "_"))
    schema = strings.ToLower(schema)
    if len(schema) > 60 {
        schema = schema[:60]
    }

    if _, err := adminDB.Exec(fmt.Sprintf(`DROP SCHEMA IF EXISTS %q CASCADE`, schema)); err != nil {
        t.Fatalf("drop schema: %v", err)
    }
    if _, err := adminDB.Exec(fmt.Sprintf(`CREATE SCHEMA %q`, schema)); err != nil {
        t.Fatalf("create schema: %v", err)
    }

    schemaDSN := dsn
    sep := "?"
    if strings.Contains(dsn, "?") {
        sep = "&"
    }
    schemaDSN = fmt.Sprintf("%s%ssearch_path=%s", dsn, sep, schema)

    db, err := sql.Open("postgres", schemaDSN)
    if err != nil {
        t.Fatalf("open schema db: %v", err)
    }
    if err := runMigrations(db); err != nil {
        t.Fatalf("migrations: %v", err)
    }

    cleanup := func() {
        db.Close()
        _, _ = adminDB.Exec(fmt.Sprintf(`DROP SCHEMA IF EXISTS %q CASCADE`, schema))
        adminDB.Close()
    }
    return db, cleanup
}

func runMigrations(db *sql.DB) error {
    dir := migrationsDir()
    entries, err := os.ReadDir(dir)
    if err != nil {
        return err
    }
    var files []string
    for _, e := range entries {
        if strings.HasSuffix(e.Name(), ".sql") {
            files = append(files, e.Name())
        }
    }
    sort.Strings(files)
    for _, name := range files {
        b, err := os.ReadFile(filepath.Join(dir, name))
        if err != nil {
            return fmt.Errorf("%s: %w", name, err)
        }
        if _, err := db.Exec(string(b)); err != nil {
            return fmt.Errorf("%s: %w", name, err)
        }
    }
    return nil
}

func migrationsDir() string {
    // Walk up until we find backend/migrations.
    wd, _ := os.Getwd()
    for i := 0; i < 6; i++ {
        candidate := filepath.Join(wd, "migrations")
        if st, err := os.Stat(candidate); err == nil && st.IsDir() {
            return candidate
        }
        candidate = filepath.Join(wd, "backend", "migrations")
        if st, err := os.Stat(candidate); err == nil && st.IsDir() {
            return candidate
        }
        wd = filepath.Dir(wd)
    }
    panic("backend/migrations not found")
}
```

- [ ] **Step 4: Bring up test DB and run**

```bash
docker run -d --name rsspal-pg-test -p 5432:5432 \
  -e POSTGRES_PASSWORD=postgres -e POSTGRES_DB=rsspal_test \
  postgres:15-alpine
# wait ~3s
cd backend && go test ./internal/repository/testdb/...
```

Expected: PASS.

- [ ] **Step 5: Document the fixture**

Append to `backend/README.md` (or create if missing) a one-paragraph "Integration tests" section explaining `TEST_DB_URL` and the docker command above.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/repository/testdb/ backend/README.md
git commit -m "test: add Postgres integration-test fixture for RLS work"
```

---

## Phase 1 — Querier interface + per-request transaction middleware

### Task 1.1: Define the Querier interface

**Files:**
- Create: `backend/internal/repository/querier.go`

- [ ] **Step 1: Write the interface**

```go
// backend/internal/repository/querier.go
package repository

import (
    "context"
    "database/sql"
)

// Querier is the minimal subset of *sql.DB used by repositories. Both *sql.DB
// and *sql.Tx satisfy it, so repository code can run either against the global
// pool (worker, startup) or inside a per-request transaction (HTTP handlers).
type Querier interface {
    Exec(query string, args ...interface{}) (sql.Result, error)
    ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
    Query(query string, args ...interface{}) (*sql.Rows, error)
    QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
    QueryRow(query string, args ...interface{}) *sql.Row
    QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
}
```

- [ ] **Step 2: Verify both *sql.DB and *sql.Tx satisfy it**

```go
// at the bottom of querier.go
var (
    _ Querier = (*sql.DB)(nil)
    _ Querier = (*sql.Tx)(nil)
)
```

- [ ] **Step 3: Build**

Run: `cd backend && go build ./...`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add backend/internal/repository/querier.go
git commit -m "feat(repo): add Querier interface (precursor to RLS tx-per-request)"
```

### Task 1.2: RLS middleware

**Files:**
- Create: `backend/internal/api/rls.go`
- Modify: `backend/cmd/server/main.go` — register middleware

The middleware:
1. Reads `userID` and `isAdmin` from the gin context (set by `AuthMiddleware` upstream).
2. Calls `db.BeginTx(c.Request.Context(), nil)`.
3. Executes `SET LOCAL app.user_id = $1` and, if admin, `SET LOCAL app.is_admin = 'true'`.
4. Stashes the `*sql.Tx` under context key `"tx"`.
5. After `c.Next()` returns: commits on 2xx/3xx/4xx (still a successful HTTP exchange, just user-error), rolls back only on 5xx/panic.

- [ ] **Step 1: Write the middleware test**

```go
// backend/internal/api/rls_test.go
package api_test

import (
    "net/http"
    "net/http/httptest"
    "testing"

    "github.com/bytedance/rss-pal/internal/api"
    "github.com/bytedance/rss-pal/internal/repository/testdb"
    "github.com/gin-gonic/gin"
)

func TestRLSMiddleware_SetsUserID(t *testing.T) {
    db, cleanup := testdb.New(t)
    defer cleanup()

    gin.SetMode(gin.TestMode)
    r := gin.New()
    r.Use(func(c *gin.Context) { c.Set("userID", 42); c.Next() })
    r.Use(api.RLSTxMiddleware(db))
    r.GET("/check", func(c *gin.Context) {
        var got string
        tx := c.MustGet("tx")
        // current_setting('x', true) returns '' if unset, never errors.
        err := tx.(interface {
            QueryRow(string, ...interface{}) *sqlRowShim
        }) // type-assert; or use repository.Querier
        _ = err
        _ = got
    })
    req := httptest.NewRequest(http.MethodGet, "/check", nil)
    w := httptest.NewRecorder()
    r.ServeHTTP(w, req)
    if w.Code != http.StatusOK {
        t.Fatalf("status: %d", w.Code)
    }
}
```

**NOTE:** the above sketch is intentionally rough — the real test should use the `Querier` interface and `QueryRow` to read back `current_setting('app.user_id')` and assert it equals `"42"`. Final form:

```go
package api_test

import (
    "net/http"
    "net/http/httptest"
    "testing"

    "github.com/bytedance/rss-pal/internal/api"
    "github.com/bytedance/rss-pal/internal/repository"
    "github.com/bytedance/rss-pal/internal/repository/testdb"
    "github.com/gin-gonic/gin"
)

func TestRLSMiddleware_SetsUserID(t *testing.T) {
    db, cleanup := testdb.New(t)
    defer cleanup()

    gin.SetMode(gin.TestMode)
    r := gin.New()
    r.Use(func(c *gin.Context) { c.Set("userID", 42); c.Next() })
    r.Use(api.RLSTxMiddleware(db))
    r.GET("/check", func(c *gin.Context) {
        q := c.MustGet("tx").(repository.Querier)
        var got string
        if err := q.QueryRow(`SELECT current_setting('app.user_id', true)`).Scan(&got); err != nil {
            t.Fatalf("scan: %v", err)
            return
        }
        if got != "42" {
            t.Fatalf("expected app.user_id=42, got %q", got)
        }
        c.Status(http.StatusOK)
    })
    req := httptest.NewRequest(http.MethodGet, "/check", nil)
    w := httptest.NewRecorder()
    r.ServeHTTP(w, req)
    if w.Code != http.StatusOK {
        t.Fatalf("status: %d", w.Code)
    }
}
```

- [ ] **Step 2: Run, expect compile failure (RLSTxMiddleware undefined)**

Run: `cd backend && go test ./internal/api/ -run TestRLSMiddleware_SetsUserID`
Expected: FAIL — `undefined: api.RLSTxMiddleware`.

- [ ] **Step 3: Implement the middleware**

```go
// backend/internal/api/rls.go
package api

import (
    "database/sql"
    "net/http"

    "github.com/gin-gonic/gin"
)

// CtxKeyTx is the gin context key under which the per-request *sql.Tx is stored.
const CtxKeyTx = "tx"

// RLSTxMiddleware opens a per-request transaction, sets app.user_id (and
// app.is_admin if applicable) via SET LOCAL, and exposes the tx via the gin
// context. The transaction is committed on success (HTTP status < 500) or
// rolled back on 5xx / panic. Must run AFTER AuthMiddleware.
func RLSTxMiddleware(db *sql.DB) gin.HandlerFunc {
    return func(c *gin.Context) {
        uidRaw, exists := c.Get("userID")
        if !exists {
            c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing user"})
            return
        }
        userID, _ := uidRaw.(int)
        isAdmin := c.GetBool("isAdmin")

        tx, err := db.BeginTx(c.Request.Context(), nil)
        if err != nil {
            c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "tx begin"})
            return
        }
        // SET LOCAL accepts only literal values in parameterized form via
        // set_config(), which IS local when the third arg is true.
        if _, err := tx.Exec(`SELECT set_config('app.user_id', $1, true)`, userID); err != nil {
            _ = tx.Rollback()
            c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "tx setup"})
            return
        }
        if isAdmin {
            if _, err := tx.Exec(`SELECT set_config('app.is_admin', 'true', true)`); err != nil {
                _ = tx.Rollback()
                c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "tx setup"})
                return
            }
        }

        c.Set(CtxKeyTx, tx)

        defer func() {
            if rec := recover(); rec != nil {
                _ = tx.Rollback()
                panic(rec) // re-raise so gin's recovery middleware logs it
            }
        }()

        c.Next()

        if c.Writer.Status() >= 500 || len(c.Errors) > 0 {
            _ = tx.Rollback()
            return
        }
        if err := tx.Commit(); err != nil {
            // Best-effort: log via gin
            _ = c.Error(err)
        }
    }
}
```

- [ ] **Step 4: Run test**

Run: `cd backend && go test ./internal/api/ -run TestRLSMiddleware_SetsUserID`
Expected: PASS.

- [ ] **Step 5: Wire up in router**

In `backend/cmd/server/main.go`, find the JWT-protected route group (the `authed := router.Group("/api"); authed.Use(authHandler.AuthMiddleware())` block) and insert `authed.Use(api.RLSTxMiddleware(db))` immediately after the auth middleware. Public routes (login, share, bookmarklet, extension ingest, PDF image serve) must NOT use this middleware — they get their own tx setup in Phase 4.

- [ ] **Step 6: Build**

Run: `cd backend && go build ./...`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add backend/internal/api/rls.go backend/internal/api/rls_test.go backend/cmd/server/main.go
git commit -m "feat(api): RLS tx-per-request middleware sets app.user_id"
```

---

## Phase 2 — Repository refactor to Querier

Every repository struct currently holds `*sql.DB`. We switch them to hold `Querier`, then provide a `WithCtx(c *gin.Context)` helper that returns a copy bound to the request tx. Handlers call `repo.WithCtx(c).Method(...)`. The worker calls `repo` directly (the underlying `*sql.DB`).

### Task 2.1: Refactor template — `ArticleRepository`

**Files:**
- Modify: `backend/internal/repository/article.go`
- Modify: `backend/internal/api/article.go` (call sites)

This task is done first as the canonical example. Engineer reads this carefully and then applies the same pattern to every other repo in Task 2.2.

- [ ] **Step 1: Change the struct field and constructor**

In `article.go`, find:
```go
type ArticleRepository struct {
    db *sql.DB
}
func NewArticleRepository(db *sql.DB) *ArticleRepository {
    return &ArticleRepository{db: db}
}
```
Replace with:
```go
type ArticleRepository struct {
    db Querier
    raw *sql.DB // underlying pool, kept for WithCtx; nil when copied via WithCtx
}
func NewArticleRepository(db *sql.DB) *ArticleRepository {
    return &ArticleRepository{db: db, raw: db}
}

// WithCtx returns a repository view bound to the per-request transaction
// stashed under api.CtxKeyTx. Falls back to the underlying pool if no tx
// is present (e.g. tests that bypass middleware).
func (r *ArticleRepository) WithCtx(c interface{ Get(string) (interface{}, bool) }) *ArticleRepository {
    if v, ok := c.Get("tx"); ok {
        if q, ok := v.(Querier); ok {
            return &ArticleRepository{db: q, raw: r.raw}
        }
    }
    return r
}
```

(The `interface{ Get(string) (interface{}, bool) }` shape lets us avoid importing gin from `repository`. Handlers pass `c *gin.Context` which satisfies it.)

- [ ] **Step 2: Verify the package compiles**

Run: `cd backend && go build ./internal/repository/`
Expected: PASS.

- [ ] **Step 3: Update one representative handler call site**

In `backend/internal/api/article.go`, find `h.articleRepo.GetByIDWithFeedType(id, getUserID(c))` and similar. Change to `h.articleRepo.WithCtx(c).GetByIDWithFeedType(id, getUserID(c))`. (We keep `userID` as a parameter for now; RLS is defense-in-depth. We will tighten this in Phase 5.)

There are many call sites for the article repo. Use this grep to enumerate:
```bash
grep -n "h\.articleRepo\." backend/internal/api/*.go
```
Update every `h.articleRepo.X(...)` → `h.articleRepo.WithCtx(c).X(...)`.

- [ ] **Step 4: Build**

Run: `cd backend && go build ./...`
Expected: PASS.

- [ ] **Step 5: Run existing tests**

Run: `cd backend && go test ./...`
Expected: PASS (no behavior change yet — RLS migration hasn't run).

- [ ] **Step 6: Commit**

```bash
git add backend/internal/repository/article.go backend/internal/api/
git commit -m "refactor(repo): ArticleRepository takes Querier; handlers use WithCtx"
```

### Task 2.2: Apply the same refactor to every other repository

**Files (one task step per file; each is the same pattern as Task 2.1):**

- [ ] `backend/internal/repository/feed.go` — `FeedRepository`
- [ ] `backend/internal/repository/clip.go` — `ClipRepository`
- [ ] `backend/internal/repository/daily_digest.go` — `DailyDigestRepository`
- [ ] `backend/internal/repository/event.go` — `EventRepository`
- [ ] `backend/internal/repository/feed_health.go` — `FeedHealthRepository`
- [ ] `backend/internal/repository/hidden_article.go` — `HiddenArticleRepository`
- [ ] `backend/internal/repository/insight.go` — `InsightRepository`
- [ ] `backend/internal/repository/link_set.go` — `LinkSetRepository`
- [ ] `backend/internal/repository/playback_progress.go` — `PlaybackProgressRepository`
- [ ] `backend/internal/repository/preference.go` — `PreferenceRepository`
- [ ] `backend/internal/repository/progress.go` — `ProgressRepository`
- [ ] `backend/internal/repository/share.go` — `ShareRepository`
- [ ] `backend/internal/repository/stats.go` — `StatsRepository`
- [ ] `backend/internal/repository/template.go` — `TemplateRepository`
- [ ] `backend/internal/repository/user_tag.go` — `UserTagRepository`
- [ ] `backend/internal/repository/user.go` — `UserRepository` (still receives `*sql.DB` for admin paths like login; add WithCtx but it's optional for this one — userRepo lookups happen before the tx exists, e.g. JWT verification).
- [ ] `backend/internal/repository/weekly_digest.go` — `WeeklyDigestRepository`

For each file, perform Task 2.1 steps 1–2 (change struct + constructor + add `WithCtx`). Do NOT update handler call sites yet for these — that happens in step below as one batch.

- [ ] **Step (after all files): batch-update every handler**

Run:
```bash
cd backend
# Find every handler that calls a repo method without WithCtx.
grep -nE "h\.[a-zA-Z]+Repo\.[A-Z]" internal/api/*.go | grep -v "WithCtx"
```
For each match in files that mount on the `authed` group, prefix `.WithCtx(c)`. Skip files for public endpoints (`share.go`, `bookmarklet.go`, `extension_ingest.go`, `article_images.go`) — those get manual tx handling in Phase 4.

- [ ] **Final step: build + test + commit**

```bash
cd backend && go build ./... && go test ./...
git add backend/internal/repository/ backend/internal/api/
git commit -m "refactor(repo): switch all repos to Querier + WithCtx pattern"
```

---

## Phase 3 — RLS policies migration

### Task 3.1: Write migration 033

**Files:**
- Create: `backend/migrations/033_enable_rls.sql`

This migration:
1. Defines `app.user_id` and `app.bypass_rls` as customizable GUCs (Postgres allows arbitrary `app.*` settings without registration; no DDL needed, but we document them).
2. Enables RLS on each private + shared-but-owned table.
3. Creates a `USING` policy and (for tables with INSERT/UPDATE from handlers) a `WITH CHECK` policy.

- [ ] **Step 1: Write the migration**

```sql
-- backend/migrations/033_enable_rls.sql
-- Row-Level Security: defense-in-depth multi-tenant isolation.
--
-- Conventions:
--   * Two custom GUCs drive every policy:
--       app.user_id    — the authenticated user's id, set by the HTTP middleware
--                        in a SET LOCAL inside the per-request tx.
--       app.bypass_rls — 'true' on worker / migration / admin contexts that
--                        legitimately need cross-user access.
--   * current_setting(name, true) returns '' (not error) when unset, so a
--     missing app.user_id casts to NULL and the USING clause fails closed.
--   * Policies are PERMISSIVE (default). One policy per table.

-- Helper: a SQL function so policies are concise and the cast lives in one
-- place. SECURITY INVOKER (default) so it can be inlined by the planner.
CREATE OR REPLACE FUNCTION app_current_user_id() RETURNS INT AS $$
    SELECT NULLIF(current_setting('app.user_id', true), '')::int;
$$ LANGUAGE sql STABLE;

CREATE OR REPLACE FUNCTION app_rls_bypass() RETURNS BOOLEAN AS $$
    SELECT COALESCE(current_setting('app.bypass_rls', true), '') = 'true';
$$ LANGUAGE sql STABLE;

-- ============================================================
-- Private tables: scoped by user_id
-- ============================================================

-- reading_progress
ALTER TABLE reading_progress ENABLE ROW LEVEL SECURITY;
ALTER TABLE reading_progress FORCE ROW LEVEL SECURITY;
CREATE POLICY reading_progress_user_isolation ON reading_progress
    USING (app_rls_bypass() OR user_id = app_current_user_id())
    WITH CHECK (app_rls_bypass() OR user_id = app_current_user_id());

-- playback_progress
ALTER TABLE playback_progress ENABLE ROW LEVEL SECURITY;
ALTER TABLE playback_progress FORCE ROW LEVEL SECURITY;
CREATE POLICY playback_progress_user_isolation ON playback_progress
    USING (app_rls_bypass() OR user_id = app_current_user_id())
    WITH CHECK (app_rls_bypass() OR user_id = app_current_user_id());

-- user_preferences  (note: some legacy rows may have NULL user_id; allow bypass)
ALTER TABLE user_preferences ENABLE ROW LEVEL SECURITY;
ALTER TABLE user_preferences FORCE ROW LEVEL SECURITY;
CREATE POLICY user_preferences_user_isolation ON user_preferences
    USING (app_rls_bypass() OR user_id = app_current_user_id())
    WITH CHECK (app_rls_bypass() OR user_id = app_current_user_id());

-- user_tags
ALTER TABLE user_tags ENABLE ROW LEVEL SECURITY;
ALTER TABLE user_tags FORCE ROW LEVEL SECURITY;
CREATE POLICY user_tags_user_isolation ON user_tags
    USING (app_rls_bypass() OR user_id = app_current_user_id())
    WITH CHECK (app_rls_bypass() OR user_id = app_current_user_id());

-- article_user_tags
ALTER TABLE article_user_tags ENABLE ROW LEVEL SECURITY;
ALTER TABLE article_user_tags FORCE ROW LEVEL SECURITY;
CREATE POLICY article_user_tags_user_isolation ON article_user_tags
    USING (app_rls_bypass() OR user_id = app_current_user_id())
    WITH CHECK (app_rls_bypass() OR user_id = app_current_user_id());

-- tag_suggestion_dismissals
ALTER TABLE tag_suggestion_dismissals ENABLE ROW LEVEL SECURITY;
ALTER TABLE tag_suggestion_dismissals FORCE ROW LEVEL SECURITY;
CREATE POLICY tag_suggestion_dismissals_user_isolation ON tag_suggestion_dismissals
    USING (app_rls_bypass() OR user_id = app_current_user_id())
    WITH CHECK (app_rls_bypass() OR user_id = app_current_user_id());

-- interest_topics
ALTER TABLE interest_topics ENABLE ROW LEVEL SECURITY;
ALTER TABLE interest_topics FORCE ROW LEVEL SECURITY;
CREATE POLICY interest_topics_user_isolation ON interest_topics
    USING (app_rls_bypass() OR user_id = app_current_user_id())
    WITH CHECK (app_rls_bypass() OR user_id = app_current_user_id());

-- interest_tags
ALTER TABLE interest_tags ENABLE ROW LEVEL SECURITY;
ALTER TABLE interest_tags FORCE ROW LEVEL SECURITY;
CREATE POLICY interest_tags_user_isolation ON interest_tags
    USING (app_rls_bypass() OR user_id = app_current_user_id())
    WITH CHECK (app_rls_bypass() OR user_id = app_current_user_id());

-- interest_categories
ALTER TABLE interest_categories ENABLE ROW LEVEL SECURITY;
ALTER TABLE interest_categories FORCE ROW LEVEL SECURITY;
CREATE POLICY interest_categories_user_isolation ON interest_categories
    USING (app_rls_bypass() OR user_id = app_current_user_id())
    WITH CHECK (app_rls_bypass() OR user_id = app_current_user_id());

-- user_insights
ALTER TABLE user_insights ENABLE ROW LEVEL SECURITY;
ALTER TABLE user_insights FORCE ROW LEVEL SECURITY;
CREATE POLICY user_insights_user_isolation ON user_insights
    USING (app_rls_bypass() OR user_id = app_current_user_id())
    WITH CHECK (app_rls_bypass() OR user_id = app_current_user_id());

-- article_events
ALTER TABLE article_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE article_events FORCE ROW LEVEL SECURITY;
CREATE POLICY article_events_user_isolation ON article_events
    USING (app_rls_bypass() OR user_id = app_current_user_id())
    WITH CHECK (app_rls_bypass() OR user_id = app_current_user_id());

-- weekly_digests
ALTER TABLE weekly_digests ENABLE ROW LEVEL SECURITY;
ALTER TABLE weekly_digests FORCE ROW LEVEL SECURITY;
CREATE POLICY weekly_digests_user_isolation ON weekly_digests
    USING (app_rls_bypass() OR user_id = app_current_user_id())
    WITH CHECK (app_rls_bypass() OR user_id = app_current_user_id());

-- daily_digests
ALTER TABLE daily_digests ENABLE ROW LEVEL SECURITY;
ALTER TABLE daily_digests FORCE ROW LEVEL SECURITY;
CREATE POLICY daily_digests_user_isolation ON daily_digests
    USING (app_rls_bypass() OR user_id = app_current_user_id())
    WITH CHECK (app_rls_bypass() OR user_id = app_current_user_id());

-- hidden_articles
ALTER TABLE hidden_articles ENABLE ROW LEVEL SECURITY;
ALTER TABLE hidden_articles FORCE ROW LEVEL SECURITY;
CREATE POLICY hidden_articles_user_isolation ON hidden_articles
    USING (app_rls_bypass() OR user_id = app_current_user_id())
    WITH CHECK (app_rls_bypass() OR user_id = app_current_user_id());

-- user_ai_configs
ALTER TABLE user_ai_configs ENABLE ROW LEVEL SECURITY;
ALTER TABLE user_ai_configs FORCE ROW LEVEL SECURITY;
CREATE POLICY user_ai_configs_user_isolation ON user_ai_configs
    USING (app_rls_bypass() OR user_id = app_current_user_id())
    WITH CHECK (app_rls_bypass() OR user_id = app_current_user_id());

-- ============================================================
-- Shared-but-owned tables
-- ============================================================

-- feeds: shared rows (owner_id IS NULL) visible to all; owned rows only to owner.
ALTER TABLE feeds ENABLE ROW LEVEL SECURITY;
ALTER TABLE feeds FORCE ROW LEVEL SECURITY;
CREATE POLICY feeds_owner_isolation ON feeds
    USING (
        app_rls_bypass()
        OR owner_id IS NULL
        OR owner_id = app_current_user_id()
    )
    WITH CHECK (
        app_rls_bypass()
        OR owner_id IS NULL
        OR owner_id = app_current_user_id()
    );

-- articles: visibility via parent feed. Note: shared content, so multiple
-- users may legitimately see the same article row.
ALTER TABLE articles ENABLE ROW LEVEL SECURITY;
ALTER TABLE articles FORCE ROW LEVEL SECURITY;
CREATE POLICY articles_via_feed ON articles
    USING (
        app_rls_bypass()
        OR EXISTS (
            SELECT 1 FROM feeds f
            WHERE f.id = articles.feed_id
              AND (f.owner_id IS NULL OR f.owner_id = app_current_user_id())
        )
    )
    WITH CHECK (
        app_rls_bypass()
        OR EXISTS (
            SELECT 1 FROM feeds f
            WHERE f.id = articles.feed_id
              AND (f.owner_id IS NULL OR f.owner_id = app_current_user_id())
        )
    );

-- summary_templates: system templates (user_id IS NULL, is_system=true)
-- visible to all; per-user templates only to owner.
ALTER TABLE summary_templates ENABLE ROW LEVEL SECURITY;
ALTER TABLE summary_templates FORCE ROW LEVEL SECURITY;
CREATE POLICY summary_templates_isolation ON summary_templates
    USING (
        app_rls_bypass()
        OR (is_system = true AND user_id IS NULL)
        OR user_id = app_current_user_id()
    )
    WITH CHECK (
        app_rls_bypass()
        OR user_id = app_current_user_id()  -- handlers cannot create system templates
    );
```

- [ ] **Step 2: Write a smoke test for the migration**

```go
// backend/internal/repository/rls_migration_test.go
package repository_test

import (
    "testing"

    "github.com/bytedance/rss-pal/internal/repository/testdb"
)

// TestMigration033_EnablesRLS verifies that after migrations run, the
// expected tables have RLS enabled.
func TestMigration033_EnablesRLS(t *testing.T) {
    db, cleanup := testdb.New(t)
    defer cleanup()

    expected := []string{
        "reading_progress", "playback_progress", "user_preferences",
        "user_tags", "article_user_tags", "tag_suggestion_dismissals",
        "interest_topics", "interest_tags", "interest_categories",
        "user_insights", "article_events", "weekly_digests",
        "daily_digests", "hidden_articles", "user_ai_configs",
        "feeds", "articles", "summary_templates",
    }

    for _, name := range expected {
        var enabled bool
        err := db.QueryRow(`
            SELECT relrowsecurity
              FROM pg_class
             WHERE relname = $1
               AND relkind = 'r'
        `, name).Scan(&enabled)
        if err != nil {
            t.Errorf("%s: %v", name, err)
            continue
        }
        if !enabled {
            t.Errorf("%s: RLS not enabled", name)
        }
    }
}
```

- [ ] **Step 3: Run the test**

Run: `cd backend && go test ./internal/repository/ -run TestMigration033_EnablesRLS`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add backend/migrations/033_enable_rls.sql backend/internal/repository/rls_migration_test.go
git commit -m "feat(db): migration 033 enables RLS with policies on private + shared-owned tables"
```

### Task 3.2: Verify policies don't break existing repo behavior

Before going further, run the **whole existing test suite** with the new migration applied. If anything broke, fix it before continuing.

- [ ] **Step 1: Run full suite against the RLS-enabled DB**

```bash
cd backend && go test ./...
```
Expected: PASS. Tests that didn't touch the DB are unaffected. Tests that used pure logic (no DB) are unaffected.

- [ ] **Step 2: If anything fails, the failure mode tells you which table needs a worker bypass** — proceed to Phase 4.

---

## Phase 4 — Bypass contexts (worker, public endpoints, admin)

### Task 4.1: Worker sets `app.bypass_rls`

**Files:**
- Modify: `backend/internal/repository/db.go` — add `SetBypass`.
- Modify: `backend/cmd/worker/main.go` — call SetBypass after NewDB.

The worker uses the same `*sql.DB` pool but on every connection it touches, we need `app.bypass_rls = 'true'` to be set **session-wide** (not LOCAL). The cleanest way with `database/sql`'s pool is to use the driver's connection-init hook:

`lib/pq` doesn't have a connection hook in the driver, but we can wrap the DSN with `default_transaction_isolation` etc. Since GUCs need to be set per-connection at acquisition, we use a different approach: set the GUC via the DSN's `options` parameter.

`postgres://...?options=-c%20app.bypass_rls%3Dtrue` — this passes `-c app.bypass_rls=true` to the backend at startup, which becomes a session-default. Every connection in the pool inherits it.

- [ ] **Step 1: Write a test for the worker DSN helper**

```go
// backend/internal/repository/db_test.go
package repository_test

import (
    "net/url"
    "strings"
    "testing"

    "github.com/bytedance/rss-pal/internal/repository"
)

func TestWithBypass_AddsOptionsParam(t *testing.T) {
    in := "postgres://postgres:postgres@localhost:5432/rsspal?sslmode=disable"
    out := repository.DSNWithBypass(in)
    u, err := url.Parse(out)
    if err != nil {
        t.Fatalf("parse: %v", err)
    }
    opts := u.Query().Get("options")
    if !strings.Contains(opts, "app.bypass_rls=true") {
        t.Fatalf("expected options to include app.bypass_rls=true, got %q", opts)
    }
}
```

- [ ] **Step 2: Run, expect compile failure**

Run: `cd backend && go test ./internal/repository/ -run TestWithBypass_AddsOptionsParam`
Expected: FAIL — undefined: DSNWithBypass.

- [ ] **Step 3: Implement**

In `backend/internal/repository/db.go`:

```go
// DSNWithBypass returns dsn with the `options` query parameter extended so
// that every connection opened against it inherits app.bypass_rls=true. Use
// this for worker / migration contexts that must legitimately cross users.
func DSNWithBypass(dsn string) string {
    u, err := url.Parse(dsn)
    if err != nil {
        return dsn // best-effort: don't break startup on a malformed dsn
    }
    q := u.Query()
    existing := q.Get("options")
    add := "-c app.bypass_rls=true"
    if existing == "" {
        q.Set("options", add)
    } else if !strings.Contains(existing, "app.bypass_rls=") {
        q.Set("options", existing+" "+add)
    }
    u.RawQuery = q.Encode()
    return u.String()
}
```

Also, **change `NewDB`** to take a flag (or add a sibling `NewBypassDB`):

```go
// NewBypassDB opens a *sql.DB whose connections all have app.bypass_rls=true.
// Use only from the worker and from one-shot CLI tools.
func NewBypassDB(cfg *config.DatabaseConfig) (*sql.DB, error) {
    dsn := dsnFromCfg(cfg)
    dsn = DSNWithBypass(dsn)
    db, err := sql.Open("postgres", dsn)
    if err != nil {
        return nil, err
    }
    if err := db.Ping(); err != nil {
        return nil, err
    }
    return db, nil
}
```

(Extract the DSN-from-config logic into a private helper if not already.)

- [ ] **Step 4: Update worker `main.go`**

Change:
```go
db, err := repository.NewDB(&cfg.Database)
```
to:
```go
db, err := repository.NewBypassDB(&cfg.Database)
```

Same for `cmd/backfill_content/main.go`, `cmd/backfill_metrics/main.go`, `cmd/seed/main.go`. Audit each — if it does cross-user work, it needs `NewBypassDB`.

- [ ] **Step 5: Test worker startup smoke**

```go
// backend/internal/repository/db_test.go (append)
func TestNewBypassDB_BypassesRLS(t *testing.T) {
    db, cleanup := testdb.New(t)
    defer cleanup()

    // Manually set bypass on this connection and verify a private table is readable.
    if _, err := db.Exec(`SELECT set_config('app.bypass_rls', 'true', false)`); err != nil {
        t.Fatalf("set bypass: %v", err)
    }
    // Insert into a private table with someone else's user_id.
    if _, err := db.Exec(`INSERT INTO users (username, password_hash) VALUES ('a', 'x'), ('b', 'y')`); err != nil {
        t.Fatalf("seed users: %v", err)
    }
    if _, err := db.Exec(`INSERT INTO reading_progress (article_id, progress, user_id) VALUES (1, 0.5, 1), (1, 0.5, 2)`); err != nil {
        // article_id=1 may not exist; relax the test:
        t.Skipf("seed reading_progress: %v (expected: foreign key)", err)
    }
    var n int
    if err := db.QueryRow(`SELECT COUNT(*) FROM reading_progress`).Scan(&n); err != nil {
        t.Fatalf("count: %v", err)
    }
    if n != 2 {
        t.Fatalf("bypass should see both rows, got %d", n)
    }
}
```

(Adjust the seed to use a real article — easier to seed an article first; the engineer should make this test concrete.)

- [ ] **Step 6: Run + commit**

```bash
cd backend && go test ./internal/repository/...
git add backend/internal/repository/db.go backend/internal/repository/db_test.go \
        backend/cmd/worker/main.go backend/cmd/backfill_content/main.go \
        backend/cmd/backfill_metrics/main.go backend/cmd/seed/main.go
git commit -m "feat(worker): worker DB connections bypass RLS via options DSN param"
```

### Task 4.2: Public-token endpoints set `app.user_id` manually

Endpoints that bypass JWT auth and resolve a user from a token:
- `share.go` — public link tokens; the share creator's user_id is recoverable from the token.
- `bookmarklet.go` — bookmarklet token in `users.bookmarklet_token` maps to a user.
- `extension_ingest.go` — same as bookmarklet, plus signed Chrome-extension requests.
- `article_images.go` — currently public; serves PDF page images. Either keep it public with a path-signed URL, or require the JWT.

Each of these handlers needs to begin a tx and `SET LOCAL app.user_id` itself, since they're not behind `RLSTxMiddleware`.

- [ ] **Step 1: Add a helper**

```go
// backend/internal/api/rls.go (append)

// RunInTxAsUser opens a transaction on db, sets app.user_id=uid via SET LOCAL,
// runs fn with the tx, and commits or rolls back. Use for endpoints that
// resolve a user from a token instead of JWT.
func RunInTxAsUser(db *sql.DB, ctx context.Context, uid int, fn func(*sql.Tx) error) error {
    tx, err := db.BeginTx(ctx, nil)
    if err != nil {
        return err
    }
    if _, err := tx.Exec(`SELECT set_config('app.user_id', $1, true)`, uid); err != nil {
        _ = tx.Rollback()
        return err
    }
    if err := fn(tx); err != nil {
        _ = tx.Rollback()
        return err
    }
    return tx.Commit()
}
```

- [ ] **Step 2: Convert each public-token handler**

For `share.go` GetByToken:
1. Look up `share_tokens.created_by` (or `articles.feed_id → feeds.owner_id`) to find the owning user.
2. Wrap the article fetch in `RunInTxAsUser(db, ctx, ownerID, func(tx) { repo.WithTx(tx)... })`.

For `bookmarklet.go` and `extension_ingest.go`:
1. They already resolve `ownerID` from the token (see `GetByBookmarkletToken`).
2. Wrap the rest of the handler in `RunInTxAsUser(db, ctx, ownerID, ...)`.

For `article_images.go`:
- Look up `articles.feed_id → feeds.owner_id` to derive the owner.
- Wrap the file-system serve in the same helper.

This is fiddly because each handler currently calls the repo on the global pool. The minimal change is: at the top of the handler, after resolving `ownerID`, do `tx, _ := db.BeginTx(...); defer tx.Rollback(); tx.Exec("SET LOCAL app.user_id = ownerID"); c.Set("tx", tx)`; then call `repo.WithCtx(c).Method(...)` as elsewhere.

- [ ] **Step 3: Per-endpoint test**

For each converted endpoint, write a test that:
1. Creates two users (A, B).
2. Creates a share token for A's article.
3. Without auth, calls `/api/share/:token` and expects A's article.
4. Creates a share token for an article in B's private feed.
5. Without auth, calls `/api/share/:token` for B's token and expects B's article (works because the share resolves the right ownerID).
6. Negative: calls `/api/share/:bogus` and expects 404.

Place tests in `backend/internal/api/share_rls_test.go` and similar.

- [ ] **Step 4: Build + test + commit**

```bash
cd backend && go build ./... && go test ./...
git add backend/internal/api/
git commit -m "feat(api): public-token endpoints run inside a tx with SET LOCAL app.user_id"
```

### Task 4.3: Admin endpoints

`admin.go` endpoints check `isAdmin` and operate on backup files. They mostly don't query private tables, but the **restore** endpoint may need bypass to write into other users' tables.

- [ ] **Step 1: Audit admin.go**

For each admin handler, check whether any SQL it runs (directly or transitively) needs to touch other users' rows. Backups (pg_dump) run as the OS-level Postgres user via `exec.Command` so bypass is automatic. The restore handler shells out to `psql` similarly — also bypassed at OS level.

- [ ] **Step 2: For any admin handler that DOES run in-process SQL across users, add bypass**

If found, the cleanest pattern is: when `isAdmin` is true in `AuthMiddleware`, the `RLSTxMiddleware` also sets `app.bypass_rls='true'` via `SELECT set_config('app.bypass_rls', 'true', true)`. **But this is dangerous** — it makes the admin's normal browsing also bypass RLS. Better: introduce a separate route group `adminGroup := router.Group("/api/admin"); adminGroup.Use(api.RLSBypassMiddleware(db))` that sets bypass per-request, and use this group ONLY for cross-user admin actions.

If no admin handler currently needs cross-user SQL, skip this — Phase 4.3 is a no-op.

- [ ] **Step 3: Commit if any changes**

```bash
git add backend/internal/api/admin.go backend/cmd/server/main.go
git commit -m "feat(admin): cross-user admin endpoints bypass RLS via dedicated middleware"
```

---

## Phase 5 — Cross-user leak end-to-end tests

These are the tests that prove RLS works. They are the most important deliverable of the whole plan.

**Files:**
- Create: `backend/internal/api/rls_leak_test.go`

### Task 5.1: Two-user fixture + isolation matrix

- [ ] **Step 1: Build a helper that creates two users with realistic data**

```go
// backend/internal/api/rls_leak_test.go
package api_test

import (
    "database/sql"
    "testing"

    "github.com/bytedance/rss-pal/internal/repository/testdb"
)

type rlsFixture struct {
    db          *sql.DB
    userA, userB int
    feedA, feedB int    // private feeds (owner_id = userA / userB)
    sharedFeed   int    // owner_id IS NULL
    articleA, articleB, articleShared int
}

func newRLSFixture(t *testing.T) (*rlsFixture, func()) {
    db, cleanup := testdb.New(t)

    f := &rlsFixture{db: db}
    must := func(_ sql.Result, err error) {
        t.Helper()
        if err != nil {
            t.Fatalf("seed: %v", err)
        }
    }
    err := db.QueryRow(`INSERT INTO users (username, password_hash) VALUES ('a', 'x') RETURNING id`).Scan(&f.userA)
    if err != nil { t.Fatalf("user A: %v", err) }
    err = db.QueryRow(`INSERT INTO users (username, password_hash) VALUES ('b', 'y') RETURNING id`).Scan(&f.userB)
    if err != nil { t.Fatalf("user B: %v", err) }

    err = db.QueryRow(`INSERT INTO feeds (url, title, owner_id) VALUES ('http://a', 'A', $1) RETURNING id`, f.userA).Scan(&f.feedA)
    if err != nil { t.Fatalf("feed A: %v", err) }
    err = db.QueryRow(`INSERT INTO feeds (url, title, owner_id) VALUES ('http://b', 'B', $1) RETURNING id`, f.userB).Scan(&f.feedB)
    if err != nil { t.Fatalf("feed B: %v", err) }
    err = db.QueryRow(`INSERT INTO feeds (url, title) VALUES ('http://shared', 'S') RETURNING id`).Scan(&f.sharedFeed)
    if err != nil { t.Fatalf("shared feed: %v", err) }

    err = db.QueryRow(`INSERT INTO articles (feed_id, title, url, published_at) VALUES ($1, 'a1', 'http://a/1', NOW()) RETURNING id`, f.feedA).Scan(&f.articleA)
    if err != nil { t.Fatalf("article A: %v", err) }
    err = db.QueryRow(`INSERT INTO articles (feed_id, title, url, published_at) VALUES ($1, 'b1', 'http://b/1', NOW()) RETURNING id`, f.feedB).Scan(&f.articleB)
    if err != nil { t.Fatalf("article B: %v", err) }
    err = db.QueryRow(`INSERT INTO articles (feed_id, title, url, published_at) VALUES ($1, 's1', 'http://s/1', NOW()) RETURNING id`, f.sharedFeed).Scan(&f.articleShared)
    if err != nil { t.Fatalf("article shared: %v", err) }

    // Per-user state
    must(db.Exec(`INSERT INTO reading_progress (article_id, progress, user_id) VALUES ($1, 0.5, $2), ($3, 0.5, $4)`,
        f.articleA, f.userA, f.articleB, f.userB))
    must(db.Exec(`INSERT INTO hidden_articles (article_id, user_id) VALUES ($1, $2)`, f.articleShared, f.userA))
    must(db.Exec(`INSERT INTO user_tags (user_id, name) VALUES ($1, 'tagA'), ($2, 'tagB')`, f.userA, f.userB))

    return f, cleanup
}

// asUser runs fn in a tx with app.user_id = uid (no bypass).
func asUser(t *testing.T, db *sql.DB, uid int, fn func(*sql.Tx)) {
    t.Helper()
    tx, err := db.BeginTx(t.Context(), nil)
    if err != nil { t.Fatalf("begin: %v", err) }
    defer tx.Rollback()
    if _, err := tx.Exec(`SELECT set_config('app.user_id', $1, true)`, uid); err != nil {
        t.Fatalf("set: %v", err)
    }
    fn(tx)
}
```

- [ ] **Step 2: Write the isolation matrix tests**

```go
func TestRLS_FeedsAreScoped(t *testing.T) {
    f, cleanup := newRLSFixture(t)
    defer cleanup()

    asUser(t, f.db, f.userA, func(tx *sql.Tx) {
        var ids []int
        rows, _ := tx.Query(`SELECT id FROM feeds ORDER BY id`)
        defer rows.Close()
        for rows.Next() {
            var id int
            _ = rows.Scan(&id)
            ids = append(ids, id)
        }
        // userA sees: shared + own. Not B's.
        want := map[int]bool{f.feedA: true, f.sharedFeed: true}
        for _, id := range ids {
            if !want[id] {
                t.Errorf("userA leaked feed id=%d", id)
            }
        }
        if len(ids) != 2 {
            t.Errorf("userA sees %d feeds, want 2", len(ids))
        }
    })
}

func TestRLS_ArticlesAreScoped(t *testing.T) {
    f, cleanup := newRLSFixture(t)
    defer cleanup()

    asUser(t, f.db, f.userB, func(tx *sql.Tx) {
        var n int
        _ = tx.QueryRow(`SELECT COUNT(*) FROM articles WHERE id = $1`, f.articleA).Scan(&n)
        if n != 0 {
            t.Errorf("userB can see userA's private article")
        }
        _ = tx.QueryRow(`SELECT COUNT(*) FROM articles WHERE id = $1`, f.articleShared).Scan(&n)
        if n != 1 {
            t.Errorf("userB cannot see shared article")
        }
    })
}

func TestRLS_ReadingProgressIsScoped(t *testing.T) {
    f, cleanup := newRLSFixture(t)
    defer cleanup()

    asUser(t, f.db, f.userA, func(tx *sql.Tx) {
        var n int
        _ = tx.QueryRow(`SELECT COUNT(*) FROM reading_progress`).Scan(&n)
        if n != 1 {
            t.Errorf("userA should see exactly 1 reading_progress row, got %d", n)
        }
    })
}

func TestRLS_InsertWithWrongUserIDIsBlocked(t *testing.T) {
    f, cleanup := newRLSFixture(t)
    defer cleanup()

    asUser(t, f.db, f.userA, func(tx *sql.Tx) {
        _, err := tx.Exec(`INSERT INTO user_tags (user_id, name) VALUES ($1, 'forged')`, f.userB)
        if err == nil {
            t.Fatalf("userA forging row for userB should fail RLS WITH CHECK")
        }
    })
}

func TestRLS_NoUserIDSetSeesNothingPrivate(t *testing.T) {
    f, cleanup := newRLSFixture(t)
    defer cleanup()

    // No SET app.user_id at all.
    tx, _ := f.db.BeginTx(t.Context(), nil)
    defer tx.Rollback()

    var n int
    _ = tx.QueryRow(`SELECT COUNT(*) FROM reading_progress`).Scan(&n)
    if n != 0 {
        t.Errorf("unauthenticated session leaks reading_progress (got %d rows)", n)
    }
}
```

- [ ] **Step 3: Run**

Run: `cd backend && go test ./internal/api/ -run TestRLS_`
Expected: PASS (all 5).

- [ ] **Step 4: Extend the matrix for every private table**

Add similar tests for `hidden_articles`, `user_tags`, `playback_progress`, `article_events`, `user_insights`, `weekly_digests`, `daily_digests`, `interest_topics`, `interest_tags`, `interest_categories`, `user_preferences`, `user_ai_configs`, `tag_suggestion_dismissals`, `article_user_tags`. Pattern is identical: seed two users' rows, switch context, count.

- [ ] **Step 5: HTTP-level cross-user leak test**

Spin up the real router (`gin.New()` + handler registration) and do JWT-as-A → call A's endpoints → JWT-as-B → call same endpoints with A's ids in path → expect 404/403. Place in `backend/internal/api/rls_http_leak_test.go`.

Pick 6 critical endpoints to cover:
1. `GET /api/articles/:id` (article detail)
2. `GET /api/articles/:id/content`
3. `POST /api/articles/:id/save` (and subsequent unsave)
4. `POST /api/articles/:id/hide`
5. `GET /api/feeds/:id`
6. `DELETE /api/feeds/:id`

- [ ] **Step 6: Commit**

```bash
git add backend/internal/api/rls_leak_test.go backend/internal/api/rls_http_leak_test.go
git commit -m "test(api): cross-user RLS leak matrix + critical HTTP endpoint coverage"
```

---

## Phase 6 — Deployment + docs

### Task 6.1: Apply migration to production

- [ ] **Step 1: Take a backup**

```bash
docker-compose exec postgres pg_dump -U postgres rsspal > backups.pre-migrate-033-$(date +%Y%m%d-%H%M%S).sql
```

- [ ] **Step 2: Verify the backup is readable**

```bash
head -50 backups.pre-migrate-033-*.sql | head
```

- [ ] **Step 3: Dry-run on a copy**

```bash
docker-compose exec postgres psql -U postgres -c 'CREATE DATABASE rsspal_dryrun_033 TEMPLATE rsspal'
docker-compose exec -T postgres psql -U postgres -d rsspal_dryrun_033 < backend/migrations/033_enable_rls.sql
docker-compose exec postgres psql -U postgres -d rsspal_dryrun_033 -c '\dt+' # sanity
docker-compose exec postgres psql -U postgres -c 'DROP DATABASE rsspal_dryrun_033'
```

- [ ] **Step 4: Apply to live DB**

```bash
docker-compose exec -T postgres psql -U postgres -d rsspal < backend/migrations/033_enable_rls.sql
```

- [ ] **Step 5: Rebuild + restart**

```bash
docker-compose up -d --build api worker
docker-compose logs -f api | head -50  # watch for errors
```

- [ ] **Step 6: Smoke-test**

Log in as admin via the frontend, view feeds, view an article. Then:
- Create a second user via invite code.
- Log in as user-2 in a private window.
- Verify user-2 cannot see admin's private feeds (only shared ones).

### Task 6.2: Documentation

- [ ] **Step 1: Append to CLAUDE.md**

Add a section "## Multi-tenant rules" with the following content:

```markdown
## Multi-tenant rules (RLS)

Every per-user table has Postgres Row-Level Security enabled. The HTTP middleware (`api.RLSTxMiddleware`) opens a transaction per request and sets `app.user_id` so policies filter rows automatically. The worker sets `app.bypass_rls=true` on its connections (via DSN `options`) to perform cross-user batch work.

**When adding a new table:**
1. If it stores per-user state, give it `user_id INT NOT NULL REFERENCES users(id) ON DELETE CASCADE`.
2. In the same migration, enable RLS: `ALTER TABLE foo ENABLE ROW LEVEL SECURITY; ALTER TABLE foo FORCE ROW LEVEL SECURITY;`
3. Create the standard policy: `CREATE POLICY foo_user_isolation ON foo USING (app_rls_bypass() OR user_id = app_current_user_id()) WITH CHECK (app_rls_bypass() OR user_id = app_current_user_id());`
4. Add a test in `backend/internal/api/rls_leak_test.go` that creates two users' rows and verifies isolation.

**When adding a new HTTP endpoint:**
- If behind `/api/` JWT auth, do nothing — middleware sets `app.user_id`.
- If public + token-resolved (share, bookmarklet, extension), use `api.RunInTxAsUser(db, ctx, ownerID, ...)` to bind a tx with the resolved user.

**When adding a new worker task:**
- Pass `Querier`-style repos as usual. The worker's pool has `app.bypass_rls=true` set as a session default; queries see all rows by default.
```

- [ ] **Step 2: Update README backend section to mention `TEST_DB_URL`**

(already done in Task 0.1.)

- [ ] **Step 3: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: multi-tenant RLS rules for future tables and endpoints"
```

---

## Self-review checklist (engineer runs before merge)

- [ ] `go build ./...` clean.
- [ ] `go test ./...` clean against an RLS-enabled DB.
- [ ] `grep -nE "h\.[a-zA-Z]+Repo\.[A-Z]" backend/internal/api/*.go | grep -v WithCtx` returns only public-token / admin handlers, never a JWT-authenticated handler.
- [ ] Every private table listed in the "Definitions & invariants" section appears in `migrations/033_enable_rls.sql` with both `USING` and `WITH CHECK`.
- [ ] Every private table has at least one row in the leak-test matrix.
- [ ] Worker logs show no "permission denied" or "row violates row-level security" errors during a full minute of polling.
- [ ] Two-user smoke test in the actual UI: user B cannot see user A's private feeds, saved articles, tags, or hidden articles.

## Risks & open questions

- **Performance**: RLS subqueries on `articles` (policy joins to feeds) may regress some queries. Re-check `articles_via_feed` policy on a populated DB. If slow, add `ALTER POLICY ... USING (... LEAKPROOF)` is not applicable; instead simplify by adding `articles.owner_id` (denormalized cache of `feeds.owner_id`, kept in sync by trigger) and use that in the policy. Defer this until a profile shows pain.
- **Transaction overhead per request**: `BeginTx` adds ~25–100μs per request. Acceptable for a 100-user app. If we ever go higher, consider per-request `*sql.Conn` pinning with `SET app.user_id` + `RESET` on release.
- **Migration ordering**: migration 033 references all tables created in 001–032. If any of those tables were renamed or dropped, the migration will fail. Engineer must `\dt` the live DB before applying to confirm every table name is current.
- **Existing rows with NULL user_id**: migration 003 added `user_id` with `ADD COLUMN IF NOT EXISTS` (nullable). Any pre-migration rows still have `user_id IS NULL`. These will become invisible to all users after RLS. Before enabling RLS, run a one-off update / cleanup (or admin-attribute) for any nulls. Engineer must audit:

```sql
SELECT 'reading_progress' AS t, COUNT(*) FROM reading_progress WHERE user_id IS NULL
UNION ALL SELECT 'user_preferences', COUNT(*) FROM user_preferences WHERE user_id IS NULL
UNION ALL SELECT 'interest_topics', COUNT(*) FROM interest_topics WHERE user_id IS NULL;
```
Any non-zero row needs a backfill (assign to admin user_id) before the migration can be safely applied.
- **Share tokens & PDF image serving**: Phase 4.2 makes these run-as-owner. Verify the owner relationship resolves correctly (share token → article → feed.owner_id; if shared feed, set app.user_id to share creator instead). Test explicitly.
