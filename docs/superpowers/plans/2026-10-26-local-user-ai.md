# Browser-local user AI

Confirmed scope: retain AI settings with save, clear and connectivity test; keep user API keys in this browser only. Calls go directly to an HTTPS OpenAI-compatible endpoint after a manual action. Persist successful article summaries, never credentials. Explain manual generation, CORS, local storage and cross-device limitations. Existing site-operated AI is independent and never consumes local credentials.

1. Add account-scoped temporary/browser-persistent configuration and an isolated provider fetch client (no site JWT/cookies, redirects or proxy fallback). Test storage isolation, CORS errors, incomplete output, cancellation and credential routing.
2. Add authenticated summary-input and summary-result endpoints. Reuse server prompt quality/anchors, verify content digest, ownership, output bounds and current job lease; acknowledge only committed results. Test ownership, stale content and failed commits.
3. Route the manual summary action through local AI when configured; retain existing system path otherwise. Add save/test/delete settings, manual-generation disclosure and migration notice. Stop accepting/using server user keys; clear existing active keys and reject future stored keys with a migration constraint. Do not auto-download old keys.
4. Update privacy/operations documentation; inspect existing backup format and prevent old credential reintroduction where applicable.
5. Run frontend suite/build, backend tests with isolated PostgreSQL, integration review and browser QA using a fake CORS provider. Merge latest master, push and verify Tencent deployment, revision, health, bundle and changed endpoints. Do not call a real paid provider with user credentials for testing.
