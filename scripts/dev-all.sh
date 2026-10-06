#!/usr/bin/env bash
# Runs the backend (:8787) and the dashboard (:3000) together. Ctrl+C stops both.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
[ -d "$ROOT/apps/dashboard/node_modules" ] || (cd "$ROOT/apps/dashboard" && bun install)

"$ROOT/scripts/dev.sh" &
BACK=$!
(cd "$ROOT/apps/dashboard" && SPARKBASE_API_URL=http://localhost:8787 bun run dev) &
FRONT=$!
trap 'kill $BACK $FRONT 2>/dev/null; wait 2>/dev/null' INT TERM EXIT
echo "backend  http://localhost:8787"
echo "frontend http://localhost:3000"
wait
