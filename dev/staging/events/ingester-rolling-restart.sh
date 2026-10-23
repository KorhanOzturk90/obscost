#!/usr/bin/env bash
# Rolling restart of the ingester(s). Every ingester restarts, one zone at a time: counters reset, in-memory series are replayed from the WAL (classic) or Kafka (ingest storage) and active series repopulate.
# Safe to re-run: each run is one complete restart, and it waits for any
# rollout already in progress first.
set -euo pipefail
source "$(dirname "$0")/lib.sh"
rolling_restart ingester
