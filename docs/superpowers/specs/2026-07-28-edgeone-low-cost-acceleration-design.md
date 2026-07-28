# EdgeOne Low-Cost Dynamic Acceleration — Design

**Date:** 2026-07-28
**Status:** Approved for implementation

## Problem

The application work completed on 2026-07-27 makes prefetched and repeated
article navigation render immediately, but a truly cold article request still
crosses the public network from users in mainland China to the OCI origin at
`158.179.181.219`.

Production measurements show that this remaining delay is not application
compute:

- The article-detail handler normally completes in 5–15 ms.
- A public 34-byte health response still takes roughly 1.8–2.4 seconds.
- A current measurement from the deployment client took 0.75 seconds to
  connect, 1.85 seconds to finish TLS, and 2.29 seconds to first byte.
- The public route already negotiates HTTP/2, so the remaining leverage is the
  client-to-origin route rather than another backend optimization.

Classic CDN caching cannot materially improve the authenticated
`GET /api/articles/:id` path. The API is private and must be revalidated, so the
target is EdgeOne dynamic smart acceleration: users connect to a nearby
EdgeOne node and EdgeOne selects an optimized route to the overseas origin.

## Goals

- Reduce cold article request latency for users in mainland China without
  caching private API responses.
- Preserve the current application, authentication, and origin behavior.
- Keep normal monthly EdgeOne spend near 31 CNY at current traffic.
- Put a hard ceiling on unexpected request and traffic growth.
- Validate the accelerated route before changing production DNS.
- Keep rollback to the direct OCI origin possible through one DNS change.

## Non-Goals

- Moving the API, database, RSSHub, or worker out of OCI.
- Caching authenticated article, progress, preference, or account responses.
- Using Edge Functions, HTTP/3 (QUIC), media processing, Bot Management,
  four-layer proxying, or enterprise mainland-network optimization.
- Moving authoritative DNS from DNSPod to EdgeOne.
- Hiding the origin IP or restricting the origin firewall to EdgeOne during
  the first rollout.

## Options Considered

### Classic Tencent CDN

This would cost less than 1 CNY per month at current traffic and would help
hashed JavaScript and CSS after a cache fill. It does not provide dynamic
content smart acceleration, so a cold private article request would still use
the ordinary public route to OCI. It is not selected.

### EdgeOne free plan plus Add-on Suite

The free plan alone provides static acceleration but not smart acceleration.
The current mainland Add-on Suite costs 108 CNY per month, which is more than
the Personal plan. It is not selected.

### EdgeOne Personal plan with smart acceleration

The Personal plan costs 29.9 CNY per month and includes 50 GB and 3 million
requests. Smart acceleration is billed at 100 VAU per million requests, with
VAU priced at 0.1 CNY. At the observed monthly projection of about 76,000
requests, the smart-acceleration charge is about 0.76 CNY. Expected total spend
is therefore about 30.66 CNY per month.

This is the selected option. HTTP/3 remains disabled because it creates a
second request-based charge and is not needed to establish whether optimized
dynamic routing fixes the current bottleneck.

Official pricing references:

