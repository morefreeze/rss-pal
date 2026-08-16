# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

RSS Pal is a personal RSS reader service with AI-powered summarization and personalized recommendations. The UI and AI prompts are in Chinese. It consists of a Go backend, a React/TypeScript frontend, and PostgreSQL, all orchestrated via Docker Compose with an RSSHub sidecar.

## Delivery Workflow

For implementation changes in this repository, once development and the relevant full verification are complete, merge the completed feature branch into `master`, push `master`, and deploy to Tencent production unless the user explicitly opts out. Do not stop at an integration-choice prompt.

- A `master` push triggers `.github/workflows/deploy-tencent.yml`; wait for the workflow to finish and verify its exact head commit.
- Treat `tencent-rss-pal:/opt/rss-pal` as the production runtime and connect directly from the local machine unless the user explicitly requests another path.
- Verify the remote revision, running containers, local/public API health, and the public frontend bundle or affected UI as applicable before reporting completion.
- Preserve unrelated working-tree files, including untracked backup artifacts.

## Common Commands

### Docker (recommended)
```bash
docker-compose up -d          # Start all services (postgres, rsshub, api, worker, frontend)
docker-compose up -d --build  # Rebuild and start
docker-compose logs -f api    # Follow API logs
docker-compose logs -f worker # Follow worker logs
```

### Backend
```bash
cd backend
go mod tidy
go run ./cmd/server   # API server on :8080
go run ./cmd/worker   # RSS fetch worker (runs in separate terminal)
go test ./...         # Run all tests
go test ./internal/rss/...  # Run tests for a single package
```

### Frontend
```bash
cd frontend
npm install
npm run dev    # Dev server with proxy to backend at :8080
npm run build  # Production build (tsc + vite build)
```

### Database
```bash
psql -U postgres -d rsspal -f backend/migrations/001_init.sql  # Manual initial migration
# Docker Compose auto-runs migrations from backend/migrations/ on first start
```

## Architecture

Two primary Go binaries share the `internal/` package:
- **`cmd/server`** — Gin HTTP API serving `/api/*` routes. JWT-based auth (`golang-jwt/jwt/v4` with HS256, 7-day expiry). Middleware extracts user from JWT in `Authorization: Bearer` header.
- **`cmd/worker`** — Background loop polling RSS feeds every 1 minute with concurrency limits (5 feeds, 3 content fetches, 2 AI summaries). Also runs: transcript backfill, summary backfill, article classification, link-set candidate detection, daily interest generation, and daily DB backups.

Additional binaries: `cmd/backfill_content`, `cmd/backfill_metrics`, `cmd/seed`.

Backend follows a layered pattern: `api/` (HTTP handlers) → `service/` (business logic) → `repository/` (SQL via `database/sql` + `lib/pq`) → `model/`. There is no ORM; all SQL is handwritten with positional parameters (`$1`, `$2`, ...).

### Key backend packages
- **`internal/ai`** — Calls OpenAI-compatible API (default: Claude Haiku via `CLAUDE_BASE_URL`). Generates brief/detailed summaries, extracts topics, produces interest analyses, classifies articles into categories, and generates weekly digests. Prompts are in Chinese.
- **`internal/rss`** — `Fetcher` parses RSS/Atom via `gofeed` with HTTP cache headers (ETag/If-Modified-Since). `ContentFetcher` scrapes full article content from URLs using `goquery`. Also handles video/media detection, link-set extraction, and HTML feed scraping.
- **`internal/transcript`** — Fetches video/audio transcripts from YouTube CC, Bilibili CC, and HTML page scraping. Returns combined text appended to article content.
- **`internal/backup`** — Scheduled daily PostgreSQL backup (pg_dump) with retention and restore.
- **`internal/service`** — Business logic for feed health scoring, link-set ranking, and summarizer orchestration.
- **`internal/config`** — All config from environment variables with sensible defaults.

