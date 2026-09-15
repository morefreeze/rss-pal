# Registration and authentication abuse protection

> **For agentic workers:** Execute inline with executing-plans; registration, middleware, frontend errors and deployment form one security boundary.

**Goal:** Prevent concurrent invite reuse and bound unauthenticated authentication work; require server-verified Turnstile for stranger-facing registration.

**Architecture:** Preserve single-use invitations until a separate public-share admission policy is implemented. PostgreSQL holds atomic expiring attempt budgets shared by replicas/restarts; middleware caps auth request bodies and in-flight work before parsing/password hashing. Turnstile is verified inside registration with exact hostname/action checks and fail-closed errors. Never return an administrator credential from public bootstrap.

**Tech Stack:** Go 1.25, Gin, PostgreSQL 15, React/TypeScript, Cloudflare Turnstile.

## Execution checklist

- [x] Reproduce concurrent invite consumption with real PostgreSQL; fix `Register` using `SELECT ... FOR UPDATE` and guarded consumption in the same transaction. Check rollback after duplicate username and expired invites.
- [x] Add `042_auth_rate_limits.sql` and repository tests proving atomic limits across independent pools, expiration and failure behavior. Store HMAC identifiers, never plaintext passwords/tokens/IPs. Apply migration before API startup.
- [x] Add auth middleware and route tests: persistent global/IP/account budgets, IPv4-mapped IPv6 normalization and IPv6 /64 grouping, 16 KiB body cap, unsupported payload rejection, bounded per-action local concurrency, 429 plus Retry-After, and 503 on limiter failure. Protect login/register/refresh/logout/init.
- [x] Add Turnstile verifier tests with local HTTP upstream: reject missing, failed, replayed, wrong-action, wrong-host, malformed and unavailable responses; verify before consuming invites. Add explicit runtime public config, default fail closed without keys. Use action `signup` and production hostname `rss.morefreeze.top`.
- [x] Add React widget lifecycle and auth error tests; require fresh proof after every registration attempt, retain form fields and post-auth intent, display 429/503 accurately.
- [x] Run focused red/green checks, real DB integration, Go suite/race/vet, frontend check/build, and deployment-script tests. Review diff for bypasses, sensitive outputs and unrelated files.
- [ ] Per repository Delivery Workflow, merge into master, push, wait for exact-head Tencent workflow and verify revision, container status, direct/public health and frontend asset. Validate live missing-proof rejection without creating unsolicited accounts. Real Turnstile success/replay validation requires a browser-issued proof.

## Initial policy (adjustable after observed traffic)

Each auth action has independent 600/min global and 120/min per-client admission budgets; rejected clients never consume global capacity. Login: 30/15min per client and 10/15min per normalized account. Registration: 5/hour per client and 60/hour globally after successful human verification. Refresh: 60/min per client and 10/min per refresh credential. Logout: shared auth budget. Init: 5/hour per client. IPv6 clients share a /64 budget. Rate limit state is durable and denies requests on storage failure.

## Delivery boundaries

No public shared-link invite bypass is introduced. Turnstile configuration is required for registration; verification downtime blocks new registrations but not login/reading. Existing single-use invitations remain mandatory. No claim of absolute bot elimination: CAPTCHA plus durable rate limits increases cost and bounds throughput. Full public enrollment still needs an explicit admission/total-user policy.

## Verified results

- Before fix: one invite concurrently created 12 users. After fix: exactly one; rollback and expiry cases pass.
- Review regressions reproduced global-budget exhaustion by one denied IP, cleanup deleting renewed rows, and refresh 429/503/network failure clearing credentials. All pass after fixes; independent review rechecked those fixes with no remaining P1/P2.
- Go full suite passed against isolated PostgreSQL 15 on port 55432; authentication/repository race checks and `go vet ./...` passed.
- Frontend: 566 Vitest cases and 9 legacy checks passed; production build passed. Dependency installation used the existing lockfile in this isolated worktree.
- Deployment service-selection, proxy-ready, direct-network and real nginx template checks passed; migration ordering now includes 042.
- Cloudflare widget created for rss.morefreeze.top only (public sitekey 0x4AAAAAAE1xAT8jJo4y7hKT, managed mode, pre-clearance off). Secret transfer pending user-created local env file because native terminal UI access was denied.
