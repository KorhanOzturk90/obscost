#!/usr/bin/env bash
# The dev/mimir-k8s tenants' rules, synced by that rig's own script (it
# targets the same in-cluster gateway and namespace names as this env).
set -euo pipefail
exec "$(dirname "$0")/../../mimir-k8s/scripts/sync-rules.sh" "$@"