### Frontend
- React 18 + React Router 6 + Vite. No state management library.
- Single API client at `src/api/client.ts` using axios with `withCredentials: true`.
- Markdown rendering via `react-markdown` + `remark-gfm` + `rehype-highlight` + `remark-math`/`rehype-katex`.
- Vite dev server proxies `/api` to `localhost:8080`. In production, nginx handles the proxy.
- Audio/video player with playback progress tracking (`src/player/PlayerContext.tsx`).

### RSSHub
- Docker Compose includes a `diygod/rsshub:chromium-bundled` sidecar for routes requiring browser-mode (e.g. Bilibili). Requires `BILIBILI_COOKIE` in `.env` for authenticated routes.
- Authenticated feed preview uses `rss.NewPublicFetcher`: user-controlled, discovered, and probed URLs use the DNS/redirect/rebinding-safe `httpx` client. Only supported-platform URLs safely derived onto the configured RSSHub origin use the trusted RSSHub client, whose redirects are confined to that exact canonical origin. Worker fetches retain `rss.NewFetcher` for trusted stored workflows.

## Environment Variables

| Variable | Default | Purpose |
|----------|---------|---------|
| `SERVER_PORT` | `8080` | API server port |
| `DB_HOST/PORT/USER/PASSWORD/NAME` | `localhost:5432/postgres/postgres/rsspal` | PostgreSQL connection |
| `DB_SSLMODE` | `disable` | PostgreSQL SSL mode |
| `CLAUDE_API_KEY` | (empty) | Required for AI summaries |
| `CLAUDE_BASE_URL` | `https://api.anthropic.com` | OpenAI-compatible API base URL |
| `JWT_SECRET` | `rss-pal-default-secret-change-me` | JWT signing key (change in production) |
| `SHARE_SECRET` | (empty; required by Compose) | Separate share-token signing key; minimum 32 bytes and never reuse `JWT_SECRET` |
| `AUTH_PASSWORD` | `admin` | Initial admin password |
| `RSSHUB_BASE_URL` | `http://rsshub:1200` | RSSHub instance URL |
| `BACKUP_DIR` | `/backups` | Database backup directory |
| `MEDIA_PROXY_URL` | (empty) | Forward proxy for audio relay upstream fetches (GFW-blocked podcast CDNs); production uses the OCI squid tunnel |
| `JINA_API_KEY` | (empty) | Jina API key (if used) |
| `BILIBILI_COOKIE` | (empty) | Bilibili auth cookie for RSSHub |

## Database

PostgreSQL 15. Incremental migrations currently run through `backend/migrations/039_article_shares.sql`. Migrations auto-run via Docker entrypoint (`/docker-entrypoint-initdb.d`) on first start only. For existing volumes, Compose's one-shot `status-migrate` applies migrations 037, 038, and 039 before API and Worker start.

Key tables: `users`, `feeds`, `articles`, `user_preferences`, `interest_topics`, `interest_categories`, `reading_progress`, `playback_progress`, `article_events`, `user_insights`, `user_tags`, `article_user_tags`, `saved_articles`, `feed_health_metrics`, `weekly_digests`, `article_shares`, `ai_templates`, `link_set_candidates`, and the subscription Explore tables.

## Important Details

- Article deduplication is by `(feed_id, url)` unique constraint.
- Article content has a 50,000 char limit when scraped.
- Recommended articles are scored from `user_preferences` signals (like=5, dislike=-10, save=3, read_duration/60) over the last 30 days.
- Reading progress uses scroll position (0.0–1.0 float) with upsert on `article_id` unique.
- Auth is JWT-based (HS256). First run creates admin via `/api/auth/init`. Other users register via invite codes (`/api/auth/register`).
- Feeds support two types: `rss` (standard RSS/Atom) and `html` (scrapes arbitrary web pages for article links).
- Articles can be classified by AI into categories defined in `model.ValidCategories`.
- Link sets: feeds with `expand_links=true` have the worker extract linked articles as child articles (`parent_article_id`).
- The module path is `github.com/bytedance/rss-pal` (Go 1.25).

## Article sharing contract

