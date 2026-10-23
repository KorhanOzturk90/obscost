#!/usr/bin/env bash
# Port-forward the Mimir gateway to localhost:$MIMIR_PORT and Grafana to
# localhost:$GRAFANA_PORT, in the background, re-establishing either if
# its pod moves (a port-forward dies with its pod - and the event scripts
# restart pods on purpose). Works against any cluster; nothing is exposed
# beyond 127.0.0.1.
#
#   scripts/forward.sh        # start (no-op if already running)
#   scripts/forward.sh stop
set -euo pipefail
source "$(dirname "$0")/lib.sh"
mkdir -p "$STATE"
pidfile=$STATE/forward.pid

running() { [[ -f $pidfile ]] && kill -0 "$(cat "$pidfile")" 2>/dev/null; }

if [[ ${1:-} == stop ]]; then
  if running; then kill -- "-$(cat "$pidfile")" 2>/dev/null || kill "$(cat "$pidfile")"; fi
  rm -f "$pidfile"
  exit 0
fi

if ! running; then
  # One process group for both loops, so `stop` takes them all.
  KUBECONFIG=${KUBECONFIG:-} setsid bash -c '
    loop() { while true; do kubectl port-forward --address 127.0.0.1 -n "$1" "svc/$2" "$3:$4" >/dev/null 2>&1; sleep 2; done; }
    loop '"$MIMIR_NS"' mimir-gateway '"$MIMIR_PORT"' 80 &
    loop '"$MONITORING_NS"' kps-grafana '"$GRAFANA_PORT"' 80 &
    wait' >"$STATE/forward.log" 2>&1 < /dev/null &
  echo $! > "$pidfile"
fi

for _ in $(seq 30); do
  curl -fsS -o /dev/null "http://127.0.0.1:$MIMIR_PORT/ready" 2>/dev/null && break
  sleep 1
done
curl -fsS -o /dev/null "http://127.0.0.1:$MIMIR_PORT/ready" ||
  { echo "Mimir gateway not answering on 127.0.0.1:$MIMIR_PORT" >&2; exit 1; }
log "Mimir: http://127.0.0.1:$MIMIR_PORT   Grafana: http://127.0.0.1:$GRAFANA_PORT"
