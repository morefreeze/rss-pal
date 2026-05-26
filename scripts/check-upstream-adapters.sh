#!/usr/bin/env bash
# scripts/check-upstream-adapters.sh
#
# Shows OpenCLI commits that have touched the twitter adapter files since
# the "Last reviewed" date in docs/extension-adapters/upstream-map.md.
#
# Output is informational — our adapters are independent implementations,
# not derivatives. Heavy churn upstream is a SIGNAL that x.com changed,
# not a directive to copy code. After running this, manually re-verify our
# adapters on x.com if you want to act on it.
set -euo pipefail

UPSTREAM_DIR="${UPSTREAM_DIR:-/tmp/opencli-upstream}"
MAP="docs/extension-adapters/upstream-map.md"

if [ ! -d "$UPSTREAM_DIR/.git" ]; then
  echo "Cloning OpenCLI to $UPSTREAM_DIR..."
  git clone --depth 200 https://github.com/jackwener/opencli "$UPSTREAM_DIR"
else
  echo "Updating $UPSTREAM_DIR..."
  git -C "$UPSTREAM_DIR" fetch --depth 200 origin
  git -C "$UPSTREAM_DIR" reset --hard origin/HEAD
fi

# Extract the "Last reviewed" date from the map (first instance only)
SINCE=$(grep -m 1 "^Last manual review:" "$MAP" | sed 's/^Last manual review: //')
if [ -z "$SINCE" ]; then
  echo "Could not find 'Last manual review:' line in $MAP"
  exit 1
fi
echo "Last review: $SINCE"
echo ""

for f in clis/twitter/list-tweets.js clis/twitter/tweets.js clis/twitter/bookmarks.js; do
  echo "=== $f (since $SINCE) ==="
  git -C "$UPSTREAM_DIR" log --since="$SINCE" --oneline -- "$f" 2>/dev/null || echo "(no upstream commits since review)"
  echo ""
done

echo "Done. If upstream churned heavily, manually QA our adapters in a logged-in browser."
echo "After verifying, edit '$MAP' to bump 'Last manual review:' to today's date."
