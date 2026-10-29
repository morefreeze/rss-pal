# Public verified account access

User decision: a verified email, Google or Apple identity can register directly; no invitation requirement. Keep existing username/password login and all existing account data. Existing users bind a new identity from Settings → Account after signing in. Equal email strings never merge accounts automatically.

## Implemented behavior

- `/api/auth/methods` reports actual configuration readiness. Unconfigured channels are hidden and fail closed on direct API calls.
- Public registration uses `AUTH_PUBLIC_REGISTRATION=true`. This disables the old unverified `/auth/register` path. New verified identities create non-admin users with no automatic subscriptions.
- Email: six-digit code, 10-minute expiry, five guesses, one-use consumption, 60-second resend delay, persistent per-email/IP/global send budgets; failed sends invalidate the challenge. SMTP requires TLS; Tencent SES uses the official SDK with a signed HTTPS transactional template request and no automatic retries. Public sends require the existing configured captcha.
- Google: authorization code + S256 PKCE + nonce, fixed issuer/endpoints and signed ID-token validation. Apple: form_post callback, secure browser cookie, ES256 client secret and signed ID-token validation.
- OAuth state and completion tickets are browser-bound, expiring and single-use. Provider tokens and app bearer tokens never appear in redirect URLs. Completion uses a short-lived opaque ticket and removes it from browser history. Binding completes only with the same authenticated account.
- Invalid codes/tickets use HTTP400 business errors so retrying does not trigger the application's expired-session interceptor or consume an extra OTP guess.

## Configuration and activation

Production source/runtime: `tencent-rss-pal:/opt/rss-pal`, public origin `https://rss.morefreeze.top`. At pre-deploy inspection on 2026-10-22, none of the new OAuth/SMTP environment keys was present. On 2026-10-23, Tencent SES Hong Kong domain and sender were verified, template 227021 passed review, and one authorized console test reached morefreeze@gmail.com (delivery at 18:31:06, user confirmed receipt). This is provider-delivery evidence; app OTP acceptance must be verified separately. Leave missing channels disabled and retain the existing registration mode until a verified channel can actually work; setting the public flag with all channels unavailable would prevent all new registration.

Put credentials in the protected runtime `.env` or existing secret management, never Git or chat:

| Channel | Required configuration | Registered callback |
| --- | --- | --- |
| Common | `AUTH_PUBLIC_ORIGIN=https://rss.morefreeze.top` | HTTPS origin must match |
| Email | independent random `AUTH_OTP_SECRET` of at least 32 characters; `SMTP_HOST`, `SMTP_PORT`, `SMTP_USERNAME`, `SMTP_PASSWORD`, `SMTP_FROM`; existing captcha credentials | sender/domain authorization and actual inbox receipt |
| Tencent SES email | `AUTH_EMAIL_PROVIDER=tencent-ses`; independent `AUTH_OTP_SECRET`; `TENCENT_SES_SECRET_ID`, `TENCENT_SES_SECRET_KEY`, `TENCENT_SES_REGION=ap-hongkong`, `TENCENT_SES_TEMPLATE_ID=227021`, `TENCENT_SES_FROM=RSS Pal <noreply@rss.morefreeze.top>`, `TENCENT_SES_REPLY_TO=morefreeze@gmail.com`; existing captcha credentials | no callback; template variable `code` |
| QQ | `QQ_CLIENT_ID`, `QQ_CLIENT_SECRET`; get_user_info permission, website app | `https://rss.morefreeze.top/api/auth/oauth/qq/callback` |
| GitHub | `GITHUB_CLIENT_ID`, `GITHUB_CLIENT_SECRET`; no requested scopes | `https://rss.morefreeze.top/api/auth/oauth/github/callback` |
| Google | `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET` | `https://rss.morefreeze.top/api/auth/oauth/google/callback` |
| Apple | `APPLE_CLIENT_ID` (web Services ID), `APPLE_TEAM_ID`, `APPLE_KEY_ID`, `APPLE_PRIVATE_KEY` (P-256 PEM, literal `\n` supported) | `https://rss.morefreeze.top/api/auth/oauth/apple/callback` |

