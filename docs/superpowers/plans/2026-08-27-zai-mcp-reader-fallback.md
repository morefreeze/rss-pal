# Z.ai MCP Reader Fallback Implementation Plan

> **For agentic workers:** Use superpowers:executing-plans because the protocol client, fetch routing, API entry point, configuration, and deployment form one tightly coupled change. Track progress with the checkbox steps below.

**Goal:** Add Z.ai Web Reader MCP as the first fallback for every server-side Markdown fetch, use a fresh Reader call for manual re-fetch, retain Jina as the final fallback, and deploy with an independent Reader API key.

**Architecture:** `ContentFetcher` keeps direct extraction as the fast path and delegates insufficient results to a focused MCP client. The MCP client owns the initialize/session/tool-call lifecycle and a two-slot concurrency limit. Automatic callers use Reader cache; the manual article-content endpoint uses a fresh entry point. Reader failure is non-fatal and falls through to the existing Jina path.

**Tech Stack:** Go, JSON-RPC/MCP over HTTP/SSE, Gin, Docker Compose, Z.ai Web Reader MCP, Tencent Cloud.

---

## File Map

- Create `backend/internal/rss/zai_reader.go`: MCP client, response decoding, validation, and concurrency limit.
- Create `backend/internal/rss/zai_reader_test.go`: protocol and failure fixtures.
- Modify `backend/internal/rss/content.go`: direct → Reader → Jina routing and cached/fresh entry points.
- Modify `backend/internal/rss/content_test.go`: routing, cache mode, disabled-key, and Jina fallback tests.
- Modify `backend/internal/api/content.go`: manual re-fetch uses the fresh entry point.
- Modify/add `backend/internal/api/*_test.go`: verify the manual handler selects fresh fetching without changing its response contract.
- Modify `docker-compose.yml`: pass Reader variables to API and worker.
- Modify `.env.example`: document the dedicated Reader configuration.

### Task 1: Implement the Z.ai Web Reader MCP client with protocol tests

**Files:**
- Create: `backend/internal/rss/zai_reader_test.go`
- Create: `backend/internal/rss/zai_reader.go`

- [ ] **Step 1: Write failing MCP lifecycle tests**

Use `httptest.Server` to record three requests and assert this exact sequence:

1. JSON-RPC `initialize` with protocol version `2024-11-05`;
2. `notifications/initialized` carrying the returned `Mcp-Session-Id`;
3. `tools/call` for `webReader` carrying the same session ID.

The fixture returns SSE `data:` payloads and nested Reader JSON. Assert Markdown, title, `return_format=markdown`, `retain_images=true`, and both `no_cache=false` and `no_cache=true` variants.

- [ ] **Step 2: Run the tests and verify RED**

Run: `cd backend && go test ./internal/rss -run 'TestZAIReader' -count=1 -v`

Expected: FAIL because the Reader client does not exist.

- [ ] **Step 3: Write failing error-class tests**

Cover non-2xx responses, missing session ID, top-level JSON-RPC error, `result.isError=true`, missing text blocks, malformed nested JSON, embedded Reader error, empty content, and context timeout. Assert errors expose only a safe stage/status summary and never the bearer token, session ID, or response body.

- [ ] **Step 4: Implement the minimal MCP client**

Add a package-private client initialized from `ZAI_READER_API_KEY` and optional `ZAI_READER_MCP_URL`, defaulting to `https://api.z.ai/api/mcp/web_reader/mcp`. Send bearer authentication and `Accept: application/json, text/event-stream`, decode JSON or SSE `data:` frames, decode the nested JSON text block, and return `ContentResult`. Bound live calls with a package-wide semaphore of capacity two and make no automatic retries.

- [ ] **Step 5: Run the focused client tests and verify GREEN**

Run: `cd backend && go test ./internal/rss -run 'TestZAIReader' -count=1 -v`

