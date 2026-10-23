#!/usr/bin/env bash
# Remove everything `make deploy` installed, including Mimir's data
# (PVCs, bucket), so a different PROFILE can be deployed. Leaves the
# cluster itself and the Prometheus Operator CRDs.
set -euo pipefail
source "$(dirname "$0")/lib.sh"
helm -n "$TENANTS_NS" uninstall tenant-agent --wait 2>/dev/null || true
helm -n "$MONITORING_NS" uninstall kps --wait 2>/dev/null || true
helm -n "$MIMIR_NS" uninstall mimir --wait 2>/dev/null || true
kubectl delete namespace "$TENANTS_NS" "$MIMIR_NS" "$MONITORING_NS" object-store --ignore-not-found --wait=true
log "undeployed"
