# Public verified account access

User decision: a verified email, Google or Apple identity can register directly; no invitation requirement. Keep existing username/password login and all existing account data. Existing users bind a new identity from Settings → Account after signing in. Equal email strings never merge accounts automatically.

## Implemented behavior

- `/api/auth/methods` reports actual configuration readiness. Unconfigured channels are hidden and fail closed on direct API calls.
- Public registration uses `AUTH_PUBLIC_REGISTRATION=true`. This disables the old unverified `/auth/register` path. New verified identities create non-admin users with no automatic subscriptions.
- Email: six-digit code, 10-minute expiry, five guesses, one-use consumption, 60-second resend delay, persistent per-email/IP/global send budgets; failed sends invalidate the challenge. SMTP requires TLS; public sends require the existing configured captcha.
- Google: authorization code + S256 PKCE + nonce, fixed issuer/endpoints and signed ID-token validation. Apple: form_post callback, secure browser cookie, ES256 client secret and signed ID-token validation.
- OAuth state and completion tickets are browser-bound, expiring and single-use. Provider tokens and app bearer tokens never appear in redirect URLs. Completion uses a short-lived opaque ticket and removes it from browser history. Binding completes only with the same authenticated account.
- Invalid codes/tickets use HTTP400 business errors so retrying does not trigger the application's expired-session interceptor or consume an extra OTP guess.

## Configuration and activation

Production source/runtime: `tencent-rss-pal:/opt/rss-pal`, public origin `https://rss.morefreeze.top`. At pre-deploy inspection on 2026-10-22, none of the new OAuth/SMTP environment keys was present. No live provider or email delivery has been claimed or attempted. Leave missing channels disabled and retain the existing registration mode until a verified channel can actually work; setting the public flag with all channels unavailable would prevent all new registration.

Put credentials in the protected runtime `.env` or existing secret management, never Git or chat:

| Channel | Required configuration | Registered callback |
| --- | --- | --- |
| Common | `AUTH_PUBLIC_ORIGIN=https://rss.morefreeze.top` | HTTPS origin must match |
| Email | independent random `AUTH_OTP_SECRET` of at least 32 characters; `SMTP_HOST`, `SMTP_PORT`, `SMTP_USERNAME`, `SMTP_PASSWORD`, `SMTP_FROM`; existing captcha credentials | sender/domain authorization and actual inbox receipt |
| Google | `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET` | `https://rss.morefreeze.top/api/auth/oauth/google/callback` |
| Apple | `APPLE_CLIENT_ID` (web Services ID), `APPLE_TEAM_ID`, `APPLE_KEY_ID`, `APPLE_PRIVATE_KEY` (P-256 PEM, literal `\n` supported) | `https://rss.morefreeze.top/api/auth/oauth/apple/callback` |

Use an existing authorized sender/provider. Creating paid service accounts, changing DNS or sending acceptance email requires the provider/destination to be supplied or authorized. Apple web setup requires a Services ID associated with a primary App ID enabled for Sign in with Apple; configure the exact domain/return URL and key. See [Google OIDC](https://developers.google.com/identity/openid-connect/openid-connect) and [Apple web setup](https://developer.apple.com/help/account/capabilities/configure-sign-in-with-apple-for-the-web/).

After configuring one or more channels, deploy the API using the established release workflow and check `/api/auth/methods`. Set `AUTH_PUBLIC_REGISTRATION=true` for the authorized verified signup release, then verify actual new signup/login, existing-account binding, sign-out/sign-in retention of subscriptions/reading data, and mobile Safari callback behavior. Test unavailable/mismatched/replayed flows. Verify a second account cannot take over or see the first account's data. Do not substitute signed test fixtures for provider consent and inbox-delivery evidence.

## Verification

- Backend repository/API/config/server suites with isolated real PostgreSQL; focused auth suites under race detector, Go vet, server/worker builds.
- Full mocked Google and Apple authorization/start/callback/exchange fixtures including signed JWKS tokens, issuer/audience/signature/expiry/nonce failures, state/ticket replay, PKCE, Apple signing/cookie claims, switched-owner binding and app-role database access.
- Frontend full checks/build; OTP retry, owner switch, no invitation/password in public signup, retained subscription intent, callback StrictMode one-use exchange and session preservation.
- Local browser fixture at 375px: no horizontal overflow, email/code inputs16px, all verified-access buttons44px. This proves the built UI layout, not real email/Google/Apple availability or a physical iPhone run.
