# Shared by events/*.sh: operational events the validation lane triggers
# mid-run. Each script
#   - does one operator action the way an operator would (kubectl against
#     the chart's own objects; zone-aware StatefulSets are rolled by the
#     rollout-operator, as in production),
#   - waits until the cluster has settled again,
#   - and appends a `start` and an `end` record to the event log.
#
# Event log: one JSON object per line, $EVENT_LOG (default
# .state/events.jsonl). Fields:
#   ts       RFC 3339 UTC, second precision    unix  same, seconds
#   event    script name                       phase start | end | noop | failed
#   profile  ingest | classic                  detail free text / object
# A `noop` record (and no start/end) means the cluster was already in the
# requested state - e.g. scaling to the replica count it already has.

source "$(dirname "${BASH_SOURCE[0]}")/../scripts/lib.sh"
: "${EVENT_LOG:=$STATE/events.jsonl}"
EVENT=$(basename "$0" .sh)
mkdir -p "$(dirname "$EVENT_LOG")"

event_log() { # <phase> [detail json]
  local now; now=$(date -u +%s)
  jq -cn --arg ts "$(date -u -d "@$now" +%FT%TZ)" --argjson unix "$now" \
    --arg event "$EVENT" --arg phase "$1" --arg profile "$(current_profile)" \
    --argjson detail "${2:-null}" \
    '{ts: $ts, unix: $unix, event: $event, phase: $phase, profile: $profile, detail: $detail}' |
    tee -a "$EVENT_LOG" >&2
}

current_profile() {
  if kubectl -n "$MIMIR_NS" get statefulset mimir-kafka >/dev/null 2>&1; then echo ingest; else echo classic; fi
}

# Run the rest of the script with start/end records, and a `failed`
# record if it exits non-zero.
begin_event() {
  event_log start "${1:-null}"
  trap 'rc=$?; if ((rc)); then event_log failed "{\"exit\": $rc}"; fi' EXIT
}
end_event() { event_log end "${1:-null}"; trap - EXIT; }

# The workload objects behind a component: its Deployment, or its
# zone-aware StatefulSets (one per zone).
objects() { # <component>
  kubectl -n "$MIMIR_NS" get deploy,statefulset -l "app.kubernetes.io/component=$1" -o name
}

# Wait until every object of the component runs its latest revision with
# all replicas ready. For the zone-aware StatefulSets (updateStrategy
# OnDelete) the rollout-operator, not the StatefulSet controller, deletes
# pods - one zone at a time - so `kubectl rollout status` cannot be used.
wait_settled() { # <component> [timeout seconds]
  local comp=$1 deadline=$((SECONDS + ${2:-1200})) obj
  while ((SECONDS < deadline)); do
    local settled=1
    for obj in $(objects "$comp"); do
      if [[ $obj == deployment* ]]; then
        kubectl -n "$MIMIR_NS" rollout status "$obj" --timeout=5s >/dev/null 2>&1 || settled=0
      else
        local s; s=$(kubectl -n "$MIMIR_NS" get "$obj" -o json)
        local want ready upd cur
        want=$(jq '.spec.replicas' <<<"$s"); ready=$(jq '.status.readyReplicas // 0' <<<"$s")
        upd=$(jq -r '.status.updateRevision' <<<"$s")
        # every pod of this StatefulSet on the update revision
        cur=$(kubectl -n "$MIMIR_NS" get pods -o json | jq --arg o "${obj#statefulset.apps/}" --arg u "$upd" \
          '[.items[] | select(.metadata.ownerReferences[0].name == $o)
             | select(.metadata.labels["controller-revision-hash"] != $u or .metadata.deletionTimestamp != null)] | length')
        [[ $want == "$ready" && $cur == 0 ]] || settled=0
      fi
    done
    ((settled)) && return 0
    sleep 5
  done
  echo "$comp did not settle in time" >&2
  return 1
}

# A rolling restart of every pod of a component, the operator's way:
# `kubectl rollout restart` stamps a new pod-template annotation. A
# Deployment rolls itself; zone-aware StatefulSets are rolled zone by zone
# by the rollout-operator.
rolling_restart() { # <component>
  local comp=$1 objs
  objs=$(objects "$comp")
  [[ -n $objs ]] || { echo "no $comp in namespace $MIMIR_NS" >&2; exit 1; }
  wait_settled "$comp" 600 # never start on top of an unfinished rollout
  local pods_before; pods_before=$(kubectl -n "$MIMIR_NS" get pods -l "app.kubernetes.io/component=$comp" -o name | sort | paste -sd, -)
  begin_event "$(jq -cn --arg c "$comp" --arg p "$pods_before" '{component: $c, pods: ($p | split(","))}')"
  # shellcheck disable=SC2086
  kubectl -n "$MIMIR_NS" rollout restart $objs >/dev/null
  sleep 5
  wait_settled "$comp"
  end_event "$(jq -cn --arg c "$comp" '{component: $c}')"
}
