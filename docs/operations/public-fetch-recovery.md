# Public feed and image fetch recovery

Image proxy requests and public subscription previews validate the complete DNS
answer before choosing at most four unique public IPs. Only a complete bounded
200, validated redirect, or bodyless 304 can win. HTML/feed clients retain their
single race of checked addresses.

Images start one protected primary per request. Three shared primary lanes and
one shared secondary lane bound physical concurrency to four and prevent a
hedge from taking the three primary admissions. Lane waiting happens before a
global candidate permit or socket is acquired. The trade-off is at most three
simultaneous distinct primary image requests, reserving the fourth connection
for a stalled peer's alternative; a fourth image waits within its original
budget even when that spare lane is idle.

After two seconds without connection I/O, the scheduler may add one secondary.
Progress covers CONNECT, TLS, response headers and body reads/writes. The
primary retains its original deadline and is never cancelled merely for
inactivity. A first secondary may yield after inactivity when another checked
peer is pending; its sockets close and permits return before replacement. A
yielded peer can resume once after the untried peers. This scheduler-owned
yield is distinct from caller cancellation or an upstream body failure. A final
attempt is not repeatedly discarded for being quiet, preserving the remaining
budget for a legitimate slow alternative. If a primary fails, its secondary
can become the protected primary and release the spare lane.

Pre-body EOF, unexpected EOF and connection reset also permit one fresh attempt
to the same pinned IP. Scheduler resumes and EOF recovery share the same total
allowance: at most two attempts per checked address and eight per URL hop,
under the existing redirect limit and original deadline. HTTP error statuses
(including CONNECT 500), genuine body truncation, oversize responses, blocked
redirects, caller cancellation, and authenticated/non-GET requests do not
themselves grant another attempt. A complete winner stops all remaining work.
This mitigates bounded scheduling/connection failures; it does not repair an
unavailable proxy address family or guarantee that every possible timing of
four peers is equivalent to eager racing. With only two active candidates per
image, a final quiet peer can still consume the remaining budget before other
alternatives get another turn.

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