Expected: all MCP lifecycle and failure tests PASS.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/rss/zai_reader.go backend/internal/rss/zai_reader_test.go
git commit -m "feat(rss): add Z.ai MCP reader client"
```

### Task 2: Route server-side content through Reader before Jina

**Files:**
- Modify: `backend/internal/rss/content.go`
- Modify: `backend/internal/rss/content_test.go`

- [ ] **Step 1: Write failing routing tests**

Add table-driven tests proving:

- direct content of at least 300 characters contacts neither Reader nor Jina;
- a short body, non-200 response, or direct transport error tries Reader first;
- automatic calls send `no_cache=false`;
- `FetchContentFresh` sends `no_cache=true`;
- successful Reader content and title are returned;
- every Reader error falls through to Jina;
- an empty `ZAI_READER_API_KEY` never contacts MCP and preserves direct → Jina behavior;
- when all fallbacks fail, existing direct-result/original-transport-error semantics remain unchanged.

Use local `httptest.Server` endpoints for direct, Reader, and Jina traffic; add injectable endpoint/client fields only as needed by the tests.

- [ ] **Step 2: Run the routing tests and verify RED**

Run: `cd backend && go test ./internal/rss -run 'TestContentFetcher.*(Reader|Fresh|Fallback)' -count=1 -v`

Expected: FAIL because Reader routing and `FetchContentFresh` do not exist.

- [ ] **Step 3: Implement cached/fresh routing**

Keep `FetchContent` and `FetchContentWithMetadata` as cached automatic entry points. Add `FetchContentFresh(ctx, url)` for manual re-fetch. Centralize the route in one helper that performs direct extraction, tries Reader once with the requested cache mode, then tries Jina, while preserving title and current terminal return behavior. Log only safe Reader failure summaries.

- [ ] **Step 4: Run focused RSS tests and verify GREEN**

Run: `cd backend && go test ./internal/rss -run 'Test(ContentFetcher|ZAIReader|FetchContent)' -count=1 -v`

Expected: all selected tests PASS, including the pre-existing cleanup and limit tests.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/rss/content.go backend/internal/rss/content_test.go
git commit -m "feat(rss): fall back through Z.ai reader"
```

### Task 3: Make manual article re-fetch bypass Reader cache

**Files:**
- Modify: `backend/internal/api/content.go`
- Modify/add: `backend/internal/api/*_test.go`

- [ ] **Step 1: Write a failing handler test**

Exercise `POST /api/articles/:id/content` with a controlled content fetcher and assert the handler selects the fresh entry point. Preserve the current success payload `{ "content": "..." }`, database update, video rejection, and empty-content behavior.

- [ ] **Step 2: Run the API test and verify RED**

Run: `cd backend && go test ./internal/api -run 'TestContentHandler.*Fresh' -count=1 -v`

Expected: FAIL because the handler still calls `FetchContent`.

- [ ] **Step 3: Change the handler to call `FetchContentFresh`**

Make only the manual endpoint fresh. Do not change feed import, discovery, worker, batch, bookmarklet, DOM, transcript, PDF, or RSS/Atom entry-point behavior.

- [ ] **Step 4: Run focused API tests and verify GREEN**

Run: `cd backend && go test ./internal/api -run 'TestContentHandler' -count=1 -v`

