# Registration and authentication protection

## Behavior

Registration requires BOTH an unused, unexpired invitation and a fresh Cloudflare Turnstile proof. Sharing links still carry post-auth navigation intent only; they are not invitations. Public/bootstrap initialization never returns an administrator JWT: administrators sign in with their configured password.

Turnstile uses `signup` and an exact server-configured hostname allowlist. Missing configuration, unavailable Siteverify, invalid hostname/action, forged or replayed proof all reject registration before invitation consumption. Registration POSTs are never automatically replayed; the widget is replaced after each attempt. An unavailable challenge blocks registration, not ordinary login/reading.

## Configuration

Set in the production environment/secret store, never commit secrets:

- `TURNSTILE_SITE_KEY`: public sitekey for the RSS Pal registration widget.
- `TURNSTILE_SECRET`: server-only verification secret.
- `TURNSTILE_HOSTNAMES`: exact frontend hostname, `rss.morefreeze.top` in production. Do not allow localhost, wildcards or caller-supplied Host headers.

The frontend obtains the public key from `/api/auth/registration-config` at runtime. No frontend rebuild is needed when rotating only the widget keypair, but restart/recreate API to reload its environment.

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
- A total user-count cap and a share-link enrollment policy are separate product decisions. Turnstile and rate limiting reduce abuse and bound admission; they cannot guarantee that every human-assisted signup is legitimate.
