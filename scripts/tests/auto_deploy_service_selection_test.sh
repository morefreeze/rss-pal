#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(cd "$(dirname "$0")/../.." && pwd)
SOURCE_FILE="$ROOT_DIR/scripts/auto_deploy.sh"

FUNCTIONS=$(sed -n '/^add_deploy_service() {$/,/^}$/p; /^select_deploy_services() {$/,/^}$/p; /^configure_compose_files() {$/,/^}$/p; /^deploy_runtime_services() {$/,/^}$/p' "$SOURCE_FILE")
if [[ "$FUNCTIONS" != *"select_deploy_services()"* ]]; then
  echo "FAIL: select_deploy_services is missing" >&2
  exit 1
fi
eval "$FUNCTIONS"

assert_selection() {
  local changed_files="$1"
  local expected_all="$2"
  local expected_services="$3"

  select_deploy_services "$changed_files"
  [ "$DEPLOY_ALL" = "$expected_all" ] || {
    echo "FAIL: DEPLOY_ALL=$DEPLOY_ALL, want $expected_all for $changed_files" >&2
    exit 1
  }
  [ "${DEPLOY_SERVICES[*]}" = "$expected_services" ] || {
    echo "FAIL: services=${DEPLOY_SERVICES[*]}, want $expected_services for $changed_files" >&2
    exit 1
  }
}

assert_selection $'backend/internal/rss/content.go\ndocs/note.md' false "api worker"
assert_selection $'frontend/src/App.tsx\nnginx.prod.conf' false "frontend"
assert_selection 'status-monitor/server.py' false "status-monitor"
assert_selection $'backend/internal/rss/content.go\nfrontend/src/App.tsx\nstatus-monitor/server.py' false "api worker frontend status-monitor"
assert_selection 'docker-compose.yml' true ""
assert_selection 'docs/readme.md' false ""

compose_calls=""
compose_mock() {
  compose_calls+="$*"$'\n'
}
COMPOSE=compose_mock
COMPOSE_FILES=(-f docker-compose.yml)

select_deploy_services $'backend/internal/rss/content.go\nfrontend/src/App.tsx'
deploy_runtime_services
[[ "$compose_calls" == *"-f docker-compose.yml build api worker frontend"* ]] || {
  echo "FAIL: selected services were not passed to compose build: $compose_calls" >&2
  exit 1
}
[[ "$compose_calls" == *"-f docker-compose.yml up -d api worker"* ]] || {
  echo "FAIL: backend services did not retain their dependency-aware compose up: $compose_calls" >&2
  exit 1
}
[[ "$compose_calls" == *"-f docker-compose.yml up -d --no-deps frontend"* ]] || {
  echo "FAIL: frontend was not restarted independently: $compose_calls" >&2
  exit 1
}
[[ "$compose_calls" != *"status-monitor"* ]] || {
  echo "FAIL: unchanged status-monitor leaked into scoped deploy: $compose_calls" >&2
  exit 1
}

compose_calls=""
select_deploy_services 'frontend/src/App.tsx'
deploy_runtime_services
[ "$compose_calls" = $'-f docker-compose.yml build frontend\n-f docker-compose.yml up -d --no-deps frontend\n' ] || {
  echo "FAIL: frontend-only deploy touched its dependency chain: $compose_calls" >&2
  exit 1
}

compose_calls=""
select_deploy_services 'docker-compose.yml'
deploy_runtime_services
[ "$compose_calls" = $'-f docker-compose.yml up -d --build\n' ] || {
  echo "FAIL: compose changes must retain full deployment: $compose_calls" >&2
  exit 1
}

compose_calls=""
compose_mock() {
  compose_calls+="$*"$'\n'
  [[ "$*" == *" build "* ]] && return 23
  return 0
}
select_deploy_services 'frontend/src/App.tsx'
if deploy_runtime_services; then
  echo "FAIL: compose build failure was swallowed" >&2
  exit 1
fi
[ "$compose_calls" = $'-f docker-compose.yml build frontend\n' ] || {
  echo "FAIL: compose up ran after build failure: $compose_calls" >&2
  exit 1
}

log() { :; }
ORIGINAL_DIR=$PWD
TMP_DIR=$(mktemp -d)
trap 'rm -rf "$TMP_DIR"' EXIT
cd "$TMP_DIR"
touch docker-compose.yml docker-compose.override.yml
touch docker-compose.override.oci-egress.v2.yml
configure_compose_files
[ "${COMPOSE_FILES[*]}" = "-f docker-compose.yml -f docker-compose.override.yml -f docker-compose.override.oci-egress.v2.yml" ] || {
  echo "FAIL: compose file discovery missed newest override: ${COMPOSE_FILES[*]}" >&2
  exit 1
}
cd "$ORIGINAL_DIR"

echo "PASS: deploy service selection is scoped by changed paths"
