# Tencent registration and selected share owners

Approved by user on 2026-09-16. Extends the existing share-registration plan.

Goal: replace public signup challenge with Tencent Captcha 2.0 and restrict share invitations to the configured owner IDs, with future all/disabled modes.

Architecture: preserve RegistrationVerifier interface; encode Tencent ticket and randstr as JSON proof inside captcha_response. Provider selected by AUTH_CAPTCHA_PROVIDER. Secrets stay server-side. Selected share owners enforced inside locked registration transaction; public share response exposes only registration_allowed boolean, never owner IDs.

Execution: subagent-driven implementation for independent backend verifier while root handles browser provisioning and owner policy/frontend integration. Each area gets failing tests before code, then spec and quality review before delivery.

- [x] Implement Tencent verifier with official API signing/SDK, bounded timeout and strict success-only acceptance; cover missing/malformed proof, rejected ticket, API errors, transport errors and missing configuration. Keep Turnstile adapter for rollback without automatic fallback.
- [x] Add policy modes selected_owners (default, empty deny), all, disabled. Parse positive configured owner IDs; repository lock reads created_by and checks policy. Test allowlisted owner, other owner/admin rejected, all accepted, disabled rejected, revoked/expired rejected.
- [x] Add registration_allowed to public share wire response only after resolving active share. Only eligible links pass shareRef into auth flow; direct forged signup still blocked server-side.
- [x] Frontend Tencent 2.0 dynamically loaded; validated form submit launches challenge; callback supplies ticket/randstr to server. Failure/cancel/loading timeout permits retry but never registers. Preserve existing auth intent and regular invite flow.
- [ ] Configure Tencent console instance and server credentials safely, identify current share owner read-only, wire Compose and docs. Do not print secrets or purchase resources without approval.
- [ ] Run focused and full frontend/backend checks serially against test DB, review spec then code, merge/push/deploy only with production configuration ready. Verify exact deployed commit, health and original share page. No CAPTCHA solving or production test-account creation.

## Verification 2026-09-16

- Backend full `go test -p 1 ./... -count=1`: passed, real local Postgres and restricted app-role fixtures.
- Focused race registration/captcha/share policy tests: passed; `go vet ./...`: passed.
- Frontend 583 Vitest tests, 9 legacy tests (including real nginx), production build: passed.
- Independent spec and quality reviews passed after correcting pre-challenge form validation.
- Tencent instance 190958346: Web/App, account scene, always challenge, slider, balanced risk, exact main hostname + enforced domain checking. Free 20,000-use package active 2026-09-16 to 2026-09-23; no purchase or postpaid enrollment.
- Production owner read-only verified: share o6XKeCZTDZ22 created_by=1, username admin, is_admin=true.
- Deployment pending local secret file completion; no production switch until credentials verified.
