# Admin article model configuration

Approved in chat on 2026-09-28. Six providers: OpenAI, Z.AI/智谱, DeepSeek, Alibaba Bailian, Anthropic/Claude, xAI/Grok.

Admin navigation gains AI 模型配置. Small (<= threshold) and large (> threshold) article selectors each choose provider then fetch models using that provider's key. Threshold defaults to 10000 Unicode characters and is editable. Existing GLM small/large defaults are preserved. Credentials are managed once per provider, encrypted at rest with a server secret, never returned to browsers. Fixed official endpoint choices prevent credentials being sent to arbitrary URLs. Changing endpoint requires a key belonging to it; current server Z.AI credentials are only inherited for the exact current endpoint.

Persist a versioned global singleton configuration in PostgreSQL with admin/bypass RLS. Save atomically; reject stale revisions. API and worker read the persisted configuration at the start of each article operation. Selection occurs before truncation and is pinned through brief/detailed, retries and fallbacks; errors loading persisted settings fail the operation instead of silently using another company. Personal AI overrides remain isolated from global routing. Nonarticle digests and interests remain on existing settings.

Fetch current model lists server-side with bounded requests and pagination, official endpoints only. Return explicit errors for credentials/network/unsupported catalogs, preserve the saved model during refresh failures, and label any official public-catalog fallback accurately. Lists filter out known non-chat models. Saving requires keys and valid selections; no network inference is needed merely to configure. Separate saved configuration from draft UI, and discard stale list requests after company changes.

Most companies use OpenAI chat completions; Claude uses native Messages with JSON/SSE translation and provider-reported input/cache/output token recording. Vision calls must never reuse another company's model/key. Unknown model pricing remains incomplete, not zero or fabricated.

Validation: boundary routing, independent providers, concurrency and fallback pinning, provider discovery pagination/errors, credential redaction/encryption and admin authorization, Claude JSON/stream usage/errors, browser selection/save/reload, backend race tests and frontend build. Deploy API, worker and frontend only, verify runtime hashes/health and saved default settings without triggering unnecessary paid summaries.
