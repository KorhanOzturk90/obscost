#!/usr/bin/env bash
# Scale ingesters to REPLICAS per zone (default 2, from the chart's 1),
# e.g. `make event-scale-ingesters ARGS=REPLICAS=2` or
# `REPLICAS=1 events/scale-ingesters.sh` to go back.
#
# Under ingest storage each new ingester owns a new Kafka partition, so new
# series start landing on it; under classic it takes ring tokens and
# series move. Re-running with the current count logs a `noop`. Scaling
# down goes through the rollout-operator's prepare-downscale webhook, as
# in production. After scaling, re-run `make promcost-config` so the
# inventory's replica count matches.
set -euo pipefail
for a in "$@"; do [[ $a == REPLICAS=* ]] && REPLICAS=${a#REPLICAS=}; done
source "$(dirname "$0")/lib.sh"
REPLICAS=${REPLICAS:-2}
[[ $REPLICAS =~ ^[1-9][0-9]*$ ]] || { echo "REPLICAS must be a positive integer" >&2; exit 2; }

wait_settled ingester 600
zones=$(objects ingester)
before=$(for z in $zones; do kubectl -n "$MIMIR_NS" get "$z" -o jsonpath='{.spec.replicas}{"\n"}'; done | sort -u | paste -sd, -)
detail=$(jq -cn --arg from "$before" --argjson to "$REPLICAS" '{component: "ingester", per_zone_from: $from, per_zone_to: $to}')
if [[ $before == "$REPLICAS" ]]; then
  event_log noop "$detail"
  exit 0
fi
begin_event "$detail"
# shellcheck disable=SC2086
kubectl -n "$MIMIR_NS" scale $zones --replicas="$REPLICAS" >/dev/null
sleep 5
wait_settled ingester
end_event "$detail"
