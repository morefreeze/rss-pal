# Admin article model configuration implementation plan

> **For agentic workers:** Execute provider protocol and frontend tasks in isolated scopes; parent handles configuration, routing, integration, review and deployment.

**Goal:** Administrators select providers and dynamically discovered models separately for small and large articles.

**Architecture:** PostgreSQL global settings plus encrypted per-provider credentials; server-side model discovery; per-article immutable summarizer selection shared by API/worker. Claude adapts native Messages into the existing JSON/SSE consumer contract.

**Tech Stack:** Go net/http/database/sql, PostgreSQL, React/TypeScript, Vite.

## Contract

`GET /api/admin/ai` -> `{revision, threshold, small:{provider,model}, large:{provider,model}, providers:[{id,name,endpoints:[{id,name}],endpoint,has_key}]}`.

`PUT /api/admin/ai` takes `{revision,threshold,small,large,credentials:{[provider]:{endpoint,api_key}}}`; omitted/empty key preserves only same-endpoint credentials; returns refreshed redacted GET shape. Stale revision ->409, invalid ->400, storage failure->503.

`POST /api/admin/ai/models` takes `{provider,endpoint,api_key?}`; uses draft key or saved key for that endpoint, returns `{models:[{id,name}],source,warning?}`. Bounded request, no credential echo. Missing key is400, upstream failures502.

- [ ] Add provider catalog/discovery and encrypted settings store in `backend/internal/airouting/`; migration048 uses FORCE RLS, bypass policy. Test filtering, pagination, upstream failure, secret roundtrip, invalid endpoint and revision behavior.
- [ ] Add `backend/internal/api/admin_ai.go`, admin-only GET/PUT/models with no-store and bounded bodies/timeouts; wire server. Verify unauthorized access, redacted responses, invalid payloads.
- [ ] Add explicit resolver to only default summarizers, context-aware selection before truncation, preserved routing errors, selected-company vision isolation; configure both binaries. Verify 10000/10001 Unicode boundary, independent endpoints, pinning, personal overrides and races.
- [ ] Add Claude adapter in `backend/internal/ai/anthropic.go`, used by doAdmitted; convert native JSON/SSE to the existing interface with actual input/cache/output usage. Test real httptest headers/body, partial stream/error/close.
- [ ] Add `frontend/src/pages/AdminAIPage.tsx` and CSS, typed `frontend/src/api/adminAI.ts`, navigation and route. Accessible provider/key controls, small/large cards, auto list loading, explicit refresh, stale response guards, save success/error and revision conflict.
- [ ] Run `go test -race ./internal/airouting ./internal/ai ./internal/aiusage ./internal/api`, frontend build and UI validation. Review spec compliance then correctness; fix actionable defects.
- [ ] Build Linux binaries/frontend, commit source, prepare incremental patch from4adc0c8, inspect remote source compatibility, migrate and deploy only three affected services. Verify runtime/health/browser and source parity; record limitations honestly.
