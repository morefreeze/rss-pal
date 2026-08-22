#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."
PROJECT_DIR="$(pwd)"
LOG_FILE="$PROJECT_DIR/scripts/deploy.log"
AUTO_DEPLOY_REEXEC="${AUTO_DEPLOY_REEXEC:-0}"
AUTO_DEPLOY_PREV_COMMIT="${AUTO_DEPLOY_PREV_COMMIT:-}"
AUTO_DEPLOY_CHANGED_FILES="${AUTO_DEPLOY_CHANGED_FILES:-}"

log() { echo "[$(date '+%Y-%m-%d %H:%M:%S')] $*" | tee -a "$LOG_FILE"; }

configure_outbound_proxy() {
  if [ -n "${https_proxy:-${HTTPS_PROXY:-}}" ]; then
    log "Using existing outbound proxy settings"
    return
  fi

  local proxy="${DEPLOY_PROXY:-http://172.18.0.1:3128}"
  if curl -fsS --max-time 5 -x "$proxy" https://api.github.com/rate_limit >/dev/null 2>&1; then
    export http_proxy="$proxy"
    export https_proxy="$proxy"
    export HTTP_PROXY="$proxy"
    export HTTPS_PROXY="$proxy"
    export no_proxy="${no_proxy:-localhost,127.0.0.1,::1,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16}"
    export NO_PROXY="${NO_PROXY:-$no_proxy}"
    log "Using outbound proxy: $proxy"
  else
    log "No outbound proxy configured; continuing with direct network"
  fi
}

configure_outbound_proxy

legacy_compose_supported() {
  local version="$1"
  if [[ ! "$version" =~ ([0-9]+)\.([0-9]+)\.([0-9]+) ]]; then
    return 1
  fi
  local major="${BASH_REMATCH[1]}"
  local minor="${BASH_REMATCH[2]}"
  local patch="${BASH_REMATCH[3]}"
  (( major > 1 || (major == 1 && (minor > 29 || (minor == 29 && patch >= 2))) ))
}

# Prefer the v2 plugin. Legacy Compose must be at least 1.29.2 because older
# releases do not support depends_on: condition: service_completed_successfully.
if docker compose version >/dev/null 2>&1; then
  COMPOSE="docker compose"
elif command -v docker-compose >/dev/null 2>&1; then
  LEGACY_COMPOSE_VERSION=$(docker-compose version --short 2>/dev/null || docker-compose version 2>/dev/null || true)
  if ! legacy_compose_supported "$LEGACY_COMPOSE_VERSION"; then
    log "ERROR: docker-compose must be version 1.29.2 or newer for status-migrate (found: ${LEGACY_COMPOSE_VERSION:-unknown})"
    exit 1
  fi
  COMPOSE="docker-compose"
else
  log "ERROR: neither 'docker-compose' nor 'docker compose' is available"
  exit 1
fi

log "=== Auto deploy started (compose=$COMPOSE) ==="

# Refresh the OCI egress tunnel if the unit exists. The systemd service can
# hold a stale forward (listener alive, TCP channel dead) after network flaps;
# all overseas feed fetches then fail with EOF until the tunnel is restarted.
if systemctl cat rss-pal-oci-egress.service >/dev/null 2>&1; then
  if sudo -n systemctl restart rss-pal-oci-egress.service 2>/dev/null; then
    log "Restarted rss-pal-oci-egress.service (fresh egress tunnel)"
  else
    log "WARN: could not restart rss-pal-oci-egress.service (passwordless sudo unavailable?)"
  fi
fi

# Compose file set. With no -f flags, docker compose only auto-loads
# docker-compose.yml + docker-compose.override.yml — the OCI egress override
# is silently skipped and every deploy drops the egress proxy env from
# api/worker/rsshub, breaking all GFW-blocked feeds until manually fixed.
# Always pass the full -f list explicitly.
COMPOSE_FILES=(-f docker-compose.yml)
if [ -f docker-compose.override.yml ]; then
  COMPOSE_FILES+=(-f docker-compose.override.yml)
