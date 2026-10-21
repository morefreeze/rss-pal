#!/usr/bin/env bash
set -euo pipefail
ROOT_DIR=$(cd "$(dirname "$0")/../.." && pwd)
SOURCE_FILE="$ROOT_DIR/scripts/auto_deploy.sh"
eval "$(sed -n '/^add_deploy_service() {$/,/^}$/p; /^select_deploy_services() {$/,/^}$/p; /^normalize_status_migrate_command() {$/,/^}$/p; /^only_status_migrate_command_changed() {$/,/^}$/p; /^deploy_runtime_services() {$/,/^}$/p' "$SOURCE_FILE")"
TASK_DIR=$(mktemp -d)
trap 'rm -rf "$TASK_DIR"' EXIT
cd "$TASK_DIR"
git init -q
git config user.name 'Migration scope test'
git config user.email 'migration-scope@example.invalid'
cat > docker-compose.yml <<'YAML'
services:
  status-migrate:
    image: postgres:15-alpine
    command: ["sh", "-ec", "psql -v ON_ERROR_STOP=1 -f /migrations/059_article_capture_drafts.sql"]
  api:
    image: api
  status-monitor:
    image: monitor
YAML
git add docker-compose.yml
git commit -qm base
BEFORE=$(git rev-parse HEAD)
sed 's@059_article_capture_drafts.sql@059_article_capture_drafts.sql \&\& psql -v ON_ERROR_STOP=1 -f /migrations/061_shortcut_capture_notes.sql@' docker-compose.yml > next.yml
mv next.yml docker-compose.yml
git add docker-compose.yml
git commit -qm migration
AFTER=$(git rev-parse HEAD)
select_deploy_services $'docker-compose.yml\nbackend/migrations/061_shortcut_capture_notes.sql\nfrontend/src/api/client.ts' "$BEFORE" "$AFTER"
[[ "$DEPLOY_ALL" = false && "${DEPLOY_SERVICES[*]:-}" = 'api worker frontend' ]] || { echo "FAIL: migration-only compose change must preserve unrelated services; all=$DEPLOY_ALL services=${DEPLOY_SERVICES[*]:-}" >&2; exit 1; }
compose_calls=""
compose_mock() { compose_calls+="$*"$'\n'; }
COMPOSE=compose_mock
COMPOSE_FILES=(-f docker-compose.yml)
deploy_runtime_services
[[ "$compose_calls" = $'-f docker-compose.yml build api worker frontend\n-f docker-compose.yml up -d --no-deps frontend\n-f docker-compose.yml up -d api worker\n' ]] || { echo "FAIL: migration rollout omitted dependencies or touched other services: $compose_calls" >&2; exit 1; }
# A changed image or extra service configuration must retain the full rollout.
sed 's/image: monitor/image: monitor-new/' docker-compose.yml > next.yml
mv next.yml docker-compose.yml
git add docker-compose.yml
git commit -qm structural
STRUCTURAL=$(git rev-parse HEAD)
select_deploy_services 'docker-compose.yml' "$BEFORE" "$STRUCTURAL"
[[ "$DEPLOY_ALL" = true ]] || { echo 'FAIL: structural compose change escaped full rollout' >&2; exit 1; }
select_deploy_services 'docker-compose.override.yml' "$BEFORE" "$AFTER"
[[ "$DEPLOY_ALL" = true ]] || { echo 'FAIL: an override must retain full rollout' >&2; exit 1; }
select_deploy_services 'docker-compose.yml' missing "$AFTER"
[[ "$DEPLOY_ALL" = true ]] || { echo 'FAIL: missing revision must fail closed to full rollout' >&2; exit 1; }
# A non-psql command or removed migration command cannot use the exception.
git checkout -q "$AFTER" -- docker-compose.yml
sed 's/psql -v ON_ERROR_STOP=1 -f /echo /g' docker-compose.yml > next.yml
mv next.yml docker-compose.yml
git add docker-compose.yml
git commit -qm invalid-migration-command
INVALID=$(git rev-parse HEAD)
select_deploy_services 'docker-compose.yml' "$BEFORE" "$INVALID"
[[ "$DEPLOY_ALL" = true ]] || { echo 'FAIL: non-migration commands must retain full rollout' >&2; exit 1; }
echo 'PASS: migration-only compose changes keep API/worker migration dependencies and preserve unrelated services'
