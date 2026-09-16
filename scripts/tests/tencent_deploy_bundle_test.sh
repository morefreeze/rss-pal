#!/usr/bin/env bash
set -euo pipefail
ROOT_DIR=$(cd "$(dirname "$0")/../.." && pwd)
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT
mkdir -p "$TMP/source/scripts" "$TMP/bin"
cat > "$TMP/source/scripts/auto_deploy.sh" <<'PROBE'
#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
[ "$AUTO_DEPLOY_TARGET_COMMIT" = "$(git rev-parse FETCH_HEAD)" ]
printf '%s\n' "$AUTO_DEPLOY_TARGET_COMMIT" > "$PROBE_OUTPUT"
PROBE
git init -q --initial-branch=master "$TMP/source"
git -C "$TMP/source" add .
git -C "$TMP/source" -c user.name=test -c user.email=test@example.com commit -qm initial
git clone -q "$TMP/source" "$TMP/repo"
printf new > "$TMP/source/new"
git -C "$TMP/source" add .
git -C "$TMP/source" -c user.name=test -c user.email=test@example.com commit -qm update
SHA=$(git -C "$TMP/source" rev-parse HEAD)
git -C "$TMP/source" branch deploy
git -C "$TMP/source" bundle create "$TMP/release.bundle" deploy
# No usable origin: bundle deployment must not depend on any network fetch.
git -C "$TMP/repo" remote set-url origin /nonexistent/origin
cat > "$TMP/bin/sudo" <<'MOCK'
#!/bin/bash
[ "$1" = -H ] && shift
[ "$1" = -u ] && shift 2
exec "$@"
MOCK
printf '#!/bin/bash\nexit 0\n' > "$TMP/bin/flock"
printf '#!/bin/bash\nexit 1\n' > "$TMP/bin/systemctl"
cat > "$TMP/bin/bash" <<'MOCK'
#!/bin/bash
if [ "${1:-}" = -lc ]; then shift; exec /bin/bash --noprofile --norc -c "$@"; fi
exec /bin/bash "$@"
MOCK
chmod +x "$TMP/bin/"*
sed -e "s#^LOCK_FILE=.*#LOCK_FILE=$TMP/lock#" -e "s#^REPO_DIR=.*#REPO_DIR=$TMP/repo#" "$ROOT_DIR/deploy/tencent/rss-pal-deploy-from-actions" > "$TMP/wrapper"
export PROBE_OUTPUT="$TMP/probe"
PATH="$TMP/bin:$PATH" /bin/bash "$TMP/wrapper" "$TMP/release.bundle" "$SHA"
[ "$(cat "$PROBE_OUTPUT")" = "$SHA" ]
rm "$PROBE_OUTPUT"
if PATH="$TMP/bin:$PATH" /bin/bash "$TMP/wrapper" "$TMP/release.bundle" 0000000000000000000000000000000000000000; then
 echo 'FAIL: accepted mismatched commit' >&2; exit 1
fi
[ ! -e "$PROBE_OUTPUT" ]
printf corrupt > "$TMP/corrupt.bundle"
if PATH="$TMP/bin:$PATH" /bin/bash "$TMP/wrapper" "$TMP/corrupt.bundle" "$SHA"; then
 echo 'FAIL: accepted corrupt bundle' >&2; exit 1
fi
[ ! -e "$PROBE_OUTPUT" ]
# Exercise the real deployment script with the verified local target and a dead
# origin. A docs-only commit must sync without fetching or touching services.
cp "$ROOT_DIR/scripts/auto_deploy.sh" "$TMP/repo/scripts/auto_deploy.sh"
cat > "$TMP/bin/docker" <<'MOCK'
#!/bin/bash
[ "$*" = "compose version" ] && exit 0
echo "unexpected Docker operation: $*" >&2
exit 1
MOCK
chmod +x "$TMP/bin/docker"
RSS_PAL_DEPLOY_DIRECT=1 AUTO_DEPLOY_TARGET_COMMIT="$SHA" PATH="$TMP/bin:$PATH" /bin/bash "$TMP/repo/scripts/auto_deploy.sh"
[ "$(git -C "$TMP/repo" rev-parse HEAD)" = "$SHA" ]
# Re-running an older job must never rewind a newer deployment.
OLD_SHA=$(git -C "$TMP/repo" rev-parse HEAD^)
if RSS_PAL_DEPLOY_DIRECT=1 AUTO_DEPLOY_TARGET_COMMIT="$OLD_SHA" PATH="$TMP/bin:$PATH" /bin/bash "$TMP/repo/scripts/auto_deploy.sh"; then
 echo 'FAIL: accepted stale target' >&2; exit 1
fi
[ "$(git -C "$TMP/repo" rev-parse HEAD)" = "$SHA" ]
echo 'PASS: offline bundle deployment, mismatch/corruption rejection, exact target and no rewind' 
