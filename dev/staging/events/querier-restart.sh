#!/usr/bin/env bash
# Rolling restart of the querier(s). The main read path's queriers restart: query-path counters (cortex_query_*) reset on those pods.
# Safe to re-run: each run is one complete restart, and it waits for any
# rollout already in progress first.
set -euo pipefail
source "$(dirname "$0")/lib.sh"
rolling_restart querier
