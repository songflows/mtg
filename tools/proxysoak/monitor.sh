#!/usr/bin/env bash
# Poll API + server connection stats for DURATION seconds.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
# shellcheck source=/dev/null
source "$ROOT/deploy/.env"

DURATION="${1:-120}"
INTERVAL="${2:-10}"
BASE="https://${MTG_DOMAIN}:${MTG_API_PUBLIC_PORT}"
USERS=(105880381 311471741)

echo "=== soak monitor ${DURATION}s, interval ${INTERVAL}s ==="
start=$(date +%s)
while (( $(date +%s) - start < DURATION )); do
  ts=$(date -u +%H:%M:%S)
  echo "--- $ts ---"
  for u in "${USERS[@]}"; do
    json=$(curl -sk "$BASE/v1/users/$u" -H "Authorization: $MTG_API_AUTH_HEADER")
    echo "$u: $(echo "$json" | python3 -c "import sys,json; d=json.load(sys.stdin)['data']; print(f\"conn={d.get('current_connections')} ips={d.get('active_unique_ips')} expired={d.get('expired')}\")" 2>/dev/null || echo "$json")"
  done
  ssh -o ConnectTimeout=5 "${MTG_SSH_USER}@${MTG_SSH_HOST}" \
    "ss -tn state established sport = :${MTG_PROXY_PORT} 2>/dev/null | tail -n +2 | wc -l | xargs echo established:" \
    2>/dev/null || echo "ssh: skip"
  sleep "$INTERVAL"
done
echo "=== done ==="
