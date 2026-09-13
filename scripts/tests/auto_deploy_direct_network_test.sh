#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(cd "$(dirname "$0")/../.." && pwd)
SOURCE_FILE="$ROOT_DIR/scripts/auto_deploy.sh"

FUNCTIONS=$(sed -n '/^configure_outbound_proxy() {$/,/^}$/p; /^wait_for_outbound_proxy() {$/,/^}$/p; /^refresh_outbound_proxy() {$/,/^}$/p' "$SOURCE_FILE")
if [[ "$FUNCTIONS" != *"configure_outbound_proxy()"* ]] || [[ "$FUNCTIONS" != *"wait_for_outbound_proxy()"* ]] || [[ "$FUNCTIONS" != *"refresh_outbound_proxy()"* ]]; then
  echo "FAIL: direct deployment requires configurable proxy and tunnel refresh functions" >&2
  exit 1
fi
eval "$FUNCTIONS"

logs=""
events=""
log() {
  logs+="$*"$'\n'
}

curl_calls=0
curl() {
  curl_calls=$((curl_calls + 1))
  events+="curl $*"$'\n'
  return 0
}

systemctl_calls=0
systemctl_args=""
systemctl() {
  systemctl_calls=$((systemctl_calls + 1))
  systemctl_args+="$*"$'\n'
  events+="systemctl $*"$'\n'
  return 0
}

sudo_calls=0
sudo_args=""
sudo() {
  sudo_calls=$((sudo_calls + 1))
  sudo_args+="$*"$'\n'
  events+="sudo $*"$'\n'
  return 0
}

sleep_calls=0
sleep() {
  sleep_calls=$((sleep_calls + 1))
}

export RSS_PAL_DEPLOY_DIRECT=1
export http_proxy=http://lower-http.test
export https_proxy=http://lower-https.test
export all_proxy=socks5://lower-all.test
export HTTP_PROXY=http://upper-http.test
export HTTPS_PROXY=http://upper-https.test
export ALL_PROXY=socks5://upper-all.test
export no_proxy=localhost
export NO_PROXY=localhost

configure_outbound_proxy
refresh_outbound_proxy

[ "$curl_calls" -eq 0 ] || {
  echo "FAIL: direct deployment probed the network through curl" >&2
  exit 1
}
for proxy_var in http_proxy https_proxy all_proxy HTTP_PROXY HTTPS_PROXY ALL_PROXY; do
  [ -z "${!proxy_var+x}" ] || {
    echo "FAIL: direct deployment left $proxy_var set" >&2
    exit 1
  }
done
[ "$no_proxy" = "*" ] && [ "$NO_PROXY" = "*" ] || {
  echo "FAIL: direct deployment did not bypass proxies for every destination" >&2
  exit 1
}
[ "$systemctl_calls" -eq 0 ] && [ "$sudo_calls" -eq 0 ] && [ "$curl_calls" -eq 0 ] || {
  echo "FAIL: direct deployment touched the OCI egress tunnel" >&2
  exit 1
}
[ -z "$events" ] || {
  echo "FAIL: direct deployment performed an egress event: $events" >&2
  exit 1
}
lower_logs=$(printf '%s' "$logs" | tr '[:upper:]' '[:lower:]')
[[ "$lower_logs" == *"direct"* ]] || {
  echo "FAIL: direct deployment mode was not logged" >&2
  exit 1
}

unset RSS_PAL_DEPLOY_DIRECT
logs=""
curl_calls=0
systemctl_calls=0
systemctl_args=""
sudo_calls=0
sudo_args=""
sleep_calls=0
events=""
DEPLOY_PROXY=http://proxy.test:3128

refresh_outbound_proxy

[ "$systemctl_calls" -eq 1 ] && [[ "$systemctl_args" == *'cat rss-pal-oci-egress.service'* ]] || {
  echo "FAIL: normal deployment did not discover rss-pal-oci-egress.service" >&2
  exit 1
}
[ "$sudo_calls" -eq 1 ] && [[ "$sudo_args" == *'-n systemctl restart rss-pal-oci-egress.service'* ]] || {
  echo "FAIL: normal deployment did not restart rss-pal-oci-egress.service" >&2
  exit 1
}
[ "$curl_calls" -eq 1 ] && [ "$sleep_calls" -eq 0 ] || {
  echo "FAIL: normal deployment did not probe the refreshed proxy exactly once" >&2
  exit 1
}
[ "$events" = $'systemctl cat rss-pal-oci-egress.service\nsudo -n systemctl restart rss-pal-oci-egress.service\ncurl -fsS --connect-timeout 2 --max-time 5 -x http://proxy.test:3128 https://api.github.com/rate_limit\n' ] || {
  echo "FAIL: normal deployment did not restart before its readiness probe: $events" >&2
  exit 1
}
[[ "$logs" == *"Restarted rss-pal-oci-egress.service"* ]] && [[ "$logs" == *"Outbound proxy is ready: $DEPLOY_PROXY"* ]] || {
  echo "FAIL: normal deployment did not complete the refresh/readiness flow" >&2
  exit 1
}

echo "PASS: direct deployment skips egress setup and normal deployment refreshes it"
