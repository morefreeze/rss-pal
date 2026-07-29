# OCI to Beijing Reverse Proxy Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `rss.morefreeze.top` serve the Beijing rss-pal application through OCI without changing public DNS or resuming OCI application writes.

**Architecture:** OCI remains the public TLS endpoint and proxies all requests to Beijing over HTTPS. Upstream SNI is disabled to avoid the mainland filing filter, while certificate verification uses `rss.morefreeze.top`; all Beijing SSH operations go through OCI.

**Tech Stack:** Nginx 1.24 on OCI, HTTPS/TLS, Docker Compose, PostgreSQL, curl, OpenSSL

---

### Task 1: Capture the failing state and validate the upstream

**Files:**
- Inspect: `/etc/nginx/sites-enabled/rss-pal` on `oci-rss-pal`
- Inspect: `/opt/rss-pal/docker-compose.yml` on both servers

- [ ] **Step 1: Confirm the public API currently fails on OCI**

Run:

```bash
curl --noproxy '*' -ksS -o /dev/null \
  -w 'status=%{http_code} remote=%{remote_ip}\n' \
  https://rss.morefreeze.top/api/health
```

Expected before cutover:

```text
status=502 remote=158.179.181.219
```

- [ ] **Step 2: Confirm OCI can reach Beijing without SNI**

Run:

```bash
ssh oci-rss-pal \
  "curl --noproxy '*' -kfsS https://192.144.171.125/api/health \
    -H 'Host: rss.morefreeze.top'"
```

Expected:

```json
{"status":"ok","version":"v0.0.2"}
```

- [ ] **Step 3: Confirm certificate verification works without SNI**

Run:

```bash
ssh oci-rss-pal \
  "printf '' | openssl s_client \
    -connect 192.144.171.125:443 \
    -noservername \
    -verify_hostname rss.morefreeze.top \
    -CAfile /etc/ssl/certs/ca-certificates.crt 2>/dev/null |
    tail -n 3"
```

Expected:

```text
Early data was not sent
Verify return code: 0 (ok)
```

- [ ] **Step 4: Confirm Beijing state through the OCI jump host**

Run:

```bash
ssh -o ControlMaster=no -o ControlPath=none \
  -o ProxyJump=oci-rss-pal tencent-rss-pal \
  "cd /opt/rss-pal &&
   docker compose ps --status running --services &&
   docker compose exec -T postgres psql -U postgres -d rsspal -Atc \
     'SELECT (SELECT count(*) FROM users),
             (SELECT count(*) FROM feeds),
             (SELECT count(*) FROM articles);'"
```

Expected services include `api`, `frontend`, `postgres`, `rsshub`,
`status-monitor`, and `worker`. The approved database snapshot contains:

```text
2|30|2373
```

### Task 2: Install the reversible OCI Nginx change

**Files:**
- Create temporarily: `/Users/bytedance/mygit/rss-pal/.codex-rss-pal-oci-nginx.conf`
- Modify: `/etc/nginx/sites-enabled/rss-pal` on `oci-rss-pal`
- Create backup: `/etc/nginx/rss-pal.pre-beijing-20260729T1925.bak` on `oci-rss-pal`

- [ ] **Step 1: Create the exact candidate Nginx configuration**

Create `/Users/bytedance/mygit/rss-pal/.codex-rss-pal-oci-nginx.conf` with:

```nginx
server {
    server_name rss.morefreeze.top;

    location / {
        proxy_pass https://192.144.171.125;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_read_timeout 60s;

        proxy_ssl_server_name off;
        proxy_ssl_name rss.morefreeze.top;
        proxy_ssl_verify on;
        proxy_ssl_verify_depth 3;
        proxy_ssl_trusted_certificate /etc/ssl/certs/ca-certificates.crt;
    }

    listen 443 ssl http2; # managed by Certbot
    ssl_certificate /etc/letsencrypt/live/rss.morefreeze.top/fullchain.pem; # managed by Certbot
    ssl_certificate_key /etc/letsencrypt/live/rss.morefreeze.top/privkey.pem; # managed by Certbot
    include /etc/letsencrypt/options-ssl-nginx.conf; # managed by Certbot
    ssl_dhparam /etc/letsencrypt/ssl-dhparams.pem; # managed by Certbot
}

server {
    if ($host = rss.morefreeze.top) {
        return 301 https://$host$request_uri;
    } # managed by Certbot

    listen 80;
    server_name rss.morefreeze.top;
    return 404; # managed by Certbot
}
```