fi
EGRESS_FILE=$(ls docker-compose.override.oci-egress*.yml 2>/dev/null | sort -V | tail -1 || true)
if [ -n "$EGRESS_FILE" ]; then
  COMPOSE_FILES+=(-f "$EGRESS_FILE")
  log "Including egress override: $EGRESS_FILE"
fi

check_runtime_services() {
  local services service container_ids container_id runtime_state runtime_status failed=0
  if ! services=$($COMPOSE "${COMPOSE_FILES[@]}" config --services 2>/dev/null); then
    log "ERROR: could not enumerate configured Compose services"
    return 1
  fi
  if [ -z "$services" ]; then
    log "ERROR: Compose returned no configured services"
    return 1
  fi

  while IFS= read -r service; do
    [ -z "$service" ] && continue
    if [ "$service" = "status-migrate" ]; then
      continue
    fi
    if ! container_ids=$($COMPOSE "${COMPOSE_FILES[@]}" ps -a -q "$service" 2>/dev/null); then
      log "ERROR: could not query runtime service $service"
      return 1
    fi
    container_id="${container_ids%%$'\n'*}"
    if [ -z "$container_id" ]; then
      log "ERROR: runtime service $service has no container"
      failed=$((failed + 1))
      continue
    fi
    if ! runtime_state=$(docker inspect -f '{{.State.Status}} {{.State.ExitCode}}' "$container_id" 2>/dev/null); then
      log "ERROR: could not inspect runtime service $service"
      return 1
    fi
    runtime_status="${runtime_state%% *}"
    if [ "$runtime_status" != "running" ]; then
      log "ERROR: runtime service $service is $runtime_state"
      failed=$((failed + 1))
    fi
  done <<< "$services"

  if [ "$failed" -gt 0 ]; then
    log "ERROR: $failed runtime service(s) are missing or not running"
    return 1
  fi
}

rollback_deployment() {
  log "Rolling back to $PREV_COMMIT..."
  git checkout "$PREV_COMMIT"
  if $COMPOSE "${COMPOSE_FILES[@]}" up -d --build 2>&1 | tee -a "$LOG_FILE"; then
    sleep 10
    log "Rollback complete. Staying on $(git rev-parse --short HEAD)"
  else
    log "Rollback rebuild failed. Staying on $(git rev-parse --short HEAD)"
  fi
}

# 1. Save current commit and pull latest main. A continuation keeps the
# original rollback target and changed-file set after re-execing a new script.
CONTINUATION="false"
if [ "$AUTO_DEPLOY_REEXEC" = "1" ]; then
  if [ -z "$AUTO_DEPLOY_PREV_COMMIT" ]; then
    log "ERROR: auto-deploy continuation is missing its rollback commit"
    exit 1
  fi
  PREV_COMMIT="$AUTO_DEPLOY_PREV_COMMIT"
  CHANGED_FILES="$AUTO_DEPLOY_CHANGED_FILES"
  CONTINUATION="true"
  log "Continuing deployment after auto_deploy.sh re-exec (rollback=$PREV_COMMIT)"
else
  PREV_COMMIT=$(git rev-parse HEAD)
  log "Current commit: $PREV_COMMIT"

  git fetch origin master
  BEHIND=$(git rev-list HEAD..origin/master --count 2>/dev/null || echo "0")
  CHANGED_FILES=$(git diff --name-only HEAD..origin/master 2>/dev/null || true)

  if [ "$BEHIND" = "0" ]; then
    log "Already up to date, nothing to do."
    exit 0
  fi

  log "Behind by $BEHIND commits, pulling..."
  git merge --no-edit origin/master

  if printf '%s\n' "$CHANGED_FILES" | grep -Fxq 'scripts/auto_deploy.sh' && [ "$AUTO_DEPLOY_REEXEC" != "1" ]; then
    log "auto_deploy.sh changed; starting the merged script once"
    if env AUTO_DEPLOY_REEXEC=1 AUTO_DEPLOY_PREV_COMMIT="$PREV_COMMIT" AUTO_DEPLOY_CHANGED_FILES="$CHANGED_FILES" /bin/bash "$PROJECT_DIR/scripts/auto_deploy.sh"; then
      exit 0
    else
      REEXEC_STATUS=$?
      log "merged auto_deploy.sh failed with exit code $REEXEC_STATUS"
      if [ "$(git rev-parse HEAD)" != "$PREV_COMMIT" ]; then
        rollback_deployment
      fi
      exit "$REEXEC_STATUS"
    fi
  fi
