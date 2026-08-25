# GitBook Capture Body-Limit Repair Implementation Plan

> **For agentic workers:** Choose the execution mode with the Execution Routing section below. Use superpowers:executing-plans for small or tightly coupled plans, and superpowers:subagent-driven-development for larger plans with independently reviewable tasks. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Allow the 1.29 MiB GitBook page capture to pass both Nginx proxies while preserving the Go API's existing 4 MiB request limit.

**Architecture:** Add a tested 5 MiB client-body limit to the repository-owned frontend Nginx and the public Tencent host Nginx. The proxy limit stays slightly above the API limit so oversized requests receive the Go handler's intentional response. Deploy only the frontend container plus a host Nginx configuration reload, then verify the full public proxy chain with a body larger than the former 1 MiB limit.

**Tech Stack:** Nginx, Docker Compose, Node.js built-in test runner, Go/Gin API, Tencent Cloud host

---

## File map

- Create `frontend/test/nginxRequestBodyLimit.test.cjs`: static regression test that enforces the 5 MiB frontend Nginx contract.
- Modify `frontend/nginx.conf`: repository-owned frontend proxy limit.
- Modify `/etc/nginx/sites-available/rss-pal` on `tencent-rss-pal`: live public-proxy limit; create a timestamped backup first.

### Task 1: Add the frontend Nginx regression test

**Files:**
- Create: `frontend/test/nginxRequestBodyLimit.test.cjs`
- Test: `frontend/test/nginxRequestBodyLimit.test.cjs`

- [ ] **Step 1: Write the failing test**

Create `frontend/test/nginxRequestBodyLimit.test.cjs` with:

```javascript
const assert = require('node:assert/strict')
const { readFileSync } = require('node:fs')
const { resolve } = require('node:path')
const test = require('node:test')

const nginx = readFileSync(resolve('nginx.conf'), 'utf8')

test('frontend proxy accepts capture bodies up to the API handoff limit', () => {
  assert.match(nginx, /^\s*client_max_body_size\s+5m;\s*$/m)
})
```

- [ ] **Step 2: Run the test and verify it fails**

Run from `frontend/`:

```bash
node --test test/nginxRequestBodyLimit.test.cjs
```

Expected: FAIL because `frontend/nginx.conf` does not contain `client_max_body_size 5m;`.

### Task 2: Implement and validate the repository Nginx limit

**Files:**
- Modify: `frontend/nginx.conf:15`
- Test: `frontend/test/nginxRequestBodyLimit.test.cjs`

- [ ] **Step 1: Add the 5 MiB server limit**

Insert the directive after the SSL session settings and before the upstream/proxy locations:

```nginx
    # Keep the proxy ceiling above the Go HTML-capture limit (4 MiB) so the
    # application returns its structured "内容过大" error when necessary.
    client_max_body_size 5m;
```

- [ ] **Step 2: Run the focused test and verify it passes**

Run from `frontend/`:

```bash
node --test test/nginxRequestBodyLimit.test.cjs
```

Expected: one passing test.

- [ ] **Step 3: Validate the complete Nginx configuration**

Run from the repository root:

```bash
docker run --rm \
  -v "$PWD/frontend/nginx.conf:/etc/nginx/conf.d/default.conf:ro" \
  -v "$PWD/certs:/etc/nginx/certs:ro" \
  nginx:alpine nginx -t
```

Expected: `syntax is ok` and `test is successful`.

- [ ] **Step 4: Run frontend regression checks**

Run from `frontend/`:

```bash
npm run test:legacy
npm run build
```

Expected: every legacy test passes and the Vite production build completes.

- [ ] **Step 5: Commit the implementation**

```bash
git add frontend/nginx.conf frontend/test/nginxRequestBodyLimit.test.cjs
git commit -m "fix(nginx): allow large page captures"
```

### Task 3: Publish the versioned repair

**Files:**
- No additional file changes.

- [ ] **Step 1: Verify the commit contains only intended tracked changes**

```bash
git status --short --branch
git show --stat --oneline HEAD
git diff --check HEAD^ HEAD
```

Expected: the implementation commit contains only `frontend/nginx.conf` and `frontend/test/nginxRequestBodyLimit.test.cjs`; pre-existing untracked backup/course files remain untouched.

- [ ] **Step 2: Push the commits required by Tencent deployment**

```bash
git push origin master
```

Expected: `origin/master` advances through the design, plan, and implementation commits without force-push.

### Task 4: Prepare the live host configuration without reloading it

**Files:**
- Modify on Tencent: `/etc/nginx/sites-available/rss-pal`
- Create on Tencent: `/etc/nginx/sites-available/rss-pal.bak-20260825-gitbook-limit`

- [ ] **Step 1: Reconfirm the live configuration is still missing the limit**

```bash
ssh tencent-rss-pal \
  "sudo nginx -T 2>/dev/null | sed -n '/server_name rss.morefreeze.top;/,/^}/p'"
```

Expected: the HTTPS server block has no `client_max_body_size` directive.

- [ ] **Step 2: Back up and edit the dormant host configuration**

```bash
ssh tencent-rss-pal \
  "sudo cp /etc/nginx/sites-available/rss-pal /etc/nginx/sites-available/rss-pal.bak-20260825-gitbook-limit && \
   sudo perl -0pi -e 's/(server \\{\\n    server_name rss\\.morefreeze\\.top;\\n)/\$1    client_max_body_size 5m;\\n/' /etc/nginx/sites-available/rss-pal"
```

