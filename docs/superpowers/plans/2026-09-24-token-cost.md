# Token cost implementation plan

Goal: Implement the user's selected token-based cost accounting in the production admin dashboard.
Architecture: Capture provider usage for every admitted HTTP response (JSON and SSE), store per-request metadata and price snapshots without prompts or credentials, aggregate exact time windows independently from task quotas. Show USD estimated API value, never present it as Coding subscription charges. Unknown usage/prices remain unknown; no historical backfill.
Tech stack: Go, PostgreSQL, React/TypeScript.

- [ ] Add failing tests for JSON/SSE usage, cached-input subtraction, missing/invalid usage and unknown providers/models.
- [ ] Add usage migration and collector; connect API and worker at startup; request streaming usage.
- [ ] Add aggregation and admin token/rate tables, preserving task quota statistics and unrelated changes.
- [ ] Run focused Go/database/frontend checks and builds, inspect diff.
- [ ] Deploy migration and only affected API/worker/frontend artifacts to Tencent; verify live configuration, usage persistence and admin response/rendering.

Prices verified 2026-09-24: https://docs.z.ai/guides/overview/pricing (USD per million: glm-5.3-flash input .15, cached .03, output .50). Regional BigModel prices are not interchangeable. Retain missing prices for other models/providers.