Expected: all selected tests PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/api/content.go backend/internal/api/*content*_test.go
git commit -m "feat(api): refresh article content without reader cache"
```

### Task 4: Wire the dedicated Reader configuration

**Files:**
- Modify: `docker-compose.yml`
- Modify: `.env.example`

- [ ] **Step 1: Add configuration assertions**

Use repository searches to establish the initial absence of `ZAI_READER_API_KEY` and `ZAI_READER_MCP_URL` from both API and worker Compose environments.

- [ ] **Step 2: Add API and worker environment wiring**

Pass `ZAI_READER_API_KEY` and `ZAI_READER_MCP_URL` to both services. Document that the key is dedicated to Reader, must not reuse `CLAUDE_API_KEY`, and that the MCP URL is optional with the production default supplied by the application.

- [ ] **Step 3: Validate the Compose model and example config**

Run: `docker compose config >/dev/null && rg -n 'ZAI_READER_(API_KEY|MCP_URL)' docker-compose.yml .env.example`

Expected: Compose validates; both variables appear in API, worker, and `.env.example`; no real secret is committed.

- [ ] **Step 4: Commit**

```bash
git add docker-compose.yml .env.example
git commit -m "chore(config): wire dedicated Z.ai reader key"
```

### Task 5: Verify the implementation and prepare the deployable revision

**Files:**
- Verify all files in the File Map.

- [ ] **Step 1: Format and run focused tests**

Run: `gofmt -w backend/internal/rss/zai_reader.go backend/internal/rss/zai_reader_test.go backend/internal/rss/content.go backend/internal/rss/content_test.go backend/internal/api/content.go backend/internal/api/*content*_test.go`

Run: `cd backend && go test ./internal/rss ./internal/api -count=1`

Expected: focused packages PASS.

- [ ] **Step 2: Run the complete backend suite**

Run: `cd backend && go test ./... -count=1`

Expected: PASS. If the known transcript timeout recurs, rerun that exact test repeatedly and then rerun the complete suite; do not classify any new failure as a baseline flake.

- [ ] **Step 3: Check the complete diff**

Run: `git diff --check origin/master...HEAD && git status --short && git log --oneline origin/master..HEAD`

Expected: no whitespace errors, no uncommitted implementation files, no secret values, and only the planned files changed.

- [ ] **Step 4: Push and integrate without rewriting history**

Push `codex/zai-mcp-reader`, update against current `origin/master` without force-push, then fast-forward `master` only if its checked-out state and remote ancestry remain safe. Push the exact reviewed commit and record its SHA.

### Task 6: Create the independent key and deploy to Tencent production

**Operational scope:** Z.ai console and `tencent-rss-pal:/opt/rss-pal`.

- [ ] **Step 1: Create the key without exposing it**

Use the Z.ai console to create `rss-pal-reader`. Capture the one-time value directly into a permission-0600 temporary file or clipboard pipeline; never print it. Prove that key can complete initialize → initialized → `webReader` against `https://example.com`, recording only status/content metadata.

- [ ] **Step 2: Back up and update Tencent environment**

Back up `/opt/rss-pal/.env` with a timestamp. Add or replace only `ZAI_READER_API_KEY`; keep the Reader URL unset unless an override is required. Verify the variable is present and non-empty using a boolean/length check only.

- [ ] **Step 3: Deploy the reviewed revision**

Fetch the pushed revision in `/opt/rss-pal`, build API and worker, and recreate them with:

```bash
docker compose -f docker-compose.yml \
  -f docker-compose.override.yml \
  -f docker-compose.override.oci-egress-20260729.yml \
  up -d --build api worker
```

Do not recreate production with fewer Compose files.

- [ ] **Step 4: Verify production state and behavior**

Record evidence that:

- deployed Git SHA equals the pushed reviewed SHA;
- API and worker images/containers were recreated and are healthy;
- both containers have a non-empty Reader key without exposing it;
- worker proxy variables and the three-file Compose configuration remain active;
- API, worker, Tencent-direct, and `https://rss.morefreeze.top` health checks pass;
- a real production server-side short/blocked fetch reaches Reader and returns usable Markdown through the production proxy;
- recent API/worker logs contain no Reader authentication, panic, fatal, or proxy-connection errors.

- [ ] **Step 5: Roll back if verification fails**

Restore the timestamped `.env` backup and previous revision/images, recreate API and worker with the same three Compose files, and repeat health checks. Remove the local temporary key file after success or rollback.

### Task 7: Final evidence report

- [ ] Report the reviewed/deployed SHA, commits, focused/full tests, Z.ai MCP proof, independent-key presence check, Compose provenance, container/image state, direct/public health, real Reader fallback result, logs, and any remaining evidence gaps. Never include either API key or an MCP session ID.
