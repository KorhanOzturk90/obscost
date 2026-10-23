#!/usr/bin/env bash
# Operational event on dev/mimir-local: restart the (monolithic) Mimir.
# Every cortex_* counter resets, and the ingester rebuilds its in-memory
# active-series tracker from samples arriving after the restart. It is the
# local stand-in for an ingester rollout. Appends {name, start, end} to
# .run/events.json for the harness (--events).
set -euo pipefail
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
EVENTS="$HERE/../.run/events.json"
now() { date -u +%Y-%m-%dT%H:%M:%SZ; }

start=$(now)
docker restart promcost-validation-mimir-1 >/dev/null
until [ "$(curl -s -o /dev/null -w '%{http_code}' http://localhost:8080/ready)" = 200 ]; do sleep 2; done
end=$(now)

[ -s "$EVENTS" ] || echo '[]' > "$EVENTS"
jq --arg s "$start" --arg e "$end" '. + [{name: "Mimir restart (counter reset, active-series tracker rebuilt)", start: $s, end: $e}]' \
  "$EVENTS" > "$EVENTS.tmp" && mv "$EVENTS.tmp" "$EVENTS"
echo "restart: $start → $end (recorded in $EVENTS)"
