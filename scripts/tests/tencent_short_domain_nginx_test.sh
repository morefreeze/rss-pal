#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(cd "$(dirname "$0")/../.." && pwd)
BOOTSTRAP_CONFIG="$ROOT_DIR/deploy/nginx/r-morefreeze-bootstrap.conf"
FINAL_CONFIG="$ROOT_DIR/deploy/nginx/rss-pal-tencent.conf"
DEPLOY_WRAPPER="$ROOT_DIR/deploy/tencent/rss-pal-deploy-from-actions"

fail() {
  echo "FAIL: $*" >&2
  exit 1
}

assert_contains() {
  local file=$1
  local text=$2
  local description=$3
  grep -Fq -- "$text" "$file" || fail "$description"
}

assert_not_contains() {
  local file=$1
  local text=$2
  local description=$3
  if grep -Fq -- "$text" "$file"; then
    fail "$description"
  fi
}

assert_count() {
  local expected=$1
  local text=$2
  local file=$3
  local description=$4
  local actual
  actual=$(grep -Fc -- "$text" "$file" || true)
  [ "$actual" -eq "$expected" ] || fail "$description (expected $expected, got $actual)"
}

server_block() {
  local number=$1
  local file=$2
  awk -v wanted="$number" '
    /^server \{$/ { current++ }
    current == wanted { print }
    current > wanted { exit }
  ' "$file"
}

for required_file in "$BOOTSTRAP_CONFIG" "$FINAL_CONFIG" "$DEPLOY_WRAPPER"; do
  [ -f "$required_file" ] || fail "missing ${required_file#"$ROOT_DIR"/}"
done

# The bootstrap listener must expose only ACME HTTP validation for the short host.
assert_count 1 'server {' "$BOOTSTRAP_CONFIG" 'bootstrap must contain exactly one server'
assert_contains "$BOOTSTRAP_CONFIG" 'listen 80;' 'bootstrap must listen on HTTP port 80'
assert_contains "$BOOTSTRAP_CONFIG" 'server_name r.morefreeze.top;' 'bootstrap must select only the short host'
assert_contains "$BOOTSTRAP_CONFIG" 'access_log off;' 'bootstrap must disable access logging'
assert_contains "$BOOTSTRAP_CONFIG" 'location ^~ /.well-known/acme-challenge/ {' 'bootstrap must serve ACME challenges'
assert_contains "$BOOTSTRAP_CONFIG" 'root /var/www/html;' 'bootstrap must use the shared ACME webroot'
assert_contains "$BOOTSTRAP_CONFIG" 'location / {' 'bootstrap must define a catch-all location'
assert_contains "$BOOTSTRAP_CONFIG" 'return 404;' 'bootstrap catch-all must return 404'
assert_count 2 'location ' "$BOOTSTRAP_CONFIG" 'bootstrap must contain only ACME and catch-all locations'
assert_not_contains "$BOOTSTRAP_CONFIG" 'proxy_pass' 'bootstrap must not expose the application'
assert_not_contains "$BOOTSTRAP_CONFIG" 'listen 443' 'bootstrap must not enable TLS before certificate issuance'
assert_not_contains "$BOOTSTRAP_CONFIG" 'ssl_certificate' 'bootstrap must not reference a certificate'
assert_not_contains "$BOOTSTRAP_CONFIG" 'rss.morefreeze.top' 'bootstrap must not claim the primary hostname'

# Final ingress consists of HTTP redirect and TLS servers for each hostname.
assert_count 4 'server {' "$FINAL_CONFIG" 'final ingress must contain exactly four servers'
MAIN_HTTP=$(server_block 1 "$FINAL_CONFIG")
MAIN_TLS=$(server_block 2 "$FINAL_CONFIG")
SHORT_HTTP=$(server_block 3 "$FINAL_CONFIG")
SHORT_TLS=$(server_block 4 "$FINAL_CONFIG")

[[ "$MAIN_HTTP" == *'listen 80;'* && "$MAIN_HTTP" == *'server_name rss.morefreeze.top;'* ]] || fail 'first server must be primary-host HTTP'
[[ "$MAIN_HTTP" == *'return 301 https://$host$request_uri;'* ]] || fail 'primary-host HTTP must redirect to TLS'
[[ "$MAIN_HTTP" != *'proxy_pass'* ]] || fail 'primary-host HTTP must not proxy'

[[ "$MAIN_TLS" == *'listen 443 ssl http2;'* && "$MAIN_TLS" == *'server_name rss.morefreeze.top;'* ]] || fail 'second server must be primary-host TLS'
[[ "$MAIN_TLS" == *'access_log off;'* ]] || fail 'primary-host TLS must disable access logging'
[[ "$MAIN_TLS" == *'client_max_body_size 5M;'* ]] || fail 'primary-host TLS must preserve the 5M request limit'
[[ "$MAIN_TLS" == *'proxy_read_timeout 60s;'* ]] || fail 'primary-host TLS must preserve the proxy timeout'
[[ "$MAIN_TLS" == *'location / {'* && "$MAIN_TLS" == *'proxy_pass http://127.0.0.1:8082;'* ]] || fail 'primary-host TLS must proxy its complete surface'
[[ "$MAIN_TLS" == *'ssl_certificate /etc/letsencrypt/live/rss.morefreeze.top/fullchain.pem;'* ]] || fail 'primary-host TLS must use its existing full chain'
[[ "$MAIN_TLS" == *'ssl_certificate_key /etc/letsencrypt/live/rss.morefreeze.top/privkey.pem;'* ]] || fail 'primary-host TLS must use its existing private key'

[[ "$SHORT_HTTP" == *'listen 80;'* && "$SHORT_HTTP" == *'server_name r.morefreeze.top;'* ]] || fail 'third server must be short-host HTTP'
[[ "$SHORT_HTTP" == *'return 301 https://$host$request_uri;'* ]] || fail 'short-host HTTP must redirect to TLS'
[[ "$SHORT_HTTP" != *'proxy_pass'* ]] || fail 'short-host HTTP must not proxy'

[[ "$SHORT_TLS" == *'listen 443 ssl http2;'* && "$SHORT_TLS" == *'server_name r.morefreeze.top;'* ]] || fail 'fourth server must be short-host TLS'
[[ "$SHORT_TLS" == *'access_log off;'* ]] || fail 'short-host TLS must disable access logging'
[[ "$SHORT_TLS" == *'ssl_certificate /etc/letsencrypt/live/r.morefreeze.top/fullchain.pem;'* ]] || fail 'short-host TLS must use its own full chain'
[[ "$SHORT_TLS" == *'ssl_certificate_key /etc/letsencrypt/live/r.morefreeze.top/privkey.pem;'* ]] || fail 'short-host TLS must use its own private key'
for location in \
  'location ^~ /api/s/ {' \
  'location = /api/proxy/image {' \
  'location ^~ /assets/ {' \
  'location = /favicon.svg {' \
  'location = /favicon-32.png {' \
  'location = /apple-touch-icon.png {' \
  'location ~ "^/[A-Za-z0-9]{12}$" {'; do
  [[ "$SHORT_TLS" == *"$location"* ]] || fail "short-host TLS is missing allowed route: $location"
done
[[ "$SHORT_TLS" == *'location / {'* && "$SHORT_TLS" == *'return 404;'* ]] || fail 'short-host TLS must reject every other route'
short_location_count=$(printf '%s\n' "$SHORT_TLS" | grep -c '^[[:space:]]*location ')
[ "$short_location_count" -eq 8 ] || fail "short-host TLS must contain exactly seven allowed routes and one catch-all (got $short_location_count)"
short_proxy_count=$(printf '%s\n' "$SHORT_TLS" | grep -Fc 'proxy_pass http://127.0.0.1:8082;' || true)
[ "$short_proxy_count" -eq 7 ] || fail "every and only allowed short-host route must proxy (got $short_proxy_count)"

# One main proxy plus seven short-host proxies, each forwarding the same identity headers.
assert_count 8 'proxy_pass http://127.0.0.1:8082;' "$FINAL_CONFIG" 'all eight allowed application surfaces must use the local frontend'
assert_count 8 'proxy_set_header Host $host;' "$FINAL_CONFIG" 'every proxy location must forward Host'
assert_count 8 'proxy_set_header X-Real-IP $remote_addr;' "$FINAL_CONFIG" 'every proxy location must forward X-Real-IP'
assert_count 8 'proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;' "$FINAL_CONFIG" 'every proxy location must forward X-Forwarded-For'
assert_count 8 'proxy_set_header X-Forwarded-Proto $scheme;' "$FINAL_CONFIG" 'every proxy location must forward X-Forwarded-Proto'

# The root-owned Actions entrypoint must fetch and run the branch deployment script directly as ubuntu.
[ -x "$DEPLOY_WRAPPER" ] || fail 'Actions deployment wrapper must be executable'
assert_contains "$DEPLOY_WRAPPER" 'flock -n 9' 'deployment wrapper must prevent concurrent runs'
assert_contains "$DEPLOY_WRAPPER" 'trap cleanup EXIT' 'deployment wrapper must always clean up its fetched script'
assert_contains "$DEPLOY_WRAPPER" 'git fetch origin master' 'deployment wrapper must refresh origin/master'
assert_contains "$DEPLOY_WRAPPER" 'git show origin/master:scripts/auto_deploy.sh' 'deployment wrapper must fetch the deployment script from origin/master'
DIRECT_ENV="sudo -H -u ubuntu env -u http_proxy -u https_proxy -u HTTP_PROXY -u HTTPS_PROXY -u all_proxy -u ALL_PROXY no_proxy='*' NO_PROXY='*'"
assert_count 2 "$DIRECT_ENV" "$DEPLOY_WRAPPER" 'bootstrap fetch and fetched script must each receive the direct-network environment'
assert_contains "$DEPLOY_WRAPPER" "NO_PROXY='*' RSS_PAL_DEPLOY_DIRECT=1 bash -lc" 'fetched deployment script must explicitly select direct mode'
if grep -Eq '(https?|socks[0-9a-z]*)://[^[:space:]'"'"']+' "$DEPLOY_WRAPPER"; then
  fail 'deployment wrapper must not define any proxy URL'
fi

echo 'PASS: Tencent bootstrap, final short-domain ingress, and direct Actions wrapper match the contract'