fi

RUNTIME_CHANGED=false
if [ -n "$CHANGED_FILES" ]; then
  while IFS= read -r file; do
    case "$file" in
      backend/*|frontend/*|status-monitor/*|docker-compose*.yml|certs/*|nginx.prod.conf|rss-pal.nginx)
        RUNTIME_CHANGED=true
        break
        ;;
    esac
  done <<EOF_CHANGED
$CHANGED_FILES
EOF_CHANGED
fi

if [ "$RUNTIME_CHANGED" != "true" ]; then
  log "No runtime changes detected; repository sync complete."
  exit 0
fi

# 3. Rebuild and restart
log "Building and restarting containers..."
if $COMPOSE "${COMPOSE_FILES[@]}" up -d --build 2>&1 | tee -a "$LOG_FILE"; then
  # 4. Health check: wait and verify containers are healthy
  log "Build succeeded, running health check..."
  sleep 15

  if ! STATUS_MIGRATE_IDS=$($COMPOSE "${COMPOSE_FILES[@]}" ps -a -q status-migrate 2>/dev/null); then
    log "⚠️  could not query status-migrate after compose up, rolling back..."
    ROLLBACK=true
  else
    STATUS_MIGRATE_ID="${STATUS_MIGRATE_IDS%%$'\n'*}"
    if [ -z "$STATUS_MIGRATE_ID" ]; then
      log "⚠️  status-migrate container is missing after compose up, rolling back..."
      ROLLBACK=true
    elif ! STATUS_MIGRATE_EXIT_CODE=$(docker inspect -f '{{.State.ExitCode}}' "$STATUS_MIGRATE_ID" 2>/dev/null); then
      log "⚠️  could not inspect status-migrate after compose up, rolling back..."
      ROLLBACK=true
    elif [ "$STATUS_MIGRATE_EXIT_CODE" != "0" ]; then
      log "⚠️  status-migrate exited with code $STATUS_MIGRATE_EXIT_CODE, rolling back..."
      ROLLBACK=true
    elif $COMPOSE "${COMPOSE_FILES[@]}" rm -f status-migrate 2>&1 | tee -a "$LOG_FILE"; then
      log "status-migrate completed successfully and was removed"
    else
      log "⚠️  could not remove successful status-migrate container, rolling back..."
      ROLLBACK=true
    fi
  fi

  if [ "${ROLLBACK:-false}" != "true" ]; then
    if ! check_runtime_services; then
      log "⚠️  runtime service check failed after build, rolling back..."
      ROLLBACK=true
    else
      # Try hitting the API to confirm it's alive
      API_PORT=$(grep -oP 'SERVER_PORT=\K\d+' "$PROJECT_DIR/.env" 2>/dev/null || echo "8080")
      HEALTH=$(curl -sf -o /dev/null -w "%{http_code}" "http://localhost:$API_PORT/api/health" 2>/dev/null || echo "000")
      if [ "$HEALTH" = "200" ] || [ "$HEALTH" = "401" ]; then
        log "✅ Deploy successful! New commit: $(git rev-parse --short HEAD)"
        exit 0
      else
        log "⚠️  Health check failed (HTTP $HEALTH), rolling back..."
        ROLLBACK=true
      fi
    fi
  fi
else
  log "⚠️  compose build failed, rolling back..."
  ROLLBACK=true
fi

# 5. Rollback if needed
if [ "${ROLLBACK:-false}" = "true" ]; then
  rollback_deployment
  exit 1
fi
