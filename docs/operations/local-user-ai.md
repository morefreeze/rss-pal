# Personal AI keys stay in the browser

User settings support Save to this browser, Test connection, and Delete. The
unchecked default uses account-scoped sessionStorage; opting to remember uses
account-scoped localStorage. No personal key or endpoint is fetched from or
saved to the API. Changing the endpoint requires entering its key again.

Browser calls use HTTPS OpenAI-compatible `/chat/completions`, no site cookies
or JWT, no redirects, no automatic retry, and no server proxy fallback. A test
makes one small provider request; summary generation makes two requests. All
are explicit actions. Calls need provider CORS support. The personal summary
path uses text/body/subtitles and the existing source-anchor/grounding policy;
it does not upload vision images. Personal keys can also serve explicit prompt
polishing. Site-operated AI (including interest analysis and worker processing)
is independent and never reads these local keys.

The server prepares bounded prompts from an owned article and saves only
completed brief/detailed summaries. Input fingerprints cover content and prior
summary. Saving verifies ownership with the request RLS transaction and uses
atomic content/prior-summary checks under the existing job lease. Failure,
incomplete provider output, account changes, and aborted generation cannot
produce an acknowledged saved result. Success is sent after commit.

Migration 067 clears historical active personal keys and forbids nonempty
values in `user_ai_configs.api_key`. Legacy GET/PUT `/api/settings/ai` returns
410 without reading or echoing keys. No automatic credential download/migration
is provided. Current application JSON backups do not include this configuration.
Independent historical SQL dumps may contain legacy keys: retain restricted
access, rotate affected provider credentials, and manage those archives under
their retention policy. Local unrelated backup artifacts were not changed.
Administrator platform keys are separate and retain their existing handling.

Validation (2026-10-26): frontend 109 files / 963 tests plus 9 legacy checks;
production build; full Go suite with an isolated PostgreSQL 15; targeted real-RLS
HTTP tests for ownership, invalid/extra fields, size, content and summary version
conflicts, failed commit, and database migration replay. Browser QA checked save,
manual-only connectivity test, CORS failure feedback, and 390px layout using
synthetic credentials/provider output. No real paid provider was called. Before
deployment, production had zero nonempty personal AI keys.
