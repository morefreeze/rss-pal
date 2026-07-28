# EdgeOne Low-Cost Dynamic Acceleration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Route `rss.morefreeze.top` through mainland EdgeOne smart acceleration while preserving private API cache isolation and keeping normal monthly spend below 35 CNY.

**Architecture:** DNSPod continues to host authoritative DNS and points only the `rss` hostname at an EdgeOne CNAME. EdgeOne terminates client HTTPS, bypasses cache for `/api/*`, caches immutable frontend assets, and uses smart routing over HTTPS to the literal OCI origin IP; DNS can be restored to the origin A record for rollback.

**Tech Stack:** Tencent Cloud EdgeOne Personal plan, DNSPod, nginx 1.24 on OCI, HTTPS/HTTP/2, browser network inspection, `dig`, `curl`, and nginx access logs.

---

### Task 1: Capture the pre-change state and performance baseline

**Files:**
- Create: `docs/superpowers/reports/2026-07-28-edgeone-rollout.md`

- [ ] **Step 1: Record repository and origin state**

Run:

```bash
git status --short
git rev-parse --short HEAD
dig +noall +answer NS morefreeze.top
dig +noall +answer rss.morefreeze.top A
dig +noall +answer rss.morefreeze.top CNAME
openssl s_client \
  -connect 158.179.181.219:443 \
  -servername rss.morefreeze.top \
  </dev/null 2>/dev/null |
  openssl x509 -noout -subject -issuer -dates -ext subjectAltName
```

Expected:

- The repository commit includes the EdgeOne design.
- DNSPod remains authoritative.
- `rss.morefreeze.top` resolves directly to `158.179.181.219`.
- The origin certificate covers only `rss.morefreeze.top` and is currently
  valid.

- [ ] **Step 2: Measure five direct-origin homepage and health samples**

Run:

```bash
for path in / /health; do
  echo "PATH $path"
  for sample in 1 2 3 4 5; do
    curl --silent --show-error --output /dev/null \
      --write-out \
      "sample=$sample code=%{http_code} ip=%{remote_ip} http=%{http_version} connect=%{time_connect} tls=%{time_appconnect} ttfb=%{time_starttransfer} total=%{time_total}\n" \
      "https://rss.morefreeze.top${path}"
  done
done
```

Expected: ten successful samples, with the raw timings retained for median and
range calculation.

- [ ] **Step 3: Record origin nginx health and recent errors**

Run:

```bash
ssh oci-rss-pal \
  "sudo -n nginx -t &&
   docker compose -f /opt/rss-pal/docker-compose.yml ps &&
   sudo -n tail -n 200 /var/log/nginx/access.log |
   awk '\$9 ~ /^5/ {print}' |
   tail -n 20"
```

Expected:

- `nginx -t` succeeds.
- Application containers are running.
- There is no sustained recent 5xx pattern.

- [ ] **Step 4: Create the rollout evidence report**

Create `docs/superpowers/reports/2026-07-28-edgeone-rollout.md` with:

```markdown
# EdgeOne Rollout Evidence

**Date:** 2026-07-28
**Origin:** `158.179.181.219`
**Production host:** `rss.morefreeze.top`

## Pre-change state

- Git commit:
- DNS answer:
- Origin certificate expiry:
- Container health:

## Direct-origin baseline

| Route | Median TTFB | TTFB range | Median total | Total range |
|---|---:|---:|---:|---:|
| `/` |  |  |  |  |
| `/health` |  |  |  |  |

## EdgeOne configuration

- Plan:
- Acceleration region:
- EdgeOne CNAME:
- Smart acceleration:
- HTTP/3:
- Request cap:
- Traffic cap:

## Pre-cutover validation

- HTTPS:
- API cache bypass:
- Static cache:
- Authenticated workflow:

## Post-cutover result

| Route | Median TTFB | Improvement | Median total | Improvement |
|---|---:|---:|---:|---:|
| `/` |  |  |  |  |
| `/health` |  |  |  |  |
| Cold article |  |  |  |  |
| Prefetched article |  |  |  |  |
| Repeat article |  |  |  |  |

## Cost verification

- Three-hour requests:
- Three-hour traffic:
- Three-hour VAU:
- Monthly projection:

## Rollback state

- DNS rollback record:
- EdgeOne hostname status:
```

Fill the pre-change and baseline fields with the collected values. Do not
record cookies, authorization headers, access tokens, passwords, account IDs,
or article content.

- [ ] **Step 5: Commit the baseline report**

