# Shortcut direct capture implementation plan

**Goal:** Share from iPhone into the same RSS Pal account regardless of browser or Home Screen app login state.

**Architecture:** A logged-in settings page issues a random, five-minute, single-use connection code. The public shortcut exchanges that code for a capture-only credential and saves it in its own Shortcuts file. Subsequent shares POST the URL directly and display the server result. The public Apple installer contains no account credentials. Settings can revoke all shortcut credentials independently of browser sessions and bookmarklets.

**Tech stack:** Go/Gin, PostgreSQL, React/TypeScript, Apple Shortcuts native workflow.

## Scope and security

- Keep one-button native installation; replace the published workflow only after native execution verifies connection and capture.
- Connection codes and credentials use 32 random bytes and only SHA-256 digests are stored server-side. Pairing creation invalidates an older pending code for that user; exchange consumes it atomically. Revocation removes both active credentials and pending codes.
- Capture credentials authorize only `POST /api/shortcuts/capture`. Reuse the existing public-URL validation, content fetch, user-owned clip persistence, RLS transaction and capture task budget. Force link expansion off.
- Return no-store responses for connection endpoints, never put a long-lived credential in an app link, and never include credentials in the shared shortcut or repository.
- A successful response follows database commit. Failures and revoked connections remain visible in Shortcuts. No automatic browser navigation.

## Execution (inline; components share one connection protocol)

- [x] Add failing Go tests for expired/replayed codes, concurrent exchange, account isolation, revocation and capture-only authorization. Use migrated local PostgreSQL, including the application-role boundary.
- [x] Add migration 057 and focused shortcut credential repository/handlers; wire authenticated settings and public connect/capture routes and capture budget. Run targeted Go tests and server build.
- [x] Add failing React tests for install/connect/revoke states, expiring connection links and no copied credentials. Implement settings connection control with a native shortcut run link carrying only the short-lived code. Retain browser fallback capture page.
- [ ] Author native workflow: distinguish connection input from shared URLs, exchange and persist credential, load credential and POST URL, report success/error without opening a browser. Export exact native workflow and verify on Mac using isolated test account/API; publish an approved Apple share link.
- [x] Run frontend checks/build and backend checks. Request independent review, address findings and recheck affected paths.
- [ ] Integrate into current remote master without changing unrelated local work, push, use established Tencent deployment, verify exact revision, migration, health, installer and authenticated capture isolation. Report physical iPhone verification separately.

## Verification commands

`cd backend && go test ./internal/api ./internal/repository -run Shortcut -count=1 -v`

`cd backend && go test ./internal/api ./internal/repository ./cmd/server`

`cd frontend && npm run check && npm run build`

Native smoke checks must exercise the exported shortcut itself, rather than infer behavior from plist contents. Published record must have approved signing and match the exported workflow.

## Verification and release status

- Full API/repository/server checks passed on isolated PostgreSQL 16; frontend 82 files / 759 tests and nine legacy tests passed, with production build.
- Independent review found and fixed a token/user lock-order deadlock and transaction-time expiry extension. Deterministic concurrency tests fail with old logic and pass with the corrected implementation.
- The signed 36-action workflow was imported and executed in Shortcuts.app on macOS. An isolated backend account paired successfully; a second native invocation loaded the saved credential, POSTed a Chinese-path URL with query and fragment, and committed one private clip. The test changes only API base URL and credential file name, leaving all workflow actions identical. Native macOS privacy prompts for network, file save and notifications were exercised.
- The final production workflow has been imported as 加入 RSS Pal. Apple sharing is currently gated by a system “Unlock Mac” prompt; the new public record must be obtained and checked before mainline integration/deployment. Physical iPhone and the user's real account remain device acceptance checks.
