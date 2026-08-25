# GitBook capture request-size repair

## Problem

Both RSS Pal capture paths fail for `https://morefreeze.gitbook.io/mon-test`.
The rendered GitBook page produces an approximately 1.29 MiB JSON request after
the existing client-side cleanup. Production Nginx rejects that request with
HTTP 413 before it reaches RSS Pal:

- bookmarklet requests fail at the public host Nginx;
- extension requests use the same `/api/bookmarklet/capture` endpoint and hit
  the same limit;
- the Go handler already permits a 4 MiB request body, so the proxy and
  application limits are inconsistent.

## Approaches considered

1. **Align both Nginx layers with the existing API limit (chosen).** Set
   `client_max_body_size 5m` at the public Tencent Nginx server and in
   `frontend/nginx.conf`. The extra 1 MiB lets requests above the API's 4 MiB
   cap reach Go and receive its intentional JSON error instead of an Nginx HTML
   error.
2. **Special-case GitBook extraction in both clients.** Capture only GitBook's
   article subtree or fetch its Markdown representation. This reduces traffic
   but duplicates site-specific behavior across the bookmarklet and extension,
   and does not fix the proxy/application contract for other large pages.
3. **Raise only the public Nginx limit.** This fixes the first proxy but leaves
   the frontend container's default 1 MiB limit, so the request merely fails at
   the next hop.

## Approved design

### Configuration

- Add `client_max_body_size 5m;` to the server block in
  `frontend/nginx.conf` so container rebuilds preserve the limit.
- Back up `/etc/nginx/sites-available/rss-pal` on `tencent-rss-pal`, then add
  the same directive to the HTTPS `rss.morefreeze.top` server block.
- Do not change the Go `captureMaxBodyBytes` value. The application remains the
  authoritative 4 MiB safety boundary.
- Do not broaden this change to PDF or backup-upload limits; those endpoints
  have separate application contracts and are outside this incident.

### Request flow

After the change, a capture travels through these limits:

1. public Tencent Nginx: 5 MiB;
2. frontend container Nginx: 5 MiB;
3. Go bookmarklet handler: 4 MiB.

The 1.29 MiB GitBook capture therefore reaches the handler. Requests above
4 MiB continue to fail safely at the application boundary with the existing
`内容过大` response.

### Deployment safety

- Validate the repository configuration with Nginx before deployment.
- Back up the live host configuration before editing it.
- Run `nginx -t` before reloading the host Nginx.
- Rebuild only the frontend container so its embedded Nginx configuration is
  updated; do not restart unrelated services.

### Verification

1. Confirm the frontend image contains `client_max_body_size 5m` and passes
   `nginx -t`.
2. Confirm live `nginx -T` shows the directive for `rss.morefreeze.top`.
3. Send an unauthenticated request larger than 1 MiB through the public URL and
   confirm it reaches Go (401), rather than being rejected by Nginx (413).
4. Re-run the real GitBook save once with the bookmarklet and once with the
   extension; both must return a created, updated, or duplicate application
   result rather than HTTP 413.
5. Check production access/error logs for the two requests and verify there is
   no new `client intended to send too large body` entry.

## Rollback

Restore the timestamped host Nginx backup, validate it with `nginx -t`, and
reload Nginx. The repository change can be reverted independently and the
frontend image rebuilt if needed.