- Owner-scoped management routes are `POST /api/articles/:id/shares`, `GET /api/articles/:id/shares`, and idempotent `DELETE /api/articles/:id/shares/:share_id`. Multiple links per article are supported and management responses report `active`, `expired`, or `revoked`.
- Public routes are `GET /api/share/:token` for the immutable allowlisted snapshot and `GET /api/share/:token/assets/:asset` for token-protected local PDF assets. All invalid, unknown, tampered, expired, revoked, and database-failure cases use the same public 404. Both responses are `no-store`; page and asset limits are independently 60/minute/IP and 240/minute/IP.
- `SHARE_SECRET` signs versioned tokens, must be at least 32 bytes, and is deliberately independent from `JWT_SECRET`. Legacy compatibility accepts only exactly eight ASCII alphanumeric characters, hashes them with SHA-256 for lookup, and migration 039 grants migrated links a 30-day grace period.
- Snapshots never expose ownership, feed URL, reader state, editor notes, processing errors, or manual tags. Remote media bytes are not archived; only their safe snapshot metadata is retained. Local PDF image references are rewritten to the token-protected asset route.
- Public-reader authentication links use normalized `intent=use` or `intent=subscribe&source=...`; no caller-controlled `next` is honored. After login/registration, a safe subscription source is prefilled and previewed exactly once but never auto-subscribed.

## Multi-tenant rules (RLS)

Per-user tables have Postgres Row-Level Security enabled (migration 033). The HTTP middleware `api.RLSTxMiddleware` opens a transaction per JWT-authenticated request and sets `app.user_id` via `set_config(..., true)` so policies filter rows automatically. Bookmarklet and extension token endpoints use `api.PublicTokenMiddleware` to derive an owner before accessing RLS-protected data. The worker sets `app.bypass_rls=true` on its pool DSN (via `repository.NewBypassDB`) so cross-user batch work isn't blocked.

`article_shares` is an intentional exception and has no RLS: signed-token resolution must happen before any user identity exists. Public reads may access only its immutable wire-safe snapshot. Every owner-facing create/list/revoke path must first authorize the article through the request's RLS transaction and must filter `created_by`; keep both obligations covered by repository and HTTP tests.

The runtime role `rsspal_app` is NOSUPERUSER NOBYPASSRLS (migration 034). Until the operator switches `.env` (see the Phase 3 prep section below), the app connects as `postgres` and RLS is paper-only — every test that needs to verify enforcement uses `testdb.NewAsApp(t, schema)` to open a `rsspal_app`-bound connection.

### When adding a new table

1. If the table stores per-user state, give it `user_id INT NOT NULL REFERENCES users(id) ON DELETE CASCADE`.
2. In the same migration, enable RLS and add the standard policy:
   ```sql
   ALTER TABLE foo ENABLE ROW LEVEL SECURITY;
   ALTER TABLE foo FORCE ROW LEVEL SECURITY;
   CREATE POLICY foo_user_isolation ON foo
       USING (app_rls_bypass() OR user_id = app_current_user_id())
       WITH CHECK (app_rls_bypass() OR user_id = app_current_user_id());
   ```
3. The `ALTER DEFAULT PRIVILEGES` from migration 034 ensures `rsspal_app` automatically gets DML; no extra GRANT needed.
4. Add a row to the `TestRLS_PrivateTablesAreScoped` matrix in `backend/internal/repository/rls_leak_test.go` so the leak suite covers the new table.

For "shared-but-owned" tables (rows reachable by multiple users via an ownership chain like `feeds.owner_id`), model after `articles_via_feed` in migration 033.

### When adding a new repository

Follow the canonical `Querier + WithCtx` pattern (see `backend/internal/repository/article.go`). The struct field is `db Querier`. Add a `WithCtx(c ctxkey.CtxGetter) *FooRepository` method that returns a copy bound to the per-request tx (key: `ctxkey.Tx`) or the receiver if no tx is in context. Preserve all auxiliary struct fields.

If a repository method opens its own inner transaction (`r.db.Begin()`), use `txOrBegin(r.db)` from `backend/internal/repository/tx.go` so a wrapping outer tx is reused with no-op commit/rollback. **Callers MUST propagate errors** from these methods — swallowing the error inside an outer tx commits partial state.