- [ ] **Step 2: Copy the candidate and save the current live file**

Run:

```bash
scp .codex-rss-pal-oci-nginx.conf oci-rss-pal:/tmp/rss-pal.nginx.candidate
ssh oci-rss-pal \
  "sudo install -m 0644 /etc/nginx/sites-enabled/rss-pal \
     /etc/nginx/rss-pal.pre-beijing-20260729T1925.bak &&
   sudo install -m 0644 /tmp/rss-pal.nginx.candidate \
     /etc/nginx/sites-enabled/rss-pal"
```

Expected: exit status `0`.

- [ ] **Step 3: Validate before reload**

Run:

```bash
ssh oci-rss-pal "sudo nginx -t"
```

Expected:

```text
syntax is ok
test is successful
```

- [ ] **Step 4: Reload Nginx**

Run:

```bash
ssh oci-rss-pal "sudo systemctl reload nginx && systemctl is-active nginx"
```

Expected:

```text
active
```

### Task 3: Verify the cutover or roll back

**Files:**
- Verify: `/etc/nginx/sites-enabled/rss-pal` on `oci-rss-pal`
- Rollback source: `/etc/nginx/rss-pal.pre-beijing-20260729T1925.bak`

- [ ] **Step 1: Verify the public frontend and API**

Run:

```bash
curl --noproxy '*' -fsS -o /dev/null \
  -w 'root=%{http_code} remote=%{remote_ip}\n' \
  https://rss.morefreeze.top/
curl --noproxy '*' -fsS \
  https://rss.morefreeze.top/api/health
curl --noproxy '*' -ksS -o /dev/null \
  -w 'articles=%{http_code} remote=%{remote_ip}\n' \
  'https://rss.morefreeze.top/api/articles?page=1&limit=1'
```

Expected:

```text
root=200 remote=158.179.181.219
{"status":"ok","version":"v0.0.2"}
articles=401 remote=158.179.181.219
```

The unauthenticated `401` confirms the Beijing API is now reachable rather
than returning the previous OCI `502`.

- [ ] **Step 2: Verify the authenticated page**

Open `https://rss.morefreeze.top/` in the user's existing authenticated browser
session. Expected: the article list loads and contains data from the accepted
Beijing snapshot.

- [ ] **Step 3: Verify write ownership**

Run:

```bash
ssh oci-rss-pal \
  "cd /opt/rss-pal &&
   docker compose ps --status running --services | sort &&
   docker compose exec -T postgres psql -U postgres -d rsspal -Atc \
     \"SELECT count(*) FROM pg_stat_activity
       WHERE datname=current_database()
       AND pid<>pg_backend_pid()
       AND state IS DISTINCT FROM 'idle';\""
```

Expected: OCI `api`, `worker`, and `status-monitor` are absent; active business
connections are `0`.

Run:

```bash
ssh -o ControlMaster=no -o ControlPath=none \
  -o ProxyJump=oci-rss-pal tencent-rss-pal \
  "cd /opt/rss-pal &&
   docker compose ps --status running --services | sort"
```

Expected: Beijing `api`, `frontend`, `postgres`, `rsshub`, `status-monitor`, and
`worker` are running.

- [ ] **Step 4: Roll back if any required check fails**

Run only on failure:

```bash
ssh oci-rss-pal \
  "sudo install -m 0644 \
     /etc/nginx/rss-pal.pre-beijing-20260729T1925.bak \
     /etc/nginx/sites-enabled/rss-pal &&
   sudo nginx -t &&
   sudo systemctl reload nginx"
```

Expected: Nginx returns to the pre-cutover configuration and public
`/api/health` returns the previous `502`.

- [ ] **Step 5: Remove the local candidate after successful verification**

Delete only:

```text
/Users/bytedance/mygit/rss-pal/.codex-rss-pal-oci-nginx.conf
```

- [ ] **Step 6: Commit the implementation record**

Run:

```bash
git add docs/superpowers/plans/2026-07-29-oci-beijing-reverse-proxy.md
git commit -m "docs: plan OCI to Beijing proxy cutover"
```

Expected: one commit containing only this plan.
