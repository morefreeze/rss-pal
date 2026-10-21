# Explore discovery channels implementation plan

**Goal:** Separate content topics and discovery provenance, retain multiple channels per blog, and filter Explore by channel.

**Architecture:** Derive stable channel identities from existing provider observations. Collapse Reddit week/month into a subreddit channel. Batch enrich visible candidates; filter before pagination. Preserve historical provenance after provider removal. Return configured channel choices independently of the selected filter. Keep authorization, ranking, collection, and snapshots unchanged. Directory provenance topics render as 综合 instead of pretending to be content topics.

**Tech stack:** Go/PostgreSQL, React/TypeScript, Vitest.

- [x] Add repository regression coverage for multi-channel attribution, Reddit deduplication, snapshot/cold/saved filtering, pagination, and topic intersection. Observe failures.
- [x] Add channel DTO and aggregation in `backend/internal/repository/explore_discovery.go`; wire page and saved queries and API channel validation. Use a single batched read, no per-card database calls.
- [x] Add card and page tests for distinct topic/channel rows, multiple badges, query persistence and reset. Observe failures.
- [x] Wire frontend API types, feed hook, URL query, filter control and wrapping badges. Keep existing topic feedback semantics.
- [x] Run focused and full frontend tests/build, repository/API/server tests against isolated PostgreSQL; self-review security, pagination and mobile layout.
- [ ] Integrate current origin/master, push master, verify Deploy Tencent result, deployed revision/health and live UI/API behavior.

Validation commands: `TEST_DB_URL=postgres://postgres:postgres@127.0.0.1:55439/rsspal_test?sslmode=disable go test -p 1 ./internal/repository ./internal/api ./cmd/server`; `npm run check`; `npm run build`.

Verified: repository/API/server full suite and focused Explore/Reddit rerun pass on isolated tmpfs PostgreSQL. Frontend 90 files / 821 tests, 9 legacy checks, build pass. Independent review has no blocking findings. Mobile browser fixture at 390px verifies multiple badges, filter URL and no overflow; production login unavailable in isolated browser.
