# Registration and authentication protection

## Behavior

Registration requires a fresh server-verified Tencent Captcha 2.0 proof and either an unused, unexpired invitation code or an active article-share invitation from an allowed owner. A share link may admit multiple independent ordinary accounts; it never grants the new account access to the sharing owner’s private data. The server validates signed/legacy tokens or short codes and checks current expiry/revocation under a share-row lock in the same transaction as account creation. Public/bootstrap initialization never returns an administrator JWT: administrators sign in with their configured password.

Tencent uses the official `DescribeCaptchaResult` API (`CaptchaType=9`), with success only when `CaptchaCode=1`. Missing configuration, unavailable API, forged/replayed or disaster-recovery tickets reject registration. The frontend dynamically loads `TJCaptcha.js` and opens verification after a validated registration form submit. Cancel/failure permits retry with a new challenge; no registration POST is automatically replayed. An unavailable challenge blocks registration, not ordinary login/reading.

## Configuration

Set in the production environment/secret store, never commit secrets:

- `AUTH_CAPTCHA_PROVIDER=tencent`; explicit `turnstile` remains available for operator rollback, never automatic fallback.
- `TENCENT_CAPTCHA_APP_ID`: public Web captcha instance ID.
- `TENCENT_CAPTCHA_APP_SECRET`: server-only instance secret.
- `TENCENT_CAPTCHA_SECRET_ID` and `TENCENT_CAPTCHA_SECRET_KEY`: server-only cloud API credentials restricted to captcha verification. Configure the instance for `rss.morefreeze.top`, signup use, normal verification; do not enable fail-open disaster tickets.
- `SHARE_REGISTRATION_MODE=selected_owners`: default; admits only owners in `SHARE_REGISTRATION_OWNER_IDS`, e.g. `1` for the verified current production admin. Uses immutable user IDs, not username or administrator flag. An empty/malformed list fails closed.
- Future `SHARE_REGISTRATION_MODE=all` admits all active shares; `disabled` disables only share invitation signup. Ordinary invitation codes remain available in either mode. Unknown modes deny share signup.
- Turnstile rollback only: `TURNSTILE_SITE_KEY`, `TURNSTILE_SECRET`, `TURNSTILE_HOSTNAMES` (exact main hostname).

The frontend obtains only availability, provider and public instance/site key from `/api/auth/registration-config`. Restart/recreate API after environment changes. Public share responses add `registration_allowed`, without disclosing owners. Signup always independently checks the locked share row, so query-string edits cannot override policy. Existing eligible links work without recreation. Revocation/expiry blocks future signup; already-created independent accounts remain.

Compose applies migration 042 before API startup. It creates shared pre-auth budgets without user-scoped RLS; HMAC keys use JWT_SECRET and do not persist raw IPs, usernames, passwords or refresh tokens. All application instances must share the same database and JWT secret. Rotating JWT_SECRET resets budget identities as well as invalidating access tokens.

## Initial rate policy

| Scope | Budget |
| --- | --- |
| Each action, per IP | 120 attempts/minute |
| Each action, global admitted work | 600 attempts/minute |
| Login, per IP | 30 attempts/15 minutes |
| Login, per normalized username | 10 attempts/15 minutes |
| Register, per IP | 5 attempts/hour |
| Register, global after valid human proof | 60 attempts/hour |
| Refresh, per IP | 60 attempts/minute |
| Refresh, per credential | 10 attempts/minute |
| Init, per IP | 5 attempts/hour |

Rejected per-IP/account requests do not consume global admission. Separate action budgets and in-flight slots keep rejected registration work from exhausting login capacity. IPv4-mapped IPv6 is canonicalized; IPv6 addresses share a /64 budget. Fixed windows begin on first use and denied requests do not extend them. Database updates are atomic across replicas and restarts. Expired records are pruned in bounded batches using SKIP LOCKED.

Auth request bodies are capped at 16 KiB. In-flight limits: login 8, refresh 8, registration 4, logout 4, init 2 per API process. Rate-limited responses use 429 + Retry-After; database failures reject with 503. Frontend retains valid remembered-device credentials on 429/503/network failure and only clears them after definitive invalid authentication.

## Operations

- Keep API bound to loopback as in Compose and restrict access to controlled proxies. Gin uses the rightmost untrusted address in X-Forwarded-For; never overwrite this with arbitrary caller headers or expose a trusted-private hop to untrusted clients.
- Do not clear budgets or rotate JWT_SECRET to diagnose an ordinary 429; respect Retry-After. Shared NAT addresses may need observed-data-driven threshold changes.
- A total user-count cap is a separate product decision. Active share links are reusable enrollment invitations; revoking a share closes that enrollment source. Captcha verification and rate limiting reduce abuse and bound admission; they cannot guarantee that every human-assisted signup is legitimate.