- [EdgeOne package pricing](https://cloud.tencent.com/document/product/1552/94158)
- [VAU pricing](https://cloud.tencent.com/document/product/1552/94161)
- [Package feature comparison](https://cloud.tencent.com/document/product/1552/94165)
- [Free Add-on Suite pricing](https://cloud.tencent.com/document/product/1552/133025)

## Prerequisites

- `morefreeze.top` must have a valid ICP filing before EdgeOne can use the
  mainland China or global acceleration zone.
- The Tencent Cloud China main account must be able to purchase and manage an
  EdgeOne Personal plan.
- The DNSPod zone must permit replacing the current `rss` A record with the
  EdgeOne-provided CNAME.

The purchase and DNS cutover are external state changes. The order must be
reviewed immediately before submission. If the ICP check fails, no order or DNS
change is made.

## EdgeOne Configuration

### Site and access mode

- Plan: Personal, one month for initial validation.
- Site: `morefreeze.top`.
- Acceleration region: mainland China, or global only if mainland China is
  included and the price remains the documented Personal-plan price.
- Access mode: CNAME.
- Accelerated hostname: `rss.morefreeze.top`.
- Authoritative DNS remains at DNSPod.

### Origin

- Origin type: IPv4 address.
- Origin address: `158.179.181.219`.
- Origin protocol: HTTPS.
- Origin port: `443`.
- Origin Host header: `rss.morefreeze.top`.
- Client-facing HTTPS: EdgeOne-managed free certificate for
  `rss.morefreeze.top`.

Using the literal origin IP prevents a DNS recursion after
`rss.morefreeze.top` is changed to the EdgeOne CNAME. The existing origin
certificate and nginx virtual host remain unchanged.

### Cache safety

Rules are ordered from most specific to least specific:

1. `/api/*`: EdgeOne node cache TTL `0` / bypass cache. Preserve cookies,
   `Authorization`, query strings, request methods, and origin response
   headers.
2. `/` and `/index.html`: follow the origin `Cache-Control: no-cache,
   must-revalidate`.
3. `/assets/*`: follow the origin `Cache-Control: public, immutable,
   max-age=31536000`.
4. Other paths: follow origin cache headers.

The first rule is mandatory even though article-detail responses already use
private cache headers. It prevents a future origin-header regression from
leaking one user's data through an edge cache.

### Network features

- Enable HTTP/2 for client connections.
- Enable smart acceleration for host `rss.morefreeze.top`.
- Keep HTTP/3 (QUIC) disabled.
- Keep Edge Functions, image/video processing, Bot Management, four-layer
  proxying, and enterprise international acceleration disabled.
- Do not enable origin protection during the first rollout.

EdgeOne currently supports the smart-acceleration action at HOST or whole-site
scope, not URL-path scope. All requests for `rss.morefreeze.top` therefore
count toward smart-acceleration usage; at current traffic this adds about
0.76 CNY per month.

Official behavior references:

- [Smart acceleration](https://cloud.tencent.com/document/product/1552/70959)
- [Rule-engine match and action support](https://cloud.tencent.com/document/product/1552/90438)

## Cost Controls

Configure monthly per-host caps:

- HTTP/HTTPS requests: stop acceleration at 500,000 requests.
- L7 traffic: stop acceleration at 5 GB.
- Warning threshold: 50% for both caps.

Current projected usage is about 76,000 requests and 0.3 GB per month. The caps
leave material headroom while limiting the normal maximum smart-acceleration
request charge to about 5 CNY. EdgeOne may take about ten minutes to enforce a
cap, so this is a budget guard rather than an exact billing boundary.

Do not purchase traffic, request, or VAU add-on packages. Do not enable
automatic renewal until the seven-day performance and cost review passes.

Official reference:

- [Usage cap policy](https://cloud.tencent.com/document/product/1552/101093)

## Rollout

1. Record direct-origin timing for the homepage, health endpoint, and a fixed
   authenticated article set.
2. Create the EdgeOne site and hostname without changing DNS.
3. Validate the EdgeOne route by resolving `rss.morefreeze.top` to an EdgeOne
   node locally while keeping the HTTP Host and TLS SNI unchanged.
4. Verify HTTPS, login, article detail, progress writes, likes/saves, and
   logout through the validation route.
5. Verify `/api/*` responses are not cached and `/assets/*` becomes an edge
   cache hit after the first request.
6. Set the DNSPod `rss` record TTL to 300 seconds.
7. Replace the `rss` A record with the EdgeOne CNAME.
8. Repeat performance, correctness, and cache-isolation tests through public
   DNS.
9. Review EdgeOne计费用量 after it has stabilized, normally at least three
   hours later, and again after seven days.

## Success Criteria

- Median public health TTFB improves by at least 30% from the direct-origin
  baseline.
- Median cold list-to-full-article time improves by at least 30% from the
  measured 3–5 second baseline.
- Prefetched and repeated articles retain immediate rendering.
- No authenticated `/api/*` response reports an edge cache hit.
- Hashed `/assets/*` reports an edge cache hit after warming.
- Login, logout, progress, like/save/hide, content refresh, and summary refresh
  remain correct.
- No new sustained 5xx/52x response pattern appears.
- Projected monthly bill remains at or below 35 CNY under normal traffic.

## Rollback

Rollback requires no application deployment:

1. Restore DNSPod `rss.morefreeze.top` to A record `158.179.181.219`.
2. Wait one TTL and verify the public IP, TLS certificate, HTTP/2, and health
   response.
3. Disable the EdgeOne hostname after traffic has returned to the origin.
4. Cancel renewal or destroy the EdgeOne plan only after DNS rollback is
   confirmed.

The origin nginx and certificate stay active throughout the rollout, so DNS
rollback returns users to the exact pre-EdgeOne route.

## Testing and Evidence

Keep the pre- and post-cutover samples in the implementation report:

- DNS answer and resolved edge/origin IP.
- TLS and negotiated HTTP version.
- Five timing samples each for homepage, health, cold article, prefetched
  article, and repeated article.
- Response cache indicators for `/api/*` and `/assets/*`.
- Origin nginx request status and upstream timing.
- EdgeOne traffic, request, and VAU usage after three hours and seven days.
- The final monthly cost projection based on EdgeOne计费用量, not only nginx
  access logs.
