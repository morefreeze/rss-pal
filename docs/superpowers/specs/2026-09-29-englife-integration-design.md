# englife administrator connection

Approved in chat on 2026-09-29: one administrator connects an englife account for site-wide YouTube enrichment, using the existing browser extension. No deployment or real-account operation is included in implementation approval.

## Connection

Administrator route `/admin/integrations/englife` shows connection state, account, last check, optional upstream remaining quota and recent tasks. The page obtains a short-lived single-use pairing token. An extension-owned confirmation page explains which RSS Pal server receives the englife session and that it serves all users. Only after an explicit click does the extension request optional cookies permission and send englife-domain cookies directly to the pairing completion endpoint. Google cookies and passwords are never read. Pair tokens are hashed, expire in ten minutes, bound to the administrator, and recheck administrator privileges on redemption. Cookie payloads never enter page messages, logs or API responses.

Session data is AES-GCM encrypted using ENGLIFE_SESSION_KEY (base64, 32 bytes, same on server and worker). With no key, integration is disabled and existing fetching continues. A status read never initiates upstream work. Explicit check refreshes account/quota state; disconnect removes credentials and invalidates outstanding pair tokens. Expired credentials pause upstream calls until explicit recheck or reconnection. Exhausted quota pauses new generation, while already-submitted tasks can still be reconciled and retrieved read-only. A connection_id changes only on successful pairing redemption, so worker health checks cannot falsely complete a reconnect.

## Fetching

Use englife as a final fallback after existing caption methods. Auth/me supplies CSRF, quota is checked before submission, POST /api/jobs accepts url and targetLanguage (zh), and documents are retrieved by task identifier through preview then document endpoints, as observed in the public client. Persist task id and state keyed by account ID, video ID and language across API and worker processes. Serialize task creation with a database lock. Never repeat an ambiguous POST automatically: first reconcile against the upstream task list; unresolved ambiguity is shown for administrator action. Resume pending tasks on later cycles instead of occupying workers with long polling. For connected YouTube articles, bound the primary chain to 45 seconds and reserve up to 45 seconds of the worker parent deadline for englife; disabled/disconnected providers retain original primary behavior. Cache ready output. Label output `视频整理（englife）`, not raw captions.

## Acceptance

Tests cover admin boundaries, pairing expiry/replay, domain validation, encryption, no credential disclosure, CSRF, quota/401 behavior, cross-process deduplication state, unknown submission outcomes, document mapping, extension consent and sender validation, UI states and stale responses. Real Google login, cross-IP session reuse, live processing and production deployment remain explicitly unverified until an administrator connects an account.