### When adding a new HTTP endpoint

- **JWT-authenticated route** (under `apiGroup` in `cmd/server/main.go`): nothing extra. `AuthMiddleware` + `RLSTxMiddleware` already set `userID` on the gin context and stash the tx under `ctxkey.Tx`. Repositories pick it up via `WithCtx(c)`.
- **Bookmarklet/extension public-token route** (registered on root `router`): preserve `api.PublicTokenMiddleware(db, yourHandler.ResolveOwner)`. The resolver receives `(c *gin.Context, tx *sql.Tx)` and returns `(userID int, err error)`, derives the owner from the existing bookmarklet/extension token record, and returns `api.ErrPublicTokenInvalid` for invalid tokens so middleware emits 401. Article sharing is not this pattern: its signed token resolves an `article_shares` snapshot directly and all unavailable cases must remain the uniform public 404.
- **Best-effort writes inside a handler**: do NOT use `_ = repo.X(...)`. A failure inside the outer tx poisons the whole transaction. Use `bestEffort(c, "label", func() error { return repo.X(...) })` from `backend/internal/api/savepoint.go` — it opens a SAVEPOINT and rolls back to it on failure, leaving the outer tx healthy.

### When adding a new worker task

- Worker pool already bypasses RLS via `app.bypass_rls=true` in its DSN (`repository.NewBypassDB`). Repos accept `*sql.DB` directly; `WithCtx` is a no-op without a tx in context.
- If the task spawns a goroutine that outlives the originating request (e.g. async insight generation in `runAsyncManual`), do NOT use `WithCtx(c)` on that goroutine's repo calls — the request tx is gone by the time the goroutine runs. Use the bare repo and accept that the goroutine runs with whatever bypass state is on the worker pool.

### When adding a new CLI under `backend/cmd/`

- Use `repository.NewBypassDB(&cfg.Database)` if the CLI does cross-user work (e.g. `cmd/worker`, `cmd/backfill_content`, `cmd/backfill_metrics`, `cmd/seed`, `cmd/backfill_image_dimensions`).
- The API server (`cmd/server`) and any future per-user CLI uses plain `NewDB`.

### Testing isolation

For DB-level isolation tests, use `testdb.NewWithSchema(t)` for the privileged seeding handle plus `testdb.NewAsApp(t, schema)` for a `rsspal_app`-bound handle that's actually subject to RLS. See `backend/internal/repository/rls_leak_test.go` for the canonical fixture. For HTTP-level tests, stand up the gin router against a `rsspal_app` pool and exercise endpoints with signed JWTs; see `backend/internal/api/rls_http_leak_test.go`.

The default `testdb.New(t)` opens a SUPERUSER connection with `app.bypass_rls=true` baked into its DSN — use it for migration smoke tests, schema introspection, or anything that does NOT need to prove RLS enforcement. Don't use it to verify isolation; the bypass will mask leaks.

## Multi-tenant DB role (Phase 3 prep)

Migration 034 creates a `rsspal_app` Postgres role with `NOSUPERUSER NOBYPASSRLS`. Until production `.env` is switched, the app continues connecting as `postgres` (superuser, bypasses RLS). To make RLS load-bearing in production:

1. Apply migration 034: `docker-compose exec -T postgres psql -U postgres -d rsspal < backend/migrations/034_app_role_and_grants.sql`
2. Set a real password: `docker-compose exec postgres psql -U postgres -c "ALTER ROLE rsspal_app PASSWORD '<strong-password>'"`
3. Update `.env`: `DB_USER=rsspal_app` and `DB_PASSWORD=<strong-password>`. Also set `DB_ADMIN_USER=postgres` and `DB_ADMIN_PASSWORD=<original postgres password>` so backup/restore (which uses a separate `*sql.DB` via `repository.NewAdminDB`) continues to work with the privilege it needs.
4. Restart: `docker-compose up -d --build api worker`
5. Smoke-test isolation with a second invited user.
