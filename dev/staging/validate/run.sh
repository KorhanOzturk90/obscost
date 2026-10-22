#!/usr/bin/env bash
# One validation run against a known environment:
#
#   ./run.sh local             # dev/mimir-local via ../workload/local (classic, RF=1, local rule evaluation)
#   ./run.sh staging-ingest    # dev/staging, PROFILE=ingest  (ingest storage, remote rule evaluation)
#   ./run.sh staging-classic   # dev/staging, PROFILE=classic (classic RF=3, remote rule evaluation)
#
# WINDOWS (default 1h,6h,24h), EVENTS (an events JSON file) and, for staging,
# EXPECT_EVAL (remote; local after `make mimir RULER_EVAL=local`) pass through.
# PROMCOST names the binary; it is the only place the name lives.
set -euo pipefail
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WORKLOAD="$HERE/../workload"
: "${PROMCOST:?set PROMCOST to the promcost binary}"
export PROMCOST
WINDOWS=${WINDOWS:-1h,6h,24h}
args=()
[ -n "${EVENTS:-}" ] && args+=(--events "$EVENTS")
[ -n "${OUT:-}" ] && args+=(--out "$OUT")

case "${1:-}" in
  local)
    exec "$HERE/validate.py" --env local --url http://localhost:8080 --metrics-tenant infra \
      --ground-truth "$WORKLOAD/.run/ground-truth.json" \
      --expect-eval local --expect-write-path classic --windows "$WINDOWS" "${args[@]}"
    ;;
  staging-ingest | staging-classic)
    # dev/staging's gateway, forwarded to the host (its MIMIR_PORT).
    url=${MIMIR_URL:-http://localhost:8091}
    wp=$([ "$1" = staging-ingest ] && echo "ingest storage" || echo classic)
    # dev/staging's state dir: KUBECONFIG, `make promcost-config`'s promcost.yaml
    # (replica counts read from the cluster), `make events-json`'s events.json.
    state=${STAGING_STATE:-$HERE/../.state}
    export KUBECONFIG=${KUBECONFIG:-$state/kubeconfig}
    [ -s "$state/promcost.yaml" ] && args+=(--config "$state/promcost.yaml")
    [ -z "${EVENTS:-}" ] && [ -s "$state/events.json" ] && args+=(--events "$state/events.json")
    make -s -C "$WORKLOAD" k8s-truth >/dev/null
    exec "$HERE/validate.py" --env "$1" --url "$url" --metrics-tenant monitoring \
      --ground-truth "$WORKLOAD/.run/ground-truth-k8s.json" \
      --observer-log "$WORKLOAD/.run/observer-$1.jsonl" \
      --expect-eval "${EXPECT_EVAL:-remote}" --expect-write-path "$wp" --windows "$WINDOWS" "${args[@]}"
    ;;
  *)
    sed -n '2,10p' "$0" | sed 's/^# \{0,1\}//'
    exit 2
    ;;
esac
