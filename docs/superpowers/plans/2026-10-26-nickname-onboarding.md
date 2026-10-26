# One-time nickname setup

Goal: Prompt email/external signup accounts without nicknames after login. Allow skipping for the current browser session and completing setup from account settings. Existing named accounts are not prompted.

Design: Keep login usernames unchanged. Add nickname and nickname_setup_required to users; backfill externally created users and set the flag for new external accounts. Authenticated PUT /auth/nickname accepts a trimmed 1–32 character nickname with explicit confirmation; an atomic conditional UPDATE enforces one successful setup across devices. User payloads include nickname and needs_nickname. Show nickname with username fallback in navigation.

- [x] Add failing API tests for validation, confirmation, persistence, account isolation and concurrent setup.
- [x] Add migration, deployment migration wiring, repository atomic update, authenticated handler and user payload fields.
- [x] Add UI tests for skip, red warning, second confirmation, cancellation, errors and locked settings.
- [x] Implement shared nickname setup component, login prompt, settings entry and navigation display.
- [x] Run backend tests with PostgreSQL, frontend checks/build and deployment regression checks; review diff.
- [ ] Integrate into current origin/master, push and verify Deploy Tencent revision, health and served assets.

Verification: frontend 104 suites / 927 tests and 9 legacy tests passed; production build passed. PostgreSQL-backed API and repository full suites passed, plus focused nickname tests after review fixes. Server package tests and deployment service-selection/bundle regression tests passed. Browser visual checks confirmed warning, confirmation and session skip. Review corrected nested-dialog scroll locking and deferred success until transaction commit.
