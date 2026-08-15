#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."
PROJECT_DIR="$(pwd)"
LOG_FILE="$PROJECT_DIR/scripts/deploy.log"

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

# Pick whichever compose CLI is installed: the legacy docker-compose binary or
# the v2 plugin subcommand. OCI hosts (Ubuntu 22.04+) only ship the plugin.
if command -v docker-compose >/dev/null 2>&1; then
  COMPOSE="docker-compose"
elif docker compose version >/dev/null 2>&1; then
  COMPOSE="docker compose"
else
  log "ERROR: neither 'docker-compose' nor 'docker compose' is available"
  exit 1
fi

log "=== Auto deploy started (compose=$COMPOSE) ==="

# 1. Save current commit for rollback
PREV_COMMIT=$(git rev-parse HEAD)
log "Current commit: $PREV_COMMIT"

# 2. Pull latest main
git fetch origin master
BEHIND=$(git rev-list HEAD..origin/master --count 2>/dev/null || echo "0")
CHANGED_FILES=$(git diff --name-only HEAD..origin/master 2>/dev/null || true)

if [ "$BEHIND" = "0" ]; then
  log "Already up to date, nothing to do."
  exit 0
fi

log "Behind by $BEHIND commits, pulling..."
git merge --no-edit origin/master

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
if $COMPOSE up -d --build 2>&1 | tee -a "$LOG_FILE"; then
  # 4. Health check: wait and verify containers are healthy
  log "Build succeeded, running health check..."
  sleep 15

  FAILED=$($COMPOSE ps --filter "status=exited" -q 2>/dev/null | wc -l | tr -d ' ')
  if [ "$FAILED" -gt 0 ]; then
    log "⚠️  $FAILED container(s) exited after build, rolling back..."
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
else
  log "⚠️  compose build failed, rolling back..."
  ROLLBACK=true
fi

# 5. Rollback if needed
if [ "${ROLLBACK:-false}" = "true" ]; then
  log "Rolling back to $PREV_COMMIT..."
  git checkout "$PREV_COMMIT"
  $COMPOSE up -d --build 2>&1 | tee -a "$LOG_FILE"
  sleep 10
  log "Rollback complete. Staying on $(git rev-parse --short HEAD)"
fi
