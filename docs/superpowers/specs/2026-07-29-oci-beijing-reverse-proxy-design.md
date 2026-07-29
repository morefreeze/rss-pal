# OCI to Beijing Reverse Proxy Design

## Goal

Restore `rss.morefreeze.top` immediately while keeping the public DNS and TLS
certificate on OCI and moving all application reads and writes to the Beijing
server.

## Constraints

- Do not point public DNS directly at the unfiled Beijing server.
- Do not resume the OCI API, worker, or status-monitor.
- Do not perform a final database synchronization; the user accepted the
  existing Beijing snapshot.
- Do not connect directly to the Beijing server from the local/company egress.
  All Beijing SSH access must use `oci-rss-pal` as a jump host.
- Keep rollback possible without changing DNS.

## Architecture

```text
Browser
  -> rss.morefreeze.top / 158.179.181.219 (OCI)
  -> OCI Nginx terminates public TLS
  -> HTTPS to 192.144.171.125 (Beijing), SNI disabled
  -> Beijing Nginx -> frontend/API -> Beijing PostgreSQL
```

OCI sends the original HTTP `Host` header, forwarding headers, and request
scheme. The upstream TLS connection disables SNI so Tencent's filing filter
does not reject the connection, while `proxy_ssl_name` remains
`rss.morefreeze.top` so Nginx can verify the Beijing certificate.

## Change

Only `/etc/nginx/sites-enabled/rss-pal` on OCI changes:

- Replace `proxy_pass http://127.0.0.1:8082` with
  `proxy_pass https://192.144.171.125`.
- Disable upstream SNI explicitly.
- Verify the upstream certificate against `rss.morefreeze.top` using the
  system CA bundle.
- Preserve the existing Host, forwarding, timeout, public certificate, and
  HTTP-to-HTTPS redirect settings.

The current OCI Nginx file is copied to a backup outside `sites-enabled` before
installation. Nginx configuration validation must pass before a reload.

## Verification

- Public `/` returns 200 from OCI.
- Public `/api/health` returns 200 with the Beijing API response.
- OCI-to-Beijing HTTPS works without SNI and validates the certificate name.
- Beijing PostgreSQL still contains the accepted snapshot.
- OCI API/worker/status-monitor remain stopped and its database has no business
  writes.
- Beijing API/worker remain running.

## Rollback

If Nginx validation, public health, or functional checks fail, restore the
saved OCI Nginx file, run `nginx -t`, and reload. DNS never changes, so rollback
does not depend on propagation.
