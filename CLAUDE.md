# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

RSS Pal is a personal RSS reader service with AI-powered summarization and personalized recommendations. The UI and AI prompts are in Chinese. It consists of a Go backend, a React/TypeScript frontend, and PostgreSQL, all orchestrated via Docker Compose with an RSSHub sidecar.

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
- **`cmd/worker`** — Background loop polling RSS feeds every 1 minute with concurrency limits (5 feeds, 3 content fetches, 2 AI summaries). Also runs: transcript backfill, summary backfill, article classification, link-set candidate detection, daily insight generation, and daily DB backups.

Additional binaries: `cmd/backfill_content`, `cmd/backfill_metrics`, `cmd/seed`.

Backend follows a layered pattern: `api/` (HTTP handlers) → `service/` (business logic) → `repository/` (SQL via `database/sql` + `lib/pq`) → `model/`. There is no ORM; all SQL is handwritten with positional parameters (`$1`, `$2`, ...).

### Key backend packages
- **`internal/ai`** — Calls OpenAI-compatible API (default: Claude Haiku via `CLAUDE_BASE_URL`). Generates brief/detailed summaries, extracts topics, produces insights, classifies articles into categories, and generates weekly digests. Prompts are in Chinese.
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

## Environment Variables

| Variable | Default | Purpose |
|----------|---------|---------|
| `SERVER_PORT` | `8080` | API server port |
| `DB_HOST/PORT/USER/PASSWORD/NAME` | `localhost:5432/postgres/postgres/rsspal` | PostgreSQL connection |
| `DB_SSLMODE` | `disable` | PostgreSQL SSL mode |
| `CLAUDE_API_KEY` | (empty) | Required for AI summaries |
| `CLAUDE_BASE_URL` | `https://api.anthropic.com` | OpenAI-compatible API base URL |
| `JWT_SECRET` | `rss-pal-default-secret-change-me` | JWT signing key (change in production) |
| `AUTH_PASSWORD` | `admin` | Initial admin password |
| `RSSHUB_BASE_URL` | `http://rsshub:1200` | RSSHub instance URL |
| `BACKUP_DIR` | `/backups` | Database backup directory |
| `JINA_API_KEY` | (empty) | Jina API key (if used) |
| `BILIBILI_COOKIE` | (empty) | Bilibili auth cookie for RSSHub |

## Database

PostgreSQL 15. Incremental migrations at `backend/migrations/001_init.sql` through `022_link_set_candidates.sql`. Migrations auto-run via Docker entrypoint (`/docker-entrypoint-initdb.d`) on first start only — subsequent migrations need manual application.

Key tables: `users`, `feeds`, `articles`, `user_preferences`, `interest_topics`, `interest_categories`, `reading_progress`, `playback_progress`, `article_events`, `user_insights`, `user_tags`, `article_user_tags`, `saved_articles`, `feed_health_metrics`, `weekly_digests`, `share_tokens`, `ai_templates`, `link_set_candidates`.

## Important Details

- Article deduplication is by `(feed_id, url)` unique constraint.
- Article content has a 50,000 char limit when scraped.
- Recommended articles are scored from `user_preferences` signals (like=5, dislike=-10, save=3, read_duration/60) over the last 30 days.
- Reading progress uses scroll position (0.0–1.0 float) with upsert on `article_id` unique.
- Auth is JWT-based (HS256). First run creates admin via `/api/auth/init`. Other users register via invite codes (`/api/auth/register`).
- Feeds support two types: `rss` (standard RSS/Atom) and `html` (scrapes arbitrary web pages for article links).
- Articles can be classified by AI into categories defined in `model.ValidCategories`.
- Link sets: feeds with `expand_links=true` have the worker extract linked articles as child articles (`parent_article_id`).
- The module path is `github.com/bytedance/rss-pal` (Go 1.24).
