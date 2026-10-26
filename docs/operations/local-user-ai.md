# Personal AI keys stay in the browser

User settings support Save to this browser, Test connection, and Delete. The
unchecked default uses account-scoped sessionStorage; opting to remember uses
account-scoped localStorage. No personal key or endpoint is fetched from or
saved to the API. Changing the endpoint requires entering its key again.

Browser calls use HTTPS OpenAI-compatible `/chat/completions` or the exact official Claude `/v1/messages` endpoint, no site cookies
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


## Verified endpoint presets (2026-10-26)

AI settings now offers official endpoint presets, plus editable Custom. Selection
alone never saves a key or sends a provider request. An endpoint change clears
the current draft key; replacing a saved endpoint still requires its new key.
Existing custom configurations remain supported. Models remain user-selected:
an endpoint being browser-accessible does not prove model access or account quota.

Live Chromium requests from the production site origin verified readable invalid-key
responses for OpenAI, DeepSeek, SiliconFlow, OpenRouter, Bailian Beijing/Singapore,
Moonshot, xAI and Groq. Claude requires native Messages with `x-api-key`,
`anthropic-version: 2023-06-01` and `anthropic-dangerous-direct-browser-access: true`.
Its OpenAI compatibility endpoint did not pass browser CORS. Native protocol is
selected only by exact match to `https://api.anthropic.com/v1`; a custom lookalike
origin never receives these headers. Only text content blocks are saved; thinking
blocks and incomplete outputs are excluded.

GLM domestic/international pay-as-you-go and Coding endpoints failed preflight;
they appear disabled with a cross-origin limitation, rather than being labelled
usable. Both GLM Anthropic compatibility routes also failed. Using a GLM model
through another platform requires that platform's key, never a Zhipu key.

The evidence file `evidence/2026-10-26-personal-ai-cors.json` retains the exact
URLs, request header sets, browser outcomes and curl preflight cross-checks.
Browser results take precedence when a non-browser curl request is treated
differently by a WAF. These are CORS checks using invalid probe keys, not successful
paid inference. Users must still click Test connection with their own provider key.