Run:

```bash
git add docs/superpowers/reports/2026-07-28-edgeone-rollout.md
git commit -m "docs: record EdgeOne rollout baseline"
```

Expected: one commit containing only the rollout evidence report.

### Task 2: Verify eligibility and review the order

**Files:**
- Modify: `docs/superpowers/reports/2026-07-28-edgeone-rollout.md`

- [ ] **Step 1: Verify the Tencent Cloud session and ICP status**

Open the Tencent Cloud China EdgeOne console using the user's existing browser
session. Confirm:

- The session belongs to the intended Tencent Cloud main account.
- `morefreeze.top` passes the EdgeOne ICP check for mainland China.
- The account can purchase an EdgeOne Personal plan.

Do not start an order if the domain fails ICP validation. Record only
`ICP eligible: yes/no` in the report.

- [ ] **Step 2: Open the Personal-plan order preview**

Configure the order preview as:

- Plan: Personal.
- Quantity: one.
- Duration: one month.
- Automatic renewal: off.
- Listed price before discounts: 29.9 CNY.
- Do not add traffic, request, VAU, DDoS, or other packages.

Expected: the final order page contains only one EdgeOne Personal plan.

- [ ] **Step 3: Obtain action-time purchase confirmation**

Before clicking the final order-submission button, report the exact payable
amount, duration, renewal state, and selected account to the user. Wait for
explicit confirmation.

Expected: no purchase occurs without this confirmation.

- [ ] **Step 4: Submit the confirmed order**

After confirmation, submit and pay for the order using the payment method
already selected by the user or account. Do not save or change payment
credentials.

Expected: EdgeOne displays an active Personal plan. Record the plan type,
activation time, and expiry time without recording account identifiers.

### Task 3: Create the EdgeOne site and safe origin route

**Files:**
- Modify: `docs/superpowers/reports/2026-07-28-edgeone-rollout.md`

- [ ] **Step 1: Add the site in CNAME mode**

Create:

- Site: `morefreeze.top`.
- Plan: the confirmed Personal plan.
- Acceleration region: mainland China.
- Access mode: CNAME.

Expected: the site becomes active without replacing the DNSPod authoritative
name servers.

- [ ] **Step 2: Add the accelerated hostname**

Create hostname `rss.morefreeze.top` with:

- Origin type: IPv4.
- Origin: `158.179.181.219`.
- Origin protocol: HTTPS.
- Origin port: `443`.
- Origin Host: `rss.morefreeze.top`.

Expected: EdgeOne produces a CNAME target for `rss.morefreeze.top`. Record the
target in the rollout report.

- [ ] **Step 3: Configure the edge HTTPS certificate**

Request and deploy an EdgeOne-managed free certificate for
`rss.morefreeze.top`. Do not change or delete the existing Let's Encrypt
certificate on OCI.

Expected: the EdgeOne hostname shows a deployed, valid certificate.

- [ ] **Step 4: Verify EdgeOne-to-origin connectivity**

Use EdgeOne's built-in access verification or origin check.

Expected:

- HTTPS origin handshake succeeds.
- `/` returns 200.
- `/health` returns 200.
- No 521, 522, 525, or 555 response appears.

If this fails, verify the origin address, HTTPS protocol, port, Host header,
and certificate state. Do not change public DNS while any origin check fails.

### Task 4: Configure cache isolation, network acceleration, and cost guards

**Files:**
- Modify: `docs/superpowers/reports/2026-07-28-edgeone-rollout.md`

- [ ] **Step 1: Add the private API cache-bypass rule**

Create the highest-priority rule:

- Match: URL Path equals `/api/*`.
- Node cache TTL: do not cache / TTL 0.
- Preserve query strings and cookies.
- Do not rewrite `Authorization`.

Expected: the published rule explicitly prevents EdgeOne node caching for all
API paths.

- [ ] **Step 2: Preserve frontend cache semantics**

Create or verify:

- `/` and `/index.html`: follow the origin cache header.
- `/assets/*`: follow the origin cache header.
- Other paths: follow the origin cache header.

Expected: HTML keeps revalidating while hashed assets retain
`public, immutable, max-age=31536000`.

- [ ] **Step 3: Enable only the required network features**

Set:

- HTTP/2: enabled.
- Smart acceleration: enabled for HOST `rss.morefreeze.top`.
- HTTP/3 (QUIC): disabled.
- Edge Functions: no rule.
- Media processing: disabled.
- Bot Management: disabled.
- Four-layer proxy: absent.
- Enterprise mainland network optimization: absent.

