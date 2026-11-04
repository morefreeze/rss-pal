#!/usr/bin/env bash
set -euo pipefail
ROOT_DIR=$(cd "$(dirname "$0")/../.." && pwd)
eval "$(sed -n '/^add_deploy_service() {$/,/^}$/p; /^select_deploy_services() {$/,/^}$/p; /^prepare_frontend_nginx() {$/,/^}$/p; /^verify_frontend_nginx() {$/,/^}$/p; /^deploy_runtime_services() {$/,/^}$/p; /^rollback_deployment() {$/,/^}$/p' "$ROOT_DIR/scripts/auto_deploy.sh")"
TASK_DIR=$(mktemp -d)
trap 'rm -rf "$TASK_DIR"' EXIT
PROJECT_DIR="$TASK_DIR"
mkdir -p "$PROJECT_DIR/scripts"
cp "$ROOT_DIR/scripts/ensure_image_proxy_nginx.py" "$PROJECT_DIR/scripts/"
cp "$ROOT_DIR/scripts/tests/fixtures/nginx.prod.before-image-timeout.conf" "$PROJECT_DIR/nginx.prod.conf"
log() { :; }
calls="$TASK_DIR/calls"
COMPOSE=compose_fixture
COMPOSE_FILES=(-f docker-compose.yml)
compose_fixture() {
  printf '%s\n' "$*" >> "$calls"
  case "$*" in
    *' exec -T frontend nginx -T') cat "$PROJECT_DIR/nginx.prod.conf" ;;
  esac
}

# A backend rollout also repairs the actual bind source and adds only frontend.
select_deploy_services 'backend/internal/api/proxy.go'
prepare_frontend_nginx
[[ "$FRONTEND_FORCE_RECREATE" = true && "${DEPLOY_SERVICES[*]}" = 'api worker frontend' ]]
[[ -f "$FRONTEND_NGINX_BACKUP" ]]
deploy_runtime_services
grep -Fx -- '-f docker-compose.yml up -d --no-deps --force-recreate frontend' "$calls"
grep -Fx -- '-f docker-compose.yml exec -T frontend nginx -t' "$calls"
grep -Fx -- '-f docker-compose.yml exec -T frontend nginx -T' "$calls"
[[ "$(tail -1 "$calls")" = '-f docker-compose.yml up -d api worker' ]]
[[ "$(cat "$calls")" != *'status-monitor'* ]]

# Updating this helper must be a frontend change even with no source nginx edit.
select_deploy_services 'scripts/ensure_image_proxy_nginx.py'
[[ "${DEPLOY_SERVICES[*]}" = frontend ]]
prepare_frontend_nginx
[[ "$FRONTEND_FORCE_RECREATE" = false ]]
: > "$calls"
deploy_runtime_services
! grep -F -- '--force-recreate' "$calls"
grep -Fx -- '-f docker-compose.yml up -d --no-deps frontend' "$calls"

# A valid host file does not prove the already-running container loaded it.
select_deploy_services 'backend/internal/api/proxy.go'
prepare_frontend_nginx
: > "$calls"
deploy_runtime_services
grep -Fx -- '-f docker-compose.yml exec -T frontend nginx -T' "$calls"
! grep -F -- 'up -d --no-deps frontend' "$calls"

# nginx syntax and the running configuration are separate mandatory checks.
compose_fixture() {
  case "$*" in
    *' exec -T frontend nginx -t') return 7 ;;
  esac
}
if verify_frontend_nginx; then echo 'FAIL: nginx syntax failure ignored' >&2; exit 1; fi
compose_fixture() {
  case "$*" in
    *' exec -T frontend nginx -T') cat "$ROOT_DIR/scripts/tests/fixtures/nginx.prod.before-image-timeout.conf" ;;
  esac
}
if verify_frontend_nginx; then echo 'FAIL: stale running mount accepted' >&2; exit 1; fi
: > "$calls"
compose_fixture() {
  printf '%s\n' "$*" >> "$calls"
  case "$*" in
    *' exec -T frontend nginx -T') cat "$ROOT_DIR/scripts/tests/fixtures/nginx.prod.before-image-timeout.conf" ;;
  esac
}
if deploy_runtime_services; then echo 'FAIL: backend started after stale mounted config' >&2; exit 1; fi
! grep -F -- 'up -d api worker' "$calls"

cp "$ROOT_DIR/scripts/tests/fixtures/nginx.prod.before-image-timeout.conf" "$PROJECT_DIR/nginx.prod.conf"
printf '\nserver { location ^~ /api { return 404; } }\n' >> "$PROJECT_DIR/nginx.prod.conf"
cp "$PROJECT_DIR/nginx.prod.conf" "$TASK_DIR/before-refusal"
if prepare_frontend_nginx; then echo 'FAIL: unrecognized mounted config was overwritten' >&2; exit 1; fi
cmp "$PROJECT_DIR/nginx.prod.conf" "$TASK_DIR/before-refusal"

# Rollback restores the private mount before checking out an older revision
# (which may remove the helper), then recreates frontend against that inode.
cp "$ROOT_DIR/scripts/tests/fixtures/nginx.prod.before-image-timeout.conf" "$PROJECT_DIR/nginx.prod.conf"
select_deploy_services 'backend/internal/api/proxy.go'
prepare_frontend_nginx
PREV_COMMIT=previous
LOG_FILE="$TASK_DIR/deploy.log"
configure_compose_files() { :; }
sleep() { :; }
git() {
  if [ "$1" = checkout ]; then
    cmp "$PROJECT_DIR/nginx.prod.conf" "$ROOT_DIR/scripts/tests/fixtures/nginx.prod.before-image-timeout.conf" || return
    rm "$PROJECT_DIR/scripts/ensure_image_proxy_nginx.py"
  else
    echo previous
  fi
}
compose_fixture() { printf '%s\n' "$*" >> "$calls"; }
: > "$calls"
rollback_deployment
grep -Fx -- '-f docker-compose.yml up -d --no-deps --force-recreate frontend' "$calls"
[[ ! -e "$PROJECT_DIR/scripts/ensure_image_proxy_nginx.py" ]]
echo 'PASS: actual mounted template repair, frontend selection/recreation and runtime nginx gates'
