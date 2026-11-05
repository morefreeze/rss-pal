# FeedBurner HTTPS compatibility implementation plan

> For agentic workers: execute this cohesive change inline using executing-plans. Obtain an independent requesting-code-review review before integration. The user's standing development instructions authorize mainline integration and Tencent deployment after verification.

**Goal:** Accept legacy default-port HTTP FeedBurner URLs through the existing safe HTTPS fetch path, and use that canonical identity consistently for preview, subscription creation/import and later fetches.

**Architecture:** Add a pure RSS URL canonicalizer for the exact `feeds.feedburner.com` origin. It upgrades HTTP without changing path, escaped path or query, and never grants RSSHub transport trust. Call it before the resolver's empty-RSSHub early return, before Preview constructs `actual_url`, and from the API's existing Create normalization, also called by frontend OPML import. Existing stored rows are not rewritten. The public fetch path keeps DNS/IP pinning, TLS verification, redirect checks, body bounds and request deadlines; the stored-worker transport behavior is otherwise preserved.

**Tech stack:** Go 1.25, net/url, existing RSS/Atom fetcher and Go tests.

## Evidence and scope

The ordinary QA account reproduces HTTP404, while the original RSS is valid and contains100 items. Same-container probes observe the same Google1577-byte HTTP404 using two independent public IPs; environmental hostname forwarding reads valid RSS. Pinned-IP CONNECT to port80 is rejected403, so changing all HTTP requests to tunnels is not a usable fix. HTTPS succeeds in same-container probes and the real UI. Several earlier HTTPS requests exhausted30 seconds; socket observations establish actual network transfer rather than queue starvation. This patch addresses the deterministic legacy-HTTP incompatibility. Do not claim it cures arbitrary network timeouts or change DNS/proxy strategies without additional evidence. Keep the issue unchecked until deployed original HTTP and HTTPS previews pass repeat acceptance; retain all timeout evidence.

## Task1 URL identity and fetch consistency

- [x] Write failing tests in `backend/internal/rss/feed_url_test.go` for legacy HTTP, explicit80, escaped paths/query, exact-origin boundaries, unchanged custom ports/credentials/other schemes, no RSSHub trust, canonical Preview actual_url, and stored Fetch conditional headers. Add `backend/internal/api/feed_url_test.go` normalization coverage.
- [x] Run `cd backend && go test ./internal/rss ./internal/api -run 'FeedBurner|LegacyFeedBurner' -count=1`. The retained `canonical-red.log` demonstrates expected HTTP-versus-HTTPS assertions, with no compile errors.
- [x] Create `backend/internal/rss/feed_url.go` with this pure implementation:

```go
func CanonicalFeedURL(raw string) string {
    u, err := url.Parse(raw)
    if err != nil || u.User != nil || !strings.EqualFold(u.Scheme, "http") ||
        !strings.EqualFold(u.Hostname(), "feeds.feedburner.com") ||
        strings.HasSuffix(u.Host, ":") || (u.Port() != "" && u.Port() != "80") {
        return raw
    }
    u.Scheme, u.Host = "https", "feeds.feedburner.com"
    return u.String()
}
```

- [x] Set `input = CanonicalFeedURL(input)` at the start of `resolveFeedURL` in `backend/internal/rss/resolver.go`, before its empty-base guard. Set `rawURL = CanonicalFeedURL(rawURL)` at the start of `Fetcher.Preview` in `backend/internal/rss/fetcher.go`. Return `rss.CanonicalFeedURL(u.String())` from successful `normalizeFeedURL` in `backend/internal/api/feed.go`.
- [x] Run targeted tests, full `go test ./...`, plus race-enabled `./internal/httpx ./internal/rss ./internal/api`. Preserve skip/environment boundaries. Verify OPML imports through POST /feeds and therefore calls the same normalization and no migrations, AI code, historical rows, public sharing, proxy configuration or frontend generation settings change.
- [x] Independently review the exact diff and adversarial URL boundaries. Fix important findings and reverify. Commit the implementation and this plan.

## Task2 Delivery and ordinary-user acceptance

- [ ] Fetch actual mainline and fast-forward push the reviewed commit to master, without touching the unrelated dirty canonical checkout or failed AI candidate.
- [ ] Wait for `.github/workflows/deploy-tencent.yml` for the exact revision; verify actual `/opt/rss-pal` HEAD, API/worker instances, local/public health, unchanged frontend assets and protected AI source. Use the established entrypoint only if Actions genuinely stalls, with verified exact bundle and no duplicate deploy.
- [ ] Using the restored ordinary QA session, preview original HTTP and HTTPS URLs, verify RSS type/title and ten actual article links, then cancel. Reload and repeat. No subscription confirmation because it triggers unbounded background AI processing; no new sources or summaries. Capture screenshots, DOM and whitelisted response metadata, never authentication headers.
- [ ] Only after acceptance, mark FEEDBURNER-ACCESS complete in the existing private Page, move its existing blocks under completed, preserve all user notes and22 distinct cloud attachments, and synchronize local TODO/state/sample index with exact checkbox counts. If original HTTPS timeouts remain reproducible, keep it partial and describe the remaining evidence precisely.

## Self-review

Exact origin and default-port checks preserve validation boundaries, including malformed trailing-colon hosts. Canonical identity is returned to confirmation and reused by Create/import, while resolver-side upgrading supports already-stored URLs without data migration. No global HTTP tunneling, hostname proxy bypass, timeout increase, blind retry or bulk AI generation is included. Acceptance distinguishes source preview from confirmed subscription and HTTPS recovery from a claimed universal networking fix.

Validation before review: backend full suite exit0 (database-dependent tests without a local DSN may skip); network/RSS/API race suite exit0. Separate isolated PostgreSQL/RLS run executes the canonical creation and OPML owner/export tests, both PASS under race; the baseline duplicate-conflict path logs an existing failed-transaction commit diagnostic after its expected409 response. The new integration assertion also fails against an overlay of the pre-fix API code, then passes with this implementation. No production database writes or network calls occur in those tests.
