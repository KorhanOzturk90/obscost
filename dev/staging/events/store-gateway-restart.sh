#!/usr/bin/env bash
# Rolling restart of the store-gateway(s). Every store-gateway restarts, one zone at a time, re-syncing block index headers from the bucket.
# Safe to re-run: each run is one complete restart, and it waits for any
# rollout already in progress first.
set -euo pipefail
source "$(dirname "$0")/lib.sh"
rolling_restart store-gateway
