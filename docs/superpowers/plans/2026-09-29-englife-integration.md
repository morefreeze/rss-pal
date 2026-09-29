# englife Integration Implementation Plan

> **For agentic workers:** Use subagent-driven-development for the separately owned browser bridge/UI work; root implements Go service and integration. Review specification before code quality.

**Goal:** Administrator-authorized shared englife session for YouTube enrichment.
**Architecture:** Browser extension obtains explicit consent and redeems a single-use pairing token. PostgreSQL stores encrypted session and persistent upstream jobs. Go service is a resumable fallback provider. React administers connection state.
**Tech Stack:** Go/Gin/PostgreSQL, React/TypeScript, Chrome MV3, AES-GCM.

## Contracts

- GET /api/admin/integrations/englife returns `{state, account?, checked_at?, remaining?, jobs: [{video_id,status,updated_at}], configured}`; state is setup_required, disconnected, ready, expired, quota_exhausted, unavailable.
- POST same path + /pair returns `{token,expires_at}`; POST + /check returns status; DELETE base disconnects.
- POST /api/integrations/englife/complete uses `{token,cookies:[{name,value,domain,path,secure,expirationDate?}]}`, no bearer; returns `{ok:true}`. Token is secret, limited to this one operation.
- Page bridge: window.postMessage `{source:'rss-pal-englife-page',type:'PING'|'AUTHORIZE',requestId,token?,serverOrigin?}` to same origin. Extension responds `{source:'rss-pal-englife-extension',requestId,ok,code?}`. AUTHORIZE only opens extension confirmation, success does not mean connected. Page polls read-only status while pairing is active.
- Extension allows production https://rss.morefreeze.top, plus http://localhost:5173 and http://127.0.0.1:5173 for development; backend is same-origin /api. Explicit extension-owned consent is mandatory. No credential payload in page bridge.

## Work

- [x] Root: add failing Go tests for upstream auth, CSRF, document mapping, quota/401 and crypto boundaries; run `go test ./internal/englife` and verify RED.
- [x] Root: implement `backend/internal/englife/{client,store,service}.go`, migration 048, encrypted session, atomic pairing and resumable jobs; run targeted tests to GREEN.
- [x] UI/extension implementer: write failing consent/sender/domain and UI tests, then implement extension/englife files plus AdminEnglifePage, frontend API client, navigation. Run node test and targeted vitest to GREEN.
- [x] Root: add admin handler and public token redemption route; wire fallback into API and worker; use generated-content label; add key to compose/env docs. Verify backend build and relevant regression tests.
- [x] Review specification compliance then security/correctness; fix issues and rerun affected tests.
- [x] Build frontend and review browser rendering; record integration limitations and setup instructions without claiming live session success.

## Verification commands

`cd backend && go test ./internal/englife ./internal/transcript ./internal/api ./cmd/worker ./cmd/server`

`cd frontend && npm test -- test/AdminEnglifePage.test.tsx && npm run build`

`cd extension && node --test englife/*.test.js && npm test`

Do not deploy, log in, submit real videos or overwrite existing checkout changes. All implementation stays in the managed worktree until reviewed.

## Verified outcome (2026-09-29)

- PostgreSQL16 isolated test container on localhost:55439: englife store, API admin/replay/expiry, repository suite, worker/server suites passed.
- Final transcript suite passed. One earlier concurrent run hit an existing 2-second fake yt-dlp timeout; the exact unchanged test then passed three consecutive runs and the full transcript suite passed. No timeout threshold was changed.
- Frontend full suite: 63 files, 615 tests passed. TypeScript/Vite production build passed (existing bundle-size warning).
- Extension full suite: 161 tests passed. Browser preview verified admin page with simulated account/status only.
- Specification review and independent code-quality review approved after fixing connection identity, quota-safe retrieval, recovery health, and reserved fallback execution time.
- Live third-party login and cross-IP session reuse are not verified. Deployment is not performed.