Expected: only smart acceleration can generate VAU usage.

- [ ] **Step 4: Add monthly usage caps**

Create host-level monthly caps:

- HTTP/HTTPS requests: 500,000.
- L7 traffic: 5 GB.
- Warning threshold: 50%.
- Threshold action: stop the hostname.

Expected: both policies are active for `rss.morefreeze.top`. Record them in the
report.

### Task 5: Validate EdgeOne before changing DNS

**Files:**
- Modify: `docs/superpowers/reports/2026-07-28-edgeone-rollout.md`

- [ ] **Step 1: Resolve one EdgeOne node from the assigned CNAME**

Run with the exact CNAME shown by the console:

```bash
read -r "edgeone_cname?Paste the exact EdgeOne CNAME target: "
edgeone_ip=$(dig +short "$edgeone_cname" A | head -n 1)
printf 'cname=%s\nedge_ip=%s\n' "$edgeone_cname" "$edgeone_ip"
```

Expected: one or more EdgeOne anycast/edge IP addresses. Copy one returned IP
into `edgeone_ip` without changing DNS.

- [ ] **Step 2: Verify TLS and public content through the edge IP**

Run:

```bash
curl --silent --show-error --output /dev/null \
  --resolve "rss.morefreeze.top:443:${edgeone_ip}" \
  --write-out \
  "code=%{http_code} ip=%{remote_ip} http=%{http_version} connect=%{time_connect} tls=%{time_appconnect} ttfb=%{time_starttransfer} total=%{time_total}\n" \
  https://rss.morefreeze.top/

curl --silent --show-error --include \
  --resolve "rss.morefreeze.top:443:${edgeone_ip}" \
  https://rss.morefreeze.top/health
```

Expected: valid HTTPS, HTTP 200, and no EdgeOne origin error.

- [ ] **Step 3: Verify that API responses cannot be served from edge cache**

Run twice:

```bash
curl --silent --show-error --include \
  --resolve "rss.morefreeze.top:443:${edgeone_ip}" \
  https://rss.morefreeze.top/api/auth/me
```

Expected:

- Both calls return the unauthenticated origin behavior.
- No response reports an EdgeOne cache hit.
- No positive `Age` value appears.

Then use an authenticated browser session to request one article twice and
verify both API requests remain bypass/miss, without copying the authorization
header or response body into logs.

- [ ] **Step 4: Verify that immutable assets warm at the edge**

Read the current asset path from the HTML, then request it twice through the
edge:

```bash
asset_path=$(
  curl --silent --show-error \
    --resolve "rss.morefreeze.top:443:${edgeone_ip}" \
    https://rss.morefreeze.top/ |
  rg -o '/assets/[^" ]+\.js' -m1
)

curl --silent --show-error --head \
  --resolve "rss.morefreeze.top:443:${edgeone_ip}" \
  "https://rss.morefreeze.top${asset_path}"
curl --silent --show-error --head \
  --resolve "rss.morefreeze.top:443:${edgeone_ip}" \
  "https://rss.morefreeze.top${asset_path}"
```

Expected:

- The asset retains a one-year immutable cache policy.
- The second request reports an EdgeOne cache hit.

- [ ] **Step 5: Run the authenticated browser smoke test**

Through the validation route, verify:

1. Login succeeds.
2. A cold article opens.
3. A prefetched article opens.
4. Reading progress saves and reloads.
5. Like/save can be toggled and remains user-specific.
6. Logout succeeds and private content is not visible afterward.

Expected: no authentication, cookie, CORS, cache-isolation, or mutation
regression. Record pass/fail only; do not store private content.

### Task 6: Cut production DNS over to EdgeOne

**Files:**
- Modify: `docs/superpowers/reports/2026-07-28-edgeone-rollout.md`

- [ ] **Step 1: Save the exact rollback record**

Record:

```text
Host: rss.morefreeze.top
Type: A
Value: 158.179.181.219
```

Expected: the rollback value is present in the report before any DNS write.

- [ ] **Step 2: Lower the DNS TTL**

In DNSPod, set the existing `rss` A record TTL to 300 seconds. Wait until the
previous TTL has elapsed before replacing the record.

Expected: public DNS shows the A record with TTL at or below 300 seconds.

- [ ] **Step 3: Replace the A record with the EdgeOne CNAME**

Replace only:

