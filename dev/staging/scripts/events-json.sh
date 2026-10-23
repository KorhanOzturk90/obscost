#!/usr/bin/env bash
# The event log ($EVENT_LOG, JSON lines) as the validation harness's
# --events file: a JSON list of {name, start, end, profile, detail}, one
# entry per event that started, with `end` null for one that failed or is
# still running. `noop` records are left out: nothing happened.
#   scripts/events-json.sh > .state/events.json
set -euo pipefail
source "$(dirname "$0")/lib.sh"
: "${EVENT_LOG:=$STATE/events.jsonl}"
[[ -s $EVENT_LOG ]] || { echo "[]"; exit 0; }
jq -s '
  reduce .[] as $r ({open: {}, out: []};
    if $r.phase == "start" then .open[$r.event] = (.out | length)
      | .out += [{name: $r.event, start: $r.ts, end: null, profile: $r.profile, detail: $r.detail}]
    elif ($r.phase == "end" or $r.phase == "failed") and (.open[$r.event] != null) then
      .out[.open[$r.event]] |= (if $r.phase == "end" then .end = $r.ts else .failed = $r.ts end)
      | del(.open[$r.event])
    else . end)
  | .out' "$EVENT_LOG"
