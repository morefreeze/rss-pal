#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(cd "$(dirname "$0")/../.." && pwd)
SOURCE_FILE="$ROOT_DIR/scripts/auto_deploy.sh"

FUNCTION_BODY=$(sed -n '/^wait_for_outbound_proxy() {$/,/^}$/p' "$SOURCE_FILE")
if [ -z "$FUNCTION_BODY" ]; then
  echo "FAIL: wait_for_outbound_proxy is missing" >&2
  exit 1
fi
eval "$FUNCTION_BODY"

logs=""
log() {
  logs+="$*"$'\n'
}

curl_attempts=0
curl() {
  curl_attempts=$((curl_attempts + 1))
  [ "$curl_attempts" -ge 3 ]
}

sleep_calls=0
sleep() {
  sleep_calls=$((sleep_calls + 1))
}

DEPLOY_PROXY=http://proxy.test:3128
wait_for_outbound_proxy
[ "$curl_attempts" -eq 3 ] || {
  echo "FAIL: expected 3 readiness attempts, got $curl_attempts" >&2
  exit 1
}
[ "$sleep_calls" -eq 2 ] || {
  echo "FAIL: expected 2 sleeps before readiness, got $sleep_calls" >&2
  exit 1
}
[[ "$logs" == *"Outbound proxy is ready: $DEPLOY_PROXY"* ]] || {
  echo "FAIL: readiness was not logged" >&2
  exit 1
}

curl_attempts=0
sleep_calls=0
logs=""
curl() {
  curl_attempts=$((curl_attempts + 1))
  return 1
}

if wait_for_outbound_proxy; then
  echo "FAIL: permanently unavailable proxy was accepted" >&2
  exit 1
fi
[ "$curl_attempts" -eq 15 ] || {
  echo "FAIL: expected 15 readiness attempts, got $curl_attempts" >&2
  exit 1
}
[ "$sleep_calls" -eq 14 ] || {
  echo "FAIL: expected 14 sleeps before timeout, got $sleep_calls" >&2
  exit 1
}
[[ "$logs" == *"Outbound proxy did not become ready: $DEPLOY_PROXY"* ]] || {
  echo "FAIL: timeout was not logged" >&2
  exit 1
}

echo "PASS: outbound proxy readiness is bounded and retryable"
