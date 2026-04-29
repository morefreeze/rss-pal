# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

RSS Pal is a personal RSS reader service with AI-powered summarization and personalized recommendations. The UI and AI prompts are in Chinese. It consists of a Go backend, a React/TypeScript frontend, and PostgreSQL, all orchestrated via Docker Compose.

## Common Commands

### Docker (recommended)
```bash
docker-compose up -d          # Start all services (postgres, api, worker, frontend)
docker-compose up -d --build  # Rebuild and start
docker-compose logs -f api    # Follow API logs
docker-compose logs -f worker # Follow worker logs
```

### Backend (manual)
```bash
cd backend
go mod tidy
go run ./cmd/server   # API server on :8080
go run ./cmd/worker   # RSS fetch worker (runs in separate terminal)
```

### Frontend (manual)
```bash
cd frontend
npm install
npm run dev    # Dev server with proxy to backend at :8080
npm run build  # Production build
```

### Database
```bash
psql -U postgres -d rsspal -f backend/migrations/001_init.sql  # Manual migration
# Docker Compose auto-runs migrations from backend/migrations/ on first start
```

## Architecture

Two independent Go binaries share the `internal/` package:
- **`cmd/server`** — Gin HTTP API serving `/api/*` routes. Cookie-based auth (`auth_token` cookie checked by middleware).
- **`cmd/worker`** — Background loop polling RSS feeds every 1 minute, fetching full article content, and re-fetching articles with short content (<300 chars).

Backend follows a layered pattern: `api/` (HTTP handlers) → `service/` (business logic) → `repository/` (SQL via `database/sql` + `lib/pq`) → `model/`. There is no ORM; all SQL is handwritten with positional parameters (`$1`, `$2`, ...).

### Key packages
- **`internal/ai`** — Calls Claude API directly over HTTP (model: `claude-3-haiku-20240307`). Generates brief/detailed summaries, extracts topics, and produces insights. Prompts are in Chinese.
- **`internal/rss`** — `Fetcher` parses RSS via `gofeed` with HTTP cache headers (ETag/If-Modified-Since). `ContentFetcher` scrapes full article content from URLs using `goquery`.
- **`internal/config`** — All config from environment variables with sensible defaults.

### Frontend
- React 18 + React Router 6 + Vite. No state management library.
- Single API client at `src/api/client.ts` using axios with `withCredentials: true`.
- Vite dev server proxies `/api` to `localhost:8080`. In production, nginx handles the proxy.
- Routes: `/feeds`, `/articles`, `/articles/:id`, `/insights`, `/stats`.

## Environment Variables

| Variable | Default | Purpose |
|----------|---------|---------|
| `SERVER_PORT` | `8080` | API server port |
| `DB_HOST/PORT/USER/PASSWORD/NAME` | `localhost:5432/postgres/postgres/rsspal` | PostgreSQL connection |
| `DB_SSLMODE` | `disable` | PostgreSQL SSL mode |
| `CLAUDE_API_KEY` | (empty) | Required for AI summaries |
| `AUTH_PASSWORD` | `admin` | Login password |

## Database

PostgreSQL 15. Single migration file at `backend/migrations/001_init.sql`. Tables: `feeds`, `articles`, `user_preferences`, `interest_topics`, `reading_progress`. Migrations auto-run via Docker entrypoint (`/docker-entrypoint-initdb.d`).

## Important Details

- Article deduplication is by `(feed_id, url)` unique constraint.
- Article content has a 50,000 char limit when scraped.
- Recommended articles are scored from `user_preferences` signals (like=5, dislike=-10, save=3, read_duration/60) over the last 30 days.
- Reading progress uses scroll position (0.0–1.0 float) with upsert on `article_id` unique.
- Auth is a simple password check setting a hardcoded cookie value (`"authenticated"`), not a JWT.