- Host: `rss`.
- Old type/value: A / `158.179.181.219`.
- New type/value: CNAME / the exact EdgeOne target.
- TTL: 300 seconds.

Expected: DNSPod saves the record successfully. Do not change apex or other
subdomains.

- [ ] **Step 4: Verify public DNS and HTTPS**

Run until the public resolver returns the CNAME, without waiting longer than
one previous TTL:

```bash
dig +noall +answer rss.morefreeze.top CNAME
dig +noall +answer rss.morefreeze.top A
curl --silent --show-error --output /dev/null \
  --write-out \
  "code=%{http_code} ip=%{remote_ip} http=%{http_version} connect=%{time_connect} tls=%{time_appconnect} ttfb=%{time_starttransfer} total=%{time_total}\n" \
  https://rss.morefreeze.top/health
```

Expected: the CNAME is present, the resolved IP is an EdgeOne IP, HTTPS is
valid, HTTP/2 is negotiated, and health returns 200.

### Task 7: Measure production performance and correctness

**Files:**
- Modify: `docs/superpowers/reports/2026-07-28-edgeone-rollout.md`

- [ ] **Step 1: Repeat the five-sample public timing matrix**

Run the Task 1 homepage and health loop unchanged.

Expected: the comparison uses the same client, routes, sample count, and curl
fields as the direct-origin baseline.

- [ ] **Step 2: Measure authenticated article navigation**

Using the same mainland client and representative article set, record five
samples for:

- Cold list-to-full-article.
- Prefetched list-to-full-article.
- Return-and-repeat list-to-full-article.

Expected:

- Cold median improves by at least 30% from the 3–5 second baseline.
- Prefetched and repeat rendering remain immediate.
- The article API remains uncacheable at the edge.

- [ ] **Step 3: Check origin health and EdgeOne errors**

Run:

```bash
ssh oci-rss-pal \
  "docker compose -f /opt/rss-pal/docker-compose.yml ps &&
   sudo -n tail -n 500 /var/log/nginx/access.log |
   awk '\$9 ~ /^5/ {print}' |
   tail -n 40"
```

In EdgeOne, inspect recent response-code metrics for 5xx and 52x.

Expected: containers stay healthy and neither layer shows a new sustained error
pattern.

- [ ] **Step 4: Roll back immediately if a hard failure occurs**

Hard failures include invalid TLS, login failure, private API cache hit,
sustained 52x/5xx, or cold latency regression.

Restore DNSPod:

```text
Host: rss
Type: A
Value: 158.179.181.219
TTL: 300
```

Then verify:

```bash
dig +noall +answer rss.morefreeze.top A
curl --silent --show-error --output /dev/null \
  --write-out \
  "code=%{http_code} ip=%{remote_ip} http=%{http_version} total=%{time_total}\n" \
  https://rss.morefreeze.top/health
```

Expected: public DNS returns the origin A record and health returns 200.

### Task 8: Verify cost and finish the rollout

**Files:**
- Modify: `docs/superpowers/reports/2026-07-28-edgeone-rollout.md`

- [ ] **Step 1: Inspect stabilized EdgeOne usage**

At least three hours after cutover, read EdgeOne计费用量 for:

- Security-acceleration requests.
- Security-acceleration traffic.
- Smart-acceleration VAU.

Expected: values are consistent with traffic since cutover and well below the
configured caps.

- [ ] **Step 2: Project the monthly bill**

Calculate:

```text
Monthly total =
  29.9 CNY Personal plan
  + projected smart-acceleration requests / 1,000,000 * 10 CNY
  + any unexpected bill item
```

Expected: normal projected total is no more than 35 CNY and unexpected bill
items equal zero.

- [ ] **Step 3: Review automatic renewal after seven days**

If the seven-day latency, correctness, and cost results pass:

- Enable automatic renewal for the Personal plan only after user confirmation.

If the results fail:

- Restore the direct-origin A record.
- Confirm public traffic has returned to OCI.
- Disable the EdgeOne hostname.
- Do not renew the plan.

- [ ] **Step 4: Complete and commit the rollout report**

Fill every report field with measured evidence and the renewal decision. Run:

```bash
rg -n "^- .*:$|\\|  \\|" docs/superpowers/reports/2026-07-28-edgeone-rollout.md
git diff --check
git add docs/superpowers/reports/2026-07-28-edgeone-rollout.md
git commit -m "docs: record EdgeOne rollout results"
```

Expected:

- The placeholder scan returns no unfinished report fields.
- `git diff --check` succeeds.
- The commit contains only the completed report.
