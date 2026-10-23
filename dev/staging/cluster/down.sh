#!/usr/bin/env bash
# Delete the local cluster and its data volume. Only ever touches the
# k3s container cluster/up.sh created.
set -euo pipefail
cd "$(dirname "$0")"
source ./lib.sh
: "${STATE:=$PWD/../.state}"
"$(dirname "$0")/../scripts/forward.sh" stop 2>/dev/null || true
docker rm -f "$NODE" >/dev/null 2>&1 && log "removed $NODE" || log "$NODE not running"
docker volume rm "$NODE-data" >/dev/null 2>&1 || true
rm -f "$STATE/kubeconfig"