Use an existing authorized sender/provider. Creating paid service accounts, changing DNS or sending acceptance email requires the provider/destination to be supplied or authorized. Apple web setup requires a Services ID associated with a primary App ID enabled for Sign in with Apple; configure the exact domain/return URL and key. See [Google OIDC](https://developers.google.com/identity/openid-connect/openid-connect) and [Apple web setup](https://developer.apple.com/help/account/capabilities/configure-sign-in-with-apple-for-the-web/).

After configuring one or more channels, deploy the API using the established release workflow and check `/api/auth/methods`. Set `AUTH_PUBLIC_REGISTRATION=true` for the authorized verified signup release, then verify actual new signup/login, existing-account binding, sign-out/sign-in retention of subscriptions/reading data, and mobile Safari callback behavior. Test unavailable/mismatched/replayed flows. Verify a second account cannot take over or see the first account's data. Do not substitute signed test fixtures for provider consent and inbox-delivery evidence.

## Verification

- Backend repository/API/config/server suites with isolated real PostgreSQL; focused auth suites under race detector, Go vet, server/worker builds.
- Full mocked Google and Apple authorization/start/callback/exchange fixtures including signed JWKS tokens, issuer/audience/signature/expiry/nonce failures, state/ticket replay, PKCE, Apple signing/cookie claims, switched-owner binding and app-role database access.
- Frontend full checks/build; OTP retry, owner switch, no invitation/password in public signup, retained subscription intent, callback StrictMode one-use exchange and session preservation.
- Local browser fixture at 375px: no horizontal overflow, email/code inputs16px, all verified-access buttons44px. This proves the built UI layout, not real email/Google/Apple availability or a physical iPhone run.

## Tencent SES runtime

Use the dedicated API-only rsspal-ses-sender user, bound only to RSSPalSESSendEmailOnly (`ses:SendEmail`, resource `*`). These credentials are independent from Tencent Captcha credentials. The API does not support limiting this operation to a single sender. No console login, dedicated IP or additional permissions are required. Keep credentials in the mode-0600 production environment file. The API sender always addresses a single recipient, uses TriggerType=1 and template JSON data, rejects redirects, enforces a 15-second deadline, sanitizes failures and requires a nonempty MessageId before accepting the send. An accepted API request alone does not prove inbox receipt.

## Tencent Google OAuth egress

When recreating only the API after changing protected environment values, include the same production Compose files used by the deployment script. On the current Tencent host these are `docker-compose.yml`, `docker-compose.override.yml` and `docker-compose.override.oci-egress-20260729.yml`. A plain `docker compose up -d --no-deps api` omits the explicitly selected egress file, dropping the API's proxy environment. Direct Google token/JWKS requests then time out even though `/api/health` and `/api/auth/methods` look healthy.

Use the established deployment workflow for releases. If a configuration-only API recreation is necessary, preserve the deployed file set (check Compose container labels and `scripts/auto_deploy.sh`), then check Google token/JWKS connectivity through the configured proxy without sending credentials. Finish with actual consent/callback/exchange and sign-out/sign-in verification. Do not print OAuth codes, state, tickets, tokens or client secrets in diagnostics.

## GitHub OAuth runtime

Register an OAuth App owned by the RSS Pal operator. Set homepage to `https://rss.morefreeze.top` and callback to exactly `https://rss.morefreeze.top/api/auth/oauth/github/callback`; do not enable Device Flow for this web flow. Install its client ID and client secret only in the protected production environment file. Keep `github` unavailable until both exist, and preserve the production egress override when recreating API configuration.

The server uses authorization code + S256 PKCE, checks single-use browser-bound state, then calls only `https://api.github.com/user` with the short-lived provider token. It requests no scopes, persists only the stable positive GitHub numeric user ID (no email, avatar or mutable username), and never uses a public email to merge identities. Provider tokens remain transient and are not exposed to the browser or stored with identity records. Existing users bind from Settings → Account; verify sign-out/sign-in returns to that same account. Update public privacy disclosure before enabling.

## QQ website authorization

Official code/token protocol: https://wiki.connect.qq.com/使用authorization_code获取access_token/ and identity endpoint: https://wiki.connect.qq.com/获取用户openid_oauth2-0/ . QQ requires a server-side GET token exchange; URLs and response bodies may contain secrets, so never log provider request URLs/bodies. The implementation requests JSON, fixes the QQ token, identity and profile endpoints to graph.qq.com, blocks redirects, checks the returned client_id matches our configured app, and stores app_id:OpenID as an opaque case-sensitive identity. After verifying OpenID, it calls GET /user/get_user_info with access_token, oauth_consumer_key and openid. Only QQ nickname and a validated HTTPS qlogo.cn avatar URL are retained; gender, location, membership fields and provider tokens are not stored. Nonzero/missing ret, is_lost and malformed responses fail the flow. Mobile user agents request display=mobile.

Apply 069 and 070 via status-migrate before API startup. The replay-safe 065/069 constraint changes preserve a provider set already widened by later migrations; validate the entire ordered replay with existing QQ identities. Before submitting review, confirm actual app permissions and enable the credentials with `QQ_REVIEW_PENDING=true`. Display the testing restriction explicitly; do not advertise unrestricted public availability until approval. Configure the exact callback, publish factual privacy disclosure, and verify real binding/sign-out/sign-in before claiming live acceptance.

Douban new application is externally blocked pending an accessible official application channel; no current key or secret exists. Historical OAuth documentation or a mocked adapter is not proof of production availability. Do not expose a Douban button or claim support yet.

### QQ application review

Configure the existing QQ App ID and secret before submitting review. Set `QQ_REVIEW_PENDING=true` while pending or rejected: login and registration show the official QQ icon, visible “QQ登录” label and developer/debug-account restriction notice. QQ enforces the testing account restriction; the local flag is informational. The entry initiates real OAuth and must remain reachable during review. After approval set the flag to false and recreate the API with the complete production Compose override set. Never put credentials in source or frontend assets.

QQ now opens a 450×320 resizable window synchronously during the click, before requesting the authorization URL. Only the current OAuth flow is copied into the new window; its opener is detached before leaving the origin. After the callback validates and exchanges the one-use ticket, a per-flow BroadcastChannel signals the original page to load the requested destination. No provider token or application credential is sent over that channel. Blocked popups show a retry instruction. Real acceptance still requires the RSS Pal app's own QR/login page and successful callback; an official provider error page or the generic mobile one-click shell does not count as acceptance.

### QQ display profile

Migration 070 adds display_name/avatar_url to account_identities. Profile metadata is committed atomically with the verified QQ identity. Subject remains app_id:OpenID; nickname/avatar never determine account ownership. Existing custom nicknames and account privileges remain unchanged. The QQ nickname is a display fallback, leaving users free to set their one-time custom nickname later. Desktop/mobile account menus and bound login methods display the QQ nickname/avatar. Avatar requests use no-referrer and fall back to the account icon on failure. Successful mocks and schema tests do not certify live get_user_info while QQ authorization is blocked by 100008.
