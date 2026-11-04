# Public feed and image fetch recovery

Image proxy requests and public subscription previews use the existing
checked-address, complete-response transport. Each GET tries at most four
unique public IPs and accepts the first complete 200, validated redirect, or
bodyless 304. EOF, failed proxy CONNECT, and stalled partial responses do not
prevent another checked address from completing. The caller's total deadline
still applies; this is not an additional round of retries after that deadline.

If no candidate produces an accepted response, images allow one fresh connection
to each checked IP that failed with a pre-body EOF, unexpected EOF, or connection
reset. The initial connections close and release their permits before this
second wave. It uses the same original request deadline and pinned public IPs:
at most four unique addresses and eight candidate attempts per URL hop, under
the existing redirect limit. A complete winner stops recovery. HTTP error
statuses (including proxy CONNECT 500), body truncation, oversize responses,
blocked redirects, cancelled requests, and authenticated/non-GET requests do
not themselves trigger a retry. This mitigates transient pre-body connection
failures; it does not repair an unavailable proxy address family. HTML/feed
clients retain their single wave.

The image workload has four shared active candidate permits and a 10 MiB body
limit. HTML captures and public feeds share a separate four-permit workload,
so slow images cannot consume preview/capture permits. HTML captures retain
their existing 8 MiB limit. Public feed requests, including subscription
previews, now gain an 8 MiB complete-response limit; they previously had no
response-size cap. Each candidate is bounded, and cancelled/losing connections
close before their permits are released. Conditional feed headers and 304 metadata
are retained; a 304's declared representation size is not a response body.

The image handler retains its 90-second full-response budget and sends success
and cache headers only after the complete bounded image is available. Public
feeds retain their 30-second client budget. Worker and derived trusted RSSHub
fetches retain their existing client and origin restrictions. Non-GET requests,
GETs with a body, Authorization, or Cookie use the single-address streaming
transport. All DNS answers are checked before the four-address cap; redirects,
IP pinning, public URL identity, Host, and TLS SNI retain the existing guards.

## Mounted frontend nginx configuration

Tencent binds `/opt/rss-pal/nginx.prod.conf` over the image's nginx configuration.
Updating `frontend/nginx.conf` alone cannot repair that running mount. During a
runtime deployment, `scripts/ensure_image_proxy_nginx.py` recognizes the private
template's flat API proxy and adds an exact `/api/proxy/image` location with a
120-second read timeout, using its existing upstream and headers. Other bytes,
custom routes, generic API deadlines, and file mode/ownership remain intact.
Unknown, ambiguous, conflicting, or symlinked input is rejected without overwrite.
More-specific prefixes and matching regex locations are conflicts. Regex
syntax outside the helper's small equivalent ASCII subset requires manual
review, including inline PCRE flags and escaped quoted patterns.
Server-level includes also require review because they can contain routes the
helper cannot inspect. Static `proxy_pass` values must contain only the origin:
a trailing slash or other URI
component would change prefix replacement semantics when copied to an exact
location and is refused before backup or file changes.

A change creates `nginx.prod.conf.image-proxy-<UTC timestamp>.bak` and atomically
replaces the source file. The deploy script adds only frontend to the existing
service selection and forces its recreation so Docker remounts the new inode.
`nginx -t` and the actual `nginx -T` output must pass before backend startup.
When the host template is already current, backend-only deployments still check
the running nginx configuration without recreating frontend. A failed rollout
restores the original template before checking out the rollback revision and
forces recreation again. The established deployment entrypoint continues to
run all repository-supplied code as `ubuntu`.

Controlled regression checks (no external sites):

```sh
cd backend
go test -race ./internal/httpx ./internal/rss ./internal/api
cd ..
python3 scripts/tests/image_proxy_nginx_test.py
bash scripts/tests/auto_deploy_image_proxy_test.sh
bash scripts/tests/auto_deploy_service_selection_test.sh
bash scripts/tests/auto_deploy_migration_scope_test.sh
bash scripts/tests/auto_deploy_direct_network_test.sh
bash scripts/tests/auto_deploy_proxy_ready_test.sh
bash scripts/tests/tencent_deploy_bundle_test.sh
```

These checks prove controlled failure recovery and deployment behavior. Live
acceptance additionally requires the exact deployed revision, actual mounted
nginx configuration, uncached image loads/dimensions and reloads, and two feed
previews returning ten items followed by Cancel without creating subscriptions.
