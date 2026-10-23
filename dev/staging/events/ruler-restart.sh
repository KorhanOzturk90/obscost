#!/usr/bin/env bash
# Rolling restart of the ruler(s). The ruler restarts: rule-evaluation counters reset and rule groups are re-sharded onto the new pod.
# Safe to re-run: each run is one complete restart, and it waits for any
# rollout already in progress first.
set -euo pipefail
source "$(dirname "$0")/lib.sh"
rolling_restart ruler
