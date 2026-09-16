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
  [ "${DEPLOY_SERVICES[*]:-}" = "$expected_services" ] || {
    echo "FAIL: services=${DEPLOY_SERVICES[*]:-}, want $expected_services for $changed_files" >&2
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

assert_call_order() {
  local previous_line=0 expected line
  for expected in "$@"; do
    line=$(printf '%s' "$compose_calls" | grep -nFx -- "$expected" | cut -d: -f1)
    [ -n "$line" ] || {
      echo "FAIL: missing compose call '$expected': $compose_calls" >&2
      exit 1
    }
    [ "$line" -gt "$previous_line" ] || {
      echo "FAIL: compose call '$expected' was out of order: $compose_calls" >&2
      exit 1
    }
    previous_line=$line
  done
}

select_deploy_services $'backend/internal/rss/content.go\nfrontend/src/App.tsx'
deploy_runtime_services
assert_call_order \
  '-f docker-compose.yml build api worker frontend' \
  '-f docker-compose.yml up -d --no-deps frontend' \
  '-f docker-compose.yml up -d api worker'
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
[ "$compose_calls" = $'-f docker-compose.yml build\n-f docker-compose.yml up -d --no-deps frontend\n-f docker-compose.yml up -d\n' ] || {
  echo "FAIL: compose changes must build first, replace frontend independently, then start all services: $compose_calls" >&2
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

COMPOSE_FILE="$ROOT_DIR/docker-compose.yml"
WORKFLOW_FILE="$ROOT_DIR/.github/workflows/deploy-tencent.yml"
README_FILE="$ROOT_DIR/README.md"

status_migrate_block=$(sed -n '/^  status-migrate:$/,/^  api:$/p' "$COMPOSE_FILE")
migration_command=$(printf '%s\n' "$status_migrate_block" | sed -n 's/^    command: \["sh", "-ec", "\(.*\)"\]$/\1/p')
expected_migration_command='psql -v ON_ERROR_STOP=1 -f /migrations/037_service_heartbeats.sql && psql -v ON_ERROR_STOP=1 -f /migrations/038_subscription_explore.sql && psql -v ON_ERROR_STOP=1 -f /migrations/039_article_shares.sql && psql -v ON_ERROR_STOP=1 -f /migrations/040_explore_provider_materialized_at.sql && psql -v ON_ERROR_STOP=1 -f /migrations/041_article_share_short_codes.sql && psql -v ON_ERROR_STOP=1 -f /migrations/042_auth_rate_limits.sql && psql -v ON_ERROR_STOP=1 -f /migrations/043_task_budgets.sql'
[ "$migration_command" = "$expected_migration_command" ] || {
  echo "FAIL: status-migrate must execute ordered 037-042 psql steps joined only by &&" >&2
  exit 1
}

required_short_origin='SHORT_SHARE_ORIGIN: ${SHORT_SHARE_ORIGIN:?SHORT_SHARE_ORIGIN required in .env}'
short_origin_count=$(grep -Fc -- "$required_short_origin" "$COMPOSE_FILE" || true)
[ "$short_origin_count" -eq 1 ] || {
  echo "FAIL: required SHORT_SHARE_ORIGIN must appear exactly once, got $short_origin_count" >&2
  exit 1
}
all_short_origin_count=$(grep -Ec '^[[:space:]]+SHORT_SHARE_ORIGIN:' "$COMPOSE_FILE" || true)
[ "$all_short_origin_count" -eq 1 ] || {
  echo "FAIL: SHORT_SHARE_ORIGIN must be injected only once, got $all_short_origin_count" >&2
  exit 1
}
api_block=$(sed -n '/^  api:$/,/^  worker:$/p' "$COMPOSE_FILE")
[[ "$api_block" == *"$required_short_origin"* ]] || {
  echo "FAIL: required SHORT_SHARE_ORIGIN is not scoped to the API service" >&2
  exit 1
}

short_curl_count=0
while IFS= read -r short_curl; do
  [ -z "$short_curl" ] && continue
  short_curl_count=$((short_curl_count + 1))
  [[ "$short_curl" == *"curl --noproxy '*'"* ]] || {
    echo "FAIL: short-domain curl is not explicitly direct: $short_curl" >&2
    exit 1
  }
done <<EOF_SHORT_CURLS
$(grep -F -- '--resolve r.morefreeze.top:443:192.144.171.125' "$WORKFLOW_FILE" || true)
EOF_SHORT_CURLS
[ "$short_curl_count" -eq 3 ] || {
  echo "FAIL: expected three short-domain curl gates, got $short_curl_count" >&2
  exit 1
}

short_origin_readme_row=$(grep -F -- '| `SHORT_SHARE_ORIGIN` |' "$README_FILE" || true)
[[ "$short_origin_readme_row" == *'| —（必填） |'* ]] || {
  echo "FAIL: README must mark SHORT_SHARE_ORIGIN required without a default" >&2
  exit 1
}
[[ "$short_origin_readme_row" == *'只支持精确值 `https://r.morefreeze.top`'* ]] || {
  echo "FAIL: README must document r.morefreeze.top as the only supported short origin" >&2
  exit 1
}
quick_start_section=$(sed -n '/### 使用 Docker Compose（推荐）/,/^2\. 启动：/p' "$README_FILE")
[[ "$quick_start_section" == *'`SHORT_SHARE_ORIGIN` 只支持精确值 `https://r.morefreeze.top`'* ]] || {
  echo "FAIL: README quick start must require the exact production short origin" >&2
  exit 1
}
[[ "$quick_start_section" == *'必须通过 hosts、DNS 或反向代理把这个精确域名路由到本地实例'* ]] || {
  echo "FAIL: README quick start must explain exact-host routing for local E2E" >&2
  exit 1
}

echo "PASS: deploy service selection is scoped by changed paths"