Expected: the original file is preserved at the explicit backup path and only the first (HTTPS) `rss.morefreeze.top` server block receives the directive.

- [ ] **Step 3: Validate the dormant live configuration and show exact scope**

```bash
ssh tencent-rss-pal \
  "sudo diff -u /etc/nginx/sites-available/rss-pal.bak-20260825-gitbook-limit /etc/nginx/sites-available/rss-pal || true; sudo nginx -t"
```

Expected: the diff contains one added `client_max_body_size 5m;` line and `nginx -t` succeeds. Do not reload Nginx yet.

- [ ] **Step 4: Obtain fresh confirmation for the operational boundary**

Report that the next action will replace only `rss-pal-frontend-1` and reload host Nginx after a successful syntax check. Wait for confirmation before either action.

### Task 5: Deploy the two-layer limit after confirmation

**Files:**
- No additional source changes.

- [ ] **Step 1: Confirm Tencent source contains the implementation commit**

```bash
ssh tencent-rss-pal \
  "cd /opt/rss-pal && git fetch origin master && git rev-list HEAD..origin/master --count && git log -1 --oneline origin/master"
```

Expected: `origin/master` ends at the implementation commit. If the checkout is behind, run the existing `/opt/rss-pal/scripts/auto_deploy.sh` path rather than merging around it.

- [ ] **Step 2: Rebuild and replace only the frontend container**

```bash
ssh tencent-rss-pal \
  "cd /opt/rss-pal && docker compose build frontend && docker compose up -d --no-deps frontend"
```

Expected: `rss-pal-frontend-1` is recreated and returns to `Up` state; unrelated containers are not recreated.

- [ ] **Step 3: Verify the container Nginx limit before changing public traffic**

```bash
ssh tencent-rss-pal \
  "cd /opt/rss-pal && docker compose exec -T frontend nginx -T 2>/dev/null | grep -F 'client_max_body_size 5m;' && docker compose exec -T frontend nginx -t"
```

Expected: the directive is present and the container configuration test succeeds.

- [ ] **Step 4: Reload only the host Nginx configuration**

```bash
ssh tencent-rss-pal "sudo nginx -t && sudo systemctl reload nginx"
```

Expected: syntax validation succeeds and Nginx reloads without restarting the host or containers.

### Task 6: Verify the public chain and real capture paths

**Files:**
- No source changes.

- [ ] **Step 1: Prove a body larger than 1 MiB reaches Go**

Run locally:

```bash
perl -e 'print qq({"url":"https://example.com/probe","title":"probe","html":"); print "x" x 1300000; print qq("})' \
  | curl -sS -o /tmp/rss-pal-large-body-probe.out -w '%{http_code}\n' \
      -H 'Content-Type: application/json' \
      -H 'Authorization: Bearer rss-pal-large-body-probe' \
      --data-binary @- \
      https://rss.morefreeze.top/api/bookmarklet/capture
```

Expected: HTTP `401`, proving both Nginx layers accepted and forwarded the 1.3 MB request. Before the repair this request returns `413`.

- [ ] **Step 2: Verify public health and frontend identity**

```bash
curl -fsS https://rss.morefreeze.top/api/health
ssh tencent-rss-pal \
  "cd /opt/rss-pal && docker compose ps frontend api && docker inspect -f '{{.Image}}' rss-pal-frontend-1"
```

Expected: public health succeeds; frontend and API are running; the frontend container points at the newly built image.

- [ ] **Step 3: Verify the real bookmarklet path**

Open `https://morefreeze.gitbook.io/mon-test`, click the RSS Pal bookmarklet once, and confirm the receiver reports created, updated, or duplicate instead of HTTP 413.

- [ ] **Step 4: Verify the real extension path**

On the same GitBook page, click the RSS Pal extension and confirm its popup reports success or duplicate instead of HTTP 413.

- [ ] **Step 5: Verify production logs contain no new proxy rejection**

```bash
ssh tencent-rss-pal \
  "sudo tail -200 /var/log/nginx/access.log | grep -E 'POST /api/bookmarklet/capture|POST /api/extension/ingest' | tail -20; sudo tail -200 /var/log/nginx/error.log | grep 'client intended to send too large body' | tail -20"
```

Expected: the two manual requests are not 413, and the error-log timestamps do not advance after deployment.

### Task 7: Roll back if verification fails

**Files:**
- Restore on Tencent: `/etc/nginx/sites-available/rss-pal`

- [ ] **Step 1: Restore the host configuration only if host validation or reload fails**

```bash
ssh tencent-rss-pal \
  "sudo cp /etc/nginx/sites-available/rss-pal.bak-20260825-gitbook-limit /etc/nginx/sites-available/rss-pal && sudo nginx -t && sudo systemctl reload nginx"
```

Expected: the previous host configuration is restored and Nginx reload succeeds.

- [ ] **Step 2: Keep the source repair for diagnosis unless it independently caused the failure**

Do not reset, force-push, or remove user files. If the frontend image is the failing component, report its image ID and the observed error before requesting permission for any image rollback.

## Execution routing

Use **Inline Execution** with `superpowers:executing-plans`. The source change is one directive plus one focused regression test; production operations are tightly coupled to the same two-layer limit and do not benefit from parallel delegation.
